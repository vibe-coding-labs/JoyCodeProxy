package openai

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/vibe-coding-labs/JoyCodeProxy/pkg/joycode"
)

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if !requirePOST(w, r) {
		return
	}
	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		slog.Error("decode chat request", "error", err)
		writeError(w, 400, "invalid JSON")
		return
	}
	model := DefaultModel(req.Model)
	jcBody := TranslateRequest(&req)
	client := s.getClient(r)
	if req.Stream {
		s.handleStreamChat(w, client, jcBody, model)
	} else {
		s.handleNonStreamChat(w, client, jcBody, model)
	}
}

func (s *Server) handleNonStreamChat(w http.ResponseWriter, client *joycode.Client, jcBody map[string]interface{}, model string) {
	resp, err := client.Post("/api/saas/openai/v1/chat/completions", jcBody)
	if err != nil {
		slog.Error("chat non-stream upstream error", "model", model, "error", err)
		return
	}
	writeJSON(w, 200, TranslateResponse(resp, model))
}

func (s *Server) handleStreamChat(w http.ResponseWriter, client *joycode.Client, jcBody map[string]interface{}, model string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		slog.Error("streaming not supported by response writer")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "close")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(200)

	resp, err := client.PostStream("/api/saas/openai/v1/chat/completions", jcBody)
	if err != nil {
		slog.Error("chat stream upstream error", "model", model, "error", err)
		fmt.Fprintf(w, "data: {\"error\":{\"message\":\"%s\"}}\n\n", err.Error())
		flusher.Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
		return
	}
	defer resp.Body.Close()

	// Pipe JoyCode SSE response directly — already OpenAI-compatible format
	buf := make([]byte, 4096)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			w.Write(buf[:n])
			flusher.Flush()
		}
		if readErr != nil {
				if readErr.Error() != "EOF" {
					slog.Error("chat stream read error", "model", model, "error", readErr)
				}
			break
		}
	}
}
