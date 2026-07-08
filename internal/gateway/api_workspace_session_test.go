package gateway

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/admin"
	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/dbutil"
	"github.com/hrygo/hotplex/internal/security"
	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/internal/worker"
)

type staticConfigProvider struct{ cfg *config.Config }

func (p staticConfigProvider) Get() *config.Config { return p.cfg }

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

func createAPIKeyViaAdmin(t *testing.T, env *testAuthEnv, rawUserID string) (string, string) {
	t.Helper()
	store := env.store.(*session.SQLiteStore)
	dbResolver := security.NewDBResolver(store.DB(), dbutil.DialectSQLite)
	t.Cleanup(dbResolver.Close)
	env.auth.SetKeyResolver(dbResolver)

	cfg := config.Default()
	cfg.Admin.TokenScopes = map[string][]string{
		"admin-token": {admin.ScopeAdminWrite, admin.ScopeAdminRead},
	}
	adminAPI := admin.New(admin.Deps{
		Log:            slog.Default(),
		Config:         staticConfigProvider{cfg: cfg},
		WorkspaceStore: env.store,
		DB:             store.DB(),
		DBResolver:     dbResolver,
		KeyValidator:   env.auth,
	})

	req := httptest.NewRequest(http.MethodPost, "/admin/api-keys", strings.NewReader(`{"user_id":"`+rawUserID+`"}`))
	req.Header.Set("Authorization", "Bearer admin-token")
	w := httptest.NewRecorder()
	adminAPI.Middleware(http.HandlerFunc(adminAPI.HandleAPIKeyUserCreate)).ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	var created admin.APIKeyUser
	require.NoError(t, json.NewDecoder(w.Body).Decode(&created))

	var storedUserID string
	err := store.DB().QueryRowContext(req.Context(),
		"SELECT user_id FROM api_key_users WHERE api_key = ?", created.APIKey,
	).Scan(&storedUserID)
	require.NoError(t, err)
	return created.APIKey, storedUserID
}

// TestCreateSession_RealStore_OwnerOK: the workspace owner can create a session
// bound to their own workspace (real store ownership check, spec ① §9.2).
func TestCreateSession_RealStore_OwnerOK(t *testing.T) {
	t.Parallel()
	env, api, sm, bridge := newWorkspaceSessionEnv(t)
	cookie := env.loginAs(t, "admin", "adminpass", http.StatusOK)
	ws := env.createWorkspace(t, cookie, "u-admin", "proj", "wsess-owner")

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
	ws := env.createWorkspace(t, cookieAdmin, "u-admin", "admin-proj", "wsess-admin")

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
	ws := env.createWorkspace(t, cookie, "u-admin", "proj", "wsess-key3")

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

func TestListSessions_APIKeyCreatedViaAdminAlias(t *testing.T) {
	t.Parallel()
	env, api, sm, _ := newWorkspaceSessionEnv(t)
	apiKey, storedUserID := createAPIKeyViaAdmin(t, env, "alice")

	sm.On("List", mock.Anything, storedUserID, "", "", 100, 0).
		Return([]*session.SessionInfo{}, nil).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/sessions?platform=all&api_key="+url.QueryEscape(apiKey), nil)
	w := httptest.NewRecorder()
	api.ListSessions(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	sm.AssertExpectations(t)
}

func TestCreateSession_APIKeyCreatedViaAdminAlias(t *testing.T) {
	t.Parallel()
	env, api, sm, bridge := newWorkspaceSessionEnv(t)
	apiKey, storedUserID := createAPIKeyViaAdmin(t, env, "alice")

	sm.On("Get", mock.Anything).Return(nil, session.ErrSessionNotFound).Once()
	bridge.On("StartSession", mock.Anything, mock.MatchedBy(func(p worker.SessionStartParams) bool {
		return p.UserID == storedUserID &&
			p.ClientKey == "c1" &&
			p.WorkerType == worker.TypeClaudeCode &&
			p.Platform == platformWebChat
	})).Return(nil).Once()

	req := httptest.NewRequest(http.MethodPost, "/api/sessions?client_session_id=c1&api_key="+url.QueryEscape(apiKey), nil)
	w := httptest.NewRecorder()
	api.CreateSession(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	bridge.AssertExpectations(t)
	sm.AssertExpectations(t)
}
