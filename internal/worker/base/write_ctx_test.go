package base

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWriteWithCtx_ReturnsWriteResult(t *testing.T) {
	t.Parallel()
	want := errors.New("write failed")
	got := WriteWithCtx(context.Background(), func() error {
		return want
	}, 0)
	require.ErrorIs(t, got, want, "write error propagates when ctx is not cancelled")
}

func TestWriteWithCtx_ReturnsNilOnSuccess(t *testing.T) {
	t.Parallel()
	got := WriteWithCtx(context.Background(), func() error {
		return nil
	}, 0)
	require.NoError(t, got)
}

func TestWriteWithCtx_AlreadyCancelledReturnsCtxErr(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	called := make(chan struct{}, 1)
	_ = WriteWithCtx(ctx, func() error {
		close(called)
		return nil
	}, 0)

	// writeFn should not even be invoked when ctx is already done (fast path).
	select {
	case <-called:
		t.Fatal("writeFn called despite ctx already cancelled")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestWriteWithCtx_WriteCompletesDuringGracePeriod(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	customErr := errors.New("epipe")

	// writeFn cancels ctx itself then returns a real error quickly. WriteWithCtx
	// should return the real error (received during the grace window) rather
	// than ctx.Err().
	err := WriteWithCtx(ctx, func() error {
		cancel() // cancel while write is "in flight"
		return customErr
	}, 5*time.Second)
	require.ErrorIs(t, err, customErr, "real write error returned when write finishes within grace period")
}

func TestWriteWithCtx_DefaultFallbackConstant(t *testing.T) {
	t.Parallel()
	// fallback=0 should select DefaultWriteTimeout (30s), not fail or panic.
	// We only verify the constant is sane; exercising the full 30s wait is
	// too slow for unit tests.
	require.Equal(t, 30*time.Second, DefaultWriteTimeout)
}

func TestWriteWithCtx_OrphanReturnsCtxErrWithinFallback(t *testing.T) {
	// Not parallel: uses timing-sensitive goroutine coordination.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// writeFn blocks on a channel we control, simulating a stalled syscall.Write.
	// We close the channel AFTER asserting, via defer, so the orphaned goroutine
	// finishes and doesn't leak past the test.
	release := make(chan struct{})
	defer close(release)

	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- WriteWithCtx(ctx, func() error {
			close(started)
			<-release
			return errors.New("finished after orphan")
		}, 50*time.Millisecond)
	}()

	// Wait for writeFn to start, then cancel ctx while it's "blocked writing".
	<-started
	cancel()

	// WriteWithCtx should return ctx.Err() within fallback+epsilon (50ms + slop).
	// It must NOT wait for writeFn to finish (that would defeat the purpose).
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled, "orphan path returns ctx.Err() after fallback")
	case <-time.After(2 * time.Second):
		t.Fatal("WriteWithCtx did not return within 2s of ctx cancel + fallback")
	}
}
