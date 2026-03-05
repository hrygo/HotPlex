package base

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
)

// ReadBodyWithError reads the request body and returns an error on failure.
// This is the most basic form - just read and return.
func ReadBodyWithError(r *http.Request) ([]byte, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	return body, nil
}

// ReadBodyWithLog reads the request body and logs errors using the provided logger.
// Returns (body, true) on success, (nil, false) on failure (response already sent).
//
// Usage:
//
//	body, ok := base.ReadBodyWithLog(w, r, logger)
//	if !ok {
//	    return // response already sent
//	}
func ReadBodyWithLog(w http.ResponseWriter, r *http.Request, logger *slog.Logger) ([]byte, bool) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		if logger != nil {
			logger.Error("Read body failed", "error", err)
		}
		http.Error(w, "Bad request", http.StatusBadRequest)
		return nil, false
	}
	return body, true
}

// ReadBodyWithLogAndClose is like ReadBodyWithLog but also closes the body.
// Use this when you don't need the body after reading.
//
// Usage:
//
//	body, ok := base.ReadBodyWithLogAndClose(w, r, logger)
//	if !ok {
//	    return
//	}
func ReadBodyWithLogAndClose(w http.ResponseWriter, r *http.Request, logger *slog.Logger) ([]byte, bool) {
	body, ok := ReadBodyWithLog(w, r, logger)
	if !ok {
		return nil, false
	}
	_ = r.Body.Close()
	return body, true
}

// CheckMethod validates the HTTP method and sends error response if invalid.
// Returns true if method is valid, false otherwise (response already sent).
//
// Usage:
//
//	if !base.CheckMethod(w, r, http.MethodPost) {
//	    return
//	}
func CheckMethod(w http.ResponseWriter, r *http.Request, allowed string) bool {
	if r.Method != allowed {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	return true
}

// CheckMethodPOST is a convenience wrapper for POST method check.
func CheckMethodPOST(w http.ResponseWriter, r *http.Request) bool {
	return CheckMethod(w, r, http.MethodPost)
}

// CheckMethodGET is a convenience wrapper for GET method check.
func CheckMethodGET(w http.ResponseWriter, r *http.Request) bool {
	return CheckMethod(w, r, http.MethodGet)
}
