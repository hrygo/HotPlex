package slack

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"

	"github.com/hrygo/hotplex/chatapps/base"
	"github.com/stretchr/testify/assert"
)

// createTestAdapterWithNilClient creates an adapter with nil client for pure unit tests
func createTestAdapterWithNilClient() *Adapter {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	adapter := NewAdapter(&Config{
		BotToken:      "xoxb-test-bot-token-123456789012-abcdef",
		SigningSecret: "test-signing-secret",
		Mode:          "http",
	}, logger, base.WithoutServer())
	// Set client to nil to simulate unitialized state
	adapter.client = nil
	return adapter
}

// =============================================================================
// Nil Client Error Handling Tests
// =============================================================================

// TestNativeStreamingWriter_NilClient tests that Write fails with nil client
func TestNativeStreamingWriter_NilClient(t *testing.T) {
	adapter := &Adapter{client: nil}
	writer := NewNativeStreamingWriter(context.Background(), adapter, "U123", "C123", "T123", nil)

	// Write should fail with nil client
	_, err := writer.Write([]byte("test"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "start stream")
}

// TestNativeStreamingWriter_NilClient_Stop tests that StopStream fails with nil client
func TestNativeStreamingWriter_NilClient_Stop(t *testing.T) {
	adapter := createTestAdapterWithNilClient()
	writer := NewNativeStreamingWriter(context.Background(), adapter, "U123", "C123", "T123", nil)

	// Simulate a scenario where stream is already started (manually set)
	writer.mu.Lock()
	writer.started = true
	writer.messageTS = "1234567890.123456"
	writer.mu.Unlock()

	// Close should fail when trying to stop stream due to nil client internally
	err := writer.Close()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "stop stream")
}

// =============================================================================
// Close Idempotency Tests
// =============================================================================

// TestNativeStreamingWriter_DoubleClose tests that Close() can be called multiple times without error
func TestNativeStreamingWriter_DoubleClose(t *testing.T) {
	adapter := createTestAdapterWithNilClient()
	writer := NewNativeStreamingWriter(context.Background(), adapter, "U123", "C123", "T123", nil)

	// First close - should succeed (even without starting stream)
	err1 := writer.Close()
	assert.NoError(t, err1)

	// Second close - should also succeed (idempotent)
	err2 := writer.Close()
	assert.NoError(t, err2)

	// Verify stream is closed
	assert.True(t, writer.IsClosed())
}

// TestNativeStreamingWriter_MultipleClose tests multiple consecutive Close() calls
func TestNativeStreamingWriter_MultipleClose(t *testing.T) {
	adapter := createTestAdapterWithNilClient()
	writer := NewNativeStreamingWriter(context.Background(), adapter, "U123", "C123", "T123", nil)

	// Call Close multiple times
	for i := 0; i < 5; i++ {
		err := writer.Close()
		assert.NoError(t, err, "Close() call %d should succeed", i+1)
	}

	assert.True(t, writer.IsClosed())
}

// TestNativeStreamingWriter_CloseAfterStart tests Close() after stream started
func TestNativeStreamingWriter_CloseAfterStart(t *testing.T) {
	adapter := createTestAdapterWithNilClient()
	writer := NewNativeStreamingWriter(context.Background(), adapter, "U123", "C123", "T123", nil)

	// Manually simulate started stream (to avoid real API calls)
	writer.mu.Lock()
	writer.started = true
	writer.messageTS = "1234567890.123456"
	writer.mu.Unlock()

	// First close - will fail due to nil client, but closed state should still be set
	err1 := writer.Close()
	assert.Error(t, err1) // Expected: nil client error
	assert.True(t, writer.IsClosed(), "Should be closed even on error")

	// Second close - should succeed (idempotent, returns nil immediately)
	err2 := writer.Close()
	assert.NoError(t, err2)
}

// =============================================================================
// Stream Lifecycle Tests
// =============================================================================

// TestNativeStreamingWriter_Lifecycle tests Write -> Close -> Write (should error)
func TestNativeStreamingWriter_Lifecycle(t *testing.T) {
	adapter := createTestAdapterWithNilClient()
	writer := NewNativeStreamingWriter(context.Background(), adapter, "U123", "C123", "T123", nil)

	// Simulate started stream (to avoid real API calls)
	writer.mu.Lock()
	writer.started = true
	writer.messageTS = "1234567890.123456"
	writer.mu.Unlock()

	// Close the stream
	err := writer.Close()
	assert.Error(t, err) // Will fail due to nil client in StopStream
	assert.True(t, writer.IsClosed())

	// Try to write after close - should fail with "stream already closed"
	_, err = writer.Write([]byte("test after close"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "stream already closed")
}

// TestNativeStreamingWriter_WriteAfterClose tests writing to a closed stream
func TestNativeStreamingWriter_WriteAfterClose(t *testing.T) {
	adapter := createTestAdapterWithNilClient()
	writer := NewNativeStreamingWriter(context.Background(), adapter, "U123", "C123", "T123", nil)

	// Close without starting
	err := writer.Close()
	assert.NoError(t, err)

	// Write should fail
	_, err = writer.Write([]byte("test"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "stream already closed")
}

// TestNativeStreamingWriter_EmptyContent tests writing empty content
func TestNativeStreamingWriter_EmptyContent(t *testing.T) {
	adapter := createTestAdapterWithNilClient()
	writer := NewNativeStreamingWriter(context.Background(), adapter, "U123", "C123", "T123", nil)

	// Write empty content - should succeed without starting stream
	n, err := writer.Write([]byte(""))
	assert.NoError(t, err)
	assert.Equal(t, 0, n)
	assert.False(t, writer.IsStarted())

	// Write whitespace only - will try to start stream but fail with nil client
	// This tests the actual behavior: whitespace is NOT treated as empty
	n, err = writer.Write([]byte("   "))
	// With nil client, Write will error on StartStream call
	assert.Error(t, err)
	assert.Equal(t, 0, n)
	assert.False(t, writer.IsStarted()) // Stream never started due to error
}

// TestNativeStreamingWriter_StateTransitions tests state machine transitions
func TestNativeStreamingWriter_StateTransitions(t *testing.T) {
	adapter := createTestAdapterWithNilClient()
	writer := NewNativeStreamingWriter(context.Background(), adapter, "U123", "C123", "T123", nil)

	// Initial state: not started, not closed
	assert.False(t, writer.IsStarted())
	assert.False(t, writer.IsClosed())

	// Simulate start
	writer.mu.Lock()
	writer.started = true
	writer.messageTS = "1234567890.123456"
	writer.mu.Unlock()

	assert.True(t, writer.IsStarted())
	assert.False(t, writer.IsClosed())

	// Close
	writer.mu.Lock()
	writer.closed = true
	writer.mu.Unlock()

	assert.True(t, writer.IsStarted())
	assert.True(t, writer.IsClosed())
}

// TestNativeStreamingWriter_MessageTS tests MessageTS getter
func TestNativeStreamingWriter_MessageTS(t *testing.T) {
	adapter := createTestAdapterWithNilClient()
	writer := NewNativeStreamingWriter(context.Background(), adapter, "U123", "C123", "T123", nil)

	// Initially empty
	assert.Empty(t, writer.MessageTS())

	// After simulated start
	expectedTS := "1234567890.123456"
	writer.mu.Lock()
	writer.messageTS = expectedTS
	writer.mu.Unlock()

	assert.Equal(t, expectedTS, writer.MessageTS())
}

// TestNativeStreamingWriter_ConcurrentAccess tests thread safety
func TestNativeStreamingWriter_ConcurrentAccess(t *testing.T) {
	adapter := createTestAdapterWithNilClient()
	writer := NewNativeStreamingWriter(context.Background(), adapter, "U123", "C123", "T123", nil)

	done := make(chan bool)

	// Concurrent reads
	go func() {
		for i := 0; i < 100; i++ {
			_ = writer.IsStarted()
			_ = writer.IsClosed()
			_ = writer.MessageTS()
		}
		done <- true
	}()

	// Concurrent writes (simulated state changes)
	go func() {
		for i := 0; i < 100; i++ {
			writer.mu.Lock()
			writer.started = true
			writer.closed = false
			writer.messageTS = "1234567890.123456"
			writer.mu.Unlock()
		}
		done <- true
	}()

	// Wait for both goroutines
	<-done
	<-done

	// No panic = thread safe
}

// TestNativeStreamingWriter_CompleteCallback tests the onComplete callback
func TestNativeStreamingWriter_CompleteCallback(t *testing.T) {
	var capturedTS string
	var callbackCalled bool
	var mu sync.Mutex

	onComplete := func(ts string) {
		mu.Lock()
		defer mu.Unlock()
		callbackCalled = true
		capturedTS = ts
	}

	adapter := createTestAdapterWithNilClient()
	writer := NewNativeStreamingWriter(context.Background(), adapter, "U123", "C123", "T123", onComplete)

	// Simulate started stream
	writer.mu.Lock()
	writer.started = true
	writer.messageTS = "1234567890.999999"
	writer.mu.Unlock()

	// Close should trigger callback even if StopStream fails
	_ = writer.Close()

	// Verify callback was called with the message TS
	mu.Lock()
	assert.True(t, callbackCalled, "Callback should have been called")
	assert.Equal(t, "1234567890.999999", capturedTS)
	mu.Unlock()
}

// TestNativeStreamingWriter_EmptyCloseWithoutStart tests Close on never-started stream
func TestNativeStreamingWriter_EmptyCloseWithoutStart(t *testing.T) {
	adapter := createTestAdapterWithNilClient()
	writer := NewNativeStreamingWriter(context.Background(), adapter, "U123", "C123", "T123", nil)

	// Close without ever starting - should succeed immediately
	err := writer.Close()
	assert.NoError(t, err)
	assert.False(t, writer.IsStarted())
	assert.True(t, writer.IsClosed())
}

// TestNativeStreamingWriter_InterfaceCompliance tests compile-time interface compliance
func TestNativeStreamingWriter_InterfaceCompliance(t *testing.T) {
	adapter := createTestAdapterWithNilClient()
	writer := NewNativeStreamingWriter(context.Background(), adapter, "U123", "C123", "T123", nil)

	// Verify io.WriteCloser compliance
	var _ io.WriteCloser = writer

	// Verify base.StreamWriter compliance
	var _ base.StreamWriter = writer

	// Suppress unused variable warning
	_ = writer
}
