package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/pkg/events"
)

// wsSandboxDir 构造 owner 沙箱内的 work_dir（$HOME/.hotplex/workspaces/<ownerUserID>/<sub>），
// 使 security.ValidateWorkspaceWorkDir 约束自动满足。用真实 $HOME（与校验同源），可并行；
// 目录无需预先存在 —— workspace Create 只持久化路径字符串。
func wsSandboxDir(t *testing.T, ownerUserID, sub string) string {
	t.Helper()
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	return filepath.Join(home, ".hotplex", "workspaces", ownerUserID, sub)
}

func (e *testAuthEnv) createWorkspace(t *testing.T, cookie, ownerUID, name, sub string) *session.Workspace {
	t.Helper()
	workDir := wsSandboxDir(t, ownerUID, sub)
	body := []byte(`{"name":"` + name + `","work_dir":"` + workDir + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces", bytes.NewReader(body))
	req.Header.Set("Cookie", cookie)
	w := httptest.NewRecorder()
	e.wsHandlers.Create(w, req)
	require.Equal(t, http.StatusOK, w.Code, "create ws body=%s", w.Body.String())
	var ws session.Workspace
	require.NoError(t, json.NewDecoder(w.Body).Decode(&ws))
	return &ws
}

func (e *testAuthEnv) listWorkspaces(t *testing.T, cookie string) []*session.Workspace {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces", nil)
	req.Header.Set("Cookie", cookie)
	w := httptest.NewRecorder()
	e.wsHandlers.List(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Workspaces []*session.Workspace `json:"workspaces"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	return resp.Workspaces
}

func TestWorkspaceCRUD(t *testing.T) {
	t.Parallel()
	env := newTestAuthEnv(t)
	cookie := env.loginAs(t, "admin", "adminpass", http.StatusOK)

	// Create
	ws := env.createWorkspace(t, cookie, "u-admin", "proj", "crud")
	require.NotEmpty(t, ws.ID)
	require.Equal(t, wsSandboxDir(t, "u-admin", "crud"), ws.WorkDir)

	// List → 1
	require.Len(t, env.listWorkspaces(t, cookie), 1)

	// Get
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+ws.ID, nil)
	req.SetPathValue("id", ws.ID)
	req.Header.Set("Cookie", cookie)
	w := httptest.NewRecorder()
	env.wsHandlers.Get(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// Update name OK
	req2 := httptest.NewRequest(http.MethodPatch, "/api/workspaces/"+ws.ID, bytes.NewReader([]byte(`{"name":"renamed"}`)))
	req2.SetPathValue("id", ws.ID)
	req2.Header.Set("Cookie", cookie)
	w2 := httptest.NewRecorder()
	env.wsHandlers.Update(w2, req2)
	require.Equal(t, http.StatusOK, w2.Code)

	// Update work_dir → OK (mutable at workspace-level, must stay in sandbox)
	otherDir := wsSandboxDir(t, "u-admin", "other")
	req3 := httptest.NewRequest(http.MethodPatch, "/api/workspaces/"+ws.ID, bytes.NewReader([]byte(`{"work_dir":"`+otherDir+`"}`)))
	req3.SetPathValue("id", ws.ID)
	req3.Header.Set("Cookie", cookie)
	w3 := httptest.NewRecorder()
	env.wsHandlers.Update(w3, req3)
	require.Equal(t, http.StatusOK, w3.Code)
	var wsUpdated session.Workspace
	require.NoError(t, json.NewDecoder(w3.Body).Decode(&wsUpdated))
	require.Equal(t, otherDir, wsUpdated.WorkDir)

	// Delete (no active sessions) OK
	req4 := httptest.NewRequest(http.MethodDelete, "/api/workspaces/"+ws.ID, nil)
	req4.SetPathValue("id", ws.ID)
	req4.Header.Set("Cookie", cookie)
	w4 := httptest.NewRecorder()
	env.wsHandlers.Delete(w4, req4)
	require.Equal(t, http.StatusOK, w4.Code)
}

func TestWorkspace_Isolation_AcrossUsers(t *testing.T) {
	t.Parallel()
	env := newTestAuthEnv(t)
	cookieAdmin := env.loginAs(t, "admin", "adminpass", http.StatusOK)
	wsAdmin := env.createWorkspace(t, cookieAdmin, "u-admin", "admin-proj", "iso-admin")

	env.createUser(t, "alice", "alicepass1", "user")
	cookieAlice := env.loginAs(t, "alice", "alicepass1", http.StatusOK)
	wsAlice := env.createWorkspace(t, cookieAlice, "u-alice", "alice-proj", "iso-alice")

	// Alice lists → only her own, never admin's
	list := env.listWorkspaces(t, cookieAlice)
	require.Len(t, list, 1)
	require.Equal(t, wsAlice.ID, list[0].ID)
	require.NotEqual(t, wsAdmin.ID, list[0].ID)

	// Alice accesses admin's workspace → 403
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+wsAdmin.ID, nil)
	req.SetPathValue("id", wsAdmin.ID)
	req.Header.Set("Cookie", cookieAlice)
	w := httptest.NewRecorder()
	env.wsHandlers.Get(w, req)
	require.Equal(t, http.StatusForbidden, w.Code)
	require.Contains(t, w.Body.String(), "WORKSPACE_FORBIDDEN")
}

func TestWorkspace_UniqueWorkDirPerUser(t *testing.T) {
	t.Parallel()
	env := newTestAuthEnv(t)
	cookie := env.loginAs(t, "admin", "adminpass", http.StatusOK)
	env.createWorkspace(t, cookie, "u-admin", "first", "unique")

	// Same owner + same work_dir → 409 WORK_DIR_TAKEN (per-user 1:1, spec §6.2)
	dupDir := wsSandboxDir(t, "u-admin", "unique")
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces", bytes.NewReader([]byte(`{"name":"second","work_dir":"`+dupDir+`"}`)))
	req.Header.Set("Cookie", cookie)
	w := httptest.NewRecorder()
	env.wsHandlers.Create(w, req)
	require.Equal(t, http.StatusConflict, w.Code)
	require.Contains(t, w.Body.String(), "WORK_DIR_TAKEN")
}

func TestWorkspace_RequiresAuth(t *testing.T) {
	t.Parallel()
	env := newTestAuthEnv(t)
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces", nil) // no cookie
	w := httptest.NewRecorder()
	env.wsHandlers.List(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func (e *testAuthEnv) patchWorkspace(t *testing.T, cookie, id, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPatch, "/api/workspaces/"+id, bytes.NewReader([]byte(body)))
	req.SetPathValue("id", id)
	req.Header.Set("Cookie", cookie)
	w := httptest.NewRecorder()
	e.wsHandlers.Update(w, req)
	return w
}

func TestWorkspace_PatchAgentConfigOverrides_Validation(t *testing.T) {
	t.Parallel()
	env := newTestAuthEnv(t)
	cookie := env.loginAs(t, "admin", "adminpass", http.StatusOK)
	ws := env.createWorkspace(t, cookie, "u-admin", "proj", "patch")

	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantCode   string
	}{
		{
			name:       "valid overrides accepted",
			body:       `{"agent_config_overrides":"{\"SOUL.md\":\"x\",\"USER.md\":\"y\"}"}`,
			wantStatus: http.StatusOK,
		},
		{
			name:       "empty object clears overrides",
			body:       `{"agent_config_overrides":"{}"}`,
			wantStatus: http.StatusOK,
		},
		{
			name:       "invalid JSON rejected",
			body:       `{"agent_config_overrides":"{not json"}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "INVALID_CONFIG_JSON",
		},
		{
			name:       "unknown key rejected",
			body:       `{"agent_config_overrides":"{\"META-COGNITION.md\":\"x\"}"}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "UNKNOWN_CONFIG_FILE",
		},
		{
			name:       "non-string value rejected",
			body:       `{"agent_config_overrides":"{\"SOUL.md\":123}"}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "INVALID_CONFIG_VALUE",
		},
		{
			name:       "oversized file rejected",
			body:       `{"agent_config_overrides":"{\"SOUL.md\":\"` + strings.Repeat("a", 8001) + `\"}"}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "CONFIG_TOO_LARGE",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			w := env.patchWorkspace(t, cookie, ws.ID, tt.body)
			require.Equal(t, tt.wantStatus, w.Code, "body=%s", w.Body.String())
			if tt.wantCode != "" {
				require.Contains(t, w.Body.String(), tt.wantCode)
			}
		})
	}
}

func TestWorkspace_PatchAgentConfigOverrides_Persists(t *testing.T) {
	t.Parallel()
	env := newTestAuthEnv(t)
	cookie := env.loginAs(t, "admin", "adminpass", http.StatusOK)
	ws := env.createWorkspace(t, cookie, "u-admin", "proj", "persist")

	w := env.patchWorkspace(t, cookie, ws.ID, `{"agent_config_overrides":"{\"SOUL.md\":\"ws-soul\"}"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// Get round-trips the stored JSON (inner quotes are escaped by outer JSON encoding)
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+ws.ID, nil)
	req.SetPathValue("id", ws.ID)
	req.Header.Set("Cookie", cookie)
	gr := httptest.NewRecorder()
	env.wsHandlers.Get(gr, req)
	require.Equal(t, http.StatusOK, gr.Code)
	require.Contains(t, gr.Body.String(), `\"SOUL.md\":\"ws-soul\"`)
}

func TestWorkspace_PatchWorkerPreference_Whitelist(t *testing.T) {
	t.Parallel()
	env := newTestAuthEnv(t)
	cookie := env.loginAs(t, "admin", "adminpass", http.StatusOK)
	ws := env.createWorkspace(t, cookie, "u-admin", "proj", "wp")

	// The gateway test binary does not import the real worker adapters, so the
	// registry holds only testNoopType ("noop_gateway_test", registered in init).
	// Use it as the stand-in for "a valid registered type" — the 4 real
	// constants are validated at the worker-package boundary (TestValidateType).
	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantCode   string
	}{
		{"valid registered type", `{"worker_preference":"` + string(testNoopType) + `"}`, http.StatusOK, ""},
		{"empty keeps default", `{"worker_preference":""}`, http.StatusOK, ""},
		{"unknown rejected", `{"worker_preference":"bogus"}`, http.StatusBadRequest, "INVALID_WORKER_TYPE"},
		{"TypeUnknown rejected", `{"worker_preference":"unknown"}`, http.StatusBadRequest, "INVALID_WORKER_TYPE"},
		{"case sensitive rejected", `{"worker_preference":"Claude_Code"}`, http.StatusBadRequest, "INVALID_WORKER_TYPE"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			w := env.patchWorkspace(t, cookie, ws.ID, tt.body)
			require.Equal(t, tt.wantStatus, w.Code, "body=%s", w.Body.String())
			if tt.wantCode != "" {
				require.Contains(t, w.Body.String(), tt.wantCode)
			}
		})
	}
}

func TestWorkspace_PatchWorkerPreference_Persists(t *testing.T) {
	t.Parallel()
	env := newTestAuthEnv(t)
	cookie := env.loginAs(t, "admin", "adminpass", http.StatusOK)
	ws := env.createWorkspace(t, cookie, "u-admin", "proj", "wp-persist")

	// Set a valid preference (testNoopType is the registered test worker).
	w := env.patchWorkspace(t, cookie, ws.ID, `{"worker_preference":"`+string(testNoopType)+`"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// Re-fetch and confirm persisted (not just echoed in the PATCH response).
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+ws.ID, nil)
	req.SetPathValue("id", ws.ID)
	req.Header.Set("Cookie", cookie)
	gr := httptest.NewRecorder()
	env.wsHandlers.Get(gr, req)
	require.Equal(t, http.StatusOK, gr.Code)
	var got session.Workspace
	require.NoError(t, json.NewDecoder(gr.Body).Decode(&got))
	require.Equal(t, string(testNoopType), got.WorkerPreference)
}

func TestWorkspace_PatchPermissionMode_Whitelist(t *testing.T) {
	t.Parallel()
	env := newTestAuthEnv(t)
	cookie := env.loginAs(t, "admin", "adminpass", http.StatusOK)
	ws := env.createWorkspace(t, cookie, "u-admin", "proj", "pm")

	// ValidatePermissionMode accepts the 4 current tiers + "" ("worker default").
	// Legacy values like "plan" (old 3-tier system) are rejected — empty is always
	// valid (means "no explicit override"; bridge leaves it "" for the worker default).
	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantCode   string
	}{
		{"read-only", `{"permission_mode":"read-only"}`, http.StatusOK, ""},
		{"workspace", `{"permission_mode":"workspace"}`, http.StatusOK, ""},
		{"auto-edit", `{"permission_mode":"auto-edit"}`, http.StatusOK, ""},
		{"bypass", `{"permission_mode":"bypass"}`, http.StatusOK, ""},
		{"empty means worker default", `{"permission_mode":""}`, http.StatusOK, ""},
		{"unknown rejected", `{"permission_mode":"bogus"}`, http.StatusBadRequest, "INVALID_PERMISSION_MODE"},
		{"legacy plan rejected", `{"permission_mode":"plan"}`, http.StatusBadRequest, "INVALID_PERMISSION_MODE"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			w := env.patchWorkspace(t, cookie, ws.ID, tt.body)
			require.Equal(t, tt.wantStatus, w.Code, "body=%s", w.Body.String())
			if tt.wantCode != "" {
				require.Contains(t, w.Body.String(), tt.wantCode)
			}
		})
	}
}

func TestWorkspace_PatchPermissionMode_Persists(t *testing.T) {
	t.Parallel()
	env := newTestAuthEnv(t)
	cookie := env.loginAs(t, "admin", "adminpass", http.StatusOK)
	ws := env.createWorkspace(t, cookie, "u-admin", "proj", "pm-persist")

	w := env.patchWorkspace(t, cookie, ws.ID, `{"permission_mode":"read-only"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// Re-fetch and confirm persisted (snake_case wire key, not just echoed).
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+ws.ID, nil)
	req.SetPathValue("id", ws.ID)
	req.Header.Set("Cookie", cookie)
	gr := httptest.NewRecorder()
	env.wsHandlers.Get(gr, req)
	require.Equal(t, http.StatusOK, gr.Code)
	var got session.Workspace
	require.NoError(t, json.NewDecoder(gr.Body).Decode(&got))
	require.Equal(t, "read-only", got.PermissionMode)
}

// TestWorkspace_JSONWireContract guards the snake_case wire contract consumed by
// webchat (webchat/lib/api/workspaces.ts). Decode into a raw map — NOT
// session.Workspace — so the assertion sees the actual on-wire keys. A struct
// round-trip masks tag bugs because both encode and decode share the same struct.
func TestWorkspace_JSONWireContract(t *testing.T) {
	t.Parallel()
	env := newTestAuthEnv(t)
	cookie := env.loginAs(t, "admin", "adminpass", http.StatusOK)
	env.createWorkspace(t, cookie, "u-admin", "proj", "wire")

	req := httptest.NewRequest(http.MethodGet, "/api/workspaces", nil)
	req.Header.Set("Cookie", cookie)
	w := httptest.NewRecorder()
	env.wsHandlers.List(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Workspaces []json.RawMessage `json:"workspaces"`
		Limit      int               `json:"limit"`
		Offset     int               `json:"offset"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Len(t, resp.Workspaces, 1)
	require.Equal(t, 100, resp.Limit, "ListWorkspacesResponse.limit must be echoed (default 100)")
	require.Equal(t, 0, resp.Offset, "ListWorkspacesResponse.offset must be echoed")

	var m map[string]any
	require.NoError(t, json.Unmarshal(resp.Workspaces[0], &m))
	require.Equal(t, "proj", m["name"], "snake_case wire contract; actual keys: %#v", m)
	require.Contains(t, m, "id")
	require.Contains(t, m, "work_dir")
	require.Contains(t, m, "owner_user_id")
	require.Contains(t, m, "created_at", "workspace wire contract must carry created_at")
	require.Contains(t, m, "updated_at", "workspace wire contract must carry updated_at")
	require.NotContains(t, m, "Name", "PascalCase field leaked onto the wire")
}

// ---------------------------------------------------------------------------
// spec ⑦ Phase 1: api-key channel workspace access (cross-channel tenant)
// ---------------------------------------------------------------------------

const testAPIKeyHeader = "X-API-Key"

func (e *testAuthEnv) createWorkspaceWithAPIKey(t *testing.T, key, ownerUID, name, sub string) *session.Workspace {
	t.Helper()
	workDir := wsSandboxDir(t, ownerUID, sub)
	body := []byte(`{"name":"` + name + `","work_dir":"` + workDir + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces", bytes.NewReader(body))
	req.Header.Set(testAPIKeyHeader, key)
	w := httptest.NewRecorder()
	e.wsHandlers.Create(w, req)
	require.Equal(t, http.StatusOK, w.Code, "create ws (api-key) body=%s", w.Body.String())
	var ws session.Workspace
	require.NoError(t, json.NewDecoder(w.Body).Decode(&ws))
	return &ws
}

func (e *testAuthEnv) listWorkspacesWithAPIKey(t *testing.T, key string) []*session.Workspace {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces", nil)
	req.Header.Set(testAPIKeyHeader, key)
	w := httptest.NewRecorder()
	e.wsHandlers.List(w, req)
	require.Equal(t, http.StatusOK, w.Code, "list ws (api-key) body=%s", w.Body.String())
	var resp struct {
		Workspaces []*session.Workspace `json:"workspaces"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	return resp.Workspaces
}

// TestWorkspace_APIKey_CRUD: api-key 服务账号可通过 X-API-Key header 完成 workspace
// 全 CRUD，owner_user_id 落到 api-key 对应的 users.id（migration 018 模型）。
func TestWorkspace_APIKey_CRUD(t *testing.T) {
	t.Parallel()
	env := newTestAuthEnv(t)
	env.addAPIKeyUser(t, "svc1-key", "u-svc1", "apikey:svc1")

	// Create — owner must be the api-key user, not "api_user" or "anonymous".
	ws := env.createWorkspaceWithAPIKey(t, "svc1-key", "u-svc1", "svc-proj", "apikey-crud")
	require.NotEmpty(t, ws.ID)
	require.Equal(t, "u-svc1", ws.OwnerUserID, "workspace owner must resolve to the api-key user's users.id")

	// List → only this api-key user's workspaces.
	require.Len(t, env.listWorkspacesWithAPIKey(t, "svc1-key"), 1)

	// Get via api-key.
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+ws.ID, nil)
	req.SetPathValue("id", ws.ID)
	req.Header.Set(testAPIKeyHeader, "svc1-key")
	w := httptest.NewRecorder()
	env.wsHandlers.Get(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// Update name via api-key.
	req2 := httptest.NewRequest(http.MethodPatch, "/api/workspaces/"+ws.ID, bytes.NewReader([]byte(`{"name":"svc-renamed"}`)))
	req2.SetPathValue("id", ws.ID)
	req2.Header.Set(testAPIKeyHeader, "svc1-key")
	w2 := httptest.NewRecorder()
	env.wsHandlers.Update(w2, req2)
	require.Equal(t, http.StatusOK, w2.Code)

	// Delete via api-key.
	req3 := httptest.NewRequest(http.MethodDelete, "/api/workspaces/"+ws.ID, nil)
	req3.SetPathValue("id", ws.ID)
	req3.Header.Set(testAPIKeyHeader, "svc1-key")
	w3 := httptest.NewRecorder()
	env.wsHandlers.Delete(w3, req3)
	require.Equal(t, http.StatusOK, w3.Code)
}

// TestWorkspace_APIKey_Isolation: two api-key service accounts cannot see each
// other's workspaces — the owner check (conn.go:350 / workspace_handlers.go:135)
// enforces per-tenant isolation across the api-key channel just as it does for
// cookie users (TestWorkspace_Isolation_AcrossUsers).
func TestWorkspace_APIKey_Isolation(t *testing.T) {
	t.Parallel()
	env := newTestAuthEnv(t)
	env.addAPIKeyUser(t, "svc1-key", "u-svc1", "apikey:svc1")
	env.addAPIKeyUser(t, "svc2-key", "u-svc2", "apikey:svc2")

	ws1 := env.createWorkspaceWithAPIKey(t, "svc1-key", "u-svc1", "svc1-proj", "apikey-iso1")

	// svc2 lists → sees nothing of svc1's.
	list := env.listWorkspacesWithAPIKey(t, "svc2-key")
	require.Len(t, list, 0)

	// svc2 tries to read svc1's workspace → 403.
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+ws1.ID, nil)
	req.SetPathValue("id", ws1.ID)
	req.Header.Set(testAPIKeyHeader, "svc2-key")
	w := httptest.NewRecorder()
	env.wsHandlers.Get(w, req)
	require.Equal(t, http.StatusForbidden, w.Code)
	require.Contains(t, w.Body.String(), "WORKSPACE_FORBIDDEN")
}

// TestWorkspace_APIKey_Priority_Over_Cookie: when a request carries BOTH an
// api-key header and a cookie, the api-key identity wins. This is the spec ⑦
// contract — the machine channel is authoritative for programmatic integrations
// and must not silently inherit an operator's browser cookie. Without this
// guarantee a third-party integration run from an admin's browser would create
// workspaces owned by the admin instead of the service account.
func TestWorkspace_APIKey_Priority_Over_Cookie(t *testing.T) {
	t.Parallel()
	env := newTestAuthEnv(t)
	env.addAPIKeyUser(t, "svc1-key", "u-svc1", "apikey:svc1")
	cookieAdmin := env.loginAs(t, "admin", "adminpass", http.StatusOK)

	body := []byte(`{"name":"dual-channel","work_dir":"` + wsSandboxDir(t, "u-svc1", "apikey-prio") + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces", bytes.NewReader(body))
	req.Header.Set(testAPIKeyHeader, "svc1-key")
	req.Header.Set("Cookie", cookieAdmin)
	w := httptest.NewRecorder()
	env.wsHandlers.Create(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var ws session.Workspace
	require.NoError(t, json.NewDecoder(w.Body).Decode(&ws))
	require.Equal(t, "u-svc1", ws.OwnerUserID, "api-key identity must win over cookie when both are present")
	require.NotEqual(t, "u-admin", ws.OwnerUserID)
}

// TestWorkspace_MixedCredentials_NoBypassForNonAdminAPIKey: spec ⑦ P2.7 —
// identity and admin authority are now same-source (both derive from the
// AuthenticateRequest uid). A request carrying BOTH an api-key (non-owner
// service account, role=user) AND a valid admin cookie resolves uid to the
// api-key user, so the admin bypass does NOT apply — the cookie no longer
// grants authority on its own. This flips the pre-P2.7 behavior (when isAdmin
// was cookie-only) documented in the PR #773 review P2-1 finding.
func TestWorkspace_MixedCredentials_NoBypassForNonAdminAPIKey(t *testing.T) {
	t.Parallel()
	env := newTestAuthEnv(t)
	env.addAPIKeyUser(t, "svc1-key", "u-svc1", "apikey:svc1")
	cookieAdmin := env.loginAs(t, "admin", "adminpass", http.StatusOK)
	// admin owns this workspace; svc1 (api-key user) does not.
	wsAdmin := env.createWorkspace(t, cookieAdmin, "u-admin", "admin-proj", "mixed-admin")

	// Request carries svc1's api-key (non-owner uid) + admin's cookie.
	// uid resolves to u-svc1 (role=user) → isAdmin=false → 403, even with the
	// admin cookie present (authority is now same-source, not cookie-bound).
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+wsAdmin.ID, nil)
	req.SetPathValue("id", wsAdmin.ID)
	req.Header.Set(testAPIKeyHeader, "svc1-key")
	req.Header.Set("Cookie", cookieAdmin)
	w := httptest.NewRecorder()
	env.wsHandlers.Get(w, req)
	require.Equal(t, http.StatusForbidden, w.Code, "same-source authority: admin cookie must not grant bypass when uid resolves to a non-admin api-key identity")
	require.Contains(t, w.Body.String(), "WORKSPACE_FORBIDDEN")
}

// TestWorkspace_AdminCookie_CanReadOthers: spec ⑦ P2.7 — a cookie-authenticated
// admin (uid resolves to a role=admin user) still earns the admin bypass under
// same-source authority, so it can read any workspace regardless of owner.
// Guards against the P2.7 change over-tightening and locking out genuine admins.
func TestWorkspace_AdminCookie_CanReadOthers(t *testing.T) {
	t.Parallel()
	env := newTestAuthEnv(t)
	env.createUser(t, "alice", "alicepass1", "user")
	cookieAlice := env.loginAs(t, "alice", "alicepass1", http.StatusOK)
	wsAlice := env.createWorkspace(t, cookieAlice, "u-alice", "alice-proj", "admin-read")

	cookieAdmin := env.loginAs(t, "admin", "adminpass", http.StatusOK)
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+wsAlice.ID, nil)
	req.SetPathValue("id", wsAlice.ID)
	req.Header.Set("Cookie", cookieAdmin)
	w := httptest.NewRecorder()
	env.wsHandlers.Get(w, req)
	require.Equal(t, http.StatusOK, w.Code, "admin uid (role=admin) must retain the bypass under same-source authority")
}

// TestWorkspace_APIKey_Invalid_Rejected: an unregistered api-key is rejected
// with 401, matching the cookie-path enforcement (TestWorkspace_RequiresAuth).
func TestWorkspace_APIKey_Invalid_Rejected(t *testing.T) {
	t.Parallel()
	env := newTestAuthEnv(t)
	env.addAPIKeyUser(t, "svc1-key", "u-svc1", "apikey:svc1") // lock dev mode via AddKey

	req := httptest.NewRequest(http.MethodGet, "/api/workspaces", nil)
	req.Header.Set(testAPIKeyHeader, "bogus-key")
	w := httptest.NewRecorder()
	env.wsHandlers.List(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

// ---------------------------------------------------------------------------
// work_dir sandbox 前缀约束（security.ValidateWorkspaceWorkDir）
// ---------------------------------------------------------------------------

// TestWorkspace_Create_OutsideSandbox_Rejected: 新约束 —— workspace work_dir 必须落在
// owner 沙箱前缀 $HOME/.hotplex/workspaces/<uid> 下。
func TestWorkspace_Create_OutsideSandbox_Rejected(t *testing.T) {
	t.Parallel()
	env := newTestAuthEnv(t)
	cookie := env.loginAs(t, "admin", "adminpass", http.StatusOK)

	body := []byte(`{"name":"escape","work_dir":"/tmp/hotplex-escape"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces", bytes.NewReader(body))
	req.Header.Set("Cookie", cookie)
	w := httptest.NewRecorder()
	env.wsHandlers.Create(w, req)
	require.Equal(t, http.StatusForbidden, w.Code)
	require.Contains(t, w.Body.String(), "WORK_DIR_OUTSIDE_SANDBOX")
}

// TestWorkspace_Update_WorkDir_OutsideSandbox_Rejected: PATCH 改 work_dir 也必须留在沙箱内。
func TestWorkspace_Update_WorkDir_OutsideSandbox_Rejected(t *testing.T) {
	t.Parallel()
	env := newTestAuthEnv(t)
	cookie := env.loginAs(t, "admin", "adminpass", http.StatusOK)
	ws := env.createWorkspace(t, cookie, "u-admin", "proj", "upd-sandbox")

	// 改成沙箱外 → 403
	w := env.patchWorkspace(t, cookie, ws.ID, `{"work_dir":"/tmp/hotplex-escape"}`)
	require.Equal(t, http.StatusForbidden, w.Code)
	require.Contains(t, w.Body.String(), "WORK_DIR_OUTSIDE_SANDBOX")

	// 改成沙箱内另一个子目录 → 200
	inside := wsSandboxDir(t, "u-admin", "upd-moved")
	w2 := env.patchWorkspace(t, cookie, ws.ID, `{"work_dir":"`+inside+`"}`)
	require.Equal(t, http.StatusOK, w2.Code, w2.Body.String())
}

// TestWorkspace_OwnerIsolation_Sandbox: 即使 work_dir 落在 workspaces 树下，只要不在
// 调用者自己 uid 的子树下（例如别人的 uid），也必须被拒。
func TestWorkspace_OwnerIsolation_Sandbox(t *testing.T) {
	t.Parallel()
	env := newTestAuthEnv(t)
	cookie := env.loginAs(t, "admin", "adminpass", http.StatusOK)

	otherDir := wsSandboxDir(t, "u-someone-else", "stolen")
	body := []byte(`{"name":"cross","work_dir":"` + otherDir + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces", bytes.NewReader(body))
	req.Header.Set("Cookie", cookie)
	w := httptest.NewRecorder()
	env.wsHandlers.Create(w, req)
	require.Equal(t, http.StatusForbidden, w.Code)
	require.Contains(t, w.Body.String(), "WORK_DIR_OUTSIDE_SANDBOX")
}

// TestWorkspace_Grandfather_LegacyWorkDir: 沙箱约束上线前的老 workspace work_dir 可能
// 不在新前缀下。PATCH 非 work_dir 字段（如 name）不得触发前缀校验 —— 老 workspace 继续
// 可用（grandfather）；仅 PATCH work_dir 时才强制前缀。
func TestWorkspace_Grandfather_LegacyWorkDir(t *testing.T) {
	t.Parallel()
	env := newTestAuthEnv(t)
	cookie := env.loginAs(t, "admin", "adminpass", http.StatusOK)

	// 直接经 store 写入 legacy work_dir（绕过 handler 校验，模拟上线前老数据）。
	legacy := &session.Workspace{
		ID: "ws-legacy", OwnerUserID: "u-admin", Name: "legacy", WorkDir: "/tmp/hotplex-legacy-pre-sandbox", Status: "active",
	}
	require.NoError(t, env.store.CreateWorkspace(context.Background(), legacy, env.wsHandlers.nowUnix()))

	// PATCH name → 200，不得触发 work_dir 前缀校验。
	w := env.patchWorkspace(t, cookie, "ws-legacy", `{"name":"legacy-renamed"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

// TestWorkspace_UpdateWorkDir_ActiveSessionBlocked: work_dir 参与 DeriveSessionKey
// (key.go)，改它会 shift 确定性 session id 并孤立活跃会话的历史。Update 必须在有活跃
// 会话时拒绝改动（对齐 Delete 的 DeleteWorkspaceIfEmpty 守卫，spec §9.1）。
func TestWorkspace_UpdateWorkDir_ActiveSessionBlocked(t *testing.T) {
	t.Parallel()
	env := newTestAuthEnv(t)
	cookie := env.loginAs(t, "admin", "adminpass", http.StatusOK)
	ws := env.createWorkspace(t, cookie, "u-admin", "guard", "initial")

	// 直接写一条活跃 session 绑定该 workspace（绕过 WS 握手）。
	sqlite := env.store.(*session.SQLiteStore)
	require.NoError(t, sqlite.Upsert(context.Background(), &session.SessionInfo{
		ID:          "sess-active",
		UserID:      "u-admin",
		WorkspaceID: ws.ID,
		State:       events.StateRunning,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}))

	// PATCH 改 work_dir 到沙箱内另一目录 → 409 WORKSPACE_NOT_EMPTY。
	moved := wsSandboxDir(t, "u-admin", "moved")
	w := env.patchWorkspace(t, cookie, ws.ID, `{"work_dir":"`+moved+`"}`)
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "WORKSPACE_NOT_EMPTY")

	// 同值 PATCH（abs == ws.WorkDir）不触发守卫 → 200。
	w2 := env.patchWorkspace(t, cookie, ws.ID, `{"work_dir":"`+ws.WorkDir+`"}`)
	require.Equal(t, http.StatusOK, w2.Code, w2.Body.String())
}

// TestWorkspace_Update_OwnerIsolation_AdminEdit: admin 代改别人的 workspace 时，
// work_dir 沙箱必须按 workspace OWNER 校验（spec §2 G2），而非操作者 admin 自己。
// Create 路径无此问题（创建者即 owner）；Update 的 admin-edit 才会两者分离。
func TestWorkspace_Update_OwnerIsolation_AdminEdit(t *testing.T) {
	t.Parallel()
	env := newTestAuthEnv(t)
	adminCookie := env.loginAs(t, "admin", "adminpass", http.StatusOK)
	env.createUser(t, "bob", "bobpass", "user")

	// 直接经 store 写入 bob 的 workspace（owner=u-bob，绕过 handler 创建）。
	bob := &session.Workspace{
		ID: "ws-bob", OwnerUserID: "u-bob", Name: "bob-proj",
		WorkDir: wsSandboxDir(t, "u-bob", "orig"), Status: "active",
	}
	require.NoError(t, env.store.CreateWorkspace(context.Background(), bob, env.wsHandlers.nowUnix()))

	// admin 代改：work_dir 落 admin 自己沙箱 → 拒（owner 是 bob，必须落 bob 沙箱）。
	adminDir := wsSandboxDir(t, "u-admin", "hijack")
	w := env.patchWorkspace(t, adminCookie, "ws-bob", `{"work_dir":"`+adminDir+`"}`)
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "WORK_DIR_OUTSIDE_SANDBOX")

	// admin 代改：work_dir 落 bob 沙箱 → 200（owner 隔离保持，admin 可代办）。
	bobDir := wsSandboxDir(t, "u-bob", "moved")
	w2 := env.patchWorkspace(t, adminCookie, "ws-bob", `{"work_dir":"`+bobDir+`"}`)
	require.Equal(t, http.StatusOK, w2.Code, w2.Body.String())
}
