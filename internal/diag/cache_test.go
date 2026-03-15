package diag

import (
	"testing"
	"time"
)

func TestDiagnosisCache_NewDiagnosisCache(t *testing.T) {
	tests := []struct {
		name    string
		ttl     time.Duration
		maxSize int
		wantTTL time.Duration
		wantMax int
	}{
		{"defaults", 0, 0, 30 * time.Minute, 100},
		{"custom", 10 * time.Minute, 50, 10 * time.Minute, 50},
		{"negative defaults to -1", -1, -1, 30 * time.Minute, 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := NewDiagnosisCache(tt.ttl, tt.maxSize)
			if cache.ttl != tt.wantTTL {
				t.Errorf("ttl = %v, want %v", cache.ttl, tt.wantTTL)
			}
			if cache.maxSize != tt.wantMax {
				t.Errorf("maxSize = %v, want %v", cache.maxSize, tt.wantMax)
			}
		})
	}
}

func TestDiagnosisCache_SetGet(t *testing.T) {
	cache := NewDiagnosisCache(5*time.Minute, 10)

	// Test miss
	result, ok := cache.Get("nonexistent")
	if ok {
		t.Error("expected miss for nonexistent key")
	}
	if result != nil {
		t.Error("expected nil result for miss")
	}

	// Test set and get
	testResult := &DiagResult{ID: "diag-1", Status: StatusPending}
	cache.Set("hash1", testResult)

	result, ok = cache.Get("hash1")
	if !ok {
		t.Error("expected hit for existing key")
	}
	if result.ID != "diag-1" {
		t.Errorf("got ID %q, want %q", result.ID, "diag-1")
	}

	// Verify hit count
	stats := cache.Stats()
	if stats.HitCount != 1 {
		t.Errorf("HitCount = %d, want 1", stats.HitCount)
	}
}

func TestDiagnosisCache_TTLExpiration(t *testing.T) {
	cache := NewDiagnosisCache(50*time.Millisecond, 10)

	testResult := &DiagResult{ID: "diag-expiring"}
	cache.Set("hash1", testResult)

	// Should exist immediately
	_, ok := cache.Get("hash1")
	if !ok {
		t.Error("expected hit before TTL expiration")
	}

	// Wait for TTL
	time.Sleep(60 * time.Millisecond)

	// Should be expired now
	_, ok = cache.Get("hash1")
	if ok {
		t.Error("expected miss after TTL expiration")
	}
}

func TestDiagnosisCache_Eviction(t *testing.T) {
	cache := NewDiagnosisCache(5*time.Minute, 3)

	// Add 3 entries (at capacity)
	for i := 0; i < 3; i++ {
		cache.Set(string(rune('a'+i)), &DiagResult{ID: string(rune('a' + i))})
		time.Sleep(time.Millisecond) // Ensure different timestamps
	}

	stats := cache.Stats()
	if stats.Size != 3 {
		t.Errorf("Size = %d, want 3", stats.Size)
	}

	// Add one more - should trigger eviction
	cache.Set("d", &DiagResult{ID: "d"})

	stats = cache.Stats()
	if stats.Size != 3 {
		t.Errorf("Size after eviction = %d, want 3", stats.Size)
	}

	// First entry should have been evicted
	_, ok := cache.Get("a")
	if ok {
		t.Error("expected oldest entry to be evicted")
	}
}

func TestDiagnosisCache_Clear(t *testing.T) {
	cache := NewDiagnosisCache(5*time.Minute, 10)

	cache.Set("hash1", &DiagResult{ID: "diag1"})
	cache.Set("hash2", &DiagResult{ID: "diag2"})

	stats := cache.Stats()
	if stats.Size != 2 {
		t.Errorf("Size before clear = %d, want 2", stats.Size)
	}

	cache.Clear()

	stats = cache.Stats()
	if stats.Size != 0 {
		t.Errorf("Size after clear = %d, want 0", stats.Size)
	}
	if stats.HitCount != 0 || stats.MissCount != 0 {
		t.Error("expected counters to be reset after clear")
	}
}

func TestDiagnosisCache_Cleanup(t *testing.T) {
	cache := NewDiagnosisCache(50*time.Millisecond, 10)

	// Add entries with slight delay
	cache.Set("hash1", &DiagResult{ID: "old"})
	time.Sleep(30 * time.Millisecond)
	cache.Set("hash2", &DiagResult{ID: "newer"})

	// Wait for first to expire but not second
	time.Sleep(30 * time.Millisecond)

	cleaned := cache.Cleanup()
	if cleaned != 1 {
		t.Errorf("Cleanup returned %d, want 1", cleaned)
	}

	stats := cache.Stats()
	if stats.Size != 1 {
		t.Errorf("Size after cleanup = %d, want 1", stats.Size)
	}
}

func TestComputeHash(t *testing.T) {
	ctx1 := &DiagContext{
		OriginalSessionID: "session-1",
		Platform:          "slack",
		Trigger:           TriggerAuto,
		Error: &ErrorInfo{
			Type:    ErrorTypeExit,
			Message: "process exited with code 1",
		},
	}

	ctx2 := &DiagContext{
		OriginalSessionID: "session-1",
		Platform:          "slack",
		Trigger:           TriggerAuto,
		Error: &ErrorInfo{
			Type:    ErrorTypeExit,
			Message: "process exited with code 1",
		},
	}

	ctx3 := &DiagContext{
		OriginalSessionID: "session-2", // Different
		Platform:          "slack",
		Trigger:           TriggerAuto,
		Error: &ErrorInfo{
			Type:    ErrorTypeExit,
			Message: "process exited with code 1",
		},
	}

	hash1 := ComputeHash(ctx1)
	hash2 := ComputeHash(ctx2)
	hash3 := ComputeHash(ctx3)

	// Same context should produce same hash
	if hash1 != hash2 {
		t.Error("expected same hash for identical contexts")
	}

	// Different context should produce different hash
	if hash1 == hash3 {
		t.Error("expected different hashes for different contexts")
	}

	// Hash should be valid hex
	if len(hash1) != 64 { // SHA256 = 32 bytes = 64 hex chars
		t.Errorf("hash length = %d, want 64", len(hash1))
	}
}

func TestDiagnosisCache_CheckDuplicate(t *testing.T) {
	cache := NewDiagnosisCache(5*time.Minute, 10)

	ctx := &DiagContext{
		OriginalSessionID: "session-1",
		Platform:          "slack",
		Trigger:           TriggerAuto,
		Error:             &ErrorInfo{Type: ErrorTypeExit, Message: "test error"},
	}

	// First check - not duplicate
	result := cache.CheckDuplicate(ctx)
	if result.IsDuplicate {
		t.Error("expected first check to not be duplicate")
	}

	// Store the diagnosis
	cache.Store(ctx, &DiagResult{ID: "cached-diag"})

	// Second check - should be duplicate
	result = cache.CheckDuplicate(ctx)
	if !result.IsDuplicate {
		t.Error("expected second check to be duplicate")
	}
	if result.Existing == nil {
		t.Error("expected existing result for duplicate")
	}
	if result.Existing.ID != "cached-diag" {
		t.Errorf("existing ID = %q, want %q", result.Existing.ID, "cached-diag")
	}
}

func TestCacheStats_HitRate(t *testing.T) {
	cache := NewDiagnosisCache(5*time.Minute, 10)

	// No operations - 0% hit rate
	stats := cache.Stats()
	if stats.HitRate != 0 {
		t.Errorf("empty cache HitRate = %v, want 0", stats.HitRate)
	}

	// Add and get (1 hit)
	cache.Set("hash1", &DiagResult{})
	cache.Get("hash1")

	// Miss (1 miss)
	cache.Get("nonexistent")

	stats = cache.Stats()
	expectedRate := 0.5 // 1 hit / 2 total
	if stats.HitRate != expectedRate {
		t.Errorf("HitRate = %v, want %v", stats.HitRate, expectedRate)
	}
}
