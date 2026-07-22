// Package runtimecontext provides the read-only RuntimeContext facade (#852).
//
// It is a thin aggregator that unifies four authoritative data sources behind
// stable provider-adapter interfaces, so callers can read a session's runtime
// context without depending on store internals:
//
//   - eventstore raw events (eventstore.EventStore)
//   - materialized turns (eventstore.TurnQuerier)
//   - the worker's own session id + identity + effective spec (session.Store)
//   - workspace agent-config metadata (session workspace store)
//
// The facade performs NO writes and changes NO semantics: eventstore and turns
// remain the source of truth. A future slice adds Save (persist context
// updates) per docs/v2/API-DESIGN.md §Runtime Context API; the first cut is
// intentionally read-only (issue #852: "Implement read-only RuntimeContext.Load
// facade first").
package runtimecontext

import (
	"context"

	"github.com/hrygo/hotplex/internal/agentspec"
)

// ErrEmptySessionID is returned by Load when the caller passes an empty
// session id. Surfaced as a sentinel so callers can distinguish a programmer
// error from a not-found / store error.
const ErrEmptySessionID runtimeContextError = "runtimecontext: empty session id"

// runtimeContextError is a string-backed error type so the package's sentinels
// can be compared with errors.Is while remaining allocation-free and avoiding
// fmt.Errorf wrapping at the declaration site.
type runtimeContextError string

func (e runtimeContextError) Error() string { return string(e) }

// ContextSnapshot is the read-only, aggregated runtime context for a single
// session, produced by Load. It is a pure projection: every field is derived
// from one of the four authoritative sources, nothing is synthesized or
// mutated. Secret-free — it carries no tokens, credentials, or prompt bodies
// (the same invariant as agentspec.AgentIdentity / EffectiveAgentSpecSnapshot).
//
// Optional sources (workspace, turns, stats, events) appear as their zero value
// (nil / empty) when the session has none or when the caller skipped them via
// ContextLoadOptions; their absence is never an error. Only a failure to read
// the required session source is reported.
type ContextSnapshot struct {
	SessionID string
	// Session is the REQUIRED projection of session-level runtime context
	// (identity, worker session id, effective spec, work dir). Always populated
	// on a successful Load.
	Session SessionContext
	// Workspace is the projection of workspace agent-config metadata. Nil when
	// the session has no workspace (platform/cron sessions) or was skipped.
	Workspace *WorkspaceContext
	// Turns are the most-recent N materialized turns (oldest→newest). Empty
	// when the session has no turns or turns were skipped.
	Turns []TurnSummary
	// TurnStats is the aggregated turn statistics. Nil when there are no turns,
	// stats were skipped, or the underlying source returned none.
	TurnStats *TurnStatSummary
	// Events are the most-recent N persisted events (oldest→newest). Empty when
	// the session has no events or events were skipped. Deliberately a summary
	// projection (no raw Data payload) — callers needing raw event bodies read
	// the eventstore directly; the facade stays bounded and secret-conscious.
	Events []EventSummary
	// LoadedAt is the unix-millis timestamp the snapshot was assembled (UTC).
	// Lets a consumer detect staleness without a wall-clock of its own.
	LoadedAt int64
}

// SessionContext is the projection of session-level runtime context. It reuses
// the agentspec value objects (AgentIdentity #848, EffectiveAgentSpecSnapshot
// #866) because those are already public, secret-free domain types — reusing
// them avoids duplication; they are not store-internal "private provider
// structures" the facade must hide.
type SessionContext struct {
	// WorkerSessionID is the session id used by the worker runtime itself
	// (sessions.worker_session_id). Empty for workers whose id IS the gateway id
	// (Claude Code via --session-id); populated for workers that self-generate
	// (OpenCode Server).
	WorkerSessionID string
	// WorkDir is the working directory bound to the session.
	WorkDir string
	// WorkspaceID is the multitenancy anchor. Empty for platform/cron sessions.
	WorkspaceID string
	// WorkerType is the normalized worker type (claude_code|opencode_server|
	// codex_cli|acp). Low cardinality — label-safe.
	WorkerType string
	// Platform is the originating platform (webchat|feishu|slack|yuanxin|cron),
	// empty for direct WS.
	Platform string
	// State is the session lifecycle state (created|running|idle|terminated).
	State string
	// Identity is the bound agent identity, always populated (derived for legacy
	// sessions via EffectiveIdentity so they still correlate). Check Identity
	// .Anonymous to detect unauthenticated sessions.
	Identity agentspec.AgentIdentity
	// SpecSnapshot is the effective AgentSpec snapshot that governed the session
	// at start time. Nil for sessions with no tool whitelist or created before
	// #866 (legacy fallback). On resume it is the source of truth for the
	// effective policy.
	SpecSnapshot *agentspec.EffectiveAgentSpecSnapshot
}

// WorkspaceContext is the projection of workspace agent-config metadata — the
// "workspace agent config overrides" source in the API-DESIGN. The facade does
// not interpret AgentConfigOverrides; it surfaces the raw JSON so a consumer
// can resolve it as it sees fit.
type WorkspaceContext struct {
	ID                   string
	Name                 string
	WorkDir              string
	PermissionMode       string // read-only|workspace|auto-edit|bypass; "" = worker default
	WorkerPreference     string
	AgentConfigOverrides string // raw JSON value (spec ②); facade does not interpret
}

// TurnSummary is the facade's read-only projection of a materialized turn. It
// carries the fields useful for a context view (ordering, cost, success,
// tokens) without the full content body — large/sensitive turn content stays in
// the eventstore.
type TurnSummary struct {
	TurnNum    int
	Generation int64
	Role       string
	Model      string
	Success    *bool
	Source     string
	TokensIn   int64
	TokensOut  int64
	DurationMs int64
	CostUSD    float64
	CreatedAt  int64
}

// TurnStatSummary is the aggregated turn statistics for a session, projected
// from the materialized turns table.
type TurnStatSummary struct {
	Generation   int64
	TotalTurns   int
	SuccessTurns int
	FailedTurns  int
	TotalDurMs   int64
	TotalCostUSD float64
	TotalTokIn   int64
	TotalTokOut  int64
}

// EventSummary is the facade's read-only projection of a persisted AEP event —
// ordering and provenance only, no data payload.
type EventSummary struct {
	Seq       int64
	Type      string
	Direction string
	Source    string
	CreatedAt int64
}

// ContextLoadOptions controls how much context Load aggregates. The zero value
// loads everything with sensible defaults (20 turns, 50 events, stats + workspace
// included when present). Use the Skip* fields or negative limits to opt OUT of
// a source — Load never errors on an absent or skipped optional source.
type ContextLoadOptions struct {
	// MaxTurns caps the number of recent turns. Zero means a default of 20. A
	// negative value skips turns entirely.
	MaxTurns int
	// MaxEvents caps the number of recent events. Zero means a default of 50. A
	// negative value skips events entirely.
	MaxEvents int
	// SkipStats, when true, omits aggregated turn statistics.
	SkipStats bool
	// SkipWorkspace, when true, omits workspace metadata even when the session
	// references a workspace.
	SkipWorkspace bool
}

// DefaultMaxTurns / DefaultMaxEvents are the limits Load applies when the
// corresponding option is zero. Exported so tests and callers can reference the
// exact boundary.
const (
	DefaultMaxTurns  = 20
	DefaultMaxEvents = 50

	// MaxAllowedTurns / MaxAllowedEvents cap a caller-supplied limit so a
	// misconfigured huge MaxTurns/MaxEvents cannot turn Load into an unbounded
	// scan (resource bound). Silently capped, not rejected: Load is a
	// best-effort observer that never errors on an optional source, and a capped
	// limit still returns the most-recent N. The caps are 50×/100× the defaults
	// — generous for any diagnostics/history view.
	MaxAllowedTurns  = 1000
	MaxAllowedEvents = 5000
)

// ContextUpdate is reserved for a future read-write slice of the runtime-context
// facade. It carries the context deltas a future Save would persist — WITHOUT
// changing eventstore/turns semantics. Defined now so callers and docs can
// reference the target shape (API-DESIGN §Runtime Context API shows the full
// RuntimeContext interface with both Load and Save). No first-cut code reads or
// writes it.
type ContextUpdate struct {
	// Metadata is free-form, secret-free context to merge into the session's
	// runtime context view (never into eventstore/turns). Reserved.
	Metadata map[string]any
}

// Loader is the read-only runtime-context contract (issue #852 first cut). The
// facade aggregates the four authoritative sources behind provider adapters,
// without changing the semantics of any underlying store. A future slice
// extends this into the full RuntimeContext interface (Load + Save).
type Loader interface {
	Load(ctx context.Context, sessionID string, opts ContextLoadOptions) (*ContextSnapshot, error)
}
