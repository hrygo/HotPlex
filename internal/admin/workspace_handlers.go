package admin

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/internal/web"
	"github.com/hrygo/hotplex/internal/worker"
)

// updateAdminWorkspaceRequest is the body for PATCH /admin/workspaces/{id} (issue
// #807). Only permission_mode is accepted — the admin console scope is minimal
// (list + inline permission_mode change); name/work_dir edits stay on the user
// self-service /api/workspaces/{id} endpoint so an admin can't accidentally
// rewrite a user's work_dir. PermissionMode is a pointer so "field absent" (nil,
// → 400) is distinct from "clear override" ("").
type updateAdminWorkspaceRequest struct {
	PermissionMode *string `json:"permission_mode"`
}

// HandleListAdminWorkspaces returns every workspace across owners with readable
// owner identity for the admin console global view (spec §3.1, issue #807).
// Distinct from GET /api/workspaces (user self-service, own workspaces only):
// this lists all workspaces joined with users.display_name + users.username so an
// admin scanning the table can identify ownership instead of raw owner_user_id
// UUIDs.
//
// Auth: AdminAPI.Middleware has already enforced admin (role==admin && status==
// active) before this handler runs; requireScope narrows to admin:read. GET is not
// audited (admin read endpoints aren't — audit.go isWriteMethod).
//
// @Summary      List all workspaces (admin)
// @Description  Returns every workspace across owners with owner display identity. Requires admin:read.
// @Tags         Admin API
// @Produce      json
// @Security     AdminBearerAuth
// @Success      200  {object}  WorkspaceListResponse
// @Failure      403  {object}  ErrorResponse  "Insufficient scope: need admin:read"
// @Router       /admin/workspaces [get]
func (a *AdminAPI) HandleListAdminWorkspaces(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, ScopeAdminRead) {
		return
	}
	if a.wsStore == nil {
		// Webchat/multitenancy disabled — return an empty list rather than 503 so
		// the console renders cleanly in single-tenant deployments.
		respondJSON(w, map[string]any{"workspaces": []any{}})
		return
	}
	views, err := a.wsStore.ListAllWorkspacesWithOwner(r.Context())
	if err != nil {
		a.log.Error("admin: list workspaces with owner", "err", err)
		web.WriteAppError(w, http.StatusInternalServerError, "INTERNAL", "list workspaces failed")
		return
	}
	respondJSON(w, map[string]any{"workspaces": views})
}

// HandleUpdateAdminWorkspacePermissionMode changes a single workspace's
// permission_mode (spec §3.2, issue #807). Admin-only by middleware; only the
// permission_mode field is accepted.
//
// Effect semantics: UpdateWorkspace writes the row; running sessions are
// unaffected (the worker process already has its permission params injected).
// New sessions (new conversation / /reset) pick up the new value via
// resolveWorkspacePermissionMode at init — the UI surfaces this as "takes effect
// for new sessions".
//
// The PATCH write is auto-audited by AdminAPI.Middleware's defer (audit.go) as
// AuditWorkspacePermissionModeUpdate via adminActionFor; this handler does not
// call AdminAudit itself (the middleware covers both ok and failed results).
//
// @Summary      Update workspace permission mode (admin)
// @Description  Change a workspace's permission_mode. Admin-only. Affects new sessions only.
// @Tags         Admin API
// @Accept       json
// @Produce      json
// @Security     AdminBearerAuth
// @Param        id    path      string  true  "Workspace ID"
// @Param        body  body      updateAdminWorkspaceRequest  true  "permission_mode to set"
// @Success      200   {object}  WorkspaceResponse
// @Failure      400   {object}  ErrorResponse  "INVALID_PERMISSION_MODE / missing permission_mode"
// @Failure      404   {object}  ErrorResponse  "WORKSPACE_NOT_FOUND"
// @Failure      409   {object}  ErrorResponse  "WORKSPACE_VERSION_MISMATCH"
// @Router       /admin/workspaces/{id} [patch]
func (a *AdminAPI) HandleUpdateAdminWorkspacePermissionMode(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, ScopeAdminWrite) {
		return
	}
	id := r.PathValue("id")
	if id == "" {
		web.WriteAppError(w, http.StatusBadRequest, "BAD_REQUEST", "missing workspace id")
		return
	}
	if a.wsStore == nil {
		web.WriteAppError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "workspace store not configured")
		return
	}
	var req updateAdminWorkspaceRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		web.WriteAppError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid request body")
		return
	}
	// permission_mode must be explicitly present (nil = field omitted). This
	// endpoint's only job is changing that one field, so an absent field is a
	// client bug, not a no-op. "" is valid (clears the override → config default),
	// mirroring the user self-service PATCH semantics (workspace_handlers.go:286).
	if req.PermissionMode == nil {
		web.WriteAppError(w, http.StatusBadRequest, "BAD_REQUEST", "permission_mode field is required")
		return
	}
	if err := worker.ValidatePermissionMode(*req.PermissionMode); err != nil {
		web.WriteAppError(w, http.StatusBadRequest, "INVALID_PERMISSION_MODE", err.Error())
		return
	}
	ws, err := a.wsStore.GetWorkspaceByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, session.ErrWorkspaceNotFound) {
			web.WriteAppError(w, http.StatusNotFound, "WORKSPACE_NOT_FOUND", "workspace not found")
			return
		}
		a.log.Error("admin: get workspace", "id", id, "err", err)
		web.WriteAppError(w, http.StatusInternalServerError, "INTERNAL", "get workspace failed")
		return
	}
	ws.PermissionMode = *req.PermissionMode
	if err := a.wsStore.UpdateWorkspace(r.Context(), ws, time.Now().Unix()); err != nil {
		if errors.Is(err, session.ErrWorkspaceConflict) {
			web.WriteAppError(w, http.StatusConflict, "WORKSPACE_VERSION_MISMATCH", "workspace concurrently modified, please re-fetch and retry")
			return
		}
		// ErrWorkspaceNotEmpty is intentionally not mapped: the store only raises
		// it when a work_dir change is blocked by an active session, but this
		// endpoint only changes permission_mode (never work_dir), so it's
		// unreachable for this path. A concurrent edit that loses the updated_at
		// CAS is caught above as ErrWorkspaceConflict; any other failure is a 500.
		a.log.Error("admin: update workspace permission_mode", "id", id, "err", err)
		web.WriteAppError(w, http.StatusInternalServerError, "INTERNAL", "update failed")
		return
	}
	respondJSON(w, ws)
}
