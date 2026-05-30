package cron

import (
	"regexp"
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
	if isHTTPStatus(msg) {
		return errClassServer
	}
	if containsAny(msg, "connection refused", "temporary") {
		return errClassTimeout
	}
	return errClassExec
}

// httpStatusRe is a pre-compiled regex that matches standalone HTTP status codes,
// using word boundaries to avoid false positives from substrings like "500ms".
var httpStatusRe = regexp.MustCompile(`\b(?:500|502|503|504)\b`)

func isHTTPStatus(msg string) bool {
	return httpStatusRe.MatchString(msg)
}

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
