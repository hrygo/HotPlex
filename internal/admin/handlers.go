package admin

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/hrygo/hotplex/internal/cron"
	"github.com/hrygo/hotplex/internal/dbutil"
	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/internal/web"
)

// HandleStats returns gateway runtime statistics.
//
// @Summary      Get gateway statistics
// @Description  Returns uptime, active sessions, WebSocket connections, and per-worker-type aggregates.
// @Tags         Admin API
// @Produce      json
// @Security     AdminBearerAuth
// @Success      200  {object}  StatsResponse
// @Failure      403  {object}  ErrorResponse  "Insufficient scope: need stats:read"
// @Failure      503  {object}  ErrorResponse  "Failed to query sessions"
// @Router       /admin/stats [get]
func (a *AdminAPI) HandleStats(w http.ResponseWriter, r *http.Request) {
	if !hasScope(r, ScopeStatsRead) {
		web.WriteAppError(w, http.StatusForbidden, "INSUFFICIENT_SCOPE", "insufficient scope: need stats:read")
		return
	}
	total, _, _ := a.sm.Stats()
	sessions, err := a.sm.List(r.Context(), "", "", 0, 0)
	if err != nil {
		a.log.Error("admin: failed to list sessions for stats", "err", err)
		web.WriteAppError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "failed to query sessions")
		return
	}

	byType := make(map[string]any)
	for _, si := range sessions {
		s, ok := si.(*session.SessionInfo)
		if !ok {
			continue
		}
		key := string(s.WorkerType)
		m, _ := byType[key].(map[string]any)
		if m == nil {
			m = map[string]any{
				"sessions": 0,
				// avg_memory_mb / avg_cpu_percent stay 0: SessionInfo carries no
				// per-session memory/cpu stats, so there is no aggregation source yet.
				"avg_memory_mb":   0,
				"avg_cpu_percent": 0,
			}
			byType[key] = m
		}
		m["sessions"] = m["sessions"].(int) + 1 //nolint:errcheck // m["sessions"] always int: set in the m==nil init branch above
	}

	respondJSON(w, StatsResponse{
		Gateway: GatewayStatsDetail{
			UptimeSeconds:        int(time.Since(a.startedAt).Seconds()),
			WebsocketConnections: a.hub.ConnectionsOpen(),
			SessionsActive:       total,
			SessionsTotal:        len(sessions),
		},
		Workers:  byType,
		Database: DatabaseStatsDetail{SessionsCount: len(sessions)},
	})
}

// HandleHealth reports the health of all gateway components.
//
// @Summary      Get gateway health
// @Description  Returns health status for gateway, database, and workers. Requires health:read scope.
// @Tags         Admin API
// @Produce      json
// @Security     AdminBearerAuth
// @Success      200  {object}  HealthResponse
// @Failure      403  {object}  ErrorResponse  "Insufficient scope: need health:read"
// @Router       /admin/health [get]
func (a *AdminAPI) HandleHealth(w http.ResponseWriter, r *http.Request) {
	if !hasScope(r, ScopeHealthRead) {
		web.WriteAppError(w, http.StatusForbidden, "INSUFFICIENT_SCOPE", "insufficient scope: need health:read")
		return
	}
	cfg := a.cfg.Get()
	dbHealthy := true
	var dbErr string
	if _, err := a.sm.List(r.Context(), "", "", 1, 0); err != nil {
		dbHealthy = false
		dbErr = "query failed"
		a.log.Warn("admin: health check DB probe failed", "err", err)
	}

	status := "healthy"
	if !dbHealthy {
		status = "degraded"
	}

	dbCheck := map[string]any{
		"status": map[bool]string{true: "healthy", false: "unhealthy"}[dbHealthy],
		"type":   string(dbutil.ParseDialect(cfg.DB.Driver)),
		"path":   cfg.DB.Path,
	}
	if dbErr != "" {
		dbCheck["error"] = dbErr
	}

	respondJSON(w, map[string]any{
		"status": status,
		"checks": map[string]any{
			"gateway": map[string]any{
				"status":         "healthy",
				"uptime_seconds": int(time.Since(a.startedAt).Seconds()),
			},
			"database": dbCheck,
			"workers": map[string]any{
				"status": "healthy",
			},
		},
		"version": a.version(),
	})
}

// HandleWorkerHealth reports per-worker health statuses.
//
// @Summary      Get worker health
// @Description  Returns health status for each connected worker. Returns 503 if any worker is unhealthy.
// @Tags         Admin API
// @Produce      json
// @Security     AdminBearerAuth
// @Success      200  {object}  WorkerHealthResponse
// @Failure      403  {object}  ErrorResponse  "Insufficient scope: need health:read"
// @Failure      503  {object}  WorkerHealthResponse  "One or more workers are unhealthy"
// @Router       /admin/health/workers [get]
func (a *AdminAPI) HandleWorkerHealth(w http.ResponseWriter, r *http.Request) {
	if !hasScope(r, ScopeHealthRead) {
		web.WriteAppError(w, http.StatusForbidden, "INSUFFICIENT_SCOPE", "insufficient scope: need health:read")
		return
	}

	statuses := a.sm.WorkerHealthStatuses()
	allHealthy := true
	for _, ws := range statuses {
		if !ws.Healthy {
			allHealthy = false
			break
		}
	}

	w.Header().Set("Content-Type", "application/json")
	body, _ := json.Marshal(map[string]any{
		"status":     map[bool]string{true: "ok", false: "degraded"}[allHealthy],
		"workers":    statuses,
		"checked_at": time.Now().UTC().Format(time.RFC3339),
	})
	if !allHealthy {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	_, _ = w.Write(body)
}

// HandleLogs returns recent gateway log entries.
//
// @Summary      Get recent logs
// @Description  Returns the most recent log entries from the in-memory ring buffer. Requires admin:read scope.
// @Tags         Admin API
// @Produce      json
// @Security     AdminBearerAuth
// @Param        limit  query    int  false  "Max entries to return (1-1000)"  default(100)
// @Success      200    {object}  LogsResponse
// @Failure      403    {object}  ErrorResponse  "Insufficient scope: need admin:read"
// @Router       /admin/logs [get]
func (a *AdminAPI) HandleLogs(w http.ResponseWriter, r *http.Request) {
	if !hasScope(r, ScopeAdminRead) {
		web.WriteAppError(w, http.StatusForbidden, "INSUFFICIENT_SCOPE", "insufficient scope: need admin:read")
		return
	}
	limit, _ := web.ParsePagination(r)
	logs, total := a.logCollector.Recent(limit)
	if logs == nil {
		logs = []logEntry{}
	}
	respondJSON(w, map[string]any{
		"logs":  logs,
		"total": total,
		"limit": limit,
	})
}

// HandleConfigValidate validates a partial gateway configuration without applying it.
//
// @Summary      Validate config
// @Description  Validates a partial configuration object and returns any errors or warnings. Requires config:read scope.
// @Tags         Admin API
// @Accept       json
// @Produce      json
// @Security     AdminBearerAuth
// @Param        body  body      ConfigValidateRequest  true  "Partial configuration to validate"
// @Success      200   {object}  ConfigValidateResponse
// @Failure      400   {object}  ConfigValidateResponse  "Validation failed"
// @Failure      403   {object}  ErrorResponse           "Insufficient scope: need config:read"
// @Router       /admin/config/validate [post]
func (a *AdminAPI) HandleConfigValidate(w http.ResponseWriter, r *http.Request) {
	if !hasScope(r, ScopeConfigRead) {
		web.WriteAppError(w, http.StatusForbidden, "INSUFFICIENT_SCOPE", "insufficient scope: need config:read")
		return
	}
	if r.Body == nil {
		web.WriteAppError(w, http.StatusBadRequest, "BAD_REQUEST", "empty request body")
		return
	}
	var body struct {
		Gateway *struct {
			Addr               string `json:"addr"`
			ReadBufferSize     int    `json:"read_buffer_size"`
			WriteBufferSize    int    `json:"write_buffer_size"`
			BroadcastQueueSize int    `json:"broadcast_queue_size"`
		} `json:"gateway"`
		DB *struct {
			Path string `json:"path"`
		} `json:"db"`
		Worker *struct {
			IdleTimeout      string `json:"idle_timeout"`
			ExecutionTimeout string `json:"execution_timeout"`
		} `json:"worker"`
		Security *struct {
			TLSEnabled bool `json:"tls_enabled"`
		} `json:"security"`
		Session *struct {
			RetentionPeriod string `json:"retention_period"`
			GCScanInterval  string `json:"gc_scan_interval"`
		} `json:"session"`
		Pool *struct {
			MaxSize int `json:"max_size"`
		} `json:"pool"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		web.WriteAppError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON")
		return
	}

	var validationErrs []string
	var warnings []string

	if body.Gateway != nil {
		if body.Gateway.ReadBufferSize < 0 {
			validationErrs = append(validationErrs, "gateway.read_buffer_size must be non-negative")
		}
		if body.Gateway.WriteBufferSize < 0 {
			validationErrs = append(validationErrs, "gateway.write_buffer_size must be non-negative")
		}
		if body.Gateway.BroadcastQueueSize < 0 {
			validationErrs = append(validationErrs, "gateway.broadcast_queue_size must be non-negative")
		}
	}

	if body.DB != nil {
		if body.DB.Path != "" && len(body.DB.Path) > 4096 {
			validationErrs = append(validationErrs, "db.path exceeds maximum length")
		}
	}

	if body.Pool != nil {
		if body.Pool.MaxSize <= 0 {
			validationErrs = append(validationErrs, "pool.max_size must be positive")
		}
		if body.Pool.MaxSize > 10000 {
			validationErrs = append(validationErrs, "pool.max_size must not exceed 10000")
		}
	}

	valid := len(validationErrs) == 0
	cfg := a.cfg.Get()
	if len(cfg.Security.APIKeys) == 0 {
		warnings = append(warnings, "no API keys configured; running in open-access mode")
	}

	status := http.StatusOK
	if !valid {
		status = http.StatusBadRequest
	}
	w.WriteHeader(status)
	respondJSON(w, map[string]any{
		"valid":    valid,
		"errors":   validationErrs,
		"warnings": warnings,
	})
}

// HandleConfigRollback rolls back the gateway configuration to a previous version.
//
// @Summary      Rollback config
// @Description  Reverts the live configuration to a specified history version. Requires config:read scope.
// @Tags         Admin API
// @Accept       json
// @Produce      json
// @Security     AdminBearerAuth
// @Param        body  body      RollbackRequest  true  "Version to roll back to"
// @Success      200   {object}  RollbackResponse
// @Failure      400   {object}  ErrorResponse  "Invalid version or rollback failed"
// @Failure      403   {object}  ErrorResponse  "Insufficient scope: need config:read"
// @Failure      503   {object}  ErrorResponse  "Config rollback not available"
// @Router       /admin/config/rollback [post]
func (a *AdminAPI) HandleConfigRollback(w http.ResponseWriter, r *http.Request) {
	if !hasScope(r, ScopeConfigRead) {
		web.WriteAppError(w, http.StatusForbidden, "INSUFFICIENT_SCOPE", "insufficient scope: need config:read")
		return
	}
	if a.configWatcher == nil {
		web.WriteAppError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "config rollback is not available (no config file specified)")
		return
	}
	var body struct {
		Version int `json:"version"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		web.WriteAppError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON")
		return
	}
	if body.Version < 1 {
		web.WriteAppError(w, http.StatusBadRequest, "BAD_REQUEST", "version must be a positive integer")
		return
	}

	_, idx, err := a.configWatcher.Rollback(body.Version)
	if err != nil {
		a.log.Error("admin: config rollback failed", "err", err)
		web.WriteAppError(w, http.StatusBadRequest, "BAD_REQUEST", "rollback failed")
		return
	}

	a.log.Info("config: rollback applied", "version", body.Version, "history_index", idx)
	respondJSON(w, map[string]any{
		"ok":            true,
		"rolled_back":   body.Version,
		"history_index": idx,
	})
}

// HandleDebugSession returns extended debug information for a session.
//
// @Summary      Debug session
// @Description  Returns session details plus debug snapshot including worker state and turn count. Requires admin:read scope.
// @Tags         Admin API
// @Produce      json
// @Security     AdminBearerAuth
// @Param        id   path      string  true  "Session ID"
// @Success      200  {object}  DebugSessionResponse
// @Failure      403  {object}  ErrorResponse  "Insufficient scope: need admin:read"
// @Failure      404  {object}  ErrorResponse  "Session not found"
// @Router       /admin/debug/sessions/{id} [get]
func (a *AdminAPI) HandleDebugSession(w http.ResponseWriter, r *http.Request) {
	if !hasScope(r, ScopeAdminRead) {
		web.WriteAppError(w, http.StatusForbidden, "INSUFFICIENT_SCOPE", "insufficient scope: need admin:read")
		return
	}
	id := r.PathValue("id")
	si, err := a.sm.Get(r.Context(), id)
	if err != nil {
		web.WriteAppError(w, http.StatusNotFound, "NOT_FOUND", "not found")
		return
	}

	snap, dbgOK := a.sm.DebugSnapshot(id)
	if !dbgOK {
		a.log.Warn("admin: failed to get debug snapshot", "session_id", id)
	}

	// Returns the full SessionInfo (Context, PlatformKey, WorkDir, Title may carry PII) by
	// design: this is an admin-only debug endpoint gated by ScopeAdminRead above, and these
	// fields are required for session troubleshooting. Do not JSON-whitelist them without an
	// admin-visible alternative; track field-level redaction as a separate concern if ever needed.
	//
	// Augment the in-memory snapshot with DB-backed counts (issue #879 #5).
	// snap.TurnCount (managedSession.TurnCount) and last_seq_sent (SeqGen) are
	// volatile: both reset to 0 on resume/restart, so the endpoint would
	// otherwise report a freshly-resumed session as "0 turns" while the turns
	// and events tables still hold the durable history.
	debug := map[string]any{
		"available":     dbgOK,
		"has_worker":    snap.HasWorker,
		"turn_count":    snap.TurnCount,
		"last_seq_sent": a.hub.NextSeqPeek(id),
		"worker_health": snap.WorkerHealth,
		"runtime_only":  true, // turn_count/last_seq_sent are ephemeral; see db_* below.
		"db_turn_count": nil,
		"db_last_seq":   nil,
	}
	if a.turnStore != nil {
		if stats, err := a.turnStore.TurnStats(r.Context(), id); err != nil {
			a.log.Warn("admin: query durable turn count failed", "session_id", id, "err", err)
		} else if stats != nil {
			debug["db_turn_count"] = stats.TotalTurns
		}
		if seq, err := a.turnStore.LatestSeq(r.Context(), id); err != nil {
			a.log.Warn("admin: query durable event seq failed", "session_id", id, "err", err)
		} else {
			debug["db_last_seq"] = seq
		}
	}
	respondJSON(w, map[string]any{
		"session": si,
		"debug":   debug,
	})
}

// HandleRestart initiates a graceful gateway restart.
//
// @Summary      Restart gateway
// @Description  Triggers a graceful gateway restart via the restart helper. Requires admin:write scope.
// @Tags         Admin API
// @Produce      json
// @Security     AdminBearerAuth
// @Success      200  {object}  RestartResponse
// @Failure      403  {object}  ErrorResponse  "Insufficient scope: need admin:write"
// @Failure      409  {object}  ErrorResponse  "Gateway restart already in progress"
// @Failure      500  {object}  ErrorResponse  "Gateway restart preparation failed"
// @Failure      503  {object}  ErrorResponse  "Restart not configured"
// @Router       /admin/restart [post]
func (a *AdminAPI) HandleRestart(w http.ResponseWriter, r *http.Request) {
	if !hasScope(r, ScopeAdminWrite) {
		web.WriteAppError(w, http.StatusForbidden, "INSUFFICIENT_SCOPE", "insufficient scope: need admin:write")
		return
	}
	if a.restartPrepare == nil && a.restart == nil {
		web.WriteAppError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "restart is not configured")
		return
	}

	if a.restartPrepare != nil {
		commit, abort, err := a.restartPrepare(r.Context())
		if err != nil {
			if errors.Is(err, ErrRestartConflict) {
				web.WriteAppError(w, http.StatusConflict, "RESTART_REJECTED", "gateway restart could not be scheduled")
				return
			}
			a.log.Error("admin: restart prepare failed", "error_kind", "prepare_failed")
			web.WriteAppError(w, http.StatusInternalServerError, "INTERNAL", "gateway restart could not be scheduled")
			return
		}
		if err := respondJSONAndFlush(w, map[string]any{"status": "restarting"}); err != nil {
			a.log.Warn("admin: restart acceptance response failed", "error_kind", "response_failed")
			if abort != nil {
				if abortErr := abort(); abortErr != nil {
					a.log.Error("admin: restart abort failed", "error_kind", "abort_failed")
				}
			}
			return
		}
		go func() {
			if commitErr := commit(); commitErr != nil {
				a.log.Error("admin: restart commit failed", "error_kind", "commit_failed")
				if abort != nil {
					if abortErr := abort(); abortErr != nil {
						a.log.Error("admin: restart abort failed", "error_kind", "abort_failed")
					}
				}
			}
		}()
		return
	}

	go func() {
		a.log.Info("admin: initiating gateway restart via helper")
		if err := a.restart(); err != nil {
			a.log.Error("admin: restart failed", "err", err)
		}
	}()

	respondJSON(w, map[string]any{
		"status": "restarting",
	})
}

// respondStoreError handles store/DB operation errors without leaking internal
// details. Duplicate-user errors return 409 Conflict; not-found errors return 404;
// all others are logged and return 500.
func respondStoreError(w http.ResponseWriter, log *slog.Logger, op string, err error) {
	if errors.Is(err, ErrUserIDExists) {
		web.WriteAppError(w, http.StatusConflict, "CONFLICT", "user_id already exists")
		return
	}
	if err != nil && (strings.Contains(err.Error(), "UNIQUE constraint failed: cron_jobs.name") ||
		strings.Contains(err.Error(), "cron_jobs.name")) {
		web.WriteAppError(w, http.StatusConflict, "CONFLICT", "job name already exists")
		return
	}
	if errors.Is(err, sql.ErrNoRows) ||
		errors.Is(err, cron.ErrJobNotFound) ||
		errors.Is(err, session.ErrSessionNotFound) {
		web.WriteAppError(w, http.StatusNotFound, "NOT_FOUND", "not found")
		return
	}
	log.Error(op, "err", err)
	web.WriteAppError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
}
