package admin

// Operator fence endpoints (#877). Fenced executions block fresh input until
// an operator resolves or abandons them; both decisions are conditional on the
// fence_version fencing token, so concurrent operators (or a gateway restart
// between inspect and action) conflict with 409 instead of double-migrating.
//
// Security contract:
//   - actor comes from the auth context (ActorFromRequest), never the body
//   - request bodies carry only decision, fence version, and bounded
//     reason/evidence_ref text; prompts, payload content, and credentials are
//     never accepted, stored, or echoed here
//   - responses expose the secret-free FenceListItem projection only

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/hrygo/hotplex/internal/audit"
	"github.com/hrygo/hotplex/internal/execution"
	"github.com/hrygo/hotplex/internal/observability"
	"github.com/hrygo/hotplex/internal/web"
)

const (
	// fenceListDefaultLimit / fenceListMaxLimit bound the operator fence list
	// (spec §5.3): default 100, hard cap 500 — tighter than the generic
	// web.ParsePagination cap because fenced rows are rare by construction.
	fenceListDefaultLimit = 100
	fenceListMaxLimit     = 500

	fenceReasonMaxLen   = 512 // operator justification, bounded audit detail
	fenceEvidenceMaxLen = 256 // pointer (ticket/run ref), never inline content
)

// FenceListItem is the wire shape of a fenced execution. Deliberately narrow:
// no payload hash, no error detail, nothing derived from user content.
type FenceListItem struct {
	ExecutionID    string `json:"execution_id"`
	SessionID      string `json:"session_id"`
	DeliveryStatus string `json:"delivery_status"`
	RuntimeStatus  string `json:"runtime_status"`
	FenceReason    string `json:"fence_reason"`
	FenceVersion   int64  `json:"fence_version"`
	FenceCreatedAt *int64 `json:"fence_created_at,omitempty"`
	UpdatedAt      int64  `json:"updated_at"`
}

func fenceListItem(rec *execution.Record) FenceListItem {
	return FenceListItem{
		ExecutionID:    rec.ExecutionID,
		SessionID:      rec.SessionID,
		DeliveryStatus: string(rec.Status),
		RuntimeStatus:  string(rec.RuntimeStatus),
		FenceReason:    rec.FenceReason,
		FenceVersion:   rec.FenceVersion,
		FenceCreatedAt: rec.FenceCreatedAt,
		UpdatedAt:      rec.UpdatedAt,
	}
}

func parseFencePagination(r *http.Request) (limit, offset int) {
	limit = fenceListDefaultLimit
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 {
		limit = l
	}
	if limit > fenceListMaxLimit {
		limit = fenceListMaxLimit
	}
	if o, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && o >= 0 {
		offset = o
	}
	return limit, offset
}

// HandleListFences lists currently fenced executions for operator inspection.
//
// @Summary      List fenced executions
// @Description  Lists executions whose runtime ended ambiguous and raised a fence. Requires runtime:read scope.
// @Tags         Admin API
// @Produce      json
// @Security     AdminBearerAuth
// @Param        session_id  query    string  false  "Filter by session ID"
// @Param        limit       query    int     false  "Max results (default 100, max 500)"
// @Param        offset      query    int     false  "Pagination offset"
// @Success      200  {object}  map[string]any  "fences list"
// @Failure      403  {object}  ErrorResponse  "Insufficient scope: need runtime:read"
// @Failure      503  {object}  ErrorResponse  "Execution store not configured"
// @Router       /admin/executions/fences [get]
func (a *AdminAPI) HandleListFences(w http.ResponseWriter, r *http.Request) {
	if !hasScope(r, ScopeRuntimeRead) {
		web.WriteAppError(w, http.StatusForbidden, "INSUFFICIENT_SCOPE", "insufficient scope: need runtime:read")
		return
	}
	if a.runtimeExec == nil {
		web.WriteAppError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "execution store not configured")
		return
	}

	sessionID := strings.TrimSpace(r.URL.Query().Get("session_id"))
	limit, offset := parseFencePagination(r)

	records, err := a.runtimeExec.ListFences(r.Context(), sessionID, limit, offset)
	if err != nil {
		a.log.Error("admin: list fences", "err", err)
		web.WriteAppError(w, http.StatusInternalServerError, "INTERNAL", "failed to list fences")
		return
	}

	items := make([]FenceListItem, 0, len(records))
	for _, rec := range records {
		items = append(items, fenceListItem(rec))
	}
	respondJSON(w, map[string]any{
		"fences": items,
		"limit":  limit,
		"offset": offset,
	})
}

// fenceActionBody is the operator decision payload. Bounded free-text fields
// only; the store never persists them (Admin audit layer only).
type fenceActionBody struct {
	Decision             string `json:"decision"`
	ExpectedFenceVersion *int64 `json:"expected_fence_version"`
	Reason               string `json:"reason"`
	EvidenceRef          string `json:"evidence_ref"`
}

// HandleFenceAction applies resolve/abandon to a fenced execution.
//
// @Summary      Apply operator fence action
// @Description  Resolves (clears fence, keeps runtime unknown) or abandons (clears fence, fails runtime with OPERATOR_ABANDONED) a fenced execution. Conditional on expected_fence_version; conflicts return 409. Requires runtime:write scope.
// @Tags         Admin API
// @Accept       json
// @Produce      json
// @Security     AdminBearerAuth
// @Param        id    path   string           true  "Execution ID"
// @Param        body  body   fenceActionBody  true  "Operator decision"
// @Success      200   {object}  map[string]any  "Applied decision and updated execution fact"
// @Failure      400   {object}  ErrorResponse  "Invalid decision, fence version, reason, or evidence_ref"
// @Failure      403   {object}  ErrorResponse  "Insufficient scope: need runtime:write"
// @Failure      404   {object}  ErrorResponse  "Execution not found"
// @Failure      409   {object}  ErrorResponse  "Fence version conflict: re-inspect before retrying"
// @Failure      503   {object}  ErrorResponse  "Execution store not configured or timed out"
// @Router       /admin/executions/{id}/fence-action [post]
func (a *AdminAPI) HandleFenceAction(w http.ResponseWriter, r *http.Request) {
	if !hasScope(r, ScopeRuntimeWrite) {
		web.WriteAppError(w, http.StatusForbidden, "INSUFFICIENT_SCOPE", "insufficient scope: need runtime:write")
		return
	}
	if a.runtimeExec == nil {
		web.WriteAppError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "execution store not configured")
		return
	}

	executionID := strings.TrimSpace(r.PathValue("id"))
	if executionID == "" {
		web.WriteAppError(w, http.StatusBadRequest, "BAD_REQUEST", "execution id is required")
		return
	}

	var body fenceActionBody
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&body); err != nil {
		web.WriteAppError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body")
		return
	}

	decision := execution.FenceDecision(body.Decision)
	if decision != execution.FenceDecisionResolve && decision != execution.FenceDecisionAbandon {
		web.WriteAppError(w, http.StatusBadRequest, "BAD_REQUEST", "decision must be resolve or abandon")
		return
	}
	if body.ExpectedFenceVersion == nil || *body.ExpectedFenceVersion < 0 {
		web.WriteAppError(w, http.StatusBadRequest, "BAD_REQUEST", "expected_fence_version is required and must be >= 0")
		return
	}
	reason := strings.TrimSpace(body.Reason)
	if reason == "" || len(reason) > fenceReasonMaxLen {
		web.WriteAppError(w, http.StatusBadRequest, "BAD_REQUEST", "reason must be 1-512 characters")
		return
	}
	evidenceRef := strings.TrimSpace(body.EvidenceRef)
	if len(evidenceRef) > fenceEvidenceMaxLen {
		web.WriteAppError(w, http.StatusBadRequest, "BAD_REQUEST", "evidence_ref must be at most 256 characters")
		return
	}

	updated, err := a.runtimeExec.ApplyFenceDecision(r.Context(), execution.FenceActionRequest{
		ExecutionID:          executionID,
		ExpectedFenceVersion: *body.ExpectedFenceVersion,
		Decision:             decision,
	})
	a.auditFenceAction(r, executionID, decision, *body.ExpectedFenceVersion, reason, evidenceRef, err)
	if err != nil {
		a.writeFenceActionError(w, r, decision, err)
		return
	}

	observability.RuntimeFenceActions().Add(r.Context(), 1, metric.WithAttributes(
		attribute.String("decision", string(decision)),
		attribute.String("result", "ok"),
	))
	a.log.Info("admin: fence action applied",
		"decision", string(decision),
		"execution_id", executionID,
		"session_id", updated.SessionID,
		"fence_version", updated.FenceVersion,
		"actor", ActorFromRequest(r),
	)

	// Abandon emits the additive runtime.execution.failed event so connected
	// clients observe the terminal state. Best-effort: the store write is
	// already durable, and fenced sessions usually have no live connections.
	if decision == execution.FenceDecisionAbandon && a.runtimeNotifier != nil {
		a.runtimeNotifier.NotifyExecutionAbandoned(r.Context(), updated.SessionID, updated.ExecutionID)
	}

	respondJSON(w, map[string]any{
		"decision":  string(decision),
		"execution": fenceListItem(updated),
	})
}

// writeFenceActionError maps store errors to operator-actionable statuses and
// records the low-cardinality outcome metrics (#877 spec §11).
func (a *AdminAPI) writeFenceActionError(w http.ResponseWriter, r *http.Request, decision execution.FenceDecision, err error) {
	status := http.StatusInternalServerError
	code := "INTERNAL"
	msg := "failed to apply fence action"
	result := "error"
	switch {
	case errors.Is(err, execution.ErrNotFound):
		status, code, msg = http.StatusNotFound, "FENCE_NOT_FOUND", "execution not found"
	case errors.Is(err, execution.ErrFenceConflict):
		status, code, msg = http.StatusConflict, "FENCE_CONFLICT",
			"fence version conflict: re-inspect the fence and retry deliberately"
		result = "conflict"
	case errors.Is(err, context.DeadlineExceeded):
		status, code, msg = http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "execution store timeout"
	}

	observability.RuntimeFenceActions().Add(r.Context(), 1, metric.WithAttributes(
		attribute.String("decision", string(decision)),
		attribute.String("result", result),
	))
	if result == "conflict" {
		observability.RuntimeFenceConflicts().Add(r.Context(), 1)
	}
	if status >= 500 {
		a.log.Error("admin: fence action failed", "err", err, "decision", string(decision))
	} else {
		a.log.Warn("admin: fence action rejected", "err", err, "decision", string(decision))
	}
	web.WriteAppError(w, status, code, msg)
}

// auditFenceAction records the operator decision into the tamper-evident
// user_activity table with bounded rationale detail (#877). The Middleware
// defer already emits the decision-agnostic admin_audit slog line
// (runtime.fence.action); this row carries the specific action plus
// reason/evidence_ref, which never reach the execution store.
func (a *AdminAPI) auditFenceAction(r *http.Request, executionID string, decision execution.FenceDecision, expectedVersion int64, reason, evidenceRef string, applyErr error) {
	if a.auditCollector == nil {
		return
	}
	action := AuditRuntimeFenceResolve
	if decision == execution.FenceDecisionAbandon {
		action = AuditRuntimeFenceAbandon
	}
	outcome := audit.OutcomeSuccess
	result := "ok"
	if applyErr != nil {
		outcome = audit.OutcomeFailure
		result = "rejected"
	}
	detail, _ := json.Marshal(map[string]any{
		"execution_id":           executionID,
		"expected_fence_version": expectedVersion,
		"reason":                 reason,
		"evidence_ref":           evidenceRef,
		"result":                 result,
	})
	userID, userIDType := actorIdentity(ActorFromRequest(r))
	_ = a.auditCollector.Enqueue(context.Background(), &audit.UserActivity{
		Ts:           time.Now().UnixMilli(),
		UserID:       userID,
		UserIDType:   userIDType,
		Platform:     audit.PlatformAdmin,
		Action:       "admin." + action,
		ResourceType: "runtime",
		Outcome:      outcome,
		DetailJSON:   string(detail),
		IP:           clientIP(r),
		UserAgent:    r.UserAgent(),
	})
}
