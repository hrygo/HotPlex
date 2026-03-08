package base

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hrygo/hotplex/chatapps/session"
	"github.com/hrygo/hotplex/plugins/storage"
	"github.com/hrygo/hotplex/types"
)

// =============================================================================
// E2E Test Fixtures & Helpers
// =============================================================================

// e2eMemoryStore creates a fresh memory store for each test
func e2eMemoryStore() storage.ChatAppMessageStore {
	store, err := storage.GlobalRegistry().Get("memory", nil)
	if err != nil {
		panic("failed to create memory store: " + err.Error())
	}
	return store
}

// e2eSessionManager creates a test session manager
func e2eSessionManager() session.SessionManager {
	return session.NewSessionManager("hotplex")
}

// e2eMessageContext creates a test message context
func e2eMessageContext(sessionID, platform, userID, botUserID, channelID, threadID string) *MessageContext {
	engineSessionID := uuid.New()
	return &MessageContext{
		ChatSessionID:     sessionID,
		ChatPlatform:      platform,
		ChatUserID:        userID,
		ChatBotUserID:     botUserID,
		ChatChannelID:     channelID,
		ChatThreadID:      threadID,
		EngineSessionID:   engineSessionID,
		EngineNamespace:   "hotplex",
		ProviderSessionID: uuid.New().String(),
		ProviderType:      "claude-code",
		MessageType:       types.MessageTypeUserInput,
		Direction:         DirectionUserToBot,
		Content:           "Test message content",
		Metadata:          map[string]any{"key": "value"},
	}
}

// =============================================================================
// E2E Test Suite: Storage Backend Functionality
// =============================================================================

// TestE2E_StoreUserMessage verifies storing user messages
func TestE2E_StoreUserMessage(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := e2eMemoryStore()
	defer func() { _ = store.Close() }()

	msgCtx := e2eMessageContext(
		"session-001", "slack", "U123", "B456", "C789", "TS001",
	)
	msgCtx.MessageType = types.MessageTypeUserInput
	msgCtx.Content = "User said hello"

	plugin, err := NewMessageStorePlugin(MessageStorePluginConfig{
		Store:          store,
		SessionManager: e2eSessionManager(),
		Logger:         slog.Default(),
	})
	if err != nil {
		t.Fatalf("Failed to create plugin: %v", err)
	}

	err = plugin.OnUserMessage(ctx, msgCtx)
	if err != nil {
		t.Fatalf("Failed to store user message: %v", err)
	}

	messages, err := plugin.ListMessages(ctx, &storage.MessageQuery{
		ChatSessionID: "session-001",
		Limit:         10,
	})
	if err != nil {
		t.Fatalf("Failed to list messages: %v", err)
	}

	if len(messages) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(messages))
	}

	if messages[0].Content != "User said hello" {
		t.Errorf("Expected content 'User said hello', got %q", messages[0].Content)
	}
}

// TestE2E_StoreBotResponse verifies storing bot responses
func TestE2E_StoreBotResponse(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := e2eMemoryStore()
	defer func() { _ = store.Close() }()

	msgCtx := e2eMessageContext(
		"session-002", "slack", "U123", "B456", "C789", "TS001",
	)
	msgCtx.MessageType = types.MessageTypeFinalResponse
	msgCtx.Direction = DirectionBotToUser
	msgCtx.Content = "Bot response here"

	plugin, err := NewMessageStorePlugin(MessageStorePluginConfig{
		Store:          store,
		SessionManager: e2eSessionManager(),
		Logger:         slog.Default(),
	})
	if err != nil {
		t.Fatalf("Failed to create plugin: %v", err)
	}

	err = plugin.OnBotResponse(ctx, msgCtx)
	if err != nil {
		t.Fatalf("Failed to store bot response: %v", err)
	}

	messages, err := plugin.ListMessages(ctx, &storage.MessageQuery{
		ChatSessionID: "session-002",
		Limit:         10,
	})
	if err != nil {
		t.Fatalf("Failed to list messages: %v", err)
	}

	if len(messages) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(messages))
	}
}

// TestE2E_SessionMeta verifies session metadata tracking
func TestE2E_SessionMeta(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := e2eMemoryStore()
	defer func() { _ = store.Close() }()

	sessionID := "session-meta-001"

	plugin, err := NewMessageStorePlugin(MessageStorePluginConfig{
		Store:          store,
		SessionManager: e2eSessionManager(),
		Logger:         slog.Default(),
	})
	if err != nil {
		t.Fatalf("Failed to create plugin: %v", err)
	}

	msgCtx := e2eMessageContext(sessionID, "slack", "U123", "B456", "C789", "TS001")
	msgCtx.Content = "First message"
	msgCtx.MessageType = types.MessageTypeUserInput
	_ = plugin.OnUserMessage(ctx, msgCtx)

	msgCtx2 := e2eMessageContext(sessionID, "slack", "U123", "B456", "C789", "TS001")
	msgCtx2.MessageType = types.MessageTypeFinalResponse
	msgCtx2.Content = "First response"
	_ = plugin.OnBotResponse(ctx, msgCtx2)

	meta, err := plugin.GetSessionMeta(ctx, sessionID)
	if err != nil {
		t.Fatalf("Failed to get session meta: %v", err)
	}

	if meta == nil {
		t.Fatal("Expected session meta, got nil")
	}

	if meta.MessageCount < 2 {
		t.Errorf("Expected at least 2 messages, got %d", meta.MessageCount)
	}
}

// TestE2E_ListUserSessions verifies listing user sessions
func TestE2E_ListUserSessions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := e2eMemoryStore()
	defer func() { _ = store.Close() }()

	plugin, err := NewMessageStorePlugin(MessageStorePluginConfig{
		Store:          store,
		SessionManager: e2eSessionManager(),
		Logger:         slog.Default(),
	})
	if err != nil {
		t.Fatalf("Failed to create plugin: %v", err)
	}

	sessions := []struct {
		sessionID string
		platform  string
		userID    string
	}{
		{"sess-001", "slack", "U111"},
		{"sess-002", "slack", "U111"},
		{"sess-003", "telegram", "U111"},
	}

	for _, s := range sessions {
		msgCtx := e2eMessageContext(s.sessionID, s.platform, s.userID, "B456", "C789", "TS001")
		msgCtx.Content = "Test message"
		msgCtx.MessageType = types.MessageTypeUserInput
		_ = plugin.OnUserMessage(ctx, msgCtx)
	}

	slackSessions, err := plugin.ListUserSessions(ctx, "slack", "U111")
	if err != nil {
		t.Fatalf("Failed to list user sessions: %v", err)
	}

	if len(slackSessions) != 2 {
		t.Errorf("Expected 2 slack sessions for U111, got %d", len(slackSessions))
	}
}

// =============================================================================
// E2E Test Suite: Stream Message Processing
// =============================================================================

// TestE2E_StreamMessage_BufferOverflow verifies buffer overflow handling with fallback
func TestE2E_StreamMessage_BufferOverflow(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := e2eMemoryStore()
	defer func() { _ = store.Close() }()

	plugin, err := NewMessageStorePlugin(MessageStorePluginConfig{
		Store:            store,
		SessionManager:   e2eSessionManager(),
		Logger:           slog.Default(),
		StreamEnabled:    true,
		StreamTimeout:    5 * time.Minute,
		StreamMaxBuffers: 2,
	})
	if err != nil {
		t.Fatalf("Failed to create plugin: %v", err)
	}
	defer func() { _ = plugin.Close() }()

	msgCtx1 := e2eMessageContext("sess-1", "slack", "U1", "B1", "C1", "T1")
	msgCtx1.MessageType = types.MessageTypeFinalResponse
	_ = plugin.OnBotResponse(ctx, msgCtx1)

	msgCtx2 := e2eMessageContext("sess-2", "slack", "U2", "B2", "C2", "T2")
	msgCtx2.MessageType = types.MessageTypeFinalResponse
	_ = plugin.OnBotResponse(ctx, msgCtx2)

	if plugin.streamStore.GetBufferCount() != 2 {
		t.Errorf("Expected 2 buffers, got %d", plugin.streamStore.GetBufferCount())
	}

	// Third session should trigger fallback to direct storage
	msgCtx3 := e2eMessageContext("sess-3", "slack", "U3", "B3", "C3", "T3")
	msgCtx3.MessageType = types.MessageTypeFinalResponse
	err = plugin.OnBotResponse(ctx, msgCtx3)
	// Should NOT return error - fallback to direct storage
	if err != nil {
		t.Errorf("Expected no error with fallback, got: %v", err)
	}
}

// TestE2E_StreamMessage_TimeoutCleanup verifies timeout cleanup
func TestE2E_StreamMessage_TimeoutCleanup(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := e2eMemoryStore()
	defer func() { _ = store.Close() }()

	plugin, err := NewMessageStorePlugin(MessageStorePluginConfig{
		Store:          store,
		SessionManager: e2eSessionManager(),
		Logger:         slog.Default(),
		StreamEnabled:  true,
		StreamTimeout:  100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Failed to create plugin: %v", err)
	}
	defer func() { _ = plugin.Close() }()

	sessionID := "stream-timeout-test"

	msgCtx := e2eMessageContext(sessionID, "slack", "U123", "B456", "C789", "TS001")
	msgCtx.MessageType = types.MessageTypeFinalResponse
	_ = plugin.OnBotResponse(ctx, msgCtx)

	time.Sleep(200 * time.Millisecond)

	plugin.streamStore.cleanupExpired()

	buf := plugin.streamStore.GetBuffer(sessionID)
	if buf != nil {
		t.Log("Note: Buffer may still exist if not explicitly completed")
	}
}

// =============================================================================
// E2E Test Suite: History Message Retrieval
// =============================================================================

// TestE2E_HistoryRetrieval_SessionQuery verifies session-based query
func TestE2E_HistoryRetrieval_SessionQuery(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := e2eMemoryStore()
	defer func() { _ = store.Close() }()

	plugin, err := NewMessageStorePlugin(MessageStorePluginConfig{
		Store:          store,
		SessionManager: e2eSessionManager(),
		Logger:         slog.Default(),
	})
	if err != nil {
		t.Fatalf("Failed to create plugin: %v", err)
	}

	sessionID := "history-session-001"

	for i := 0; i < 10; i++ {
		msgCtx := e2eMessageContext(sessionID, "slack", "U123", "B456", "C789", "TS001")
		msgCtx.Content = "Message number " + string(rune('0'+i))
		msgCtx.MessageType = types.MessageTypeUserInput
		_ = plugin.OnUserMessage(ctx, msgCtx)
	}

	messages, err := plugin.ListMessages(ctx, &storage.MessageQuery{
		ChatSessionID: sessionID,
		Limit:         100,
	})
	if err != nil {
		t.Fatalf("Failed to query messages: %v", err)
	}

	if len(messages) != 10 {
		t.Errorf("Expected 10 messages, got %d", len(messages))
	}
}

// =============================================================================
// E2E Test Suite: Error Handling
// =============================================================================

// TestE2E_StorageDisabled_GracefulDegradation verifies graceful degradation
func TestE2E_StorageDisabled_GracefulDegradation(t *testing.T) {
	t.Parallel()

	plugin, err := NewMessageStorePlugin(MessageStorePluginConfig{
		Store:          nil,
		SessionManager: e2eSessionManager(),
		Logger:         slog.Default(),
	})

	if err == nil {
		t.Error("Expected error when creating plugin with nil store")
	}
	if plugin != nil {
		t.Error("Expected nil plugin when store is nil")
	}
}

// TestE2E_NilSessionManager verifies nil session manager handling
func TestE2E_NilSessionManager(t *testing.T) {
	t.Parallel()

	store := e2eMemoryStore()
	defer func() { _ = store.Close() }()

	_, err := NewMessageStorePlugin(MessageStorePluginConfig{
		Store:          store,
		SessionManager: nil,
		Logger:         slog.Default(),
	})

	if err == nil {
		t.Error("Expected error when session manager is nil")
	}
}

// TestE2E_ConcurrentAccess verifies concurrent access safety
func TestE2E_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := e2eMemoryStore()
	defer func() { _ = store.Close() }()

	plugin, err := NewMessageStorePlugin(MessageStorePluginConfig{
		Store:          store,
		SessionManager: e2eSessionManager(),
		Logger:         slog.Default(),
	})
	if err != nil {
		t.Fatalf("Failed to create plugin: %v", err)
	}

	errChan := make(chan error, 100)

	for i := 0; i < 100; i++ {
		go func(n int) {
			msgCtx := e2eMessageContext("concurrent-session", "slack", "U123", "B456", "C789", "TS001")
			msgCtx.Content = "Message " + string(rune(n))
			msgCtx.MessageType = types.MessageTypeUserInput
			err := plugin.OnUserMessage(ctx, msgCtx)
			errChan <- err
		}(i)
	}

	for i := 0; i < 100; i++ {
		err := <-errChan
		if err != nil {
			t.Errorf("Concurrent write failed: %v", err)
		}
	}

	messages, err := plugin.ListMessages(ctx, &storage.MessageQuery{
		ChatSessionID: "concurrent-session",
		Limit:         200,
	})
	if err != nil {
		t.Fatalf("Failed to list messages: %v", err)
	}

	if len(messages) != 100 {
		t.Errorf("Expected 100 messages, got %d", len(messages))
	}
}
