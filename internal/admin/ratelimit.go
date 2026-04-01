package admin

import (
	"sync"
	"time"
)

// simpleRateLimiter implements a token-bucket rate limiter.
type simpleRateLimiter struct {
	tokens     float64
	maxTokens  float64
	refillRate float64
	lastRefill time.Time
	mu         sync.Mutex
}

func NewRateLimiter(reqPerSec, burst int) *simpleRateLimiter {
	return &simpleRateLimiter{
		tokens:     float64(burst),
		maxTokens:  float64(burst),
		refillRate: float64(reqPerSec),
		lastRefill: time.Now(),
	}
}

// Allow returns true if a request is allowed under the rate limit.
func (r *simpleRateLimiter) Allow() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	elapsed := time.Since(r.lastRefill).Seconds()
	r.tokens = min(r.maxTokens, r.tokens+elapsed*r.refillRate)
	r.lastRefill = time.Now()
	if r.tokens >= 1 {
		r.tokens--
		return true
	}
	return false
}
