package gateway

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/security"
	"github.com/hrygo/hotplex/internal/session"
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
	uid, ok := h.cookieAuth.Authenticate(r)
	return uid, ok
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
func (h *WorkspaceHandlers) isAdmin(r *http.Request) bool {
	idp := h.auth.IdentityProvider()
	if idp == nil {
		return false
	}
	uid, ok := h.cookieAuth.Authenticate(r)
	if !ok {
		return false
	}
	u, err := idp.Lookup(r.Context(), uid)
	return err == nil && u.Role == "admin" && u.Status == "active"
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
	respondJSON(w, map[string]any{"workspaces": wss})
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
		ws.AgentConfigOverrides = req.AgentConfigOverrides
	}
	if req.WorkerPreference != "" {
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
	n, err := h.store.CountActiveSessionsInWorkspace(r.Context(), ws.ID)
	if err != nil {
		writeAppError(w, http.StatusInternalServerError, "INTERNAL", "count failed")
		return
	}
	if n > 0 {
		writeAppError(w, http.StatusConflict, "WORKSPACE_NOT_EMPTY", "workspace has active sessions")
		return
	}
	if err := h.store.DeleteWorkspace(r.Context(), ws.ID); err != nil {
		writeAppError(w, http.StatusInternalServerError, "INTERNAL", "delete failed")
		return
	}
	w.WriteHeader(http.StatusOK)
}
