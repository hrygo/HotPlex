package chatapps

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"
)

// Test RateLimiter core functionality
func TestRateLimiter_Allow(t *testing.T) {
	rl := NewRateLimiter(10, 10) // 10 tokens, refill 10 per second

	// Should allow first few requests
	for i := 0; i < 10; i++ {
		if !rl.Allow() {
			t.Errorf("Expected allow on attempt %d", i+1)
		}
	}

	// Should deny when tokens exhausted
	if rl.Allow() {
		t.Error("Expected deny when tokens exhausted")
	}
}

func TestRateLimiter_Refill(t *testing.T) {
	rl := NewRateLimiter(2, 10) // 2 tokens, refill 10 per second

	// Exhaust tokens
	rl.Allow()
	rl.Allow()

	// Wait for refill
	time.Sleep(200 * time.Millisecond)

	// Should get at least 1 token back (10 tokens/sec * 0.2 sec = 2 tokens)
	if !rl.Allow() {
		t.Error("Expected allow after token refill")
	}
}

func TestRateLimiter_Wait(t *testing.T) {
	rl := NewRateLimiter(1, 1) // 1 token/sec refill - very slow

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// First request should succeed immediately
	rl.Allow()

	// Second request should wait and fail due to timeout (refill too slow)
	err := rl.Wait(ctx)
	if err == nil {
		t.Error("Expected timeout error")
	}
}

// Test IsRetryableError classification
func TestIsRetryableError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{"timeout", errors.New("connection timeout"), true},
		{"rate limit", errors.New("rate limit exceeded: 429"), true},
		{"server error 500", errors.New("internal server error: 500"), true},
		{"server error 503", errors.New("service unavailable: 503"), true},
		{"auth error 401", errors.New("unauthorized: 401"), false},
		{"auth error 403", errors.New("forbidden: 403"), false},
		{"not found 404", errors.New("not found: 404"), false},
		{"validation error", errors.New("validation failed"), false},
		{"unknown error", errors.New("something went wrong"), true}, // default to retry
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsRetryableError(tt.err)
			if result != tt.expected {
				t.Errorf("IsRetryableError(%v) = %v, want %v", tt.err, result, tt.expected)
			}
		})
	}
}

// Test MessageQueue basic operations
func TestMessageQueue_EnqueueDequeue(t *testing.T) {
	logger := slog.Default()
	q := NewMessageQueue(logger, 10, 0, 1)

	msg := &ChatMessage{Content: "test"}
	err := q.Enqueue("slack", "session-1", msg)
	if err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	if q.Size() != 1 {
		t.Errorf("Expected size 1, got %d", q.Size())
	}

	dequeued, ok := q.Dequeue()
	if !ok {
		t.Fatal("Expected to dequeue message")
	}
	if dequeued.Platform != "slack" {
		t.Errorf("Expected platform slack, got %s", dequeued.Platform)
	}

	if q.Size() != 0 {
		t.Errorf("Expected size 0 after dequeue, got %d", q.Size())
	}
}

func TestMessageQueue_QueueFull(t *testing.T) {
	logger := slog.Default()
	q := NewMessageQueue(logger, 2, 0, 1) // max size 2

	msg := &ChatMessage{Content: "test"}

	// Fill the queue
	if err := q.Enqueue("slack", "s1", msg); err != nil {
		t.Fatalf("Enqueue 1 failed: %v", err)
	}
	if err := q.Enqueue("slack", "s2", msg); err != nil {
		t.Fatalf("Enqueue 2 failed: %v", err)
	}

	// Queue is full, should get error
	err := q.Enqueue("slack", "s3", msg)
	if err != ErrQueueFull {
		t.Errorf("Expected ErrQueueFull, got %v", err)
	}
}

// Test Dead Letter Queue
func TestMessageQueue_DLQ(t *testing.T) {
	logger := slog.Default()
	q := NewMessageQueue(logger, 10, 5, 0) // DLQ size 5

	msg := &QueuedMessage{
		Platform:  "slack",
		SessionID: "session-1",
		Message:   &ChatMessage{Content: "test"},
		Retries:   3,
	}

	// Add to DLQ
	q.AddToDLQ(msg)

	if q.DLQLen() != 1 {
		t.Errorf("Expected DLQ length 1, got %d", q.DLQLen())
	}

	// GetDLQ should return copy
	dlq := q.GetDLQ()
	if len(dlq) != 1 {
		t.Errorf("Expected 1 message in DLQ, got %d", len(dlq))
	}
	if dlq[0].Platform != "slack" {
		t.Errorf("Expected platform slack, got %s", dlq[0].Platform)
	}
}

func TestMessageQueue_DLQOverflow(t *testing.T) {
	logger := slog.Default()
	q := NewMessageQueue(logger, 10, 2, 0) // DLQ size 2

	// Add more than DLQ size
	for i := 0; i < 3; i++ {
		msg := &QueuedMessage{
			Platform:  "slack",
			SessionID: "session-1",
			Message:   &ChatMessage{Content: "test"},
			Retries:   3,
		}
		q.AddToDLQ(msg)
	}

	// Should only keep latest 2 (FIFO)
	if q.DLQLen() != 2 {
		t.Errorf("Expected DLQ length 2 (overflow), got %d", q.DLQLen())
	}
}
