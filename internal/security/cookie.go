package security

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	// cookieName is the HTTP cookie name for webchat session authentication.
	cookieName = "webchat_session"

	// cookieMaxAge is the default cookie lifetime (24 hours).
	cookieMaxAge = 24 * time.Hour

	// hmacKeyLen is the HMAC secret key length in bytes.
	hmacKeyLen = 32
)

// CookieAuth provides HMAC-SHA256 signed cookie issuance and verification for
// same-origin webchat deployments. The HMAC key is generated at startup via
// crypto/rand and is never stored on disk or embedded in the binary.
//
// Cookie format: Base64(timestamp|userID|hex(HMAC-SHA256(timestamp|userID, secret)))
// Cookie attributes: HttpOnly, SameSite=Strict, Path=/, conditional Secure, 24h Max-Age.
//
// Immutability: the secret and maxAge fields are set once at construction and never
// modified. This allows safe concurrent access from both Hub.HandleHTTP (WS upgrade)
// and Authenticator.AuthenticateRequest (REST API) without additional locking.
type CookieAuth struct {
	secret []byte
	maxAge time.Duration
}

// NewCookieAuth creates a CookieAuth with a cryptographically random HMAC key.
func NewCookieAuth() (*CookieAuth, error) {
	secret := make([]byte, hmacKeyLen)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("security: generate cookie secret: %w", err)
	}
	return &CookieAuth{
		secret: secret,
		maxAge: cookieMaxAge,
	}, nil
}

// SetCookie issues a new HMAC-signed cookie for the given userID.
// If the request already carries a valid cookie, this is a no-op (avoids
// resetting the expiry on every page refresh).
func (c *CookieAuth) SetCookie(w http.ResponseWriter, r *http.Request, userID string) error {
	// Skip if the request already has a valid cookie for this user.
	if uid, ok := c.Authenticate(r); ok && uid == userID {
		return nil
	}

	value := c.sign(userID)

	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    value,
		Path:     "/",
		MaxAge:   int(c.maxAge.Seconds()),
		HttpOnly: true,
		Secure:   isHTTPS(r),
		SameSite: http.SameSiteStrictMode,
	})
	return nil
}

// Authenticate extracts and validates the cookie from the request.
// Returns (userID, true) if valid, ("", false) otherwise.
func (c *CookieAuth) Authenticate(r *http.Request) (string, bool) {
	cookie, err := r.Cookie(cookieName)
	if err != nil {
		return "", false
	}
	return c.verify(cookie.Value)
}

// sign creates a signed cookie value: Base64(timestamp|userID|hexHMAC).
// The HMAC signature is hex-encoded to avoid binary bytes in the delimited format,
// making the cookie value safe for debugging and unambiguous to parse.
func (c *CookieAuth) sign(userID string) string {
	ts := time.Now().Unix()
	payload := fmt.Sprintf("%d|%s", ts, userID)

	mac := hmac.New(sha256.New, c.secret)
	mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))

	return base64.RawURLEncoding.EncodeToString([]byte(payload + "|" + sig))
}

// verify parses and validates a signed cookie value.
func (c *CookieAuth) verify(encoded string) (string, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return "", false
	}

	parts := strings.SplitN(string(raw), "|", 3)
	if len(parts) != 3 {
		return "", false
	}

	tsStr, userID, sigHex := parts[0], parts[1], parts[2]

	// Check timestamp freshness.
	ts, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		return "", false
	}
	if time.Since(time.Unix(ts, 0)) > c.maxAge {
		return "", false
	}

	// Decode hex-encoded HMAC signature.
	sig, err := hex.DecodeString(sigHex)
	if err != nil {
		return "", false
	}

	// Verify HMAC signature (constant-time comparison via hmac.Equal).
	payload := tsStr + "|" + userID
	mac := hmac.New(sha256.New, c.secret)
	mac.Write([]byte(payload))
	expected := mac.Sum(nil)

	if !hmac.Equal(sig, expected) {
		return "", false
	}

	return userID, true
}

// isHTTPS determines if the request was made over HTTPS.
// Checks TLS state and X-Forwarded-Proto header (set by reverse proxies).
// Note: X-Forwarded-Proto is trusted unconditionally — a misconfigured or
// direct-access client could set this header, causing the Secure flag on
// the cookie. This is a self-DoS (cookie won't be sent back over HTTP),
// not a security bypass.
func isHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return r.Header.Get("X-Forwarded-Proto") == "https"
}
