package config

import (
	"fmt"
	"regexp"
	"strings"
)

// OAuthConfig holds enterprise SSO (OIDC) settings for WebChat multitenancy (spec ④).
// Independent from bot configs (Slack/Feishu) — those are Message Channel track,
// not WebChat SSO. This config is WebChat-only.
type OAuthConfig struct {
	// ExternalURL is the base URL for constructing OAuth callback URLs.
	// When empty, derived from the request Host header (same-origin deployments).
	// Must be set explicitly behind reverse proxies with different public URL.
	ExternalURL string `mapstructure:"external_url"`

	// Providers is the list of configured OIDC identity providers.
	// Multiple providers render as multiple SSO buttons on the login page (spec ⑥).
	Providers []OAuthProviderConfig `mapstructure:"providers"`
}

// OAuthProviderConfig defines a single OIDC identity provider.
// One standard OIDC client implementation covers all OIDC-compatible IdPs
// (Keycloak, Okta, Azure AD, Authing, 派拉, 玉符, 宁盾, etc.).
type OAuthProviderConfig struct {
	// Name is the unique identifier for this provider. Appears in URL paths
	// (/api/auth/oauth/{name}/login) and user_identities.provider.
	// Must match [a-z0-9-]+ to be URL-safe and prevent path traversal.
	Name string `mapstructure:"name"`

	// DisplayName is the human-readable label shown on the login page (spec ⑥).
	DisplayName string `mapstructure:"display_name"`

	// Issuer is the OIDC issuer URL. The OIDC discovery endpoint is auto-resolved
	// from {issuer}/.well-known/openid-configuration.
	Issuer string `mapstructure:"issuer"`

	// ClientID is the OAuth2 client identifier registered with the IdP.
	ClientID string `mapstructure:"client_id"`

	// ClientSecret is the OAuth2 client secret.
	ClientSecret string `mapstructure:"client_secret"`

	// Scopes are the OIDC scopes to request. Defaults to ["openid", "profile"].
	Scopes []string `mapstructure:"scopes"`

	// Optional claim name overrides. When empty, OIDC standard claim names are used.
	UsernameClaim    string `mapstructure:"username_claim"`
	DisplayNameClaim string `mapstructure:"display_name_claim"`
	EmailClaim       string `mapstructure:"email_claim"`
}

var oauthProviderNameRe = regexp.MustCompile(`^[a-z0-9-]+$`)

// Validate checks the OAuthConfig for correctness.
func (c *OAuthConfig) Validate() error {
	seen := make(map[string]bool)
	for i, p := range c.Providers {
		if p.Name == "" {
			return fmt.Errorf("oauth.providers[%d]: name is required", i)
		}
		if !oauthProviderNameRe.MatchString(p.Name) {
			return fmt.Errorf("oauth.providers[%d]: name %q must match [a-z0-9-]+", i, p.Name)
		}
		if seen[p.Name] {
			return fmt.Errorf("oauth.providers[%d]: duplicate provider name %q", i, p.Name)
		}
		seen[p.Name] = true

		if p.Issuer == "" {
			return fmt.Errorf("oauth.providers[%d] (%s): issuer is required", i, p.Name)
		}
		if p.ClientID == "" {
			return fmt.Errorf("oauth.providers[%d] (%s): client_id is required", i, p.Name)
		}
		if p.ClientSecret == "" {
			return fmt.Errorf("oauth.providers[%d] (%s): client_secret is required", i, p.Name)
		}
	}
	return nil
}

// DefaultScopes returns the scopes to request if none configured.
func (p OAuthProviderConfig) DefaultScopes() []string {
	if len(p.Scopes) > 0 {
		return p.Scopes
	}
	return []string{"openid", "profile"}
}

// EffectiveDisplayName returns DisplayName or falls back to Name.
func (p OAuthProviderConfig) EffectiveDisplayName() string {
	if p.DisplayName != "" {
		return p.DisplayName
	}
	return p.Name
}

// UsernameClaimName returns the configured or default claim for username.
func (p OAuthProviderConfig) UsernameClaimName() string {
	if p.UsernameClaim != "" {
		return p.UsernameClaim
	}
	return "preferred_username"
}

// DisplayNameClaimName returns the configured or default claim for display name.
func (p OAuthProviderConfig) DisplayNameClaimName() string {
	if p.DisplayNameClaim != "" {
		return p.DisplayNameClaim
	}
	return "name"
}

// EmailClaimName returns the configured or default claim for email.
func (p OAuthProviderConfig) EmailClaimName() string {
	if p.EmailClaim != "" {
		return p.EmailClaim
	}
	return "email"
}

// CallbackURL constructs the OAuth callback URL for this provider.
func (c *OAuthConfig) CallbackURL(externalURL, providerName string) string {
	base := strings.TrimRight(externalURL, "/")
	return fmt.Sprintf("%s/api/auth/oauth/%s/callback", base, providerName)
}
