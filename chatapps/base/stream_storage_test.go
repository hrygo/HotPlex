package base

import (
	"context"
	"testing"
	"time"

	"github.com/hrygo/hotplex/plugins/storage"
	"github.com/hrygo/hotplex/types"
)

func TestStreamBuffer_Append(t *testing.T) {
	t.Parallel()
	buf := &StreamBuffer{
		SessionID:   "test-session",
		Chunks:      make([]string, 0),
		LastUpdated: time.Now(),
	}

	buf.Append("Hello ")
	buf.Append("World")
	buf.Append("!")

	if len(buf.Chunks) != 3 {
		t.Errorf("expected 3 chunks, got %d", len(buf.Chunks))
	}

	merged := buf.Merge()
	if merged != "Hello World!" {
		t.Errorf("expected 'Hello World!', got '%s'", merged)
	}
}

func TestStreamBuffer_IsExpired(t *testing.T) {
	t.Parallel()
	buf := &StreamBuffer{
		SessionID:   "test-session",
		Chunks:      make([]string, 0),
		LastUpdated: time.Now().Add(-2 * time.Minute),
	}

	if !buf.IsExpired(1 * time.Minute) {
		t.Error("expected buffer to be expired")
	}

	if buf.IsExpired(5 * time.Minute) {
		t.Error("expected buffer to not be expired")
	}
}

func TestStreamMessageStore_OnStreamChunk(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mockStore := &mockStorage{}
	store := NewStreamMessageStore(mockStore, 5*time.Minute, 100, nil)
	defer store.Close()

	sessionID := "test-session-1"
	chunks := []string{"Hello ", "World", "!"}

	for _, chunk := range chunks {
		if err := store.OnStreamChunk(ctx, sessionID, chunk); err != nil {
			t.Fatalf("OnStreamChunk failed: %v", err)
		}
	}

	buf := store.GetBuffer(sessionID)
	if buf == nil {
		t.Fatal("buffer not found")
	}

	if len(buf.Chunks) != 3 {
		t.Errorf("expected 3 chunks, got %d", len(buf.Chunks))
	}
}

func TestStreamMessageStore_OnStreamComplete(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mockStore := &mockStorage{}
	store := NewStreamMessageStore(mockStore, 5*time.Minute, 100, nil)
	defer store.Close()

	sessionID := "test-session-2"
	chunks := []string{"Test ", "Message"}

	for _, chunk := range chunks {
		_ = store.OnStreamChunk(ctx, sessionID, chunk)
	}

	msg := &storage.ChatAppMessage{
		ChatSessionID: sessionID,
		MessageType:   types.MessageTypeFinalResponse,
	}

	if err := store.OnStreamComplete(ctx, sessionID, msg); err != nil {
		t.Fatalf("OnStreamComplete failed: %v", err)
	}

	// Verify buffer was cleared
	if store.GetBuffer(sessionID) != nil {
		t.Error("expected buffer to be cleared after completion")
	}

	// Verify stored message has merged content
	if mockStore.lastStoredMsg == nil {
		t.Fatal("no message was stored")
	}
	if mockStore.lastStoredMsg.Content != "Test Message" {
		t.Errorf("expected 'Test Message', got '%s'", mockStore.lastStoredMsg.Content)
	}
}

func TestStreamMessageStore_CleanupExpired(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mockStore := &mockStorage{}
	// Very short timeout for testing
	store := NewStreamMessageStore(mockStore, 100*time.Millisecond, 100, nil)
	defer store.Close()

	sessionID := "test-session-expired"
	_ = store.OnStreamChunk(ctx, sessionID, "test")

	// Wait for timeout
	time.Sleep(200 * time.Millisecond)

	// Trigger cleanup
	store.cleanupExpired()

	// Buffer should be cleaned up
	if store.GetBuffer(sessionID) != nil {
		t.Error("expected expired buffer to be cleaned up")
	}
}

// mockStorage is a mock implementation of storage.ChatAppMessageStore for testing
type mockStorage struct {
	storedMsgs    []*storage.ChatAppMessage
	lastStoredMsg *storage.ChatAppMessage
}

func (m *mockStorage) Initialize(ctx context.Context) error { return nil }
func (m *mockStorage) Close() error                         { return nil }
func (m *mockStorage) Name() string                         { return "mock" }
func (m *mockStorage) Version() string                      { return "1.0.0" }
func (m *mockStorage) Get(ctx context.Context, messageID string) (*storage.ChatAppMessage, error) {
	return nil, nil
}
func (m *mockStorage) List(ctx context.Context, query *storage.MessageQuery) ([]*storage.ChatAppMessage, error) {
	return nil, nil
}
func (m *mockStorage) Count(ctx context.Context, query *storage.MessageQuery) (int64, error) {
	return 0, nil
}
func (m *mockStorage) StoreUserMessage(ctx context.Context, msg *storage.ChatAppMessage) error {
	m.storedMsgs = append(m.storedMsgs, msg)
	m.lastStoredMsg = msg
	return nil
}
func (m *mockStorage) StoreBotResponse(ctx context.Context, msg *storage.ChatAppMessage) error {
	m.storedMsgs = append(m.storedMsgs, msg)
	m.lastStoredMsg = msg
	return nil
}
func (m *mockStorage) GetSessionMeta(ctx context.Context, chatSessionID string) (*storage.SessionMeta, error) {
	return nil, nil
}
func (m *mockStorage) ListUserSessions(ctx context.Context, platform, userID string) ([]string, error) {
	return nil, nil
}
func (m *mockStorage) DeleteSession(ctx context.Context, chatSessionID string) error { return nil }
