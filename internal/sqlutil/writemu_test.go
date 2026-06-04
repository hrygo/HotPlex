package sqlutil

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWriteMu_WithLock_Nil(t *testing.T) {
	var mu *WriteMu
	err := mu.WithLock(func() error {
		return nil
	})
	require.NoError(t, err)
}

func TestWriteMu_WithLock_ReturnsError(t *testing.T) {
	mu := NewWriteMu(DialectSQLite)
	expected := errors.New("test error")
	err := mu.WithLock(func() error {
		return expected
	})
	require.ErrorIs(t, err, expected)
}

func TestWriteMu_WithLock_SerializesAccess(t *testing.T) {
	mu := NewWriteMu(DialectSQLite)
	var concurrent atomic.Int32
	var maxConcurrent atomic.Int32

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := mu.WithLock(func() error {
				cur := concurrent.Add(1)
				for {
					old := maxConcurrent.Load()
					if cur <= old || maxConcurrent.CompareAndSwap(old, cur) {
						break
					}
				}
				concurrent.Add(-1)
				return nil
			})
			require.NoError(t, err)
		}()
	}
	wg.Wait()

	require.Equal(t, int32(1), maxConcurrent.Load(), "WithLock should serialize all writes")
}

func TestWriteMu_WithLock_Nil_Concurrent(t *testing.T) {
	var mu *WriteMu
	var count atomic.Int32

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = mu.WithLock(func() error {
				count.Add(1)
				return nil
			})
		}()
	}
	wg.Wait()

	require.Equal(t, int32(50), count.Load(), "nil WriteMu should still execute all closures")
}
