package gateway

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/hrygo/hotplex/internal/agentconfig"
	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/security"
	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/internal/web"
	"github.com/hrygo/hotplex/internal/worker"
)

// WorkspaceHandlers serves /api/workspaces (spec §9.1, §11.3).
type WorkspaceHandlers struct {
	store session.UserWorkspaceStore
	auth  *security.Authenticator
	now   func() time.Time
}

// NewWorkspaceHandlers constructs workspace CRUD handlers.
func NewWorkspaceHandlers(store session.UserWorkspaceStore, auth *security.Authenticator) *WorkspaceHandlers {
	return &WorkspaceHandlers{store: store, auth: auth, now: time.Now}
}

func (h *WorkspaceHandlers) nowUnix() int64 { return h.now().Unix() }

func (h *WorkspaceHandlers) currentUser(r *http.Request) (string, bool) {
	// Dual-channel auth (spec ⑦ Phase 1): api-key header/query first (machine
	// integration), cookie second (WebChat UI). AuthenticateRequest chains these
	// two with shared disabled-user enforcement — the same enforcement used by
	// the REST session API and the WS upgrade path. Aligning workspace REST on
	// the same chain makes workspace a cross-channel tenant anchor: the same
	// users.id owns workspaces regardless of entry channel.
	uid, _, err := h.auth.AuthenticateRequest(r)
	if err != nil {
		return "", false
	}
	return uid, true
}

func (h *WorkspaceHandlers) requireAuth(w http.ResponseWriter, r *http.Request) (string, bool) {
	uid, ok := h.currentUser(r)
	if !ok {
		writeAppError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "not authenticated")
		return "", false
	}
	return uid, true
}

// isAdmin reports whether uid is an active admin (role==admin && status==active).
//
// Same-source authority (spec ⑦ P2.7): uid is the identity currentUser already
// resolved via AuthenticateRequest (api-key first, cookie fallback), so
// identity and admin authority now derive from the same channel. This closes
// the cross-channel ambiguity flagged in the PR #773 review (P2-1, finding B):
// a request carrying both api-key + admin cookie no longer earns the bypass
// from the cookie alone — the resolved uid itself must be an admin.
func (h *WorkspaceHandlers) isAdmin(r *http.Request, uid string) bool {
	idp := h.auth.IdentityProvider()
	if idp == nil {
		return false
	}
	u, err := idp.Lookup(r.Context(), uid)
	if err != nil || u.Role != "admin" || u.Status != "active" {
		return false
	}
	return true
}

type createWorkspaceRequest struct {
	Name           string  `json:"name"`
	WorkDir        string  `json:"work_dir"`
	PermissionMode *string `json:"permission_mode"` // nil/"" = "worker default" (no explicit override, stored as ""); else one of worker.PermissionMode* (issue #789)
}

// Create: POST /api/workspaces
func (h *WorkspaceHandlers) Create(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.requireAuth(w, r)
	if !ok {
		return
	}
	var req createWorkspaceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAppError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid body")
		return
	}
	if req.Name == "" || req.WorkDir == "" {
		writeAppError(w, http.StatusBadRequest, "BAD_REQUEST", "name and work_dir required")
		return
	}
	// Permission mode is optional; nil/"" means "worker default" (stored as "" — no
	// explicit override). Validate before construction so an invalid tier is rejected early (issue #789).
	var permMode string
	if req.PermissionMode != nil {
		// r3 (#804): permission_mode is admin-only. Fail-closed 403 before format
		// validation so a non-admin never learns whether their value was valid.
		if !h.isAdmin(r, uid) {
			writeAppError(w, http.StatusForbidden, "PERMISSION_DENIED", "permission_mode can only be configured by admins")
			return
		}
		if err := worker.ValidatePermissionMode(*req.PermissionMode); err != nil {
			writeAppError(w, http.StatusBadRequest, "INVALID_PERMISSION_MODE", err.Error())
			return
		}
		permMode = *req.PermissionMode
	}
	// Security dual-check (same standard as SwitchWorkDir, spec §9.1).
	abs, err := config.ExpandAndAbs(req.WorkDir)
	if err != nil {
		writeAppError(w, http.StatusBadRequest, "INVALID_WORK_DIR", err.Error())
		return
	}
	if err := security.ValidateWorkDir(abs); err != nil {
		writeAppError(w, http.StatusForbidden, "WORK_DIR_FORBIDDEN", err.Error())
		return
	}
	if err := security.ValidateWorkspaceWorkDir(abs, uid); err != nil {
		writeAppError(w, http.StatusForbidden, "WORK_DIR_OUTSIDE_SANDBOX", "work_dir must be under $HOME/.hotplex/workspaces/<your-user-id>")
		return
	}
	ws := &session.Workspace{
		ID: uuid.NewString(), OwnerUserID: uid, Name: req.Name, WorkDir: abs, Status: "active",
		PermissionMode: permMode,
	}
	if err := h.store.CreateWorkspace(r.Context(), ws, h.nowUnix()); err != nil {
		if isUniqueViolation(err) {
			writeAppError(w, http.StatusConflict, "WORK_DIR_TAKEN", "work_dir already used by you")
			return
		}
		writeAppError(w, http.StatusInternalServerError, "INTERNAL", "create failed")
		return
	}
	respondJSON(w, ws)
}

// List: GET /api/workspaces — returns only the caller's workspaces (private, spec §9.1).
func (h *WorkspaceHandlers) List(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.requireAuth(w, r)
	if !ok {
		return
	}
	limit, offset := web.ParsePagination(r)
	// LIMIT/OFFSET 下推到 store 层（PR #773 P2）：跨通道租户接入后 api-key 通道可能
	// 程序化批量创建 workspace，内存分页会退化为无界查询。
	wss, err := h.store.ListWorkspacesByOwner(r.Context(), uid, limit, offset)
	if err != nil {
		writeAppError(w, http.StatusInternalServerError, "INTERNAL", "list failed")
		return
	}
	respondJSON(w, map[string]any{
		"workspaces": wss,
		"limit":      limit,
		"offset":     offset,
	})
}

// Get: GET /api/workspaces/{id}
func (h *WorkspaceHandlers) Get(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.requireAuth(w, r)
	if !ok {
		return
	}
	ws, err := h.store.GetWorkspaceByID(r.Context(), r.PathValue("id"))
	if err != nil {
		writeAppError(w, http.StatusNotFound, "WORKSPACE_NOT_FOUND", "not found")
		return
	}
	if ws.OwnerUserID != uid && !h.isAdmin(r, uid) {
		writeAppError(w, http.StatusForbidden, "WORKSPACE_FORBIDDEN", "not your workspace")
		return
	}
	respondJSON(w, ws)
}

type updateWorkspaceRequest struct {
	Name                 string  `json:"name"`
	AgentConfigOverrides string  `json:"agent_config_overrides"`
	WorkerPreference     *string `json:"worker_preference"` // nil = omit (no change); "" = explicit clear to default
	WorkDir              string  `json:"work_dir"`          // workspace-level mutable (session-level inherits)
	PermissionMode       *string `json:"permission_mode"`   // nil = omit (no change); "" = clear to "worker default" (no explicit override); else worker.PermissionMode* (issue #789)
}

// Update: PATCH /api/workspaces/{id}
func (h *WorkspaceHandlers) Update(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.requireAuth(w, r)
	if !ok {
		return
	}
	var req updateWorkspaceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAppError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid body")
		return
	}
	ws, err := h.store.GetWorkspaceByID(r.Context(), r.PathValue("id"))
	if err != nil {
		writeAppError(w, http.StatusNotFound, "WORKSPACE_NOT_FOUND", "not found")
		return
	}
	// Admin authority is needed in two places: the owner check (only when the acting
	// user isn't the owner) and the permission_mode gate (whenever the field is
	// present). Memoize so an admin editing another's workspace with permission_mode
	// pays one idp.Lookup, not two — and an owner editing their own workspace without
	// permission_mode pays zero.
	var admin bool
	var adminResolved bool
	resolveAdmin := func() bool {
		if !adminResolved {
			admin, adminResolved = h.isAdmin(r, uid), true
		}
		return admin
	}
	if ws.OwnerUserID != uid && !resolveAdmin() {
		writeAppError(w, http.StatusForbidden, "WORKSPACE_FORBIDDEN", "not your workspace")
		return
	}
	if req.WorkDir != "" {
		abs, err := config.ExpandAndAbs(req.WorkDir)
		if err != nil {
			writeAppError(w, http.StatusBadRequest, "INVALID_WORK_DIR", err.Error())
			return
		}
		if err := security.ValidateWorkDir(abs); err != nil {
			writeAppError(w, http.StatusForbidden, "WORK_DIR_FORBIDDEN", err.Error())
			return
		}
		// Sandbox is keyed by the workspace OWNER, not the acting user, so an
		// admin editing another user's workspace keeps owner isolation (spec §2 G2).
		// Create (:106) passes uid because the creator IS the owner; Update is the
		// admin-edit path where the two can differ.
		if err := security.ValidateWorkspaceWorkDir(abs, ws.OwnerUserID); err != nil {
			writeAppError(w, http.StatusForbidden, "WORK_DIR_OUTSIDE_SANDBOX", "work_dir must be under the workspace owner's sandbox ($HOME/.hotplex/workspaces/<owner-user-id>)")
			return
		}
		// work_dir participates in DeriveSessionKey (key.go), so changing it shifts
		// the deterministic session id and orphans any bound active session's
		// history. Reject the change while active sessions exist, mirroring the
		// DeleteWorkspaceIfEmpty guard used by Delete (spec §9.1). No-op when the
		// value is unchanged.
		if abs != ws.WorkDir {
			n, err := h.store.CountActiveSessionsInWorkspace(r.Context(), ws.ID)
			if err != nil {
				writeAppError(w, http.StatusInternalServerError, "INTERNAL", "check active sessions failed")
				return
			}
			if n > 0 {
				writeAppError(w, http.StatusConflict, "WORKSPACE_NOT_EMPTY", "cannot change work_dir while the workspace has active sessions")
				return
			}
		}
		ws.WorkDir = abs
	}
	if req.Name != "" {
		ws.Name = req.Name
	}
	if req.AgentConfigOverrides != "" {
		if _, err := agentconfig.ValidateOverrides(req.AgentConfigOverrides); err != nil {
			switch {
			case errors.Is(err, agentconfig.ErrInvalidConfigJSON):
				writeAppError(w, http.StatusBadRequest, "INVALID_CONFIG_JSON", err.Error())
			case errors.Is(err, agentconfig.ErrUnknownConfigFile):
				writeAppError(w, http.StatusBadRequest, "UNKNOWN_CONFIG_FILE", err.Error())
			case errors.Is(err, agentconfig.ErrConfigTooLarge):
				writeAppError(w, http.StatusBadRequest, "CONFIG_TOO_LARGE", err.Error())
			default:
				writeAppError(w, http.StatusBadRequest, "INVALID_CONFIG_VALUE", err.Error())
			}
			return
		}
		ws.AgentConfigOverrides = req.AgentConfigOverrides
	}
	if req.WorkerPreference != nil {
		// nil = field omitted (no change); "" = explicit clear to default.
		// ValidateType accepts "" as inherit-default, so clearing needs no special case.
		if err := worker.ValidateType(worker.WorkerType(*req.WorkerPreference)); err != nil {
			writeAppError(w, http.StatusBadRequest, "INVALID_WORKER_TYPE", err.Error())
			return
		}
		ws.WorkerPreference = *req.WorkerPreference
	}
	if req.PermissionMode != nil {
		// r3 (#804): permission_mode is admin-only. nil = field omitted (no change);
		// "" = clear to default. Fail-closed 403 before format validation; a non-admin
		// owner can still PATCH other fields (name/work_dir/etc) — only this field is gated.
		if !resolveAdmin() {
			writeAppError(w, http.StatusForbidden, "PERMISSION_DENIED", "permission_mode can only be configured by admins")
			return
		}
		if err := worker.ValidatePermissionMode(*req.PermissionMode); err != nil {
			writeAppError(w, http.StatusBadRequest, "INVALID_PERMISSION_MODE", err.Error())
			return
		}
		ws.PermissionMode = *req.PermissionMode
	}
	if err := h.store.UpdateWorkspace(r.Context(), ws, h.nowUnix()); err != nil {
		if errors.Is(err, session.ErrWorkspaceNotEmpty) {
			writeAppError(w, http.StatusConflict, "WORKSPACE_NOT_EMPTY", "cannot change work_dir while the workspace has active sessions")
			return
		}
		if errors.Is(err, session.ErrWorkspaceConflict) {
			writeAppError(w, http.StatusConflict, "WORKSPACE_VERSION_MISMATCH", "workspace concurrently modified, please re-fetch and retry")
			return
		}
		if isUniqueViolation(err) {
			writeAppError(w, http.StatusConflict, "WORK_DIR_TAKEN", "work_dir already used by you")
			return
		}
		writeAppError(w, http.StatusInternalServerError, "INTERNAL", "update failed")
		return
	}
	respondJSON(w, ws)
}

// Delete: DELETE /api/workspaces/{id} — hard delete after verifying no active sessions (spec §9.1).
func (h *WorkspaceHandlers) Delete(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.requireAuth(w, r)
	if !ok {
		return
	}
	ws, err := h.store.GetWorkspaceByID(r.Context(), r.PathValue("id"))
	if err != nil {
		writeAppError(w, http.StatusNotFound, "WORKSPACE_NOT_FOUND", "not found")
		return
	}
	if ws.OwnerUserID != uid && !h.isAdmin(r, uid) {
		writeAppError(w, http.StatusForbidden, "WORKSPACE_FORBIDDEN", "not your workspace")
		return
	}
	// 原子删除：仅当无活跃会话时成功，防 Count↔Delete TOCTOU（spec §9.1）。
	if err := h.store.DeleteWorkspaceIfEmpty(r.Context(), ws.ID); err != nil {
		if errors.Is(err, session.ErrWorkspaceNotEmpty) {
			writeAppError(w, http.StatusConflict, "WORKSPACE_NOT_EMPTY", "workspace has active sessions")
			return
		}
		if errors.Is(err, session.ErrWorkspaceNotFound) {
			writeAppError(w, http.StatusNotFound, "WORKSPACE_NOT_FOUND", "workspace concurrently deleted")
			return
		}
		writeAppError(w, http.StatusInternalServerError, "INTERNAL", "delete failed")
		return
	}
	w.WriteHeader(http.StatusOK)
}
