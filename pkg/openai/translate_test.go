package openai

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/vibe-coding-labs/JoyCodeProxy/pkg/joycode"
)

// --- TranslateRequest tests ---

// Test 1: Basic request with model and messages
func TestTranslateRequest_Basic(t *testing.T) {
	req := &ChatRequest{
		Model:    "JoyAI-Code",
		Messages: json.RawMessage(`[{"role":"user","content":"hello"}]`),
	}
	body := TranslateRequest(req)
	if body["model"] != "JoyAI-Code" {
		t.Errorf("expected model=JoyAI-Code, got %v", body["model"])
	}
	msgs, ok := body["messages"].([]interface{})
	if !ok || len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %v", body["messages"])
	}
	msg, ok := msgs[0].(map[string]interface{})
	if !ok {
		t.Fatal("message is not a map")
	}
	if msg["role"] != "user" || msg["content"] != "hello" {
		t.Errorf("unexpected message content: %v", msg)
	}
}

// Test 2: With max_tokens
func TestTranslateRequest_MaxTokens(t *testing.T) {
	req := &ChatRequest{
		Model:     "JoyAI-Code",
		MaxTokens: 1024,
	}
	body := TranslateRequest(req)
	if body["max_tokens"] != 1024 {
		t.Errorf("expected max_tokens=1024, got %v", body["max_tokens"])
	}
}

// Test 3: With temperature
func TestTranslateRequest_Temperature(t *testing.T) {
	temp := 0.7
	req := &ChatRequest{
		Model:       "JoyAI-Code",
		Temperature: &temp,
	}
	body := TranslateRequest(req)
	if body["temperature"] != 0.7 {
		t.Errorf("expected temperature=0.7, got %v", body["temperature"])
	}
}

// Test 4: With top_p
func TestTranslateRequest_TopP(t *testing.T) {
	topP := 0.9
	req := &ChatRequest{
		Model: "JoyAI-Code",
		TopP:  &topP,
	}
	body := TranslateRequest(req)
	if body["top_p"] != 0.9 {
		t.Errorf("expected top_p=0.9, got %v", body["top_p"])
	}
}

// Test 5: With tools
func TestTranslateRequest_Tools(t *testing.T) {
	req := &ChatRequest{
		Model: "JoyAI-Code",
		Tools: json.RawMessage(`[{"type":"function","function":{"name":"test"}}]`),
	}
	body := TranslateRequest(req)
	tools, ok := body["tools"].([]interface{})
	if !ok || len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %v", body["tools"])
	}
	tool, ok := tools[0].(map[string]interface{})
	if !ok {
		t.Fatal("tool is not a map")
	}
	if tool["type"] != "function" {
		t.Errorf("expected tool type=function, got %v", tool["type"])
	}
}

// Test 6: With tool_choice
func TestTranslateRequest_ToolChoice(t *testing.T) {
	req := &ChatRequest{
		Model:      "JoyAI-Code",
		ToolChoice: json.RawMessage(`"auto"`),
	}
	body := TranslateRequest(req)
	if body["tool_choice"] == nil {
		t.Error("expected tool_choice to be set")
	}
	// tool_choice is stored as json.RawMessage, compare as string
	tcBytes, _ := json.Marshal(body["tool_choice"])
	if string(tcBytes) != `"auto"` {
		t.Errorf("expected tool_choice=\"auto\", got %s", tcBytes)
	}
}

// Test 7: With stop sequences
func TestTranslateRequest_Stop(t *testing.T) {
	req := &ChatRequest{
		Model: "JoyAI-Code",
		Stop:  json.RawMessage(`["STOP","END"]`),
	}
	body := TranslateRequest(req)
	if body["stop"] == nil {
		t.Error("expected stop to be set")
	}
}

// Test 8: With thinking (for reasoning model)
func TestTranslateRequest_Thinking(t *testing.T) {
	req := &ChatRequest{
		Model:    "GLM-5.1",
		Thinking: json.RawMessage(`{"type":"enabled","budget_tokens":5000}`),
	}
	body := TranslateRequest(req)
	if body["thinking"] == nil {
		t.Error("expected thinking to be set for reasoning model GLM-5.1")
	}
}

// Test 9: Empty request (no optional fields)
func TestTranslateRequest_Empty(t *testing.T) {
	req := &ChatRequest{}
	body := TranslateRequest(req)
	if body["model"] != "" {
		t.Errorf("expected empty model, got %v", body["model"])
	}
	if _, exists := body["messages"]; exists {
		t.Error("expected no messages key")
	}
	if _, exists := body["max_tokens"]; exists {
		t.Error("expected no max_tokens key")
	}
	if _, exists := body["temperature"]; exists {
		t.Error("expected no temperature key")
	}
	if _, exists := body["top_p"]; exists {
		t.Error("expected no top_p key")
	}
	if _, exists := body["tools"]; exists {
		t.Error("expected no tools key")
	}
}

// Test 10: Stream flag preserved
func TestTranslateRequest_StreamFlag(t *testing.T) {
	req := &ChatRequest{
		Model:  "JoyAI-Code",
		Stream: true,
	}
	body := TranslateRequest(req)
	if body["stream"] != true {
		t.Errorf("expected stream=true, got %v", body["stream"])
	}

	req2 := &ChatRequest{
		Model:  "JoyAI-Code",
		Stream: false,
	}
	body2 := TranslateRequest(req2)
	if body2["stream"] != false {
		t.Errorf("expected stream=false, got %v", body2["stream"])
	}
}

// --- TranslateResponse tests ---

// Test 11: Normal response has all required fields
func TestTranslateResponse_Normal(t *testing.T) {
	jcResp := map[string]interface{}{
		"choices": []interface{}{"choice1"},
		"usage":   map[string]interface{}{"total_tokens": 10},
	}
	resp := TranslateResponse(jcResp, "JoyAI-Code")
	if resp["model"] != "JoyAI-Code" {
		t.Errorf("expected model=JoyAI-Code, got %v", resp["model"])
	}
	if resp["choices"] == nil {
		t.Error("expected choices to be present")
	}
	if resp["usage"] == nil {
		t.Error("expected usage to be present")
	}
	if resp["created"] == nil {
		t.Error("expected created to be present")
	}
}

// Test 12: Verify id has "chatcmpl-" prefix
func TestTranslateResponse_IDPrefix(t *testing.T) {
	jcResp := map[string]interface{}{}
	resp := TranslateResponse(jcResp, "JoyAI-Code")
	id, ok := resp["id"].(string)
	if !ok {
		t.Fatal("id is not a string")
	}
	if !strings.HasPrefix(id, "chatcmpl-") {
		t.Errorf("expected id to start with 'chatcmpl-', got %s", id)
	}
}

// Test 13: Verify object is "chat.completion"
func TestTranslateResponse_Object(t *testing.T) {
	jcResp := map[string]interface{}{}
	resp := TranslateResponse(jcResp, "JoyAI-Code")
	if resp["object"] != "chat.completion" {
		t.Errorf("expected object=chat.completion, got %v", resp["object"])
	}
}

// --- TranslateModels tests ---

// Test 14: Single model
func TestTranslateModels_Single(t *testing.T) {
	models := []joycode.ModelInfo{
		{Label: "JoyAI-Code", ModelID: "JoyAI-Code"},
	}
	result := TranslateModels(models)
	if result["object"] != "list" {
		t.Errorf("expected object=list, got %v", result["object"])
	}
	data, ok := result["data"].([]map[string]interface{})
	if !ok || len(data) != 1 {
		t.Fatalf("expected 1 model entry, got %v", result["data"])
	}
	if data[0]["id"] != "JoyAI-Code" {
		t.Errorf("expected id=JoyAI-Code, got %v", data[0]["id"])
	}
}

// Test 15: Multiple models
func TestTranslateModels_Multiple(t *testing.T) {
	models := []joycode.ModelInfo{
		{Label: "JoyAI-Code", ModelID: "JoyAI-Code"},
		{Label: "GLM-5.1", ModelID: "GLM-5.1"},
		{Label: "Kimi-K2.6", ModelID: "Kimi-K2.6"},
	}
	result := TranslateModels(models)
	data, ok := result["data"].([]map[string]interface{})
	if !ok || len(data) != 3 {
		t.Fatalf("expected 3 model entries, got %d", len(data))
	}
}

// Test 16: Model with ModelID uses ModelID
func TestTranslateModels_UsesModelID(t *testing.T) {
	models := []joycode.ModelInfo{
		{Label: "Display Name", ModelID: "internal-id"},
	}
	result := TranslateModels(models)
	data := result["data"].([]map[string]interface{})
	if data[0]["id"] != "internal-id" {
		t.Errorf("expected id=internal-id, got %v", data[0]["id"])
	}
}

// Test 17: Model without ModelID uses Label
func TestTranslateModels_UsesLabel(t *testing.T) {
	models := []joycode.ModelInfo{
		{Label: "Display Name", ModelID: ""},
	}
	result := TranslateModels(models)
	data := result["data"].([]map[string]interface{})
	if data[0]["id"] != "Display Name" {
		t.Errorf("expected id=Display Name, got %v", data[0]["id"])
	}
}

// Test 18: Model with capabilities includes them
func TestTranslateModels_Capabilities(t *testing.T) {
	models := []joycode.ModelInfo{
		{Label: "JoyAI-Code", ModelID: "JoyAI-Code"},
	}
	result := TranslateModels(models)
	data := result["data"].([]map[string]interface{})
	caps, exists := data[0]["capabilities"]
	if !exists {
		t.Error("expected capabilities for JoyAI-Code")
	}
	capMap, ok := caps.(ModelCapability)
	if !ok {
		t.Fatalf("expected ModelCapability, got %T", caps)
	}
	if capMap.MaxTokens != 64000 {
		t.Errorf("expected MaxTokens=64000, got %d", capMap.MaxTokens)
	}
}

// Test 19: Empty model list
func TestTranslateModels_Empty(t *testing.T) {
	result := TranslateModels([]joycode.ModelInfo{})
	data, ok := result["data"].([]map[string]interface{})
	if !ok {
		t.Fatal("data is not a slice of maps")
	}
	if len(data) != 0 {
		t.Errorf("expected 0 models, got %d", len(data))
	}
}

// --- TranslateStreamChunk tests ---

// Test 20: Valid chunk gets model and id added
func TestTranslateStreamChunk_Valid(t *testing.T) {
	data := `{"choices":[{"delta":{"content":"hello"}}]}`
	result := TranslateStreamChunk(data, "JoyAI-Code")
	if !strings.HasPrefix(result, "data: ") {
		t.Errorf("expected data prefix, got %s", result)
	}
	if !strings.HasSuffix(result, "\n\n") {
		t.Errorf("expected double newline suffix, got %q", result)
	}
	// Extract the JSON payload
	payload := strings.TrimPrefix(result, "data: ")
	payload = strings.TrimSuffix(payload, "\n\n")
	var chunk map[string]interface{}
	if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
		t.Fatalf("invalid JSON in chunk: %s", payload)
	}
	if chunk["model"] != "JoyAI-Code" {
		t.Errorf("expected model=JoyAI-Code, got %v", chunk["model"])
	}
	id, _ := chunk["id"].(string)
	if !strings.HasPrefix(id, "chatcmpl-") {
		t.Errorf("expected id with chatcmpl- prefix, got %s", id)
	}
}

// Test 21: [DONE] passes through
func TestTranslateStreamChunk_Done(t *testing.T) {
	result := TranslateStreamChunk("[DONE]", "JoyAI-Code")
	if result != "data: [DONE]\n\n" {
		t.Errorf("expected 'data: [DONE]\\n\\n', got %q", result)
	}
}

// Test 22: Invalid JSON passes through as-is
func TestTranslateStreamChunk_InvalidJSON(t *testing.T) {
	invalidData := "not json at all"
	result := TranslateStreamChunk(invalidData, "JoyAI-Code")
	expected := "data: not json at all\n\n"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

// Test 23: Verify object is "chat.completion.chunk"
func TestTranslateStreamChunk_Object(t *testing.T) {
	data := `{"choices":[{"delta":{"content":"x"}}]}`
	result := TranslateStreamChunk(data, "JoyAI-Code")
	payload := strings.TrimPrefix(result, "data: ")
	payload = strings.TrimSuffix(payload, "\n\n")
	var chunk map[string]interface{}
	if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
		t.Fatalf("invalid JSON: %s", payload)
	}
	if chunk["object"] != "chat.completion.chunk" {
		t.Errorf("expected object=chat.completion.chunk, got %v", chunk["object"])
	}
}

// --- DefaultModel tests ---

// Test 24: Empty returns JoyAI-Code
func TestDefaultModel_Empty(t *testing.T) {
	result := DefaultModel("")
	if result != joycode.DefaultModel {
		t.Errorf("expected %s, got %s", joycode.DefaultModel, result)
	}
}

// Test 25: Non-empty returns input
func TestDefaultModel_NonEmpty(t *testing.T) {
	result := DefaultModel("GLM-5.1")
	if result != "GLM-5.1" {
		t.Errorf("expected GLM-5.1, got %s", result)
	}
}

// Test 26: Specific model name preserved
func TestDefaultModel_SpecificName(t *testing.T) {
	result := DefaultModel("Kimi-K2.6")
	if result != "Kimi-K2.6" {
		t.Errorf("expected Kimi-K2.6, got %s", result)
	}
}

// --- ModelCapabilities / ReasoningModels tests ---

// Test 27: ReasoningModels map has expected entries
func TestReasoningModels(t *testing.T) {
	expected := []string{"GLM-5.1", "Kimi-K2.6", "MiniMax-M2.7"}
	for _, m := range expected {
		if !ReasoningModels[m] {
			t.Errorf("expected %s to be a reasoning model", m)
		}
	}
	// Verify non-reasoning models are absent
	nonReasoning := []string{"JoyAI-Code", "GLM-5", "GLM-4.7", "Kimi-K2.5"}
	for _, m := range nonReasoning {
		if ReasoningModels[m] {
			t.Errorf("expected %s to NOT be a reasoning model", m)
		}
	}
}

// Test 28: ModelCapabilities has expected entries for all known models
func TestModelCapabilities(t *testing.T) {
	expected := []string{
		"JoyAI-Code", "MiniMax-M2.7", "Kimi-K2.5",
		"Kimi-K2.6", "GLM-5.1", "GLM-5", "GLM-4.7", "Doubao-Seed-2.0-pro",
	}
	for _, m := range expected {
		caps, ok := ModelCapabilities[m]
		if !ok {
			t.Errorf("expected capabilities for %s", m)
			continue
		}
		if caps.Ctx == 0 {
			t.Errorf("expected non-zero Ctx for %s", m)
		}
		if caps.MaxTokens == 0 {
			t.Errorf("expected non-zero MaxTokens for %s", m)
		}
	}
	// Spot-check specific capabilities
	if !ModelCapabilities["MiniMax-M2.7"].Reasoning {
		t.Error("expected MiniMax-M2.7 to have Reasoning=true")
	}
	if !ModelCapabilities["Kimi-K2.5"].Vision {
		t.Error("expected Kimi-K2.5 to have Vision=true")
	}
}
