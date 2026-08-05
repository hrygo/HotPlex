package admin

import (
	"errors"
	"net/http"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/hrygo/hotplex/internal/agentspec"
	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/observability"
	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/internal/web"
)

// RuntimePlanReport is the redacted wire shape of
// GET /admin/sessions/{id}/runtime-plan (#946 spec §6.6): the desired-state
// plan projection plus the observed bootstrap summary. It never carries
// prompts, secrets, full commands, or raw worker errors.
type RuntimePlanReport struct {
	Plan     agentspec.EffectiveRuntimePlanView `json:"plan"`
	Observed agentspec.ObservedSummary          `json:"observed"`
}

// HandleSessionRuntimePlan returns the redacted effective runtime plan for a
// session: plan hash, worker type, permission/sandbox summary, observed
// bootstrap state, warnings and blocked codes (#946 spec §6.6).
//
// Requires runtime:read AND session:read. The diagnostic projection is
// computed on demand from the session's persisted facts and the live config —
// no plan table, no second persisted truth (#946 spec §6.6).
//
// @Summary      Get session runtime plan
// @Description  Returns the redacted desired-state runtime plan and observed bootstrap summary for a session. Requires runtime:read and session:read scopes.
// @Tags         Admin API
// @Produce      json
// @Security     AdminBearerAuth
// @Param        id   path      string  true  "Session ID"
// @Success      200  {object}  RuntimePlanReport
// @Failure      400  {object}  ErrorResponse  "Missing session ID"
// @Failure      403  {object}  ErrorResponse  "Insufficient scope: need runtime:read and session:read"
// @Failure      404  {object}  ErrorResponse  "Session not found"
// @Failure      500  {object}  ErrorResponse  "Plan resolution failed"
// @Router       /admin/sessions/{id}/runtime-plan [get]
func (a *AdminAPI) HandleSessionRuntimePlan(w http.ResponseWriter, r *http.Request) {
	if !hasScope(r, ScopeRuntimeRead) {
		web.WriteAppError(w, http.StatusForbidden, "INSUFFICIENT_SCOPE", "insufficient scope: need runtime:read")
		return
	}
	if !hasScope(r, ScopeSessionRead) {
		web.WriteAppError(w, http.StatusForbidden, "INSUFFICIENT_SCOPE", "insufficient scope: need session:read")
		return
	}

	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		web.WriteAppError(w, http.StatusBadRequest, "INVALID_REQUEST", "session id is required")
		return
	}

	raw, err := a.sm.Get(r.Context(), id)
	if err != nil {
		if r.Context().Err() != nil {
			web.WriteAppError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "request cancelled")
			return
		}
		web.WriteAppError(w, http.StatusNotFound, "NOT_FOUND", "session not found")
		return
	}
	si, ok := raw.(*session.SessionInfo)
	if !ok || si == nil {
		web.WriteAppError(w, http.StatusNotFound, "NOT_FOUND", "session not found")
		return
	}

	var cfg *config.Config
	if a.cfg != nil {
		cfg = a.cfg.Get()
	}
	plan, err := buildSessionPlan(cfg, si)
	if err != nil && !errors.Is(err, agentspec.ErrPlanBlocked) {
		// A blocked plan is a valid diagnostic projection (its Blocked reasons
		// are the payload); any other failure is an internal error.
		a.log.Warn("admin: runtime plan resolution failed", "session_id", id, "err", err)
		web.WriteAppError(w, http.StatusInternalServerError, "INTERNAL", "failed to resolve runtime plan")
		return
	}

	observed := observedSummaryFor(si)
	observability.RuntimePlanObserved().Add(r.Context(), 1,
		metric.WithAttributes(attribute.String("state", observed.State)))

	respondJSON(w, RuntimePlanReport{Plan: plan.Redacted(), Observed: observed})
}

// buildSessionPlan reconstructs the desired-state plan from the session's
// persisted facts plus the live config snapshot. The session's dispatched
// worker type and tool whitelist enter as top-precedence init metadata; the
// config chain below them mirrors the original resolution. Pure — no I/O.
func buildSessionPlan(cfg *config.Config, si *session.SessionInfo) (agentspec.EffectiveRuntimePlan, error) {
	in := agentspec.Input{
		Cfg: cfg,
		InitMeta: agentspec.InitMetadata{
			WorkerType:   string(si.WorkerType),
			AllowedTools: si.AllowedTools,
		},
		BotName:     si.BotName,
		Platform:    si.Platform,
		PlatformKey: si.PlatformKey,
		UserID:      si.UserID,
		WorkspaceID: si.WorkspaceID,
	}
	return (agentspec.Resolver{}).ResolvePlan(in)
}

// observedSummaryFor maps the session's verifiable facts onto the observed
// bootstrap states (#946 spec §6.5). First slice: a Worker-reported
// permission ceiling is declared (not independently enforced → never
// "enforced"); an active session without a reported ceiling is unknown; a
// session with only the plan is planned.
func observedSummaryFor(si *session.SessionInfo) agentspec.ObservedSummary {
	observed := agentspec.ObservedSummary{WorkerType: string(si.WorkerType)}
	switch {
	case si.PermissionCeiling != "":
		observed.State = agentspec.ObservedDeclared
		observed.PermissionCeiling = si.PermissionCeiling
	case si.State.IsActive():
		observed.State = agentspec.ObservedUnknown
	default:
		observed.State = agentspec.ObservedPlanned
	}
	return observed
}
