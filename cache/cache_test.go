package cache

import (
	"context"
	"testing"
	"time"
)

func TestNoOpCache(t *testing.T) {
	ctx := context.Background()
	cache := NewNoOpCache()

	// Test Get (should return nil)
	entry, err := cache.Get(ctx, "test-key")
	if err != nil {
		t.Errorf("Get returned error: %v", err)
	}
	if entry != nil {
		t.Errorf("Get should return nil for NoOpCache, got: %v", entry)
	}

	// Test Set (should succeed)
	err = cache.Set(ctx, "test-key", []byte("test-value"))
	if err != nil {
		t.Errorf("Set returned error: %v", err)
	}

	// Test Delete (should succeed)
	err = cache.Delete(ctx, "test-key")
	if err != nil {
		t.Errorf("Delete returned error: %v", err)
	}

	// Test Exists (should return false)
	exists, err := cache.Exists(ctx, "test-key")
	if err != nil {
		t.Errorf("Exists returned error: %v", err)
	}
	if exists {
		t.Errorf("Exists should return false for NoOpCache")
	}

	// Test Clear (should succeed)
	err = cache.Clear(ctx)
	if err != nil {
		t.Errorf("Clear returned error: %v", err)
	}

	// Test Close (should succeed)
	err = cache.Close()
	if err != nil {
		t.Errorf("Close returned error: %v", err)
	}

	// Test Name
	if cache.Name() != "noop" {
		t.Errorf("Name should return 'noop', got: %s", cache.Name())
	}
}

func TestNoOpCacheTagged(t *testing.T) {
	ctx := context.Background()
	cache := NewNoOpCache()

	// Test DeleteByTag (should succeed)
	err := cache.DeleteByTag(ctx, "test-tag")
	if err != nil {
		t.Errorf("DeleteByTag returned error: %v", err)
	}

	// Test ListKeysByTag (should return empty slice)
	keys, err := cache.ListKeysByTag(ctx, "test-tag")
	if err != nil {
		t.Errorf("ListKeysByTag returned error: %v", err)
	}
	if len(keys) != 0 {
		t.Errorf("ListKeysByTag should return empty slice, got: %v", keys)
	}
}

func TestNoOpCacheStats(t *testing.T) {
	ctx := context.Background()
	cache := NewNoOpCache()

	stats, err := cache.GetStats(ctx)
	if err != nil {
		t.Errorf("GetStats returned error: %v", err)
	}
	if stats.Backend != "noop" {
		t.Errorf("Stats.Backend should be 'noop', got: %s", stats.Backend)
	}
	if stats.HitRatio() != 0.0 {
		t.Errorf("Stats.HitRatio should be 0.0, got: %f", stats.HitRatio())
	}
}

func TestCacheEntry(t *testing.T) {
	now := time.Now()

	// Test entry without expiration
	entry := &CacheEntry{
		Key:       "test",
		Value:     []byte("value"),
		CreatedAt: now,
	}
	if entry.IsExpired() {
		t.Errorf("Entry without expiration should not be expired")
	}

	// Test entry with future expiration
	entry.ExpiresAt = now.Add(1 * time.Hour)
	if entry.IsExpired() {
		t.Errorf("Entry with future expiration should not be expired")
	}

	// Test entry with past expiration
	entry.ExpiresAt = now.Add(-1 * time.Hour)
	if !entry.IsExpired() {
		t.Errorf("Entry with past expiration should be expired")
	}
}

func TestCacheOptions(t *testing.T) {
	opts := applyOptions(
		WithTTL(1*time.Hour),
		WithTags("tag1", "tag2"),
		WithMetadata(map[string]string{"key": "value"}),
		WithSkipCache(true),
	)

	if opts.TTL != 1*time.Hour {
		t.Errorf("TTL should be 1h, got: %v", opts.TTL)
	}
	if len(opts.Tags) != 2 {
		t.Errorf("Should have 2 tags, got: %d", len(opts.Tags))
	}
	if opts.Metadata["key"] != "value" {
		t.Errorf("Metadata should contain key=value")
	}
	if !opts.SkipCache {
		t.Errorf("SkipCache should be true")
	}
}

func TestComputeKey(t *testing.T) {
	// Test deterministic hashing
	key1 := ComputeKey("test", "value")
	key2 := ComputeKey("test", "value")
	if key1 != key2 {
		t.Errorf("ComputeKey should be deterministic: %s != %s", key1, key2)
	}

	// Test different inputs produce different keys
	key3 := ComputeKey("test", "different")
	if key1 == key3 {
		t.Errorf("Different inputs should produce different keys")
	}

	// Test key format (should be hex-encoded SHA256 = 64 chars)
	if len(key1) != 64 {
		t.Errorf("Key should be 64 chars (SHA256 hex), got: %d", len(key1))
	}
}

func TestCacheKeyHelpers(t *testing.T) {
	// Test PromptCacheKey
	promptKey := PromptCacheKey("session123", "test prompt")
	if len(promptKey) == 0 {
		t.Errorf("PromptCacheKey should not be empty")
	}

	// Test ResponseCacheKey
	responseKey := ResponseCacheKey("session123", "test prompt", "claude-3-5-sonnet")
	if len(responseKey) == 0 {
		t.Errorf("ResponseCacheKey should not be empty")
	}

	// Test SessionCacheKey
	sessionKey := SessionCacheKey("session123")
	if sessionKey != KeyPrefixSession+"session123" {
		t.Errorf("SessionCacheKey format incorrect: %s", sessionKey)
	}

	// Test ToolCacheKey
	toolKey := ToolCacheKey("bash", map[string]interface{}{"command": "ls"})
	if len(toolKey) == 0 {
		t.Errorf("ToolCacheKey should not be empty")
	}
}

func TestGlobalCache(t *testing.T) {
	// Test default global cache (should be NoOpCache)
	cache := GetGlobalCache()
	if cache.Name() != "noop" {
		t.Errorf("Default global cache should be NoOpCache")
	}

	// Test SetGlobalCache
	customCache := NewNoOpCache()
	SetGlobalCache(customCache)
	if GetGlobalCache() != customCache {
		t.Errorf("SetGlobalCache should update the global cache")
	}

	// Test DefaultCache alias
	if DefaultCache() != customCache {
		t.Errorf("DefaultCache should return the same instance as GetGlobalCache")
	}

	// Reset to default
	SetGlobalCache(NewNoOpCache())
}

func TestGlobalHelperFunctions(t *testing.T) {
	ctx := context.Background()

	// Test Get (should return nil for NoOpCache)
	entry, err := Get(ctx, "test-key")
	if err != nil {
		t.Errorf("Get returned error: %v", err)
	}
	if entry != nil {
		t.Errorf("Get should return nil for NoOpCache")
	}

	// Test Set (should succeed)
	err = Set(ctx, "test-key", []byte("test-value"))
	if err != nil {
		t.Errorf("Set returned error: %v", err)
	}

	// Test Delete (should succeed)
	err = Delete(ctx, "test-key")
	if err != nil {
		t.Errorf("Delete returned error: %v", err)
	}
}

func TestCacheStatsHitRatio(t *testing.T) {
	tests := []struct {
		name     string
		stats    *CacheStats
		expected float64
	}{
		{
			name: "no requests",
			stats: &CacheStats{
				Hits:    0,
				Misses:  0,
				Backend: "test",
			},
			expected: 0.0,
		},
		{
			name: "all hits",
			stats: &CacheStats{
				Hits:    100,
				Misses:  0,
				Backend: "test",
			},
			expected: 1.0,
		},
		{
			name: "all misses",
			stats: &CacheStats{
				Hits:    0,
				Misses:  100,
				Backend: "test",
			},
			expected: 0.0,
		},
		{
			name: "50% hit ratio",
			stats: &CacheStats{
				Hits:    50,
				Misses:  50,
				Backend: "test",
			},
			expected: 0.5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ratio := tt.stats.HitRatio()
			if ratio != tt.expected {
				t.Errorf("HitRatio() = %f, expected %f", ratio, tt.expected)
			}
		})
	}
}
