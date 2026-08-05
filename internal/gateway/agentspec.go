package gateway

import (
	"context"
	"log/slog"
	"slices"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/hrygo/hotplex/internal/agentspec"
	"github.com/hrygo/hotplex/internal/observability"
	"github.com/hrygo/hotplex/internal/worker"
)

// BuildWebChatInput assembles the agentspec.Input for a webchat session-creation
// entry. It is the single shared "buildInput" used by both the WS init path and
// the REST create-session path (design spec §3.5, findings F4/F8): the two
// entries differ only in their INPUTS — WS carries initData.AllowedTools while
// REST has no AllowedTools source (nil) — so the WS≡REST equivalence reduces to
// "same semantic request → same Input → same AgentSpec" via the pure Resolver.
//
// workerType is the entry's already-resolved value (WS: initData.WorkerType;
// REST: body > query > workspace.WorkerPreference > default). Config is not
// needed: webchat worker_type is request-driven, not the config 5-level fallback
// (that applies only to messaging platforms).
func BuildWebChatInput(workerType worker.WorkerType, allowedTools []string, userID, workspaceID string) agentspec.Input {
	return agentspec.Input{
		InitMeta: agentspec.InitMetadata{
			WorkerType:   string(workerType),
			AllowedTools: allowedTools,
		},
		Platform:    platformWebChat,
		UserID:      userID,
		WorkspaceID: workspaceID,
	}
}

// ShadowCompareStartParams runs the agentspec normalization for a webchat entry
// and logs any divergence from the legacy SessionStartParams. It is purely
// OBSERVATIONAL in first-cut: it recovers from any panic and never influences
// the params actually used (the legacy construction stays authoritative — design
// spec §3.5, finding F8). It exists to prove WS≡REST equivalence in production
// ahead of switching the agentspec path to authoritative in a follow-up slice.
//
// Divergences are logged at two levels:
//   - resolve errors (e.g. an unknown worker_type the live WS path tolerates but
//     the resolver's boundary rejects) → Debug: an expected, explained divergence
//     reserved for a future validation-unification decision.
//   - field mismatches on the AgentSpec-owned StartParams fields (WorkerType,
//     AllowedTools) → Warn: a genuine surprise worth investigating.
func ShadowCompareStartParams(log *slog.Logger, in agentspec.Input, legacy worker.SessionStartParams) {
	if log == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			log.Debug("agentspec shadow: recovered panic", "recover", r)
		}
	}()

	spec, err := (agentspec.Resolver{}).Resolve(in)
	if err != nil {
		log.Debug("agentspec shadow: resolve divergence",
			"err", err, "legacy_worker_type", legacy.WorkerType)
		return
	}

	if worker.WorkerType(spec.Worker.Type) != legacy.WorkerType {
		log.Warn("agentspec shadow: worker_type divergence",
			"agentspec", spec.Worker.Type, "legacy", legacy.WorkerType)
	}
	if !slices.Equal(spec.Policy.AllowedTools, legacy.AllowedTools) {
		log.Warn("agentspec shadow: allowed_tools divergence",
			"agentspec", spec.Policy.AllowedTools, "legacy", legacy.AllowedTools)
	}
}

// ShadowResolvePlan runs the #946 EffectiveRuntimePlan resolution in shadow
// mode at the WS/REST entries. It is purely OBSERVATIONAL: it recovers from
// any panic, never influences the legacy SessionStartParams (which stay
// dispatch-authoritative), and only emits redacted diagnostics plus
// low-cardinality metrics (the plan hash is never a metric label — #946 spec
// §6.4). Blocked plans surface as bounded reason codes, never as silent
// success.
func ShadowResolvePlan(log *slog.Logger, in agentspec.Input) {
	if log == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			log.Debug("runtime plan shadow: recovered panic", "recover", r)
		}
	}()

	plan, err := (agentspec.Resolver{}).ResolvePlan(in)
	if err != nil {
		codes := make([]string, 0, len(plan.Blocked))
		for _, b := range plan.Blocked {
			codes = append(codes, b.Code)
			observability.RuntimePlanBlocked().Add(context.Background(), 1,
				metric.WithAttributes(attribute.String("code", b.Code)))
		}
		observability.RuntimePlanResolutions().Add(context.Background(), 1,
			metric.WithAttributes(attribute.String("result", "blocked")))
		log.Warn("runtime plan shadow: blocked",
			"platform", in.Platform, "blocked", codes)
		return
	}

	observability.RuntimePlanResolutions().Add(context.Background(), 1,
		metric.WithAttributes(attribute.String("result", "ok")))
	if len(plan.Warnings) > 0 {
		codes := make([]string, 0, len(plan.Warnings))
		for _, w := range plan.Warnings {
			codes = append(codes, w.Code)
		}
		log.Warn("runtime plan shadow: warnings",
			"platform", in.Platform, "plan_hash", plan.PlanHash, "warnings", codes)
	}
}
