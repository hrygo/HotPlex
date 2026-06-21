package gateway

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/hrygo/hotplex/internal/security"
	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/internal/web"
)

// AppError is the JSON error envelope for multitenancy API endpoints (spec §12).
type AppError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// writeAppError writes a structured JSON error (spec §12 error codes).
func writeAppError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": AppError{Code: code, Message: msg}})
}

// AuthHandlers holds dependencies for authentication + admin endpoints (spec §8, §11.1-11.2).
type AuthHandlers struct {
	auth       *security.Authenticator
	cookieAuth *security.CookieAuth
	store      session.UserWorkspaceStore
	idp        *security.LocalAccountProvider
	now        func() time.Time
}

// NewAuthHandlers constructs auth + admin handlers.
func NewAuthHandlers(auth *security.Authenticator, cookieAuth *security.CookieAuth, store session.UserWorkspaceStore, idp *security.LocalAccountProvider) *AuthHandlers {
	return &AuthHandlers{auth: auth, cookieAuth: cookieAuth, store: store, idp: idp, now: time.Now}
}

func (h *AuthHandlers) nowUnix() int64 { return h.now().Unix() }

// currentUserID extracts the user ID from the cookie. Returns ("", false) if absent/invalid.
func (h *AuthHandlers) currentUserID(r *http.Request) (string, bool) {
	uid, ok := h.cookieAuth.Authenticate(r)
	return uid, ok
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// Login: POST /api/auth/login (spec §8.2 account-login channel).
func (h *AuthHandlers) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAppError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid body")
		return
	}
	uid, err := h.idp.Authenticate(r.Context(), security.LoginCredentials{Username: req.Username, Password: req.Password})
	if err != nil {
		var ie *security.IdentityError
		if errors.As(err, &ie) {
			switch ie.Code {
			case security.ErrCodeUserDisabled:
				writeAppError(w, http.StatusForbidden, "USER_DISABLED", "user disabled")
			default:
				writeAppError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "invalid credentials")
			}
			return
		}
		writeAppError(w, http.StatusInternalServerError, "INTERNAL", "auth error")
		return
	}
	if err := h.cookieAuth.SetCookie(w, r, uid); err != nil {
		writeAppError(w, http.StatusInternalServerError, "INTERNAL", "cookie error")
		return
	}
	// 在 touch 前读原 last_login_at,判定是否首次登录(供前端 onboarding)。
	firstLogin := false
	if u, lerr := h.idp.Lookup(r.Context(), uid); lerr == nil && u.LastLoginAt == 0 {
		firstLogin = true
	}
	_ = h.store.TouchUserLastLogin(r.Context(), uid, h.nowUnix()) // non-critical on success
	respondJSON(w, map[string]any{"user_id": uid, "first_login": firstLogin})
}

// Logout: POST /api/auth/logout — clears the cookie.
func (h *AuthHandlers) Logout(w http.ResponseWriter, r *http.Request) {
	h.cookieAuth.Clear(w, r)
	w.WriteHeader(http.StatusOK)
}

// BootstrapStatus: GET /api/auth/bootstrap-status — whether any admin exists.
//
// Public (no auth): the login page polls this to guide first-time setup.
// Registered OUTSIDE the CookieAuth-gated auth block in routes.go so it stays
// reachable when the system is not yet bootstrapped (CookieAuth may be nil).
func BootstrapStatus(store session.UserWorkspaceStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		has, err := store.HasAdmin(r.Context())
		if err != nil {
			writeAppError(w, http.StatusInternalServerError, "INTERNAL", "check bootstrap status")
			return
		}
		// Public, unauthenticated endpoint polled by the login page on load.
		// Cache briefly to dampen scripted/automated load — bootstrap state
		// changes rarely (only on first admin creation), so 30s is a safe TTL.
		// (Pairs with idx_users_role migration 021 for the underlying query.)
		w.Header().Set("Cache-Control", "public, max-age=30")
		respondJSON(w, map[string]bool{"bootstrapped": has})
	}
}

// Me: GET /api/auth/me — returns the current user's profile.
func (h *AuthHandlers) Me(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.currentUserID(r)
	if !ok {
		writeAppError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "not authenticated")
		return
	}
	idp := h.auth.IdentityProvider()
	if idp == nil {
		writeAppError(w, http.StatusServiceUnavailable, "NO_IDP", "identity provider not configured")
		return
	}
	u, err := idp.Lookup(r.Context(), uid)
	if err != nil {
		writeAppError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "user not found")
		return
	}
	// Return the full user profile; PasswordHash is json:"-" so it never
	// leaves the server. This fills display_name/created_at/updated_at/
	// last_login_at expected by the frontend User type, which the minimal
	// string map omitted (webchat/lib/api/auth.ts).
	respondJSON(w, u)
}

type acceptInviteRequest struct {
	Code     string `json:"code"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// AcceptInvite: POST /api/auth/accept-invite (spec §8.6 invitation registration).
//
// 顺序设计（防用户名枚举）：先 CAS 消费邀请码，再创建用户，冲突时不回滚。
// 旧实现先做 GetUserByUsername 预检查再消费码 —— 用户名冲突不消耗码，攻击者持单个
// 有效码即可无限枚举已注册用户名。新顺序让每次探测都消耗一个一次性码，枚举成本
// 从 O(1 码) 升至 O(N 码)，码由 admin 控制故枚举不再可行。代价：合法用户用户名
// 冲突时码被消费，需 admin 重新发码（一次性码的安全语义）。
func (h *AuthHandlers) AcceptInvite(w http.ResponseWriter, r *http.Request) {
	var req acceptInviteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAppError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid body")
		return
	}
	if req.Code == "" || req.Username == "" || req.Password == "" {
		writeAppError(w, http.StatusBadRequest, "BAD_REQUEST", "code, username, password required")
		return
	}
	// Validate username format BEFORE touching the invitation: a malformed or
	// reserved-namespace ("apikey:") username must not consume a one-time code
	// (review fix — prevents both invite-burning and migration-018 namespace
	// collision / identity takeover).
	if err := security.ValidateUsername(req.Username); err != nil {
		writeAppError(w, http.StatusBadRequest, "INVALID_USERNAME",
			"username must be 3-64 chars, [a-zA-Z0-9_.-], and not start with 'apikey:'")
		return
	}
	// Validate password length BEFORE consuming the invitation: bcrypt rejects
	// passwords >72 bytes (GenerateFromPassword returns bcrypt.ErrPasswordTooLong),
	// which would otherwise burn the one-time code and surface as a confusing 500.
	// Min 8 mirrors the `hotplex admin create` CLI policy (review fix).
	if len(req.Password) > 72 {
		writeAppError(w, http.StatusBadRequest, "INVALID_PASSWORD", "password too long (max 72 bytes)")
		return
	}
	if len(req.Password) < 8 {
		writeAppError(w, http.StatusBadRequest, "INVALID_PASSWORD", "password too short (min 8 chars)")
		return
	}
	ctx := r.Context()
	inv, err := h.store.GetInvitationByCode(ctx, req.Code)
	if err != nil {
		writeAppError(w, http.StatusNotFound, "INVITATION_NOT_FOUND", "invitation not found")
		return
	}
	if inv.UsedBy != nil {
		writeAppError(w, http.StatusBadRequest, "INVITATION_USED", "invitation already used")
		return
	}
	if h.nowUnix() > inv.ExpiresAt {
		writeAppError(w, http.StatusBadRequest, "INVITATION_EXPIRED", "invitation expired")
		return
	}

	// 先 CAS 消费邀请码（spec §8.6）：防用户名枚举。用 inv.CreatedBy（admin，已存在，
	// 满足 used_by 的 FK 约束）作占位消费；用户创建成功后再更新为真实接受者。
	// 用户名冲突时码已被消费（不回滚 = 不重新引入枚举），攻击者每次探测消耗一个一次性码。
	if err := h.store.MarkInvitationUsed(ctx, inv.ID, inv.CreatedBy, h.nowUnix()); err != nil {
		writeAppError(w, http.StatusBadRequest, "INVITATION_USED", "invitation already used")
		return
	}

	hash, err := h.idp.HashPassword(req.Password)
	if err != nil {
		slog.Error("accept-invite: hash password failed; invitation consumed",
			"invitation_id", inv.ID, "err", err)
		writeAppError(w, http.StatusInternalServerError, "INTERNAL", "hash error")
		return
	}
	uid := uuid.NewString()
	u := &security.User{ID: uid, Username: req.Username, PasswordHash: hash, Role: inv.Role, Status: "active"}
	if err := h.store.CreateUser(ctx, u, h.nowUnix()); err != nil {
		// 邀请码已被消费且不回滚（回滚会重新引入枚举）。用户名冲突 = 码失效，
		// 合法用户需联系 admin 重新发码（一次性码的安全语义）。
		if isUniqueViolation(err) {
			writeAppError(w, http.StatusConflict, "USERNAME_TAKEN", "username taken")
			return
		}
		slog.Error("accept-invite: create user failed; invitation consumed",
			"invitation_id", inv.ID, "user_id", uid, "err", err)
		writeAppError(w, http.StatusInternalServerError, "INTERNAL", "create user failed")
		return
	}
	// 用户已创建（uid 存在，满足 FK），将占位 used_by 更新为真实接受者。
	// 非关键失败：码已消费、用户已建，仅 used_by 审计指向 admin，记日志即可。
	if err := h.store.SetInvitationUsedBy(ctx, inv.ID, inv.CreatedBy, uid); err != nil {
		slog.Error("accept-invite: set invitation used_by failed",
			"invitation_id", inv.ID, "user_id", uid, "err", err)
	}
	_ = h.cookieAuth.SetCookie(w, r, uid)
	respondJSON(w, map[string]any{"user_id": uid, "first_login": true})
}

// isUniqueViolation detects a UNIQUE constraint violation across SQLite and PG.
// Uses driver-specific error types where possible; falls back to error string
// matching for drivers that don't expose structured codes. The SQLite arm uses
// the exact phrase "UNIQUE constraint failed" (matching dbutil.Dialect's
// canonical IsUniqueViolation) rather than a looser "UNIQUE" substring, so an
// unrelated SQLite error whose text happens to contain "UNIQUE" is not
// misclassified as a constraint violation (review fix).
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	// pgx/v5: check for *pgconn.PgError with SQLSTATE 23505
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) && pgErr.SQLState() == "23505" {
		return true
	}
	// SQLite (modernc.org/sqlite): exact-phrase match (no structured code).
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// --- admin endpoints (spec §11.2, require admin role) ---
// Pagination helper is shared via internal/web.ParsePagination (PR #764 review:
// dedup with the admin port — both now clamp to the same MaxLimit).

type createInvitationRequest struct {
	Role string `json:"role"` // 'user' | 'admin'
	TTL  int    `json:"ttl"`  // seconds; 0 = default 7 days
}

const defaultInvitationTTL = 7 * 24 * 3600

// AdminCreateInvitation: POST /api/admin/invitations
func (h *AuthHandlers) AdminCreateInvitation(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	var req createInvitationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAppError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid body")
		return
	}
	if req.Role != "user" && req.Role != "admin" {
		req.Role = "user"
	}
	ttl := req.TTL
	if ttl <= 0 {
		ttl = defaultInvitationTTL
	}
	code, err := security.GenerateInviteCode()
	if err != nil {
		writeAppError(w, http.StatusInternalServerError, "INTERNAL", "generate invite code failed")
		return
	}
	inv := &session.Invitation{
		ID: uuid.NewString(), Code: code, CreatedBy: uid,
		Role: req.Role, ExpiresAt: h.nowUnix() + int64(ttl),
	}
	if err := h.store.CreateInvitation(r.Context(), inv, h.nowUnix()); err != nil {
		writeAppError(w, http.StatusInternalServerError, "INTERNAL", "create invitation failed")
		return
	}
	respondJSON(w, map[string]any{"id": inv.ID, "code": code, "role": inv.Role, "expires_at": inv.ExpiresAt})
}

// AdminListInvitations: GET /api/admin/invitations
func (h *AuthHandlers) AdminListInvitations(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	limit, offset := web.ParsePagination(r)
	invs, err := h.store.ListInvitations(r.Context(), limit, offset)
	if err != nil {
		writeAppError(w, http.StatusInternalServerError, "INTERNAL", "list failed")
		return
	}
	respondJSON(w, map[string]any{"invitations": invs, "limit": limit, "offset": offset})
}

// AdminDeleteInvitation: DELETE /api/admin/invitations/{id}
func (h *AuthHandlers) AdminDeleteInvitation(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	id := r.PathValue("id")
	if err := h.store.DeleteInvitation(r.Context(), id); err != nil {
		writeAppError(w, http.StatusInternalServerError, "INTERNAL", "delete failed")
		return
	}
	w.WriteHeader(http.StatusOK)
}

// AdminListUsers: GET /api/admin/users
func (h *AuthHandlers) AdminListUsers(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	limit, offset := web.ParsePagination(r)
	users, err := h.store.ListUsers(r.Context(), limit, offset)
	if err != nil {
		writeAppError(w, http.StatusInternalServerError, "INTERNAL", "list failed")
		return
	}
	respondJSON(w, map[string]any{"users": users, "limit": limit, "offset": offset})
}

type updateUserStatusRequest struct {
	Status string `json:"status"` // 'active' | 'disabled'
}

// AdminUpdateUserStatus: PATCH /api/admin/users/{id}
func (h *AuthHandlers) AdminUpdateUserStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	id := r.PathValue("id")
	var req updateUserStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAppError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid body")
		return
	}
	if req.Status != "active" && req.Status != "disabled" {
		writeAppError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid status")
		return
	}
	if err := h.store.UpdateUserStatus(r.Context(), id, req.Status, h.nowUnix()); err != nil {
		writeAppError(w, http.StatusInternalServerError, "INTERNAL", "update failed")
		return
	}
	w.WriteHeader(http.StatusOK)
}

// requireAdmin validates the current user is an active admin.
// Returns (uid, true) on success; writes an error and returns ("", false) otherwise.
func (h *AuthHandlers) requireAdmin(w http.ResponseWriter, r *http.Request) (string, bool) {
	uid, ok := h.cookieAuth.Authenticate(r)
	if !ok {
		writeAppError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "not authenticated")
		return "", false
	}
	idp := h.auth.IdentityProvider()
	if idp == nil {
		writeAppError(w, http.StatusServiceUnavailable, "NO_IDP", "no identity provider")
		return "", false
	}
	u, err := idp.Lookup(r.Context(), uid)
	if err != nil || u.Status != "active" {
		writeAppError(w, http.StatusForbidden, "USER_DISABLED", "user disabled")
		return "", false
	}
	if u.Role != "admin" {
		writeAppError(w, http.StatusForbidden, "FORBIDDEN", "admin only")
		return "", false
	}
	return uid, true
}

// resolveCookieAdmin 解析 cookie 认证用户是否为 active admin，返回 (user, ok)。
// 供 WorkspaceHandlers.isAdmin 复用，保证包内 admin 判定语义（role==admin && status==active）
// 单一定义。AuthHandlers.requireAdmin 因需区分错误码（NO_IDP/USER_DISABLED/FORBIDDEN）
// 保留自有分支，但其判定与此处同源。
func resolveCookieAdmin(cookieAuth *security.CookieAuth, auth *security.Authenticator, r *http.Request) (*security.User, bool) {
	idp := auth.IdentityProvider()
	if idp == nil {
		return nil, false
	}
	uid, ok := cookieAuth.Authenticate(r)
	if !ok {
		return nil, false
	}
	u, err := idp.Lookup(r.Context(), uid)
	if err != nil || u.Role != "admin" || u.Status != "active" {
		return nil, false
	}
	return u, true
}
