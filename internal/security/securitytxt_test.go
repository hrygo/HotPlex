package security

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSecurityTxtHandler(t *testing.T) {
	t.Parallel()

	t.Run("returns 404 when contact is empty", func(t *testing.T) {
		t.Parallel()
		handler := SecurityTxtHandler(func() string { return "" })
		req := httptest.NewRequest(http.MethodGet, "/.well-known/security.txt", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		require.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("returns security.txt with contact", func(t *testing.T) {
		t.Parallel()
		handler := SecurityTxtHandler(func() string { return "mailto:security@example.com" })
		req := httptest.NewRequest(http.MethodGet, "/.well-known/security.txt", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, "text/plain; charset=utf-8", rec.Header().Get("Content-Type"))
		body := rec.Body.String()
		require.Contains(t, body, "Contact: mailto:security@example.com")
		require.Contains(t, body, "Preferred-Languages: zh, en")
		require.Contains(t, body, "Expires:")
	})

	t.Run("sets cache control for 24 hours", func(t *testing.T) {
		t.Parallel()
		handler := SecurityTxtHandler(func() string { return "mailto:sec@example.com" })
		req := httptest.NewRequest(http.MethodGet, "/.well-known/security.txt", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		require.Contains(t, rec.Header().Get("Cache-Control"), "max-age=86400")
	})

	t.Run("accepts URL contact", func(t *testing.T) {
		t.Parallel()
		handler := SecurityTxtHandler(func() string {
			return "https://example.com/security-policy"
		})
		req := httptest.NewRequest(http.MethodGet, "/.well-known/security.txt", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		require.Contains(t, rec.Body.String(), "Contact: https://example.com/security-policy")
	})

	t.Run("does not hardcode any domain", func(t *testing.T) {
		t.Parallel()
		handler := SecurityTxtHandler(func() string { return "mailto:custom@test.org" })
		req := httptest.NewRequest(http.MethodGet, "/.well-known/security.txt", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		body := rec.Body.String()
		// No hardcoded domains should appear
		require.False(t, strings.Contains(body, "hotplex.com") || strings.Contains(body, "hrygo.com"))
	})
}
