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
// Returns empty ThreadKey if either parameter is empty to prevent key collisions.
func NewThreadKey(channelID, threadTS string) ThreadKey {
	if channelID == "" || threadTS == "" {
		return ""
	}
	return ThreadKey(channelID + ":" + threadTS)
}

// OwnedThread represents a thread owned by this bot.
type OwnedThread struct {
	// LastActive is the last activity timestamp
	LastActive time.Time
	// ClaimedAt is when ownership was first claimed
	ClaimedAt time.Time
}

// ThreadOwnershipTracker tracks which threads THIS bot owns.
// This is bot-centric: the bot maintains a set of threads it owns,
// NOT thread-centric where threads track which bots own them.
//
// In multi-bot scenarios, each bot instance independently tracks its own
// owned threads. When @BotA @BotB is mentioned, both BotA and BotB
// independently add the thread to their respective ownership sets.
//
// Ownership Rules (from docs/design/bot-behavior-spec.md):
//   - R1: First response claims ownership (this bot claims)
//   - R2: Only thread owner responds to non-@ messages
//   - R3: @BotB in BotA's thread → BotB claims ownership (BotA releases if mentioned others)
//   - R4: @BotA @BotB → Both bots independently claim ownership
//   - R5: @mentions excluding this bot → release ownership
type ThreadOwnershipTracker struct {
	mu           sync.RWMutex
	ownedThreads map[ThreadKey]*OwnedThread // threads owned by THIS bot
	ttl          time.Duration
	logger       *slog.Logger
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	cleanupInt   time.Duration
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
		ownedThreads: make(map[ThreadKey]*OwnedThread),
		ttl:          ttl,
		logger:       logger,
		ctx:          ctx,
		cancel:       cancel,
		cleanupInt:   cleanupInt,
	}

	// Start background cleanup goroutine
	t.wg.Add(1)
	go t.cleanupLoop()

	return t
}

// Claim claims ownership of a thread for THIS bot.
// Returns true if this is a new claim.
// Returns false if key is empty (invalid thread key).
func (t *ThreadOwnershipTracker) Claim(key ThreadKey) bool {
	if key == "" {
		return false
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	thread, exists := t.ownedThreads[key]
	if !exists {
		// New thread - claim ownership
		t.ownedThreads[key] = &OwnedThread{
			LastActive: now,
			ClaimedAt:  now,
		}
		t.logger.Debug("Thread ownership claimed", "thread_key", key)
		return true
	}

	// Existing thread - update activity
	thread.LastActive = now
	return false
}

// Release releases ownership of a thread for THIS bot.
func (t *ThreadOwnershipTracker) Release(key ThreadKey) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if _, exists := t.ownedThreads[key]; exists {
		delete(t.ownedThreads, key)
		t.logger.Debug("Thread ownership released", "thread_key", key)
	}
}

// Owns checks if THIS bot owns a thread (and ownership hasn't expired).
// Returns false if key is empty (invalid thread key).
func (t *ThreadOwnershipTracker) Owns(key ThreadKey) bool {
	if key == "" {
		return false
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	thread, exists := t.ownedThreads[key]
	if !exists {
		return false
	}

	// Check TTL
	return time.Since(thread.LastActive) <= t.ttl
}

// UpdateLastActive updates the last active timestamp for an owned thread.
func (t *ThreadOwnershipTracker) UpdateLastActive(key ThreadKey) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if thread, exists := t.ownedThreads[key]; exists {
		thread.LastActive = time.Now()
	}
}

// CleanupExpired removes expired thread ownerships.
// Should be called periodically.
func (t *ThreadOwnershipTracker) CleanupExpired() int {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	expired := 0
	for key, thread := range t.ownedThreads {
		if now.Sub(thread.LastActive) > t.ttl {
			delete(t.ownedThreads, key)
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
