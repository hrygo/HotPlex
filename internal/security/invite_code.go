package security

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

// GenerateInviteCode returns a 32-byte cryptographically random invite code,
// base64url-encoded (URL-safe, no padding). Used by admin invitation creation.
// Returns an error if the system's CSPRNG is unavailable.
func GenerateInviteCode() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("security: crypto/rand failed: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
