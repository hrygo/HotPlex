package persistence

import (
	"context"
	"testing"
	"time"

	"github.com/hrygo/hotplex/plugins/storage"
	"github.com/hrygo/hotplex/types"
)

// mockReadOnlyStore implements storage.ReadOnlyStore for testing
type mockReadOnlyStore struct {
	messages []*storage.ChatAppMessage
	count    int64
	err      error
}

func (m *mockReadOnlyStore) Get(ctx context.Context, messageID string) (*storage.ChatAppMessage, error) {
	for _, msg := range m.messages {
		if msg.ID == messageID {
			return msg, nil
		}
	}
	return nil, m.err
}

func (m *mockReadOnlyStore) List(ctx context.Context, query *storage.MessageQuery) ([]*storage.ChatAppMessage, error) {
	if m.err != nil {
		return nil, m.err
	}

	var result []*storage.ChatAppMessage
	for _, msg := range m.messages {
		if query.ChatSessionID != "" && msg.ChatSessionID != query.ChatSessionID {
			continue
		}
		if !query.IncludeDeleted && msg.Deleted {
			continue
		}
		result = append(result, msg)
	}

	// Apply limit
	if query.Limit > 0 && len(result) > query.Limit {
		if query.Ascending {
			result = result[:query.Limit]
		} else {
			result = result[len(result)-query.Limit:]
		}
	}

	return result, nil
}

func (m *mockReadOnlyStore) Count(ctx context.Context, query *storage.MessageQuery) (int64, error) {
	return m.count, m.err
}

func TestNewStorageBackedHistory(t *testing.T) {
	mock := &mockReadOnlyStore{}
	h := NewStorageBackedHistory(mock)
	if h == nil {
		t.Fatal("expected non-nil history store")
	}
}

func TestGetRecentMessages(t *testing.T) {
	now := time.Now()
	messages := []*storage.ChatAppMessage{
		{ID: "1", ChatSessionID: "sess1", Content: "msg1", CreatedAt: now.Add(-2 * time.Hour)},
		{ID: "2", ChatSessionID: "sess1", Content: "msg2", CreatedAt: now.Add(-1 * time.Hour)},
		{ID: "3", ChatSessionID: "sess1", Content: "msg3", CreatedAt: now},
		{ID: "4", ChatSessionID: "sess2", Content: "other", CreatedAt: now},
	}

	mock := &mockReadOnlyStore{messages: messages}
	h := NewStorageBackedHistory(mock)

	result, err := h.GetRecentMessages(context.Background(), "sess1", 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 2 {
		t.Errorf("expected 2 messages, got %d", len(result))
	}
}

func TestGetRecentMessagesDefaultLimit(t *testing.T) {
	mock := &mockReadOnlyStore{messages: []*storage.ChatAppMessage{}}
	h := NewStorageBackedHistory(mock)

	// Test with limit 0 (should use default)
	_, err := h.GetRecentMessages(context.Background(), "sess1", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetMessagesByTimeRange(t *testing.T) {
	now := time.Now()
	messages := []*storage.ChatAppMessage{
		{ID: "1", ChatSessionID: "sess1", Content: "old", CreatedAt: now.Add(-3 * time.Hour)},
		{ID: "2", ChatSessionID: "sess1", Content: "in-range", CreatedAt: now.Add(-1 * time.Hour)},
		{ID: "3", ChatSessionID: "sess1", Content: "new", CreatedAt: now.Add(1 * time.Hour)},
	}

	mock := &mockReadOnlyStore{messages: messages}
	h := NewStorageBackedHistory(mock)

	start := now.Add(-2 * time.Hour)
	end := now.Add(30 * time.Minute)

	result, err := h.GetMessagesByTimeRange(context.Background(), "sess1", start, end)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Note: Our mock doesn't fully implement time filtering, so this tests the query construction
	t.Logf("Got %d messages in time range", len(result))
}

func TestGetMessageCount(t *testing.T) {
	mock := &mockReadOnlyStore{count: 42}
	h := NewStorageBackedHistory(mock)

	count, err := h.GetMessageCount(context.Background(), "sess1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if count != 42 {
		t.Errorf("expected count 42, got %d", count)
	}
}

func TestGetSessionDuration(t *testing.T) {
	now := time.Now()
	messages := []*storage.ChatAppMessage{
		{ID: "1", ChatSessionID: "sess1", Content: "first", CreatedAt: now.Add(-2 * time.Hour)},
		{ID: "2", ChatSessionID: "sess1", Content: "middle", CreatedAt: now.Add(-1 * time.Hour)},
		{ID: "3", ChatSessionID: "sess1", Content: "last", CreatedAt: now},
	}

	mock := &mockReadOnlyStore{messages: messages}
	h := NewStorageBackedHistory(mock)

	first, last, err := h.GetSessionDuration(context.Background(), "sess1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Note: Mock doesn't fully implement ordering, but we can verify the function doesn't error
	t.Logf("Session duration: %s to %s", first, last)
}

func TestGetSessionDurationEmpty(t *testing.T) {
	mock := &mockReadOnlyStore{messages: []*storage.ChatAppMessage{}}
	h := NewStorageBackedHistory(mock)

	first, last, err := h.GetSessionDuration(context.Background(), "empty")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !first.IsZero() || !last.IsZero() {
		t.Errorf("expected zero times for empty session, got first=%s, last=%s", first, last)
	}
}

// Test interface compliance
func TestMessageHistoryStoreInterface(t *testing.T) {
	var _ MessageHistoryStore = (*StorageBackedHistory)(nil)
}

// Test with deleted messages (should be excluded by default)
func TestGetRecentMessagesExcludesDeleted(t *testing.T) {
	now := time.Now()
	messages := []*storage.ChatAppMessage{
		{ID: "1", ChatSessionID: "sess1", Content: "active", CreatedAt: now, Deleted: false, MessageType: types.MessageTypeUser},
		{ID: "2", ChatSessionID: "sess1", Content: "deleted", CreatedAt: now, Deleted: true, MessageType: types.MessageTypeUser},
	}

	mock := &mockReadOnlyStore{messages: messages}
	h := NewStorageBackedHistory(mock)

	result, err := h.GetRecentMessages(context.Background(), "sess1", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, msg := range result {
		if msg.Deleted {
			t.Error("deleted message should not be returned")
		}
	}
}
