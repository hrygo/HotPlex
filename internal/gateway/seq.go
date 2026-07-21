package gateway

import (
	"context"
	"sync"
	"sync/atomic"
)

// SeqHydrator reads the latest persisted event seq for a session, so the Hub
// can seed the in-memory SeqGen on reconnect (issue #879). Both
// *eventstore.SQLiteStore and *eventstore.pgStore satisfy it.
type SeqHydrator interface {
	LatestSeq(ctx context.Context, sessionID string) (int64, error)
}

// SeqFlusher drains pending collector writes for a session so LatestSeq is
// accurate before SeqGen hydration. Set by gateway_run.go when the collector
// is available; nil when eventstore is disabled.
type SeqFlusher interface {
	FlushSession(sessionID string) error
}

// SeqGen generates monotonically increasing sequence numbers per session.
// Uses sync.Map + per-session atomic.Int64 to eliminate cross-session contention.
type SeqGen struct {
	seq      sync.Map // sessionID → *atomic.Int64
	hydrated sync.Map // sessionID → *atomic.Bool: set once Init ran from persisted state
}

// NewSeqGen creates a new sequence generator.
func NewSeqGen() *SeqGen {
	return &SeqGen{}
}

// Peek returns the current sequence number for a session without incrementing.
func (g *SeqGen) Peek(sessionID string) int64 {
	val, ok := g.seq.Load(sessionID)
	if !ok {
		return 0
	}
	return val.(*atomic.Int64).Load() //nolint:errcheck // LoadOrStore guarantees *atomic.Int64
}

// Initialized reports whether a sequence counter has been created for sessionID.
// A hydrated empty session is initialized even though its current value is 0.
func (g *SeqGen) Initialized(sessionID string) bool {
	_, ok := g.seq.Load(sessionID)
	return ok
}

// Next returns the next sequence number for a session.
func (g *SeqGen) Next(sessionID string) int64 {
	val, _ := g.seq.LoadOrStore(sessionID, new(atomic.Int64))
	return val.(*atomic.Int64).Add(1) //nolint:errcheck // LoadOrStore guarantees *atomic.Int64
}

// Remove deletes the sequence counter for a physically deleted session.
func (g *SeqGen) Remove(sessionID string) {
	g.seq.Delete(sessionID)
	g.hydrated.Delete(sessionID)
}

// IsHydrated reports whether the counter has been seeded from persisted events.
// Unlike Initialized, a bare Next (which implicitly creates the counter via
// LoadOrStore) does NOT set this flag, so EnsureSeqHydrated can still raise the
// floor from the DB even when a racing producer allocated seqs first. This
// closes the resume-race window where the old Initialized()-based guard
// short-circuited hydration and let the counter collide with history.
func (g *SeqGen) IsHydrated(sessionID string) bool {
	v, ok := g.hydrated.Load(sessionID)
	if !ok {
		return false
	}
	return v.(*atomic.Bool).Load() //nolint:errcheck // LoadOrStore guarantees *atomic.Bool
}

// MarkHydrated records that the counter has been hydrated from persisted state.
func (g *SeqGen) MarkHydrated(sessionID string) {
	v, _ := g.hydrated.LoadOrStore(sessionID, new(atomic.Bool))
	v.(*atomic.Bool).Store(true) //nolint:errcheck // LoadOrStore guarantees *atomic.Bool
}

// Init seeds the sequence counter for a session to start, but only if start is
// greater than the current value. This lets the gateway hydrate the counter
// from persisted events on reconnect so seq continues monotonically instead of
// restarting from 1 (issue #879: WS disconnect deleted the counter, reconnect
// collided with persisted seq segments). CAS-based — safe against concurrent
// Next/Init; never regresses an already-higher counter.
func (g *SeqGen) Init(sessionID string, start int64) {
	val, _ := g.seq.LoadOrStore(sessionID, new(atomic.Int64))
	cur := val.(*atomic.Int64) //nolint:errcheck // LoadOrStore guarantees *atomic.Int64
	for {
		old := cur.Load()
		if start <= old {
			return
		}
		if cur.CompareAndSwap(old, start) {
			return
		}
	}
}
