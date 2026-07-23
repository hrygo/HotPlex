package runtimecontext

import (
	"context"
	"errors"

	"github.com/hrygo/hotplex/internal/eventstore"
	"github.com/hrygo/hotplex/internal/session"
)

// This file holds the concrete provider adapters — the ONLY place the facade
// touches store types. Each adapter wraps a real store behind one of the
// facade's provider interfaces (facade.go), projecting store structs into the
// facade's own value types. Callers depend on the facade interfaces, never on
// the store types, so the boundary isolates store internals (AC: "adapters
// don't leak private provider structures"). A future memory backend replaces an
// adapter by implementing the same interface — no facade change required.

// --- session ---

// SessionStore is the minimal session-store capability the session adapter
// needs. session.Store (and both concrete stores) satisfy it structurally.
type SessionStore interface {
	Get(ctx context.Context, id string) (*session.SessionInfo, error)
}

// sessionReader adapts a SessionStore to SessionReader.
type sessionReader struct{ store SessionStore }

// NewSessionReader wraps a session store as a SessionReader. Identity is
// projected via SessionInfo.EffectiveIdentity (deterministic for legacy rows),
// and the effective AgentSpec snapshot (#866) is forwarded as-is.
func NewSessionReader(s SessionStore) SessionReader {
	return &sessionReader{store: s}
}

func (a *sessionReader) Session(ctx context.Context, sessionID string) (*SessionContext, error) {
	info, err := a.store.Get(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if info == nil {
		return nil, nil
	}
	return &SessionContext{
		WorkerSessionID: info.WorkerSessionID,
		WorkDir:         info.WorkDir,
		WorkspaceID:     info.WorkspaceID,
		WorkerType:      string(info.WorkerType),
		Platform:        info.Platform,
		State:           string(info.State),
		Identity:        info.EffectiveIdentity(),
		SpecSnapshot:    info.SpecSnapshot,
	}, nil
}

// --- workspace ---

// WorkspaceStore is the minimal workspace-store capability the workspace
// adapter needs. session.UserWorkspaceStore (and both concrete stores) satisfy
// it structurally.
type WorkspaceStore interface {
	GetWorkspaceByID(ctx context.Context, id string) (*session.Workspace, error)
}

// workspaceReader adapts a WorkspaceStore to WorkspaceReader.
type workspaceReader struct{ store WorkspaceStore }

// NewWorkspaceReader wraps a workspace store as a WorkspaceReader.
func NewWorkspaceReader(s WorkspaceStore) WorkspaceReader {
	return &workspaceReader{store: s}
}

func (a *workspaceReader) Workspace(ctx context.Context, workspaceID string) (*WorkspaceContext, error) {
	ws, err := a.store.GetWorkspaceByID(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	if ws == nil {
		return nil, nil
	}
	return &WorkspaceContext{
		ID:                   ws.ID,
		Name:                 ws.Name,
		WorkDir:              ws.WorkDir,
		PermissionMode:       ws.PermissionMode,
		WorkerPreference:     ws.WorkerPreference,
		AgentConfigOverrides: ws.AgentConfigOverrides,
	}, nil
}

// --- events ---

// EventStore is the minimal event-store capability the event adapter needs.
// eventstore.EventStore (and both concrete stores) satisfy it structurally.
type EventStore interface {
	QueryBySession(ctx context.Context, sessionID string, cursor int64, dir eventstore.CursorDirection, limit int) (*eventstore.EventPage, error)
}

// eventReader adapts an EventStore to EventReader, fetching the most-recent N
// events via the CursorLatest path. QueryBySession returns events in insertion
// id ASC order (the store reverses the DESC SQL in Go), so the projection is
// already oldest→newest.
type eventReader struct{ store EventStore }

// NewEventReader wraps an eventstore as an EventReader.
func NewEventReader(s EventStore) EventReader {
	return &eventReader{store: s}
}

func (a *eventReader) RecentEvents(ctx context.Context, sessionID string, limit int) ([]EventSummary, error) {
	if limit <= 0 {
		return nil, nil
	}
	page, err := a.store.QueryBySession(ctx, sessionID, 0, eventstore.CursorLatest, limit)
	if err != nil {
		// A session with no persisted events is normal (e.g. before the first
		// turn), not an error. The store surfaces this as ErrNotFound; the
		// facade's contract is "empty when none", so swallow it.
		if errors.Is(err, eventstore.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	if page == nil || len(page.Events) == 0 {
		return nil, nil
	}
	out := make([]EventSummary, 0, len(page.Events))
	for _, e := range page.Events {
		out = append(out, EventSummary{
			Seq:       e.Seq,
			Type:      e.Type,
			Direction: e.Direction,
			Source:    e.Source,
			CreatedAt: e.CreatedAt,
		})
	}
	return out, nil
}

// --- turns ---

// TurnStore is the minimal turn-store capability the turn adapter needs.
// eventstore.TurnQuerier (and both concrete stores) satisfy it structurally.
type TurnStore interface {
	QueryLatestTurns(ctx context.Context, sessionID string, limit int) ([]*eventstore.TurnRecord, error)
	QueryTurnStats(ctx context.Context, sessionID string) (*eventstore.TurnStats, error)
}

// turnReader adapts a TurnStore to TurnReader. QueryLatestTurns returns the
// newest generation's turns in id ASC order (reversed in Go), oldest→newest.
type turnReader struct{ store TurnStore }

// NewTurnReader wraps a turn querier as a TurnReader.
func NewTurnReader(s TurnStore) TurnReader {
	return &turnReader{store: s}
}

func (a *turnReader) RecentTurns(ctx context.Context, sessionID string, limit int) ([]TurnSummary, error) {
	if limit <= 0 {
		return nil, nil
	}
	records, err := a.store.QueryLatestTurns(ctx, sessionID, limit)
	if err != nil {
		// No turns yet is normal, not an error (mirrors RecentEvents).
		if errors.Is(err, eventstore.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	if len(records) == 0 {
		return nil, nil
	}
	out := make([]TurnSummary, 0, len(records))
	for _, r := range records {
		out = append(out, TurnSummary{
			TurnNum:    r.TurnNum,
			Generation: r.Generation,
			Role:       r.Role,
			Model:      r.Model,
			Success:    r.Success,
			Source:     r.Source,
			TokensIn:   r.TokensIn,
			TokensOut:  r.TokensOut,
			DurationMs: r.DurationMs,
			CostUSD:    r.CostUSD,
			CreatedAt:  r.CreatedAt,
		})
	}
	return out, nil
}

func (a *turnReader) TurnStats(ctx context.Context, sessionID string) (*TurnStatSummary, error) {
	stats, err := a.store.QueryTurnStats(ctx, sessionID)
	if err != nil {
		// No turns ⇒ no stats. Normal, not an error (mirrors RecentEvents).
		if errors.Is(err, eventstore.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	if stats == nil {
		return nil, nil
	}
	return &TurnStatSummary{
		Generation:   stats.Generation,
		TotalTurns:   stats.TotalTurns,
		SuccessTurns: stats.SuccessTurns,
		FailedTurns:  stats.FailedTurns,
		TotalDurMs:   stats.TotalDurMs,
		TotalCostUSD: stats.TotalCostUSD,
		TotalTokIn:   stats.TotalTokIn,
		TotalTokOut:  stats.TotalTokOut,
	}, nil
}
