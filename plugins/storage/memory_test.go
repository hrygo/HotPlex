package storage

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"
)

// TestMemoryStorage_BasicOps tests basic MemoryStorage operations
func TestMemoryStorage_BasicOps(t *testing.T) {
	// Create memory storage
	factory := &MemoryFactory{}
	store, err := factory.Create(PluginConfig{})
	if err != nil {
		t.Fatalf("Failed to create memory storage: %v", err)
	}

	ctx := context.Background()

	// Test StoreUserMessage
	msg := &ChatAppMessage{
		ChatSessionID:     "test-session-1",
		ChatPlatform:      "slack",
		ChatUserID:        "U123456",
		ChatBotUserID:     "U654321",
		ChatChannelID:     "C123456",
		EngineSessionID:   uuid.New(),
		EngineNamespace:   "hotplex",
		ProviderSessionID: "provider-1",
		ProviderType:      "claude-code",
		MessageType:       "user_input",
		FromUserID:        "U123456",
		FromUserName:      "Test User",
		Content:           "Hello, world!",
	}

	err = store.StoreUserMessage(ctx, msg)
	if err != nil {
		t.Fatalf("Failed to store user message: %v", err)
	}

	if msg.ID == "" {
		t.Error("Message ID should be generated")
	}

	// Test List
	messages, err := store.List(ctx, &MessageQuery{
		ChatSessionID: "test-session-1",
	})
	if err != nil {
		t.Fatalf("Failed to list messages: %v", err)
	}

	if len(messages) != 1 {
		t.Errorf("Expected 1 message, got %d", len(messages))
	}

	// Test Get
	retrieved, err := store.Get(ctx, msg.ID)
	if err != nil {
		t.Fatalf("Failed to get message: %v", err)
	}

	if retrieved.Content != "Hello, world!" {
		t.Errorf("Expected content 'Hello, world!', got '%s'", retrieved.Content)
	}

	// Test Count
	count, err := store.Count(ctx, &MessageQuery{
		ChatSessionID: "test-session-1",
	})
	if err != nil {
		t.Fatalf("Failed to count messages: %v", err)
	}

	if count != 1 {
		t.Errorf("Expected count 1, got %d", count)
	}

	// Test GetSessionMeta
	meta, err := store.GetSessionMeta(ctx, "test-session-1")
	if err != nil {
		t.Fatalf("Failed to get session meta: %v", err)
	}

	if meta.ChatPlatform != "slack" {
		t.Errorf("Expected platform 'slack', got '%s'", meta.ChatPlatform)
	}

	if meta.MessageCount != 1 {
		t.Errorf("Expected message count 1, got %d", meta.MessageCount)
	}

	// Test ListUserSessions
	sessions, err := store.ListUserSessions(ctx, "slack", "U123456")
	if err != nil {
		t.Fatalf("Failed to list user sessions: %v", err)
	}

	if len(sessions) != 1 {
		t.Errorf("Expected 1 session, got %d", len(sessions))
	}

	// Test DeleteSession
	err = store.DeleteSession(ctx, "test-session-1")
	if err != nil {
		t.Fatalf("Failed to delete session: %v", err)
	}

	// Verify deletion
	messages, err = store.List(ctx, &MessageQuery{
		ChatSessionID:  "test-session-1",
		IncludeDeleted: true,
	})
	if err != nil {
		t.Fatalf("Failed to list messages after delete: %v", err)
	}

	if len(messages) != 1 || !messages[0].Deleted {
		t.Error("Message should be soft deleted")
	}
}

// TestMemoryStorage_ConcurrentAccess tests concurrent access to MemoryStorage
func TestMemoryStorage_ConcurrentAccess(t *testing.T) {
	factory := &MemoryFactory{}
	store, err := factory.Create(PluginConfig{})
	if err != nil {
		t.Fatalf("Failed to create memory storage: %v", err)
	}

	ctx := context.Background()
	sessionID := "concurrent-test-session"
	engineSessionID := uuid.New()

	// Run concurrent writes
	var wg sync.WaitGroup
	numWriters := 10
	messagesPerWriter := 100

	for i := 0; i < numWriters; i++ {
		wg.Add(1)
		go func(writerID int) {
			defer wg.Done()
			for j := 0; j < messagesPerWriter; j++ {
				msg := &ChatAppMessage{
					ChatSessionID:     sessionID,
					ChatPlatform:      "slack",
					ChatUserID:        "U123456",
					EngineSessionID:   engineSessionID,
					ProviderSessionID: "provider-1",
					ProviderType:      "claude-code",
					MessageType:       "user_input",
					Content:           "Message from writer %d, count %d",
				}
				_ = store.StoreUserMessage(ctx, msg)
			}
		}(i)
	}

	wg.Wait()

	// Verify count
	count, err := store.Count(ctx, &MessageQuery{
		ChatSessionID: sessionID,
	})
	if err != nil {
		t.Fatalf("Failed to count messages: %v", err)
	}

	expected := numWriters * messagesPerWriter
	if count != int64(expected) {
		t.Errorf("Expected %d messages, got %d", expected, count)
	}

	// Verify session meta
	meta, err := store.GetSessionMeta(ctx, sessionID)
	if err != nil {
		t.Fatalf("Failed to get session meta: %v", err)
	}

	if meta.MessageCount != int64(expected) {
		t.Errorf("Expected message count %d, got %d", expected, meta.MessageCount)
	}
}

// TestMemoryStorage_StoreBotResponse tests storing bot responses
func TestMemoryStorage_StoreBotResponse(t *testing.T) {
	factory := &MemoryFactory{}
	store, err := factory.Create(PluginConfig{})
	if err != nil {
		t.Fatalf("Failed to create memory storage: %v", err)
	}

	ctx := context.Background()

	// Store user message
	userMsg := &ChatAppMessage{
		ChatSessionID:     "session-1",
		ChatPlatform:      "slack",
		ChatUserID:        "U123456",
		EngineSessionID:   uuid.New(),
		ProviderSessionID: "provider-1",
		ProviderType:      "claude-code",
		MessageType:       "user_input",
		FromUserID:        "U123456",
		Content:           "Hello",
	}

	err = store.StoreUserMessage(ctx, userMsg)
	if err != nil {
		t.Fatalf("Failed to store user message: %v", err)
	}

	// Store bot response
	botMsg := &ChatAppMessage{
		ChatSessionID:     "session-1",
		ChatPlatform:      "slack",
		ChatUserID:        "U123456",
		EngineSessionID:   userMsg.EngineSessionID,
		ProviderSessionID: "provider-1",
		ProviderType:      "claude-code",
		MessageType:       "final_response",
		FromUserID:        "U654321",
		Content:           "Hello! How can I help you?",
	}

	err = store.StoreBotResponse(ctx, botMsg)
	if err != nil {
		t.Fatalf("Failed to store bot response: %v", err)
	}

	// Verify both messages
	messages, err := store.List(ctx, &MessageQuery{
		ChatSessionID: "session-1",
	})
	if err != nil {
		t.Fatalf("Failed to list messages: %v", err)
	}

	if len(messages) != 2 {
		t.Errorf("Expected 2 messages, got %d", len(messages))
	}
}

// TestMemoryStorage_QueryByEngineSession tests querying by engine session
func TestMemoryStorage_QueryByEngineSession(t *testing.T) {
	factory := &MemoryFactory{}
	store, err := factory.Create(PluginConfig{})
	if err != nil {
		t.Fatalf("Failed to create memory storage: %v", err)
	}

	ctx := context.Background()
	engineSessionID := uuid.New()

	// Store multiple messages with same engine session
	for i := 0; i < 5; i++ {
		msg := &ChatAppMessage{
			ChatSessionID:     "session-1",
			ChatPlatform:      "slack",
			ChatUserID:        "U123456",
			EngineSessionID:   engineSessionID,
			ProviderSessionID: "provider-1",
			ProviderType:      "claude-code",
			MessageType:       "user_input",
			Content:           "Message %d",
		}
		_ = store.StoreUserMessage(ctx, msg)
	}

	// Query by engine session
	messages, err := store.List(ctx, &MessageQuery{
		EngineSessionID: engineSessionID,
	})
	if err != nil {
		t.Fatalf("Failed to query by engine session: %v", err)
	}

	if len(messages) != 5 {
		t.Errorf("Expected 5 messages, got %d", len(messages))
	}
}

// TestMemoryStorage_List_Limit tests that List respects query.Limit parameter
func TestMemoryStorage_List_Limit(t *testing.T) {
	factory := &MemoryFactory{}
	store, err := factory.Create(PluginConfig{})
	if err != nil {
		t.Fatalf("Failed to create memory storage: %v", err)
	}

	ctx := context.Background()

	// Store 10 messages
	for i := 0; i < 10; i++ {
		msg := &ChatAppMessage{
			ChatSessionID:     "limit-test-session",
			ChatPlatform:      "slack",
			ChatUserID:        "U123456",
			EngineSessionID:   uuid.New(),
			ProviderSessionID: "provider-1",
			ProviderType:      "claude-code",
			MessageType:       "user_input",
			Content:           fmt.Sprintf("Message %d", i),
		}
		err = store.StoreUserMessage(ctx, msg)
		if err != nil {
			t.Fatalf("Failed to store message: %v", err)
		}
	}

	// Test with Limit = 5
	messages, err := store.List(ctx, &MessageQuery{
		ChatSessionID: "limit-test-session",
		Limit:         5,
	})
	if err != nil {
		t.Fatalf("Failed to list messages: %v", err)
	}

	if len(messages) != 5 {
		t.Errorf("Expected 5 messages with limit, got %d", len(messages))
	}

	// Test with Limit = 0 (should return all)
	messages, err = store.List(ctx, &MessageQuery{
		ChatSessionID: "limit-test-session",
		Limit:         0,
	})
	if err != nil {
		t.Fatalf("Failed to list messages: %v", err)
	}

	if len(messages) != 10 {
		t.Errorf("Expected 10 messages with no limit, got %d", len(messages))
	}
}

// TestMessageType_IsStorable tests IsStorable functionality
func TestMessageType_IsStorable(t *testing.T) {
	// Test with types package if available
	// This is a placeholder - actual implementation depends on types.MessageType
	testCases := []struct {
		messageType string
		shouldStore bool
	}{
		{"user_input", true},
		{"final_response", true},
		{"tool_use", false},
		{"tool_result", false},
	}

	for _, tc := range testCases {
		t.Run(tc.messageType, func(t *testing.T) {
			// Actual test would use types.MessageType
			_ = tc.shouldStore
		})
	}
}

// BenchmarkMemoryStorage_Write benchmarks memory storage write performance
func BenchmarkMemoryStorage_Write(b *testing.B) {
	factory := &MemoryFactory{}
	store, err := factory.Create(PluginConfig{})
	if err != nil {
		b.Fatalf("Failed to create memory storage: %v", err)
	}

	ctx := context.Background()
	engineSessionID := uuid.New()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		msg := &ChatAppMessage{
			ChatSessionID:     "bench-session",
			ChatPlatform:      "slack",
			ChatUserID:        "U123456",
			EngineSessionID:   engineSessionID,
			ProviderSessionID: "provider-1",
			ProviderType:      "claude-code",
			MessageType:       "user_input",
			Content:           "Benchmark message",
		}
		_ = store.StoreUserMessage(ctx, msg)
	}
}

// BenchmarkMemoryStorage_Read benchmarks memory storage read performance
func BenchmarkMemoryStorage_Read(b *testing.B) {
	factory := &MemoryFactory{}
	store, err := factory.Create(PluginConfig{})
	if err != nil {
		b.Fatalf("Failed to create memory storage: %v", err)
	}

	ctx := context.Background()
	engineSessionID := uuid.New()
	sessionID := "bench-session"

	// Pre-populate storage
	for i := 0; i < 1000; i++ {
		msg := &ChatAppMessage{
			ChatSessionID:     sessionID,
			ChatPlatform:      "slack",
			ChatUserID:        "U123456",
			EngineSessionID:   engineSessionID,
			ProviderSessionID: "provider-1",
			ProviderType:      "claude-code",
			MessageType:       "user_input",
			Content:           "Benchmark message",
		}
		_ = store.StoreUserMessage(ctx, msg)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = store.List(ctx, &MessageQuery{
			ChatSessionID: sessionID,
			Limit:         100,
		})
	}
}

// BenchmarkMemoryStorage_ConcurrentWrite benchmarks concurrent write performance
func BenchmarkMemoryStorage_ConcurrentWrite(b *testing.B) {
	factory := &MemoryFactory{}
	store, err := factory.Create(PluginConfig{})
	if err != nil {
		b.Fatalf("Failed to create memory storage: %v", err)
	}

	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		engineSessionID := uuid.New()
		i := 0
		for pb.Next() {
			msg := &ChatAppMessage{
				ChatSessionID:     "bench-session",
				ChatPlatform:      "slack",
				ChatUserID:        "U123456",
				EngineSessionID:   engineSessionID,
				ProviderSessionID: "provider-1",
				ProviderType:      "claude-code",
				MessageType:       "user_input",
				Content:           "Benchmark message",
			}
			_ = store.StoreUserMessage(ctx, msg)
			i++
		}
	})
}

// TestMemoryStorage_List_NegativeLimit tests List with negative limit
func TestMemoryStorage_List_NegativeLimit(t *testing.T) {
	factory := &MemoryFactory{}
	store, err := factory.Create(PluginConfig{})
	if err != nil {
		t.Fatalf("Failed to create memory storage: %v", err)
	}

	ctx := context.Background()

	// Store a message
	msg := &ChatAppMessage{
		ChatSessionID:     "neg-limit-test",
		ChatPlatform:      "slack",
		ChatUserID:        "U123456",
		EngineSessionID:   uuid.New(),
		ProviderSessionID: "provider-1",
		ProviderType:      "claude-code",
		MessageType:       "user_input",
		Content:           "Test message",
	}
	err = store.StoreUserMessage(ctx, msg)
	if err != nil {
		t.Fatalf("Failed to store message: %v", err)
	}

	// Test with negative limit - should return all messages
	messages, err := store.List(ctx, &MessageQuery{
		ChatSessionID: "neg-limit-test",
		Limit:         -1,
	})
	if err != nil {
		t.Fatalf("Failed to list messages: %v", err)
	}

	// Negative limit should return all (or treated as no limit)
	if len(messages) != 1 {
		t.Errorf("Expected 1 message with negative limit, got %d", len(messages))
	}
}
