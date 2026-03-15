package diag

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

// DiagnosisCache provides deduplication for similar diagnoses.
// It uses content hashing to detect duplicate error patterns.
type DiagnosisCache struct {
	entries  map[string]*cacheEntry
	ttl      time.Duration
	maxSize  int
	mu       sync.RWMutex
	hitCount int64
	missCount int64
}

type cacheEntry struct {
	result    *DiagResult
	hash      string
	createdAt time.Time
	hits      int64
}

// NewDiagnosisCache creates a new diagnosis cache.
func NewDiagnosisCache(ttl time.Duration, maxSize int) *DiagnosisCache {
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	if maxSize <= 0 {
		maxSize = 100
	}
	return &DiagnosisCache{
		entries: make(map[string]*cacheEntry),
		ttl:     ttl,
		maxSize: maxSize,
	}
}

// ComputeHash generates a hash for a diagnosis context.
// This is used to detect duplicate/similar diagnoses.
func ComputeHash(ctx *DiagContext) string {
	h := sha256.New()

	// Include key fields in hash
	h.Write([]byte(ctx.OriginalSessionID))
	h.Write([]byte(ctx.Platform))
	h.Write([]byte(ctx.Trigger))

	if ctx.Error != nil {
		h.Write([]byte(ctx.Error.Type))
		h.Write([]byte(ctx.Error.Message))
	}

	if ctx.Conversation != nil {
		// Only hash first 1KB of conversation to avoid huge hashes
		conv := ctx.Conversation.Processed
		if len(conv) > 1024 {
			conv = conv[:1024]
		}
		h.Write([]byte(conv))
	}

	return hex.EncodeToString(h.Sum(nil))
}

// Get retrieves a cached diagnosis by hash.
func (c *DiagnosisCache) Get(hash string) (*DiagResult, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.entries[hash]
	if !ok {
		c.missCount++
		return nil, false
	}

	// Check TTL
	if time.Since(entry.createdAt) > c.ttl {
		c.missCount++
		return nil, false
	}

	entry.hits++
	c.hitCount++
	return entry.result, true
}

// Set stores a diagnosis result in the cache.
func (c *DiagnosisCache) Set(hash string, result *DiagResult) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check capacity and evict oldest if needed
	if len(c.entries) >= c.maxSize {
		c.evictOldest()
	}

	c.entries[hash] = &cacheEntry{
		result:    result,
		hash:      hash,
		createdAt: time.Now(),
		hits:      0,
	}
}

// evictOldest removes the oldest entry (called with lock held).
func (c *DiagnosisCache) evictOldest() {
	var oldestKey string
	var oldestTime time.Time

	for k, v := range c.entries {
		if oldestKey == "" || v.createdAt.Before(oldestTime) {
			oldestKey = k
			oldestTime = v.createdAt
		}
	}

	if oldestKey != "" {
		delete(c.entries, oldestKey)
	}
}

// Stats returns cache statistics.
func (c *DiagnosisCache) Stats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return CacheStats{
		Size:      len(c.entries),
		MaxSize:   c.maxSize,
		TTL:       c.ttl,
		HitCount:  c.hitCount,
		MissCount: c.missCount,
		HitRate:   c.hitRateLocked(),
	}
}

func (c *DiagnosisCache) hitRateLocked() float64 {
	total := c.hitCount + c.missCount
	if total == 0 {
		return 0
	}
	return float64(c.hitCount) / float64(total)
}

// CacheStats contains cache statistics.
type CacheStats struct {
	Size      int
	MaxSize   int
	TTL       time.Duration
	HitCount  int64
	MissCount int64
	HitRate   float64
}

// Clear clears the cache.
func (c *DiagnosisCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries = make(map[string]*cacheEntry)
	c.hitCount = 0
	c.missCount = 0
}

// Cleanup removes expired entries.
func (c *DiagnosisCache) Cleanup() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	cleaned := 0

	for k, v := range c.entries {
		if now.Sub(v.createdAt) > c.ttl {
			delete(c.entries, k)
			cleaned++
		}
	}

	return cleaned
}

// DedupResult represents the result of a deduplication check.
type DedupResult struct {
	IsDuplicate bool
	Existing    *DiagResult
	Hash        string
}

// CheckDuplicate checks if a diagnosis is a duplicate.
func (c *DiagnosisCache) CheckDuplicate(ctx *DiagContext) *DedupResult {
	hash := ComputeHash(ctx)

	result, ok := c.Get(hash)
	return &DedupResult{
		IsDuplicate: ok,
		Existing:    result,
		Hash:        hash,
	}
}

// Store stores a diagnosis with its computed hash.
func (c *DiagnosisCache) Store(ctx *DiagContext, result *DiagResult) {
	hash := ComputeHash(ctx)
	c.Set(hash, result)
}
