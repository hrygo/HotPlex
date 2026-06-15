package security

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/config"
)

func TestCookieAuth_SetAndVerify(t *testing.T) {
	t.Parallel()
	ca, err := NewCookieAuth()
	require.NoError(t, err)

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	require.NoError(t, ca.SetCookie(w, r, "u-real-123"))

	r2 := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, c := range w.Result().Cookies() {
		r2.AddCookie(c)
	}
	uid, ok := ca.Authenticate(r2)
	require.True(t, ok)
	require.Equal(t, "u-real-123", uid)
}

func TestCookieAuth_SlidingRefreshNearExpiry(t *testing.T) {
	t.Parallel()
	ca, err := NewCookieAuth()
	require.NoError(t, err)

	// Cookie issued 6 days ago; TTL is 7 days → past half-TTL threshold (3.5d) → refresh.
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	ca.setCookieAt(w, r, "u-1", time.Now().Add(-6*24*time.Hour))

	r2 := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, c := range w.Result().Cookies() {
		r2.AddCookie(c)
	}
	w2 := httptest.NewRecorder()
	uid, ok := ca.AuthenticateAndMaybeRefresh(w2, r2)
	require.True(t, ok)
	require.Equal(t, "u-1", uid)
	require.NotEmpty(t, w2.Result().Header.Get("Set-Cookie"), "接近过期必须滑动刷新")
}

func TestCookieAuth_NoRefreshWhenFresh(t *testing.T) {
	t.Parallel()
	ca, _ := NewCookieAuth()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	require.NoError(t, ca.SetCookie(w, r, "u-1"))

	r2 := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, c := range w.Result().Cookies() {
		r2.AddCookie(c)
	}
	w2 := httptest.NewRecorder()
	_, ok := ca.AuthenticateAndMaybeRefresh(w2, r2)
	require.True(t, ok)
	require.Empty(t, w2.Result().Header.Get("Set-Cookie"), "新鲜 cookie 不应刷新")
}

func TestCookieAuth_ExpiredRejected(t *testing.T) {
	t.Parallel()
	ca, _ := NewCookieAuth()
	// Issued 8 days ago, exceeds 7-day TTL → rejected.
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	ca.setCookieAt(w, r, "u-1", time.Now().Add(-8*24*time.Hour))

	r2 := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, c := range w.Result().Cookies() {
		r2.AddCookie(c)
	}
	_, ok := ca.Authenticate(r2)
	require.False(t, ok, "过期 cookie 必须拒绝")
}

func TestAuthenticator_IdentityProvider(t *testing.T) {
	t.Parallel()
	auth := NewAuthenticator(&config.SecurityConfig{})
	require.Nil(t, auth.IdentityProvider(), "默认无 IDP")
	auth.SetIdentityProvider(NewLocalAccountProvider(&stubUserStore{byUsername: map[string]*User{}}, testBcryptCost))
	require.NotNil(t, auth.IdentityProvider())
}
