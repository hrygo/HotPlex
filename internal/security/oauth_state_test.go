package security

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGenerateStateAndVerifier(t *testing.T) {
	t.Parallel()
	state, verifier, challenge, err := GenerateStateAndVerifier()
	require.NoError(t, err)
	require.NotEmpty(t, state)
	require.NotEmpty(t, verifier)
	require.NotEmpty(t, challenge)
	require.Len(t, state, 64, "state is 32 bytes hex = 64 chars")

	// Two calls must produce different values.
	state2, _, _, _ := GenerateStateAndVerifier()
	require.NotEqual(t, state, state2)
}

func TestStateCookie_SetAndVerify(t *testing.T) {
	t.Parallel()
	ca, err := NewCookieAuth("")
	require.NoError(t, err)

	state, verifier, _, _ := GenerateStateAndVerifier()
	payload := StateCookiePayload{
		State:        state,
		CodeVerifier: verifier,
		Provider:     "keycloak",
		IssuedAt:     time.Now(),
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/auth/oauth/keycloak/login", nil)
	SetStateCookie(w, r, ca, payload)

	// Extract the cookie and set it on a new request.
	r2 := httptest.NewRequest(http.MethodGet, "/api/auth/oauth/keycloak/callback?code=x&state="+state, nil)
	for _, c := range w.Result().Cookies() {
		r2.AddCookie(c)
	}

	got, err := VerifyStateCookie(r2, ca, state, "keycloak")
	require.NoError(t, err)
	require.Equal(t, state, got.State)
	require.Equal(t, verifier, got.CodeVerifier)
	require.Equal(t, "keycloak", got.Provider)
}

func TestStateCookie_CSRFDetected(t *testing.T) {
	t.Parallel()
	ca, _ := NewCookieAuth("")

	state, verifier, _, _ := GenerateStateAndVerifier()
	payload := StateCookiePayload{
		State: state, CodeVerifier: verifier, Provider: "kc", IssuedAt: time.Now(),
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	SetStateCookie(w, r, ca, payload)

	r2 := httptest.NewRequest(http.MethodGet, "/callback?code=x&state=WRONG", nil)
	for _, c := range w.Result().Cookies() {
		r2.AddCookie(c)
	}

	_, err := VerifyStateCookie(r2, ca, "WRONG", "kc")
	require.Error(t, err)
	require.Contains(t, err.Error(), "csrf")
}

func TestStateCookie_ProviderMismatch(t *testing.T) {
	t.Parallel()
	ca, _ := NewCookieAuth("")

	state, verifier, _, _ := GenerateStateAndVerifier()
	payload := StateCookiePayload{
		State: state, CodeVerifier: verifier, Provider: "kc", IssuedAt: time.Now(),
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	SetStateCookie(w, r, ca, payload)

	r2 := httptest.NewRequest(http.MethodGet, "/callback", nil)
	for _, c := range w.Result().Cookies() {
		r2.AddCookie(c)
	}

	_, err := VerifyStateCookie(r2, ca, state, "different_provider")
	require.Error(t, err)
	require.Contains(t, err.Error(), "provider mismatch")
}

func TestStateCookie_Expired(t *testing.T) {
	t.Parallel()
	ca, _ := NewCookieAuth("")

	state, verifier, _, _ := GenerateStateAndVerifier()
	payload := StateCookiePayload{
		State: state, CodeVerifier: verifier, Provider: "kc",
		IssuedAt: time.Now().Add(-10 * time.Minute), // expired
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	SetStateCookie(w, r, ca, payload)

	r2 := httptest.NewRequest(http.MethodGet, "/callback", nil)
	for _, c := range w.Result().Cookies() {
		r2.AddCookie(c)
	}

	_, err := VerifyStateCookie(r2, ca, state, "kc")
	require.Error(t, err)
	require.Contains(t, err.Error(), "expired")
}

func TestStateCookie_Tampered(t *testing.T) {
	t.Parallel()
	ca, _ := NewCookieAuth("")

	w := httptest.NewRecorder()
	// Set a tampered cookie directly.
	http.SetCookie(w, &http.Cookie{
		Name:  stateCookieName,
		Value: "tampered_value",
	})

	r2 := httptest.NewRequest(http.MethodGet, "/callback", nil)
	for _, c := range w.Result().Cookies() {
		r2.AddCookie(c)
	}

	_, err := VerifyStateCookie(r2, ca, "any", "any")
	require.Error(t, err)
}

func TestStateCookie_Missing(t *testing.T) {
	t.Parallel()
	ca, _ := NewCookieAuth("")
	r := httptest.NewRequest(http.MethodGet, "/callback", nil)
	_, err := VerifyStateCookie(r, ca, "any", "any")
	require.Error(t, err)
	require.Contains(t, err.Error(), "cookie missing")
}
