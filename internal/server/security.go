package server

import (
	"crypto/subtle"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
)

// SecurityConfig holds the allowed origins and API key for WebSocket security validation.
type SecurityConfig struct {
	allowedOrigins map[string]bool
	apiKeys        map[string]bool // Support multiple API keys
	apiKeyEnabled  bool
	mu             sync.RWMutex
	logger         *slog.Logger
}

// Environment variable names
const (
	EnvAllowedOrigins = "HOTPLEX_ALLOWED_ORIGINS"
	EnvAPIKey         = "HOTPLEX_API_KEY"  // Single API key (for simplicity)
	EnvAPIKeys        = "HOTPLEX_API_KEYS" // Multiple API keys (comma-separated)
)

// DefaultAllowedOrigins are used when no environment variable is set.
var DefaultAllowedOrigins = []string{
	"http://localhost:3000",
	"http://localhost:8080",
	"http://127.0.0.1:3000",
	"http://127.0.0.1:8080",
}

// NewSecurityConfig creates a new SecurityConfig from environment variables.
// - HOTPLEX_ALLOWED_ORIGINS: Comma-separated list of allowed origins (defaults to localhost)
// - HOTPLEX_API_KEY: Single API key for authentication
// - HOTPLEX_API_KEYS: Multiple API keys (comma-separated, takes precedence over HOTPLEX_API_KEY)
func NewSecurityConfig(logger *slog.Logger) *SecurityConfig {
	c := &SecurityConfig{
		allowedOrigins: make(map[string]bool),
		apiKeys:        make(map[string]bool),
		logger:         logger,
	}

	// Load allowed origins
	origins := parseOriginsFromEnv()
	if len(origins) == 0 {
		origins = DefaultAllowedOrigins
		logger.Warn("HOTPLEX_ALLOWED_ORIGINS not set, using defaults", "origins", origins)
	} else {
		logger.Info("Loaded allowed origins from environment", "count", len(origins))
	}
	for _, origin := range origins {
		c.allowedOrigins[strings.TrimSpace(origin)] = true
	}

	// Load API keys (optional)
	apiKeys := parseAPIKeysFromEnv()
	if len(apiKeys) > 0 {
		c.apiKeyEnabled = true
		for _, key := range apiKeys {
			c.apiKeys[key] = true
		}
		logger.Info("API key authentication enabled", "key_count", len(apiKeys))
	} else {
		// SECURITY WARNING: No API keys configured - authentication is disabled
		if isProductionMode() {
			logger.Error("SECURITY ALERT: Running in production mode without API key authentication! " +
				"Set HOTPLEX_API_KEY or HOTPLEX_API_KEYS environment variable immediately.")
		} else {
			logger.Warn("API key authentication disabled (no keys configured). " +
				"This is OK for development but set HOTPLEX_API_KEY for production.")
		}
	}

	return c
}

// isProductionMode detects if running in a production environment.
func isProductionMode() bool {
	prodIndicators := []string{
		"PRODUCTION", "PROD", "KUBERNETES_SERVICE_HOST", "K_SERVICE",
		"AWS_LAMBDA_FUNCTION_NAME", "VERCEL", "RAILWAY_ENVIRONMENT", "RENDER", "FLY_APP_NAME",
	}
	for _, env := range prodIndicators {
		if os.Getenv(env) != "" {
			return true
		}
	}
	return false
}

// parseOriginsFromEnv reads and parses the HOTPLEX_ALLOWED_ORIGINS environment variable.
// Expects comma-separated list of origins.
func parseOriginsFromEnv() []string {
	env := os.Getenv(EnvAllowedOrigins)
	if env == "" {
		return nil
	}

	origins := strings.Split(env, ",")
	// Filter empty strings
	result := make([]string, 0, len(origins))
	for _, o := range origins {
		o = strings.TrimSpace(o)
		if o != "" {
			result = append(result, o)
		}
	}
	return result
}

// parseAPIKeysFromEnv reads API keys from environment variables.
// HOTPLEX_API_KEYS takes precedence over HOTPLEX_API_KEY.
func parseAPIKeysFromEnv() []string {
	// Try multiple keys first
	if env := os.Getenv(EnvAPIKeys); env != "" {
		keys := strings.Split(env, ",")
		result := make([]string, 0, len(keys))
		for _, k := range keys {
			k = strings.TrimSpace(k)
			if k != "" {
				result = append(result, k)
			}
		}
		return result
	}

	// Fall back to single key
	if key := os.Getenv(EnvAPIKey); key != "" {
		return []string{strings.TrimSpace(key)}
	}

	return nil
}

// CheckOrigin returns a function suitable for websocket.Upgrader.CheckOrigin.
// It validates both Origin header and API key (if enabled).
func (c *SecurityConfig) CheckOrigin() func(r *http.Request) bool {
	return func(r *http.Request) bool {
		// Step 1: Validate API Key if enabled
		if c.apiKeyEnabled && !c.validateAPIKey(r) {
			c.logger.Warn("Rejected WebSocket connection: invalid or missing API key",
				"remote_addr", r.RemoteAddr,
			)
			return false
		}

		// Step 2: Validate Origin
		origin := r.Header.Get("Origin")
		if origin == "" {
			// Non-browser clients (e.g., curl, CLI tools) may not send Origin
			// Allow these connections but log for auditing
			c.logger.Debug("WebSocket connection without Origin header", "remote_addr", r.RemoteAddr)
			return true
		}

		c.mu.RLock()
		defer c.mu.RUnlock()

		allowed := c.allowedOrigins[origin]
		if !allowed {
			c.logger.Warn("Rejected WebSocket connection from unauthorized origin",
				"origin", origin,
				"remote_addr", r.RemoteAddr,
			)
		}
		return allowed
	}
}

// validateAPIKey checks for a valid API key in the request.
// Supports both X-API-Key header and api_key query parameter.
func (c *SecurityConfig) validateAPIKey(r *http.Request) bool {
	// Try header first
	apiKey := r.Header.Get("X-API-Key")

	// Fall back to query parameter
	if apiKey == "" {
		apiKey = r.URL.Query().Get("api_key")
	}

	if apiKey == "" {
		return false
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	// Use constant-time comparison to prevent timing attacks
	for key := range c.apiKeys {
		if subtle.ConstantTimeCompare([]byte(apiKey), []byte(key)) == 1 {
			return true
		}
	}
	return false
}

// AddOrigin adds a new allowed origin at runtime.
func (c *SecurityConfig) AddOrigin(origin string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.allowedOrigins[origin] = true
	c.logger.Info("Added allowed origin", "origin", origin)
}

// RemoveOrigin removes an origin from the allowed list.
func (c *SecurityConfig) RemoveOrigin(origin string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.allowedOrigins, origin)
	c.logger.Info("Removed allowed origin", "origin", origin)
}

// ListOrigins returns a copy of the current allowed origins.
func (c *SecurityConfig) ListOrigins() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	origins := make([]string, 0, len(c.allowedOrigins))
	for origin := range c.allowedOrigins {
		origins = append(origins, origin)
	}
	return origins
}

// AddAPIKey adds a new API key at runtime.
func (c *SecurityConfig) AddAPIKey(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.apiKeys[key] = true
	c.apiKeyEnabled = true
	c.logger.Info("Added API key", "key_prefix", maskAPIKey(key))
}

// RemoveAPIKey removes an API key.
func (c *SecurityConfig) RemoveAPIKey(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.apiKeys, key)
	c.apiKeyEnabled = len(c.apiKeys) > 0
	c.logger.Info("Removed API key", "key_prefix", maskAPIKey(key))
}

// IsAPIKeyEnabled returns whether API key authentication is enabled.
func (c *SecurityConfig) IsAPIKeyEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.apiKeyEnabled
}

// maskAPIKey returns a masked version of the API key for logging.
func maskAPIKey(key string) string {
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "****" + key[len(key)-4:]
}
