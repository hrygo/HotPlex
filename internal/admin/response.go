// Package admin implements the HTTP administrative API endpoints.
package admin

import (
	"encoding/json"
	"net/http"
)

// respondJSON writes data as JSON to the response.
func respondJSON(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(data)
}

// respondError writes a consistent error response.
//
//nolint:unused
func respondError(w http.ResponseWriter, msg string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
