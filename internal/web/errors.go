package web

import (
	"encoding/json"
	"net/http"
)

// AppError is the JSON error envelope returned by all API endpoints (spec §12).
//
// Shape: {"error": {"code": "...", "message": "..."}}
type AppError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// WriteAppError writes a structured JSON error envelope with the given HTTP
// status, machine-readable code, and human-readable message.
//
// Shared by gateway multitenancy endpoints and the admin API (P2.8 unification).
// It replaces raw http.Error (which emits plain text) so every API error shares
// one parseable shape — clients no longer have to guess text vs JSON by sniffing
// the Content-Type of a failed response.
func WriteAppError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": AppError{Code: code, Message: msg}})
}
