package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSameOriginRequest locks the semantics changed by issue #788 review P1:
// a wildcard "*" in AllowedOrigins must NOT satisfy the same-origin proof.
// Same-origin traffic passes via Sec-Fetch-Site; cross-origin writes need an
// exact match so default (permissive) config is not a CSRF bypass.
func TestSameOriginRequest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		allowed      []string
		secFetchSite string
		origin       string
		want         bool
	}{
		{"same-origin via Sec-Fetch-Site", []string{"*"}, "same-origin", "", true},
		{"same-site via Sec-Fetch-Site", []string{"*"}, "same-site", "", true},
		{"exact origin match", []string{"https://chat.example.com"}, "", "https://chat.example.com", true},
		{"P1: wildcard does not satisfy cross-origin", []string{"*"}, "", "https://evil.example.com", false},
		{"origin not in allowlist", []string{"https://chat.example.com"}, "", "https://evil.example.com", false},
		{"no Sec-Fetch-Site, no Origin", []string{"*"}, "", "", false},
		{"P1: wildcard default blocks cross-site fetch", []string{"*"}, "cross-site", "https://gateway.example.com", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			allowed := tt.allowed
			a := &AdminAPI{allowedOriginsFn: func() []string { return allowed }}
			r := httptest.NewRequest(http.MethodPost, "/api/admin/invitations", nil)
			if tt.secFetchSite != "" {
				r.Header.Set("Sec-Fetch-Site", tt.secFetchSite)
			}
			if tt.origin != "" {
				r.Header.Set("Origin", tt.origin)
			}
			require.Equal(t, tt.want, a.sameOriginRequest(r))
		})
	}
}

// TestCSRFMiddleware locks issue #788 review P0: /api/admin/* cookie write
// routes must reject cross-origin state-changing requests while leaving
// safe reads and same-origin writes untouched.
func TestCSRFMiddleware(t *testing.T) {
	t.Parallel()
	a := &AdminAPI{allowedOriginsFn: func() []string { return []string{"*"} }} // default permissive config
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	t.Run("P0: cross-origin POST blocked", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/admin/invitations", nil)
		req.Header.Set("Origin", "https://evil.example.com")
		a.CSRFMiddleware(okHandler).ServeHTTP(rec, req)
		require.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("P0: cross-origin PATCH blocked", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPatch, "/api/admin/users/u1", nil)
		req.Header.Set("Origin", "https://evil.example.com")
		a.CSRFMiddleware(okHandler).ServeHTTP(rec, req)
		require.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("P0: cross-origin DELETE blocked", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodDelete, "/api/admin/invitations/inv1", nil)
		req.Header.Set("Origin", "https://evil.example.com")
		a.CSRFMiddleware(okHandler).ServeHTTP(rec, req)
		require.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("same-origin POST passes", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/admin/invitations", nil)
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		a.CSRFMiddleware(okHandler).ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("GET passes regardless of origin", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/admin/invitations", nil)
		req.Header.Set("Origin", "https://evil.example.com")
		a.CSRFMiddleware(okHandler).ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
	})
}
