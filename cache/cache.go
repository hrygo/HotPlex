// Package cache provides a pluggable caching layer for HotPlex.
// It supports multiple cache backends (memory, redis, semantic) with a unified interface.
// Default implementation is noop (no-op) for backward compatibility.
package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// CacheEntry represents a cached item with metadata.
type CacheEntry struct {
	// Key is the unique identifier for this entry
	Key string

	// Value is the cached data
	Value []byte

	// CreatedAt is the timestamp when this entry was created
	CreatedAt time.Time

	// ExpiresAt is the timestamp when this entry expires (zero means no expiration)
	ExpiresAt time.Time

	// Metadata contains optional metadata about the cached entry
	Metadata map[string]string
}

// IsExpired returns true if the cache entry has expired.
func (e *CacheEntry) IsExpired() bool {
	if e.ExpiresAt.IsZero() {
		return false
	}
	return time.Now().After(e.ExpiresAt)
}

// CacheOptions configures cache operations.
type CacheOptions struct {
	// TTL is the time-to-live for the cache entry
	TTL time.Duration

	// Tags are optional tags for grouping cache entries
	Tags []string

	// Metadata is optional metadata to store with the entry
	Metadata map[string]string

	// SkipCache indicates whether to skip caching for this operation
	SkipCache bool
}

// CacheOption is a functional option for CacheOptions.
type CacheOption func(*CacheOptions)

// WithTTL sets the TTL for cache entries.
func WithTTL(ttl time.Duration) CacheOption {
	return func(o *CacheOptions) {
		o.TTL = ttl
	}
}

// WithTags sets tags for cache entries.
func WithTags(tags ...string) CacheOption {
	return func(o *CacheOptions) {
		o.Tags = tags
	}
}

// WithMetadata sets metadata for cache entries.
func WithMetadata(metadata map[string]string) CacheOption {
	return func(o *CacheOptions) {
		o.Metadata = metadata
	}
}

// WithSkipCache skips caching for this operation.
func WithSkipCache(skip bool) CacheOption {
	return func(o *CacheOptions) {
		o.SkipCache = skip
	}
}

// applyOptions applies functional options to create CacheOptions.
func applyOptions(opts ...CacheOption) *CacheOptions {
	o := &CacheOptions{
		Metadata: make(map[string]string),
	}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// Cache is the main cache interface that all backends must implement.
type Cache interface {
	// Get retrieves a value from the cache by key.
	// Returns nil if the key doesn't exist or has expired.
	Get(ctx context.Context, key string) (*CacheEntry, error)

	// Set stores a value in the cache with the given key.
	// Options can specify TTL, tags, and metadata.
	Set(ctx context.Context, key string, value []byte, opts ...CacheOption) error

	// Delete removes a value from the cache by key.
	// Returns nil if the key doesn't exist (idempotent).
	Delete(ctx context.Context, key string) error

	// Exists checks if a key exists in the cache (without checking expiration).
	Exists(ctx context.Context, key string) (bool, error)

	// Clear removes all cache entries.
	// Use with caution in production.
	Clear(ctx context.Context) error

	// Close gracefully shuts down the cache backend.
	Close() error

	// Name returns the cache backend name for logging.
	Name() string
}

// TaggedCache extends Cache with tag-based operations.
type TaggedCache interface {
	Cache

	// DeleteByTag removes all cache entries with the given tag.
	DeleteByTag(ctx context.Context, tag string) error

	// ListKeysByTag returns all keys with the given tag.
	ListKeysByTag(ctx context.Context, tag string) ([]string, error)
}

// StatsProvider provides cache statistics.
type StatsProvider interface {
	// GetStats returns cache statistics.
	GetStats(ctx context.Context) (*CacheStats, error)
}

// CacheStats contains cache statistics.
type CacheStats struct {
	// Hits is the number of cache hits
	Hits int64

	// Misses is the number of cache misses
	Misses int64

	// Size is the current number of entries in the cache
	Size int64

	// Evictions is the number of evicted entries
	Evictions int64

	// Backend is the name of the cache backend
	Backend string
}

// HitRatio returns the cache hit ratio (0.0 to 1.0).
func (s *CacheStats) HitRatio() float64 {
	total := s.Hits + s.Misses
	if total == 0 {
		return 0.0
	}
	return float64(s.Hits) / float64(total)
}

// NoOpCache is a no-operation cache implementation.
// It satisfies the Cache interface but doesn't actually cache anything.
// This is the default implementation for backward compatibility.
type NoOpCache struct{}

// NewNoOpCache creates a new no-op cache instance.
func NewNoOpCache() *NoOpCache {
	return &NoOpCache{}
}

// Get implements Cache.Get (no-op).
func (c *NoOpCache) Get(ctx context.Context, key string) (*CacheEntry, error) {
	return nil, nil
}

// Set implements Cache.Set (no-op).
func (c *NoOpCache) Set(ctx context.Context, key string, value []byte, opts ...CacheOption) error {
	return nil
}

// Delete implements Cache.Delete (no-op).
func (c *NoOpCache) Delete(ctx context.Context, key string) error {
	return nil
}

// Exists implements Cache.Exists (no-op).
func (c *NoOpCache) Exists(ctx context.Context, key string) (bool, error) {
	return false, nil
}

// Clear implements Cache.Clear (no-op).
func (c *NoOpCache) Clear(ctx context.Context) error {
	return nil
}

// Close implements Cache.Close (no-op).
func (c *NoOpCache) Close() error {
	return nil
}

// Name implements Cache.Name.
func (c *NoOpCache) Name() string {
	return "noop"
}

// DeleteByTag implements TaggedCache.DeleteByTag (no-op).
func (c *NoOpCache) DeleteByTag(ctx context.Context, tag string) error {
	return nil
}

// ListKeysByTag implements TaggedCache.ListKeysByTag (no-op).
func (c *NoOpCache) ListKeysByTag(ctx context.Context, tag string) ([]string, error) {
	return []string{}, nil
}

// GetStats implements StatsProvider.GetStats (no-op).
func (c *NoOpCache) GetStats(ctx context.Context) (*CacheStats, error) {
	return &CacheStats{
		Backend: "noop",
	}, nil
}

// ComputeKey generates a cache key from input data using SHA256.
// This is useful for caching API responses, prompts, etc.
func ComputeKey(parts ...string) string {
	hash := sha256.New()
	for _, part := range parts {
		hash.Write([]byte(part))
	}
	return hex.EncodeToString(hash.Sum(nil))
}

// globalCache is the global cache instance (default: noop).
var globalCache Cache = NewNoOpCache()

// SetGlobalCache sets the global cache instance.
// This should be called during application initialization.
func SetGlobalCache(cache Cache) {
	if cache != nil {
		globalCache = cache
	}
}

// GetGlobalCache returns the global cache instance.
func GetGlobalCache() Cache {
	return globalCache
}

// DefaultCache is an alias for GetGlobalCache for convenience.
func DefaultCache() Cache {
	return globalCache
}
