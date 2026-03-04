// Package slack provides the Slack adapter implementation for the hotplex engine.
// Per-user rate limiting for Slack commands to prevent API abuse.
package slack

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const (
	// defaultRateLimit is the default number of requests per second
	defaultRateLimit = 10.0
	// rateBurst is the burst allowance for token bucket
	rateBurst = 20
	// cleanupInterval is how often to run cleanup
	cleanupInterval = 5 * time.Minute
	// limiterTTL is how long to keep unused limiters
	limiterTTL = 10 * time.Minute
)

// SlashCommandRateLimiter provides per-user rate limiting using token bucket algorithm
type SlashCommandRateLimiter struct {
	mu       sync.RWMutex
	limiters map[string]*rate.Limiter
	lastUsed map[string]time.Time
	rate     rate.Limit
	burst    int
	done     chan struct{} // Signal to stop cleanup goroutine
}

// NewSlashCommandRateLimiter creates a new rate limiter with default settings
func NewSlashCommandRateLimiter() *SlashCommandRateLimiter {
	return NewSlashCommandRateLimiterWithConfig(defaultRateLimit, rateBurst)
}

// NewSlashCommandRateLimiterWithConfig creates a new rate limiter with custom settings
// If rps is 0 or negative, defaultRateLimit will be used
func NewSlashCommandRateLimiterWithConfig(rps float64, burst int) *SlashCommandRateLimiter {
	// Use defaults if not specified
	if rps <= 0 {
		rps = defaultRateLimit
	}
	if burst <= 0 {
		burst = rateBurst
	}

	rl := &SlashCommandRateLimiter{
		limiters: make(map[string]*rate.Limiter),
		lastUsed: make(map[string]time.Time),
		rate:     rate.Limit(rps),
		burst:    burst,
		done:     make(chan struct{}),
	}

	// Start cleanup goroutine
	go rl.cleanupLoop()

	return rl
}

// Stop gracefully stops the rate limiter cleanup goroutine
func (r *SlashCommandRateLimiter) Stop() {
	close(r.done)
}

// Allow checks if a request from the given user is allowed
// Returns true if the request is within rate limit, false if rate limited
func (r *SlashCommandRateLimiter) Allow(userID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	limiter, exists := r.limiters[userID]
	if !exists {
		// Create new limiter for user
		limiter = rate.NewLimiter(r.rate, r.burst)
		r.limiters[userID] = limiter
	}

	r.lastUsed[userID] = time.Now()
	return limiter.Allow()
}

// cleanupLoop runs periodic cleanup to prevent memory leaks
func (r *SlashCommandRateLimiter) cleanupLoop() {
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.done:
			return
		case <-ticker.C:
			r.cleanup()
		}
	}
}

// cleanup removes stale limiters that haven't been used recently
func (r *SlashCommandRateLimiter) cleanup() {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	for userID, lastUsed := range r.lastUsed {
		if now.Sub(lastUsed) > limiterTTL {
			delete(r.limiters, userID)
			delete(r.lastUsed, userID)
		}
	}
}
