package security

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

// GenerateInviteCode returns a 32-byte cryptographically random invite code,
// base64url-encoded (URL-safe, no padding). Used by admin invitation creation.
func GenerateInviteCode() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand should never fail in practice; panic on unrecoverable state.
		panic(fmt.Sprintf("security: crypto/rand failed: %v", err))
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
