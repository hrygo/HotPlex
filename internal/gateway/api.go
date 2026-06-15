package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/eventstore"
	"github.com/hrygo/hotplex/internal/messaging"
	"github.com/hrygo/hotplex/internal/security"
	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/internal/worker"

	"github.com/hrygo/hotplex/pkg/events"
)

// apiSM is the narrow subset of SessionManager that GatewayAPI needs.
// Composed from canonical sub-interfaces defined in handler.go to avoid
// duplicate method declarations.
type apiSM interface {
	SessionReader
	SessionLifecycle
	SessionTransitioner
	SessionAdmin
}

type GatewayAPI struct {
	auth       *security.Authenticator
	sm         apiSM
	bridge     SessionStarter
	cfgStore   *config.ConfigStore
	turnsStore eventstore.TurnQuerier
	eventStore EventStoreReader
	wsStore    WorkspaceReader // WebChat 多租户 workspace 归属校验（spec ①）；nil = 未启用
	log        *slog.Logger
}

// EventStoreReader defines the subset of EventStore needed by the events API.
type EventStoreReader interface {
	QueryBySession(ctx context.Context, sessionID string, cursor int64, dir eventstore.CursorDirection, limit int) (*eventstore.EventPage, error)
}

// WorkspaceReader defines the subset of UserWorkspaceStore needed by the sessions
// API for workspace ownership validation (spec ①). Kept narrow so unit tests can
// mock a single method instead of the full UserWorkspaceStore.
type WorkspaceReader interface {
	GetWorkspaceByID(ctx context.Context, id string) (*session.Workspace, error)
}

func NewGatewayAPI(log *slog.Logger, auth *security.Authenticator, sm apiSM, bridge SessionStarter, cfgStore *config.ConfigStore, turnsStore eventstore.TurnQuerier, eventStore EventStoreReader, wsStore WorkspaceReader) *GatewayAPI {
	return &GatewayAPI{auth: auth, sm: sm, bridge: bridge, cfgStore: cfgStore, turnsStore: turnsStore, eventStore: eventStore, wsStore: wsStore, log: log.With("component", "api")}
}

func respondJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// authorizeSession performs auth + path ID extraction + session lookup + ownership check.
// Returns (sessionID, sessionInfo, ok). If ok is false, an HTTP error has been written.
func (g *GatewayAPI) authorizeSession(w http.ResponseWriter, r *http.Request) (string, *session.SessionInfo, bool) {
	userID, _, err := g.auth.AuthenticateRequest(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return "", nil, false
	}
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "session id required", http.StatusBadRequest)
		return "", nil, false
	}
	si, err := g.sm.Get(r.Context(), id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return "", nil, false
	}
	if si.UserID != userID {
		http.Error(w, "ownership required", http.StatusForbidden)
		return "", nil, false
	}
	// WebChat 多租户防御深度：会话绑定的 workspace 必须仍属当前用户（spec §9.3）。
	// 平台/cron 会话 workspace_id 为空，跳过此检查（向后兼容）。
	if si.WorkspaceID != "" && g.wsStore != nil {
		ws, wsErr := g.wsStore.GetWorkspaceByID(r.Context(), si.WorkspaceID)
		if wsErr != nil || ws.OwnerUserID != userID {
			writeAppError(w, http.StatusForbidden, "WORKSPACE_FORBIDDEN", "ownership required")
			return "", nil, false
		}
	}
	return id, si, true
}

// ListSessions returns sessions belonging to the authenticated user.
//
// @Summary      List sessions
// @Description  Returns paginated list of sessions for the caller's user identity. Default platform filter is "webchat"; pass platform=all to include all platforms.
// @Tags         Gateway API
// @Produce      json
// @Security     ApiKeyAuth
// @Param        limit     query    int     false  "Maximum results (1-500)"                                      default(100)
// @Param        offset    query    int     false  "Pagination offset"                                             default(0)
// @Param        platform  query    string  false  "Platform filter (webchat/slack/feishu/all)"  default(webchat)
// @Success      200  {object}  admin.GatewaySessionListResponse
// @Failure      401  {object}  admin.ErrorResponse  "Unauthorized"
// @Failure      500  {object}  admin.ErrorResponse  "Internal error"
// @Router       /api/sessions [get]
func (g *GatewayAPI) ListSessions(w http.ResponseWriter, r *http.Request) {
	userID, _, err := g.auth.AuthenticateRequest(r)
	if err != nil {
		g.log.Warn("gateway: list sessions auth failed", "method", r.Method, "path", r.URL.Path, "err", err)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	limit := 100
	offset := 0
	platform := platformWebChat // Default to webchat as requested

	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 500 {
			limit = v
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if v, err := strconv.Atoi(o); err == nil && v >= 0 {
			offset = v
		}
	}
	if p := r.URL.Query().Get("platform"); p != "" {
		if p == "all" {
			platform = ""
		} else {
			platform = p
		}
	}

	// workspace_id 可选过滤（WebChat 多 workspace，spec §9.3）。归属校验后下推到 SQL。
	workspaceFilter := r.URL.Query().Get("workspace_id")
	if workspaceFilter != "" && g.wsStore != nil {
		ws, wsErr := g.wsStore.GetWorkspaceByID(r.Context(), workspaceFilter)
		if wsErr != nil || ws.OwnerUserID != userID {
			writeAppError(w, http.StatusForbidden, "WORKSPACE_FORBIDDEN", "not your workspace")
			return
		}
	}

	sessions, err := g.sm.List(r.Context(), userID, platform, workspaceFilter, limit, offset)
	if err != nil {
		g.log.Error("gateway: list sessions failed", "method", r.Method, "path", r.URL.Path, "err", err)
		http.Error(w, "failed to list sessions", http.StatusInternalServerError)
		return
	}
	respondJSON(w, map[string]any{"sessions": sessions, "limit": limit, "offset": offset, "platform": platform, "workspace_id": workspaceFilter})
}

// CreateSession creates a new AI agent session bound to a workspace (WebChat multi-tenant, spec ①).
//
// @Summary      Create session
// @Description  Creates a new AI agent session. workspace_id and client_session_id are required. The workspace must be owned by the caller (403 WORKSPACE_FORBIDDEN otherwise). work_dir is taken from the workspace (immutable). Session id is UUIDv5 derived from (userID, workerType, clientKey, workspaceID, workDir) — 方案3.
// @Tags         Gateway API
// @Accept       json
// @Produce      json
// @Security     ApiKeyAuth
// @Param        workspace_id      body     string  true   "Workspace ID (caller must own it)"
// @Param        client_session_id body     string  true   "Client-provided session identifier (max 128 chars)"
// @Param        title             body     string  false  "Human-readable session title"
// @Param        worker_type       body     string  false  "Worker type"      default(claudecode)
// @Success      200  {object}  admin.GatewayCreateSessionResponse
// @Failure      400  {object}  admin.ErrorResponse  "Missing client_session_id or workspace_id"
// @Failure      401  {object}  admin.ErrorResponse  "Unauthorized"
// @Failure      403  {object}  admin.ErrorResponse  "WORKSPACE_FORBIDDEN: not your workspace"
// @Failure      404  {object}  admin.ErrorResponse  "WORKSPACE_NOT_FOUND"
// @Failure      500  {object}  admin.ErrorResponse  "Failed to create session"
// @Router       /api/sessions [post]
func (g *GatewayAPI) CreateSession(w http.ResponseWriter, r *http.Request) {
	userID, botID, err := g.auth.AuthenticateRequest(r)
	if err != nil {
		g.log.Warn("gateway: create session auth failed", "method", r.Method, "path", r.URL.Path, "err", err)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse JSON body (preferred for WebChat multi-tenant) with query fallback
	// for legacy callers (client_session_id / title / worker_type as query params).
	var body struct {
		WorkspaceID     string `json:"workspace_id"`
		ClientSessionID string `json:"client_session_id"`
		Title           string `json:"title"`
		WorkerType      string `json:"worker_type"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body) // best-effort; query params may be used instead
	}
	clientSessionID := body.ClientSessionID
	if clientSessionID == "" {
		clientSessionID = strings.TrimSpace(r.URL.Query().Get("client_session_id"))
	}
	workspaceID := body.WorkspaceID
	if workspaceID == "" {
		workspaceID = strings.TrimSpace(r.URL.Query().Get("workspace_id"))
	}
	title := body.Title
	if title == "" {
		title = strings.TrimSpace(r.URL.Query().Get("title"))
	}
	clientSessionID = messaging.SanitizeText(clientSessionID)
	if clientSessionID == "" {
		g.log.Warn("gateway: create session missing client_session_id", "method", r.Method, "path", r.URL.Path)
		http.Error(w, "client_session_id is required", http.StatusBadRequest)
		return
	}
	if len(clientSessionID) > session.MaxClientKeyLen {
		g.log.Warn("gateway: create session client_session_id too long", "method", r.Method, "path", r.URL.Path, "len", len(clientSessionID))
		http.Error(w, fmt.Sprintf("client_session_id too long (max %d chars)", session.MaxClientKeyLen), http.StatusBadRequest)
		return
	}
	title = messaging.SanitizeText(title)
	if len(title) > session.MaxClientKeyLen {
		g.log.Warn("gateway: create session title too long", "method", r.Method, "path", r.URL.Path, "title_len", len(title))
		http.Error(w, fmt.Sprintf("title too long (max %d chars)", session.MaxClientKeyLen), http.StatusBadRequest)
		return
	}

	// workspace_id is mandatory on the WebChat multi-tenant track (spec ①): it
	// anchors the session key (方案3) and supplies the immutable work_dir.
	if workspaceID == "" {
		writeAppError(w, http.StatusBadRequest, "BAD_REQUEST", "workspace_id is required")
		return
	}
	if g.wsStore == nil {
		g.log.Error("gateway: workspace store not configured", "method", r.Method, "path", r.URL.Path)
		http.Error(w, "workspace store unavailable", http.StatusInternalServerError)
		return
	}
	ws, err := g.wsStore.GetWorkspaceByID(r.Context(), workspaceID)
	if err != nil {
		writeAppError(w, http.StatusNotFound, "WORKSPACE_NOT_FOUND", "workspace not found")
		return
	}
	if ws.OwnerUserID != userID {
		writeAppError(w, http.StatusForbidden, "WORKSPACE_FORBIDDEN", "ownership required")
		return
	}

	// work_dir is immutable and comes from the workspace (spec §6.2).
	workDir := ws.WorkDir
	if err := session.ValidateWorkDir(workDir); err != nil {
		g.log.Error("workspace workDir failed validation", "method", r.Method, "path", r.URL.Path, "workspace_id", workspaceID, "err", err)
		writeAppError(w, http.StatusInternalServerError, "INVALID_WORK_DIR", "workspace work_dir is invalid")
		return
	}

	// worker_type resolution: body/query > workspace.WorkerPreference > default.
	wt := worker.WorkerType(body.WorkerType)
	if wt == "" {
		wt = worker.WorkerType(r.URL.Query().Get("worker_type"))
	}
	if wt == "" {
		wt = worker.WorkerType(ws.WorkerPreference)
	}
	if wt == "" {
		wt = worker.TypeClaudeCode
	}

	// Session key 方案3: workspace_id participates in the UUIDv5 hash (spec §7).
	// Same (userID, wt, clientKey, workspaceID, workDir) → same session id.
	id := session.DeriveSessionKey(userID, wt, clientSessionID, ws.ID, workDir)

	// Default userID after derivation — bridge expects non-empty.
	if userID == "" {
		userID = "anonymous"
	}

	// Idempotency check: if session exists and is active, just return it.
	if si, err := g.sm.Get(r.Context(), id); err == nil {
		if si.State != events.StateDeleted {
			respondJSON(w, map[string]string{"session_id": id})
			return
		}
		// If it's deleted, we must physically remove it before re-creating
		// to avoid StateMachine transition errors and primary key conflicts.
		_ = g.sm.DeletePhysical(r.Context(), id)
	}

	if err := g.bridge.StartSession(r.Context(), worker.SessionStartParams{
		ID:          id,
		UserID:      userID,
		BotID:       botID,
		WorkerType:  wt,
		WorkDir:     workDir,
		Platform:    platformWebChat,
		Title:       title,
		ClientKey:   clientSessionID,
		WorkspaceID: ws.ID,
	}); err != nil {
		g.log.Error("gateway: create session failed", "session_id", id, "worker_type", wt, "work_dir", workDir, "err", err)
		http.Error(w, "failed to create session", http.StatusInternalServerError)
		return
	}
	respondJSON(w, map[string]string{"session_id": id})
}

// GetSession returns details for a single session.
//
// @Summary      Get session
// @Description  Returns full session details. Ownership check: caller must own the session.
// @Tags         Gateway API
// @Produce      json
// @Security     ApiKeyAuth
// @Param        id   path      string  true  "Session ID"
// @Success      200  {object}  admin.SessionDetailResponse
// @Failure      401  {object}  admin.ErrorResponse  "Unauthorized"
// @Failure      403  {object}  admin.ErrorResponse  "Ownership required"
// @Failure      404  {object}  admin.ErrorResponse  "Session not found"
// @Router       /api/sessions/{id} [get]
func (g *GatewayAPI) GetSession(w http.ResponseWriter, r *http.Request) {
	_, si, ok := g.authorizeSession(w, r)
	if !ok {
		return
	}
	respondJSON(w, si)
}

// DeleteSession terminates and deletes a session.
//
// @Summary      Delete session
// @Description  Gracefully terminates the worker and physically deletes the session. Ownership check applies.
// @Tags         Gateway API
// @Security     ApiKeyAuth
// @Param        id   path  string  true  "Session ID"
// @Success      204  "Session deleted"
// @Failure      401  {object}  admin.ErrorResponse  "Unauthorized"
// @Failure      403  {object}  admin.ErrorResponse  "Ownership required"
// @Failure      404  {object}  admin.ErrorResponse  "Session not found"
// @Failure      500  {object}  admin.ErrorResponse  "Failed to delete session"
// @Router       /api/sessions/{id} [delete]
func (g *GatewayAPI) DeleteSession(w http.ResponseWriter, r *http.Request) {
	id, _, ok := g.authorizeSession(w, r)
	if !ok {
		return
	}

	// Gracefully terminate the worker through the state machine before deleting.
	// Transition sends SIGTERM → wait → SIGKILL and releases pool quota.
	if err := g.sm.Transition(r.Context(), id, events.StateTerminated); err != nil {
		g.log.Debug("gateway: pre-delete transition skipped", "session_id", id, "err", err)
	}

	if err := g.sm.DeletePhysical(r.Context(), id); err != nil {
		g.log.Error("gateway: delete session failed", "session_id", id, "method", r.Method, "path", r.URL.Path, "err", err)
		http.Error(w, "failed to delete session", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// SwitchWorkDir changes the working directory for a session.
//
// @Summary      Switch working directory
// @Description  Changes the working directory for an active session. Creates a new derived session ID. Ownership check applies.
// @Tags         Gateway API
// @Accept       json
// @Produce      json
// @Security     ApiKeyAuth
// @Param        id    path      string                     true  "Session ID"
// @Param        body  body      admin.SwitchWorkDirRequest  true  "New working directory"
// @Success      200   {object}  admin.SwitchWorkDirResponse
// @Failure      400   {object}  admin.ErrorResponse  "Invalid body or path"
// @Failure      401   {object}  admin.ErrorResponse  "Unauthorized"
// @Failure      403   {object}  admin.ErrorResponse  "Ownership required"
// @Failure      409   {object}  admin.ErrorResponse  "Session not active"
// @Failure      500   {object}  admin.ErrorResponse  "Internal error"
// @Router       /api/sessions/{id}/cd [post]
func (g *GatewayAPI) SwitchWorkDir(w http.ResponseWriter, r *http.Request) {
	var body struct {
		WorkDir string `json:"work_dir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		g.log.Warn("gateway: switch workdir invalid body", "method", r.Method, "path", r.URL.Path, "err", err)
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if body.WorkDir == "" {
		g.log.Warn("gateway: switch workdir missing work_dir", "method", r.Method, "path", r.URL.Path)
		http.Error(w, "work_dir is required", http.StatusBadRequest)
		return
	}

	// Expand ~ and resolve to absolute path.
	expanded, err := validateAndExpandWorkDir(body.WorkDir)
	if err != nil {
		g.log.Warn("gateway: switch workdir invalid path", "method", r.Method, "path", r.URL.Path, "work_dir", body.WorkDir, "err", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	body.WorkDir = expanded

	_, si, ok := g.authorizeSession(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")

	if !si.State.IsActive() {
		g.log.Warn("gateway: switch workdir session not active", "session_id", id, "method", r.Method, "path", r.URL.Path, "state", si.State)
		http.Error(w, "session not active", http.StatusConflict)
		return
	}

	// Delegate to bridge.
	result, err := g.bridge.SwitchWorkDir(r.Context(), id, body.WorkDir)
	if err != nil {
		var pathErr *os.PathError
		if errors.As(err, &pathErr) || strings.Contains(err.Error(), "not a directory") {
			g.log.Warn("gateway: switch workdir bad path", "session_id", id, "method", r.Method, "path", r.URL.Path, "err", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		g.log.Error("gateway: switch workdir failed", "session_id", id, "method", r.Method, "path", r.URL.Path, "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	respondJSON(w, map[string]string{
		"old_session_id": result.OldSessionID,
		"new_session_id": result.NewSessionID,
		"work_dir":       result.WorkDir,
	})
}

// GetHistory returns turn history for a session.
//
// @Summary      Get session history
// @Description  Returns paginated turn records for a session. Use before_id for cursor-based pagination. Ownership check applies.
// @Tags         Gateway API
// @Produce      json
// @Security     ApiKeyAuth
// @Param        id         path      string  true   "Session ID"
// @Param        limit      query     int     false  "Max records (1-200)"  default(50)
// @Param        before_id  query     int     false  "Return records before this turn ID"
// @Success      200  {object}  admin.HistoryResponse
// @Failure      401  {object}  admin.ErrorResponse  "Unauthorized"
// @Failure      403  {object}  admin.ErrorResponse  "Ownership required"
// @Failure      500  {object}  admin.ErrorResponse  "Failed to get history"
// @Router       /api/sessions/{id}/history [get]
func (g *GatewayAPI) GetHistory(w http.ResponseWriter, r *http.Request) {
	id, _, ok := g.authorizeSession(w, r)
	if !ok {
		return
	}

	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 200 {
			limit = v
		}
	}

	beforeID := int64(0)
	if bid := r.URL.Query().Get("before_id"); bid != "" {
		if v, err := strconv.ParseInt(bid, 10, 64); err == nil && v > 0 {
			beforeID = v
		}
	}

	if g.turnsStore == nil {
		respondJSON(w, map[string]any{"records": []any{}, "has_more": false})
		return
	}

	fetchLimit := limit + 1
	var (
		records []*eventstore.TurnRecord
		err     error
	)

	if beforeID > 0 {
		records, err = g.turnsStore.QueryTurnsBefore(r.Context(), id, beforeID, fetchLimit)
	} else {
		records, err = g.turnsStore.QueryTurns(r.Context(), id, fetchLimit, 0)
	}

	if err != nil {
		if errors.Is(err, eventstore.ErrNotFound) {
			respondJSON(w, map[string]any{"records": []any{}, "has_more": false})
			return
		}
		g.log.Error("gateway: get history failed", "session_id", id, "err", err)
		http.Error(w, "failed to get history", http.StatusInternalServerError)
		return
	}

	hasMore := len(records) > limit
	if hasMore {
		records = records[:limit]
	}

	respondJSON(w, map[string]any{"records": records, "has_more": hasMore})
}

// GetEvents returns AEP events for a session.
//
// @Summary      Get session events
// @Description  Returns paginated AEP events for a session. Use cursor and direction for navigation. Ownership check applies.
// @Tags         Gateway API
// @Produce      json
// @Security     ApiKeyAuth
// @Param        id         path      string  true   "Session ID"
// @Param        limit      query     int     false  "Max events (1-1000)"                              default(200)
// @Param        cursor     query     int     false  "Cursor sequence number"
// @Param        direction  query     string  false  "Cursor direction (after/before/latest)"  default(latest)
// @Success      200  {object}  admin.EventsResponse
// @Failure      401  {object}  admin.ErrorResponse  "Unauthorized"
// @Failure      403  {object}  admin.ErrorResponse  "Ownership required"
// @Failure      500  {object}  admin.ErrorResponse  "Failed to get events"
// @Router       /api/sessions/{id}/events [get]
func (g *GatewayAPI) GetEvents(w http.ResponseWriter, r *http.Request) {
	if g.eventStore == nil {
		respondJSON(w, map[string]any{"events": []any{}, "oldest_seq": 0, "newest_seq": 0, "has_older": false})
		return
	}

	id, _, ok := g.authorizeSession(w, r)
	if !ok {
		return
	}

	limit := 200
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 1000 {
			limit = v
		}
	}

	var cursor int64
	if c := r.URL.Query().Get("cursor"); c != "" {
		if v, err := strconv.ParseInt(c, 10, 64); err == nil && v > 0 {
			cursor = v
		}
	}

	var dir eventstore.CursorDirection
	switch d := r.URL.Query().Get("direction"); d {
	case "after":
		dir = eventstore.CursorAfter
	case "before":
		dir = eventstore.CursorBefore
	default:
		dir = eventstore.CursorLatest
	}

	page, err := g.eventStore.QueryBySession(r.Context(), id, cursor, dir, limit)
	if err != nil {
		if errors.Is(err, eventstore.ErrNotFound) {
			respondJSON(w, map[string]any{"events": []any{}, "oldest_seq": 0, "newest_seq": 0, "has_older": false})
			return
		}
		g.log.Error("gateway: get events failed", "session_id", id, "err", err)
		http.Error(w, "failed to get events", http.StatusInternalServerError)
		return
	}

	respondJSON(w, page)
}
