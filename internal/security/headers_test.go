package security

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveCSP(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		def      string
		override string
		want     string
	}{
		{"empty override uses default", "default-src 'self'", "", "default-src 'self'"},
		{"whitespace-only override uses default", "default-src 'self'", "   ", "default-src 'self'"},
		{"tab/newline override uses default", "default-src 'self'", "\t\n", "default-src 'self'"},
		{"non-empty override used verbatim", "default-src 'self'", "default-src 'none'", "default-src 'none'"},
		{"override trimmed", "default-src 'self'", "  default-src 'none'  ", "default-src 'none'"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ResolveCSP(tt.def, tt.override)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestIsPermissiveCSP(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		directive string
		want      bool
	}{
		{"empty is not permissive", "", false},
		{"strict default-src", "default-src 'self'", false},
		{"real example strict", "default-src 'self'; script-src 'self'; style-src 'self'", false},
		{"bare wildcard", "default-src *", true},
		{"http: scheme keyword", "default-src 'self' http:", true},
		{"https: scheme keyword", "default-src 'self' https:", true},
		{"ws: scheme keyword", "connect-src 'self' ws:", true},
		{"wss: scheme keyword", "connect-src 'self' wss:", true},
		{"wss://* host pattern", "connect-src 'self' wss://*", true},
		{"https://* host pattern", "connect-src 'self' https://*", true},
		// data:/blob: are safe non-network schemes and must NOT trip.
		{"data: is not permissive", "img-src 'self' data: blob:", false},
		{"blob: is not permissive", "img-src 'self' blob:", false},
		{"default real_example_strict unchanged", "default-src 'self'; script-src 'self' 'unsafe-inline'; " +
			"style-src 'self' 'unsafe-inline'; connect-src 'self' http://192.168.1.100:9999", false},
		{"real_example_permissive trips", "default-src 'self'; connect-src 'self' http: wss://*", true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := IsPermissiveCSP(tt.directive)
			require.Equal(t, tt.want, got, "IsPermissiveCSP(%q)", tt.directive)
		})
	}
}

func TestSecurityHeaders(t *testing.T) {
	t.Parallel()
	t.Run("sets all four headers with default CSP when override is empty", func(t *testing.T) {
		t.Parallel()
		h := SecurityHeaders(DefaultWebChatCSP, "", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		h.ServeHTTP(rec, req)
		require.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
		require.Equal(t, "DENY", rec.Header().Get("X-Frame-Options"))
		require.Equal(t, "strict-origin-when-cross-origin", rec.Header().Get("Referrer-Policy"))
		require.Equal(t, DefaultWebChatCSP, rec.Header().Get("Content-Security-Policy"))
	})

	t.Run("whitespace-only override falls back to default (C12 regression)", func(t *testing.T) {
		t.Parallel()
		h := SecurityHeaders(DefaultDocsCSP, "   ", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		h.ServeHTTP(rec, req)
		require.Equal(t, DefaultDocsCSP, rec.Header().Get("Content-Security-Policy"),
			"whitespace-only override must not ship as a literal CSP header")
	})

	t.Run("non-empty override replaces default", func(t *testing.T) {
		t.Parallel()
		strict := "default-src 'none'"
		h := SecurityHeaders(DefaultWebChatCSP, strict, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		h.ServeHTTP(rec, req)
		require.Equal(t, strict, rec.Header().Get("Content-Security-Policy"))
	})

	t.Run("override is trimmed", func(t *testing.T) {
		t.Parallel()
		strict := "default-src 'none'"
		h := SecurityHeaders(DefaultWebChatCSP, "  "+strict+"  ", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		h.ServeHTTP(rec, req)
		require.Equal(t, strict, rec.Header().Get("Content-Security-Policy"))
	})
}

func TestDefaultDocsCSP_NotPermissive(t *testing.T) {
	t.Parallel()
	// After tightening, docs CSP should NOT contain permissive connect-src
	// (no bare http:/https:/ws:/wss: scheme keywords).
	require.False(t, IsPermissiveCSP(DefaultDocsCSP),
		"DefaultDocsCSP connect-src should be restricted to 'self' only")
	// Verify connect-src 'self' is present (not accidentally removed).
	require.Contains(t, DefaultDocsCSP, "connect-src 'self';")
}

func TestDefaultWebChatCSP_IsPermissive(t *testing.T) {
	t.Parallel()
	// WebChat CSP intentionally keeps permissive connect-src for WebSocket
	// connections to the gateway (ws:/wss: scheme keywords).
	require.True(t, IsPermissiveCSP(DefaultWebChatCSP),
		"DefaultWebChatCSP should remain permissive for zero-config remote deployments")
}
