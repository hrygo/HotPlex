package slack

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/hrygo/hotplex/chatapps/base"
	"github.com/hrygo/hotplex/plugins/storage"
	"github.com/hrygo/hotplex/types"
)

// =============================================================================
// Storage Config Tests
// =============================================================================

func TestStorageConfig_Defaults(t *testing.T) {
	cfg := &StorageConfig{}
	if cfg.Enabled {
		t.Error("Expected Enabled to be false by default")
	}
	if cfg.Type != "" {
		t.Error("Expected Type to be empty by default")
	}
}

func TestStorageConfig_PostgreSQL(t *testing.T) {
	cfg := &StorageConfig{
		Enabled:       true,
		Type:          "postgresql",
		PostgreSQLURL: "postgres://user:pass@localhost:5432/test",
	}
	if !cfg.Enabled {
		t.Error("Expected Enabled to be true")
	}
	if cfg.Type != "postgresql" {
		t.Error("Expected Type to be postgresql")
	}
	if cfg.PostgreSQLURL != "postgres://user:pass@localhost:5432/test" {
		t.Error("PostgreSQLURL mismatch")
	}
}

// =============================================================================
// initStoragePlugin Tests
// =============================================================================

func TestAdapter_StorageDisabled(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	adapter := NewAdapter(&Config{
		BotToken:      "xoxb-test-bot-token-123456789012-abcdef",
		SigningSecret: "test-signing-secret-123456789012345",
		Mode:          "http",
		Storage:       nil, // No storage config
	}, logger, base.WithoutServer())

	if adapter.storePlugin != nil {
		t.Error("Expected storePlugin to be nil when storage is disabled")
	}
}

func TestAdapter_StorageEnabled_Memory(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	adapter := NewAdapter(&Config{
		BotToken:      "xoxb-test-bot-token-123456789012-abcdef",
		SigningSecret: "test-signing-secret-123456789012345",
		Mode:          "http",
		Storage: &StorageConfig{
			Enabled: true,
			Type:    "memory",
		},
	}, logger, base.WithoutServer())

	if adapter.storePlugin == nil {
		t.Error("Expected storePlugin to be initialized")
	}

	// Clean up
	if err := adapter.Stop(); err != nil {
		t.Logf("Warning: failed to stop adapter: %v", err)
	}
}

func TestAdapter_Storage_SQLite(t *testing.T) {
	// Skip if CGO is not enabled (sqlite3 requires CGO)
	if testing.Short() {
		t.Skip("Skipping SQLite test in short mode")
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	adapter := NewAdapter(&Config{
		BotToken:      "xoxb-test-bot-token-123456789012-abcdef",
		SigningSecret: "test-signing-secret-123456789012345",
		Mode:          "http",
		Storage: &StorageConfig{
			Enabled:    true,
			Type:       "sqlite",
			SQLitePath: t.TempDir() + "/test.db",
		},
	}, logger, base.WithoutServer())

	// SQLite may fail if CGO is not enabled, which is acceptable in CI
	if adapter.storePlugin == nil {
		t.Skip("SQLite storage not available (likely CGO disabled)")
	}

	// Clean up
	if err := adapter.Stop(); err != nil {
		t.Logf("Warning: failed to stop adapter: %v", err)
	}
}

func TestAdapter_Storage_PostgreSQL_MissingURL(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	adapter := NewAdapter(&Config{
		BotToken:      "xoxb-test-bot-token-123456789012-abcdef",
		SigningSecret: "test-signing-secret-123456789012345",
		Mode:          "http",
		Storage: &StorageConfig{
			Enabled:       true,
			Type:          "postgresql",
			PostgreSQLURL: "", // Missing URL
		},
	}, logger, base.WithoutServer())

	// Should fail gracefully and not initialize storage
	if adapter.storePlugin != nil {
		t.Error("Expected storePlugin to be nil when PostgreSQLURL is missing")
	}
}

func TestAdapter_Storage_UnknownType(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	adapter := NewAdapter(&Config{
		BotToken:      "xoxb-test-bot-token-123456789012-abcdef",
		SigningSecret: "test-signing-secret-123456789012345",
		Mode:          "http",
		Storage: &StorageConfig{
			Enabled: true,
			Type:    "unknown",
		},
	}, logger, base.WithoutServer())

	// Should fall back to memory storage
	if adapter.storePlugin == nil {
		t.Error("Expected storePlugin to fall back to memory for unknown type")
	}

	// Clean up
	if err := adapter.Stop(); err != nil {
		t.Logf("Warning: failed to stop adapter: %v", err)
	}
}

// =============================================================================
// GetThreadHistory Tests (with memory storage)
// =============================================================================

func TestAdapter_GetThreadHistory_StorageDisabled(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	adapter := NewAdapter(&Config{
		BotToken:      "xoxb-test-bot-token-123456789012-abcdef",
		SigningSecret: "test-signing-secret-123456789012345",
		Mode:          "http",
		Storage:       nil,
	}, logger, base.WithoutServer())

	ctx := context.Background()
	_, err := adapter.GetThreadHistory(ctx, "C123", "1234567890.123456", 10)
	if err == nil {
		t.Error("Expected error when storage is disabled")
	}
	if err.Error() != "storage not enabled" {
		t.Errorf("Expected 'storage not enabled' error, got: %v", err)
	}
}

func TestAdapter_GetThreadHistory_EmptyResult(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	adapter := NewAdapter(&Config{
		BotToken:      "xoxb-test-bot-token-123456789012-abcdef",
		SigningSecret: "test-signing-secret-123456789012345",
		Mode:          "http",
		Storage: &StorageConfig{
			Enabled: true,
			Type:    "memory",
		},
	}, logger, base.WithoutServer())

	ctx := context.Background()
	messages, err := adapter.GetThreadHistory(ctx, "C123", "1234567890.123456", 10)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if len(messages) != 0 {
		t.Errorf("Expected empty messages, got %d", len(messages))
	}

	// Clean up
	_ = adapter.Stop()
}

func TestAdapter_GetThreadHistoryAsString_EmptyResult(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	adapter := NewAdapter(&Config{
		BotToken:      "xoxb-test-bot-token-123456789012-abcdef",
		SigningSecret: "test-signing-secret-123456789012345",
		Mode:          "http",
		Storage: &StorageConfig{
			Enabled: true,
			Type:    "memory",
		},
	}, logger, base.WithoutServer())

	ctx := context.Background()
	result, err := adapter.GetThreadHistoryAsString(ctx, "C123", "1234567890.123456", 10)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if result != "" {
		t.Errorf("Expected empty string, got: %q", result)
	}

	// Clean up
	_ = adapter.Stop()
}

// =============================================================================
// formatMessagesAsString Tests
// =============================================================================

func TestFormatMessagesAsString_Empty(t *testing.T) {
	result := formatMessagesAsString(nil)
	if result != "" {
		t.Errorf("Expected empty string for nil, got: %q", result)
	}

	result = formatMessagesAsString([]*storage.ChatAppMessage{})
	if result != "" {
		t.Errorf("Expected empty string for empty slice, got: %q", result)
	}
}

func TestFormatMessagesAsString_UserMessage(t *testing.T) {
	now := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	messages := []*storage.ChatAppMessage{
		{
			MessageType: types.MessageTypeUserInput,
			Content:     "Hello, world!",
			CreatedAt:   now,
		},
	}

	result := formatMessagesAsString(messages)
	expected := "[2024-01-15 10:30:00] User: Hello, world!\n"
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}
}

func TestFormatMessagesAsString_BotResponse(t *testing.T) {
	now := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	messages := []*storage.ChatAppMessage{
		{
			MessageType: types.MessageTypeFinalResponse,
			Content:     "Hi there!",
			CreatedAt:   now,
		},
	}

	result := formatMessagesAsString(messages)
	expected := "[2024-01-15 10:30:00] Assistant: Hi there!\n"
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}
}

func TestFormatMessagesAsString_Multiple(t *testing.T) {
	baseTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	messages := []*storage.ChatAppMessage{
		{
			MessageType: types.MessageTypeUserInput,
			Content:     "Hello",
			CreatedAt:   baseTime,
		},
		{
			MessageType: types.MessageTypeFinalResponse,
			Content:     "Hi there!",
			CreatedAt:   baseTime.Add(5 * time.Second),
		},
		{
			MessageType: types.MessageTypeUserInput,
			Content:     "How are you?",
			CreatedAt:   baseTime.Add(10 * time.Second),
		},
	}

	result := formatMessagesAsString(messages)
	expected := `[2024-01-15 10:30:00] User: Hello
[2024-01-15 10:30:05] Assistant: Hi there!
[2024-01-15 10:30:10] User: How are you?
`
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}
}

// =============================================================================
// storeUserMessage / storeBotResponse Tests
// =============================================================================

func TestAdapter_StoreUserMessage_StorageDisabled(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	adapter := NewAdapter(&Config{
		BotToken:      "xoxb-test-bot-token-123456789012-abcdef",
		SigningSecret: "test-signing-secret-123456789012345",
		Mode:          "http",
		Storage:       nil,
	}, logger, base.WithoutServer())

	// Should not panic when storage is disabled
	msg := &base.ChatMessage{
		UserID:  "U123",
		Content: "Hello",
		Metadata: map[string]any{
			"channel_id": "C123",
			"thread_ts":  "1234567890.123456",
		},
	}
	adapter.storeUserMessage(context.Background(), msg)
	// No assertion needed - just checking it doesn't panic
}

func TestAdapter_StoreBotResponse_StorageDisabled(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	adapter := NewAdapter(&Config{
		BotToken:      "xoxb-test-bot-token-123456789012-abcdef",
		SigningSecret: "test-signing-secret-123456789012345",
		Mode:          "http",
		Storage:       nil,
	}, logger, base.WithoutServer())

	// Should not panic when storage is disabled
	adapter.storeBotResponse(context.Background(), "session-id", "C123", "1234567890.123456", "Hello")
	// No assertion needed - just checking it doesn't panic
}

// =============================================================================
// Round-Trip Tests (Store + Retrieve)
// =============================================================================

func TestAdapter_StoreAndRetrieve_RoundTrip(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	adapter := NewAdapter(&Config{
		BotToken:      "xoxb-test-bot-token-123456789012-abcdef",
		SigningSecret: "test-signing-secret-123456789012345",
		Mode:          "http",
		BotUserID:     "B123",
		Storage: &StorageConfig{
			Enabled: true,
			Type:    "memory",
		},
	}, logger, base.WithoutServer())
	defer func() { _ = adapter.Stop() }()

	ctx := context.Background()
	channelID := "C12345"
	threadTS := "1234567890.123456"

	// Store user message
	userMsg := &base.ChatMessage{
		UserID:  "U456",
		Content: "Hello from user",
		Metadata: map[string]any{
			"channel_id": channelID,
			"thread_ts":  threadTS,
		},
	}
	adapter.storeUserMessage(ctx, userMsg)

	// Store bot response
	adapter.storeBotResponse(ctx, "test-session", channelID, threadTS, "Hello from bot")

	// Retrieve messages
	messages, err := adapter.GetThreadHistory(ctx, channelID, threadTS, 100)
	if err != nil {
		t.Fatalf("GetThreadHistory failed: %v", err)
	}

	// Verify round-trip data integrity
	if len(messages) != 2 {
		t.Fatalf("Expected 2 messages, got %d", len(messages))
	}

	// Find user and bot messages (order may vary due to storage implementation)
	var userMessage, botMessage *storage.ChatAppMessage
	for _, msg := range messages {
		switch msg.MessageType {
		case types.MessageTypeUserInput:
			userMessage = msg
		case types.MessageTypeFinalResponse:
			botMessage = msg
		}
	}

	// Verify user message
	if userMessage == nil {
		t.Fatal("User message not found in results")
	}
	if userMessage.Content != "Hello from user" {
		t.Errorf("Expected user content 'Hello from user', got %q", userMessage.Content)
	}
	if userMessage.ChatUserID != "U456" {
		t.Errorf("Expected ChatUserID 'U456', got %q", userMessage.ChatUserID)
	}

	// Verify bot response
	if botMessage == nil {
		t.Fatal("Bot message not found in results")
	}
	if botMessage.Content != "Hello from bot" {
		t.Errorf("Expected bot content 'Hello from bot', got %q", botMessage.Content)
	}
}

func TestAdapter_StoreAndRetrieveByUser_RoundTrip(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	adapter := NewAdapter(&Config{
		BotToken:      "xoxb-test-bot-token-123456789012-abcdef",
		SigningSecret: "test-signing-secret-123456789012345",
		Mode:          "http",
		BotUserID:     "B123",
		Storage: &StorageConfig{
			Enabled: true,
			Type:    "memory",
		},
	}, logger, base.WithoutServer())
	defer func() { _ = adapter.Stop() }()

	ctx := context.Background()
	channelID := "C789"
	threadTS := "9876543210.654321"
	userID1 := "U111"
	userID2 := "U222"

	// Store messages from two users
	adapter.storeUserMessage(ctx, &base.ChatMessage{
		UserID:  userID1,
		Content: "Message from user 1",
		Metadata: map[string]any{
			"channel_id": channelID,
			"thread_ts":  threadTS,
		},
	})

	adapter.storeUserMessage(ctx, &base.ChatMessage{
		UserID:  userID2,
		Content: "Message from user 2",
		Metadata: map[string]any{
			"channel_id": channelID,
			"thread_ts":  threadTS,
		},
	})

	adapter.storeBotResponse(ctx, "test-session", channelID, threadTS, "Bot response")

	// Retrieve all messages - should have 3
	allMessages, err := adapter.GetThreadHistory(ctx, channelID, threadTS, 100)
	if err != nil {
		t.Fatalf("GetThreadHistory failed: %v", err)
	}
	if len(allMessages) != 3 {
		t.Fatalf("Expected 3 total messages, got %d", len(allMessages))
	}

	// Retrieve messages filtered by user1 - should have 1
	user1Messages, err := adapter.GetThreadHistoryByUser(ctx, channelID, threadTS, userID1, 100)
	if err != nil {
		t.Fatalf("GetThreadHistoryByUser failed: %v", err)
	}
	if len(user1Messages) != 1 {
		t.Fatalf("Expected 1 message for user1, got %d", len(user1Messages))
	}
	if user1Messages[0].Content != "Message from user 1" {
		t.Errorf("Expected 'Message from user 1', got %q", user1Messages[0].Content)
	}
	if user1Messages[0].ChatUserID != userID1 {
		t.Errorf("Expected ChatUserID %q, got %q", userID1, user1Messages[0].ChatUserID)
	}

	// Retrieve messages filtered by user2 - should have 1
	user2Messages, err := adapter.GetThreadHistoryByUser(ctx, channelID, threadTS, userID2, 100)
	if err != nil {
		t.Fatalf("GetThreadHistoryByUser for user2 failed: %v", err)
	}
	if len(user2Messages) != 1 {
		t.Fatalf("Expected 1 message for user2, got %d", len(user2Messages))
	}
	if user2Messages[0].Content != "Message from user 2" {
		t.Errorf("Expected 'Message from user 2', got %q", user2Messages[0].Content)
	}

	// Retrieve messages for non-existent user - should have 0
	noMessages, err := adapter.GetThreadHistoryByUser(ctx, channelID, threadTS, "U999", 100)
	if err != nil {
		t.Fatalf("GetThreadHistoryByUser for non-existent user failed: %v", err)
	}
	if len(noMessages) != 0 {
		t.Errorf("Expected 0 messages for non-existent user, got %d", len(noMessages))
	}
}

// =============================================================================
// GetThreadHistoryByUser Tests
// =============================================================================

func TestAdapter_GetThreadHistoryByUser_StorageDisabled(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	adapter := NewAdapter(&Config{
		BotToken:      "xoxb-test-bot-token-123456789012-abcdef",
		SigningSecret: "test-signing-secret-123456789012345",
		Mode:          "http",
		Storage:       nil,
	}, logger, base.WithoutServer())

	ctx := context.Background()
	_, err := adapter.GetThreadHistoryByUser(ctx, "C123", "1234567890.123456", "U456", 10)
	if err == nil {
		t.Error("Expected error when storage is disabled")
	}
	if err.Error() != "storage not enabled" {
		t.Errorf("Expected 'storage not enabled' error, got: %v", err)
	}
}

func TestAdapter_GetThreadHistoryByUser_EmptyResult(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	adapter := NewAdapter(&Config{
		BotToken:      "xoxb-test-bot-token-123456789012-abcdef",
		SigningSecret: "test-signing-secret-123456789012345",
		Mode:          "http",
		Storage: &StorageConfig{
			Enabled: true,
			Type:    "memory",
		},
	}, logger, base.WithoutServer())

	ctx := context.Background()
	messages, err := adapter.GetThreadHistoryByUser(ctx, "C123", "1234567890.123456", "U456", 10)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if len(messages) != 0 {
		t.Errorf("Expected empty messages, got %d", len(messages))
	}

	// Clean up
	_ = adapter.Stop()
}

func TestAdapter_GetThreadHistoryByUserAsString(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	adapter := NewAdapter(&Config{
		BotToken:      "xoxb-test-bot-token-123456789012-abcdef",
		SigningSecret: "test-signing-secret-123456789012345",
		Mode:          "http",
		BotUserID:     "B123",
		Storage: &StorageConfig{
			Enabled: true,
			Type:    "memory",
		},
	}, logger, base.WithoutServer())
	defer func() { _ = adapter.Stop() }()

	ctx := context.Background()
	channelID := "C123"
	threadTS := "1234567890.123456"

	// Store a message
	adapter.storeUserMessage(ctx, &base.ChatMessage{
		UserID:  "U456",
		Content: "Test message",
		Metadata: map[string]any{
			"channel_id": channelID,
			"thread_ts":  threadTS,
		},
	})

	// Retrieve as string
	result, err := adapter.GetThreadHistoryByUserAsString(ctx, channelID, threadTS, "U456", 10)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if result == "" {
		t.Error("Expected non-empty string result")
	}
	if !containsString(result, "Test message") {
		t.Errorf("Expected result to contain 'Test message', got: %q", result)
	}
}

// Helper function
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
