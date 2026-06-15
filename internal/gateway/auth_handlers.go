package gateway

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/hrygo/hotplex/internal/security"
	"github.com/hrygo/hotplex/internal/session"
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
	_ = h.store.TouchUserLastLogin(r.Context(), uid, h.nowUnix()) // non-critical on success
	respondJSON(w, map[string]string{"user_id": uid})
}

// Logout: POST /api/auth/logout — clears the cookie.
func (h *AuthHandlers) Logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: "webchat_session", Value: "", Path: "/",
		MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode,
	})
	w.WriteHeader(http.StatusOK)
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
	respondJSON(w, map[string]string{
		"id": u.ID, "username": u.Username, "role": u.Role, "status": u.Status,
	})
}

type acceptInviteRequest struct {
	Code     string `json:"code"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// AcceptInvite: POST /api/auth/accept-invite (spec §8.6 invitation registration).
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
	if existing, _ := h.store.GetUserByUsername(ctx, req.Username); existing != nil {
		writeAppError(w, http.StatusConflict, "USERNAME_TAKEN", "username taken")
		return
	}
	hash, err := h.idp.HashPassword(req.Password)
	if err != nil {
		writeAppError(w, http.StatusInternalServerError, "INTERNAL", "hash error")
		return
	}
	uid := uuid.NewString()
	u := &security.User{ID: uid, Username: req.Username, PasswordHash: hash, Role: inv.Role, Status: "active"}
	if err := h.store.CreateUser(ctx, u, h.nowUnix()); err != nil {
		// Race: another accept grabbed the username first.
		if isUniqueViolation(err) {
			writeAppError(w, http.StatusConflict, "USERNAME_TAKEN", "username taken")
			return
		}
		writeAppError(w, http.StatusInternalServerError, "INTERNAL", "create user failed")
		return
	}
	if err := h.store.MarkInvitationUsed(ctx, inv.ID, uid, h.nowUnix()); err != nil {
		// CAS failed: another concurrent accept claimed this invitation first.
		// Disable the orphaned user to prevent a login-able account bypassing
		// the one-time invitation semantics (review P1 fix).
		_ = h.store.UpdateUserStatus(ctx, uid, "disabled", h.nowUnix())
		writeAppError(w, http.StatusConflict, "INVITATION_USED", "invitation already used")
		return
	}
	_ = h.cookieAuth.SetCookie(w, r, uid)
	respondJSON(w, map[string]string{"user_id": uid})
}

// isUniqueViolation detects a UNIQUE constraint violation across SQLite and PG.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE") || strings.Contains(msg, "23505")
}

// --- admin endpoints (spec §11.2, require admin role) ---

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
	invs, err := h.store.ListInvitations(r.Context())
	if err != nil {
		writeAppError(w, http.StatusInternalServerError, "INTERNAL", "list failed")
		return
	}
	respondJSON(w, map[string]any{"invitations": invs})
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
	users, err := h.store.ListUsers(r.Context(), 1000, 0)
	if err != nil {
		writeAppError(w, http.StatusInternalServerError, "INTERNAL", "list failed")
		return
	}
	respondJSON(w, map[string]any{"users": users})
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
