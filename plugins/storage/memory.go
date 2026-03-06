package storage

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// MemoryStorage 内存存储实现
type MemoryStorage struct {
	mu       sync.RWMutex
	messages map[string]*ChatAppMessage
	sessions map[string]*SessionMeta
	config   PluginConfig
	strategy StorageStrategy
}

type MemoryFactory struct{}

// Compile-time interface compliance checks
var (
	_ ChatAppMessageStore = (*MemoryStorage)(nil)
	_ PluginFactory       = (*MemoryFactory)(nil)
)

func (f *MemoryFactory) Create(config PluginConfig) (ChatAppMessageStore, error) {
	return &MemoryStorage{
		messages: make(map[string]*ChatAppMessage),
		sessions: make(map[string]*SessionMeta),
		config:   config,
		strategy: NewDefaultStrategy(),
	}, nil
}

func (m *MemoryStorage) Initialize(ctx context.Context) error { return nil }
func (m *MemoryStorage) Close() error                         { return nil }
func (m *MemoryStorage) Name() string                         { return "memory" }
func (m *MemoryStorage) Version() string                      { return "1.0.0" }

func (m *MemoryStorage) Get(ctx context.Context, messageID string) (*ChatAppMessage, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	msg, ok := m.messages[messageID]
	if !ok || msg.Deleted {
		return nil, fmt.Errorf("message not found: %s", messageID)
	}
	return msg, nil
}

func (m *MemoryStorage) List(ctx context.Context, query *MessageQuery) ([]*ChatAppMessage, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var results []*ChatAppMessage
	for _, msg := range m.messages {
		if msg.Deleted && !query.IncludeDeleted {
			continue
		}
		if query.ChatSessionID != "" && msg.ChatSessionID != query.ChatSessionID {
			continue
		}
		results = append(results, msg)
	}
	return results, nil
}

func (m *MemoryStorage) Count(ctx context.Context, query *MessageQuery) (int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var count int64
	for _, msg := range m.messages {
		if msg.Deleted && !query.IncludeDeleted {
			continue
		}
		if query.ChatSessionID != "" && msg.ChatSessionID != query.ChatSessionID {
			continue
		}
		count++
	}
	return count, nil
}

func (m *MemoryStorage) StoreUserMessage(ctx context.Context, msg *ChatAppMessage) error {
	if m.strategy != nil && !m.strategy.ShouldStore(msg) {
		return nil
	}
	return m.storeMessage(msg)
}

func (m *MemoryStorage) StoreBotResponse(ctx context.Context, msg *ChatAppMessage) error {
	if m.strategy != nil && !m.strategy.ShouldStore(msg) {
		return nil
	}
	return m.storeMessage(msg)
}

func (m *MemoryStorage) storeMessage(msg *ChatAppMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if msg.ID == "" {
		msg.ID = uuid.New().String()
	}
	msg.CreatedAt = time.Now()
	msg.UpdatedAt = msg.CreatedAt
	m.messages[msg.ID] = msg
	m.updateSessionMeta(msg)
	return nil
}

func (m *MemoryStorage) updateSessionMeta(msg *ChatAppMessage) {
	sessionID := msg.ChatSessionID
	if sessionID == "" {
		return
	}
	meta, ok := m.sessions[sessionID]
	if !ok {
		meta = &SessionMeta{ChatSessionID: sessionID, ChatPlatform: msg.ChatPlatform, ChatUserID: msg.ChatUserID}
		m.sessions[sessionID] = meta
	}
	meta.LastMessageID = msg.ID
	meta.LastMessageAt = msg.CreatedAt
	meta.MessageCount++
}

func (m *MemoryStorage) GetSessionMeta(ctx context.Context, chatSessionID string) (*SessionMeta, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	meta, ok := m.sessions[chatSessionID]
	if !ok {
		return nil, fmt.Errorf("session not found: %s", chatSessionID)
	}
	return meta, nil
}

func (m *MemoryStorage) ListUserSessions(ctx context.Context, platform, userID string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var sessions []string
	for _, meta := range m.sessions {
		if meta.ChatPlatform == platform && meta.ChatUserID == userID {
			sessions = append(sessions, meta.ChatSessionID)
		}
	}
	return sessions, nil
}

func (m *MemoryStorage) DeleteSession(ctx context.Context, chatSessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	for _, msg := range m.messages {
		if msg.ChatSessionID == chatSessionID {
			msg.Deleted = true
			msg.DeletedAt = &now
		}
	}
	delete(m.sessions, chatSessionID)
	return nil
}

func (m *MemoryStorage) GetStrategy() StorageStrategy        { return m.strategy }
func (m *MemoryStorage) SetStrategy(s StorageStrategy) error { m.strategy = s; return nil }
