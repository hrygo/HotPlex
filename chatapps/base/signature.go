package base

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
)

// SignatureVerifier defines the interface for webhook signature verification.
// Different platforms implement different verification strategies.
type SignatureVerifier interface {
	// Verify checks if the request signature is valid.
	// Returns true if valid, false otherwise.
	Verify(r *http.Request, body []byte) bool
}

// MessageFormat defines the message format for signature computation
type MessageFormat string

const (
	// FormatBody uses only the request body
	FormatBody MessageFormat = "body"
	// FormatSlack uses Slack's format: v0:timestamp:body
	FormatSlack MessageFormat = "slack"
	// FormatDingTalk uses DingTalk's format: timestamp+token+nonce (via Query params)
	FormatDingTalk MessageFormat = "dingtalk"
	// FormatFeishu uses Feishu's format: timestamp+secret+body
	FormatFeishu MessageFormat = "feishu"
)

// HMACSHA256Verifier implements HMAC-SHA256 signature verification.
// Used by: Slack, DingTalk, Feishu
type HMACSHA256Verifier struct {
	// Secret is the signing secret/key
	Secret string

	// SignatureHeader is the header name containing the signature
	SignatureHeader string

	// TimestampHeader is the header name containing the timestamp (optional)
	TimestampHeader string

	// Prefix is the signature prefix (e.g., "v0" for Slack, "" for others)
	Prefix string

	// Format defines the message format for signature computation
	Format MessageFormat

	// NonceParam is the query parameter name for nonce (used in DingTalk)
	NonceParam string
}

// Verify implements SignatureVerifier for HMAC-SHA256.
func (v *HMACSHA256Verifier) Verify(r *http.Request, body []byte) bool {
	signature := r.Header.Get(v.SignatureHeader)
	if signature == "" {
		return false
	}

	// Remove prefix if present
	if v.Prefix != "" && len(signature) > len(v.Prefix) {
		signature = signature[len(v.Prefix):]
	}

	// Build message based on format
	message := v.buildMessage(r, body)

	mac := hmac.New(sha256.New, []byte(v.Secret))
	mac.Write([]byte(message))
	expectedSignature := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(signature), []byte(expectedSignature))
}

// buildMessage constructs the message string based on the format
func (v *HMACSHA256Verifier) buildMessage(r *http.Request, body []byte) string {
	switch v.Format {
	case FormatSlack:
		timestamp := r.Header.Get(v.TimestampHeader)
		if timestamp != "" {
			return "v0:" + timestamp + ":" + string(body)
		}
	case FormatDingTalk:
		timestamp := r.URL.Query().Get("timestamp")
		nonce := r.URL.Query().Get(v.NonceParam)
		if timestamp != "" {
			return timestamp + v.Secret + nonce
		}
	case FormatFeishu:
		timestamp := r.Header.Get(v.TimestampHeader)
		if timestamp != "" {
			return timestamp + v.Secret + string(body)
		}
	}
	// Default: FormatBody
	return string(body)
}

// NoOpVerifier is a verifier that always returns true.
// Use this when signature verification is disabled or not needed.
type NoOpVerifier struct{}

// Verify always returns true.
func (v *NoOpVerifier) Verify(_ *http.Request, _ []byte) bool {
	return true
}

// VerifyRequest is a convenience function that checks if a verifier is configured
// and verifies the request. Returns true if no verifier is configured or if
// verification passes.
func VerifyRequest(verifier SignatureVerifier, r *http.Request, body []byte) bool {
	if verifier == nil {
		return true
	}
	return verifier.Verify(r, body)
}

// Compile-time interface compliance checks
var (
	_ SignatureVerifier = (*HMACSHA256Verifier)(nil)
	_ SignatureVerifier = (*NoOpVerifier)(nil)
)
