package security

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateUsername(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		username string
		wantErr  bool
	}{
		{"empty rejected", "", true},
		{"too short", "ab", true},
		{"too short boundary", "a", true},
		{"min length ok", "abc", false},
		{"typical ok", "alice", false},
		{"with digits ok", "alice123", false},
		{"with separators ok", "alice.b-dev_2", false},
		// Reserved namespace (migration 018 collision / takeover vector).
		{"apikey prefix rejected", "apikey:foo", true},
		{"apikey exact rejected", "apikey:", true},
		// Disallowed charset.
		{"colon rejected", "ali:ce", true},
		{"space rejected", "ali ce", true},
		{"at sign rejected", "ali@ce", true},
		{"slash rejected", "ali/ce", true},
		{"unicode rejected", "alïce", true},
		// Length boundary.
		{"max length ok", stringOfChar('a', UsernameMaxLen), false},
		{"over max rejected", stringOfChar('a', UsernameMaxLen+1), true},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateUsername(tt.username)
			if tt.wantErr {
				require.ErrorIs(t, err, ErrInvalidUsername, "username=%q", tt.username)
			} else {
				require.NoError(t, err, "username=%q", tt.username)
			}
		})
	}
}

func stringOfChar(c rune, n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = c
	}
	return string(b)
}
