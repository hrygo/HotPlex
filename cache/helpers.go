package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// CacheHelper provides convenient methods for common caching operations.
type CacheHelper struct {
	cache Cache
}

// NewCacheHelper creates a new cache helper.
func NewCacheHelper(cache Cache) *CacheHelper {
	return &CacheHelper{cache: cache}
}

// GetJSON retrieves a JSON value from the cache and unmarshals it.
func (h *CacheHelper) GetJSON(ctx context.Context, key string, out interface{}) error {
	entry, err := h.cache.Get(ctx, key)
	if err != nil {
		return err
	}
	if entry == nil {
		return ErrCacheMiss
	}
	return json.Unmarshal(entry.Value, out)
}

// SetJSON marshals a value to JSON and stores it in the cache.
func (h *CacheHelper) SetJSON(ctx context.Context, key string, value interface{}, opts ...CacheOption) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal value: %w", err)
	}
	return h.cache.Set(ctx, key, data, opts...)
}

// GetOrCompute gets a value from cache, or computes and caches it if missing.
func (h *CacheHelper) GetOrCompute(ctx context.Context, key string, compute func() ([]byte, error), opts ...CacheOption) ([]byte, error) {
	// Try to get from cache
	entry, err := h.cache.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	if entry != nil && !entry.IsExpired() {
		return entry.Value, nil
	}

	// Compute and cache
	value, err := compute()
	if err != nil {
		return nil, err
	}

	if err := h.cache.Set(ctx, key, value, opts...); err != nil {
		return nil, err
	}

	return value, nil
}

// GetOrComputeJSON gets a JSON value from cache, or computes and caches it if missing.
func (h *CacheHelper) GetOrComputeJSON(ctx context.Context, key string, compute func() (interface{}, error), out interface{}, opts ...CacheOption) error {
	// Try to get from cache
	entry, err := h.cache.Get(ctx, key)
	if err != nil {
		return err
	}
	if entry != nil && !entry.IsExpired() {
		return json.Unmarshal(entry.Value, out)
	}

	// Compute and cache
	value, err := compute()
	if err != nil {
		return err
	}

	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal computed value: %w", err)
	}

	if err := h.cache.Set(ctx, key, data, opts...); err != nil {
		return err
	}

	return json.Unmarshal(data, out)
}

// DeletePrefix deletes all keys with the given prefix.
func (h *CacheHelper) DeletePrefix(ctx context.Context, prefix string) error {
	// Note: This is a no-op for NoOpCache and may not be efficient for all backends
	// Backend-specific implementations should override this
	return nil
}

// Common cache key prefixes for HotPlex
const (
	// KeyPrefixPrompt is the prefix for prompt cache keys
	KeyPrefixPrompt = "prompt:"

	// KeyPrefixResponse is the prefix for response cache keys
	KeyPrefixResponse = "response:"

	// KeyPrefixSession is the prefix for session context cache keys
	KeyPrefixSession = "session:"

	// KeyPrefixTool is the prefix for tool result cache keys
	KeyPrefixTool = "tool:"
)

// PromptCacheKey generates a cache key for a prompt.
func PromptCacheKey(sessionID, prompt string) string {
	return KeyPrefixPrompt + ComputeKey(sessionID, prompt)
}

// ResponseCacheKey generates a cache key for a response.
func ResponseCacheKey(sessionID, prompt string, model string) string {
	return KeyPrefixResponse + ComputeKey(sessionID, prompt, model)
}

// SessionCacheKey generates a cache key for session context.
func SessionCacheKey(sessionID string) string {
	return KeyPrefixSession + sessionID
}

// ToolCacheKey generates a cache key for tool results.
func ToolCacheKey(toolName string, args map[string]interface{}) string {
	// Convert args to sorted JSON for consistent hashing
	data, _ := json.Marshal(args)
	return KeyPrefixTool + ComputeKey(toolName, string(data))
}

// Common TTL presets
var (
	// TTLShort is for short-lived cache entries (5 minutes)
	TTLShort = 5 * time.Minute

	// TTLMedium is for medium-lived cache entries (1 hour)
	TTLMedium = 1 * time.Hour

	// TTLLong is for long-lived cache entries (24 hours)
	TTLLong = 24 * time.Hour

	// TTLPermanent is for permanent cache entries (no expiration)
	TTLPermanent = time.Duration(0)
)

// ErrCacheMiss indicates that the requested key was not found in the cache.
var ErrCacheMiss = fmt.Errorf("cache miss")

// Global helper functions using the default cache

// Get is a convenience function to get a value from the default cache.
func Get(ctx context.Context, key string) (*CacheEntry, error) {
	return globalCache.Get(ctx, key)
}

// Set is a convenience function to set a value in the default cache.
func Set(ctx context.Context, key string, value []byte, opts ...CacheOption) error {
	return globalCache.Set(ctx, key, value, opts...)
}

// Delete is a convenience function to delete a value from the default cache.
func Delete(ctx context.Context, key string) error {
	return globalCache.Delete(ctx, key)
}

// GetJSON is a convenience function to get a JSON value from the default cache.
func GetJSON(ctx context.Context, key string, out interface{}) error {
	return NewCacheHelper(globalCache).GetJSON(ctx, key, out)
}

// SetJSON is a convenience function to set a JSON value in the default cache.
func SetJSON(ctx context.Context, key string, value interface{}, opts ...CacheOption) error {
	return NewCacheHelper(globalCache).SetJSON(ctx, key, value, opts...)
}

// GetOrCompute is a convenience function to get or compute a value from the default cache.
func GetOrCompute(ctx context.Context, key string, compute func() ([]byte, error), opts ...CacheOption) ([]byte, error) {
	return NewCacheHelper(globalCache).GetOrCompute(ctx, key, compute, opts...)
}
