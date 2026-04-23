package feishu

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNewReconnectBackoff(t *testing.T) {
	t.Parallel()
	b := newReconnectBackoff(2*time.Second, 60*time.Second)
	require.Equal(t, 0, b.attempt)
	require.Equal(t, 2*time.Second, b.baseDelay)
	require.Equal(t, 60*time.Second, b.maxDelay)
}

func TestReconnectBackoff_Next(t *testing.T) {
	t.Parallel()
	b := newReconnectBackoff(time.Second, 60*time.Second)

	// First call: attempt=0, delay=1s, half=500ms, jitter in [0,500ms)
	// result in [500ms, 1s)
	d1 := b.Next()
	require.GreaterOrEqual(t, d1, 500*time.Millisecond)
	require.Less(t, d1, time.Second)

	// Second call: attempt=1, delay=2s, half=1s, jitter in [0,1s)
	// result in [1s, 2s)
	d2 := b.Next()
	require.GreaterOrEqual(t, d2, time.Second)
	require.Less(t, d2, 2*time.Second)

	// Delays should grow
	require.Greater(t, d2, d1)

	// Third call: attempt=2, delay=4s, half=2s
	// result in [2s, 4s)
	d3 := b.Next()
	require.GreaterOrEqual(t, d3, 2*time.Second)
	require.Less(t, d3, 4*time.Second)
	require.Greater(t, d3, d2)
}

func TestReconnectBackoff_Next_CapsAtMax(t *testing.T) {
	t.Parallel()
	// Use very low max to easily verify capping
	b := newReconnectBackoff(time.Second, 2*time.Second)

	// First: base=1s, attempt=0, delay=1s < max=2s → half=500ms, result in [500ms,1s)
	d1 := b.Next()
	require.LessOrEqual(t, d1, time.Second)

	// Second: base=1s, attempt=1, delay=2s == max → half=1s, result in [1s,2s)
	d2 := b.Next()
	require.GreaterOrEqual(t, d2, time.Second)
	require.LessOrEqual(t, d2, 2*time.Second)

	// Third: base=1s, attempt=2, delay=4s > max → delay=max=2s, half=1s, result in [1s,2s)
	d3 := b.Next()
	require.GreaterOrEqual(t, d3, time.Second)
	require.LessOrEqual(t, d3, 2*time.Second)

	// After many calls, still capped at max
	for i := 0; i < 20; i++ {
		d := b.Next()
		require.GreaterOrEqual(t, d, time.Second, "attempt %d: delay should be >= 1s", i)
		require.LessOrEqual(t, d, 2*time.Second, "attempt %d: delay should not exceed max", i)
	}
}

func TestReconnectBackoff_Reset(t *testing.T) {
	t.Parallel()
	b := newReconnectBackoff(time.Second, 60*time.Second)

	// Advance a few times
	b.Next()
	b.Next()
	b.Next()

	// Reset back to zero
	b.Reset()
	require.Equal(t, 0, b.attempt)

	// Next should return to base delay range again
	d := b.Next()
	require.GreaterOrEqual(t, d, 500*time.Millisecond)
	require.Less(t, d, time.Second)
}

func TestReconnectBackoff_ConcurrentSafety(t *testing.T) {
	t.Parallel()
	b := newReconnectBackoff(time.Millisecond, 100*time.Millisecond)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d := b.Next()
			require.GreaterOrEqual(t, d, time.Duration(0))
			require.LessOrEqual(t, d, 100*time.Millisecond)
		}()
	}
	wg.Wait()
}
