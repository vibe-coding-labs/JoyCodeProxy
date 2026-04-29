package openai

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

func (s *Server) handleWebSearch(w http.ResponseWriter, r *http.Request) {
	if !requirePOST(w, r) {
		return
	}
	var body struct {
		Query string `json:"query"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		slog.Error("decode search request", "error", err)
		writeError(w, 400, "invalid JSON")
		return
	}
	if body.Query == "" {
		writeError(w, 400, "query is required")
		return
	}
	results, err := s.getClient(r).WebSearch(body.Query)
	if err != nil {
		slog.Error("web search upstream error", "query", body.Query, "error", err)
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]interface{}{"search_result": results})
}

func (s *Server) handleRerank(w http.ResponseWriter, r *http.Request) {
	if !requirePOST(w, r) {
		return
	}
	var body struct {
		Query     string   `json:"query"`
		Documents []string `json:"documents"`
		TopN      int      `json:"top_n"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		slog.Error("decode rerank request", "error", err)
		writeError(w, 400, "invalid JSON")
		return
	}
	if body.Query == "" || len(body.Documents) == 0 {
		writeError(w, 400, "query and documents are required")
		return
	}
	result, err := s.getClient(r).Rerank(body.Query, body.Documents, body.TopN)
	if err != nil {
		slog.Error("rerank upstream error", "query", body.Query, "error", err)
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, result)
}
