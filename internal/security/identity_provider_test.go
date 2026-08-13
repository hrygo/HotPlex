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

// TestValidateUsername_NamespaceReserved: P1 — 密码注册入口封锁机器（apikey-）
// 与 OAuth（oauth-）空间前缀及系统身份字面量（anonymous/api_user），
// 四身份空间静态不相交（spec §5.1.2 P1）。
func TestValidateUsername_NamespaceReserved(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		username string
		wantErr  bool
	}{
		// 保留前缀（机器/OAuth 空间）。
		{"apikey prefix with dash rejected", "apikey-svc1", true},
		{"apikey prefix exact rejected", "apikey-", true},
		{"oauth prefix rejected", "oauth-github", true},
		{"oauth prefix exact rejected", "oauth-", true},
		// 系统身份字面量。
		{"literal anonymous rejected", "anonymous", true},
		{"literal api_user rejected", "api_user", true},
		// 普通用户名不受影响。
		{"ordinary username ok", "alice", false},
		{"ordinary with separators ok", "alice-smith.dev", false},
		{"apikey-containing middle ok", "myapikey-service", false},
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
