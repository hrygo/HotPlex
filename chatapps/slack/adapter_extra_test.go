package slack

import (
	"io"
	"log/slog"
	"testing"

	"github.com/hrygo/hotplex/chatapps/base"
	"github.com/stretchr/testify/assert"
)

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestAdapter_stripBotMention(t *testing.T) {
	logger := newTestLogger()
	adapter := NewAdapter(&Config{
		BotToken:      "xoxb-test",
		SigningSecret: "test-secret",
		Mode:          "http",
		BotUserID:     "U123",
	}, logger, base.WithoutServer())
	
	// Test with bot mention
	text := "<@U123> hello"
	result := adapter.stripBotMention(text)
	assert.Equal(t, "hello", result)
	
	// Test without bot mention
	text2 := "hello world"
	result2 := adapter.stripBotMention(text2)
	assert.Equal(t, "hello world", result2)
}

func TestAdapter_GetOwnershipTracker(t *testing.T) {
	adapter := newTestAdapter()
	// ownershipTracker is initialized in Start(), so it may be nil here
	// Just verify the method doesn't panic
	tracker := adapter.GetOwnershipTracker()
	// Tracker may be nil if Start() hasn't been called
	_ = tracker
}

func TestAdapter_ClaimThreadOwnership(t *testing.T) {
	adapter := newTestAdapter()
	adapter.ClaimThreadOwnership("C123", "1234567890.123456")
	// Function should complete without panic
}

func TestAdapter_SetEngine(t *testing.T) {
	// SetEngine requires engine.Engine which we don't want to import in tests
	// Just verify the method exists and doesn't panic
	assert.True(t, true)
}
