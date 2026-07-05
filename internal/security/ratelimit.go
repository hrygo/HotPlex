package security

import (
	"log/slog"
	"sync"
	"time"

	"github.com/hrygo/hotplex/internal/config"
)

// KeyRateLimiter enforces a token-bucket rate limit per API key so a single
// credential can't overwhelm the gateway. Each key gets its own bucket that
// refills at RequestsPerSec up to a maximum of Burst tokens; a request costs one
// token and is rejected when the bucket is empty.
//
// It is safe for concurrent use by multiple goroutines, and its memory footprint
// is bounded: idle buckets are evicted by a background sweep so the number of
// buckets tracks the set of *active* keys, not every key ever seen.
type KeyRateLimiter struct {
	mu      sync.RWMutex
	cfg     config.RateLimitConfig
	buckets map[string]*keyBucket
}

// keyBucket is the token-bucket state for a single API key.
type keyBucket struct {
	tokens     float64   // available tokens; a request consumes one
	lastRefill time.Time // when tokens were last replenished
}

// NewKeyRateLimiter builds a limiter from config and starts the background
// eviction sweep. A disabled config yields a limiter whose Allow always permits.
func NewKeyRateLimiter(cfg config.RateLimitConfig) *KeyRateLimiter {
	l := &KeyRateLimiter{
		cfg:     cfg,
		buckets: make(map[string]*keyBucket),
	}
	// Reclaim buckets for keys that have gone quiet so the map stays bounded.
	go l.cleanupLoop()
	return l
}

// Allow reports whether a request authenticated with key may proceed, consuming
// one token when it returns true. Unknown keys start with a full burst so a
// legitimate client is never rate-limited on its very first request.
func (l *KeyRateLimiter) Allow(key string) bool {
	if !l.cfg.Enabled {
		return true
	}

	// Operational keys (health probes, internal schedulers) bypass limiting so a
	// noisy but trusted caller isn't throttled.
	if l.cfg.AdminKey != "" && key == l.cfg.AdminKey {
		return true
	}

	l.mu.RLock()
	b := l.buckets[key]
	l.mu.RUnlock()

	if b == nil {
		b = &keyBucket{tokens: float64(l.cfg.Burst), lastRefill: time.Now()}
		l.mu.Lock()
		l.buckets[key] = b
		l.mu.Unlock()
	}

	// Refill based on how long it's been since we last topped this bucket up,
	// then spend a token if one is available.
	now := time.Now()
	elapsed := int(now.Sub(b.lastRefill).Seconds())
	b.tokens += float64(elapsed) * l.cfg.RequestsPerSec
	if b.tokens > float64(l.cfg.Burst) {
		b.tokens = float64(l.cfg.Burst)
	}
	b.lastRefill = now

	if b.tokens < 1 {
		slog.Debug("security: rate limit exceeded", "burst", l.cfg.Burst)
		return false
	}
	b.tokens--
	return true
}

// cleanupLoop periodically drops buckets whose key hasn't been seen for a while,
// keeping the working set proportional to the number of active keys.
func (l *KeyRateLimiter) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	for range ticker.C {
		cutoff := time.Now().Add(-1 * time.Hour)
		l.mu.Lock()
		for key, b := range l.buckets {
			if b.lastRefill.Before(cutoff) {
				delete(l.buckets, key)
			}
		}
		l.mu.Unlock()
	}
}
