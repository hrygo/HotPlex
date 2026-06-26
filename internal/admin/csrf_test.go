package admin

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
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

	// P2-1 (issue #794): /api/workspaces/* write routes mount the same csrfMw
	// as /api/admin/*. WorkspaceHandlers authenticates via the SameSite=None
	// cookie fallback (AuthenticateRequest → AuthenticateActiveCookie), so a
	// cross-site write must be blocked at CSRFMiddleware before it reaches the
	// handler. Locks the wiring in routes.go against regression.
	for _, m := range []string{http.MethodPost, http.MethodPatch, http.MethodDelete} {
		method := m
		t.Run("P2-1: workspace "+method+" cross-origin blocked", func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			path := "/api/workspaces"
			if method != http.MethodPost {
				path = "/api/workspaces/ws-1"
			}
			req := httptest.NewRequest(method, path, nil)
			req.Header.Set("Origin", "https://evil.example.com")
			a.CSRFMiddleware(okHandler).ServeHTTP(rec, req)
			require.Equal(t, http.StatusForbidden, rec.Code)
		})
	}
}

// auditMu serializes tests that swap the package-level auditLogger via
// SetAuditLogger. The logger is process-global, so parallel audits would race
// on the swap; tests acquiring this mutex run non-parallel by construction.
var auditMu sync.Mutex

// TestCSRFMiddleware_AuditsCSRFRejects locks issue #794 P2-2: a CSRF 403 must
// leave an admin_audit trail. CSRFMiddleware runs on the gateway mux outside
// AdminAPI.Middleware's audit defer, so without an explicit audit the rejection
// is silent — yet CSRF denials are high-value security events. The proof must
// land for both admin ports: the cookie write routes here, and (via the same
// csrfMw) /api/workspaces/* writes (issue #794 P2-1).
func TestCSRFMiddleware_AuditsCSRFRejects(t *testing.T) {
	auditMu.Lock()
	t.Cleanup(func() { auditMu.Unlock() })

	prev := auditLogger
	t.Cleanup(func() { auditLogger = prev })

	var buf bytes.Buffer
	SetAuditLogger(slog.New(slog.NewTextHandler(&buf, nil)))

	a := &AdminAPI{allowedOriginsFn: func() []string { return []string{"*"} }}
	okHandler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler must not run for a blocked cross-origin write")
	})

	// Workspace write path (P2-1): the csrfMw wrapping /api/workspaces/* uses
	// this same middleware, so the audit proof covers both admin and workspace
	// cookie write routes.
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	a.CSRFMiddleware(okHandler).ServeHTTP(httptest.NewRecorder(), req)

	out := buf.String()
	require.Contains(t, out, "admin_audit", "CSRF rejection must produce an admin_audit record")
	require.Contains(t, out, "actor_user_id=anonymous", "actor must be anonymous (no auth resolved before the 403)")
	require.Contains(t, out, "action="+AuditAuthDenied, "action must be the auth-denied enum")
	require.Contains(t, out, "result="+AuditResultDenied, "result must be denied")
	require.Contains(t, out, "target=/api/workspaces", "target must carry the rejected path for forensics")
}
