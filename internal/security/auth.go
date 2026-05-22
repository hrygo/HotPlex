// Package security provides authentication and input validation middleware.
package security

import (
	"context"
	"errors"
	"net/http"
	"sync"

	"github.com/hrygo/hotplex/internal/config"
)

// apiKeyQueryParam is the query parameter name for browser-based WebSocket clients
// that cannot send custom headers (CORS restrictions).
const apiKeyQueryParam = "api_key"

// botIDHeader is the HTTP header for bot identity in multi-bot setups.
const botIDHeader = "X-Bot-ID"

// botIDQueryParam is the query parameter fallback for browser WebSocket clients.
const botIDQueryParam = "bot_id"

// Authenticator validates API keys and user credentials.
type Authenticator struct {
	mu          sync.RWMutex
	cfg         *config.SecurityConfig
	validKey    map[string]bool // set of valid API keys (hashed in production)
	keyResolver APIKeyResolver  // optional; maps API keys to user identities. nil = "api_user"
}

// NewAuthenticator creates a new authenticator.
func NewAuthenticator(cfg *config.SecurityConfig) *Authenticator {
	validKey := make(map[string]bool)
	for _, k := range cfg.APIKeys {
		validKey[k] = true
	}
	return &Authenticator{
		cfg:      cfg,
		validKey: validKey,
	}
}

// ErrUnauthorized is returned when authentication fails.
var ErrUnauthorized = errors.New("security: unauthorized")

// AuthenticateRequest validates the request's API key.
// Returns the user ID, bot ID (from X-Bot-ID header or bot_id query param), and any error.
func (a *Authenticator) AuthenticateRequest(r *http.Request) (string, string, error) {
	a.mu.RLock()
	header := a.cfg.APIKeyHeader
	if header == "" {
		header = "X-API-Key"
	}

	// Check header first, then query param (for browser WebSocket clients).
	key := r.Header.Get(header)
	if key == "" {
		key = r.URL.Query().Get(apiKeyQueryParam)
	}
	if key == "" {
		a.mu.RUnlock()
		return "", "", ErrUnauthorized
	}

	// Key lookup under RLock; map lookup is not constant-time
	// but acceptable for API keys (small set, low timing sensitivity).
	defer a.mu.RUnlock()

	botID := BotIDFromRequest(r)

	if len(a.validKey) == 0 {
		// No keys configured — allow all (dev mode).
		return "anonymous", botID, nil
	}

	if !a.validKey[key] {
		return "", "", ErrUnauthorized
	}

	return a.resolveUserID(r.Context(), key), botID, nil
}

// ReloadKeys dynamically replaces the set of valid API keys.
func (a *Authenticator) ReloadKeys(cfg *config.SecurityConfig) {
	validKey := make(map[string]bool)
	for _, k := range cfg.APIKeys {
		validKey[k] = true
	}
	a.mu.Lock()
	a.cfg = cfg
	a.validKey = validKey
	a.mu.Unlock()
}

// SetKeyResolver sets the API key → user identity resolver.
// Nil clears the mapping (all keys return "api_user").
func (a *Authenticator) SetKeyResolver(r APIKeyResolver) {
	a.mu.Lock()
	a.keyResolver = r
	a.mu.Unlock()
}

// resolveUserID returns the user identity for a valid API key.
// Checks the resolver first; falls back to "api_user" if no mapping exists.
// Caller must hold at least RLock.
func (a *Authenticator) resolveUserID(ctx context.Context, key string) string {
	if a.keyResolver != nil {
		if uid, ok := a.keyResolver.Resolve(ctx, key); ok {
			return uid
		}
	}
	return "api_user"
}

// ExtractAPIKey returns the API key from header or query param.
// Returns ("", false) if no key found, (key, true) if found (not yet validated).
func (a *Authenticator) ExtractAPIKey(r *http.Request) (string, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	header := a.cfg.APIKeyHeader
	if header == "" {
		header = "X-API-Key"
	}

	key := r.Header.Get(header)
	if key == "" {
		key = r.URL.Query().Get(apiKeyQueryParam)
	}
	if key == "" {
		return "", false
	}
	return key, true
}

// AuthenticateKey validates an API key string directly.
// Returns userID if valid, ("", false) if invalid.
// Handles dev mode (no keys configured → "anonymous").
func (a *Authenticator) AuthenticateKey(ctx context.Context, key string) (string, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if len(a.validKey) == 0 {
		// No keys configured — allow all (dev mode).
		return "anonymous", true
	}

	if !a.validKey[key] {
		return "", false
	}
	return a.resolveUserID(ctx, key), true
}

// BotIDFromRequest extracts the bot ID from X-Bot-ID header or bot_id query param.
// Returns "" if not provided (no bot isolation).
func BotIDFromRequest(r *http.Request) string {
	if v := r.Header.Get(botIDHeader); v != "" {
		return v
	}
	return r.URL.Query().Get(botIDQueryParam)
}

// Middleware returns an HTTP middleware that enforces authentication.
func (a *Authenticator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _, err := a.AuthenticateRequest(r)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Claims holds authenticated user information attached to a context.
type Claims struct {
	UserID string
	APIKey string
}

type contextKey string

const claimsKey contextKey = "security.claims"

// WithClaims attaches Claims to a context.
func WithClaims(ctx context.Context, claims Claims) context.Context {
	return context.WithValue(ctx, claimsKey, claims)
}

// ClaimsFrom extracts Claims from a context.
func ClaimsFrom(ctx context.Context) (Claims, bool) {
	c, ok := ctx.Value(claimsKey).(Claims)
	return c, ok
}
