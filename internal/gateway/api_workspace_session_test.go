package gateway

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/internal/worker"
)

// newWorkspaceSessionEnv reuses the real-store testAuthEnv (auth + cookie + idp +
// SQLite workspace store) but wires GatewayAPI with mock sm/bridge, so CreateSession
// runs its real workspace ownership check while avoiding actual worker launch.
func newWorkspaceSessionEnv(t *testing.T) (*testAuthEnv, *GatewayAPI, *mockAPISM, *mockAPIBridge) {
	t.Helper()
	env := newTestAuthEnv(t)
	sm := new(mockAPISM)
	bridge := new(mockAPIBridge)
	api := NewGatewayAPI(slog.Default(), env.auth, sm, bridge, config.NewConfigStore(&config.Config{}, nil), nil, nil, env.store)
	return env, api, sm, bridge
}

// TestCreateSession_RealStore_OwnerOK: the workspace owner can create a session
// bound to their own workspace (real store ownership check, spec ① §9.2).
func TestCreateSession_RealStore_OwnerOK(t *testing.T) {
	t.Parallel()
	env, api, sm, bridge := newWorkspaceSessionEnv(t)
	cookie := env.loginAs(t, "admin", "adminpass", http.StatusOK)
	ws := env.createWorkspace(t, cookie, "proj", "/tmp/hotplex/wsess-owner")

	sm.On("Get", mock.Anything).Return(nil, session.ErrSessionNotFound)
	bridge.On("StartSession", mock.Anything, mock.Anything).Return(nil)

	body := strings.NewReader(`{"workspace_id":"` + ws.ID + `","client_session_id":"c1"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions", body)
	req.Header.Set("Cookie", cookie)
	w := httptest.NewRecorder()
	api.CreateSession(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	bridge.AssertExpectations(t)
}

// TestCreateSession_RealStore_CrossUserForbidden: a user cannot create a session
// in another user's workspace (real store ownership check → 403).
func TestCreateSession_RealStore_CrossUserForbidden(t *testing.T) {
	t.Parallel()
	env, api, _, _ := newWorkspaceSessionEnv(t)
	cookieAdmin := env.loginAs(t, "admin", "adminpass", http.StatusOK)
	ws := env.createWorkspace(t, cookieAdmin, "admin-proj", "/tmp/hotplex/wsess-admin")

	env.createUser(t, "bob", "bobpass1234", "user")
	cookieBob := env.loginAs(t, "bob", "bobpass1234", http.StatusOK)

	body := strings.NewReader(`{"workspace_id":"` + ws.ID + `","client_session_id":"c1"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions", body)
	req.Header.Set("Cookie", cookieBob)
	w := httptest.NewRecorder()
	api.CreateSession(w, req)

	require.Equal(t, http.StatusForbidden, w.Code)
	require.Contains(t, w.Body.String(), "WORKSPACE_FORBIDDEN")
}

// TestCreateSession_RealStore_KeyMethod3: the returned session_id equals the UUIDv5
// derived from (userID, workerType, clientKey, workspaceID, workDir) — 方案3 (spec §7).
func TestCreateSession_RealStore_KeyMethod3(t *testing.T) {
	t.Parallel()
	env, api, sm, bridge := newWorkspaceSessionEnv(t)
	cookie := env.loginAs(t, "admin", "adminpass", http.StatusOK)
	ws := env.createWorkspace(t, cookie, "proj", "/tmp/hotplex/wsess-key3")

	sm.On("Get", mock.Anything).Return(nil, session.ErrSessionNotFound)
	bridge.On("StartSession", mock.Anything, mock.Anything).Return(nil)

	body := strings.NewReader(`{"workspace_id":"` + ws.ID + `","client_session_id":"c1"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions", body)
	req.Header.Set("Cookie", cookie)
	w := httptest.NewRecorder()
	api.CreateSession(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))

	// 方案3: workspace_id participates in the hash. Same inputs → same session id.
	expected := session.DeriveSessionKey("u-admin", worker.TypeClaudeCode, "c1", ws.ID, ws.WorkDir)
	require.Equal(t, expected, resp["session_id"], "session_id must be 方案3 derivation")
}
