package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/security"
	"github.com/hrygo/hotplex/internal/session"
)

// newTestSessionStore builds an isolated SQLite store running all migrations.
func newTestSessionStore(t *testing.T) session.UserWorkspaceStore {
	t.Helper()
	cfg := config.Default()
	cfg.DB.Path = filepath.Join(t.TempDir(), "test.db")
	cfg.DB.SQLite.Path = cfg.DB.Path
	cfg.DB.WALMode = true
	store, err := session.NewSQLiteStore(context.Background(), cfg, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// testBcryptCostGateway is low to keep auth handler tests fast (production uses 12).
const testBcryptCostGateway = 4

type testAuthEnv struct {
	auth     *security.Authenticator
	cookie   *security.CookieAuth
	store    session.UserWorkspaceStore
	idp      *security.LocalAccountProvider
	handlers *AuthHandlers
}

func newTestAuthEnv(t *testing.T) *testAuthEnv {
	t.Helper()
	store := newTestSessionStore(t)
	ca, err := security.NewCookieAuth()
	require.NoError(t, err)
	idp := security.NewLocalAccountProvider(store, testBcryptCostGateway)
	hash, err := idp.HashPassword("adminpass")
	require.NoError(t, err)
	require.NoError(t, store.CreateUser(context.Background(), &security.User{
		ID: "u-admin", Username: "admin", PasswordHash: hash, Role: "admin", Status: "active",
	}, 1700000000))
	auth := security.NewAuthenticator(&config.SecurityConfig{})
	auth.SetCookieAuth(ca)
	auth.SetIdentityProvider(idp)
	h := NewAuthHandlers(auth, ca, store, idp)
	return &testAuthEnv{auth: auth, cookie: ca, store: store, idp: idp, handlers: h}
}

func (e *testAuthEnv) loginAs(t *testing.T, user, pass string, wantStatus int) string {
	t.Helper()
	body := []byte(`{"username":"` + user + `","password":"` + pass + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	w := httptest.NewRecorder()
	e.handlers.Login(w, req)
	require.Equal(t, wantStatus, w.Code, "login body=%s", w.Body.String())
	return w.Result().Header.Get("Set-Cookie")
}

func (e *testAuthEnv) createUser(t *testing.T, name, pass, role string) {
	t.Helper()
	hash, err := e.idp.HashPassword(pass)
	require.NoError(t, err)
	require.NoError(t, e.store.CreateUser(context.Background(), &security.User{
		ID: "u-" + name, Username: name, PasswordHash: hash, Role: role, Status: "active",
	}, 1700000000))
}

func TestLoginHandler_SuccessIssuesCookie(t *testing.T) {
	t.Parallel()
	env := newTestAuthEnv(t)
	cookie := env.loginAs(t, "admin", "adminpass", http.StatusOK)
	require.Contains(t, cookie, "webchat_session=")
}

func TestLoginHandler_WrongPassword_401(t *testing.T) {
	t.Parallel()
	env := newTestAuthEnv(t)
	env.loginAs(t, "admin", "wrong", http.StatusUnauthorized)
}

func TestMeHandler_RequiresAuth(t *testing.T) {
	t.Parallel()
	env := newTestAuthEnv(t)
	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	w := httptest.NewRecorder()
	env.handlers.Me(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestMeHandler_ReturnsProfile(t *testing.T) {
	t.Parallel()
	env := newTestAuthEnv(t)
	cookie := env.loginAs(t, "admin", "adminpass", http.StatusOK)
	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	req.Header.Set("Cookie", cookie)
	w := httptest.NewRecorder()
	env.handlers.Me(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"username":"admin"`)
	require.Contains(t, w.Body.String(), `"role":"admin"`)
}

func TestAcceptInvite_CreatesUserAndIssuesCookie(t *testing.T) {
	t.Parallel()
	env := newTestAuthEnv(t)
	cookie := env.loginAs(t, "admin", "adminpass", http.StatusOK)

	// admin creates invitation
	req := httptest.NewRequest(http.MethodPost, "/api/admin/invitations", bytes.NewReader([]byte(`{"role":"user"}`)))
	req.Header.Set("Cookie", cookie)
	w := httptest.NewRecorder()
	env.handlers.AdminCreateInvitation(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp struct {
		Code string `json:"code"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.NotEmpty(t, resp.Code)

	// accept invite
	accReq := []byte(`{"code":"` + resp.Code + `","username":"newbie","password":"n00bpass123"}`)
	req2 := httptest.NewRequest(http.MethodPost, "/api/auth/accept-invite", bytes.NewReader(accReq))
	w2 := httptest.NewRecorder()
	env.handlers.AcceptInvite(w2, req2)
	require.Equal(t, http.StatusOK, w2.Code, "body=%s", w2.Body.String())
	require.Contains(t, w2.Result().Header.Get("Set-Cookie"), "webchat_session=")

	u, err := env.store.GetUserByUsername(context.Background(), "newbie")
	require.NoError(t, err)
	require.Equal(t, "user", u.Role)
}

func TestAcceptInvite_ExpiredInvitation(t *testing.T) {
	t.Parallel()
	env := newTestAuthEnv(t)
	require.NoError(t, env.store.CreateInvitation(context.Background(), &session.Invitation{
		ID: "inv-exp", Code: "EXPIRED1", CreatedBy: "u-admin", Role: "user", ExpiresAt: 1000,
	}, 1700000000))
	req := httptest.NewRequest(http.MethodPost, "/api/auth/accept-invite",
		bytes.NewReader([]byte(`{"code":"EXPIRED1","username":"x","password":"x1234567"}`)))
	w := httptest.NewRecorder()
	env.handlers.AcceptInvite(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "INVITATION_EXPIRED")
}

func TestAdminEndpoint_RequiresAdmin(t *testing.T) {
	t.Parallel()
	env := newTestAuthEnv(t)
	env.createUser(t, "normal", "userpass1", "user")
	cookie := env.loginAs(t, "normal", "userpass1", http.StatusOK)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/invitations", nil)
	req.Header.Set("Cookie", cookie)
	w := httptest.NewRecorder()
	env.handlers.AdminListInvitations(w, req)
	require.Equal(t, http.StatusForbidden, w.Code, "普通用户不能访问 admin 端点")
}
