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
//
// Security: negative results (sql.ErrNoRows) are cached with a shorter TTL
// to prevent cache-penetration DoS attacks where an adversary floods the
// system with invalid keys to bypass the cache and hammer the database.
//
// Memory safety: a background goroutine periodically sweeps expired entries
// to prevent unbounded memory growth when keys are rotated.
type DBResolver struct {
	db      *sql.DB
	dialect dbutil.Dialect
	cache   sync.Map // key → *cacheEntry
	done    chan struct{}
}

// cacheEntry holds a cached API key → userID resolution result.
// negative entries (key doesn't exist in DB) have isNegative=true.
type cacheEntry struct {
	userID     string
	expiresAt  time.Time
	isNegative bool // true = key was not found in DB
}

const (
	// cacheTTL is the TTL for positive cache entries (key exists).
	cacheTTL = 60 * time.Second

	// negativeCacheTTL is the TTL for negative cache entries (key not found).
	// Short to balance DoS protection with timely recognition of new keys.
	negativeCacheTTL = 5 * time.Second

	// cacheCleanupInterval is the interval between cache sweep passes.
	cacheCleanupInterval = 2 * time.Minute
)

// NewDBResolver creates a resolver backed by the api_key_users table.
// The table must exist (created by migration 010).
// Caller must call Close() on shutdown to stop the background cleanup goroutine.
func NewDBResolver(db *sql.DB, dialect dbutil.Dialect) *DBResolver {
	r := &DBResolver{db: db, dialect: dialect, done: make(chan struct{})}
	go r.cleanupLoop()
	return r
}

// Close stops the background cleanup goroutine. Safe to call multiple times.
func (r *DBResolver) Close() {
	select {
	case <-r.done:
		// already closed
	default:
		close(r.done)
	}
}

// cleanupLoop periodically sweeps expired cache entries to prevent memory leaks
// from key rotation (expired entries that are never re-queried would otherwise
// linger forever in sync.Map).
func (r *DBResolver) cleanupLoop() {
	ticker := time.NewTicker(cacheCleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-r.done:
			return
		case <-ticker.C:
			now := time.Now()
			r.cache.Range(func(k, v any) bool {
				if e, ok := v.(*cacheEntry); ok && now.After(e.expiresAt) {
					r.cache.Delete(k)
				}
				return true
			})
		}
	}
}

// Invalidate removes a cached entry. Called by Admin API after CUD operations.
func (r *DBResolver) Invalidate(key string) {
	r.cache.Delete(key)
}

// InvalidateAll clears all cached entries. Called after migrations that change
// user_id values (e.g. migration 018 remaps api_key_users.user_id to users.id).
// The 60s TTL would otherwise serve stale IDs after such migrations.
func (r *DBResolver) InvalidateAll() {
	r.cache.Range(func(k, _ any) bool {
		r.cache.Delete(k)
		return true
	})
}

func (r *DBResolver) Resolve(ctx context.Context, key string) (string, bool) {
	// Check cache first.
	if v, ok := r.cache.Load(key); ok {
		e, ok := v.(*cacheEntry)
		if !ok {
			r.cache.Delete(key)
		} else if time.Now().Before(e.expiresAt) {
			if e.isNegative {
				return "", false
			}
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
		if errors.Is(err, sql.ErrNoRows) {
			// Cache negative result to prevent cache-penetration DoS.
			r.cache.Store(key, &cacheEntry{
				expiresAt:  time.Now().Add(negativeCacheTTL),
				isNegative: true,
			})
		} else {
			slog.Warn("security: DBResolver query failed", "err", err)
		}
		return "", false
	}
	// Cache positive result.
	r.cache.Store(key, &cacheEntry{
		userID:    userID,
		expiresAt: time.Now().Add(cacheTTL),
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
