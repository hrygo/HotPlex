package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/security"
	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/internal/sqlutil"
)

// setupWorkspaceTestStore builds a real SQLiteStore (migrated) for /admin/workspaces
// handler tests. Mirrors session.helperDB but lives in the admin package so the
// handlers exercise the actual store rather than a hand-rolled mock.
func setupWorkspaceTestStore(t *testing.T) *session.SQLiteStore {
	t.Helper()
	cfg := config.Default()
	cfg.DB.Path = filepath.Join(t.TempDir(), "test.db")
	cfg.DB.SQLite.Path = cfg.DB.Path
	cfg.DB.WALMode = true
	store, err := session.NewSQLiteStore(context.Background(), cfg, sqlutil.NewWriteMu(sqlutil.DialectSQLite))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// setupWorkspaceAPI seeds one owner (Alice/u-1) + one workspace (ws-1, mode
// "workspace") and wires the store into a fresh AdminAPI.
func setupWorkspaceAPI(t *testing.T) *AdminAPI {
	t.Helper()
	store := setupWorkspaceTestStore(t)
	ctx := context.Background()
	require.NoError(t, store.CreateUser(ctx, &security.User{ID: "u-1", Username: "alice", DisplayName: "Alice", Role: "user", Status: "active"}, 1700000000))
	require.NoError(t, store.CreateWorkspace(ctx, &session.Workspace{ID: "ws-1", OwnerUserID: "u-1", Name: "proj", WorkDir: "/tmp/p", PermissionMode: "workspace"}, 1700000000))
	return newTestAPI(func(d *Deps) { d.WorkspaceStore = store })
}

// --- GET /admin/workspaces ---

func TestHandleListAdminWorkspaces(t *testing.T) {
	api := setupWorkspaceAPI(t)
	r := httptest.NewRequest(http.MethodGet, "/admin/workspaces", nil)
	r = withScope(r, ScopeAdminRead)
	w := httptest.NewRecorder()
	api.HandleListAdminWorkspaces(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Workspaces []session.AdminWorkspaceView `json:"workspaces"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Len(t, resp.Workspaces, 1)
	require.Equal(t, "ws-1", resp.Workspaces[0].ID)
	require.Equal(t, "Alice", resp.Workspaces[0].OwnerDisplayName)
	require.Equal(t, "alice", resp.Workspaces[0].OwnerUsername)
	require.Equal(t, "workspace", resp.Workspaces[0].PermissionMode)
}

func TestHandleListAdminWorkspaces_NilStore(t *testing.T) {
	api := newTestAPI() // no WorkspaceStore → multitenancy disabled
	r := httptest.NewRequest(http.MethodGet, "/admin/workspaces", nil)
	r = withScope(r, ScopeAdminRead)
	w := httptest.NewRecorder()
	api.HandleListAdminWorkspaces(w, r)

	require.Equal(t, http.StatusOK, w.Code, "nil store returns empty list, not 503")
	var resp map[string][]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Empty(t, resp["workspaces"])
}

func TestHandleListAdminWorkspaces_Forbidden(t *testing.T) {
	api := setupWorkspaceAPI(t)
	r := httptest.NewRequest(http.MethodGet, "/admin/workspaces", nil)
	r = withScope(r, ScopeSessionRead) // not admin:read
	w := httptest.NewRecorder()
	api.HandleListAdminWorkspaces(w, r)
	require.Equal(t, http.StatusForbidden, w.Code)
}

// --- PATCH /admin/workspaces/{id} ---

func TestHandleUpdateAdminWorkspacePermissionMode_LegalValues(t *testing.T) {
	api := setupWorkspaceAPI(t)
	// All four tiers + "" (clear override) must round-trip (spec §3.2, issue #789).
	for _, mode := range []string{"read-only", "workspace", "auto-edit", "bypass", ""} {
		r := httptest.NewRequest(http.MethodPatch, "/admin/workspaces/{id}",
			strings.NewReader(fmt.Sprintf(`{"permission_mode":%q}`, mode)))
		r.SetPathValue("id", "ws-1")
		r = withScope(r, ScopeAdminWrite)
		w := httptest.NewRecorder()
		api.HandleUpdateAdminWorkspacePermissionMode(w, r)

		require.Equal(t, http.StatusOK, w.Code, "mode %q should be accepted", mode)
		var ws session.Workspace
		require.NoError(t, json.NewDecoder(w.Body).Decode(&ws))
		require.Equal(t, mode, ws.PermissionMode, "mode %q should persist", mode)
	}
}

func TestHandleUpdateAdminWorkspacePermissionMode_InvalidMode(t *testing.T) {
	api := setupWorkspaceAPI(t)
	r := httptest.NewRequest(http.MethodPatch, "/admin/workspaces/{id}",
		strings.NewReader(`{"permission_mode":"god-mode"}`))
	r.SetPathValue("id", "ws-1")
	r = withScope(r, ScopeAdminWrite)
	w := httptest.NewRecorder()
	api.HandleUpdateAdminWorkspacePermissionMode(w, r)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleUpdateAdminWorkspacePermissionMode_MissingField(t *testing.T) {
	api := setupWorkspaceAPI(t)
	// Body without permission_mode → 400 (the field is this endpoint's sole job).
	r := httptest.NewRequest(http.MethodPatch, "/admin/workspaces/{id}", strings.NewReader(`{}`))
	r.SetPathValue("id", "ws-1")
	r = withScope(r, ScopeAdminWrite)
	w := httptest.NewRecorder()
	api.HandleUpdateAdminWorkspacePermissionMode(w, r)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleUpdateAdminWorkspacePermissionMode_NotFound(t *testing.T) {
	api := setupWorkspaceAPI(t)
	r := httptest.NewRequest(http.MethodPatch, "/admin/workspaces/{id}",
		strings.NewReader(`{"permission_mode":"read-only"}`))
	r.SetPathValue("id", "ws-ghost")
	r = withScope(r, ScopeAdminWrite)
	w := httptest.NewRecorder()
	api.HandleUpdateAdminWorkspacePermissionMode(w, r)
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleUpdateAdminWorkspacePermissionMode_Forbidden(t *testing.T) {
	api := setupWorkspaceAPI(t)
	r := httptest.NewRequest(http.MethodPatch, "/admin/workspaces/{id}",
		strings.NewReader(`{"permission_mode":"read-only"}`))
	r.SetPathValue("id", "ws-1")
	r = withScope(r, ScopeAdminRead) // not admin:write
	w := httptest.NewRecorder()
	api.HandleUpdateAdminWorkspacePermissionMode(w, r)
	require.Equal(t, http.StatusForbidden, w.Code)
}

// TestAdminWorkspaceUpdate_AuditTrail locks in the middleware-level audit: a
// successful PATCH /admin/workspaces/{id} must emit an admin_audit record tagged
// workspace.permission_mode.update (spec §3.3, issue #807). Drives the full
// AdminAPI.Middleware so the audit defer fires (handler-direct calls bypass it).
// NOT parallel: SetAuditLogger swaps a process-global logger.
func TestAdminWorkspaceUpdate_AuditTrail(t *testing.T) {
	var buf bytes.Buffer
	SetAuditLogger(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { SetAuditLogger(nil) })

	api := setupWorkspaceAPI(t)
	mux := http.NewServeMux()
	mux.HandleFunc("PATCH /admin/workspaces/{id}", api.HandleUpdateAdminWorkspacePermissionMode)
	handler := api.Middleware(mux)

	r := httptest.NewRequest(http.MethodPatch, "/admin/workspaces/ws-1",
		strings.NewReader(`{"permission_mode":"read-only"}`))
	r.Header.Set("Authorization", "Bearer test-token") // mockConfig token → DefaultScopes (has admin:write)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	out := buf.String()
	require.Contains(t, out, "admin_audit", "PATCH must leave an audit trail")
	require.Contains(t, out, "workspace.permission_mode.update", "audit action must be the workspace enum")
	require.Contains(t, out, "admin-token", "Bearer channel actor is admin-token")
	require.Contains(t, out, "/admin/workspaces/ws-1", "audit target must carry the workspace id")
}
