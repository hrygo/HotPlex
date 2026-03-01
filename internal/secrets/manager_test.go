package secrets

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestEnvProvider_Get(t *testing.T) {
	// Setup
	key := "TEST_SECRET_KEY"
	value := "test-secret-value"
	_ = os.Setenv(key, value)
	defer func() { _ = os.Unsetenv(key) }()

	ctx := context.Background()
	p := NewEnvProvider()

	// Test get existing secret
	got, err := p.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got != value {
		t.Errorf("Get() = %v, want %v", got, value)
	}

	// Test get non-existing secret
	_, err = p.Get(ctx, "NON_EXISTING_KEY")
	if err == nil {
		t.Error("Get() expected error for non-existing key")
	}
}

func TestEnvProvider_Set(t *testing.T) {
	key := "TEST_SET_KEY"
	value := "test-set-value"
	defer func() { _ = os.Unsetenv(key) }()

	ctx := context.Background()
	p := NewEnvProvider()

	err := p.Set(ctx, key, value)
	if err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	got := os.Getenv(key)
	if got != value {
		t.Errorf("Set() = %v, want %v", got, value)
	}
}

func TestManager_Get(t *testing.T) {
	key := "MANAGER_TEST_KEY"
	value := "manager-test-value"
	_ = os.Setenv(key, value)
	defer func() { _ = os.Unsetenv(key) }()

	ctx := context.Background()
	m := NewManager()
	m.AddProvider(NewEnvProvider())

	// Test get from provider
	got, err := m.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got != value {
		t.Errorf("Get() = %v, want %v", got, value)
	}

	// Test get from cache
	func() { _ = os.Unsetenv(key) }()
	got, err = m.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get() from cache error = %v", err)
	}
	if got != value {
		t.Errorf("Get() from cache = %v, want %v", got, value)
	}
}

func TestManager_CacheTTL(t *testing.T) {
	key := "TTL_TEST_KEY"
	value := "ttl-test-value"
	_ = os.Setenv(key, value)
	defer func() { _ = os.Unsetenv(key) }()

	ctx := context.Background()
	m := NewManager(WithTTL(100 * time.Millisecond))
	m.AddProvider(NewEnvProvider())

	// Get and cache
	_, err := m.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	// Remove from env
	func() { _ = os.Unsetenv(key) }()

	// Should still be in cache
	got, err := m.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get() from cache error = %v", err)
	}
	if got != value {
		t.Errorf("Get() from cache = %v, want %v", got, value)
	}

	// Wait for cache to expire
	time.Sleep(150 * time.Millisecond)

	// Should be cache miss now
	_, err = m.Get(ctx, key)
	if err == nil {
		t.Error("Get() expected error after cache expiry")
	}
}

func TestManager_ClearCache(t *testing.T) {
	key := "CLEAR_TEST_KEY"
	value := "clear-test-value"
	_ = os.Setenv(key, value)
	defer func() { _ = os.Unsetenv(key) }()

	ctx := context.Background()
	m := NewManager()
	m.AddProvider(NewEnvProvider())

	// Get and cache
	_, err := m.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	// Clear cache
	m.ClearCache()

	// Remove from env
	func() { _ = os.Unsetenv(key) }()

	// Should be cache miss
	_, err = m.Get(ctx, key)
	if err == nil {
		t.Error("Get() expected error after cache clear")
	}
}
