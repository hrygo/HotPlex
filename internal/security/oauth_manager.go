package security

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/hrygo/hotplex/internal/config"
)

// OAuthManager manages multiple OIDC providers. It is the central registry
// for SSO providers, supporting runtime hot-reload (provider list rebuilt from
// config changes without dropping in-flight OAuth flows).
//
// Thread-safety: providers are stored behind RWMutex. Lookup is concurrent-safe.
// In-flight flows are unaffected by reload because state cookies encode the
// provider name, and lookup at callback time reads the current registry.
type OAuthManager struct {
	mu          sync.RWMutex
	providers   map[string]*OAuthProvider
	externalURL string
	cookieAuth  *CookieAuth // for signing state cookies
}

// NewOAuthManager creates an empty manager.
func NewOAuthManager(cookieAuth *CookieAuth) *OAuthManager {
	return &OAuthManager{
		providers:  make(map[string]*OAuthProvider),
		cookieAuth: cookieAuth,
	}
}

// Reload rebuilds the provider registry from the given OAuthConfig.
// Discovery is performed for each provider; a provider that fails discovery
// is skipped (logged by caller). Returns count of successfully loaded providers.
//
// Providers not in the new config are removed. Existing providers with unchanged
// issuer/client_id are preserved (no re-discovery) to avoid churn during
// unrelated config reloads.
func (m *OAuthManager) Reload(ctx context.Context, cfg config.OAuthConfig) (int, error) {
	if len(cfg.Providers) == 0 {
		m.mu.Lock()
		m.providers = make(map[string]*OAuthProvider)
		m.externalURL = cfg.ExternalURL
		m.mu.Unlock()
		return 0, nil
	}

	externalURL := cfg.ExternalURL
	newProviders := make(map[string]*OAuthProvider)
	var errs []error

	for _, pcfg := range cfg.Providers {
		// Check if we already have this provider with same issuer+clientID.
		m.mu.RLock()
		existing, ok := m.providers[pcfg.Name]
		m.mu.RUnlock()

		if ok && existing.Config().Issuer == pcfg.Issuer && existing.Config().ClientID == pcfg.ClientID {
			// Preserve existing (no re-discovery needed); only update non-discovery fields.
			newProviders[pcfg.Name] = existing
			continue
		}

		// Build callback URL.
		callbackURL := cfg.CallbackURL(externalURL, pcfg.Name)

		// Construct OAuthProviderConfig from config.
		opCfg := OAuthProviderConfig{
			Name:             pcfg.Name,
			DisplayName:      pcfg.DisplayName,
			Issuer:           pcfg.Issuer,
			ClientID:         pcfg.ClientID,
			ClientSecret:     pcfg.ClientSecret,
			Scopes:           pcfg.DefaultScopes(),
			UsernameClaim:    pcfg.UsernameClaimName(),
			DisplayNameClaim: pcfg.DisplayNameClaimName(),
			EmailClaim:       pcfg.EmailClaimName(),
		}

		provider, err := NewOAuthProvider(ctx, opCfg, callbackURL)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		newProviders[pcfg.Name] = provider
	}

	m.mu.Lock()
	m.providers = newProviders
	m.externalURL = externalURL
	m.mu.Unlock()

	if len(errs) > 0 {
		return len(newProviders), fmt.Errorf("oauth manager: %d provider(s) failed to load: %w", len(errs), errors.Join(errs...))
	}
	return len(newProviders), nil
}

// Lookup returns the OAuthProvider by name.
func (m *OAuthManager) Lookup(name string) (*OAuthProvider, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.providers[name]
	return p, ok
}

// List returns all registered provider names (sorted by registration order is
// not guaranteed; callers should sort if deterministic order is needed).
func (m *OAuthManager) List() []ProviderInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]ProviderInfo, 0, len(m.providers))
	for _, p := range m.providers {
		out = append(out, ProviderInfo{
			Name:        p.Name(),
			DisplayName: p.DisplayName(),
		})
	}
	return out
}

// ProviderInfo is the public-facing provider descriptor (no secrets).
type ProviderInfo struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
}

// ExternalURL returns the configured external base URL.
func (m *OAuthManager) ExternalURL() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.externalURL
}

// HasProviders returns true if at least one provider is configured.
func (m *OAuthManager) HasProviders() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.providers) > 0
}

// CookieAuth returns the CookieAuth used for signing state cookies.
// Used by OAuthHandlers to issue/verify state cookies.
func (m *OAuthManager) CookieAuth() *CookieAuth { return m.cookieAuth }
