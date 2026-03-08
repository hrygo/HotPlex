// Package persistence provides session and message persistence abstractions.
package persistence

import (
	"context"
	"time"

	"github.com/hrygo/hotplex/plugins/storage"
)

// MessageHistoryStore defines the interface for retrieving message history
// for diagnostic purposes.
type MessageHistoryStore interface {
	// GetRecentMessages retrieves the most recent N messages for a session.
	GetRecentMessages(ctx context.Context, sessionID string, limit int) ([]*storage.ChatAppMessage, error)

	// GetMessagesByTimeRange retrieves messages within a time range.
	GetMessagesByTimeRange(ctx context.Context, sessionID string, start, end time.Time) ([]*storage.ChatAppMessage, error)

	// GetMessageCount returns the total message count for a session.
	GetMessageCount(ctx context.Context, sessionID string) (int64, error)

	// GetSessionDuration returns the time span of messages in a session.
	GetSessionDuration(ctx context.Context, sessionID string) (first, last time.Time, err error)
}

// StorageBackedHistory implements MessageHistoryStore using the storage plugin.
type StorageBackedHistory struct {
	store storage.ReadOnlyStore
}

// Compile-time interface compliance check
var _ MessageHistoryStore = (*StorageBackedHistory)(nil)

// NewStorageBackedHistory creates a new history store backed by the storage plugin.
func NewStorageBackedHistory(store storage.ReadOnlyStore) *StorageBackedHistory {
	return &StorageBackedHistory{store: store}
}

// GetRecentMessages retrieves the most recent N messages for a session.
func (h *StorageBackedHistory) GetRecentMessages(ctx context.Context, sessionID string, limit int) ([]*storage.ChatAppMessage, error) {
	if limit <= 0 {
		limit = 100 // Default limit
	}

	query := &storage.MessageQuery{
		ChatSessionID: sessionID,
		Limit:         limit,
		Ascending:     false, // Most recent first
		IncludeDeleted: false,
	}

	return h.store.List(ctx, query)
}

// GetMessagesByTimeRange retrieves messages within a time range.
func (h *StorageBackedHistory) GetMessagesByTimeRange(ctx context.Context, sessionID string, start, end time.Time) ([]*storage.ChatAppMessage, error) {
	query := &storage.MessageQuery{
		ChatSessionID:  sessionID,
		StartTime:      &start,
		EndTime:        &end,
		Ascending:      true, // Chronological order
		IncludeDeleted: false,
	}

	return h.store.List(ctx, query)
}

// GetMessageCount returns the total message count for a session.
func (h *StorageBackedHistory) GetMessageCount(ctx context.Context, sessionID string) (int64, error) {
	query := &storage.MessageQuery{
		ChatSessionID:  sessionID,
		IncludeDeleted: false,
	}

	return h.store.Count(ctx, query)
}

// GetSessionDuration returns the time span of messages in a session.
func (h *StorageBackedHistory) GetSessionDuration(ctx context.Context, sessionID string) (first, last time.Time, err error) {
	// Get oldest messages
	oldestQuery := &storage.MessageQuery{
		ChatSessionID:  sessionID,
		Limit:          1,
		Ascending:      true, // Oldest first
		IncludeDeleted: false,
	}

	oldest, err := h.store.List(ctx, oldestQuery)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	if len(oldest) == 0 {
		return time.Time{}, time.Time{}, nil
	}
	first = oldest[0].CreatedAt

	// Get newest messages
	newestQuery := &storage.MessageQuery{
		ChatSessionID:  sessionID,
		Limit:          1,
		Ascending:      false, // Newest first
		IncludeDeleted: false,
	}

	newest, err := h.store.List(ctx, newestQuery)
	if err != nil {
		return first, time.Time{}, err
	}
	if len(newest) > 0 {
		last = newest[0].CreatedAt
	}

	return first, last, nil
}
