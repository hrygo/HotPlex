package gateway

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSeqGen_Init_ContinuesFromStart(t *testing.T) {
	t.Parallel()
	g := NewSeqGen()
	g.Init("s1", 100)
	require.Equal(t, int64(101), g.Next("s1"))
	require.Equal(t, int64(102), g.Next("s1"))
}

func TestSeqGen_Init_DoesNotRegress(t *testing.T) {
	t.Parallel()
	g := NewSeqGen()
	g.Init("s1", 100)
	// A later Init with a smaller value (e.g. a concurrent hydrate racing with
	// an in-flight Next) must NOT lower the floor.
	g.Init("s1", 50)
	require.Equal(t, int64(101), g.Next("s1"))
}

func TestSeqGen_Init_EmptySession(t *testing.T) {
	t.Parallel()
	g := NewSeqGen()
	// A fresh session with no persisted events hydrates from 0 → Next starts at 1.
	g.Init("fresh", 0)
	require.Equal(t, int64(1), g.Next("fresh"))
}

func TestSeqGen_Init_BeforeNextIsEquivalent(t *testing.T) {
	t.Parallel()
	g := NewSeqGen()
	// Init on a session that has never called Next must seed the counter.
	g.Init("s1", 9899)
	require.Equal(t, int64(9900), g.Next("s1"))
	require.Equal(t, int64(9900), g.Peek("s1"))
}

func TestSeqGen_Init_ConcurrentWithNext(t *testing.T) {
	t.Parallel()
	g := NewSeqGen()
	// Simulate an in-flight counter (e.g. an init_ack already took seq=1).
	require.Equal(t, int64(1), g.Next("s1"))

	var wg sync.WaitGroup
	// Concurrent Init raising the floor to 1000.
	for range 50 {
		wg.Go(func() { g.Init("s1", 1000) })
	}
	// Concurrent Next callers.
	for range 50 {
		wg.Go(func() { _ = g.Next("s1") })
	}
	wg.Wait()
	// After all Init(1000) settle, the next seq must be strictly above 1000
	// (Init raised the floor; Nexts only go up from there).
	require.Greater(t, g.Next("s1"), int64(1000))
}

// mockSeqHydrator implements SeqHydrator for Hub.EnsureSeqHydrated tests.
type mockSeqHydrator struct {
	seq   int64
	err   error
	calls int
}

func (m *mockSeqHydrator) LatestSeq(_ context.Context, _ string) (int64, error) {
	m.calls++
	return m.seq, m.err
}

func TestHub_EnsureSeqHydrated(t *testing.T) {
	t.Parallel()

	t.Run("nil hydrator is no-op", func(t *testing.T) {
		t.Parallel()
		h := newTestHub(t)
		// No SetSeqHydrator → nil; Next starts at 1 (pre-issue-#879 behavior).
		require.NoError(t, h.EnsureSeqHydrated("s1"))
		require.Equal(t, int64(1), h.NextSeq("s1"))
	})

	t.Run("seeds counter from persisted seq", func(t *testing.T) {
		t.Parallel()
		h := newTestHub(t)
		h.SetSeqHydrator(&mockSeqHydrator{seq: 9899})
		require.NoError(t, h.EnsureSeqHydrated("s1"))
		// Reconnect after a session that persisted 9899 events → next seq is 9900,
		// not 1 (regression: seq collision + buried new events, issue #879).
		require.Equal(t, int64(9900), h.NextSeq("s1"))
	})

	t.Run("zero persisted seq starts at 1", func(t *testing.T) {
		t.Parallel()
		h := newTestHub(t)
		h.SetSeqHydrator(&mockSeqHydrator{seq: 0})
		require.NoError(t, h.EnsureSeqHydrated("fresh"))
		require.Equal(t, int64(1), h.NextSeq("fresh"))
	})

	t.Run("db error fails closed", func(t *testing.T) {
		t.Parallel()
		h := newTestHub(t)
		h.SetSeqHydrator(&mockSeqHydrator{err: errors.New("db down")})
		err := h.EnsureSeqHydrated("s1")
		require.ErrorContains(t, err, "db down")
		require.Equal(t, int64(0), h.NextSeqPeek("s1"))
	})

	t.Run("initialized session skips database on reconnect", func(t *testing.T) {
		t.Parallel()
		h := newTestHub(t)
		hydrator := &mockSeqHydrator{seq: 10}
		h.SetSeqHydrator(hydrator)
		require.NoError(t, h.EnsureSeqHydrated("s1"))
		require.Equal(t, int64(11), h.NextSeq("s1"))

		hydrator.err = errors.New("db down")
		require.NoError(t, h.EnsureSeqHydrated("s1"))
		require.Equal(t, 1, hydrator.calls)
		require.Equal(t, int64(12), h.NextSeq("s1"))
	})
}

func TestHub_ForgetSeqAllowsFreshHydration(t *testing.T) {
	t.Parallel()
	h := newTestHub(t)
	hydrator := &mockSeqHydrator{seq: 10}
	h.SetSeqHydrator(hydrator)
	require.NoError(t, h.EnsureSeqHydrated("s1"))
	require.Equal(t, int64(11), h.NextSeq("s1"))

	h.ForgetSeq("s1")
	hydrator.seq = 20
	require.NoError(t, h.EnsureSeqHydrated("s1"))
	require.Equal(t, 2, hydrator.calls)
	require.Equal(t, int64(21), h.NextSeq("s1"))
}

// mockSeqFlusher records FlushSession calls for test assertions.
type mockSeqFlusher struct {
	mu    sync.Mutex
	calls []string
}

func (m *mockSeqFlusher) FlushSession(sessionID string) error {
	m.mu.Lock()
	m.calls = append(m.calls, sessionID)
	m.mu.Unlock()
	return nil
}

// TestHub_EnsureSeqHydratedWaitsForConcurrentProducer verifies the write-lock
// fence: EnsureSeqHydrated blocks behind a concurrent seq operation (RLock)
// and only proceeds after the producer releases. Without the write-lock fence,
// hydration could read LatestSeq while an old forwarder is still allocating
// seqs, leading to a stale counter and UNIQUE constraint violations (issue #894).
func TestHub_EnsureSeqHydratedWaitsForConcurrentProducer(t *testing.T) {
	t.Parallel()
	h := newTestHub(t)
	h.SetSeqHydrator(&mockSeqHydrator{seq: 100})

	releaseProducer, ok := h.BeginSeqOperation("s1")
	require.True(t, ok)

	hydrated := make(chan error, 1)
	go func() {
		hydrated <- h.EnsureSeqHydrated("s1")
	}()

	require.Never(t, func() bool {
		select {
		case <-hydrated:
			return true
		default:
			return false
		}
	}, 100*time.Millisecond, 10*time.Millisecond)

	releaseProducer()
	require.NoError(t, <-hydrated)
	require.Equal(t, int64(101), h.NextSeq("s1"))
}

// TestHub_EnsureSeqHydratedCallsFlusherBeforeHydrate verifies that EnsureSeqHydrated
// drains the collector flush before reading LatestSeq. Without this, events whose
// seq was already allocated but not yet committed to the DB would be invisible,
// causing the new counter to collide with in-flight seqs (issue #894).
func TestHub_EnsureSeqHydratedCallsFlusherBeforeHydrate(t *testing.T) {
	t.Parallel()
	h := newTestHub(t)
	hydrator := &mockSeqHydrator{seq: 50}
	flusher := &mockSeqFlusher{}
	h.SetSeqHydrator(hydrator)
	h.SetSeqFlusher(flusher)

	require.NoError(t, h.EnsureSeqHydrated("s1"))

	flusher.mu.Lock()
	require.Contains(t, flusher.calls, "s1")
	flusher.mu.Unlock()

	require.Equal(t, int64(51), h.NextSeq("s1"))

	// Second call skips both flusher and hydrator (already initialized).
	hydrator.err = errors.New("should not be called")
	require.NoError(t, h.EnsureSeqHydrated("s1"))
	require.Equal(t, 1, hydrator.calls)
	flusher.mu.Lock()
	require.Len(t, flusher.calls, 1)
	flusher.mu.Unlock()
}

func TestHub_ReleaseSeqWaitsForDurableProducer(t *testing.T) {
	t.Parallel()
	h := newTestHub(t)
	releaseProducer, ok := h.BeginSeqOperation("s1")
	require.True(t, ok)
	require.Equal(t, int64(1), h.NextSeq("s1"))

	drainEntered := make(chan struct{})
	releaseDone := make(chan error, 1)
	go func() {
		releaseDone <- h.ReleaseSeq("s1", func() error {
			close(drainEntered)
			return nil
		})
	}()

	require.Never(t, func() bool {
		select {
		case <-drainEntered:
			return true
		default:
			return false
		}
	}, 50*time.Millisecond, 5*time.Millisecond)

	releaseProducer()
	require.NoError(t, <-releaseDone)
	require.Equal(t, int64(0), h.NextSeqPeek("s1"))
}

func TestHub_RejectsProducerAfterDurableSessionDeletion(t *testing.T) {
	t.Parallel()
	h := newTestHub(t)
	var exists atomic.Bool
	exists.Store(true)
	h.SetSeqSessionExists(func(string) bool { return exists.Load() })

	require.Equal(t, int64(1), h.NextSeq("s1"))
	exists.Store(false)
	require.NoError(t, h.ReleaseSeq("s1", nil))

	require.Equal(t, int64(0), h.NextSeq("s1"))
	require.Equal(t, int64(0), h.NextSeqPeek("s1"))
	release, ok := h.BeginSeqOperation("s1")
	require.False(t, ok)
	release()

	// The synchronous deleted-state notification is the sole lifecycle bypass
	// and still participates in the release barrier.
	require.Equal(t, int64(1), h.NextSeqBeforeRelease("s1"))
	require.NoError(t, h.ReleaseSeq("s1", nil))
	require.Equal(t, int64(0), h.NextSeqPeek("s1"))
}
