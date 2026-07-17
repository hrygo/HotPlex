package audit

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// testSink captures audit events for verification in tests.
type testSink struct {
	mu     sync.Mutex
	events []AuditEvent
}

func (s *testSink) OnAuditEvent(ctx context.Context, e AuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, e)
	return nil
}

func (s *testSink) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.events)
}

func (s *testSink) Events() []AuditEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]AuditEvent, len(s.events))
	copy(out, s.events)
	return out
}

// newTestCollector creates a Collector with SQLite store + spill file for testing.
func newTestCollector(t *testing.T, channelCap, batchSize int, batchInterval time.Duration) (*Collector, Store, *SpillFile, *testSink) {
	t.Helper()

	store := newTestSQLiteStore(t)
	spillPath := filepath.Join(t.TempDir(), "spill.wal")
	spill, err := OpenSpill(spillPath)
	require.NoError(t, err)

	ts := &testSink{}

	c := NewCollector(store, spill, []AlertSink{ts}, slog.Default(), CollectorConfig{
		ChannelCap:    channelCap,
		BatchSize:     batchSize,
		BatchInterval: batchInterval,
	})
	c.Start(context.Background())
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	return c, store, spill, ts
}

func TestCollector_OrderingPreserved(t *testing.T) {
	t.Parallel()

	c, store, _, _ := newTestCollector(t, 256, 4, 100*time.Millisecond)

	for i := 0; i < 10; i++ {
		ua := &UserActivity{
			Ts:         int64(1700000000000 + i*1000),
			UserID:     "u1",
			UserIDType: UserIDTypePlatform,
			Platform:   PlatformTest,
			Action:     ActionAuthLogin,
			Outcome:    OutcomeSuccess,
			DetailJSON: `{}`,
		}
		require.NoError(t, c.Enqueue(context.Background(), ua))
	}

	require.Eventually(t, func() bool { return c.Enqueued() == 10 }, 5*time.Second, 50*time.Millisecond)
	// Give the writer time to flush the final partial batch
	require.Eventually(t, func() bool {
		rows, err := store.Query(context.Background(), Query{UserID: "u1"})
		if err != nil {
			return false
		}
		return len(rows) == 10
	}, 5*time.Second, 100*time.Millisecond)

	rows, err := store.Query(context.Background(), Query{UserID: "u1"})
	require.NoError(t, err)
	require.Len(t, rows, 10)

	// Verify hash chain integrity
	brokenID, reason := VerifyChain(reverseRows(rows), "")
	require.Equal(t, int64(0), brokenID, "chain broken: %s", reason)
}

func TestCollector_SpillOnFull(t *testing.T) {
	t.Parallel()

	// Tiny channel, long batch interval → forces spills
	c, store, spill, _ := newTestCollector(t, 2, 100, 5*time.Second)

	ctx := context.Background()
	n := 20
	for i := 0; i < n; i++ {
		ua := &UserActivity{
			Ts:         int64(i),
			UserID:     "u1",
			UserIDType: UserIDTypePlatform,
			Platform:   PlatformTest,
			Action:     ActionMessageInbound,
			Outcome:    OutcomeSuccess,
			DetailJSON: `{}`,
		}
		err := c.Enqueue(ctx, ua)
		require.NoError(t, err)
	}

	// Some events should have spilled (channel cap is only 2)
	require.Greater(t, c.Spilled(), int64(0), "expected some events to spill")

	// Close the collector — this drains everything
	require.NoError(t, c.Close(context.Background()))

	// After close, spill file should be empty (all events replayed and flushed)
	remaining, err := spill.ReadAll()
	require.NoError(t, err)
	require.Empty(t, remaining, "spill file should be empty after drain")

	// All events should be in the database
	rows, err := store.Query(context.Background(), Query{})
	require.NoError(t, err)
	require.Len(t, rows, n, "all %d events should be in DB", n)

	// Verify hash chain
	brokenID, reason := VerifyChain(reverseRows(rows), "")
	require.Equal(t, int64(0), brokenID, "chain broken: %s", reason)
}

func TestCollector_SinkFanout(t *testing.T) {
	t.Parallel()

	c, _, _, ts := newTestCollector(t, 256, 2, 50*time.Millisecond)

	ctx := context.Background()
	for i := 0; i < 5; i++ {
		ua := &UserActivity{
			Ts:         int64(1000 + i),
			UserID:     "u1",
			UserIDType: UserIDTypeRegistered,
			Platform:   PlatformWebChat,
			Action:     ActionSessionCreate,
			Outcome:    OutcomeSuccess,
			DetailJSON: `{}`,
		}
		require.NoError(t, c.Enqueue(ctx, ua))
	}

	// Sink should eventually receive all 5 events
	require.Eventually(t, func() bool { return ts.Count() == 5 }, 5*time.Second, 100*time.Millisecond)

	events := ts.Events()
	require.Len(t, events, 5)
	for _, e := range events {
		require.Equal(t, "u1", e.UserID)
		require.Equal(t, ActionSessionCreate, e.Action)
	}
}

func TestCollector_CloseDrainsSpill(t *testing.T) {
	t.Parallel()

	// Channel cap = 4, very slow batch interval → some events will spill
	c, store, spill, _ := newTestCollector(t, 4, 100, 5*time.Second)

	ctx := context.Background()
	n := 12
	for i := 0; i < n; i++ {
		ua := &UserActivity{
			Ts:         int64(1700000000000 + i),
			UserID:     "u1",
			UserIDType: UserIDTypePlatform,
			Platform:   PlatformSlack,
			Action:     ActionAuthLogin,
			Outcome:    OutcomeSuccess,
			DetailJSON: `{}`,
		}
		require.NoError(t, c.Enqueue(ctx, ua))
	}

	// Close must drain spill and flush all events
	require.NoError(t, c.Close(context.Background()))

	// Spill should be empty
	remaining, err := spill.ReadAll()
	require.NoError(t, err)
	require.Empty(t, remaining)

	// All events in DB
	rows, err := store.Query(context.Background(), Query{UserID: "u1"})
	require.NoError(t, err)
	require.Len(t, rows, n, "all %d events should be persisted after close", n)

	// Hash chain intact
	brokenID, reason := VerifyChain(reverseRows(rows), "")
	require.Equal(t, int64(0), brokenID, "chain broken: %s", reason)
}

func TestCollector_DroppedIsZero(t *testing.T) {
	t.Parallel()

	// Small channel, fast batch interval to simulate bursty load
	c, _, _, _ := newTestCollector(t, 8, 4, 50*time.Millisecond)

	ctx := context.Background()
	n := 50
	for i := 0; i < n; i++ {
		ua := &UserActivity{
			Ts:         int64(i),
			UserID:     "u1",
			UserIDType: UserIDTypePlatform,
			Platform:   PlatformTest,
			Action:     ActionAuthLogin,
			Outcome:    OutcomeSuccess,
			DetailJSON: `{}`,
		}
		err := c.Enqueue(ctx, ua)
		require.NoError(t, err)
	}

	// Even under pressure, nothing should be dropped
	require.Equal(t, int64(0), c.Dropped(), "zero-loss: nothing should be dropped")
}

func TestCollector_HashChainIntegrity(t *testing.T) {
	t.Parallel()

	c, store, _, _ := newTestCollector(t, 256, 4, 100*time.Millisecond)

	ctx := context.Background()
	n := 12
	for i := 0; i < n; i++ {
		outcome := OutcomeSuccess
		if i%3 == 0 {
			outcome = OutcomeFailure
		}
		ua := &UserActivity{
			Ts:         int64(1700000000000 + i*1000),
			UserID:     "u1",
			UserIDType: UserIDTypeRegistered,
			Platform:   PlatformWebChat,
			Action:     ActionMessageInbound,
			Outcome:    outcome,
			DetailJSON: `{"seq":` + string(rune('0'+i%10)) + `}`,
		}
		require.NoError(t, c.Enqueue(ctx, ua))
	}

	// Wait for all batches to flush
	require.Eventually(t, func() bool {
		rows, err := store.Query(context.Background(), Query{})
		if err != nil {
			return false
		}
		return len(rows) == n
	}, 5*time.Second, 100*time.Millisecond)

	rows, err := store.Query(context.Background(), Query{})
	require.NoError(t, err)
	require.Len(t, rows, n)

	// Full chain verification (reverse DESC rows for VerifyChain ASC expectation)
	brokenID, reason := VerifyChain(reverseRows(rows), "")
	require.Equal(t, int64(0), brokenID, "hash chain broken: %s", reason)

	// Verify exactly one genesis row and all self_hash values are non-empty.
	genesisCount := 0
	selfHashSet := make(map[string]bool, n)
	for _, row := range rows {
		if row.PrevHash == "" {
			genesisCount++
		}
		require.NotEmpty(t, row.SelfHash, "row %d has empty self_hash", row.ID)
		selfHashSet[row.SelfHash] = true
	}
	require.Equal(t, 1, genesisCount, "exactly one genesis row expected")
	// Every prev_hash (except genesis) must reference a self_hash from another row.
	for _, row := range rows {
		if row.PrevHash != "" {
			require.True(t, selfHashSet[row.PrevHash],
				"row %d prev_hash not found in any self_hash", row.ID)
		}
	}
}

func TestCollector_EnqueueNilReturnsError(t *testing.T) {
	t.Parallel()

	c, _, _, _ := newTestCollector(t, 256, 10, 1*time.Second)

	err := c.Enqueue(context.Background(), nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "nil")
}

func TestCollector_EnqueueOnClosedReturnsError(t *testing.T) {
	t.Parallel()

	c, _, _, _ := newTestCollector(t, 256, 10, 1*time.Second)

	require.NoError(t, c.Close(context.Background()))

	err := c.Enqueue(context.Background(), &UserActivity{
		Ts: 1000, UserID: "u1", UserIDType: UserIDTypePlatform,
		Platform: PlatformTest, Action: ActionAuthLogin, Outcome: OutcomeSuccess,
		DetailJSON: `{}`,
	})
	require.Error(t, err)
}

func TestCollector_StartEnqueuedSpilledCounters(t *testing.T) {
	t.Parallel()

	c, _, _, _ := newTestCollector(t, 16, 10, 500*time.Millisecond)

	require.Equal(t, int64(0), c.Enqueued())
	require.Equal(t, int64(0), c.Spilled())
	require.Equal(t, int64(0), c.Dropped())

	ctx := context.Background()
	for i := 0; i < 5; i++ {
		ua := &UserActivity{
			Ts:         int64(i),
			UserID:     "u1",
			UserIDType: UserIDTypePlatform,
			Platform:   PlatformTest,
			Action:     ActionAuthLogin,
			Outcome:    OutcomeSuccess,
			DetailJSON: `{}`,
		}
		require.NoError(t, c.Enqueue(ctx, ua))
	}

	// At least the non-spilled events are tracked
	require.GreaterOrEqual(t, c.Enqueued(), int64(5))
	require.Equal(t, int64(0), c.Dropped())
}

func TestCollector_MultipleCloseIsSafe(t *testing.T) {
	t.Parallel()

	c, _, _, _ := newTestCollector(t, 256, 10, 1*time.Second)

	require.NoError(t, c.Close(context.Background()))
	require.NoError(t, c.Close(context.Background())) // second close should be no-op
}

func TestCollector_NilLoggerFallsBack(t *testing.T) {
	store := newTestSQLiteStore(t)
	spillPath := filepath.Join(t.TempDir(), "spill_nologger.wal")
	spill, err := OpenSpill(spillPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = spill.Close() })

	// nil logger should not panic
	c := NewCollector(store, spill, nil, nil, CollectorConfig{})
	require.NotNil(t, c)
	require.NotNil(t, c.log)
	require.NotNil(t, c.captureC)
}

func TestCollector_ConfigDefaults(t *testing.T) {
	store := newTestSQLiteStore(t)

	c := NewCollector(store, nil, nil, slog.Default(), CollectorConfig{})
	require.Equal(t, 4096, c.cfg.ChannelCap)
	require.Equal(t, 100, c.cfg.BatchSize)
	require.Equal(t, 1*time.Second, c.cfg.BatchInterval)
	require.Equal(t, 5*time.Second, c.cfg.SinkTimeout)
	require.Equal(t, 5*time.Second, c.cfg.SpillBlockTimeout)
}

func TestCollector_SinksNilIsHandled(t *testing.T) {
	store := newTestSQLiteStore(t)

	c := NewCollector(store, nil, nil, slog.Default(), CollectorConfig{
		ChannelCap: 16, BatchSize: 4, BatchInterval: 100 * time.Millisecond,
	})
	c.Start(context.Background())
	t.Cleanup(func() { _ = c.Close(context.Background()) })

	ua := &UserActivity{
		Ts: 1000, UserID: "u1", UserIDType: UserIDTypePlatform,
		Platform: PlatformTest, Action: ActionAuthLogin, Outcome: OutcomeSuccess,
		DetailJSON: `{}`,
	}
	// Should not panic with nil sinks
	require.NoError(t, c.Enqueue(context.Background(), ua))
}

// reverseRows reverses a slice of UserActivity in place (for ASC-order VerifyChain).
func reverseRows(rows []UserActivity) []UserActivity {
	for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
		rows[i], rows[j] = rows[j], rows[i]
	}
	return rows
}

// --- newEventID / UUIDv7 tests (review issue #1) ---

func TestNewEventID_IsUUIDv7Shape(t *testing.T) {
	t.Parallel()
	id := newEventID()
	// Canonical 8-4-4-4-12 = 36 chars including dashes.
	require.Len(t, id, 36)
	// Version nibble at position 14 must be '7'.
	require.Equal(t, "7", string(id[14]))
	// Variant bits: char at position 19 must be 8/9/a/b.
	c := id[19]
	require.Contains(t, "89ab", string(c), "invalid variant char %q", c)
}

func TestNewEventID_UniqueWithinMillisecond(t *testing.T) {
	t.Parallel()
	// Spec §5.6 requires EventID to be unique for sink dedup/idempotency.
	// Two IDs minted in the same millisecond must differ (regression for
	// the old ev_<ts>_<hash(ts)> scheme which collided within 1ms).
	const n = 5000
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		id := newEventID()
		_, dup := seen[id]
		require.False(t, dup, "duplicate EventID after %d minted: %s", i, id)
		seen[id] = struct{}{}
	}
}

func TestUserActivityToAuditEvent_DecodesDetail(t *testing.T) {
	t.Parallel()
	// Regression for decodeDetail stub (review issue #2): sinks must
	// receive the structured Detail map, not nil.
	ua := &UserActivity{
		Ts:         1700000000000,
		UserID:     "u1",
		UserIDType: UserIDTypePlatform,
		Platform:   PlatformTest,
		Action:     ActionAuthLogin,
		Outcome:    OutcomeSuccess,
		DetailJSON: `{"seq":42,"tool":"Bash","ok":true}`,
	}
	ev := userActivityToAuditEvent(ua)
	require.Equal(t, "u1", ev.UserID)
	require.NotNil(t, ev.Detail, "Detail must be decoded, not nil")
	require.Equal(t, float64(42), ev.Detail["seq"])
	require.Equal(t, "Bash", ev.Detail["tool"])
	require.Equal(t, true, ev.Detail["ok"])
}

func TestUserActivityToAuditEvent_DecodeDetailEmptyIsNil(t *testing.T) {
	t.Parallel()
	ua := &UserActivity{DetailJSON: ""}
	require.Nil(t, userActivityToAuditEvent(ua).Detail)
	// Malformed JSON must not panic — returns nil defensively.
	ua.DetailJSON = "{not json"
	require.Nil(t, userActivityToAuditEvent(ua).Detail)
}

// --- Spill re-spill on DB failure tests (review issue #6) ---

// failingCommitStore wraps a real SQLite store but always fails Commit,
// simulating a DB outage during drain. Every other method passes through.
type failingCommitStore struct {
	Store
}

type failingCommitTx struct {
	Tx
}

func (f *failingCommitTx) Commit() error {
	return errors.New("simulated db commit failure")
}

func (f *failingCommitStore) BeginTx(ctx context.Context) (Tx, error) {
	tx, err := f.Store.BeginTx(ctx)
	if err != nil {
		return nil, err
	}
	return &failingCommitTx{Tx: tx}, nil
}

// TestCollector_SpillFlushFailure_ReSpills verifies the zero-loss
// guarantee (spec §5.10) when the DB flush fails during a spill drain:
// the spilled records must be re-spilled so they survive for the next
// drain attempt, rather than being permanently lost.
func TestCollector_SpillFlushFailure_ReSpills(t *testing.T) {
	t.Parallel()

	store := &failingCommitStore{Store: newTestSQLiteStore(t)}
	spillPath := filepath.Join(t.TempDir(), "spill_respill.wal")
	spill, err := OpenSpill(spillPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = spill.Close() })

	c := NewCollector(store, spill, nil, slog.Default(), CollectorConfig{
		ChannelCap:    4,
		BatchSize:     100, // large so events accumulate without auto-flushing
		BatchInterval: 5 * time.Second,
	})
	t.Cleanup(func() { _ = c.Close(context.Background()) })

	ctx := context.Background()
	const n = 6 // > channel cap → at least some will spill
	for i := 0; i < n; i++ {
		ua := &UserActivity{
			Ts:         int64(1700000000000 + i),
			UserID:     "u1",
			UserIDType: UserIDTypePlatform,
			Platform:   PlatformTest,
			Action:     ActionAuthLogin,
			Outcome:    OutcomeSuccess,
			DetailJSON: `{}`,
		}
		require.NoError(t, c.Enqueue(ctx, ua))
	}
	// At least one event must have spilled (channel cap is only 4).
	require.Greater(t, c.Spilled(), int64(0), "expected some events to spill")
	c.Start(context.Background())

	// Trigger a manual drain via the spill sentinel. The DB flush will
	// fail (failingCommitStore), so the spilled records must be re-spilled.
	require.NoError(t, c.Enqueue(ctx, spillSentinel))

	// Eventually the spill file should be NON-empty (records re-spilled),
	// and Dropped() should still be 0 (zero-loss maintained).
	require.Eventually(t, func() bool {
		records, rErr := spill.ReadAll()
		return rErr == nil && len(records) > 0
	}, 5*time.Second, 50*time.Millisecond, "expected re-spilled records to survive flush failure")

	require.Equal(t, int64(0), c.Dropped(), "zero-loss: re-spill must not count as drop")
}

// TestCollector_ConcurrentGcNoBusyLock is a regression test for the
// SQLITE_BUSY errors observed when the GC goroutine (SaveCheckpoint /
// DeleteBefore) and the collector writer (BeginTx → Append → Commit)
// concurrently write to the shared SQLite DB without write-serialization
// on the transaction path.
//
// Before the fix, sqliteStore.BeginTx did not acquire writeMu, so a GC
// Tick (which does hold writeMu) running concurrently with a collector
// flush would surface as "database is locked (SQLITE_BUSY)" on the hot
// append path, failing the flush and (per zero-loss) forcing a re-spill.
//
// This test drives both writers hard in parallel and asserts that no GC
// Tick returns a SQLITE_BUSY / "database is locked" error: the writeMu
// held across the collector's BeginTx→Commit must serialize against the
// GC's locked writes. Zero-loss (Dropped()==0) is also asserted.
func TestCollector_ConcurrentGcNoBusyLock(t *testing.T) {
	t.Parallel()

	// Shared store + writeMu across collector and GC, mirroring production
	// wiring (gateway_run.go passes one writeMu to all stores). Pool size 3
	// matches the production default — without writeMu on the Tx path, two
	// concurrently-open write transactions on different pool connections
	// race for SQLite's single writer slot and surface SQLITE_BUSY.
	store := newTestSQLiteStoreWithPool(t, 3)
	gc := NewGC(store, GCConfig{
		Retention: 1 * time.Microsecond, // prune everything older than 1µs immediately
		Interval:  1 * time.Hour,        // we drive Tick manually, not via Run
	}, slog.Default())

	c := NewCollector(store, nil, nil, slog.Default(), CollectorConfig{
		ChannelCap:    64,
		BatchSize:     8,
		BatchInterval: 10 * time.Millisecond, // flush frequently to maximize overlap with GC
	})
	c.Start(context.Background())
	t.Cleanup(func() { _ = c.Close(context.Background()) })

	// stopC signals both the enqueuers and the GC goroutines to exit, so
	// neither path is interrupted mid-operation by context cancellation
	// (which would otherwise look like an error).
	stopC := make(chan struct{})

	const enqueuers = 4
	const eventsEach = 40
	var wg sync.WaitGroup
	for w := 0; w < enqueuers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < eventsEach; i++ {
				select {
				case <-stopC:
					return
				default:
				}
				ua := &UserActivity{
					Ts:         time.Now().UnixMilli(),
					UserID:     "u1",
					UserIDType: UserIDTypePlatform,
					Platform:   PlatformTest,
					Action:     ActionAuthLogin,
					Outcome:    OutcomeSuccess,
					DetailJSON: `{}`,
				}
				_ = c.Enqueue(context.Background(), ua)
			}
		}()
	}

	// GC hammer: run Tick concurrently with collector flushes. GC uses
	// context.Background() so an in-flight Tick is never cancelled — a
	// cancel would mask/conflate with a genuine SQLITE_BUSY.
	const gcTickers = 2
	var busyErrs atomic.Int32
	var gcWg sync.WaitGroup
	for g := 0; g < gcTickers; g++ {
		gcWg.Add(1)
		go func() {
			defer gcWg.Done()
			for {
				select {
				case <-stopC:
					return
				default:
				}
				// Only count SQLITE_BUSY-class errors. Other errors (e.g.
				// concurrent writes racing on the single-conn test pool)
				// that are NOT "database is locked" indicate a different
				// problem worth surfacing, but the regression we guard
				// against here is specifically the busy-lock.
				if _, err := gc.Tick(context.Background()); err != nil &&
					(strings.Contains(err.Error(), "database is locked") ||
						strings.Contains(err.Error(), "SQLITE_BUSY")) {
					busyErrs.Add(1)
				}
				time.Sleep(2 * time.Millisecond)
			}
		}()
	}

	// Let the load run for a short window, then signal stop and wait.
	<-time.After(300 * time.Millisecond)
	close(stopC)
	wg.Wait()
	gcWg.Wait()

	require.Equal(t, int32(0), busyErrs.Load(),
		"GC must not see SQLITE_BUSY — writeMu serializes GC and collector writes")
	require.Equal(t, int64(0), c.Dropped(), "zero-loss maintained under concurrent GC")
}

// PlatformTest is a convenience constant for tests (not in the public API).
const PlatformTest = "test"

// ─── I3: Close must drain in-flight sink fan-out ──────────────────────────────

// blockingSink blocks OnAuditEvent until the released latch channel is closed.
// Used to prove Close waits for in-flight fan-out goroutines before returning.
type blockingSink struct {
	release chan struct{}
	called  chan struct{} // signals OnAuditEvent was entered
}

type panicSink struct {
	called chan struct{}
}

func (s *panicSink) OnAuditEvent(_ context.Context, _ AuditEvent) error {
	select {
	case s.called <- struct{}{}:
	default:
	}
	panic("test sink panic")
}

type lifecycleSink struct {
	testSink
	closed chan struct{}
}

func (s *lifecycleSink) Close(context.Context) error {
	close(s.closed)
	return nil
}

func testAuditActivity() *UserActivity {
	return &UserActivity{
		Ts: time.Now().UnixMilli(), UserID: "u1", UserIDType: UserIDTypePlatform,
		Platform: PlatformTest, Action: ActionAuthLogin, Outcome: OutcomeSuccess, DetailJSON: `{}`,
	}
}

func TestCollector_BlockingSinkDoesNotStallPersistence(t *testing.T) {
	t.Parallel()
	store := newTestSQLiteStore(t)
	sink := &blockingSink{release: make(chan struct{}), called: make(chan struct{}, 1)}
	c := NewCollector(store, nil, []AlertSink{sink}, slog.Default(), CollectorConfig{
		ChannelCap: 8, BatchSize: 1, BatchInterval: 10 * time.Millisecond, SinkTimeout: time.Second,
	})
	c.Start(context.Background())
	t.Cleanup(func() {
		select {
		case <-sink.release:
		default:
			close(sink.release)
		}
		_ = c.Close(context.Background())
	})

	require.NoError(t, c.Enqueue(context.Background(), testAuditActivity()))
	select {
	case <-sink.called:
	case <-time.After(time.Second):
		t.Fatal("sink was not called")
	}
	require.NoError(t, c.Enqueue(context.Background(), testAuditActivity()))
	require.NoError(t, c.Enqueue(context.Background(), testAuditActivity()))
	require.Eventually(t, func() bool {
		rows, err := store.Query(context.Background(), Query{})
		return err == nil && len(rows) == 3
	}, 500*time.Millisecond, 10*time.Millisecond)
}

func TestCollector_RecoversSinkPanicAndContinues(t *testing.T) {
	t.Parallel()
	store := newTestSQLiteStore(t)
	sink := &panicSink{called: make(chan struct{}, 2)}
	c := NewCollector(store, nil, []AlertSink{sink}, slog.Default(), CollectorConfig{
		ChannelCap: 8, BatchSize: 1, BatchInterval: 10 * time.Millisecond,
	})
	c.Start(context.Background())
	t.Cleanup(func() { _ = c.Close(context.Background()) })

	require.NoError(t, c.Enqueue(context.Background(), testAuditActivity()))
	require.NoError(t, c.Enqueue(context.Background(), testAuditActivity()))
	require.Eventually(t, func() bool { return len(sink.called) == 2 }, time.Second, 10*time.Millisecond)
}

func TestCollector_ClosesLifecycleSink(t *testing.T) {
	t.Parallel()
	store := newTestSQLiteStore(t)
	sink := &lifecycleSink{closed: make(chan struct{})}
	c := NewCollector(store, nil, []AlertSink{sink}, slog.Default(), CollectorConfig{})
	c.Start(context.Background())
	require.NoError(t, c.Close(context.Background()))
	select {
	case <-sink.closed:
	case <-time.After(time.Second):
		t.Fatal("lifecycle sink was not closed")
	}
}

func TestCollector_CloseBeforeStart(t *testing.T) {
	t.Parallel()
	store := newTestSQLiteStore(t)
	c := NewCollector(store, nil, nil, slog.Default(), CollectorConfig{})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, c.Close(ctx))
}

func (s *blockingSink) OnAuditEvent(_ context.Context, _ AuditEvent) error {
	select {
	case s.called <- struct{}{}:
	default:
	}
	<-s.release // block until the test releases the latch
	return nil
}

// TestCollector_CloseWaitsForSinks verifies the I3 fix: Close must not return
// while a sink fan-out goroutine is still in flight. Before the fix, Close
// waited only on closeWg (runWriter); the fan-out goroutines spawned by the
// final flush were untracked, so Close could return and close the spill file
// while a sink was still running — a goroutine leak and use-after-close risk.
func TestCollector_CloseWaitsForSinks(t *testing.T) {
	t.Parallel()

	store := newTestSQLiteStore(t)
	sink := &blockingSink{
		release: make(chan struct{}),
		called:  make(chan struct{}, 1),
	}
	c := NewCollector(store, nil, []AlertSink{sink}, slog.Default(), CollectorConfig{
		ChannelCap:    8,
		BatchSize:     1, // flush on the very first event
		BatchInterval: 10 * time.Millisecond,
	})
	c.Start(context.Background())

	ctx := context.Background()
	require.NoError(t, c.Enqueue(ctx, &UserActivity{
		Ts:         time.Now().UnixMilli(),
		UserID:     "u1",
		UserIDType: UserIDTypePlatform,
		Platform:   PlatformTest,
		Action:     ActionAuthLogin,
		Outcome:    OutcomeSuccess,
		DetailJSON: `{}`,
	}))

	// Wait until the sink has been entered (the fan-out goroutine is in flight
	// and blocked on the latch). This is the channel-signal equivalent of
	// "the sink is currently running" — no time.Sleep per AGENTS.md.
	select {
	case <-sink.called:
	case <-time.After(2 * time.Second):
		t.Fatal("sink was never called before timeout")
	}

	// Close must NOT return while the sink is still blocked. Drive Close in a
	// goroutine and assert it hasn't completed after a short window with the
	// latch still held.
	closeDone := make(chan error, 1)
	go func() { closeDone <- c.Close(context.Background()) }()
	select {
	case <-closeDone:
		t.Fatal("Close returned before in-flight sink finished (I3 regression)")
	case <-time.After(100 * time.Millisecond):
		// expected: Close is still blocked on sinkWg
	}

	// Release the sink; Close should now complete promptly.
	close(sink.release)
	select {
	case <-closeDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not return after sink was released")
	}
}
