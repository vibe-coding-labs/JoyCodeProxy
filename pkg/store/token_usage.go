package store

import (
	"context"
	"net/http"
)

type ctxKey struct{}

// InitTokenUsage initializes token usage tracking in the request context.
// Returns a pointer that handlers can update with SetTokenUsage.
func InitTokenUsage(r *http.Request) *http.Request {
	usage := &[2]int{0, 0}
	return r.WithContext(context.WithValue(r.Context(), ctxKey{}, usage))
}

// SetTokenUsage updates the token usage for the request.
func SetTokenUsage(r *http.Request, inputTokens, outputTokens int) {
	if usage, ok := r.Context().Value(ctxKey{}).(*[2]int); ok {
		usage[0] = inputTokens
		usage[1] = outputTokens
	}
}

// GetTokenUsage retrieves the token usage from the request context.
func GetTokenUsage(r *http.Request) (int, int) {
	if usage, ok := r.Context().Value(ctxKey{}).(*[2]int); ok {
		return usage[0], usage[1]
	}
	return 0, 0
}
