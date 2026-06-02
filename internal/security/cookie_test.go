package security

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/config"
)

func TestCookieAuthSignVerify(t *testing.T) {
	t.Parallel()

	ca, err := NewCookieAuth()
	require.NoError(t, err)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)

	// Sign a cookie.
	err = ca.SetCookie(w, r, "user1")
	require.NoError(t, err)

	// Extract the cookie from the response.
	cookies := w.Result().Cookies()
	require.Len(t, cookies, 1)
	require.Equal(t, cookieName, cookies[0].Name)
	require.True(t, cookies[0].HttpOnly)
	require.Equal(t, http.SameSiteStrictMode, cookies[0].SameSite)

	// Verify the cookie in a new request.
	r2 := httptest.NewRequest("GET", "/", nil)
	r2.AddCookie(cookies[0])

	uid, ok := ca.Authenticate(r2)
	require.True(t, ok)
	require.Equal(t, "user1", uid)
}

func TestCookieAuthExpiry(t *testing.T) {
	t.Parallel()

	ca, err := NewCookieAuth()
	require.NoError(t, err)

	// Create a cookie with a manually-expired timestamp.
	ca.maxAge = 1 * time.Nanosecond
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)

	err = ca.SetCookie(w, r, "expired_user")
	require.NoError(t, err)

	// No sleep needed: maxAge is 1ns, so time.Since(ts) in verify() will
	// always exceed maxAge by the time we reach this point.

	cookies := w.Result().Cookies()
	require.Len(t, cookies, 1)

	r2 := httptest.NewRequest("GET", "/", nil)
	r2.AddCookie(cookies[0])

	uid, ok := ca.Authenticate(r2)
	require.False(t, ok, "expired cookie should be rejected")
	require.Empty(t, uid)

	// Restore default for other tests.
	ca.maxAge = cookieMaxAge
}

func TestCookieAuthTamper(t *testing.T) {
	t.Parallel()

	ca, err := NewCookieAuth()
	require.NoError(t, err)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)

	err = ca.SetCookie(w, r, "user1")
	require.NoError(t, err)

	cookies := w.Result().Cookies()
	require.Len(t, cookies, 1)

	// Tamper with the cookie value.
	tampered := cookies[0].Value + "x"
	r2 := httptest.NewRequest("GET", "/", nil)
	r2.AddCookie(&http.Cookie{Name: cookieName, Value: tampered})

	uid, ok := ca.Authenticate(r2)
	require.False(t, ok, "tampered cookie should be rejected")
	require.Empty(t, uid)

	// Empty cookie value.
	r3 := httptest.NewRequest("GET", "/", nil)
	r3.AddCookie(&http.Cookie{Name: cookieName, Value: ""})

	uid, ok = ca.Authenticate(r3)
	require.False(t, ok)
	require.Empty(t, uid)

	// Garbage base64.
	r4 := httptest.NewRequest("GET", "/", nil)
	r4.AddCookie(&http.Cookie{Name: cookieName, Value: "!!invalid!!"})

	uid, ok = ca.Authenticate(r4)
	require.False(t, ok)
	require.Empty(t, uid)
}

func TestCookieSecureFlag(t *testing.T) {
	t.Parallel()

	ca, err := NewCookieAuth()
	require.NoError(t, err)

	// HTTP request — Secure should be false.
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	err = ca.SetCookie(w, r, "user1")
	require.NoError(t, err)

	cookies := w.Result().Cookies()
	require.Len(t, cookies, 1)
	require.False(t, cookies[0].Secure, "Secure flag should be false for HTTP")

	// HTTPS request (TLS) — Secure should be true.
	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest("GET", "/", nil)
	r2.TLS = &tls.ConnectionState{} // Simulate HTTPS
	err = ca.SetCookie(w2, r2, "user2")
	require.NoError(t, err)

	cookies2 := w2.Result().Cookies()
	require.Len(t, cookies2, 1)
	require.True(t, cookies2[0].Secure, "Secure flag should be true for HTTPS")

	// X-Forwarded-Proto: https — Secure should be true.
	w3 := httptest.NewRecorder()
	r3 := httptest.NewRequest("GET", "/", nil)
	r3.Header.Set("X-Forwarded-Proto", "https")
	err = ca.SetCookie(w3, r3, "user3")
	require.NoError(t, err)

	cookies3 := w3.Result().Cookies()
	require.Len(t, cookies3, 1)
	require.True(t, cookies3[0].Secure, "Secure flag should be true for X-Forwarded-Proto: https")
}

func TestCookieNoRepeatIssue(t *testing.T) {
	t.Parallel()

	ca, err := NewCookieAuth()
	require.NoError(t, err)

	// First request: issues a cookie.
	w1 := httptest.NewRecorder()
	r1 := httptest.NewRequest("GET", "/", nil)
	err = ca.SetCookie(w1, r1, "user1")
	require.NoError(t, err)
	require.Len(t, w1.Result().Cookies(), 1, "should issue cookie on first request")

	// Second request with valid cookie: should NOT re-issue.
	cookies := w1.Result().Cookies()
	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest("GET", "/", nil)
	r2.AddCookie(cookies[0])
	err = ca.SetCookie(w2, r2, "user1")
	require.NoError(t, err)
	require.Empty(t, w2.Result().Cookies(), "should not re-issue cookie when valid one exists")
}

func TestAuthenticateRequestCookie(t *testing.T) {
	t.Parallel()

	ca, err := NewCookieAuth()
	require.NoError(t, err)

	auth := NewAuthenticator(&config.SecurityConfig{APIKeyHeader: "X-API-Key"})
	auth.SetCookieAuth(ca)

	// Issue a cookie.
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	err = ca.SetCookie(w, r, "cookie_user")
	require.NoError(t, err)

	// Request with cookie (no API key header/query) should succeed.
	cookies := w.Result().Cookies()
	r2 := httptest.NewRequest("GET", "/", nil)
	r2.AddCookie(cookies[0])

	uid, botID, err2 := auth.AuthenticateRequest(r2)
	require.NoError(t, err2)
	require.Equal(t, "cookie_user", uid)
	require.Empty(t, botID)

	// Request without any auth should fail.
	r3 := httptest.NewRequest("GET", "/", nil)
	_, _, err3 := auth.AuthenticateRequest(r3)
	require.ErrorIs(t, err3, ErrUnauthorized)
}

func TestCookieAuthWithBotID(t *testing.T) {
	t.Parallel()

	ca, err := NewCookieAuth()
	require.NoError(t, err)

	auth := NewAuthenticator(&config.SecurityConfig{APIKeyHeader: "X-API-Key"})
	auth.SetCookieAuth(ca)

	// Issue a cookie.
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	err = ca.SetCookie(w, r, "cookie_user")
	require.NoError(t, err)

	// Request with cookie + bot_id query param.
	cookies := w.Result().Cookies()
	r2 := httptest.NewRequest("GET", "/?bot_id=U12345", nil)
	r2.AddCookie(cookies[0])

	uid, botID, err2 := auth.AuthenticateRequest(r2)
	require.NoError(t, err2)
	require.Equal(t, "cookie_user", uid)
	require.Equal(t, "U12345", botID)
}
