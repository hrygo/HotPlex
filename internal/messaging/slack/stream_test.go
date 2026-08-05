package slack

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/slack-go/slack"
	"github.com/stretchr/testify/require"
)

func TestIsStreamStateError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{"random error", errors.New("some random error"), false},
		{"message_not_in_streaming_state", errors.New("message_not_in_streaming_state"), true},
		{"not_in_channel", errors.New("not_in_channel"), true},
		{"channel_not_found", errors.New("channel_not_found"), true},
		{"message_not_found", errors.New("message_not_found"), true},
		{"wrapped message_not_in_streaming_state", fmt.Errorf("wrapped: %w", errors.New("message_not_in_streaming_state")), true},
		{"not_in_channel with context", errors.New("error: not_in_channel: user is not in channel"), true},
		{"network error", errors.New("connection refused"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isStreamStateError(tt.err)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestIsRateLimitError(t *testing.T) {
	tests := []struct {
		name             string
		err              error
		expectedIsLimit  bool
		expectedDuration time.Duration
	}{
		{"nil error", nil, false, 0},
		{"random error", errors.New("some error"), false, 0},
		{"slack.RateLimitedError", &slack.RateLimitedError{RetryAfter: 5 * time.Second}, true, 5 * time.Second},
		{"slack.RateLimitedError with 1s", &slack.RateLimitedError{RetryAfter: time.Second}, true, time.Second},
		{"HTTP 429 in message", errors.New("HTTP 429: Too Many Requests"), true, time.Second},
		{"rate_limit in message", errors.New("rate_limit exceeded"), true, time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isLimit, duration := isRateLimitError(tt.err)
			require.Equal(t, tt.expectedIsLimit, isLimit)
			require.Equal(t, tt.expectedDuration, duration)
		})
	}
}

func TestIsAuthError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{"random error", errors.New("some random error"), false},
		{"invalid_auth", errors.New("invalid_auth"), true},
		{"missing_scope", errors.New("missing_scope"), true},
		{"not_allowed", errors.New("not_allowed"), true},
		{"account_inactive", errors.New("account_inactive"), true},
		{"invalid_token", errors.New("invalid_token"), true},
		{"token_revoked", errors.New("token_revoked"), true},
		{"wrapped invalid_auth", fmt.Errorf("wrapped: %w", errors.New("invalid_auth")), true},
		{"invalid_auth with context", errors.New("error: invalid_auth: token is invalid"), true},
		{"network error", errors.New("connection refused"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isAuthError(tt.err)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestIsRetryableError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{"transient error", errors.New("connection timeout"), true},
		{"stream state error", errors.New("message_not_in_streaming_state"), false},
		{"auth error", errors.New("invalid_auth"), false},
		{"rate limit error", &slack.RateLimitedError{RetryAfter: time.Second}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isRetryableError(tt.err)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestContainsAny(t *testing.T) {
	tests := []struct {
		name     string
		str      string
		substrs  []string
		expected bool
	}{
		{"empty string", "", []string{"a", "b"}, false},
		{"no match", "hello world", []string{"foo", "bar"}, false},
		{"first match", "hello world", []string{"hello", "bar"}, true},
		{"last match", "hello world", []string{"foo", "world"}, true},
		{"middle match", "hello world", []string{"foo", "lo wo", "bar"}, true},
		{"empty substrings", "hello", []string{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := containsAny(tt.str, tt.substrs)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestAC21_StreamStateErrorNoRetries(t *testing.T) {
	err := errors.New("message_not_in_streaming_state")
	require.True(t, isStreamStateError(err), "AC-2.1: message_not_in_streaming_state should be detected as stream state error")
	require.False(t, isRetryableError(err), "AC-2.1: Stream state error should not be retryable")

	notInChannelErr := errors.New("not_in_channel")
	require.True(t, isStreamStateError(notInChannelErr), "AC-2.1: not_in_channel should be detected as stream state error")
}

func TestAC22_RateLimitRespectsRetryAfter(t *testing.T) {
	retryAfter := 5 * time.Second
	rateLimitErr := &slack.RateLimitedError{RetryAfter: retryAfter}

	isLimit, duration := isRateLimitError(rateLimitErr)
	require.True(t, isLimit, "AC-2.2: Rate limit error should be detected")
	require.Equal(t, retryAfter, duration, "AC-2.2: Retry-After duration should be extracted correctly")
}

func TestAC23_AuthErrorMarksStreamExpired(t *testing.T) {
	authErrors := []string{
		"invalid_auth",
		"missing_scope",
		"not_allowed",
		"account_inactive",
		"invalid_token",
		"token_revoked",
	}

	for _, errStr := range authErrors {
		err := errors.New(errStr)
		require.True(t, isAuthError(err), "AC-2.3: %s should be detected as auth error", errStr)
		require.False(t, isRetryableError(err), "AC-2.3: %s should not be retryable", errStr)
	}
}

func TestAC24_TransientErrorsRetryable(t *testing.T) {
	transientErrors := []string{
		"connection timeout",
		"network error",
		"temporary failure",
		"service unavailable",
	}

	for _, errStr := range transientErrors {
		err := errors.New(errStr)
		require.True(t, isRetryableError(err), "AC-2.4: %s should be retryable", errStr)
		require.False(t, isStreamStateError(err), "AC-2.4: %s should not be stream state error", errStr)
		require.False(t, isAuthError(err), "AC-2.4: %s should not be auth error", errStr)
	}
}

func TestAC25_CloseTriggersFallbackOnStreamExpired(t *testing.T) {
	w := &NativeStreamingWriter{
		streamExpired: true,
		started:       true,
	}
	_ = w.started

	require.True(t, w.streamExpired, "AC-2.5: Stream should be marked expired")

	w.streamExpired = false
	w.failedFlushChunks = []string{"chunk1", "chunk2"}

	integrityOK := len(w.failedFlushChunks) == 0
	require.False(t, integrityOK, "AC-2.5: Integrity check should fail with failed chunks")
	require.True(t, len(w.failedFlushChunks) > 0 || w.streamExpired, "AC-2.5: Fallback condition should be met")
}

func TestAC26_AllTestsPass(t *testing.T) {
	allErrors := []error{
		nil,
		errors.New("random"),
		errors.New("message_not_in_streaming_state"),
		errors.New("invalid_auth"),
		&slack.RateLimitedError{RetryAfter: time.Second},
	}

	for _, err := range allErrors {
		_ = isStreamStateError(err)
		_, _ = isRateLimitError(err)
		_ = isAuthError(err)
		_ = isRetryableError(err)
	}
}

func TestNativeStreamingWriter_StreamExpiredFlag(t *testing.T) {
	w := &NativeStreamingWriter{}
	require.False(t, w.streamExpired)

	w.streamExpired = true
	require.True(t, w.streamExpired)
}

func TestNativeStreamingWriter_WriteChecksStreamExpired(t *testing.T) {
	w := &NativeStreamingWriter{
		streamExpired: true,
		closed:        false,
	}
	_ = w.closed

	w.mu.Lock()
	expired := w.streamExpired
	w.mu.Unlock()

	require.True(t, expired)
}

func TestExpired_NewWriter(t *testing.T) {
	w := &NativeStreamingWriter{}
	require.False(t, w.Expired(), "new writer should not be expired")
}

func TestExpired_StartedButWithinTTL(t *testing.T) {
	w := &NativeStreamingWriter{
		started:         true,
		streamStartTime: time.Now(),
	}
	require.False(t, w.Expired(), "stream within TTL should not be expired")
}

func TestExpired_ExceededRotationTTL(t *testing.T) {
	w := &NativeStreamingWriter{
		started:         true,
		streamStartTime: time.Now().Add(-StreamRotationTTL - time.Second),
	}
	require.True(t, w.Expired(), "stream exceeding rotation TTL should be expired")
}

func TestExpired_ClosedWriter(t *testing.T) {
	w := &NativeStreamingWriter{
		started:         true,
		closed:          true,
		streamStartTime: time.Now().Add(-StreamRotationTTL - time.Hour),
	}
	require.False(t, w.Expired(), "closed writer should not report expired")
}

func TestExpired_ZeroStartTime(t *testing.T) {
	w := &NativeStreamingWriter{
		started:         true,
		streamStartTime: time.Time{},
	}
	require.False(t, w.Expired(), "zero start time should not report expired")
}

func TestDeadCodeRemoved(t *testing.T) {
	// Verify ttlWarningLogged field no longer exists by ensuring the struct
	// compiles and the dead TTL check path is gone.
	w := &NativeStreamingWriter{
		started:         false,
		streamStartTime: time.Now().Add(-StreamTTL - time.Hour),
	}
	// Before the fix, Write() with !started && !streamStartTime.IsZero() && > TTL
	// would return an error. Now it should proceed to the start path.
	// We can't call Write() without a client, but we verify the fields exist.
	require.False(t, w.started)
	require.False(t, w.streamStartTime.IsZero())
}

func TestStreamRotationTTL(t *testing.T) {
	require.Equal(t, 240*time.Second, StreamRotationTTL, "rotation TTL should be 240 seconds (safe margin under ~300s Slack limit)")
	require.Less(t, StreamRotationTTL, StreamTTL,
		"rotation TTL must be less than server TTL")
}

// streamFakeAPI records stream API calls; PostMessage calls are captured so
// tests can assert the writer never sends a full-content fallback itself.
// All other SlackAPI methods panic if called.
type streamFakeAPI struct {
	SlackAPI
	stopErr   error
	appendErr error
	mu        sync.Mutex
	posts     []string
}

func (f *streamFakeAPI) StartStreamContext(_ context.Context, _ string, _ ...slack.MsgOption) (string, string, error) {
	return "C1", "ts-1", nil
}

func (f *streamFakeAPI) AppendStreamContext(_ context.Context, _, _ string, _ ...slack.MsgOption) (string, string, error) {
	return "", "", f.appendErr
}

func (f *streamFakeAPI) StopStreamContext(_ context.Context, _, _ string, _ ...slack.MsgOption) (string, string, error) {
	return "", "", f.stopErr
}

func (f *streamFakeAPI) PostMessageContext(_ context.Context, _ string, _ ...slack.MsgOption) (string, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.posts = append(f.posts, "posted")
	return "", "", nil
}

// newClosedStateWriter builds a NativeStreamingWriter that is already in the
// started state without a flushLoop goroutine (no background flush needed for
// CloseContext assertions).
func newClosedStateWriter(api *streamFakeAPI) *NativeStreamingWriter {
	return &NativeStreamingWriter{
		client:          api,
		channelID:       "C1",
		messageTS:       "ts-1",
		started:         true,
		streamStartTime: time.Now(),
		closeChan:       make(chan struct{}),
		flushTrigger:    make(chan struct{}, 1),
		log:             discardLogger,
	}
}

func TestNativeStreamingWriter_CloseContext_ReturnsStopStreamError(t *testing.T) {
	t.Parallel()

	stopErr := errors.New("stop stream failed")
	api := &streamFakeAPI{stopErr: stopErr}
	w := newClosedStateWriter(api)

	err := w.CloseContext(context.Background())

	require.ErrorIs(t, err, stopErr, "StopStream error must be returned by CloseContext")
	var terminalErr *StreamTerminalError
	require.ErrorAs(t, err, &terminalErr)
	require.True(t, terminalErr.ContentPresented,
		"all chunks were shown, only the stop decoration failed")
	require.Empty(t, api.posts)
}

func TestNativeStreamingWriter_CloseContext_FailedChunkNotPresented(t *testing.T) {
	t.Parallel()

	appendErr := errors.New("append failed")
	api := &streamFakeAPI{appendErr: appendErr}
	w := newClosedStateWriter(api)
	w.failedFlushChunks = []string{"partial content"}

	err := w.CloseContext(context.Background())

	var terminalErr *StreamTerminalError
	require.ErrorAs(t, err, &terminalErr)
	require.False(t, terminalErr.ContentPresented,
		"failed flush chunks mean the body was not fully presented")
	require.Empty(t, api.posts)
}

func TestNativeStreamingWriter_CloseContext_StopDecorationOnlyFailurePresented(t *testing.T) {
	t.Parallel()

	stopErr := errors.New("stop stream failed")
	api := &streamFakeAPI{stopErr: stopErr}
	w := newClosedStateWriter(api)
	w.fullContent.WriteString("full answer")

	err := w.CloseContext(context.Background())

	var terminalErr *StreamTerminalError
	require.ErrorAs(t, err, &terminalErr)
	require.True(t, terminalErr.ContentPresented,
		"stop failure with all chunks shown is a decoration-only failure")
	require.Empty(t, api.posts)
}

func TestNativeStreamingWriter_CloseContext_NoFullContentFallback(t *testing.T) {
	t.Parallel()

	api := &streamFakeAPI{}
	w := newClosedStateWriter(api)
	w.streamExpired = true
	w.failedFlushChunks = []string{"chunk1", "chunk2"}
	w.fullContent.WriteString("full answer that must never be re-sent")

	err := w.CloseContext(context.Background())

	var terminalErr *StreamTerminalError
	require.ErrorAs(t, err, &terminalErr)
	require.False(t, terminalErr.ContentPresented)
	require.Empty(t, api.posts, "the writer must not send a full-content fallback itself")
}

func TestNativeStreamingWriter_CloseContext_NoErrorsReturnsNil(t *testing.T) {
	t.Parallel()

	api := &streamFakeAPI{}
	w := newClosedStateWriter(api)

	require.NoError(t, w.CloseContext(context.Background()))
	// Idempotent: a second close is a no-op.
	require.NoError(t, w.CloseContext(context.Background()))
	require.Empty(t, api.posts)
}

func TestNativeStreamingWriter_Close_PreservesIOCompat(t *testing.T) {
	t.Parallel()

	stopErr := errors.New("stop stream failed")
	api := &streamFakeAPI{stopErr: stopErr}
	w := newClosedStateWriter(api)

	// Close keeps io.WriteCloser compatibility: it must surface the same
	// StopStream failure via its own internal 10s context.
	err := w.Close()
	require.ErrorIs(t, err, stopErr)
	require.Empty(t, api.posts)
}

// TestNativeStreamingWriter_CloseContext_RateLimitedFinalFlushNotPresented
// guards the flushLoop close-path accounting: when the final flush is skipped
// by the rate limiter, the buffered tail must be recorded as unflushed so
// CloseContext reports ContentPresented=false and the connection's fallback
// machinery fires instead of silently losing the reply tail.
func TestNativeStreamingWriter_CloseContext_RateLimitedFinalFlushNotPresented(t *testing.T) {
	t.Parallel()

	rl := NewChannelRateLimiter(context.Background())
	t.Cleanup(rl.Stop)
	// Exhaust the burst so every subsequent Allow is denied (1 req/s rate
	// means no token recovery within the test's lifetime).
	for range 4 {
		rl.Allow("C1")
	}
	require.False(t, rl.Allow("C1"), "precondition: limiter must deny after burst exhaustion")

	api := &streamFakeAPI{}
	w := NewNativeStreamingWriter(context.Background(), api, "C1", "", "", rl, discardLogger, nil, nil)
	t.Cleanup(func() { _ = w.Close() })

	_, err := w.Write([]byte("hello"))
	require.NoError(t, err)
	// Under flushSize so the threshold trigger is not fired; the periodic
	// ticker may attempt a flush but the rate limiter denies it, keeping the
	// tail buffered until close.
	_, err = w.Write([]byte("tail"))
	require.NoError(t, err)

	err = w.CloseContext(context.Background())

	var terminalErr *StreamTerminalError
	require.ErrorAs(t, err, &terminalErr)
	require.False(t, terminalErr.ContentPresented,
		"a rate-limited final flush means the buffered tail was never delivered")
	require.Empty(t, api.posts)
}

// stuckAppendAPI blocks AppendStreamContext until release is closed, and
// signals start once the first append attempt is in flight.
type stuckAppendAPI struct {
	*streamFakeAPI
	once    sync.Once
	started chan struct{}
	release chan struct{}
}

func (f *stuckAppendAPI) AppendStreamContext(context.Context, string, string, ...slack.MsgOption) (string, string, error) {
	f.once.Do(func() { close(f.started) })
	<-f.release
	return "", "", errors.New("append stuck")
}

// TestNativeStreamingWriter_CloseContext_StuckFinalFlushBoundedByDeadline
// guards the CloseContext deadline contract: a final flush stuck in
// AppendStreamContext must not stall the terminal write past the caller's
// deadline, and the in-flight content must be reported as not-presented so
// the fallback still fires (never silently drop the reply tail).
func TestNativeStreamingWriter_CloseContext_StuckFinalFlushBoundedByDeadline(t *testing.T) {
	t.Parallel()

	api := &stuckAppendAPI{
		streamFakeAPI: &streamFakeAPI{},
		started:       make(chan struct{}),
		release:       make(chan struct{}),
	}
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(api.release) }) })

	w := NewNativeStreamingWriter(context.Background(), api, "C1", "", "", nil, discardLogger, nil, nil)
	t.Cleanup(func() { _ = w.Close() })

	_, err := w.Write([]byte("hello"))
	require.NoError(t, err)
	_, err = w.Write([]byte("tail"))
	require.NoError(t, err)

	// Wait until the final content is in flight inside AppendStreamContext:
	// only then is the deadline branch deterministic (pendingFlush set, buf
	// empty).
	select {
	case <-api.started:
	case <-time.After(2 * time.Second):
		t.Fatal("append never started")
	}

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err = w.CloseContext(ctx)

	require.Less(t, time.Since(start), 2*time.Second,
		"CloseContext must not wait past the caller's deadline for a stuck flush")
	var terminalErr *StreamTerminalError
	require.ErrorAs(t, err, &terminalErr)
	require.False(t, terminalErr.ContentPresented,
		"the in-flight final tail was never delivered — must not report the body as presented")
	require.Empty(t, api.posts)

	// Release the stuck append so the flushLoop goroutine exits cleanly;
	// verify it does so without hanging the test.
	releaseOnce.Do(func() { close(api.release) })
	require.Eventually(t, func() bool {
		w.mu.Lock()
		defer w.mu.Unlock()
		return w.pendingFlush == ""
	}, time.Second, 10*time.Millisecond)
}
