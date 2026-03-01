package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/hrygo/hotplex/internal/telemetry"
)

// HealthHandler provides HTTP endpoints for health checking and metrics.
type HealthHandler struct {
	healthChecker *telemetry.HealthChecker
	metrics       *telemetry.Metrics
	startTime     time.Time
}

// NewHealthHandler creates a new HealthHandler.
func NewHealthHandler() *HealthHandler {
	return &HealthHandler{
		healthChecker: telemetry.GetHealthChecker(),
		metrics:       telemetry.GetMetrics(),
		startTime:     time.Now(),
	}
}

// HealthResponse represents the health check response.
type HealthResponse struct {
	Status    telemetry.HealthStatus `json:"status"`
	Timestamp string                 `json:"timestamp"`
	Uptime    string                 `json:"uptime"`
	Checks    map[string]bool        `json:"checks,omitempty"`
}

// ServeHTTP handles /health requests.
func (h *HealthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	status, checks := h.healthChecker.Check()

	resp := HealthResponse{
		Status:    status,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Uptime:    time.Since(h.startTime).String(),
		Checks:    checks,
	}

	w.Header().Set("Content-Type", "application/json")
	switch status {
	case telemetry.StatusUnhealthy:
		w.WriteHeader(http.StatusServiceUnavailable)
	case telemetry.StatusDegraded:
		w.WriteHeader(http.StatusPartialContent)
	default:
		w.WriteHeader(http.StatusOK)
	}

	_ = json.NewEncoder(w).Encode(resp)
}

// ReadyHandler handles /health/ready requests (Kubernetes readiness probe).
type ReadyHandler struct {
	engineReady func() bool
}

// NewReadyHandler creates a new ReadyHandler.
func NewReadyHandler(engineReady func() bool) *ReadyHandler {
	return &ReadyHandler{engineReady: engineReady}
}

func (h *ReadyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.engineReady == nil || !h.engineReady() {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status": "not_ready",
			"reason": "engine_not_initialized",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status": "ready",
	})
}

// LiveHandler handles /health/live requests (Kubernetes liveness probe).
type LiveHandler struct {
	startTime time.Time
}

// NewLiveHandler creates a new LiveHandler.
func NewLiveHandler() *LiveHandler {
	return &LiveHandler{startTime: time.Now()}
}

func (h *LiveHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "alive",
		"uptime":    time.Since(h.startTime).String(),
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}

// MetricsHandler handles /metrics requests (Prometheus format).
type MetricsHandler struct {
	metrics *telemetry.Metrics
}

// NewMetricsHandler creates a new MetricsHandler.
func NewMetricsHandler() *MetricsHandler {
	return &MetricsHandler{metrics: telemetry.GetMetrics()}
}

func (h *MetricsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	snapshot := h.metrics.Snapshot()

	// Prometheus text format
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")

	// Session metrics
	_, _ = fmt.Fprintf(w, "# HELP hotplex_sessions_active Number of currently active sessions\n")
	_, _ = fmt.Fprintf(w, "# TYPE hotplex_sessions_active gauge\n")
	_, _ = fmt.Fprintf(w, "hotplex_sessions_active %d\n", snapshot.SessionsActive)

	_, _ = fmt.Fprintf(w, "# HELP hotplex_sessions_total Total number of sessions created\n")
	_, _ = fmt.Fprintf(w, "# TYPE hotplex_sessions_total counter\n")
	_, _ = fmt.Fprintf(w, "hotplex_sessions_total %d\n", snapshot.SessionsTotal)

	_, _ = fmt.Fprintf(w, "# HELP hotplex_sessions_errors Total number of session errors\n")
	_, _ = fmt.Fprintf(w, "# TYPE hotplex_sessions_errors counter\n")
	_, _ = fmt.Fprintf(w, "hotplex_sessions_errors %d\n", snapshot.SessionsErrors)

	_, _ = fmt.Fprintf(w, "# HELP hotplex_tools_invoked Total number of tool invocations\n")
	_, _ = fmt.Fprintf(w, "# TYPE hotplex_tools_invoked counter\n")
	_, _ = fmt.Fprintf(w, "hotplex_tools_invoked %d\n", snapshot.ToolsInvoked)

	_, _ = fmt.Fprintf(w, "# HELP hotplex_dangers_blocked Total number of dangerous operations blocked\n")
	_, _ = fmt.Fprintf(w, "# TYPE hotplex_dangers_blocked counter\n")
	_, _ = fmt.Fprintf(w, "hotplex_dangers_blocked %d\n", snapshot.DangersBlocked)

	// Slack permission metrics
	_, _ = fmt.Fprintf(w, "# HELP hotplex_slack_permission_allowed Total number of Slack requests allowed\n")
	_, _ = fmt.Fprintf(w, "# TYPE hotplex_slack_permission_allowed counter\n")
	_, _ = fmt.Fprintf(w, "hotplex_slack_permission_allowed %d\n", snapshot.SlackPermissionAllowed)

	_, _ = fmt.Fprintf(w, "# HELP hotplex_slack_permission_blocked_user Total number of Slack requests blocked by user policy\n")
	_, _ = fmt.Fprintf(w, "# TYPE hotplex_slack_permission_blocked_user counter\n")
	_, _ = fmt.Fprintf(w, "hotplex_slack_permission_blocked_user %d\n", snapshot.SlackPermissionBlockedUser)

	_, _ = fmt.Fprintf(w, "# HELP hotplex_slack_permission_blocked_dm Total number of Slack requests blocked by DM policy\n")
	_, _ = fmt.Fprintf(w, "# TYPE hotplex_slack_permission_blocked_dm counter\n")
	_, _ = fmt.Fprintf(w, "hotplex_slack_permission_blocked_dm %d\n", snapshot.SlackPermissionBlockedDM)

	_, _ = fmt.Fprintf(w, "# HELP hotplex_slack_permission_blocked_mention Total number of Slack requests blocked by mention policy\n")
	_, _ = fmt.Fprintf(w, "# TYPE hotplex_slack_permission_blocked_mention counter\n")
	_, _ = fmt.Fprintf(w, "hotplex_slack_permission_blocked_mention %d\n", snapshot.SlackPermissionBlockedMention)
}
