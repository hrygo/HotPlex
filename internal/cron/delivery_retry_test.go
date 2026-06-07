package cron

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRetryBackoff(t *testing.T) {
	t.Parallel()

	tests := []struct {
		attempt  int
		expected time.Duration
	}{
		{1, 30 * time.Second},
		{2, 60 * time.Second},
		{3, 120 * time.Second},
		{4, 240 * time.Second},
		{5, 300 * time.Second}, // capped at maxBackoff
		{10, 300 * time.Second},
	}
	for _, tc := range tests {
		t.Run(fmt.Sprintf("attempt_%d", tc.attempt), func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.expected, retryBackoff(tc.attempt))
		})
	}
}

func TestEnqueue_Eviction(t *testing.T) {
	t.Parallel()

	d := NewDelivery(slog.Default(), nil, nil)

	// Fill queue to max.
	for i := range maxQueueSize {
		d.enqueue(&CronJob{ID: fmt.Sprintf("job-%d", i), Name: fmt.Sprintf("name-%d", i)},
			"result", 1, time.Now())
	}
	require.Equal(t, maxQueueSize, len(d.queue))

	// Enqueue one more — should evict oldest.
	d.enqueue(&CronJob{ID: "overflow", Name: "overflow"}, "result", 1, time.Now())
	require.Equal(t, maxQueueSize, len(d.queue))
	require.Equal(t, "job-1", d.queue[0].job.ID) // job-0 evicted
	require.Equal(t, "overflow", d.queue[maxQueueSize-1].job.ID)
}

func TestEnqueue_AttemptTracking(t *testing.T) {
	t.Parallel()

	d := NewDelivery(slog.Default(), nil, nil)
	d.enqueue(&CronJob{ID: "j1"}, "result", 2, time.Now().Add(30*time.Second))

	require.Len(t, d.queue, 1)
	require.Equal(t, 2, d.queue[0].attempt)
}

func TestFlushPending(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	d := NewDelivery(slog.Default(),
		func(_ context.Context, _ string) (string, error) { return "response", nil },
		func(_ context.Context, _ string, _ map[string]string, _ string) error {
			calls.Add(1)
			return nil
		},
	)

	now := time.Now()
	// One due entry, one future entry.
	d.enqueue(&CronJob{ID: "due", Platform: "feishu", PlatformKey: map[string]string{"chat_id": "c1"}},
		"result", 2, now.Add(-time.Second)) // past = due
	d.enqueue(&CronJob{ID: "future", Platform: "feishu", PlatformKey: map[string]string{"chat_id": "c2"}},
		"result", 2, now.Add(time.Minute)) // future = not due

	d.flushPending(context.Background())

	require.Equal(t, int32(1), calls.Load()) // only due entry delivered
	require.Len(t, d.queue, 1)               // future entry remains
	require.Equal(t, "future", d.queue[0].job.ID)
}

func TestFlushPending_TransientFailure(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	d := NewDelivery(slog.Default(),
		func(_ context.Context, _ string) (string, error) { return "response", nil },
		func(_ context.Context, _ string, _ map[string]string, _ string) error {
			n := calls.Add(1)
			if n <= 2 {
				return fmt.Errorf("rate limit exceeded, 429")
			}
			return nil
		},
	)

	job := &CronJob{ID: "retry-job", Name: "test", Platform: "feishu",
		PlatformKey: map[string]string{"chat_id": "c1"}}
	d.enqueue(job, "result", 1, time.Now().Add(-time.Second))

	// First flush: fails with transient error, enqueued for retry.
	d.flushPending(context.Background())
	require.Equal(t, int32(1), calls.Load())
	require.Len(t, d.queue, 1)
	require.Equal(t, 2, d.queue[0].attempt) // attempt incremented

	// Second flush: fails again, enqueued for retry.
	d.queue[0].nextAt = time.Now().Add(-time.Second) // make it due
	d.flushPending(context.Background())
	require.Equal(t, int32(2), calls.Load())
	require.Len(t, d.queue, 1)
	require.Equal(t, 3, d.queue[0].attempt) // attempt incremented again

	// Third flush: succeeds.
	d.queue[0].nextAt = time.Now().Add(-time.Second)
	d.flushPending(context.Background())
	require.Equal(t, int32(3), calls.Load())
	require.Empty(t, d.queue) // removed on success
}

func TestFlushPending_PermanentFailure(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	d := NewDelivery(slog.Default(),
		func(_ context.Context, _ string) (string, error) { return "response", nil },
		func(_ context.Context, _ string, _ map[string]string, _ string) error {
			calls.Add(1)
			return fmt.Errorf("not found: 404")
		},
	)

	d.enqueue(&CronJob{ID: "perm-fail", Name: "test", Platform: "feishu",
		PlatformKey: map[string]string{"chat_id": "c1"}}, "result", 1, time.Now().Add(-time.Second))

	d.flushPending(context.Background())

	require.Equal(t, int32(1), calls.Load())
	require.Empty(t, d.queue) // permanent failure: not re-enqueued
}

func TestFlushPending_ExhaustedRetries(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	d := NewDelivery(slog.Default(),
		func(_ context.Context, _ string) (string, error) { return "response", nil },
		func(_ context.Context, _ string, _ map[string]string, _ string) error {
			calls.Add(1)
			return fmt.Errorf("timeout exceeded")
		},
	)

	// attempt=maxRetryAttempts: this is the last allowed attempt.
	d.enqueue(&CronJob{ID: "exhausted", Name: "test", Platform: "feishu",
		PlatformKey: map[string]string{"chat_id": "c1"}}, "result", maxRetryAttempts, time.Now().Add(-time.Second))

	d.flushPending(context.Background())

	require.Equal(t, int32(1), calls.Load())
	require.Empty(t, d.queue) // exhausted: not re-enqueued
}

func TestRetryLoop_StopRetryLoop(t *testing.T) {
	t.Parallel()

	d := NewDelivery(slog.Default(),
		func(_ context.Context, _ string) (string, error) { return "response", nil },
		nil,
	)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	d.StartRetryLoop(ctx)

	// Add a pending entry that should be logged as permanently lost on stop.
	d.enqueue(&CronJob{ID: "pending", Name: "test", Platform: "feishu",
		PlatformKey: map[string]string{"chat_id": "c1"}}, "result", 1, time.Now().Add(time.Minute))

	d.StopRetryLoop()

	require.Empty(t, d.queue) // cleared on stop
}

func TestDeliverResult_TransientError(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	d := NewDelivery(slog.Default(),
		func(_ context.Context, _ string) (string, error) { return "response", nil },
		func(_ context.Context, _ string, _ map[string]string, _ string) error {
			calls.Add(1)
			return fmt.Errorf("connection timeout")
		},
	)

	job := &CronJob{ID: "transient", Name: "test", Platform: "feishu",
		PlatformKey: map[string]string{"chat_id": "c1"}}
	d.deliverResult(context.Background(), job, "result", 1)

	require.Equal(t, int32(1), calls.Load())
	require.Len(t, d.queue, 1)
	require.Equal(t, 2, d.queue[0].attempt) // enqueued with incremented attempt
}

func TestDeliverResult_SuccessOnRetry(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	d := NewDelivery(slog.Default(),
		func(_ context.Context, _ string) (string, error) { return "response", nil },
		func(_ context.Context, _ string, _ map[string]string, _ string) error {
			calls.Add(1)
			return nil
		},
	)

	job := &CronJob{ID: "retry-ok", Name: "test", Platform: "feishu",
		PlatformKey: map[string]string{"chat_id": "c1"}}
	d.deliverResult(context.Background(), job, "result", 2) // attempt=2 means this is a retry

	require.Equal(t, int32(1), calls.Load())
	require.Empty(t, d.queue) // success: no re-enqueue
}

func TestDeliverResult_PermanentError(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	d := NewDelivery(slog.Default(),
		func(_ context.Context, _ string) (string, error) { return "response", nil },
		func(_ context.Context, _ string, _ map[string]string, _ string) error {
			calls.Add(1)
			return fmt.Errorf("authentication failed")
		},
	)

	job := &CronJob{ID: "perm", Name: "test", Platform: "feishu",
		PlatformKey: map[string]string{"chat_id": "c1"}}
	d.deliverResult(context.Background(), job, "result", 1)

	require.Equal(t, int32(1), calls.Load())
	require.Empty(t, d.queue) // permanent error: not enqueued
}
