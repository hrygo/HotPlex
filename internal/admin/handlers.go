package admin

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/hrygo/hotplex/internal/cron"
	"github.com/hrygo/hotplex/internal/dbutil"
	"github.com/hrygo/hotplex/internal/session"
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
		http.Error(w, "insufficient scope: need stats:read", http.StatusForbidden)
		return
	}
	total, _, _ := a.sm.Stats()
	sessions, err := a.sm.List(r.Context(), "", "", 0, 0)
	if err != nil {
		a.log.Error("admin: failed to list sessions for stats", "err", err)
		http.Error(w, "failed to query sessions", http.StatusServiceUnavailable)
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
		m["sessions"] = m["sessions"].(int) + 1 //nolint:errcheck // guaranteed by filter logic
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
		http.Error(w, "insufficient scope: need health:read", http.StatusForbidden)
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
		http.Error(w, "insufficient scope: need health:read", http.StatusForbidden)
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
		http.Error(w, "insufficient scope: need admin:read", http.StatusForbidden)
		return
	}
	limit, _ := parsePagination(r)
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
		http.Error(w, "insufficient scope: need config:read", http.StatusForbidden)
		return
	}
	if r.Body == nil {
		http.Error(w, "empty request body", http.StatusBadRequest)
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
		http.Error(w, "invalid JSON", http.StatusBadRequest)
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
		http.Error(w, "insufficient scope: need config:read", http.StatusForbidden)
		return
	}
	if a.configWatcher == nil {
		http.Error(w, "config rollback is not available (no config file specified)", http.StatusServiceUnavailable)
		return
	}
	var body struct {
		Version int `json:"version"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if body.Version < 1 {
		http.Error(w, "version must be a positive integer", http.StatusBadRequest)
		return
	}

	_, idx, err := a.configWatcher.Rollback(body.Version)
	if err != nil {
		a.log.Error("admin: config rollback failed", "error", err)
		http.Error(w, "rollback failed", http.StatusBadRequest)
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
		http.Error(w, "insufficient scope: need admin:read", http.StatusForbidden)
		return
	}
	id := r.PathValue("id")
	si, err := a.sm.Get(r.Context(), id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	snap, dbgOK := a.sm.DebugSnapshot(id)
	if !dbgOK {
		a.log.Warn("admin: failed to get debug snapshot", "session_id", id)
	}

	respondJSON(w, map[string]any{
		"session": si,
		"debug": map[string]any{
			"available":     dbgOK,
			"has_worker":    snap.HasWorker,
			"turn_count":    snap.TurnCount,
			"last_seq_sent": a.hub.NextSeqPeek(id),
			"worker_health": snap.WorkerHealth,
		},
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
// @Failure      503  {object}  ErrorResponse  "Restart not configured"
// @Router       /admin/restart [post]
func (a *AdminAPI) HandleRestart(w http.ResponseWriter, r *http.Request) {
	if !hasScope(r, ScopeAdminWrite) {
		http.Error(w, "insufficient scope: need admin:write", http.StatusForbidden)
		return
	}
	if a.restart == nil {
		http.Error(w, "restart is not configured", http.StatusServiceUnavailable)
		return
	}

	go func() {
		time.Sleep(500 * time.Millisecond)
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
		http.Error(w, "user_id already exists", http.StatusConflict)
		return
	}
	if errors.Is(err, sql.ErrNoRows) ||
		errors.Is(err, cron.ErrJobNotFound) ||
		errors.Is(err, session.ErrSessionNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	log.Error(op, "error", err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}
