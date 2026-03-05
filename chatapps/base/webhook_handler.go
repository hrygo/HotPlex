package base

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// WebhookHandler provides common utilities for webhook processing.
type WebhookHandler struct {
	Logger   *slog.Logger
	Verifier SignatureVerifier
}

// NewWebhookHandler creates a new WebhookHandler.
func NewWebhookHandler(logger *slog.Logger, verifier SignatureVerifier) *WebhookHandler {
	return &WebhookHandler{
		Logger:   logger,
		Verifier: verifier,
	}
}

// HandlerFunc is the function signature for handling parsed events.
type HandlerFunc[T any] func(event T) error

// ParseFunc is the function signature for parsing request body into events.
type ParseFunc[T any] func(body []byte) (T, error)

// ProcessWebhook executes the webhook processing pipeline:
// 1. Method validation (POST only)
// 2. Body reading
// 3. Signature verification (if configured)
// 4. Event parsing
// 5. Event handling
//
// Returns true if processing was successful, false otherwise.
// Error responses are automatically written to the client.
func ProcessWebhook[T any](
	h *WebhookHandler,
	w http.ResponseWriter,
	r *http.Request,
	parse ParseFunc[T],
	handle HandlerFunc[T],
) bool {
	logger := h.Logger

	// 1. Method validation
	if !CheckMethodPOST(w, r) {
		return false
	}

	// 2. Body reading
	body, ok := ReadBodyWithLog(w, r, logger)
	if !ok {
		return false
	}

	// 3. Signature verification
	if !VerifyRequest(h.Verifier, r, body) {
		if logger != nil {
			logger.Warn("Invalid signature")
		}
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return false
	}

	// 4. Event parsing
	event, err := parse(body)
	if err != nil {
		if logger != nil {
			logger.Error("Parse event failed", "error", err)
		}
		http.Error(w, "Bad request", http.StatusBadRequest)
		return false
	}

	// 5. Event handling
	if err := handle(event); err != nil {
		if logger != nil {
			logger.Error("Handle event failed", "error", err)
		}
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return false
	}

	return true
}

// ProcessWebhookNoVerify is like ProcessWebhook but skips signature verification.
func ProcessWebhookNoVerify[T any](
	logger *slog.Logger,
	w http.ResponseWriter,
	r *http.Request,
	parse ParseFunc[T],
	handle HandlerFunc[T],
) bool {
	if !CheckMethodPOST(w, r) {
		return false
	}

	body, ok := ReadBodyWithLog(w, r, logger)
	if !ok {
		return false
	}

	event, err := parse(body)
	if err != nil {
		if logger != nil {
			logger.Error("Parse event failed", "error", err)
		}
		http.Error(w, "Bad request", http.StatusBadRequest)
		return false
	}

	if err := handle(event); err != nil {
		if logger != nil {
			logger.Error("Handle event failed", "error", err)
		}
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return false
	}

	return true
}

// RespondJSON writes a JSON response.
func (h *WebhookHandler) RespondJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

// RespondOK writes a simple OK response.
func (h *WebhookHandler) RespondOK(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{}`))
}

// RespondText writes a plain text response.
func (h *WebhookHandler) RespondText(w http.ResponseWriter, status int, text string) {
	w.WriteHeader(status)
	_, _ = w.Write([]byte(text))
}
