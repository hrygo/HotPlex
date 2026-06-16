package gateway

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
