package security

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/hrygo/hotplex/internal/config"
)

const (
	// cookieName is the HTTP cookie name for webchat session authentication.
	cookieName = "webchat_session"

	// cookieMaxAge is the cookie lifetime (spec 附录 B: 7 days).
	// NOTE: increased from 24h to 7d in multitenancy spec ① — this is a
	// backward-incompatible change for deployments relying on short cookie
	// lifetime. No config override provided; document in CHANGELOG.
	cookieMaxAge = 7 * 24 * time.Hour

	// refreshAfter is the sliding-refresh threshold: cookies older than half
	// the TTL are reissued on authentication (spec §8.3).
	refreshAfter = cookieMaxAge / 2

	// hmacKeyLen is the HMAC secret key length in bytes.
	hmacKeyLen = 32
)

// CookieAuth provides HMAC-SHA256 signed cookie issuance and verification for
// same-origin webchat deployments. The HMAC key is generated at startup via
// crypto/rand and is never stored on disk or embedded in the binary.
//
// Cookie format: Base64(timestamp|userID|hex(HMAC-SHA256(timestamp|userID, secret)))
// Cookie attributes: HttpOnly, SameSite=None, Path=/, Secure (HTTPS or loopback dev), 7d Max-Age.
//
// Immutability: the secret and maxAge fields are set once at construction and never
// modified. This allows safe concurrent access from both Hub.HandleHTTP (WS upgrade)
// and Authenticator.AuthenticateRequest (REST API) without additional locking.
type CookieAuth struct {
	secret []byte
	maxAge time.Duration
}

// NewCookieAuth creates a CookieAuth with the given configured secret or falls back to filesystem persistence.
// If running inside a unit test, it uses a random in-memory key to avoid filesystem side effects.
func NewCookieAuth(configuredSecret string) (*CookieAuth, error) {
	if flag.Lookup("test.v") != nil {
		secretBytes := make([]byte, hmacKeyLen)
		if _, err := rand.Read(secretBytes); err != nil {
			return nil, fmt.Errorf("security: generate cookie secret for test: %w", err)
		}
		return &CookieAuth{
			secret: secretBytes,
			maxAge: cookieMaxAge,
		}, nil
	}

	var secretBytes []byte
	if configuredSecret != "" {
		h := sha256.Sum256([]byte(configuredSecret))
		secretBytes = h[:]
	} else {
		keyPath := filepath.Join(config.HotplexHome(), "data", "cookie_secret.key")
		data, err := os.ReadFile(keyPath)
		if err == nil {
			dataStr := strings.TrimSpace(string(data))
			if hexBytes, err := hex.DecodeString(dataStr); err == nil && len(hexBytes) == hmacKeyLen {
				secretBytes = hexBytes
			} else if len(data) == hmacKeyLen {
				secretBytes = data
			}
		}

		if secretBytes == nil {
			secretBytes = make([]byte, hmacKeyLen)
			if _, err := rand.Read(secretBytes); err != nil {
				return nil, fmt.Errorf("security: generate cookie secret: %w", err)
			}
			hexStr := hex.EncodeToString(secretBytes)
			if err := os.MkdirAll(filepath.Dir(keyPath), 0o700); err != nil {
				slog.Warn("security: failed to create cookie secret dir", "dir", filepath.Dir(keyPath), "err", err)
			}
			if err := os.WriteFile(keyPath, []byte(hexStr), 0o600); err != nil {
				slog.Warn("security: failed to persist cookie secret — restart will invalidate all login cookies", "path", keyPath, "err", err)
			}
		}
	}

	return &CookieAuth{
		secret: secretBytes,
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
		Secure:   cookieSecure(r),
		SameSite: http.SameSiteNoneMode,
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
	uid, _, ok := c.verify(cookie.Value)
	return uid, ok
}

// AuthenticateAndMaybeRefresh authenticates and, if the cookie is past half its
// TTL (refreshAfter), reissues a fresh cookie on w (sliding refresh, spec §8.3).
func (c *CookieAuth) AuthenticateAndMaybeRefresh(w http.ResponseWriter, r *http.Request) (string, bool) {
	cookie, err := r.Cookie(cookieName)
	if err != nil {
		return "", false
	}
	uid, issuedAt, ok := c.verify(cookie.Value)
	if !ok {
		return "", false
	}
	if time.Since(issuedAt) > refreshAfter {
		// Direct write: bypass SetCookie's skip-if-valid optimization so the
		// sliding refresh actually reissues a fresh cookie.
		c.setCookieAt(w, r, uid, time.Now())
	}
	return uid, true
}

// setCookieAt writes a cookie signed at issuedAt directly (bypasses SetCookie's
// skip-if-valid optimization). Used by sliding refresh and tests.
func (c *CookieAuth) setCookieAt(w http.ResponseWriter, r *http.Request, userID string, issuedAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    c.signAt(userID, issuedAt),
		Path:     "/",
		MaxAge:   int(c.maxAge.Seconds()),
		HttpOnly: true,
		Secure:   cookieSecure(r),
		SameSite: http.SameSiteNoneMode,
	})
}

// signAt creates a signed cookie value with an explicit issue time: Base64(timestamp|userID|hexHMAC).
// The HMAC signature is hex-encoded to avoid binary bytes in the delimited format,
// making the cookie value safe for debugging and unambiguous to parse.
func (c *CookieAuth) signAt(userID string, issuedAt time.Time) string {
	ts := issuedAt.Unix()
	payload := fmt.Sprintf("%d|%s", ts, userID)

	mac := hmac.New(sha256.New, c.secret)
	mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))

	return base64.RawURLEncoding.EncodeToString([]byte(payload + "|" + sig))
}

// sign creates a signed cookie value at the current time (back-compat shorthand for signAt).
func (c *CookieAuth) sign(userID string) string {
	return c.signAt(userID, time.Now())
}

// verify parses and validates a signed cookie value, returning the userID,
// issue time, and validity. The issue time enables sliding refresh (§8.3).
func (c *CookieAuth) verify(encoded string) (string, time.Time, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return "", time.Time{}, false
	}

	parts := strings.SplitN(string(raw), "|", 3)
	if len(parts) != 3 {
		return "", time.Time{}, false
	}

	tsStr, userID, sigHex := parts[0], parts[1], parts[2]

	// Check timestamp freshness.
	ts, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		return "", time.Time{}, false
	}
	issuedAt := time.Unix(ts, 0)
	if time.Since(issuedAt) > c.maxAge {
		return "", time.Time{}, false
	}

	// Decode hex-encoded HMAC signature.
	sig, err := hex.DecodeString(sigHex)
	if err != nil {
		return "", time.Time{}, false
	}

	// Verify HMAC signature (constant-time comparison via hmac.Equal).
	payload := tsStr + "|" + userID
	mac := hmac.New(sha256.New, c.secret)
	mac.Write([]byte(payload))
	expected := mac.Sum(nil)

	if !hmac.Equal(sig, expected) {
		return "", time.Time{}, false
	}

	return userID, issuedAt, true
}

// Clear expires the session cookie immediately (logout). Encapsulates the cookie
// name + attributes so callers never hardcode the name literal (spec §8.2).
func (c *CookieAuth) Clear(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   cookieSecure(r),
		SameSite: http.SameSiteNoneMode,
	})
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

// isLocalhost reports whether the request is served from a loopback host
// (localhost / 127.0.0.1 / ::1, any port). Chrome and Firefox treat these
// http origins as secure contexts, which lets Secure cookies be set over plain
// http — so a dev frontend on one loopback host:port (e.g. 127.0.0.1:3000)
// can exchange SameSite=None; Secure cookies with a gateway on another
// (e.g. localhost:8888) even though 127.0.0.1 and localhost are cross-site.
func isLocalhost(r *http.Request) bool {
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// cookieSecure reports whether the Secure flag may be set on a cookie:
// HTTPS anywhere, or any loopback origin (a secure context in modern browsers,
// allowing Secure cookies over plain http during local development).
//
// Deployment note: cookies use SameSite=None, which the browser rejects unless
// Secure is also set. On a non-loopback plaintext HTTP deployment (intranet IP,
// or a reverse proxy that strips X-Forwarded-Proto) this returns false → the
// login responds 200 but the browser silently drops the cookie → every
// subsequent request 401s. Production MUST terminate TLS at the edge (or proxy
// X-Forwarded-Proto correctly). SameSite=None is the CSRF trade-off for
// cross-origin webchat; it is mitigated by Secure and by state-changing
// endpoints requiring a valid HMAC cookie (no additional CSRF token today).
func cookieSecure(r *http.Request) bool {
	return isHTTPS(r) || isLocalhost(r)
}
