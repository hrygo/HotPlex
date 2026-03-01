package chatapps

import (
	"context"
	"fmt"
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
	Type         InteractionType
	ThreadTS     string
	Status       InteractionStatus
	Response     *InteractionResponse
	Metadata     map[string]any
}

// InteractionManager manages pending interactions for Slack interactive components.
type InteractionManager struct {
	logger          *slog.Logger
	mu              sync.RWMutex
	pending         map[string]*PendingInteraction // interaction_id -> PendingInteraction
	cleanupInterval time.Duration
	defaultTTL      time.Duration
	ctx             context.Context
	cancel          context.CancelFunc
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

	ctx, cancel := context.WithCancel(context.Background())

	m := &InteractionManager{
		logger:          logger,
		pending:         make(map[string]*PendingInteraction),
		cleanupInterval: opts.CleanupInterval,
		defaultTTL:      opts.TTL,
		ctx:             ctx,
		cancel:          cancel,
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
	m.cancel()
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
		case <-m.ctx.Done():
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

// InteractionType defines the type of interactive message.
type InteractionType string

const (
	// InteractionTypePermission is for Claude Code permission requests (Issue #39).
	InteractionTypePermission InteractionType = "permission"
	// InteractionTypeApproval is for general approval requests (Issue #37).
	InteractionTypeApproval InteractionType = "approval"
	// InteractionTypeSelection is for selection/choice requests.
	InteractionTypeSelection InteractionType = "selection"
)

// InteractionStatus is the status of a pending interaction.
type InteractionStatus string

const (
	InteractionStatusPending   InteractionStatus = "pending"
	InteractionStatusCompleted InteractionStatus = "completed"
	InteractionStatusExpired   InteractionStatus = "expired"
	InteractionStatusCancelled InteractionStatus = "cancelled"
)

// InteractionResponse represents the user's response to an interaction.
type InteractionResponse struct {
	ActionID    string    `json:"action_id"`
	Value       string    `json:"value"`
	UserID      string    `json:"user_id"`
	RespondedAt time.Time `json:"responded_at"`
}

// IsExpired returns true if the interaction has expired.
func (p *PendingInteraction) IsExpired() bool {
	return time.Now().After(p.ExpiresAt)
}

// TimeUntilExpiry returns the duration until the interaction expires.
func (p *PendingInteraction) TimeUntilExpiry() time.Duration {
	return time.Until(p.ExpiresAt)
}

// GetBySession retrieves all pending interactions for a session.
func (m *InteractionManager) GetBySession(sessionID string) []*PendingInteraction {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var results []*PendingInteraction
	for _, interaction := range m.pending {
		if interaction.SessionID == sessionID && !time.Now().After(interaction.ExpiresAt) {
			results = append(results, interaction)
		}
	}

	return results
}

// Complete marks an interaction as completed with a response.
func (m *InteractionManager) Complete(id string, response *InteractionResponse) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	interaction, exists := m.pending[id]
	if !exists {
		return fmt.Errorf("interaction %s not found", id)
	}

	if time.Now().After(interaction.ExpiresAt) {
		return fmt.Errorf("interaction %s has expired", id)
	}

	interaction.Status = InteractionStatusCompleted
	interaction.Response = response

	m.logger.Debug("InteractionManager: completed interaction",
		"id", id,
		"response_value", response.Value)

	return nil
}

// Expire marks an interaction as expired.
func (m *InteractionManager) Expire(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	interaction, exists := m.pending[id]
	if !exists {
		return fmt.Errorf("interaction %s not found", id)
	}

	interaction.Status = InteractionStatusExpired

	m.logger.Debug("InteractionManager: expired interaction", "id", id)
	return nil
}

// PendingCount returns the number of pending (non-expired) interactions.
func (m *InteractionManager) PendingCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	count := 0
	for _, interaction := range m.pending {
		if !interaction.IsExpired() && interaction.Status == InteractionStatusPending {
			count++
		}
	}
	return count
}

// TotalCount returns the total number of interactions (including expired).
func (m *InteractionManager) TotalCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.pending)
}

// GenerateInteractionID generates a unique interaction ID.
func GenerateInteractionID(sessionID string, timestamp time.Time) string {
	return fmt.Sprintf("int_%s_%d", sessionID, timestamp.UnixNano())
}

// CreatePendingInteraction creates a new PendingInteraction with default values.
func CreatePendingInteraction(
	sessionID string,
	userID string,
	channelID string,
	interactionType InteractionType,
	metadata map[string]any,
	ttl time.Duration,
) *PendingInteraction {
	now := time.Now()

	return &PendingInteraction{
		ID:        GenerateInteractionID(sessionID, now),
		Type:      interactionType,
		SessionID: sessionID,
		UserID:    userID,
		ChannelID: channelID,
		CreatedAt: now,
		ExpiresAt: now.Add(ttl),
		Metadata:  metadata,
		Status:    InteractionStatusPending,
		ThreadTS:  "",
	}
}
