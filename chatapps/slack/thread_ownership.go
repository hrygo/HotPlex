package slack

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// ThreadKey uniquely identifies a Slack thread.
// Format: channelID:threadTS
type ThreadKey string

// NewThreadKey creates a ThreadKey from channel ID and thread timestamp.
func NewThreadKey(channelID, threadTS string) ThreadKey {
	return ThreadKey(channelID + ":" + threadTS)
}

// ThreadOwner represents ownership state of a thread.
type ThreadOwner struct {
	// OwnerIDs is the set of bot user IDs that own this thread
	OwnerIDs map[string]struct{}
	// LastActive is the last activity timestamp
	LastActive time.Time
	// ClaimedAt is when ownership was first claimed
	ClaimedAt time.Time
}

// ThreadOwnershipTracker tracks which bots own which threads.
// Thread ownership determines which bot should respond to non-@ messages in a thread.
//
// Ownership Rules (from docs/design/bot-behavior-spec.md):
//   - R1: First bot to respond claims ownership
//   - R2: Only thread owner responds to non-@ messages
//   - R3: @BotB transfers ownership from BotA to BotB
//   - R4: @BotA @BotB creates shared ownership
//   - R5: @mentions excluding current owner releases ownership
type ThreadOwnershipTracker struct {
	mu         sync.RWMutex
	threads    map[ThreadKey]*ThreadOwner
	ttl        time.Duration
	logger     *slog.Logger
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	cleanupInt time.Duration
}

// NewThreadOwnershipTracker creates a new thread ownership tracker.
// The tracker automatically cleans up expired ownerships every TTL/2 interval.
func NewThreadOwnershipTracker(ttl time.Duration, logger *slog.Logger) *ThreadOwnershipTracker {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	cleanupInt := ttl / 2
	if cleanupInt < time.Minute {
		cleanupInt = time.Minute
	}
	if cleanupInt > time.Hour {
		cleanupInt = time.Hour
	}

	ctx, cancel := context.WithCancel(context.Background())

	t := &ThreadOwnershipTracker{
		threads:    make(map[ThreadKey]*ThreadOwner),
		ttl:        ttl,
		logger:     logger,
		ctx:        ctx,
		cancel:     cancel,
		cleanupInt: cleanupInt,
	}

	// Start background cleanup goroutine
	t.wg.Add(1)
	go t.cleanupLoop()

	return t
}

// ClaimOwnership claims ownership of a thread for the specified bot.
// Returns true if this is a new claim.
func (t *ThreadOwnershipTracker) ClaimOwnership(key ThreadKey, botUserID string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	owner, exists := t.threads[key]
	if !exists {
		// New thread - claim ownership
		t.threads[key] = &ThreadOwner{
			OwnerIDs:   map[string]struct{}{botUserID: {}},
			LastActive: now,
			ClaimedAt:  now,
		}
		t.logger.Debug("Thread ownership claimed",
			"thread_key", key,
			"bot_user_id", botUserID)
		return true
	}

	// Existing thread - add to owners
	owner.OwnerIDs[botUserID] = struct{}{}
	owner.LastActive = now
	return false
}

// ReleaseOwnership removes ownership for a bot from a thread.
func (t *ThreadOwnershipTracker) ReleaseOwnership(key ThreadKey, botUserID string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	owner, exists := t.threads[key]
	if !exists {
		return
	}

	delete(owner.OwnerIDs, botUserID)
	if len(owner.OwnerIDs) == 0 {
		delete(t.threads, key)
		t.logger.Debug("Thread ownership released",
			"thread_key", key,
			"bot_user_id", botUserID)
	}
}

// TransferOwnership transfers ownership from one bot to another.
// This is triggered when @BotB is mentioned in BotA's thread.
func (t *ThreadOwnershipTracker) TransferOwnership(key ThreadKey, fromBotID, toBotID string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	owner, exists := t.threads[key]
	if !exists {
		// No existing owner - just claim
		t.threads[key] = &ThreadOwner{
			OwnerIDs:   map[string]struct{}{toBotID: {}},
			LastActive: time.Now(),
			ClaimedAt:  time.Now(),
		}
		return
	}

	// Remove old owner, add new
	delete(owner.OwnerIDs, fromBotID)
	owner.OwnerIDs[toBotID] = struct{}{}
	owner.LastActive = time.Now()

	t.logger.Debug("Thread ownership transferred",
		"thread_key", key,
		"from_bot_id", fromBotID,
		"to_bot_id", toBotID)
}

// SetOwners sets multiple owners for a thread (multi-owner support).
// This is triggered when @BotA @BotB is mentioned together.
func (t *ThreadOwnershipTracker) SetOwners(key ThreadKey, botUserIDs []string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	owner, exists := t.threads[key]
	if !exists {
		owner = &ThreadOwner{
			OwnerIDs:   make(map[string]struct{}),
			LastActive: now,
			ClaimedAt:  now,
		}
		t.threads[key] = owner
	}

	// Clear existing owners and set new ones
	owner.OwnerIDs = make(map[string]struct{})
	for _, id := range botUserIDs {
		owner.OwnerIDs[id] = struct{}{}
	}
	owner.LastActive = now

	t.logger.Debug("Thread owners set",
		"thread_key", key,
		"owner_ids", botUserIDs)
}

// IsOwner checks if a bot owns a thread.
func (t *ThreadOwnershipTracker) IsOwner(key ThreadKey, botUserID string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	owner, exists := t.threads[key]
	if !exists {
		return false
	}

	// Check TTL
	if time.Since(owner.LastActive) > t.ttl {
		return false
	}

	_, owned := owner.OwnerIDs[botUserID]
	return owned
}

// GetOwners returns all owners of a thread.
// Returns nil if thread has no owners or ownership has expired.
func (t *ThreadOwnershipTracker) GetOwners(key ThreadKey) []string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	owner, exists := t.threads[key]
	if !exists {
		return nil
	}

	// Check TTL
	if time.Since(owner.LastActive) > t.ttl {
		return nil
	}

	owners := make([]string, 0, len(owner.OwnerIDs))
	for id := range owner.OwnerIDs {
		owners = append(owners, id)
	}
	return owners
}

// HasOwner checks if a thread has any owner (not expired).
func (t *ThreadOwnershipTracker) HasOwner(key ThreadKey) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	owner, exists := t.threads[key]
	if !exists {
		return false
	}

	return time.Since(owner.LastActive) <= t.ttl
}

// UpdateLastActive updates the last active timestamp for a thread.
func (t *ThreadOwnershipTracker) UpdateLastActive(key ThreadKey) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if owner, exists := t.threads[key]; exists {
		owner.LastActive = time.Now()
	}
}

// CleanupExpired removes expired thread ownerships.
// Should be called periodically.
func (t *ThreadOwnershipTracker) CleanupExpired() int {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	expired := 0
	for key, owner := range t.threads {
		if now.Sub(owner.LastActive) > t.ttl {
			delete(t.threads, key)
			expired++
		}
	}

	if expired > 0 {
		t.logger.Debug("Cleaned up expired thread ownerships", "count", expired)
	}
	return expired
}

// cleanupLoop runs periodically to clean up expired ownerships.
func (t *ThreadOwnershipTracker) cleanupLoop() {
	defer t.wg.Done()

	ticker := time.NewTicker(t.cleanupInt)
	defer ticker.Stop()

	for {
		select {
		case <-t.ctx.Done():
			t.logger.Debug("Thread ownership cleanup loop stopped")
			return
		case <-ticker.C:
			t.CleanupExpired()
		}
	}
}

// Stop stops the background cleanup goroutine.
func (t *ThreadOwnershipTracker) Stop() {
	if t.cancel != nil {
		t.cancel()
	}
	t.wg.Wait()
}
