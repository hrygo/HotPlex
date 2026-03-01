package base

import (
	"sync"
	"time"
)

// PendingMessageStore stores pending messages awaiting approval
type PendingMessageStore struct {
	messages map[string]*PendingMessage // sessionID -> PendingMessage
	mu       sync.RWMutex
	ttl      time.Duration
}

// PendingMessage represents a message pending approval
type PendingMessage struct {
	SessionID   string
	ChannelID   string
	MessageTS   string
	OriginalMsg *ChatMessage
	CreatedAt   time.Time
	Reason      string
}

// NewPendingMessageStore creates a new pending message store
func NewPendingMessageStore(ttl time.Duration) *PendingMessageStore {
	if ttl == 0 {
		ttl = 5 * time.Minute // Default 5 minute TTL
	}

	store := &PendingMessageStore{
		messages: make(map[string]*PendingMessage),
		ttl:      ttl,
	}

	// Start cleanup goroutine
	go store.cleanupLoop()

	return store
}

// Store adds a pending message to the store
func (s *PendingMessageStore) Store(sessionID string, msg *PendingMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()

	msg.CreatedAt = time.Now()
	s.messages[sessionID] = msg
}

// Get retrieves a pending message by session ID
func (s *PendingMessageStore) Get(sessionID string) (*PendingMessage, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	msg, ok := s.messages[sessionID]
	if !ok {
		return nil, false
	}

	// Check TTL
	if time.Since(msg.CreatedAt) > s.ttl {
		return nil, false
	}

	return msg, true
}

// Delete removes a pending message from the store
func (s *PendingMessageStore) Delete(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.messages, sessionID)
}

// cleanupLoop periodically removes expired pending messages
func (s *PendingMessageStore) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		s.cleanup()
	}
}

// cleanup removes expired pending messages
func (s *PendingMessageStore) cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for sessionID, msg := range s.messages {
		if now.Sub(msg.CreatedAt) > s.ttl {
			delete(s.messages, sessionID)
		}
	}
}
