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

// TestConvertToThreadMessage tests the convertToThreadMessage function
func TestConvertToThreadMessage(t *testing.T) {
	storageMsg := &storage.ChatAppMessage{
		ID:              "msg123",
		ChatSessionID:   "session456",
		ChatPlatform:    "slack",
		ChatUserID:      "U123",
		ChatBotUserID:   "UBOT",
		ChatChannelID:   "C123",
		ChatThreadID:    "T123",
		MessageType:     types.MessageTypeUser,
		Content:         "Hello world",
		FromUserName:    "testuser",
		ToUserID:        "U456",
		CreatedAt:       time.Unix(1234567890, 0),
	}

	result := convertToThreadMessage(storageMsg)

	if result.ID != storageMsg.ID {
		t.Errorf("ID mismatch: got %s, want %s", result.ID, storageMsg.ID)
	}
	if result.SessionID != storageMsg.ChatSessionID {
		t.Errorf("SessionID mismatch: got %s, want %s", result.SessionID, storageMsg.ChatSessionID)
	}
	if result.Platform != storageMsg.ChatPlatform {
		t.Errorf("Platform mismatch: got %s, want %s", result.Platform, storageMsg.ChatPlatform)
	}
	if result.UserID != storageMsg.ChatUserID {
		t.Errorf("UserID mismatch: got %s, want %s", result.UserID, storageMsg.ChatUserID)
	}
	if result.BotUserID != storageMsg.ChatBotUserID {
		t.Errorf("BotUserID mismatch: got %s, want %s", result.BotUserID, storageMsg.ChatBotUserID)
	}
	if result.ChannelID != storageMsg.ChatChannelID {
		t.Errorf("ChannelID mismatch: got %s, want %s", result.ChannelID, storageMsg.ChatChannelID)
	}
	if result.ThreadID != storageMsg.ChatThreadID {
		t.Errorf("ThreadID mismatch: got %s, want %s", result.ThreadID, storageMsg.ChatThreadID)
	}
	if result.Type != string(storageMsg.MessageType) {
		t.Errorf("Type mismatch: got %s, want %s", result.Type, storageMsg.MessageType)
	}
	if result.Content != storageMsg.Content {
		t.Errorf("Content mismatch: got %s, want %s", result.Content, storageMsg.Content)
	}
	if result.FromUser != storageMsg.FromUserName {
		t.Errorf("FromUser mismatch: got %s, want %s", result.FromUser, storageMsg.FromUserName)
	}
	if result.ToUser != storageMsg.ToUserID {
		t.Errorf("ToUser mismatch: got %s, want %s", result.ToUser, storageMsg.ToUserID)
	}
}

// TestConvertToThreadMessage_Nil tests convertToThreadMessage with nil input
func TestConvertToThreadMessage_Nil(t *testing.T) {
	result := convertToThreadMessage(nil)
	if result.ID != "" {
		t.Errorf("expected empty result for nil input, got ID=%s", result.ID)
	}
}

// TestConvertToThreadMessages tests the convertToThreadMessages function
func TestConvertToThreadMessages(t *testing.T) {
	msgs := []*storage.ChatAppMessage{
		{
			ID:            "msg1",
			ChatSessionID: "session1",
			Content:       "Message 1",
		},
		{
			ID:            "msg2",
			ChatSessionID: "session2",
			Content:       "Message 2",
		},
	}

	result := convertToThreadMessages(msgs)

	if len(result) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result))
	}
	if result[0].ID != "msg1" {
		t.Errorf("first message ID: got %s, want msg1", result[0].ID)
	}
	if result[1].ID != "msg2" {
		t.Errorf("second message ID: got %s, want msg2", result[1].ID)
	}
}

// TestConvertToThreadMessages_Empty tests convertToThreadMessages with empty slice
func TestConvertToThreadMessages_Empty(t *testing.T) {
	result := convertToThreadMessages(nil)
	if result != nil {
		t.Errorf("expected nil for nil input, got %v", result)
	}

	result = convertToThreadMessages([]*storage.ChatAppMessage{})
	if result != nil {
		t.Errorf("expected nil for empty slice, got %v", result)
	}
}

// TestAdapter_GetThreadMessages tests GetThreadMessages implementation
func TestAdapter_GetThreadMessages(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	adapter := NewAdapter(&Config{
		BotToken:      "xoxb-test-token",
		SigningSecret: "test-secret",
		Mode:          "http",
	}, logger, base.WithoutServer())

	// Test when storage is not enabled - should return error
	ctx := context.Background()
	_, err := adapter.GetThreadMessages(ctx, "C123", "T123", 10)
	if err == nil {
		t.Error("expected error when storage not enabled")
	}
}

// TestAdapter_GetThreadMessagesByUser tests GetThreadMessagesByUser implementation
func TestAdapter_GetThreadMessagesByUser(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	adapter := NewAdapter(&Config{
		BotToken:      "xoxb-test-token",
		SigningSecret: "test-secret",
		Mode:          "http",
	}, logger, base.WithoutServer())

	ctx := context.Background()
	_, err := adapter.GetThreadMessagesByUser(ctx, "C123", "T123", "U123", 10)
	if err == nil {
		t.Error("expected error when storage not enabled")
	}
}

// TestAdapter_GetThreadMessagesAsString tests GetThreadMessagesAsString implementation
func TestAdapter_GetThreadMessagesAsString(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	adapter := NewAdapter(&Config{
		BotToken:      "xoxb-test-token",
		SigningSecret: "test-secret",
		Mode:          "http",
	}, logger, base.WithoutServer())

	ctx := context.Background()
	_, err := adapter.GetThreadMessagesAsString(ctx, "C123", "T123", 10)
	if err == nil {
		t.Error("expected error when storage not enabled")
	}
}

// TestAdapter_GetThreadMessagesByUserAsString tests GetThreadMessagesByUserAsString implementation
func TestAdapter_GetThreadMessagesByUserAsString(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	adapter := NewAdapter(&Config{
		BotToken:      "xoxb-test-token",
		SigningSecret: "test-secret",
		Mode:          "http",
	}, logger, base.WithoutServer())

	ctx := context.Background()
	_, err := adapter.GetThreadMessagesByUserAsString(ctx, "C123", "T123", "U123", 10)
	if err == nil {
		t.Error("expected error when storage not enabled")
	}
}

// TestAdapter_ImplementsThreadHistoryProvider tests compile-time interface compliance
func TestAdapter_ImplementsThreadHistoryProvider(t *testing.T) {
	var _ base.ThreadHistoryProvider = (*Adapter)(nil)
}

// TestConvertToThreadMessage_Metadata tests that Metadata field is properly copied
func TestConvertToThreadMessage_Metadata(t *testing.T) {
	storageMsg := &storage.ChatAppMessage{
		ID:              "msg123",
		ChatSessionID:   "session456",
		ChatPlatform:    "slack",
		ChatUserID:      "U123",
		ChatBotUserID:   "UBOT",
		ChatChannelID:   "C123",
		ChatThreadID:    "T123",
		MessageType:     types.MessageTypeUser,
		Content:         "Hello world",
		FromUserName:    "testuser",
		ToUserID:        "U456",
		CreatedAt:       time.Now(),
		Metadata: map[string]any{
			"thread_ts":   "1234567890.123456",
			"channel_id":  "C123",
			"is_ephemeral": true,
		},
	}

	result := convertToThreadMessage(storageMsg)

	if result.Metadata == nil {
		t.Error("Expected Metadata to be copied, got nil")
	}
	if result.Metadata["thread_ts"] != "1234567890.123456" {
		t.Errorf("Expected thread_ts '1234567890.123456', got %v", result.Metadata["thread_ts"])
	}
	if result.Metadata["is_ephemeral"] != true {
		t.Errorf("Expected is_ephemeral true, got %v", result.Metadata["is_ephemeral"])
	}
}

// TestConvertToThreadMessage_MetadataNil tests when source Metadata is nil
func TestConvertToThreadMessage_MetadataNil(t *testing.T) {
	storageMsg := &storage.ChatAppMessage{
		ID:              "msg123",
		ChatSessionID:   "session456",
		ChatPlatform:    "slack",
		MessageType:     types.MessageTypeUser,
		Content:         "Hello world",
		CreatedAt:       time.Now(),
		Metadata:        nil,
	}

	result := convertToThreadMessage(storageMsg)

	// Should not panic, Metadata can be nil
	if result.ID != "msg123" {
		t.Errorf("Expected ID 'msg123', got %s", result.ID)
	}
}
