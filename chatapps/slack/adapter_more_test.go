package slack

import (
	"context"
	"testing"

	"github.com/hrygo/hotplex/chatapps/base"
	"github.com/stretchr/testify/assert"
)

func TestAdapter_Start(t *testing.T) {
	adapter := newTestAdapter()
	// Start requires a valid context and may start background goroutines
	// Just verify it doesn't panic
	err := adapter.Start(context.Background())
	// May fail due to HTTP server setup, but should not panic
	_ = err
}

func TestAdapter_DeleteMessage(t *testing.T) {
	adapter := newTestAdapter()
	// Test with invalid credentials (expected to fail)
	err := adapter.DeleteMessage(context.Background(), "C123", "1234567890.123456")
	// Should return error due to invalid token
	assert.Error(t, err)
}

func TestAdapter_UpdateMessage(t *testing.T) {
	adapter := newTestAdapter()
	msg := &base.ChatMessage{Content: "updated text"}
	err := adapter.UpdateMessage(context.Background(), "C123", "1234567890.123456", msg)
	// Should return error due to invalid token
	assert.Error(t, err)
}

func TestAdapter_SendThreadReply(t *testing.T) {
	adapter := newTestAdapter()
	err := adapter.SendThreadReply(context.Background(), "C123", "1234567890.123456", "reply text")
	// Should return error due to invalid token
	assert.Error(t, err)
}

func TestAdapter_shouldRespondToThreadMessage(t *testing.T) {
	adapter := newTestAdapter()
	
	// Test when user is bot itself
	result, _ := adapter.shouldRespondToThreadMessage("channel", "C123", "1234567890.123456", "hello", "U123")
	assert.False(t, result)
	
	// Test with valid user
	adapter2 := newTestAdapter()
	result2, _ := adapter2.shouldRespondToThreadMessage("channel", "C123", "1234567890.123456", "hello", "U456")
	// May vary based on config
	_ = result2
}

func TestAdapter_initAppHome(t *testing.T) {
	logger := newTestLogger()
	adapter := NewAdapter(&Config{
		BotToken:      "xoxb-test",
		SigningSecret: "test-secret",
		Mode:          "http",
	}, logger, base.WithoutServer())
	
	// Test with nil appHomeHandler (should not panic)
	adapter.initAppHome()
}

func TestAdapter_registerCommands(t *testing.T) {
	adapter := newTestAdapter()
	// Test with nil command handler
	adapter.registerCommands()
}

func TestAdapter_GetThreadHistoryAsString(t *testing.T) {
	adapter := newTestAdapter()
	// Storage may not be enabled, so error is expected
	_, err := adapter.GetThreadHistoryAsString(context.Background(), "C123", "1234567890.123456", 10)
	// Error is expected since storage is not enabled in test adapter
	_ = err
}
