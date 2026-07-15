package gateway

import (
	"context"
	"errors"
	"sync"
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

func TestHub_ReleaseSeqWaitsForDurableProducer(t *testing.T) {
	t.Parallel()
	h := newTestHub(t)
	releaseProducer := h.BeginSeqOperation("s1")
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
