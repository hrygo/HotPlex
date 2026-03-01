package secrets

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Manager manages multiple secret providers with caching
type Manager struct {
	providers []Provider
	cache     map[string]cachedSecret
	mu        sync.RWMutex
	ttl       time.Duration
}

type cachedSecret struct {
	value     string
	expiresAt time.Time
}

// ManagerOption configures the Manager
type ManagerOption func(*Manager)

// WithTTL sets the cache TTL
func WithTTL(ttl time.Duration) ManagerOption {
	return func(m *Manager) {
		m.ttl = ttl
	}
}

// NewManager creates a new secret manager
func NewManager(opts ...ManagerOption) *Manager {
	m := &Manager{
		providers: make([]Provider, 0),
		cache:     make(map[string]cachedSecret),
		ttl:       5 * time.Minute,
	}

	for _, opt := range opts {
		opt(m)
	}

	return m
}

// AddProvider adds a secret provider (priority order)
func (m *Manager) AddProvider(p Provider) {
	m.providers = append(m.providers, p)
}

// Get retrieves a secret from cache or providers
func (m *Manager) Get(ctx context.Context, key string) (string, error) {
	// Check cache first
	m.mu.RLock()
	if cached, ok := m.cache[key]; ok && time.Now().Before(cached.expiresAt) {
		m.mu.RUnlock()
		return cached.value, nil
	}
	m.mu.RUnlock()

	// Query providers in order
	for _, p := range m.providers {
		value, err := p.Get(ctx, key)
		if err == nil {
			// Cache the result
			m.mu.Lock()
			m.cache[key] = cachedSecret{
				value:     value,
				expiresAt: time.Now().Add(m.ttl),
			}
			m.mu.Unlock()
			return value, nil
		}
	}

	return "", errors.New("secret not found: " + key)
}

// Set stores a secret in all providers
func (m *Manager) Set(ctx context.Context, key, value string) error {
	var lastErr error
	for _, p := range m.providers {
		if err := p.Set(ctx, key, value); err != nil {
			lastErr = err
		}
	}

	// Update cache
	m.mu.Lock()
	m.cache[key] = cachedSecret{
		value:     value,
		expiresAt: time.Now().Add(m.ttl),
	}
	m.mu.Unlock()

	return lastErr
}

// Delete removes a secret from all providers
func (m *Manager) Delete(ctx context.Context, key string) error {
	// Remove from cache
	m.mu.Lock()
	delete(m.cache, key)
	m.mu.Unlock()

	// Delete from providers
	for _, p := range m.providers {
		_ = p.Delete(ctx, key)
	}

	return nil
}

// ClearCache clears the secret cache
func (m *Manager) ClearCache() {
	m.mu.Lock()
	m.cache = make(map[string]cachedSecret)
	m.mu.Unlock()
}
