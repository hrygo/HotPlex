package opencodeserver

import (
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRingBuffer(t *testing.T) {
	t.Parallel()

	t.Run("empty returns nil", func(t *testing.T) {
		t.Parallel()
		rb := newRingBuffer(3)
		require.Nil(t, rb.Lines())
	})

	t.Run("under capacity returns all in order", func(t *testing.T) {
		t.Parallel()
		rb := newRingBuffer(3)
		rb.Add("a")
		rb.Add("b")
		require.Equal(t, []string{"a", "b"}, rb.Lines())
	})

	t.Run("exactly capacity", func(t *testing.T) {
		t.Parallel()
		rb := newRingBuffer(3)
		rb.Add("a")
		rb.Add("b")
		rb.Add("c")
		require.Equal(t, []string{"a", "b", "c"}, rb.Lines())
	})

	t.Run("over capacity keeps most recent N in order", func(t *testing.T) {
		t.Parallel()
		rb := newRingBuffer(3)
		rb.Add("a")
		rb.Add("b")
		rb.Add("c")
		rb.Add("d")
		rb.Add("e")
		require.Equal(t, []string{"c", "d", "e"}, rb.Lines())
	})

	t.Run("zero capacity returns nil and drops all", func(t *testing.T) {
		t.Parallel()
		rb := newRingBuffer(0)
		rb.Add("a")
		require.Nil(t, rb.Lines())
	})
}

func TestRingBuffer_Reset(t *testing.T) {
	t.Parallel()

	rb := newRingBuffer(3)
	rb.Add("a")
	rb.Add("b")
	require.Equal(t, []string{"a", "b"}, rb.Lines())

	rb.Reset()
	require.Nil(t, rb.Lines(), "must be empty immediately after reset")

	// Buffer must be reusable: head/n reset so new writes lay out in order.
	rb.Add("c")
	rb.Add("d")
	require.Equal(t, []string{"c", "d"}, rb.Lines())
}

func TestRingBuffer_Concurrent(t *testing.T) {
	t.Parallel()
	rb := newRingBuffer(100)
	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			rb.Add(strings.Repeat("x", n%10+1))
		}(i)
	}
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = rb.Lines()
		}()
	}
	wg.Wait()
	require.LessOrEqual(t, len(rb.Lines()), 100)
}
