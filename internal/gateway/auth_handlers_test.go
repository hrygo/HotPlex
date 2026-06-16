package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
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
	auth       *security.Authenticator
	cookie     *security.CookieAuth
	store      session.UserWorkspaceStore
	idp        *security.LocalAccountProvider
	handlers   *AuthHandlers
	wsHandlers *WorkspaceHandlers
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
	ws := NewWorkspaceHandlers(store, ca, auth)
	return &testAuthEnv{auth: auth, cookie: ca, store: store, idp: idp, handlers: h, wsHandlers: ws}
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
		bytes.NewReader([]byte(`{"code":"EXPIRED1","username":"newuser","password":"x1234567"}`)))
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

// TestAdminListUsers_OmitsPasswordHash 验证 AdminListUsers 不泄漏 bcrypt 哈希（spec §11.2）。
// security.User.PasswordHash 带 json:"-"，序列化必须剔除。
func TestAdminListUsers_OmitsPasswordHash(t *testing.T) {
	t.Parallel()
	env := newTestAuthEnv(t)
	env.createUser(t, "alice", "alicepass1", "user")
	cookie := env.loginAs(t, "admin", "adminpass", http.StatusOK)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/users", nil)
	req.Header.Set("Cookie", cookie)
	w := httptest.NewRecorder()
	env.handlers.AdminListUsers(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	body := w.Body.String()
	require.Contains(t, body, `"username":"admin"`, "应返回用户列表")
	require.NotContains(t, body, "password_hash", "PasswordHash 不得序列化")
	require.NotContains(t, body, "PasswordHash", "PasswordHash 不得序列化")
	require.NotContains(t, body, "$2", "bcrypt 哈希前缀不得出现在响应")
}

// TestAcceptInvite_UsernameTaken_ConsumesInvitation 验证用户名冲突时邀请码已被消费，
// 使攻击者无法用单个码无限枚举已注册用户名（spec §8.6 防枚举）。
func TestAcceptInvite_UsernameTaken_ConsumesInvitation(t *testing.T) {
	t.Parallel()
	env := newTestAuthEnv(t)
	env.createUser(t, "taken", "takenpass1", "user") // 预占用用户名

	// admin 创建邀请码。
	cookie := env.loginAs(t, "admin", "adminpass", http.StatusOK)
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

	// 用已占用用户名 accept → 用户名冲突（但邀请码已消费，不回滚）。
	accReq := []byte(`{"code":"` + resp.Code + `","username":"taken","password":"n00bpass123"}`)
	req2 := httptest.NewRequest(http.MethodPost, "/api/auth/accept-invite", bytes.NewReader(accReq))
	w2 := httptest.NewRecorder()
	env.handlers.AcceptInvite(w2, req2)
	require.Equal(t, http.StatusConflict, w2.Code, w2.Body.String())
	require.Contains(t, w2.Body.String(), "USERNAME_TAKEN")

	// 同码再次 accept 必须返回 INVITATION_USED —— 证明码已被消费，枚举无成本不可行。
	req3 := httptest.NewRequest(http.MethodPost, "/api/auth/accept-invite", bytes.NewReader(
		[]byte(`{"code":"`+resp.Code+`","username":"freshname","password":"n00bpass123"}`)))
	w3 := httptest.NewRecorder()
	env.handlers.AcceptInvite(w3, req3)
	require.Equal(t, http.StatusBadRequest, w3.Code, w3.Body.String())
	require.Contains(t, w3.Body.String(), "INVITATION_USED", "邀请码必须已被消费")
}

// TestAcceptInvite_InvalidUsername_PreservesInvitation 验证非法用户名（含保留命名空间
// "apikey:"）在消费邀请码之前被拒——邀请码保持未消费，可被合法用户继续使用（review fix）。
func TestAcceptInvite_InvalidUsername_PreservesInvitation(t *testing.T) {
	t.Parallel()
	env := newTestAuthEnv(t)
	cookie := env.loginAs(t, "admin", "adminpass", http.StatusOK)
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

	// 保留命名空间用户名 → 拒绝。
	req2 := httptest.NewRequest(http.MethodPost, "/api/auth/accept-invite", bytes.NewReader(
		[]byte(`{"code":"`+resp.Code+`","username":"apikey:evil","password":"n00bpass123"}`)))
	w2 := httptest.NewRecorder()
	env.handlers.AcceptInvite(w2, req2)
	require.Equal(t, http.StatusBadRequest, w2.Code, w2.Body.String())
	require.Contains(t, w2.Body.String(), "INVALID_USERNAME")

	// 邀请码未被消费：用合法用户名再次 accept 应成功。
	req3 := httptest.NewRequest(http.MethodPost, "/api/auth/accept-invite", bytes.NewReader(
		[]byte(`{"code":"`+resp.Code+`","username":"legituser","password":"n00bpass123"}`)))
	w3 := httptest.NewRecorder()
	env.handlers.AcceptInvite(w3, req3)
	require.Equal(t, http.StatusOK, w3.Code, "邀请码必须仍可用 body=%s", w3.Body.String())
}

// TestAcceptInvite_PasswordTooLong_PreservesInvitation 验证 >72 字节密码在消费邀请码
// 之前被拒——防止 bcrypt 错误烧毁一次性邀请码（review fix）。
func TestAcceptInvite_PasswordTooLong_PreservesInvitation(t *testing.T) {
	t.Parallel()
	env := newTestAuthEnv(t)
	cookie := env.loginAs(t, "admin", "adminpass", http.StatusOK)
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

	// 73 字节密码 → 拒绝（bcrypt 上限 72）。
	longPW := strings.Repeat("a", 73)
	req2 := httptest.NewRequest(http.MethodPost, "/api/auth/accept-invite", bytes.NewReader(
		[]byte(`{"code":"`+resp.Code+`","username":"legituser","password":"`+longPW+`"}`)))
	w2 := httptest.NewRecorder()
	env.handlers.AcceptInvite(w2, req2)
	require.Equal(t, http.StatusBadRequest, w2.Code, w2.Body.String())
	require.Contains(t, w2.Body.String(), "INVALID_PASSWORD")

	// 邀请码未被消费：用合法密码再次 accept 应成功。
	req3 := httptest.NewRequest(http.MethodPost, "/api/auth/accept-invite", bytes.NewReader(
		[]byte(`{"code":"`+resp.Code+`","username":"legituser","password":"n00bpass123"}`)))
	w3 := httptest.NewRecorder()
	env.handlers.AcceptInvite(w3, req3)
	require.Equal(t, http.StatusOK, w3.Code, "邀请码必须仍可用 body=%s", w3.Body.String())
}
