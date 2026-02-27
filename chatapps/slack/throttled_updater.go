package slack

import (
	"log/slog"
	"sync"
	"time"
)

const (
	// DefaultMinInterval is the default minimum interval between updates (3s)
	// Slack recommends max 1 chat.update per 3 seconds to avoid rate limiting
	DefaultMinInterval = 3000 * time.Millisecond
	// DefaultMinCharDelta is the default minimum character change required (50 chars)
	DefaultMinCharDelta = 50
)

// ThrottledUpdater manages throttled message updates to avoid Slack API rate limiting.
// It implements a dual-threshold strategy: updates are only sent when both:
// - At least minInterval has passed since last update
// - At least minCharDelta characters have changed
type ThrottledUpdater struct {
	minInterval  time.Duration // Minimum update interval (default 600ms)
	minCharDelta int           // Minimum character delta (default 50)
	lastUpdate   time.Time     // Last update timestamp
	lastLen      int           // Last content length
	pendingText  string        // Pending text to be sent
	mu           sync.Mutex

	// Optional logger for debugging
	logger *slog.Logger
}

// NewThrottledUpdater creates a new ThrottledUpdater with default settings.
func NewThrottledUpdater() *ThrottledUpdater {
	return &ThrottledUpdater{
		minInterval:  DefaultMinInterval,
		minCharDelta: DefaultMinCharDelta,
		lastUpdate:   time.Time{},
		lastLen:      0,
		pendingText:  "",
		logger:       nil,
	}
}

// NewThrottledUpdaterWithConfig creates a new ThrottledUpdater with custom settings.
func NewThrottledUpdaterWithConfig(minInterval time.Duration, minCharDelta int, logger *slog.Logger) *ThrottledUpdater {
	// Apply defaults if not specified
	if minInterval <= 0 {
		minInterval = DefaultMinInterval
	}
	if minCharDelta <= 0 {
		minCharDelta = DefaultMinCharDelta
	}

	return &ThrottledUpdater{
		minInterval:  minInterval,
		minCharDelta: minCharDelta,
		lastUpdate:   time.Time{},
		lastLen:      0,
		pendingText:  "",
		logger:       logger,
	}
}

// ShouldUpdate checks if an update should be sent based on the new content length.
// Returns true if:
// - At least minInterval has passed since last update
// - At least minCharDelta characters have changed
//
// This method does not modify internal state; it only performs the check.
func (u *ThrottledUpdater) ShouldUpdate(newLen int) bool {
	u.mu.Lock()
	defer u.mu.Unlock()

	// If this is the first update (lastUpdate is zero), allow it
	if u.lastUpdate.IsZero() {
		return true
	}

	// Check time interval
	if time.Since(u.lastUpdate) < u.minInterval {
		if u.logger != nil {
			u.logger.Debug("ThrottledUpdater: interval not met",
				"since_last", time.Since(u.lastUpdate),
				"min_interval", u.minInterval)
		}
		return false
	}

	// Check character delta
	delta := newLen - u.lastLen
	if delta < 0 {
		delta = -delta // Use absolute value for shrink detection
	}
	if delta < u.minCharDelta {
		if u.logger != nil {
			u.logger.Debug("ThrottledUpdater: char delta not met",
				"delta", delta,
				"min_delta", u.minCharDelta,
				"new_len", newLen,
				"last_len", u.lastLen)
		}
		return false
	}

	return true
}

// Update attempts to update with new text.
// It stores the new text and returns whether an update should be sent.
// If ShouldUpdate returns true, caller should send the update and call UpdateComplete().
// If ShouldUpdate returns false, the text is stored pending and no update is sent.
func (u *ThrottledUpdater) Update(text string) (string, bool) {
	u.mu.Lock()
	defer u.mu.Unlock()

	newLen := len(text)
	u.pendingText = text

	// If this is the first update, always allow
	if u.lastUpdate.IsZero() {
		u.lastLen = newLen
		u.lastUpdate = time.Now()
		return text, true
	}

	// Check both thresholds
	timePassed := time.Since(u.lastUpdate)
	delta := newLen - u.lastLen
	if delta < 0 {
		delta = -delta
	}

	shouldUpdate := timePassed >= u.minInterval && delta >= u.minCharDelta

	if shouldUpdate {
		u.lastLen = newLen
		u.lastUpdate = time.Now()
		if u.logger != nil {
			u.logger.Debug("ThrottledUpdater: sending update",
				"len", newLen,
				"time_passed", timePassed)
		}
	} else {
		if u.logger != nil {
			u.logger.Debug("ThrottledUpdater: throttled",
				"pending_len", len(u.pendingText),
				"time_passed", timePassed,
				"delta", delta)
		}
	}

	return u.pendingText, shouldUpdate
}

// UpdateComplete marks the update as complete and resets the pending text.
// Should be called after successfully sending the update.
func (u *ThrottledUpdater) UpdateComplete() {
	u.mu.Lock()
	defer u.mu.Unlock()

	u.pendingText = ""
}

// ForceUpdate forces an immediate update with the current pending text.
// Returns the pending text and true if there is content to send.
// This bypasses the throttling thresholds.
func (u *ThrottledUpdater) ForceUpdate() (string, bool) {
	u.mu.Lock()
	defer u.mu.Unlock()

	if u.pendingText == "" {
		return "", false
	}

	text := u.pendingText
	u.lastLen = len(text)
	u.lastUpdate = time.Now()
	u.pendingText = ""

	if u.logger != nil {
		u.logger.Debug("ThrottledUpdater: forced update", "len", len(text))
	}

	return text, true
}

// PendingText returns the current pending text without modifying state.
func (u *ThrottledUpdater) PendingText() string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.pendingText
}

// Reset clears all state including pending text and timestamps.
// Use this when starting a new conversation or session.
func (u *ThrottledUpdater) Reset() {
	u.mu.Lock()
	defer u.mu.Unlock()

	u.lastUpdate = time.Time{}
	u.lastLen = 0
	u.pendingText = ""

	if u.logger != nil {
		u.logger.Debug("ThrottledUpdater: reset")
	}
}

// Stats returns current throttler statistics for monitoring.
func (u *ThrottledUpdater) Stats() ThrottlerStats {
	u.mu.Lock()
	defer u.mu.Unlock()

	return ThrottlerStats{
		LastUpdate:   u.lastUpdate,
		LastLen:      u.lastLen,
		PendingLen:   len(u.pendingText),
		MinInterval:  u.minInterval,
		MinCharDelta: u.minCharDelta,
	}
}

// ThrottlerStats holds statistics about the throttler state.
type ThrottlerStats struct {
	LastUpdate   time.Time
	LastLen      int
	PendingLen   int
	MinInterval  time.Duration
	MinCharDelta int
}
