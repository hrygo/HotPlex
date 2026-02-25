package chatapps

import (
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

// InteractionCallback is a function that handles an interaction callback.
type InteractionCallback func(interaction *PendingInteraction) error

// PendingInteraction represents a pending interactive action (e.g., button click).
type PendingInteraction struct {
	ID           string
	SessionID    string
	ChannelID    string
	MessageTS    string
	ActionID     string
	UserID       string
	CallbackData string
	Callback     InteractionCallback
	CreatedAt    time.Time
	ExpiresAt    time.Time
}

// InteractionManager manages pending interactions for Slack interactive components.
type InteractionManager struct {
	logger          *slog.Logger
	mu              sync.RWMutex
	pending         map[string]*PendingInteraction // interaction_id -> PendingInteraction
	cleanupInterval time.Duration
	defaultTTL      time.Duration
	stopCleanup     chan struct{}
	wg              sync.WaitGroup
}

// InteractionManagerOptions configures the InteractionManager.
type InteractionManagerOptions struct {
	CleanupInterval time.Duration // How often to run cleanup (default: 1 min)
	TTL             time.Duration // How long to keep pending interactions (default: 10 min)
}

// NewInteractionManager creates a new InteractionManager.
func NewInteractionManager(logger *slog.Logger, opts InteractionManagerOptions) *InteractionManager {
	if logger == nil {
		logger = slog.Default()
	}

	// Set defaults
	if opts.CleanupInterval == 0 {
		opts.CleanupInterval = 1 * time.Minute
	}
	if opts.TTL == 0 {
		opts.TTL = 10 * time.Minute
	}

	m := &InteractionManager{
		logger:          logger,
		pending:         make(map[string]*PendingInteraction),
		cleanupInterval: opts.CleanupInterval,
		defaultTTL:      opts.TTL,
		stopCleanup:     make(chan struct{}),
	}

	// Start cleanup goroutine
	m.wg.Add(1)
	go m.cleanupLoop()

	return m
}

// Store adds a new pending interaction and returns its ID.
func (m *InteractionManager) Store(interaction *PendingInteraction) string {
	if interaction.ID == "" {
		interaction.ID = uuid.New().String()
	}
	if interaction.CreatedAt.IsZero() {
		interaction.CreatedAt = time.Now()
	}
	if interaction.ExpiresAt.IsZero() {
		interaction.ExpiresAt = interaction.CreatedAt.Add(m.defaultTTL)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.pending[interaction.ID] = interaction

	m.logger.Debug("InteractionManager: stored interaction",
		"id", interaction.ID,
		"action_id", interaction.ActionID,
		"user_id", interaction.UserID,
		"expires_at", interaction.ExpiresAt)

	return interaction.ID
}

// Get retrieves a pending interaction by ID.
// Returns nil if not found or expired.
func (m *InteractionManager) Get(id string) (*PendingInteraction, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	interaction, exists := m.pending[id]
	if !exists {
		return nil, false
	}

	// Check if expired
	if time.Now().After(interaction.ExpiresAt) {
		return nil, false
	}

	return interaction, true
}

// Delete removes a pending interaction.
func (m *InteractionManager) Delete(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.pending, id)

	m.logger.Debug("InteractionManager: deleted interaction",
		"id", id)
}

// HandleCallback processes an interaction callback.
// It looks up the interaction, calls the callback, and removes the interaction.
func (m *InteractionManager) HandleCallback(interactionID, userID, actionID, callbackData string) error {
	interaction, exists := m.Get(interactionID)
	if !exists {
		m.logger.Warn("InteractionManager: interaction not found or expired",
			"id", interactionID,
			"user_id", userID,
			"action_id", actionID)
		return nil // Don't error on expired interactions
	}

	// Verify user matches
	if interaction.UserID != "" && interaction.UserID != userID {
		m.logger.Warn("InteractionManager: user mismatch",
			"expected", interaction.UserID,
			"got", userID)
		return nil
	}

	// Update interaction with callback data
	interaction.CallbackData = callbackData
	interaction.UserID = userID
	interaction.ActionID = actionID

	// Call the callback if set
	if interaction.Callback != nil {
		if err := interaction.Callback(interaction); err != nil {
			m.logger.Error("InteractionManager: callback error",
				"id", interactionID,
				"error", err)
			return err
		}
	}

	// Remove after handling
	m.Delete(interactionID)

	m.logger.Debug("InteractionManager: handled callback",
		"id", interactionID,
		"action_id", actionID,
		"user_id", userID)

	return nil
}

// Stop stops the cleanup goroutine.
func (m *InteractionManager) Stop() {
	close(m.stopCleanup)
	m.wg.Wait()
	m.logger.Debug("InteractionManager: stopped")
}

// cleanupLoop periodically removes expired interactions.
func (m *InteractionManager) cleanupLoop() {
	defer m.wg.Done()

	ticker := time.NewTicker(m.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopCleanup:
			return
		case <-ticker.C:
			m.cleanup()
		}
	}
}

// cleanup removes expired interactions.
func (m *InteractionManager) cleanup() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	var toDelete []string

	for id, interaction := range m.pending {
		if now.After(interaction.ExpiresAt) {
			toDelete = append(toDelete, id)
		}
	}

	for _, id := range toDelete {
		delete(m.pending, id)
	}

	if len(toDelete) > 0 {
		m.logger.Debug("InteractionManager: cleanup completed",
			"deleted", len(toDelete),
			"remaining", len(m.pending))
	}
}

// Count returns the number of pending interactions.
func (m *InteractionManager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.pending)
}
