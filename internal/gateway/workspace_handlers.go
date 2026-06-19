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
	"github.com/hrygo/hotplex/internal/worker"
)

// WorkspaceHandlers serves /api/workspaces (spec §9.1, §11.3).
type WorkspaceHandlers struct {
	store      session.UserWorkspaceStore
	cookieAuth *security.CookieAuth
	auth       *security.Authenticator
	now        func() time.Time
}

// NewWorkspaceHandlers constructs workspace CRUD handlers.
func NewWorkspaceHandlers(store session.UserWorkspaceStore, cookieAuth *security.CookieAuth, auth *security.Authenticator) *WorkspaceHandlers {
	return &WorkspaceHandlers{store: store, cookieAuth: cookieAuth, auth: auth, now: time.Now}
}

func (h *WorkspaceHandlers) nowUnix() int64 { return h.now().Unix() }

func (h *WorkspaceHandlers) currentUser(r *http.Request) (string, bool) {
	// Delegate to AuthenticateActiveCookie so disabled users are rejected on
	// the cookie path, matching the REST API and WS upgrade enforcement.
	return h.auth.AuthenticateActiveCookie(r)
}

func (h *WorkspaceHandlers) requireAuth(w http.ResponseWriter, r *http.Request) (string, bool) {
	uid, ok := h.currentUser(r)
	if !ok {
		writeAppError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "not authenticated")
		return "", false
	}
	return uid, true
}

// isAdmin reports whether the current cookie user is an active admin.
// 委托 resolveCookieAdmin，使 admin 判定与 AuthHandlers.requireAdmin 同源（spec §11.2）。
func (h *WorkspaceHandlers) isAdmin(r *http.Request) bool {
	_, ok := resolveCookieAdmin(h.cookieAuth, h.auth, r)
	return ok
}

type createWorkspaceRequest struct {
	Name    string `json:"name"`
	WorkDir string `json:"work_dir"`
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
	ws := &session.Workspace{
		ID: uuid.NewString(), OwnerUserID: uid, Name: req.Name, WorkDir: abs, Status: "active",
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
	wss, err := h.store.ListWorkspacesByOwner(r.Context(), uid)
	if err != nil {
		writeAppError(w, http.StatusInternalServerError, "INTERNAL", "list failed")
		return
	}
	limit, offset := parsePagination(r)
	// Per-owner workspace counts are small; the store returns all active rows
	// (already ordered by created_at ASC), so we paginate in memory and echo
	// the requested limit/offset to satisfy the ListWorkspacesResponse contract.
	start := min(offset, len(wss))
	end := min(start+limit, len(wss))
	respondJSON(w, map[string]any{
		"workspaces": wss[start:end],
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
	if ws.OwnerUserID != uid && !h.isAdmin(r) {
		writeAppError(w, http.StatusForbidden, "WORKSPACE_FORBIDDEN", "not your workspace")
		return
	}
	respondJSON(w, ws)
}

type updateWorkspaceRequest struct {
	Name                 string `json:"name"`
	AgentConfigOverrides string `json:"agent_config_overrides"`
	WorkerPreference     string `json:"worker_preference"`
	WorkDir              string `json:"work_dir"` // must be rejected (immutable, spec §6.2)
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
	if req.WorkDir != "" {
		writeAppError(w, http.StatusBadRequest, "WORK_DIR_IMMUTABLE", "work_dir is immutable")
		return
	}
	ws, err := h.store.GetWorkspaceByID(r.Context(), r.PathValue("id"))
	if err != nil {
		writeAppError(w, http.StatusNotFound, "WORKSPACE_NOT_FOUND", "not found")
		return
	}
	if ws.OwnerUserID != uid && !h.isAdmin(r) {
		writeAppError(w, http.StatusForbidden, "WORKSPACE_FORBIDDEN", "not your workspace")
		return
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
	if req.WorkerPreference != "" {
		if err := worker.ValidateType(worker.WorkerType(req.WorkerPreference)); err != nil {
			writeAppError(w, http.StatusBadRequest, "INVALID_WORKER_TYPE", err.Error())
			return
		}
		ws.WorkerPreference = req.WorkerPreference
	}
	if err := h.store.UpdateWorkspace(r.Context(), ws, h.nowUnix()); err != nil {
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
	if ws.OwnerUserID != uid && !h.isAdmin(r) {
		writeAppError(w, http.StatusForbidden, "WORKSPACE_FORBIDDEN", "not your workspace")
		return
	}
	// 原子删除：仅当无活跃会话时成功，防 Count↔Delete TOCTOU（spec §9.1）。
	if err := h.store.DeleteWorkspaceIfEmpty(r.Context(), ws.ID); err != nil {
		if errors.Is(err, session.ErrWorkspaceNotEmpty) {
			writeAppError(w, http.StatusConflict, "WORKSPACE_NOT_EMPTY", "workspace has active sessions")
			return
		}
		writeAppError(w, http.StatusInternalServerError, "INTERNAL", "delete failed")
		return
	}
	w.WriteHeader(http.StatusOK)
}
