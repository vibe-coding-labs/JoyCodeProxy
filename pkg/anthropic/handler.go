package anthropic

import (
	"bufio"
	"encoding/json"
	"log"
	"net/http"

	"github.com/vibe-coding-labs/JoyCodeProxy/pkg/joycode"
)

const chatEndpoint = "/api/saas/openai/v1/chat/completions"

// Handler serves the Anthropic Messages API.
type Handler struct {
	Client *joycode.Client
}

// NewHandler creates a new Anthropic API handler.
func NewHandler(c *joycode.Client) *Handler {
	return &Handler{Client: c}
}

// RegisterRoutes registers the Anthropic Messages API endpoint.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/messages", h.handleMessages)
}

func (h *Handler) handleMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "*")
		w.WriteHeader(200)
		return
	}
	if r.Method != http.MethodPost {
		writeAnthropicError(w, 405, "method not allowed")
		return
	}

	var req MessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAnthropicError(w, 400, "invalid JSON: "+err.Error())
		return
	}
	if req.MaxTokens <= 0 {
		req.MaxTokens = 8192
	}

	if req.Stream {
		h.handleStream(w, &req)
	} else {
		h.handleNonStream(w, &req)
	}
}

func (h *Handler) handleNonStream(w http.ResponseWriter, req *MessageRequest) {
	jcBody := TranslateRequest(req)
	jcResp, err := h.Client.Post(chatEndpoint, jcBody)
	if err != nil {
		writeAnthropicError(w, 500, err.Error())
		return
	}
	resp := TranslateResponse(jcResp, req.Model)
	writeAnthropicJSON(w, 200, resp)
}

func (h *Handler) handleStream(w http.ResponseWriter, req *MessageRequest) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeAnthropicError(w, 500, "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(200)

	jcBody := TranslateRequest(req)
	jcBody["stream"] = true
	resp, err := h.Client.PostStream(chatEndpoint, jcBody)
	if err != nil {
		log.Printf("Stream error: %v", err)
		return
	}
	defer resp.Body.Close()

	msgID := NewMessageID()
	model := req.Model
	totalOutput := 0

	// message_start
	FormatSSE(w, "message_start", sseMessageStart{
		Type: "message_start",
		Message: MessageResponse{
			ID: msgID, Type: "message", Role: "assistant",
			Model: model, Content: []ContentBlock{}, Usage: Usage{},
		},
	})
	FormatSSE(w, "ping", ssePing{Type: "ping"})
	flusher.Flush()

	// Track in-progress tool calls: index -> accumulated data
	type toolCallAccum struct {
		ID        string
		Name      string
		Arguments string
	}
	toolCalls := make(map[int]*toolCallAccum)
	currentBlockIndex := 0
	textBlockStarted := false
	toolBlockStarted := map[int]bool{}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		chunk := ParseStreamChunk(line)
		if chunk == nil || len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]

		// Process tool_calls deltas
		for _, tc := range choice.Delta.ToolCalls {
			idx := tc.Index
			if _, exists := toolCalls[idx]; !exists {
				toolCalls[idx] = &toolCallAccum{
					ID:   tc.ID,
					Name: tc.Function.Name,
				}
			}
			if tc.ID != "" {
				toolCalls[idx].ID = tc.ID
			}
			if tc.Function.Name != "" {
				toolCalls[idx].Name = tc.Function.Name
			}
			toolCalls[idx].Arguments += tc.Function.Arguments

			if !toolBlockStarted[idx] {
				// End text block if it was started
				if textBlockStarted {
					FormatSSE(w, "content_block_stop", sseContentBlockStop{
						Type: "content_block_stop", Index: currentBlockIndex,
					})
					currentBlockIndex++
					textBlockStarted = false
				}
				toolBlockStarted[idx] = true
				tcID := toolCalls[idx].ID
				if tcID == "" {
					tcID = "toolu_" + newID()
				}
				FormatSSE(w, "content_block_start", sseContentBlockStart{
					Type:  "content_block_start",
					Index: currentBlockIndex,
					ContentBlock: ContentBlock{
						Type: "tool_use",
						ID:   tcID,
						Name: toolCalls[idx].Name,
					},
				})
				flusher.Flush()
			}
		}

		// Process text content
		text := choice.Delta.Content
		if text != "" {
			if !textBlockStarted {
				textBlockStarted = true
				FormatSSE(w, "content_block_start", sseContentBlockStart{
					Type:         "content_block_start",
					Index:        currentBlockIndex,
					ContentBlock: ContentBlock{Type: "text", Text: ""},
				})
				flusher.Flush()
			}
			totalOutput += len(text)
			FormatSSE(w, "content_block_delta", sseContentBlockDelta{
				Type:  "content_block_delta",
				Index: currentBlockIndex,
				Delta: deltaText{Type: "text_delta", Text: text},
			})
			flusher.Flush()
		}

		// Handle finish
		if choice.FinishReason != nil {
			fr := *choice.FinishReason
			// Close any open text block
			if textBlockStarted {
				FormatSSE(w, "content_block_stop", sseContentBlockStop{
					Type: "content_block_stop", Index: currentBlockIndex,
				})
				currentBlockIndex++
				textBlockStarted = false
			}
			// Close and flush tool call blocks with input_json_delta
			for i := 0; i < len(toolCalls); i++ {
				tc := toolCalls[i]
				if toolBlockStarted[i] {
					// Send the accumulated arguments as input_json_delta
					FormatSSE(w, "content_block_delta", sseContentBlockDelta{
						Type:  "content_block_delta",
						Index: currentBlockIndex,
						Delta: deltaText{Type: "input_json_delta", Text: tc.Arguments},
					})
					FormatSSE(w, "content_block_stop", sseContentBlockStop{
						Type: "content_block_stop", Index: currentBlockIndex,
					})
					currentBlockIndex++
				}
			}

			stopReason := "end_turn"
			if fr == "tool_calls" {
				stopReason = "tool_use"
			}

			FormatSSE(w, "message_delta", sseMessageDelta{
				Type:  "message_delta",
				Delta: deltaStop{StopReason: stopReason},
				Usage: struct {
					OutputTokens int `json:"output_tokens"`
				}{OutputTokens: totalOutput / 4},
			})
			FormatSSE(w, "message_stop", sseMessageStop{Type: "message_stop"})
			flusher.Flush()
		}
	}
}

func writeAnthropicJSON(w http.ResponseWriter, code int, v interface{}) {
	b, _ := json.Marshal(v)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(code)
	w.Write(b)
}

func writeAnthropicError(w http.ResponseWriter, code int, msg string) {
	writeAnthropicJSON(w, code, map[string]interface{}{
		"type":  "error",
		"error": map[string]string{"type": "api_error", "message": msg},
	})
}
