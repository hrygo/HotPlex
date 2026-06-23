package admin

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/hrygo/hotplex/internal/security"
	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/internal/web"
)

// UserAdminHandlers serves the /api/admin/* endpoints used by the WebChat admin
// UI (spec §8, §11.1-11.2). Unlike the AdminAPI (port 9999, Bearer+scope), these
// handlers authenticate via the cookie session established by /api/auth/login —
// they are mounted on the gateway mux alongside the other /api/auth/* routes.
//
// Mounted on the gateway mux (not adminMux) because WebChat admin page uses
// cookie auth, not Bearer tokens. requireAdmin resolves the cookie to a uid and
// enforces role==admin && status==active (P2.7: same-source authority — identity
// and admin authority both derive from the resolved uid).
type UserAdminHandlers struct {
	store      session.UserWorkspaceStore
	auth       *security.Authenticator
	cookieAuth *security.CookieAuth
	idp        *security.LocalAccountProvider
	now        func() time.Time
}

// NewUserAdminHandlers constructs the /api/admin/* handlers.
func NewUserAdminHandlers(store session.UserWorkspaceStore, auth *security.Authenticator, cookieAuth *security.CookieAuth, idp *security.LocalAccountProvider) *UserAdminHandlers {
	return &UserAdminHandlers{store: store, auth: auth, cookieAuth: cookieAuth, idp: idp, now: time.Now}
}

func (h *UserAdminHandlers) nowUnix() int64 { return h.now().Unix() }

// requireAdmin validates the current user is an active admin.
// Returns (uid, true) on success; writes an error and returns ("", false) otherwise.
func (h *UserAdminHandlers) requireAdmin(w http.ResponseWriter, r *http.Request) (string, bool) {
	uid, ok := h.cookieAuth.Authenticate(r)
	if !ok {
		web.WriteAppError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "not authenticated")
		return "", false
	}
	if h.idp == nil {
		web.WriteAppError(w, http.StatusServiceUnavailable, "NO_IDP", "no identity provider")
		return "", false
	}
	u, err := h.idp.Lookup(r.Context(), uid)
	if err != nil || u.Status != "active" {
		web.WriteAppError(w, http.StatusForbidden, "USER_DISABLED", "user disabled")
		return "", false
	}
	if u.Role != "admin" {
		web.WriteAppError(w, http.StatusForbidden, "FORBIDDEN", "admin only")
		return "", false
	}
	return uid, true
}

type createInvitationRequest struct {
	Role string `json:"role"` // 'user' | 'admin'
	TTL  int    `json:"ttl"`  // seconds; 0 = default 7 days
}

const defaultInvitationTTL = 7 * 24 * 3600

// CreateInvitation: POST /api/admin/invitations
func (h *UserAdminHandlers) CreateInvitation(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	var req createInvitationRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		web.WriteAppError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid body")
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
		web.WriteAppError(w, http.StatusInternalServerError, "INTERNAL", "generate invite code failed")
		return
	}
	inv := &session.Invitation{
		ID: uuid.NewString(), Code: code, CreatedBy: uid,
		Role: req.Role, ExpiresAt: h.nowUnix() + int64(ttl),
	}
	if err := h.store.CreateInvitation(r.Context(), inv, h.nowUnix()); err != nil {
		web.WriteAppError(w, http.StatusInternalServerError, "INTERNAL", "create invitation failed")
		return
	}
	respondJSON(w, map[string]any{"id": inv.ID, "code": code, "role": inv.Role, "expires_at": inv.ExpiresAt})
}

// ListInvitations: GET /api/admin/invitations
func (h *UserAdminHandlers) ListInvitations(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	limit, offset := web.ParsePagination(r)
	invs, err := h.store.ListInvitations(r.Context(), limit, offset)
	if err != nil {
		web.WriteAppError(w, http.StatusInternalServerError, "INTERNAL", "list failed")
		return
	}
	respondJSON(w, map[string]any{"invitations": invs, "limit": limit, "offset": offset})
}

// DeleteInvitation: DELETE /api/admin/invitations/{id}
func (h *UserAdminHandlers) DeleteInvitation(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	id := r.PathValue("id")
	if err := h.store.DeleteInvitation(r.Context(), id); err != nil {
		web.WriteAppError(w, http.StatusInternalServerError, "INTERNAL", "delete failed")
		return
	}
	w.WriteHeader(http.StatusOK)
}

// ListUsers: GET /api/admin/users
func (h *UserAdminHandlers) ListUsers(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	limit, offset := web.ParsePagination(r)
	users, err := h.store.ListUsers(r.Context(), limit, offset)
	if err != nil {
		web.WriteAppError(w, http.StatusInternalServerError, "INTERNAL", "list failed")
		return
	}
	respondJSON(w, map[string]any{"users": users, "limit": limit, "offset": offset})
}

type updateUserStatusRequest struct {
	Status string `json:"status"` // 'active' | 'disabled'
}

// UpdateUserStatus: PATCH /api/admin/users/{id}
func (h *UserAdminHandlers) UpdateUserStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	id := r.PathValue("id")
	var req updateUserStatusRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		web.WriteAppError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid body")
		return
	}
	if req.Status != "active" && req.Status != "disabled" {
		web.WriteAppError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid status")
		return
	}
	if err := h.store.UpdateUserStatus(r.Context(), id, req.Status, h.nowUnix()); err != nil {
		web.WriteAppError(w, http.StatusInternalServerError, "INTERNAL", "update failed")
		return
	}
	w.WriteHeader(http.StatusOK)
}
