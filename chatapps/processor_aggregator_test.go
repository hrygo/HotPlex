package chatapps

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/hrygo/hotplex/chatapps/base"
)

// TestMessageAggregatorProcessor_flushBufferByTimer tests the timer-based flush functionality
func TestMessageAggregatorProcessor_flushBufferByTimer(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(&strings.Builder{}, nil))
	
	// Create a mock sender to capture flushed messages
	var flushedMsg *base.ChatMessage
	mockSender := &mockAggregatedMessageSender{
		sendFunc: func(ctx context.Context, msg *base.ChatMessage) error {
			flushedMsg = msg
			return nil
		},
	}
	
	// Create processor with very short window for testing
	ctx := context.Background()
	processor := NewMessageAggregatorProcessor(ctx, logger, MessageAggregatorProcessorOptions{
		Window:     10 * time.Millisecond,
		MinContent: 100,
		MaxMsgs:    10,
		MaxBytes:   2000,
	})
	processor.SetSender(mockSender)
	
	// Create a test message with stream=true (required for aggregation)
	msg := &base.ChatMessage{
		Platform:  "slack",
		SessionID: "test-session",
		Content:   "Test content",
		Metadata: map[string]any{
			"event_type": "tool_use",
			"stream":     true, // Required for aggregation
		},
	}
	
	// Process message (will be buffered)
	result, err := processor.Process(ctx, msg)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}
	if result != nil {
		t.Fatal("Expected message to be buffered, but it was sent immediately")
	}
	
	// Wait for timer to flush (window + buffer)
	time.Sleep(50 * time.Millisecond)
	
	// Verify message was flushed
	if flushedMsg == nil {
		t.Fatal("Expected message to be flushed by timer, but it wasn't")
	}
	if flushedMsg.Content != "Test content" {
		t.Errorf("Expected content 'Test content', got %q", flushedMsg.Content)
	}
}

// TestMessageAggregatorProcessor_bufferMessage tests the buffer message functionality
func TestMessageAggregatorProcessor_bufferMessage(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(&strings.Builder{}, nil))
	
	ctx := context.Background()
	processor := NewMessageAggregatorProcessor(ctx, logger, MessageAggregatorProcessorOptions{
		Window:     100 * time.Millisecond,
		MinContent: 100,
		MaxMsgs:    10,
		MaxBytes:   2000,
	})
	
	// Test 1: Buffer short message (stream=true required)
	msg1 := &base.ChatMessage{
		Platform:  "slack",
		SessionID: "test-session",
		Content:   "Short",
		Metadata: map[string]any{
			"event_type": "tool_use",
			"stream":     true,
		},
	}
	
	result, err := processor.Process(ctx, msg1)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}
	if result != nil {
		t.Fatal("Expected short message to be buffered")
	}
	
	// Test 2: Buffer reaches max messages limit
	for i := 0; i < 9; i++ {
		msg := &base.ChatMessage{
			Platform:  "slack",
			SessionID: "test-session",
			Content:   "Message",
			Metadata:  map[string]any{"event_type": "tool_use"},
		}
		_, _ = processor.Process(ctx, msg)
	}
	
	// 10th message should trigger flush due to maxMsgs limit
	msg10 := &base.ChatMessage{
		Platform:  "slack",
		SessionID: "test-session",
		Content:   "Last",
		Metadata:  map[string]any{"event_type": "tool_use"},
	}
	
	result, err = processor.Process(ctx, msg10)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}
	if result == nil {
		t.Fatal("Expected 10th message to trigger flush due to maxMsgs limit")
	}
}

// TestMessageAggregatorProcessor_bufferMessageMaxBytes tests buffer byte limit
func TestMessageAggregatorProcessor_bufferMessageMaxBytes(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(&strings.Builder{}, nil))
	
	ctx := context.Background()
	processor := NewMessageAggregatorProcessor(ctx, logger, MessageAggregatorProcessorOptions{
		Window:     100 * time.Millisecond,
		MinContent: 100,
		MaxMsgs:    100,
		MaxBytes:   500, // Low limit for testing
	})
	
	// Send messages until byte limit is reached
	for i := 0; i < 10; i++ {
		msg := &base.ChatMessage{
			Platform:  "slack",
			SessionID: "test-session",
			Content:   strings.Repeat("x", 100), // 100 bytes each
			Metadata: map[string]any{
				"event_type": "tool_use",
				"stream":     true,
			},
		}
		
		result, err := processor.Process(ctx, msg)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}
		
		// Should flush when byte limit is exceeded
		if i >= 5 && result == nil {
			t.Errorf("Expected flush at byte limit, but message was buffered at iteration %d", i)
		}
	}
}

// TestMessageAggregatorProcessor_differentEventTypes tests event-type specific aggregation
func TestMessageAggregatorProcessor_differentEventTypes(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(&strings.Builder{}, nil))
	
	ctx := context.Background()
	processor := NewMessageAggregatorProcessor(ctx, logger, MessageAggregatorProcessorOptions{
		Window:     100 * time.Millisecond,
		MinContent: 100,
		MaxMsgs:    10,
		MaxBytes:   2000,
	})
	
	// Send tool_use message (stream=true required)
	msg1 := &base.ChatMessage{
		Platform:  "slack",
		SessionID: "test-session",
		Content:   "Tool 1",
		Metadata: map[string]any{
			"event_type": "tool_use",
			"stream":     true,
		},
	}
	
	// Send answer message (different event type)
	msg2 := &base.ChatMessage{
		Platform:  "slack",
		SessionID: "test-session",
		Content:   "Answer",
		Metadata: map[string]any{
			"event_type": "answer",
			"stream":     true,
		},
	}
	
	_, _ = processor.Process(ctx, msg1)
	_, _ = processor.Process(ctx, msg2)
	
	// Should have 2 separate buffers (one per event type)
	processor.mu.Lock()
	bufferCount := len(processor.buffers)
	processor.mu.Unlock()
	
	if bufferCount != 2 {
		t.Errorf("Expected 2 buffers (one per event type), got %d", bufferCount)
	}
}

// TestMessageAggregatorProcessor_flushBuffer tests flushBuffer function
func TestMessageAggregatorProcessor_flushBuffer(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(&strings.Builder{}, nil))
	
	ctx := context.Background()
	processor := NewMessageAggregatorProcessor(ctx, logger, MessageAggregatorProcessorOptions{
		Window:     100 * time.Millisecond,
		MinContent: 100,
	})
	
	// First buffer a message (use 'answer' event type which aggregates)
	msg1 := &base.ChatMessage{
		Platform:  "slack",
		SessionID: "test-session",
		Content:   "Buffered",
		Metadata: map[string]any{
			"event_type": "answer",
			"stream":     true,
		},
	}
	_, _ = processor.Process(ctx, msg1)
	
	// Send message with is_final flag (should flush buffer and return aggregated)
	msg2 := &base.ChatMessage{
		Platform:  "slack",
		SessionID: "test-session",
		Content:   "Final",
		Metadata: map[string]any{
			"event_type": "answer",
			"stream":     true,
			"is_final":   true,
		},
	}
	
	result, err := processor.Process(ctx, msg2)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}
	
	// is_final should return aggregated message (not nil)
	if result == nil {
		t.Fatal("Expected aggregated message from is_final")
	}
	
	// Content should include both messages
	if !strings.Contains(result.Content, "Buffered") || !strings.Contains(result.Content, "Final") {
		t.Errorf("Expected aggregated content, got: %q", result.Content)
	}
}

// mockAggregatedMessageSender is a mock implementation for testing
type mockAggregatedMessageSender struct {
	sendFunc func(ctx context.Context, msg *base.ChatMessage) error
}

func (m *mockAggregatedMessageSender) SendAggregatedMessage(ctx context.Context, msg *base.ChatMessage) error {
	if m.sendFunc != nil {
		return m.sendFunc(ctx, msg)
	}
	return nil
}
