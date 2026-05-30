package cron

import (
	"strconv"
	"strings"
)

type errClass string

const (
	errClassTimeout   errClass = "timeout"
	errClassRateLimit errClass = "rate_limit"
	errClassServer    errClass = "server_error"
	errClassExec      errClass = "execution"
)

// classifyError classifies an error into a canonical category.
// Used by both errorType (metrics label) and isTemporaryError (retry decision).
func classifyError(err error) errClass {
	if err == nil {
		return errClassExec
	}
	msg := strings.ToLower(err.Error())
	if containsAny(msg, "timeout", "deadline exceeded") {
		return errClassTimeout
	}
	if containsAny(msg, "rate limit", "429") {
		return errClassRateLimit
	}
	if isHTTPStatus(msg, 500, 502, 503, 504) {
		return errClassServer
	}
	if containsAny(msg, "connection refused", "temporary") {
		return errClassTimeout
	}
	return errClassExec
}

// isHTTPStatus checks whether msg contains standalone HTTP status codes (e.g. "500"
// or "502") without matching substrings like "500ms" or "15000". This prevents
// false positives from timeout durations containing numeric substrings.
func isHTTPStatus(msg string, codes ...int) bool {
	for _, code := range codes {
		s := strconv.Itoa(code)
		for {
			idx := strings.Index(msg, s)
			if idx < 0 {
				break
			}
			// Verify the match is a standalone number, not part of a larger number.
			prevOK := idx == 0 || !(isDigit(msg[idx-1]) || isLetter(msg[idx-1]))
			nextOK := idx+len(s) >= len(msg) || !(isDigit(msg[idx+len(s)]) || isLetter(msg[idx+len(s)]))
			if prevOK && nextOK {
				return true
			}
			msg = msg[idx+len(s):]
		}
	}
	return false
}

func isDigit(c byte) bool  { return c >= '0' && c <= '9' }
func isLetter(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }

// errorType returns the metric label for an execution error.
func errorType(err error) string {
	return string(classifyError(err))
}

// isTemporaryError reports whether an execution error is retriable.
func isTemporaryError(err error) bool {
	if err == nil {
		return false
	}
	switch classifyError(err) {
	case errClassTimeout, errClassRateLimit, errClassServer:
		return true
	default:
		return false
	}
}

func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
