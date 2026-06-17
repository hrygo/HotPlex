package gateway

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/session"
)

func (e *testAuthEnv) createWorkspace(t *testing.T, cookie, name, workDir string) *session.Workspace {
	t.Helper()
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
	ws := env.createWorkspace(t, cookie, "proj", "/tmp/hotplex-ws-crud")
	require.NotEmpty(t, ws.ID)
	require.Equal(t, "/tmp/hotplex-ws-crud", ws.WorkDir)

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

	// Update work_dir → rejected (immutable, spec §6.2)
	req3 := httptest.NewRequest(http.MethodPatch, "/api/workspaces/"+ws.ID, bytes.NewReader([]byte(`{"work_dir":"/tmp/other"}`)))
	req3.SetPathValue("id", ws.ID)
	req3.Header.Set("Cookie", cookie)
	w3 := httptest.NewRecorder()
	env.wsHandlers.Update(w3, req3)
	require.Equal(t, http.StatusBadRequest, w3.Code)
	require.Contains(t, w3.Body.String(), "WORK_DIR_IMMUTABLE")

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
	wsAdmin := env.createWorkspace(t, cookieAdmin, "admin-proj", "/tmp/hotplex-ws-admin")

	env.createUser(t, "alice", "alicepass1", "user")
	cookieAlice := env.loginAs(t, "alice", "alicepass1", http.StatusOK)
	wsAlice := env.createWorkspace(t, cookieAlice, "alice-proj", "/tmp/hotplex-ws-alice")

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
	env.createWorkspace(t, cookie, "first", "/tmp/hotplex-ws-unique")

	// Same owner + same work_dir → 409 WORK_DIR_TAKEN (per-user 1:1, spec §6.2)
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces", bytes.NewReader([]byte(`{"name":"second","work_dir":"/tmp/hotplex-ws-unique"}`)))
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
	ws := env.createWorkspace(t, cookie, "proj", "/tmp/hotplex-ws-patch")

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
	ws := env.createWorkspace(t, cookie, "proj", "/tmp/hotplex-ws-persist")

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
	ws := env.createWorkspace(t, cookie, "proj", "/tmp/hotplex-ws-wp")

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
	ws := env.createWorkspace(t, cookie, "proj", "/tmp/hotplex-ws-wp-persist")

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
