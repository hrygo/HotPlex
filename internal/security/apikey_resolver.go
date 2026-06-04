package security

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/hrygo/hotplex/internal/dbutil"
)

// APIKeyResolver maps an API key to a user identity.
// Implementations: MapResolver (YAML config), DBResolver (SQLite).
// Nil resolver means all valid keys resolve to "api_user" (default behavior).
type APIKeyResolver interface {
	// Resolve returns the userID associated with the given API key.
	// Returns ("", false) if the key has no explicit mapping —
	// caller falls back to "api_user".
	Resolve(ctx context.Context, key string) (userID string, ok bool)
}

// MapResolver resolves API keys via an in-memory map.
// Thread-safe: Update swaps the map atomically under write lock.
// This is the default implementation driven by YAML config.
type MapResolver struct {
	mu   sync.RWMutex
	data map[string]string // apiKey → userID
}

// NewMapResolver creates a resolver from a static key→user map.
// Nil or empty data is safe — Resolve always returns ("", false).
func NewMapResolver(data map[string]string) *MapResolver {
	if data == nil {
		data = make(map[string]string)
	}
	return &MapResolver{data: data}
}

func (r *MapResolver) Resolve(_ context.Context, key string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	uid, ok := r.data[key]
	return uid, ok
}

// Update replaces the mapping atomically. Called during config hot-reload.
func (r *MapResolver) Update(data map[string]string) {
	if data == nil {
		data = make(map[string]string)
	}
	r.mu.Lock()
	r.data = data
	r.mu.Unlock()
}

// DBResolver resolves API keys from the api_key_users table.
// Uses an in-memory cache with TTL to avoid repeated DB queries on hot keys.
// Supports both SQLite and PostgreSQL via dialect-aware query rebinding.
type DBResolver struct {
	db      *sql.DB
	dialect dbutil.Dialect
	cache   sync.Map // key → *cacheEntry
}

type cacheEntry struct {
	userID    string
	expiresAt time.Time
}

// NewDBResolver creates a resolver backed by the api_key_users table.
// The table must exist (created by migration 010).
func NewDBResolver(db *sql.DB, dialect dbutil.Dialect) *DBResolver {
	return &DBResolver{db: db, dialect: dialect}
}

// Invalidate removes a cached entry. Called by Admin API after CUD operations.
func (r *DBResolver) Invalidate(key string) {
	r.cache.Delete(key)
}

func (r *DBResolver) Resolve(ctx context.Context, key string) (string, bool) {
	// Check cache first.
	if v, ok := r.cache.Load(key); ok {
		e, ok := v.(*cacheEntry)
		if !ok {
			r.cache.Delete(key)
		} else if time.Now().Before(e.expiresAt) {
			return e.userID, true
		} else {
			r.cache.Delete(key)
		}
	}

	var userID string
	err := r.db.QueryRowContext(ctx,
		r.dialect.Rebind("SELECT user_id FROM api_key_users WHERE api_key = ?"),
		key,
	).Scan(&userID)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			slog.Warn("security: DBResolver query failed", "error", err)
		}
		return "", false
	}
	// Cache for 60 seconds — balances freshness with DB load.
	r.cache.Store(key, &cacheEntry{
		userID:    userID,
		expiresAt: time.Now().Add(60 * time.Second),
	})
	return userID, true
}

// ChainResolver tries multiple resolvers in order, returning the first match.
// This allows config entries to take priority over DB entries.
type ChainResolver struct {
	resolvers []APIKeyResolver
}

// NewChainResolver creates a resolver that tries each resolver in order.
func NewChainResolver(resolvers ...APIKeyResolver) *ChainResolver {
	filtered := make([]APIKeyResolver, 0, len(resolvers))
	for _, r := range resolvers {
		if r != nil {
			filtered = append(filtered, r)
		}
	}
	return &ChainResolver{resolvers: filtered}
}

func (r *ChainResolver) Resolve(ctx context.Context, key string) (string, bool) {
	for _, res := range r.resolvers {
		if uid, ok := res.Resolve(ctx, key); ok {
			return uid, true
		}
	}
	return "", false
}
