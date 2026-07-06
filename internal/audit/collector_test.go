package audit

import (
	"context"
	"log/slog"
	"path/filepath"
	"sync"
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
	c.Start()
	t.Cleanup(func() { _ = c.Close() })
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
	require.NoError(t, c.Close())

	// After close, spill file should be empty (all events replayed and flushed)
	remaining, err := spill.ReadAll()
	require.NoError(t, err)
	require.Empty(t, remaining, "spill file should be empty after drain")

	// All events should be in the database.
	// Use require.Eventually because the test may interleave with other parallel tests
	// that share the same WriteMu (e.g., session tests), causing flush latency.
	require.Eventually(t, func() bool {
		rows, err := store.Query(context.Background(), Query{})
		if err != nil {
			return false
		}
		return len(rows) == n
	}, 5*time.Second, 50*time.Millisecond, "all %d events should be in DB", n)

	rows, err := store.Query(context.Background(), Query{})
	require.NoError(t, err)

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
	require.NoError(t, c.Close())

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

	require.NoError(t, c.Close())

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

	require.NoError(t, c.Close())
	require.NoError(t, c.Close()) // second close should be no-op
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
	c.Start()
	t.Cleanup(func() { _ = c.Close() })

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

// PlatformTest is a convenience constant for tests (not in the public API).
const PlatformTest = "test"
