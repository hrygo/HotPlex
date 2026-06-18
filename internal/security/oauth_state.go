package security

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	// stateCookieName is the HTTP cookie name for the OAuth state parameter.
	stateCookieName = "oauth_state"

	// stateCookieTTL is the maximum lifetime of an OAuth flow (5 minutes).
	// The state cookie must outlive the user's interaction with the IdP login page.
	stateCookieTTL = 5 * time.Minute

	// pkceVerifierLen is the length of the PKCE code verifier in bytes (64).
	// RFC 7636 recommends 43-128 characters; 64 bytes hex-encoded = 128 chars.
	pkceVerifierLen = 64
)

// StateCookiePayload holds the data stored in the signed OAuth state cookie.
type StateCookiePayload struct {
	State        string
	CodeVerifier string
	Provider     string
	IssuedAt     time.Time
}

// GenerateStateAndVerifier produces a cryptographically random state parameter
// and PKCE code_verifier. The code_challenge (S256) is derived from the verifier.
func GenerateStateAndVerifier() (state, codeVerifier, codeChallenge string, err error) {
	stateBytes := make([]byte, 32)
	if _, err = rand.Read(stateBytes); err != nil {
		return "", "", "", fmt.Errorf("security: generate oauth state: %w", err)
	}
	state = hex.EncodeToString(stateBytes)

	verifierBytes := make([]byte, pkceVerifierLen)
	if _, err = rand.Read(verifierBytes); err != nil {
		return "", "", "", fmt.Errorf("security: generate pkce verifier: %w", err)
	}
	codeVerifier = hex.EncodeToString(verifierBytes)

	hash := sha256.Sum256([]byte(codeVerifier))
	codeChallenge = base64.RawURLEncoding.EncodeToString(hash[:])

	return state, codeVerifier, codeChallenge, nil
}

// SetStateCookie signs and sets the OAuth state cookie on the response.
// The cookie value is HMAC-signed (using the same secret as session cookies)
// to prevent tampering. Format: Base64(state|verifier|provider|timestamp|hmac).
func SetStateCookie(w http.ResponseWriter, r *http.Request, ca *CookieAuth, payload StateCookiePayload) {
	ts := payload.IssuedAt.Unix()
	raw := fmt.Sprintf("%s|%s|%s|%d", payload.State, payload.CodeVerifier, payload.Provider, ts)

	mac := hmac.New(sha256.New, ca.secret)
	mac.Write([]byte(raw))
	sig := hex.EncodeToString(mac.Sum(nil))

	value := base64.RawURLEncoding.EncodeToString([]byte(raw + "|" + sig))

	http.SetCookie(w, &http.Cookie{
		Name:     stateCookieName,
		Value:    value,
		Path:     "/",
		MaxAge:   int(stateCookieTTL.Seconds()),
		HttpOnly: true,
		Secure:   isHTTPS(r),
		SameSite: http.SameSiteLaxMode, // Lax: allow redirect back from IdP
	})
}

// VerifyStateCookie reads and validates the OAuth state cookie, checking:
// 1. Cookie exists and HMAC signature is valid (not tampered).
// 2. Cookie has not expired (within stateCookieTTL).
// 3. The state parameter matches what was stored.
// 4. The provider matches the expected provider (path injection defense).
func VerifyStateCookie(r *http.Request, ca *CookieAuth, expectedState, expectedProvider string) (*StateCookiePayload, error) {
	cookie, err := r.Cookie(stateCookieName)
	if err != nil {
		return nil, fmt.Errorf("oauth state: cookie missing")
	}

	raw, err := base64.RawURLEncoding.DecodeString(cookie.Value)
	if err != nil {
		return nil, fmt.Errorf("oauth state: decode failed")
	}

	// Split: state|verifier|provider|timestamp|hmac
	parts := strings.SplitN(string(raw), "|", 5)
	if len(parts) != 5 {
		return nil, fmt.Errorf("oauth state: malformed cookie")
	}

	state, verifier, provider, tsStr, sigHex := parts[0], parts[1], parts[2], parts[3], parts[4]

	// Verify HMAC signature.
	sig, err := hex.DecodeString(sigHex)
	if err != nil {
		return nil, fmt.Errorf("oauth state: invalid signature encoding")
	}
	payload := state + "|" + verifier + "|" + provider + "|" + tsStr
	mac := hmac.New(sha256.New, ca.secret)
	mac.Write([]byte(payload))
	expected := mac.Sum(nil)
	if !hmac.Equal(sig, expected) {
		return nil, fmt.Errorf("oauth state: signature mismatch")
	}

	// Check expiry.
	ts, err := parseTimestamp(tsStr)
	if err != nil {
		return nil, fmt.Errorf("oauth state: invalid timestamp")
	}
	if time.Since(ts) > stateCookieTTL {
		return nil, fmt.Errorf("oauth state: expired")
	}

	// Verify state and provider match.
	if state != expectedState {
		return nil, fmt.Errorf("oauth state: csrf detected")
	}
	if provider != expectedProvider {
		return nil, fmt.Errorf("oauth state: provider mismatch")
	}

	return &StateCookiePayload{
		State:        state,
		CodeVerifier: verifier,
		Provider:     provider,
		IssuedAt:     ts,
	}, nil
}

// ClearStateCookie expires the OAuth state cookie.
func ClearStateCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   isHTTPS(r),
		SameSite: http.SameSiteLaxMode,
	})
}

func parseTimestamp(s string) (time.Time, error) {
	var ts int64
	if _, err := fmt.Sscanf(s, "%d", &ts); err != nil {
		return time.Time{}, err
	}
	return time.Unix(ts, 0), nil
}
