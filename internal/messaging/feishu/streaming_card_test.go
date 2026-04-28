package feishu

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// ─── Write ────────────────────────────────────────────────────────────────────

func TestStreamingCardController_Write(t *testing.T) {
	t.Parallel()
	c := NewStreamingCardController(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	err := c.Write("hello")
	require.NoError(t, err)

	c.mu.Lock()
	buf := c.buf.String()
	c.mu.Unlock()
	require.Equal(t, "hello", buf)
}

func TestStreamingCardController_Write_MultipleAppends(t *testing.T) {
	t.Parallel()
	c := NewStreamingCardController(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	require.NoError(t, c.Write("hello "))
	require.NoError(t, c.Write("world"))

	c.mu.Lock()
	buf := c.buf.String()
	c.mu.Unlock()
	require.Equal(t, "hello world", buf)
}

func TestStreamingCardController_Write_TracksBytes(t *testing.T) {
	t.Parallel()
	c := NewStreamingCardController(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	require.NoError(t, c.Write("abc"))
	require.NoError(t, c.Write("defgh"))

	c.mu.Lock()
	written := c.bytesWritten
	c.mu.Unlock()
	require.Equal(t, int64(8), written)
}

func TestStreamingCardController_Write_SetsStreamStart(t *testing.T) {
	t.Parallel()
	c := NewStreamingCardController(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// Reset streamStartTime to zero to test that Write sets it.
	c.mu.Lock()
	c.streamStartTime = time.Time{}
	c.mu.Unlock()

	require.NoError(t, c.Write("data"))

	c.mu.Lock()
	startTime := c.streamStartTime
	c.mu.Unlock()
	require.False(t, startTime.IsZero(), "Write should set streamStartTime if zero")
}

func TestStreamingCardController_Write_TTLExpiry(t *testing.T) {
	t.Parallel()
	c := NewStreamingCardController(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// Set streamStartTime far enough in the past to exceed TTL.
	c.mu.Lock()
	c.streamStartTime = time.Now().Add(-StreamTTL - time.Second)
	c.mu.Unlock()

	err := c.Write("should fail")
	require.Error(t, err)
	require.Contains(t, err.Error(), "streaming expired")
}

func TestStreamingCardController_Write_SecondWriteAfterExpiry(t *testing.T) {
	t.Parallel()
	c := NewStreamingCardController(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	c.mu.Lock()
	c.streamStartTime = time.Now().Add(-StreamTTL - time.Second)
	c.mu.Unlock()

	// First write fails.
	err := c.Write("fail")
	require.Error(t, err)

	// Second write also fails (streamExpired flag set).
	err = c.Write("fail again")
	require.Error(t, err)
}

// ─── Flush (paths that don't need lark client) ───────────────────────────────

func TestStreamingCardController_Flush_Unchanged(t *testing.T) {
	t.Parallel()
	limiter := NewFeishuRateLimiter()
	t.Cleanup(func() { limiter.Stop() })
	c := NewStreamingCardController(nil, limiter, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// Write content and set lastFlushed to the same value.
	c.mu.Lock()
	c.buf.WriteString("hello")
	c.lastFlushed = "hello"
	c.mu.Unlock()

	// Flush should skip (content unchanged).
	err := c.Flush(context.Background())
	require.NoError(t, err)
}

func TestStreamingCardController_Flush_NoCardKitNoMsgID(t *testing.T) {
	t.Parallel()
	limiter := NewFeishuRateLimiter()
	t.Cleanup(func() { limiter.Stop() })
	c := NewStreamingCardController(nil, limiter, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// Write some content, disable cardKit so it skips to IM patch path.
	c.mu.Lock()
	c.buf.WriteString("new content")
	c.cardKitOK = false
	c.mu.Unlock()

	// Flush with cardKit disabled and no msgID → IM patch skipped (no msgID).
	err := c.Flush(context.Background())
	require.NoError(t, err)
}

func TestStreamingCardController_Flush_WithLimiter_CardKitDegraded(t *testing.T) {
	t.Parallel()
	limiter := NewFeishuRateLimiter()
	t.Cleanup(func() { limiter.Stop() })

	c := NewStreamingCardController(nil, limiter, slog.New(slog.NewTextHandler(io.Discard, nil)))

	c.mu.Lock()
	c.buf.WriteString("new content")
	c.cardKitOK = false // cardKit disabled
	c.mu.Unlock()

	// Has limiter but cardKit disabled and no msgID → IM patch skipped.
	err := c.Flush(context.Background())
	require.NoError(t, err)
}

// ─── Expired ──────────────────────────────────────────────────────────────────

func TestStreamingCardController_Expired_ZeroStartTime(t *testing.T) {
	t.Parallel()
	c := NewStreamingCardController(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// Reset to zero start time.
	c.mu.Lock()
	c.streamStartTime = time.Time{}
	c.mu.Unlock()

	// Zero start time should return false (not expired).
	require.False(t, c.Expired())
}

func TestStreamingCardController_Expired_AfterTTL(t *testing.T) {
	t.Parallel()
	c := NewStreamingCardController(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	c.mu.Lock()
	c.streamStartTime = time.Now().Add(-StreamTTL - time.Minute)
	c.mu.Unlock()

	require.True(t, c.Expired())
}

func TestStreamingCardController_Expired_JustUnderTTL(t *testing.T) {
	t.Parallel()
	c := NewStreamingCardController(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	c.mu.Lock()
	c.streamStartTime = time.Now().Add(-StreamTTL + time.Minute)
	c.mu.Unlock()

	require.False(t, c.Expired())
}

// ─── MsgID ────────────────────────────────────────────────────────────────────

func TestStreamingCardController_MsgID_Set(t *testing.T) {
	t.Parallel()
	c := NewStreamingCardController(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	c.mu.Lock()
	c.msgID = "msg_abc"
	c.mu.Unlock()

	require.Equal(t, "msg_abc", c.MsgID())
}

// ─── Close edge cases (paths without lark client) ─────────────────────────────

func TestStreamingCardController_Close_WithBufferContent(t *testing.T) {
	t.Parallel()
	c := NewStreamingCardController(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// Write some content.
	require.NoError(t, c.Write("Hello world"))

	// Transition to creating first, then Close will transition to completed.
	require.True(t, c.transition(PhaseCreating))
	require.True(t, c.transition(PhaseStreaming))

	err := c.Close(context.Background())
	require.NoError(t, err)
	require.Equal(t, PhaseCompleted, c.getPhase())
}

func TestStreamingCardController_Close_IntegrityFail(t *testing.T) {
	t.Parallel()
	c := NewStreamingCardController(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	require.True(t, c.transition(PhaseCreating))
	require.True(t, c.transition(PhaseStreaming))

	// Simulate: many bytes written, very few flushed → integrity check fails.
	c.mu.Lock()
	c.bytesWritten = 10000
	c.bytesFlushed = 100 // only 1% flushed
	c.buf.WriteString("test content")
	c.mu.Unlock()

	err := c.Close(context.Background())
	require.NoError(t, err)
}

func TestStreamingCardController_Close_IntegrityPass(t *testing.T) {
	t.Parallel()
	c := NewStreamingCardController(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	require.True(t, c.transition(PhaseCreating))
	require.True(t, c.transition(PhaseStreaming))

	// Simulate: bytes written ≈ bytes flushed → integrity OK.
	c.mu.Lock()
	c.bytesWritten = 1000
	c.bytesFlushed = 950 // 95% flushed → passes 90% threshold
	c.buf.WriteString("content")
	c.mu.Unlock()

	err := c.Close(context.Background())
	require.NoError(t, err)
}

func TestStreamingCardController_Close_CardKitDegraded_NoCardID_NoMsgID(t *testing.T) {
	t.Parallel()
	c := NewStreamingCardController(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	require.True(t, c.transition(PhaseCreating))
	require.True(t, c.transition(PhaseStreaming))

	c.mu.Lock()
	c.cardKitOK = false // degraded
	c.cardID = ""       // no cardID → no cardKit flush
	c.msgID = ""        // no msgID → no IM patch flush
	c.buf.WriteString("final content")
	c.streamingActive = false
	c.mu.Unlock()

	err := c.Close(context.Background())
	require.NoError(t, err)
}

func TestStreamingCardController_Close_StreamingActive_NoCardID(t *testing.T) {
	t.Parallel()
	c := NewStreamingCardController(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	require.True(t, c.transition(PhaseCreating))
	require.True(t, c.transition(PhaseStreaming))

	c.mu.Lock()
	c.streamingActive = true
	c.cardID = ""   // no cardID → can't disable streaming
	c.msgID = ""    // no msgID → no IM patch
	c.cardKitOK = false
	c.buf.WriteString("content")
	c.mu.Unlock()

	err := c.Close(context.Background())
	require.NoError(t, err)
	// streamingActive should be false after Close even without cardID.
	c.mu.Lock()
	require.False(t, c.streamingActive)
	c.mu.Unlock()
}

// ─── Abort (paths without lark client) ────────────────────────────────────────

func TestStreamingCardController_Abort_FromStreaming(t *testing.T) {
	t.Parallel()
	c := NewStreamingCardController(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	require.True(t, c.transition(PhaseCreating))
	require.True(t, c.transition(PhaseStreaming))

	err := c.Abort(context.Background())
	require.NoError(t, err)
	require.Equal(t, PhaseAborted, c.getPhase())
}

func TestStreamingCardController_Abort_NoCardID_NoStreaming(t *testing.T) {
	t.Parallel()
	c := NewStreamingCardController(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	require.True(t, c.transition(PhaseCreating))
	require.True(t, c.transition(PhaseStreaming))

	c.mu.Lock()
	c.cardID = ""       // no cardID → disableStreaming skipped
	c.msgID = ""        // no msgID → sendAbortMessage skipped
	c.streamingActive = false
	c.mu.Unlock()

	err := c.Abort(context.Background())
	require.NoError(t, err)
	require.Equal(t, PhaseAborted, c.getPhase())
}

func TestStreamingCardController_Abort_NoStreamingActive_NoMsgID(t *testing.T) {
	t.Parallel()
	c := NewStreamingCardController(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	require.True(t, c.transition(PhaseCreating))
	require.True(t, c.transition(PhaseStreaming))

	c.mu.Lock()
	c.streamingActive = false
	c.msgID = "" // no msgID → sendAbortMessage skipped
	c.mu.Unlock()

	err := c.Abort(context.Background())
	require.NoError(t, err)
}

// ─── EnsureCard transitions ───────────────────────────────────────────────────

func TestStreamingCardController_EnsureCard_InvalidPhase(t *testing.T) {
	t.Parallel()
	c := NewStreamingCardController(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// Already in PhaseStreaming, can't transition to Creating.
	c.phase.Store(int32(PhaseStreaming))

	err := c.EnsureCard(context.Background(), "chat123", "p2p", "", "initial")
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot transition")
}

// ─── Concurrent Close ─────────────────────────────────────────────────────────

func TestStreamingCardController_ConcurrentClose(t *testing.T) {
	t.Parallel()
	c := NewStreamingCardController(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	require.True(t, c.transition(PhaseCreating))
	require.True(t, c.transition(PhaseStreaming))

	done := make(chan error, 5)
	for range 5 {
		go func() {
			done <- c.Close(context.Background())
		}()
	}

	successCount := 0
	for range 5 {
		err := <-done
		if err == nil {
			successCount++
		}
	}
	// All Close calls should return nil (idempotent), but only one actually transitions.
	require.Equal(t, 5, successCount)
}

// ─── Write + Flush integration ────────────────────────────────────────────────

func TestStreamingCardController_WriteThenFlush(t *testing.T) {
	t.Parallel()
	limiter := NewFeishuRateLimiter()
	t.Cleanup(func() { limiter.Stop() })
	c := NewStreamingCardController(nil, limiter, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// Disable cardKit so flush skips to IM patch path (no msgID → skip).
	c.mu.Lock()
	c.cardKitOK = false
	c.mu.Unlock()

	require.NoError(t, c.Write("chunk1"))
	require.NoError(t, c.Write(" chunk2"))

	err := c.Flush(context.Background())
	require.NoError(t, err)
}

// ─── Flush: early exit paths ────────────────────────────────────────────────────

func TestStreamingCardController_Flush_CardKitDegraded_NoCardID(t *testing.T) {
	t.Parallel()
	limiter := NewFeishuRateLimiter()
	t.Cleanup(func() { limiter.Stop() })
	c := NewStreamingCardController(nil, limiter, slog.New(slog.NewTextHandler(io.Discard, nil)))

	require.NoError(t, c.Write("content"))

	// CardKit degraded, no msgID → both paths skip entirely.
	c.mu.Lock()
	c.cardKitOK = false
	c.mu.Unlock()

	err := c.Flush(context.Background())
	require.NoError(t, err)
}

func TestStreamingCardController_Flush_IMPatchNoMsgID(t *testing.T) {
	t.Parallel()
	limiter := NewFeishuRateLimiter()
	t.Cleanup(func() { limiter.Stop() })
	c := NewStreamingCardController(nil, limiter, slog.New(slog.NewTextHandler(io.Discard, nil)))

	require.NoError(t, c.Write("data"))

	// CardKit disabled, no msgID → both paths skip.
	c.mu.Lock()
	c.cardKitOK = false
	c.mu.Unlock()

	err := c.Flush(context.Background())
	require.NoError(t, err)
}

// ─── EnsureCard: nil client path ────────────────────────────────────────────────

func TestStreamingCardController_EnsureCard_TransitionFail(t *testing.T) {
	t.Parallel()
	limiter := NewFeishuRateLimiter()
	t.Cleanup(func() { limiter.Stop() })
	c := NewStreamingCardController(nil, limiter, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// Force to streaming phase so transition to creating fails.
	c.phase.Store(int32(PhaseCompleted))

	err := c.EnsureCard(context.Background(), "chat1", "p2p", "", "hello")
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot transition")
}

// ─── Close: more paths ─────────────────────────────────────────────────────────

func TestStreamingCardController_Close_NoContent(t *testing.T) {
	t.Parallel()
	c := NewStreamingCardController(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	// Close with empty buffer and not created.
	err := c.Close(context.Background())
	require.NoError(t, err)
}

// ─── Abort: more paths ─────────────────────────────────────────────────────────

func TestStreamingCardController_Abort_NotStreaming(t *testing.T) {
	t.Parallel()
	c := NewStreamingCardController(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// Not in streaming phase → early return.
	err := c.Abort(context.Background())
	require.NoError(t, err)
}

// ─── transition: edge cases ─────────────────────────────────────────────────────

func TestStreamingCardController_Transition_FromCompleted(t *testing.T) {
	t.Parallel()
	c := NewStreamingCardController(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	c.phase.Store(int32(PhaseCompleted))

	// Completed → Creating should fail.
	require.False(t, c.transition(PhaseCreating))
}

func TestStreamingCardController_PhaseString(t *testing.T) {
	t.Parallel()
	tests := []struct {
		phase CardPhase
		want  string
	}{
		{PhaseIdle, "idle"},
		{PhaseCreating, "creating"},
		{PhaseStreaming, "streaming"},
		{PhaseCompleted, "completed"},
		{PhaseAborted, "aborted"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, tt.phase.String())
		})
	}
}
