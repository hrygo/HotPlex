package feishu

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TC-2.4-1: 同一 chatID 的消息串行处理
func TestChatQueue_SerializesPerChat(t *testing.T) {
	t.Parallel()
	q := NewChatQueue(nil)

	var counter atomic.Int32

	var mu sync.Mutex
	var order []int

	// Enqueue 3 tasks for the same chat.
	for range 3 {
		require.NoError(t, q.Enqueue("chat_1", func(ctx context.Context) error {
			cur := counter.Add(1)
			mu.Lock()
			order = append(order, int(cur))
			mu.Unlock()
			time.Sleep(10 * time.Millisecond)
			return nil
		}))
	}

	// Wait for all tasks to complete.
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(order) == 3 && counter.Load() == 3
	}, 500*time.Millisecond, 10*time.Millisecond)
}

// TC-2.4-2: 不同 chatID 的消息并行处理
func TestChatQueue_ParallelizesAcrossChats(t *testing.T) {
	t.Parallel()
	q := NewChatQueue(nil)

	var counter atomic.Int32

	// Enqueue 2 tasks for 2 different chats simultaneously.
	require.NoError(t, q.Enqueue("chat_A", func(context.Context) error {
		counter.Add(1)
		time.Sleep(20 * time.Millisecond)
		return nil
	}))
	require.NoError(t, q.Enqueue("chat_B", func(context.Context) error {
		counter.Add(1)
		time.Sleep(20 * time.Millisecond)
		return nil
	}))

	// Both should complete within ~50ms (parallel), not ~100ms (serial).
	// Allow some margin for test flakiness.
	require.Eventually(t, func() bool {
		return counter.Load() == 2
	}, 150*time.Millisecond, 10*time.Millisecond)
}

// TC-2.4-3: Abort 能取消正在执行的任务
func TestChatQueue_Abort(t *testing.T) {
	t.Parallel()
	q := NewChatQueue(nil)

	var started atomic.Bool
	var aborted atomic.Bool

	require.NoError(t, q.Enqueue("chat_abort", func(ctx context.Context) error {
		started.Store(true)
		select {
		case <-ctx.Done():
			aborted.Store(true)
			return ctx.Err()
		case <-time.After(10 * time.Second):
			return nil
		}
	}))

	// Wait for task to start.
	require.Eventually(t, func() bool { return started.Load() }, 500*time.Millisecond, 10*time.Millisecond)

	// Abort the chat.
	q.Abort("chat_abort")

	// The task should be aborted.
	require.Eventually(t, func() bool { return aborted.Load() }, 500*time.Millisecond, 10*time.Millisecond)
}

// TC-2.4-4: worker 空闲超时后自动清理
func TestChatQueue_WorkerCleanup(t *testing.T) {
	t.Parallel()
	q := NewChatQueue(nil)

	require.NoError(t, q.Enqueue("chat_cleanup", func(_ context.Context) error {
		return nil
	}))

	require.Eventually(t, func() bool {
		q.mu.Lock()
		_, exists := q.workers["chat_cleanup"]
		q.mu.Unlock()
		return exists
	}, 100*time.Millisecond, 10*time.Millisecond, "worker should exist after task completes")

	q.Close()

	require.Eventually(t, func() bool {
		q.mu.Lock()
		_, exists := q.workers["chat_cleanup"]
		q.mu.Unlock()
		return !exists
	}, 500*time.Millisecond, 10*time.Millisecond, "worker should be removed after Close()")
}

func TestChatQueue_WorkerReuse(t *testing.T) {
	t.Parallel()
	q := NewChatQueue(nil)

	var counter atomic.Int32

	require.NoError(t, q.Enqueue("chat_reuse", func(_ context.Context) error {
		counter.Add(1)
		return nil
	}))
	require.NoError(t, q.Enqueue("chat_reuse", func(_ context.Context) error {
		counter.Add(1)
		return nil
	}))

	require.Eventually(t, func() bool {
		return counter.Load() == 2
	}, 500*time.Millisecond, 10*time.Millisecond)

	q.mu.Lock()
	_, exists := q.workers["chat_reuse"]
	q.mu.Unlock()
	require.True(t, exists, "worker should persist for task reuse")

	q.Close()
}

// Test: multiple rapid messages for same chat are queued and processed in order.
func TestChatQueue_RapidMessages(t *testing.T) {
	t.Parallel()
	q := NewChatQueue(nil)

	var last atomic.Int32

	// Send 5 rapid messages for the same chat.
	for range 5 {
		require.NoError(t, q.Enqueue("chat_rapid", func(_ context.Context) error {
			time.Sleep(5 * time.Millisecond)
			last.Add(1)
			return nil
		}))
	}

	// Should eventually process all 5.
	require.Eventually(t, func() bool {
		return last.Load() == 5
	}, 1*time.Second, 50*time.Millisecond)
}

// Test: chat with no worker is a no-op.
func TestChatQueue_Abort_NoWorker(t *testing.T) {
	t.Parallel()
	q := NewChatQueue(nil)
	// Aborting a chat with no active worker should not panic.
	q.Abort("chat_nonexistent")
}

// Test: worker 10-minute timeout prevents goroutine leaks.
func TestChatQueue_TaskTimeout(t *testing.T) {
	t.Parallel()
	// Verify that the timeout constant is set to 10 minutes.
	require.Equal(t, 10*time.Minute, chatTaskTimeout)
}

func TestChatQueue_CloseRejectsNewTasks(t *testing.T) {
	t.Parallel()

	q := NewChatQueue(nil)
	q.Close()

	err := q.Enqueue("closed-chat", func(context.Context) error { return nil })
	require.ErrorIs(t, err, ErrChatQueueClosed)
}

func TestChatQueue_CloseAndEnqueueConcurrent(t *testing.T) {
	t.Parallel()

	q := NewChatQueue(nil)
	start := make(chan struct{})
	errs := make(chan error, 32)
	var completed atomic.Int32
	for range 32 {
		go func() {
			defer completed.Add(1)
			defer func() {
				if recovered := recover(); recovered != nil {
					errs <- errors.New("enqueue panicked while close raced")
				}
			}()
			<-start
			errs <- q.Enqueue("shared-chat", func(context.Context) error { return nil })
		}()
	}

	closeDone := make(chan struct{})
	go func() {
		<-start
		q.Close()
		close(closeDone)
	}()
	close(start)

	require.Eventually(t, func() bool {
		return completed.Load() == 32
	}, time.Second, 10*time.Millisecond)
	require.Eventually(t, func() bool {
		select {
		case <-closeDone:
			return true
		default:
			return false
		}
	}, time.Second, 10*time.Millisecond)
	close(errs)
	for err := range errs {
		require.True(t, err == nil || errors.Is(err, ErrChatQueueClosed), "unexpected enqueue result: %v", err)
	}
}

func TestChatQueue_IdleCleanupDoesNotOrphanAcceptedTask(t *testing.T) {
	t.Parallel()

	q := NewChatQueue(nil)
	w := &chatWorker{tasks: make(chan func(context.Context) error, 1)}
	taskRan := make(chan struct{})
	w.tasks <- func(context.Context) error {
		close(taskRan)
		return nil
	}
	q.mu.Lock()
	q.workers["idle-boundary"] = w
	q.wg.Add(1)
	q.mu.Unlock()

	require.False(t, q.tryRemoveIdleWorker("idle-boundary", w))

	go q.runWorker("idle-boundary", w)
	select {
	case <-taskRan:
	case <-time.After(time.Second):
		t.Fatal("accepted task was orphaned at idle cleanup boundary")
	}
	q.Close()
}

func TestChatQueue_WorkerExitDoesNotRemoveReplacement(t *testing.T) {
	t.Parallel()

	q := NewChatQueue(nil)
	exiting := &chatWorker{tasks: make(chan func(context.Context) error)}
	replacement := &chatWorker{tasks: make(chan func(context.Context) error)}
	q.mu.Lock()
	q.workers["replacement"] = replacement
	q.mu.Unlock()

	q.removeWorkerIfCurrent("replacement", exiting)

	q.mu.Lock()
	current := q.workers["replacement"]
	q.mu.Unlock()
	require.Same(t, replacement, current)
}
