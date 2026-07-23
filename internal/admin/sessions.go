package admin

import (
	"encoding/json"
	"net/http"

	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/internal/web"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/events"
)

// CreateSession creates a new session.
//
// @Summary      Create session
// @Description  Creates a new AI agent session. Requires session:write scope.
// @Tags         Admin API
// @Produce      json
// @Security     AdminBearerAuth
// @Param        session_id   query    string  false  "Custom session ID (auto-generated if omitted)"
// @Param        user_id      query    string  false  "User identity"           default(anonymous)
// @Param        worker_type  query    string  false  "Worker type"             default(claudecode)
// @Success      200          {object}  CreateSessionResponse
// @Failure      403          {object}  ErrorResponse  "Insufficient scope: need session:write"
// @Failure      500          {object}  ErrorResponse  "Failed to create session"
// @Router       /admin/sessions [post]
func (a *AdminAPI) CreateSession(w http.ResponseWriter, r *http.Request) {
	if !hasScope(r, ScopeSessionWrite) {
		web.WriteAppError(w, http.StatusForbidden, "INSUFFICIENT_SCOPE", "insufficient scope: need session:write")
		return
	}
	id := r.URL.Query().Get("session_id")
	userID := r.URL.Query().Get("user_id")
	wt := worker.WorkerType(r.URL.Query().Get("worker_type"))
	if wt == "" {
		wt = worker.TypeClaudeCode
	}
	if id == "" {
		id = a.newSessionID()
	}
	if userID == "" {
		userID = "anonymous"
	}

	if err := a.bridge.StartSession(r.Context(), worker.SessionStartParams{
		ID:         id,
		UserID:     userID,
		WorkerType: wt,
	}); err != nil {
		a.log.Error("admin: create session failed", "err", err)
		web.WriteAppError(w, http.StatusInternalServerError, "INTERNAL", "failed to create session")
		return
	}

	a.log.Info("admin: session created",
		"session_id", id, "user_id", userID, "worker_type", string(wt),
		"admin", adminKeyPrefix(r))

	respondJSON(w, map[string]string{"session_id": id})
}

// ListSessions returns all sessions visible to the admin.
//
// @Summary      List sessions
// @Description  Lists all sessions, optionally filtered by user_id and platform. Requires session:read scope.
// @Tags         Admin API
// @Produce      json
// @Security     AdminBearerAuth
// @Param        user_id   query    string  false  "Filter by user ID"
// @Param        platform  query    string  false  "Filter by platform (webchat/slack/feishu)"
// @Param        limit     query    int     false  "Max results"  default(100)
// @Param        offset    query    int     false  "Pagination offset"  default(0)
// @Success      200       {object}  SessionListResponse
// @Failure      403       {object}  ErrorResponse  "Insufficient scope: need session:read"
// @Failure      500       {object}  ErrorResponse  "Failed to list sessions"
// @Router       /admin/sessions [get]
func (a *AdminAPI) ListSessions(w http.ResponseWriter, r *http.Request) {
	if !hasScope(r, ScopeSessionRead) {
		web.WriteAppError(w, http.StatusForbidden, "INSUFFICIENT_SCOPE", "insufficient scope: need session:read")
		return
	}
	platform := r.URL.Query().Get("platform")
	userID := r.URL.Query().Get("user_id")
	limit, offset := web.ParsePagination(r)

	sessions, err := a.sm.List(r.Context(), userID, platform, limit, offset)
	if err != nil {
		a.log.Error("admin: list sessions", "err", err)
		web.WriteAppError(w, http.StatusInternalServerError, "INTERNAL", "failed to list sessions")
		return
	}

	// Resolve readable display names for the page's user IDs so the admin view
	// shows names instead of raw IDs. Entries are absent for IDs with no users
	// row (e.g. platform-native subjects); the frontend falls back to the ID.
	userIDs := make([]string, 0, len(sessions))
	for _, s := range sessions {
		if si, ok := s.(*session.SessionInfo); ok && si.UserID != "" {
			userIDs = append(userIDs, si.UserID)
		}
	}
	identityLinks := a.buildIdentityLinks(r.Context(), userIDs)

	respondJSON(w, map[string]any{
		"sessions":       sessions,
		"identity_links": identityLinks,
		"limit":          limit,
		"offset":         offset,
	})
}

// GetSession returns details for a single session.
//
// @Summary      Get session
// @Description  Returns full session details by ID. Requires session:read scope.
// @Tags         Admin API
// @Produce      json
// @Security     AdminBearerAuth
// @Param        id   path      string  true  "Session ID"
// @Success      200  {object}  SessionDetailResponse
// @Failure      403  {object}  ErrorResponse  "Insufficient scope: need session:read"
// @Failure      404  {object}  ErrorResponse  "Session not found"
// @Router       /admin/sessions/{id} [get]
func (a *AdminAPI) GetSession(w http.ResponseWriter, r *http.Request) {
	if !hasScope(r, ScopeSessionRead) {
		web.WriteAppError(w, http.StatusForbidden, "INSUFFICIENT_SCOPE", "insufficient scope: need session:read")
		return
	}
	id := r.PathValue("id")
	si, err := a.sm.Get(r.Context(), id)
	if err != nil {
		web.WriteAppError(w, http.StatusNotFound, "NOT_FOUND", "not found")
		return
	}

	var userID string
	if sInfo, ok := si.(*session.SessionInfo); ok {
		userID = sInfo.UserID
	}
	identityLinks := a.buildIdentityLinks(r.Context(), []string{userID})

	data, _ := json.Marshal(si)
	var resp map[string]any
	_ = json.Unmarshal(data, &resp)
	resp["identity_links"] = identityLinks
	if userID != "" {
		if link, ok := identityLinks[userID]; ok {
			resp["identity_link"] = link
		}
	}

	respondJSON(w, resp)
}

// DeleteSession deletes a session by ID.
//
// @Summary      Delete session
// @Description  Permanently deletes a session. Requires session:delete scope.
// @Tags         Admin API
// @Security     AdminBearerAuth
// @Param        id   path  string  true  "Session ID"
// @Success      204  "Session deleted"
// @Failure      403  {object}  ErrorResponse  "Insufficient scope: need session:delete"
// @Failure      500  {object}  ErrorResponse  "Failed to delete session"
// @Router       /admin/sessions/{id} [delete]
func (a *AdminAPI) DeleteSession(w http.ResponseWriter, r *http.Request) {
	if !hasScope(r, ScopeSessionKill) {
		web.WriteAppError(w, http.StatusForbidden, "INSUFFICIENT_SCOPE", "insufficient scope: need session:delete")
		return
	}
	id := r.PathValue("id")
	if err := a.sm.Delete(r.Context(), id); err != nil {
		a.log.Error("admin: delete session failed", "session_id", id, "err", err)
		web.WriteAppError(w, http.StatusInternalServerError, "INTERNAL", "failed to delete session")
		return
	}

	a.log.Info("admin: session deleted",
		"session_id", id, "admin", adminKeyPrefix(r))

	w.WriteHeader(http.StatusNoContent)
}

// TerminateSession gracefully terminates a session's worker.
//
// @Summary      Terminate session
// @Description  Sends termination signal to the session worker (SIGTERM then SIGKILL). Requires session:write scope.
// @Tags         Admin API
// @Security     AdminBearerAuth
// @Param        id   path  string  true  "Session ID"
// @Success      204  "Session terminated"
// @Failure      403  {object}  ErrorResponse  "Insufficient scope: need session:write"
// @Failure      500  {object}  ErrorResponse  "Failed to terminate session"
// @Router       /admin/sessions/{id}/terminate [post]
func (a *AdminAPI) TerminateSession(w http.ResponseWriter, r *http.Request) {
	if !hasScope(r, ScopeSessionWrite) {
		web.WriteAppError(w, http.StatusForbidden, "INSUFFICIENT_SCOPE", "insufficient scope: need session:write")
		return
	}
	id := r.PathValue("id")
	if err := a.sm.Transition(r.Context(), id, events.StateTerminated); err != nil {
		a.log.Warn("admin: terminate session failed", "session_id", id, "err", err, "admin", adminKeyPrefix(r))
		web.WriteAppError(w, http.StatusInternalServerError, "INTERNAL", "failed to terminate session")
		return
	}

	a.log.Info("admin: session terminated",
		"session_id", id, "admin", adminKeyPrefix(r))

	w.WriteHeader(http.StatusNoContent)
}

// PoolStats returns session pool statistics.
//
// @Summary      Get pool stats
// @Description  Returns total, max, and per-user session pool counts. Requires stats:read scope.
// @Tags         Admin API
// @Produce      json
// @Security     AdminBearerAuth
// @Success      200  {object}  PoolStatsResponse
// @Failure      403  {object}  ErrorResponse  "Insufficient scope: need stats:read"
// @Router       /admin/sessions/pool [get]
func (a *AdminAPI) PoolStats(w http.ResponseWriter, r *http.Request) {
	if !hasScope(r, ScopeStatsRead) {
		web.WriteAppError(w, http.StatusForbidden, "INSUFFICIENT_SCOPE", "insufficient scope: need stats:read")
		return
	}
	total, max, users := a.sm.Stats()
	respondJSON(w, map[string]int{
		"total": total,
		"max":   max,
		"users": users,
	})
}

// HandleSessionStats returns turn statistics for a session.
//
// @Summary      Get session stats
// @Description  Returns turn count and token usage statistics for a session. Requires session:read scope.
// @Tags         Admin API
// @Produce      json
// @Security     AdminBearerAuth
// @Param        id   path      string  true  "Session ID"
// @Success      200  {object}  SessionStatsResponse
// @Failure      403  {object}  ErrorResponse  "Insufficient scope: need session:read"
// @Failure      404  {object}  ErrorResponse  "Session not found"
// @Failure      503  {object}  ErrorResponse  "Turn stats not available"
// @Router       /admin/sessions/{id}/stats [get]
func (a *AdminAPI) HandleSessionStats(w http.ResponseWriter, r *http.Request) {
	if !hasScope(r, ScopeSessionRead) {
		web.WriteAppError(w, http.StatusForbidden, "INSUFFICIENT_SCOPE", "insufficient scope: need session:read")
		return
	}
	if a.turnStore == nil {
		web.WriteAppError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "turn stats not available")
		return
	}

	id := r.PathValue("id")
	stats, err := a.turnStore.TurnStats(r.Context(), id)
	if err != nil {
		if r.Context().Err() != nil {
			web.WriteAppError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "request cancelled")
			return
		}
		a.log.Warn("admin: session stats", "id", id, "err", err)
		web.WriteAppError(w, http.StatusNotFound, "NOT_FOUND", "not found")
		return
	}

	respondJSON(w, stats)
}

// adminKeyPrefix returns a truncated prefix of the admin token for audit logging.
func adminKeyPrefix(r *http.Request) string {
	token := extractBearerToken(r)
	if token == "" {
		return "none"
	}
	if len(token) > 8 {
		return token[:8] + "..."
	}
	return token + "..."
}
