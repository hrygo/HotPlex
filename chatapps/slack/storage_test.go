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
			Enabled:   true,
			Type:      "sqlite",
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
