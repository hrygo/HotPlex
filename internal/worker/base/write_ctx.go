package base

import (
	"context"
	"time"
)

// DefaultWriteTimeout is the fallback deadline for WriteWithCtx when ctx has
// no deadline. It bounds how long a stalled stdin write can block the caller
// after ctx cancellation — the goroutine performing the raw syscall cannot be
// interrupted, so we wait at most this long for it to finish before returning
// and orphaning it (it completes when the child process exits and the pipe
// read end closes, yielding EPIPE).
const DefaultWriteTimeout = 30 * time.Second

// WriteWithCtx runs writeFn in a goroutine and returns its result, but bails
// out when ctx is cancelled. It exists because writes to a child process's
// stdin (pipe) block at the OS level when the pipe buffer is full and the
// child stops reading — a condition no Go-level timeout can interrupt. The
// best we can do is stop waiting so the caller is not pinned forever, and let
// the orphaned goroutine finish whenever the OS unblocks it.
//
// The fallback timeout caps the wait after ctx cancellation: even if ctx is
// already done, we give the write a brief grace period to complete normally
// (so a write that finishes a millisecond after cancel still returns its real
// error rather than ctx.Err()). Pass 0 to use DefaultWriteTimeout.
//
// Callers that hold a mutex across writeFn must NOT release it on ctx
// cancellation while the goroutine is still writing — the goroutine would
// race with the next writer. Instead, callers should accept that the mutex
// stays held until the orphaned goroutine finishes (bounded by the child
// process lifetime). This matches the pattern already used by
// claudecode.Worker.Compact.
func WriteWithCtx(ctx context.Context, writeFn func() error, fallback time.Duration) error {
	if fallback <= 0 {
		fallback = DefaultWriteTimeout
	}

	// Fast path: if ctx is already done and we can return immediately without
	// even starting a write, do so. This avoids leaking a goroutine for a
	// caller that never had a chance to write.
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- writeFn()
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		// Give the write a brief grace period to finish normally so the
		// caller gets the real error (e.g. EPIPE) when possible. If it
		// doesn't finish, orphan the goroutine and return.
		timer := time.NewTimer(fallback)
		defer timer.Stop()
		select {
		case err := <-errCh:
			return err
		case <-timer.C:
			return ctx.Err()
		}
	}
}
