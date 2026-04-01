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
