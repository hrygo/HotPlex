package gateway

import (
	"database/sql"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/eventstore"
	"github.com/hrygo/hotplex/internal/sqlutil"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/aep"
	"github.com/hrygo/hotplex/pkg/events"
)

// newCollectorBridgeForSeqTest creates a Bridge with a real Hub (hydrated to
// knownSeq) and a real eventstore Collector, so the collector-enabled seq path
// in processForwardedEvent is exercised.
func newCollectorBridgeForSeqTest(t *testing.T, sessionID string, knownSeq int64) (*Bridge, *eventstore.SQLiteStore) {
	t.Helper()

	hub := newTestHub(t)
	hub.seqGen.Init(sessionID, knownSeq)
	hub.seqGen.MarkHydrated(sessionID)

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open(sqlutil.DriverName, dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id TEXT NOT NULL,
		seq INTEGER NOT NULL,
		type TEXT NOT NULL,
		data TEXT NOT NULL,
		direction TEXT NOT NULL DEFAULT 'outbound',
		source TEXT NOT NULL DEFAULT 'normal'
			CHECK(source IN ('normal', 'crash', 'timeout', 'fresh_start')),
		created_at INTEGER NOT NULL
	)`)
	require.NoError(t, err)

	store := eventstore.NewSQLiteStore(db, nil)
	collector := eventstore.NewCollector(store, slog.Default())
	t.Cleanup(func() { _ = collector.Close() })

	b := NewBridge(BridgeDeps{Log: slog.Default(), Hub: hub})
	b.collector = collector
	b.shutdownCancel()

	return b, store
}

// TestProcessForwardedEvent_CollectorAlwaysAssignsHubSeq verifies that when
// eventstore (collector) is enabled, processForwardedEvent ALWAYS assigns the
// Hub SeqGen value, even when the Worker-provided envelope already has a
// non-zero seq.
//
// Regression for issue #879 (recurring): ACP/Codex/Claude Code mappers use a
// local atomic counter that restarts from 1 on every Worker launch. On resume,
// the bridge accepted the mapper's low seq directly (because env.Seq != 0),
// bypassing the hydrated Hub SeqGen. The low seq then collided with persisted
// events, causing UNIQUE constraint failures (2067) and lost events.
//
// Uses events.State (not Done) to isolate a single seq allocation — Done
// triggers maybeSendDoneFallback which allocates an additional seq for the
// fallback message, obscuring the primary assertion.
func TestProcessForwardedEvent_CollectorAlwaysAssignsHubSeq(t *testing.T) {
	t.Parallel()

	const sessionID = "seq-resume-test"
	const hydratedSeq = int64(295)

	b, _ := newCollectorBridgeForSeqTest(t, sessionID, hydratedSeq)

	// Simulate a Worker mapper that produced seq=1 (local counter restart).
	workerEnv := events.NewEnvelope(aep.NewID(), "", 1, events.State,
		events.StateData{State: events.StateRunning})
	require.Equal(t, int64(1), workerEnv.Seq, "precondition: worker set seq=1")

	// Verify Hub SeqGen starts at hydratedSeq.
	require.Equal(t, hydratedSeq, b.hub.NextSeqPeek(sessionID))

	fc := &forwardContext{
		sessionID:     sessionID,
		workerType:    worker.TypeACP,
		turnStartTime: time.Now(),
	}
	fw := &mockBridgeWorker{workerType: worker.TypeACP}

	b.processForwardedEvent(workerEnv, fw, forwardOpts{}, fc)

	// Hub SeqGen must have advanced exactly once — proving the bridge overrode
	// the worker's seq=1 with the Hub's hydrated value (296).
	require.Equal(t, hydratedSeq+1, b.hub.NextSeqPeek(sessionID),
		"bridge must assign Hub SeqGen when collector is enabled, not worker seq")
}

// TestProcessForwardedEvent_NoCollectorPreservesWorkerSeq verifies the inverse:
// when collector is nil (eventstore disabled), the bridge respects the
// worker-provided seq if non-zero (non-durable deployment behavior).
func TestProcessForwardedEvent_NoCollectorPreservesWorkerSeq(t *testing.T) {
	t.Parallel()

	const sessionID = "seq-nocollector-test"

	hub := newTestHub(t)
	b := NewBridge(BridgeDeps{Log: slog.Default(), Hub: hub})
	b.shutdownCancel()
	// b.collector is nil — eventstore disabled.

	hub.seqGen.Init(sessionID, 0)

	// Worker provides a non-zero seq.
	workerEnv := events.NewEnvelope(aep.NewID(), "", 42, events.State,
		events.StateData{State: events.StateRunning})

	fc := &forwardContext{
		sessionID:     sessionID,
		workerType:    worker.TypeACP,
		turnStartTime: time.Now(),
	}
	fw := &mockBridgeWorker{workerType: worker.TypeACP}

	b.processForwardedEvent(workerEnv, fw, forwardOpts{}, fc)

	// Hub SeqGen should NOT have been used (collector is nil, worker seq != 0).
	require.Equal(t, int64(0), hub.NextSeqPeek(sessionID),
		"Hub SeqGen must not advance when collector is disabled and worker provided a non-zero seq")
}
