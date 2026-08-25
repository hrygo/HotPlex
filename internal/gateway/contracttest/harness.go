// Package contracttest provides test tooling for the platform-worker E2E
// alignment suite (issue #954). The WorkerProbe implements worker.Worker and
// drives hand-written protocol fixtures through each worker type's REAL
// parser/mapper — the four branches use the production decoding/mapping code,
// not hand-built AEP envelopes — so contract assertions exercise the actual
// protocol surface.
//
// The Harness composes a full gateway stack (SQLite store + session manager +
// hub + bridge + handler) around a single WorkerProbe session, so contract
// tests exercise the real input/forwarding path end to end.
package contracttest

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/dbutil"
	"github.com/hrygo/hotplex/internal/e2econtract"
	"github.com/hrygo/hotplex/internal/eventstore"
	"github.com/hrygo/hotplex/internal/execution"
	"github.com/hrygo/hotplex/internal/gateway"
	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/internal/sqlutil"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/events"
)

// harnessKeepAlive bounds how long WaitForKinds polls before failing and how
// long the teardown may block. Both are far below the 5s per-module budget.
const (
	harnessWaitTimeout = 5 * time.Second
	harnessWaitPoll    = 10 * time.Millisecond
	harnessTeardown    = 2 * time.Second
)

// discardLogger silences gateway log output during contract tests.
var discardLogger = slog.New(slog.DiscardHandler)

// Harness is a full gateway stack bound to a single WorkerProbe session.
// It exposes the real gateway entry points (Hub/Bridge/Handler) so contract
// tests drive input through the production path and observe the mapper
// products on a fake platform connection.
type Harness struct {
	Hub     *gateway.Hub
	Bridge  *gateway.Bridge
	Handler *gateway.Handler
	manager *session.Manager

	profile   e2econtract.WorkerProfile
	sessionID string
	dbPath    string
	observer  *observerConn

	probeMu sync.Mutex
	probe   *WorkerProbe   // latest probe (what Worker() returns)
	probes  []*WorkerProbe // every probe created for this harness (teardown closes all)

	nextProbeBlocking atomic.Bool // arm the NEXT probe for the turn-gate mode (matrix C04)

	eventCollector *eventstore.Collector
	eventStore     eventstore.EventStore
}

type harnessOptions struct {
	eventCollector       bool
	stopTeardownTimeout  time.Duration
	stopForwarderTimeout time.Duration
}

// HarnessOption enables an isolated contract-harness capability without
// changing the default platform/worker matrix behavior.
type HarnessOption func(*harnessOptions)

// WithEventCollector enables durable forwarded-event assertions for tests
// such as the stopped-run quarantine contract.
func WithEventCollector() HarnessOption {
	return func(opts *harnessOptions) {
		opts.eventCollector = true
	}
}

// WithStopTimeouts overrides the server-side stop budgets for deterministic
// timeout contract tests. Production callers use Bridge defaults.
func WithStopTimeouts(teardown, forwarder time.Duration) HarnessOption {
	return func(opts *harnessOptions) {
		opts.stopTeardownTimeout = teardown
		opts.stopForwarderTimeout = forwarder
	}
}

// NewHarness builds a full gateway stack against a temp SQLite store and
// starts one platform session for profile on platform. The probe worker is
// created through the bridge's WorkerFactory seam, which rejects any worker
// type that does not match the profile. All teardown is registered on t:
// Bridge.MarkClosed → close every probe conn (so all forwarders exit; the
// closed flag makes handleWorkerExit skip crash recovery) → Bridge.Shutdown →
// Hub.Shutdown → Manager.Close → store.Close, each bounded by a 2s context.
func NewHarness(t testing.TB, platform e2econtract.Platform, profile e2econtract.WorkerProfile, options ...HarnessOption) *Harness {
	t.Helper()

	opts := harnessOptions{}
	for _, option := range options {
		option(&opts)
	}

	log := discardLogger
	cfg := config.Default()
	dbPath := filepath.Join(t.TempDir(), "sessions.db")
	cfg.DB.Path = dbPath
	cfg.DB.SQLite.Path = dbPath

	writeMu := sqlutil.NewWriteMu(sqlutil.DialectSQLite)
	store, err := session.NewSQLiteStore(context.Background(), cfg, writeMu)
	require.NoError(t, err, "contracttest: open sqlite store")
	// The execution store shares the session store's *sql.DB and writeMu, exactly
	// like production initSQLiteStores (cmd/hotplex/gateway_run.go) — the schema
	// (execution_inputs) is part of the session store's migrations. Without it the
	// input path emits no InputAck and surfaces no payload conflicts, which breaks
	// the C01/C02 contract assertions (task-4 review carry-over). It has no
	// resources of its own to close: store.Close() releases the shared DB.
	execStore, err := execution.NewSQLStore(context.Background(), store.DB(), dbutil.DialectSQLite, writeMu, log)
	require.NoError(t, err, "contracttest: open execution store")
	var eventCollector *eventstore.Collector
	var durableEventStore eventstore.EventStore
	if opts.eventCollector {
		durableEventStore = eventstore.NewSQLiteStore(store.DB(), writeMu)
		eventCollector = eventstore.NewCollector(durableEventStore, log)
	}
	cfgStore := config.NewConfigStore(cfg, nil)
	mgr, err := session.NewManager(context.Background(), log, cfg, cfgStore, store)
	require.NoError(t, err, "contracttest: create session manager")

	hub := gateway.NewHub(log, cfgStore)
	bridge := gateway.NewBridge(gateway.BridgeDeps{
		Log:                  log,
		Hub:                  hub,
		SM:                   mgr,
		EventCollector:       eventCollector,
		ExecutionStore:       execStore,
		StopTeardownTimeout:  opts.stopTeardownTimeout,
		StopForwarderTimeout: opts.stopForwarderTimeout,
	})
	handler := gateway.NewHandler(gateway.HandlerDeps{
		Log:             log,
		Hub:             hub,
		SM:              mgr,
		Bridge:          bridge,
		ExecutionStore:  execStore,
		OwnerInstanceID: "contracttest",
	})

	sessionID := "contract-" + uuid.NewString()[:8]
	observer := newObserverConn()
	hub.JoinPlatformSession(sessionID, observer)

	h := &Harness{
		Hub:            hub,
		Bridge:         bridge,
		Handler:        handler,
		manager:        mgr,
		profile:        profile,
		sessionID:      sessionID,
		dbPath:         dbPath,
		observer:       observer,
		eventCollector: eventCollector,
		eventStore:     durableEventStore,
	}
	bridge.SetWorkerFactory(&probeFactory{h: h, profile: profile, sessionID: sessionID})

	// Register teardown before the session start so a failed start still tears
	// the stack down (the probes slice is nil-safe in cleanup).
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), harnessTeardown)
		defer cancel()
		// Mark the bridge closed FIRST: handleWorkerExit then skips crash
		// recovery, so closing probe conns whose turn never ran (the harness
		// session's own probe) cannot spawn a resume-fallback worker. Then
		// close EVERY probe's event source so all forwarders drain; the harness
		// creates one probe per platform session, and closing only the latest
		// left the earlier forwarders blocking until WaitForwarders' 2s bound.
		// Emit is synchronous with Input, so no mapper write is in flight — the
		// close cannot race a send on recvCh.
		bridge.MarkClosed()
		h.probeMu.Lock()
		probes := append([]*WorkerProbe(nil), h.probes...)
		h.probeMu.Unlock()
		for _, p := range probes {
			_ = p.Conn().Close()
		}
		h.Bridge.Shutdown(cleanupCtx)
		_ = h.Hub.Shutdown(cleanupCtx)
		if h.eventCollector != nil {
			_ = h.eventCollector.Close()
		}
		_ = mgr.Close()
		_ = store.Close()
	})

	require.NoError(t, bridge.StartPlatformSession(context.Background(), worker.SessionStartParams{
		ID:          sessionID,
		UserID:      "contract-user",
		WorkerType:  profile.Type,
		Platform:    string(platform),
		PlatformKey: map[string]string{},
	}), "contracttest: start platform session")

	return h
}

// SessionID returns the harness session's ID.
func (h *Harness) SessionID() string { return h.sessionID }

// DBPath returns the SQLite file backing the harness.
func (h *Harness) DBPath() string { return h.dbPath }

// Worker returns the probe currently attached to the harness session. It is
// nil until the bridge has created the worker (i.e. before NewHarness's
// StartPlatformSession returns, it is always non-nil).
func (h *Harness) Worker() *WorkerProbe {
	h.probeMu.Lock()
	defer h.probeMu.Unlock()
	return h.probe
}

// DetachWorkerForTest removes the SessionManager attachment while leaving the
// bridge's historical run metadata intact. It models a crash/detach window in
// which control.stop must rely on persisted execution identity rather than a
// live Worker binding.
func (h *Harness) DetachWorkerForTest() {
	h.manager.DetachWorker(h.sessionID)
}

func (h *Harness) setProbe(p *WorkerProbe) {
	h.probeMu.Lock()
	defer h.probeMu.Unlock()
	h.probe = p
	h.probes = append(h.probes, p)
}

// EnableProbeBlocking arms every probe created from now on to run in the
// blocking turn-gate mode (C04: the fixture terminal is held until released,
// and a stop before the release suppresses it). The arm persists until
// DisableProbeBlocking — scenario drivers call it at BeginScenario (C04) and
// clear it at EndScenario, because a scenario's input may create MORE than one
// probe (e.g. webchat auto-resume replaces the worker mid-scenario) and every
// one of them must be gated for the stop to land deterministically.
func (h *Harness) EnableProbeBlocking() { h.nextProbeBlocking.Store(true) }

// DisableProbeBlocking clears the blocking arm so later scenarios run with the
// synchronous full-turn emission (the zero-value behavior).
func (h *Harness) DisableProbeBlocking() { h.nextProbeBlocking.Store(false) }

// Probes returns a snapshot of every probe created for this harness, oldest
// first (the harness's own platform-session probe is the first entry).
func (h *Harness) Probes() []*WorkerProbe {
	h.probeMu.Lock()
	defer h.probeMu.Unlock()
	out := make([]*WorkerProbe, len(h.probes))
	copy(out, h.probes)
	return out
}

// StoredEvents flushes and returns every durable event for the harness
// session. The harness must have been created with WithEventCollector.
func (h *Harness) StoredEvents(t testing.TB) []*eventstore.StoredEvent {
	t.Helper()
	require.NotNil(t, h.eventCollector, "contracttest: event collector is not enabled")
	require.NotNil(t, h.eventStore, "contracttest: event store is not enabled")
	require.NoError(t, h.eventCollector.FlushSession(h.sessionID), "contracttest: flush stored events")
	page, err := h.eventStore.QueryBySession(
		context.Background(), h.sessionID, 0, eventstore.CursorLatest, 1000,
	)
	require.NoError(t, err, "contracttest: query stored events")
	return page.Events
}

// Events returns a snapshot of every envelope observed by the platform side.
func (h *Harness) Events() []*events.Envelope { return h.observer.Events() }

// SessionState returns the current persisted/in-memory session state.
func (h *Harness) SessionState(t testing.TB) events.SessionState {
	t.Helper()
	si, err := h.manager.Get(context.Background(), h.sessionID)
	require.NoError(t, err, "contracttest: get session state")
	return si.State
}

// WaitForKinds blocks until every event kind in kinds has been observed on the
// harness's platform observer, then returns the full ordered event log. Fails
// the test if the kinds are not all observed within harnessWaitTimeout.
func (h *Harness) WaitForKinds(t testing.TB, kinds ...events.Kind) []*events.Envelope {
	t.Helper()
	require.Eventually(t, func() bool {
		seen := make(map[events.Kind]bool, len(kinds))
		for _, env := range h.observer.Events() {
			seen[env.Event.Type] = true
		}
		for _, kind := range kinds {
			if !seen[kind] {
				return false
			}
		}
		return true
	}, harnessWaitTimeout, harnessWaitPoll, "harness: timed out waiting for events %v", kinds)
	return h.observer.Events()
}

// AssertSingleTerminal asserts exactly one terminal event (done/error) reached
// the platform observer. When reason is non-empty, the terminal must be a done
// whose reason matches.
func (h *Harness) AssertSingleTerminal(t testing.TB, reason string) {
	t.Helper()
	var terminals []*events.Envelope
	for _, env := range h.observer.Events() {
		if env.Event.Type == events.Done || env.Event.Type == events.Error {
			terminals = append(terminals, env)
		}
	}
	require.Len(t, terminals, 1, "harness: expected exactly one terminal event (done/error), got %d: %v", len(terminals), h.observer.Events())
	if reason == "" {
		return
	}
	term := terminals[0]
	require.Equal(t, events.Done, term.Event.Type, "harness: expected a done terminal, got %s", term.Event.Type)
	data, ok := term.Event.Data.(events.DoneData)
	require.True(t, ok, "harness: done event should carry DoneData, got %T", term.Event.Data)
	require.Equal(t, reason, data.Reason, "harness: done reason mismatch")
}

// ─── probeFactory ─────────────────────────────────────────────────────────────

// probeFactory is a gateway.WorkerFactory that creates a fresh WorkerProbe for
// the harness session and rejects any worker type that does not match the
// harness profile. Rejecting mismatched types guards against a buggy bridge
// wiring accidentally driving a different worker than the scenario expects.
type probeFactory struct {
	h         *Harness
	profile   e2econtract.WorkerProfile
	sessionID string
}

func (f *probeFactory) NewWorker(wt worker.WorkerType) (worker.Worker, error) {
	if wt != f.profile.Type {
		return nil, fmt.Errorf("contracttest: harness profile %s cannot create worker type %s", f.profile.Type, wt)
	}
	probe := NewWorkerProbe(f.profile, f.sessionID)
	if f.h.nextProbeBlocking.Load() {
		probe.EnableBlocking()
	}
	f.h.setProbe(probe)
	return probe, nil
}

// ─── observerConn ─────────────────────────────────────────────────────────────

// observerConn is a messaging.PlatformConn that records every envelope the hub
// forwards to the harness session. It is registered via Hub.JoinPlatformSession
// and closed by Hub.Shutdown during teardown.
type observerConn struct {
	mu      sync.Mutex
	entries []*events.Envelope
	closed  atomic.Bool
}

func newObserverConn() *observerConn {
	return &observerConn{entries: make([]*events.Envelope, 0, 32)}
}

func (c *observerConn) WriteCtx(_ context.Context, env *events.Envelope) error {
	c.mu.Lock()
	c.entries = append(c.entries, env)
	c.mu.Unlock()
	return nil
}

func (c *observerConn) Close() error {
	c.closed.Store(true)
	return nil
}

// Events returns a snapshot of every envelope observed, in arrival order.
func (c *observerConn) Events() []*events.Envelope {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]*events.Envelope, len(c.entries))
	copy(out, c.entries)
	return out
}
