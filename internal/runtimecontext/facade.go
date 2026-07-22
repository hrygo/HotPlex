package runtimecontext

import (
	"context"
	"log/slog"
	"time"

	"github.com/hrygo/hotplex/internal/agentspec"
)

// Provider adapter interfaces — the boundary that keeps the facade decoupled
// from store internals. Concrete adapters (adapters.go) wrap the real stores
// (session.Store, eventstore.EventStore / TurnQuerier, workspace store) and
// project their types into the facade's own value types. The facade depends
// only on these interfaces, so a future memory backend can be dropped in by
// implementing the matching adapter — no facade change required (AC: "future
// memory backends addable behind interface", "adapters don't leak private
// structures").

// SessionReader provides the REQUIRED session-level runtime context. A failure
// here is fatal to Load (there is nothing to aggregate without a session).
type SessionReader interface {
	Session(ctx context.Context, sessionID string) (*SessionContext, error)
}

// EventReader provides recent persisted events (best-effort enrichment).
type EventReader interface {
	RecentEvents(ctx context.Context, sessionID string, limit int) ([]EventSummary, error)
}

// TurnReader provides recent materialized turns and aggregated stats
// (best-effort enrichment).
type TurnReader interface {
	RecentTurns(ctx context.Context, sessionID string, limit int) ([]TurnSummary, error)
	TurnStats(ctx context.Context, sessionID string) (*TurnStatSummary, error)
}

// WorkspaceReader provides workspace agent-config metadata (best-effort
// enrichment; a platform/cron session has no workspace).
type WorkspaceReader interface {
	Workspace(ctx context.Context, workspaceID string) (*WorkspaceContext, error)
}

// Facade is the read-only RuntimeContext implementation: a thin aggregator that
// composes provider adapters behind stable interfaces. It performs no writes
// and changes no semantics — eventstore and turns remain authoritative.
type Facade struct {
	sessions   SessionReader
	events     EventReader
	turns      TurnReader
	workspaces WorkspaceReader
	log        *slog.Logger
}

// Compile-time assertion that Facade satisfies the read-only contract.
var _ Loader = (*Facade)(nil)

// NewFacade builds a Facade over the four provider adapters. log defaults to
// slog.Default when nil. Any adapter may be nil: a nil adapter simply means that
// source is never loaded (Load skips it), which lets a caller assemble a
// partial facade for tests or for deployments without an eventstore.
func NewFacade(sessions SessionReader, events EventReader, turns TurnReader, workspaces WorkspaceReader, log *slog.Logger) *Facade {
	if log == nil {
		log = slog.Default()
	}
	return &Facade{
		sessions:   sessions,
		events:     events,
		turns:      turns,
		workspaces: workspaces,
		log:        log,
	}
}

// Load aggregates a session's runtime context from the configured sources. The
// session source is required and authoritative for the snapshot's existence: if
// it is missing or errors, Load returns that error (callers cannot fabricate a
// context for an unknown session). All other sources are best-effort
// enrichment — a missing workspace, empty turns, or a transient eventstore error
// is logged and leaves the corresponding field at its zero value; Load still
// returns the session context. This guarantees resume/fork/history behavior is
// unchanged: Load observes, never mutates, never blocks recovery on optional
// telemetry.
func (f *Facade) Load(ctx context.Context, sessionID string, opts ContextLoadOptions) (*ContextSnapshot, error) {
	if sessionID == "" {
		return nil, ErrEmptySessionID
	}
	if f.sessions == nil {
		// Without a session source there is no authoritative context to return;
		// surface a programmer error rather than a misleading empty snapshot.
		return nil, errNoSessionReader
	}

	sess, err := f.sessions.Session(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if sess == nil {
		// Treat a nil-but-no-error session projection as not found.
		return nil, errSessionNotFound
	}

	snap := &ContextSnapshot{
		SessionID: sessionID,
		Session:   *sess,
		LoadedAt:  time.Now().UTC().UnixMilli(),
	}

	// Workspace: only when the session references one and the caller didn't opt
	// out. A missing workspace record is expected for platform/cron sessions and
	// for a stale WorkspaceID, so it is logged at debug and never fatal.
	if !opts.SkipWorkspace && f.workspaces != nil && sess.WorkspaceID != "" {
		ws, wsErr := f.workspaces.Workspace(ctx, sess.WorkspaceID)
		if wsErr != nil {
			f.log.Debug("runtimecontext: workspace lookup failed (best-effort, ignored)",
				"session_id", sessionID, "workspace_id", sess.WorkspaceID, "err", wsErr)
		} else {
			snap.Workspace = ws
		}
	}

	// Turns: best-effort. Negative limit skips. Zero → default.
	if f.turns != nil && opts.MaxTurns >= 0 {
		limit := opts.MaxTurns
		if limit == 0 {
			limit = DefaultMaxTurns
		}
		turns, tErr := f.turns.RecentTurns(ctx, sessionID, limit)
		if tErr != nil {
			f.log.Debug("runtimecontext: turns lookup failed (best-effort, ignored)",
				"session_id", sessionID, "err", tErr)
		} else {
			snap.Turns = turns
		}
	}

	// Stats: best-effort. Only meaningful when turns exist; skip is explicit.
	if f.turns != nil && !opts.SkipStats {
		stats, sErr := f.turns.TurnStats(ctx, sessionID)
		if sErr != nil {
			f.log.Debug("runtimecontext: turn stats lookup failed (best-effort, ignored)",
				"session_id", sessionID, "err", sErr)
		} else {
			snap.TurnStats = stats
		}
	}

	// Events: best-effort. Negative limit skips. Zero → default.
	if f.events != nil && opts.MaxEvents >= 0 {
		limit := opts.MaxEvents
		if limit == 0 {
			limit = DefaultMaxEvents
		}
		events, eErr := f.events.RecentEvents(ctx, sessionID, limit)
		if eErr != nil {
			f.log.Debug("runtimecontext: events lookup failed (best-effort, ignored)",
				"session_id", sessionID, "err", eErr)
		} else {
			snap.Events = events
		}
	}

	return snap, nil
}

// errNoSessionReader / errSessionNotFound are internal sentinels. They are not
// exported because the first indicates a wiring bug and the second mirrors a
// store not-found that the adapter is expected to translate; callers compare on
// the underlying store's sentinel (e.g. session.ErrSessionNotFound) via the
// returned error chain where applicable.
var (
	errNoSessionReader = runtimeContextError("runtimecontext: no session reader configured")
	errSessionNotFound = runtimeContextError("runtimecontext: session not found")
)

// EffectiveIdentity is a convenience helper exposed for adapter authors and
// tests: it returns the identity carried by a snapshot's session projection.
// Mirrors session.SessionInfo.EffectiveIdentity so a snapshot consumer does not
// need to reach back into the session package.
func (s *ContextSnapshot) EffectiveIdentity() agentspec.AgentIdentity {
	return s.Session.Identity
}
