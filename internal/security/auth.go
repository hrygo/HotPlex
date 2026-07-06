// Package security provides authentication and input validation middleware.
package security

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/hrygo/hotplex/internal/audit"
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
//
// API keys are stored as SHA256 hashes in memory — the raw key is never kept
// after hashing. This provides O(1) lookup with a single hash computation per
// request and eliminates timing side-channels (map lookup is constant-time
// regardless of key count).
type Authenticator struct {
	mu             sync.RWMutex
	cfg            *config.SecurityConfig
	validKeyHash   map[[32]byte]bool // SHA256 hashes of config-sourced keys
	dbKeyHash      map[[32]byte]bool // SHA256 hashes of database-sourced keys
	numValidKeys   int               // count of config keys (for dev-mode check)
	numDBKeys      int               // count of DB keys (for dev-mode check)
	keyResolver    APIKeyResolver    // optional; maps API keys to user identities. nil = "api_user"
	devModeLocked  bool              // true once any key has existed; prevents dev mode re-enable
	cookieAuth     *CookieAuth       // optional; HMAC cookie auth (3rd priority after header/query)
	idp            IdentityProvider  // optional; account-login provider (LocalAccountProvider / future OAuth)
	auditCollector *audit.Collector  // optional; non-blocking audit event sink (spec §5)
}

// NewAuthenticator creates a new authenticator.
func NewAuthenticator(cfg *config.SecurityConfig) *Authenticator {
	hashes := make(map[[32]byte]bool, len(cfg.APIKeys))
	for _, k := range cfg.APIKeys {
		hashes[sha256.Sum256([]byte(k))] = true
	}
	return &Authenticator{
		cfg:           cfg,
		validKeyHash:  hashes,
		dbKeyHash:     make(map[[32]byte]bool),
		numValidKeys:  len(hashes),
		devModeLocked: len(hashes) > 0,
	}
}

// SetCookieAuth enables cookie-based authentication as a 3rd priority fallback
// after header and query param. Typically called when webchat is enabled.
func (a *Authenticator) SetCookieAuth(ca *CookieAuth) {
	a.mu.Lock()
	a.cookieAuth = ca
	a.mu.Unlock()
}

// SetIdentityProvider wires the account-login identity provider (LocalAccountProvider
// now, OAuthProvider later). Optional: nil disables account-login + Lookup.
func (a *Authenticator) SetIdentityProvider(idp IdentityProvider) {
	a.mu.Lock()
	a.idp = idp
	a.mu.Unlock()
}

// IdentityProvider returns the wired provider (may be nil). Used by handlers
// for user Lookup (e.g. /api/auth/me) and admin role checks.
func (a *Authenticator) IdentityProvider() IdentityProvider {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.idp
}

// SetAuditCollector wires the audit collector for non-blocking auth event
// emission (spec §5.2). Nil disables audit events. Safe to call before or
// after authentication begins; the field is read without lock because the
// collector pointer is immutable after startup wiring.
func (a *Authenticator) SetAuditCollector(c *audit.Collector) {
	a.auditCollector = c
}

// emitAuthEvent enqueues a non-blocking audit event for an authentication
// outcome. No-op when auditCollector is nil. Errors are silently ignored to
// keep the auth path fast and non-blocking.
//
// platform and userIDType are passed by the caller because the attribution
// depends on which auth path succeeded (review I2): cookie/webchat →
// PlatformWebChat + UserIDTypeRegistered; API-key → PlatformAPI + (registered
// if a resolver mapped the key, else platform); anonymous/denied → anonymous.
// Hardcoding PlatformAPI for every path mis-tagged webchat UUIDs as opaque
// platform handles, corrupting the by-user-type analytics the audit table
// exists to support (spec §5.4).
func (a *Authenticator) emitAuthEvent(action, outcome, userID, platform, userIDType, ip, userAgent, path, method string) {
	c := a.auditCollector
	if c == nil {
		return
	}
	if userID == "" {
		userID = audit.AnonymousUserID
	}
	if userIDType == "" {
		userIDType = audit.UserIDTypeAnonymous
	}
	detail, _ := json.Marshal(map[string]string{
		"path":   path,
		"method": method,
	})
	ua := &audit.UserActivity{
		Ts:         time.Now().UnixMilli(),
		UserID:     userID,
		UserIDType: userIDType,
		Platform:   platform,
		Action:     action,
		Outcome:    outcome,
		DetailJSON: string(detail),
		IP:         ip,
		UserAgent:  userAgent,
	}
	_ = c.Enqueue(context.Background(), ua)
}

func extractClientIP(r *http.Request) string {
	ip := r.RemoteAddr
	if idx := strings.LastIndex(ip, ":"); idx > 0 {
		ip = ip[:idx]
	}
	return ip
}

// ErrUnauthorized is returned when authentication fails.
var ErrUnauthorized = errors.New("security: unauthorized")

// AuthenticateRequest validates the request's API key.
// Returns the user ID, bot ID (from X-Bot-ID header or bot_id query param), and any error.
func (a *Authenticator) AuthenticateRequest(r *http.Request) (string, string, error) {
	ip := extractClientIP(r)
	ua := r.UserAgent()
	path := r.URL.Path
	method := r.Method

	a.mu.RLock()

	key, found := a.extractAPIKey(r)
	if !found {
		// 3rd priority: cookie auth fallback (webchat same-origin).
		// Disabled-user enforcement is shared via AuthenticateActiveCookie with
		// the WS upgrade and workspace REST paths.
		a.mu.RUnlock()
		if a.cookieAuth != nil {
			if uid, ok := a.AuthenticateActiveCookie(r); ok {
				botID := BotIDFromRequest(r)
				// Cookie auth is the webchat path; uid is a registered users.id
				// UUID → spec §5.4 requires PlatformWebChat + registered.
				a.emitAuthEvent(audit.ActionAuthLogin, audit.OutcomeSuccess, uid,
					audit.PlatformWebChat, audit.UserIDTypeRegistered, ip, ua, path, method)
				return uid, botID, nil
			}
		}
		a.emitAuthEvent(audit.ActionAuthDenied, audit.OutcomeDenied, audit.AnonymousUserID,
			audit.PlatformAPI, audit.UserIDTypeAnonymous, ip, ua, path, method)
		return "", "", ErrUnauthorized
	}

	// Dev mode: no keys configured — allow all.
	// devModeLocked prevents re-enable after keys have existed (security: auth bypass window).
	if a.numValidKeys == 0 && a.numDBKeys == 0 && !a.devModeLocked {
		a.mu.RUnlock()
		botID := BotIDFromRequest(r)
		a.emitAuthEvent(audit.ActionAuthLogin, audit.OutcomeSuccess, audit.AnonymousUserID,
			audit.PlatformAPI, audit.UserIDTypeAnonymous, ip, ua, path, method)
		return "anonymous", botID, nil
	}

	// Key lookup using constant-time comparison to prevent timing attacks.
	if !a.authenticateKey(key) {
		a.mu.RUnlock()
		a.emitAuthEvent(audit.ActionAuthAPIKeyUsed, audit.OutcomeFailure, audit.AnonymousUserID,
			audit.PlatformAPI, audit.UserIDTypeAnonymous, ip, ua, path, method)
		return "", "", ErrUnauthorized
	}

	// Snapshot resolver under lock, then release before calling external resolver.
	resolver := a.keyResolver
	idp := a.idp
	a.mu.RUnlock()

	uid := resolveUserIDWith(r.Context(), key, resolver)
	if idp != nil && uid != "anonymous" && uid != "api_user" {
		u, err := idp.Lookup(r.Context(), uid)
		if err != nil || u.Status == "disabled" {
			a.emitAuthEvent(audit.ActionAuthTokenValidated, audit.OutcomeFailure, audit.AnonymousUserID,
				audit.PlatformAPI, audit.UserIDTypeAnonymous, ip, ua, path, method)
			return "", "", ErrUnauthorized
		}
	}

	botID := BotIDFromRequest(r)
	// API-key path: if a resolver mapped the key to a real user id, the uid is
	// a registered users.id UUID → registered; otherwise it's the opaque
	// "api_user" handle → platform (spec §5.4 attribution table).
	successIDType := audit.UserIDTypePlatform
	if uid != "api_user" && uid != "anonymous" {
		successIDType = audit.UserIDTypeRegistered
	}
	a.emitAuthEvent(audit.ActionAuthAPIKeyUsed, audit.OutcomeSuccess, uid,
		audit.PlatformAPI, successIDType, ip, ua, path, method)
	return uid, botID, nil
}

// AuthenticateActiveCookie authenticates a cookie-bearing request and rejects
// disabled users. It mirrors the disabled-user enforcement of the API-key path
// (AuthenticateKey / AuthenticateRequest) so that a valid cookie alone can no
// longer grant access to a user disabled by an admin, for the cookie's full 7d
// lifetime.
//
// Returns (userID, true) only when the CookieAuth is configured, the request
// carries a valid HMAC cookie, and the user is not disabled. The idp lookup is
// skipped in dev mode (nil idp) and for system identities ("anonymous",
// "api_user"), matching AuthenticateRequest semantics. ("", false) is returned
// when there is no cookie, the cookie is invalid, the user is disabled, or the
// lookup fails.
//
// Hub.HandleHTTP (WS upgrade) and WorkspaceHandlers.requireAuth use this so the
// cookie-auth paths share one disabled-user enforcement definition with the
// REST API.
func (a *Authenticator) AuthenticateActiveCookie(r *http.Request) (string, bool) {
	// Snapshot cookieAuth/idp under RLock: SetCookieAuth/SetIdentityProvider
	// mutate them under the write lock, so an unlocked field read would race if
	// a hot reload ever reconfigures them at runtime (caught by -race). Snapshot
	// the pointers, release the lock, then do IO (Authenticate/Lookup).
	a.mu.RLock()
	cookieAuth := a.cookieAuth
	idp := a.idp
	a.mu.RUnlock()
	if cookieAuth == nil {
		return "", false
	}
	uid, ok := cookieAuth.Authenticate(r)
	if !ok {
		return "", false
	}
	if idp != nil && uid != "anonymous" && uid != "api_user" {
		u, err := idp.Lookup(r.Context(), uid)
		if err != nil || u.Status == "disabled" {
			return "", false
		}
	}
	return uid, true
}

// ReloadKeys dynamically replaces the set of valid API keys.
func (a *Authenticator) ReloadKeys(cfg *config.SecurityConfig) {
	hashes := make(map[[32]byte]bool, len(cfg.APIKeys))
	for _, k := range cfg.APIKeys {
		hashes[sha256.Sum256([]byte(k))] = true
	}
	a.mu.Lock()
	a.cfg = cfg
	a.validKeyHash = hashes
	a.numValidKeys = len(hashes)
	if len(hashes) > 0 {
		a.devModeLocked = true
	}
	a.mu.Unlock()
}

// SetKeyResolver sets the API key → user identity resolver.
// Nil clears the mapping (all keys return "api_user").
func (a *Authenticator) SetKeyResolver(r APIKeyResolver) {
	a.mu.Lock()
	a.keyResolver = r
	a.mu.Unlock()
}

// authenticateKey checks the incoming key against stored SHA256 hashes.
// O(1) lookup: hash the input once, then check both hash maps.
// Caller must hold at least RLock.
func (a *Authenticator) authenticateKey(key string) bool {
	h := sha256.Sum256([]byte(key))
	if a.validKeyHash[h] {
		return true
	}
	return a.dbKeyHash[h]
}

// AddKey adds a database-sourced API key to the valid key set.
// Called by Admin API after creating a new key in the database.
func (a *Authenticator) AddKey(key string) {
	h := sha256.Sum256([]byte(key))
	a.mu.Lock()
	a.dbKeyHash[h] = true
	a.numDBKeys = len(a.dbKeyHash)
	a.devModeLocked = true
	a.mu.Unlock()
}

// RemoveKey removes a database-sourced API key from the valid key set.
// Called by Admin API after deleting a key from the database.
func (a *Authenticator) RemoveKey(key string) {
	h := sha256.Sum256([]byte(key))
	a.mu.Lock()
	delete(a.dbKeyHash, h)
	a.numDBKeys = len(a.dbKeyHash)
	empty := a.numValidKeys == 0 && a.numDBKeys == 0 && a.devModeLocked
	a.mu.Unlock()
	if empty {
		slog.Warn("security: all API keys removed but dev mode is locked — restart gateway to restore anonymous access",
			"dev_mode_locked", true)
	}
}

// resolveUserIDWith resolves user identity without holding any lock.
func resolveUserIDWith(ctx context.Context, key string, resolver APIKeyResolver) string {
	if resolver != nil {
		if uid, ok := resolver.Resolve(ctx, key); ok {
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
	return a.extractAPIKey(r)
}

// extractAPIKey reads the API key from the configured header or query param.
// Caller must hold at least RLock.
func (a *Authenticator) extractAPIKey(r *http.Request) (string, bool) {
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
	if a.numValidKeys == 0 && a.numDBKeys == 0 && !a.devModeLocked {
		// No keys configured — allow all (dev mode).
		a.mu.RUnlock()
		return "anonymous", true
	}

	if !a.authenticateKey(key) {
		a.mu.RUnlock()
		return "", false
	}
	resolver := a.keyResolver
	idp := a.idp
	a.mu.RUnlock()

	uid := resolveUserIDWith(ctx, key, resolver)
	if idp != nil && uid != "anonymous" && uid != "api_user" {
		u, err := idp.Lookup(ctx, uid)
		if err != nil || u.Status == "disabled" {
			return "", false
		}
	}
	return uid, true
}

// BotIDFromRequest extracts the bot ID from X-Bot-ID header or bot_id query param.
// Returns "" if not provided (no bot isolation).
//
// Trust boundary: Bot ID is NOT cryptographically bound to the API key.
// Any authenticated client can specify any bot ID. This is acceptable because:
// 1. Bot ID determines routing behavior (which bot configuration to use), not authorization.
// 2. API key authentication already gates access at the connection level.
// 3. Cross-bot data isolation is enforced downstream by session key derivation.
// If API-key-to-bot-ID binding is required, implement a KeyBotBinding resolver.
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
