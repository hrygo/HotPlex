package acp

import "testing"

func TestIsFatalRPCError(t *testing.T) {
	tests := []struct {
		name    string
		message string
		want    bool
	}{
		// Session-lost messages — should be fatal.
		{name: "session not found", message: "Session not found: abc123", want: true},
		{name: "session expired", message: "Session expired for user", want: true},
		{name: "session does not exist", message: "session does not exist in store", want: true},
		{name: "invalid session id", message: "invalid session id: malformed", want: true},
		{name: "invalid session state", message: "invalid session state: corrupted", want: true},

		// Business errors — should NOT be fatal.
		{name: "rate limit", message: "rate limit exceeded", want: false},
		{name: "permission denied", message: "permission denied for resource", want: false},
		{name: "content policy", message: "content policy violation detected", want: false},
		{name: "generic invalid", message: "invalid request format", want: false},
		// False positive boundary: "invalid session" without "id"/"state" is NOT matched.
		// Realistic false-positive scenario: API key error mentioning "session" casually.
		{name: "api key invalid mentions session", message: "your API key is invalid. session will not be stored", want: false},
		{name: "empty message", message: "", want: false},

		// Case normalization.
		{name: "uppercase", message: "SESSION NOT FOUND", want: true},
		{name: "mixed case", message: "Session Expired", want: true},

		// Boundary: non-fatal context with matching substrings.
		{name: "session alone", message: "your session preferences have been saved", want: false},
		{name: "not found without session", message: "resource not found in database", want: false},
		{name: "expired without session", message: "token expired, please re-authenticate", want: false},
		{name: "invalid without session", message: "invalid request body", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := &JSONRPCError{Code: -1, Message: tt.message}
			got := isFatalRPCError(err)
			if got != tt.want {
				t.Errorf("isFatalRPCError(%q) = %v, want %v", tt.message, got, tt.want)
			}
		})
	}
}
