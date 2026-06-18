package security

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// OAuthProviderConfig holds the configuration for a single OIDC provider.
// Defined here (not in config package) to avoid circular import: security
// needs these fields for OIDC client construction, and config imports nothing
// from security. The config package's OAuthProviderConfig is the YAML-facing
// struct; OAuthManager converts at construction time.
type OAuthProviderConfig struct {
	Name             string
	DisplayName      string
	Issuer           string
	ClientID         string
	ClientSecret     string
	Scopes           []string
	UsernameClaim    string
	DisplayNameClaim string
	EmailClaim       string
}

// OIDCClaims holds the extracted user info from an OIDC ID Token / UserInfo response.
type OIDCClaims struct {
	Subject     string
	Username    string
	DisplayName string
	Email       string
}

// OAuthProvider implements IdentityProvider for a single OIDC identity provider.
// It handles OIDC discovery, authorization URL construction, token exchange,
// and ID Token verification. Unlike LocalAccountProvider (synchronous password
// check), OAuth authentication is a multi-step redirect flow handled by
// OAuthHandlers; OAuthProvider.Authenticate is NOT used for the redirect flow.
// Instead, OAuthProvider exposes BuildAuthURL / ExchangeCode for the handlers,
// and GetOrCreateUser is called after claims extraction.
//
// OAuthProvider does NOT implement IdentityProvider.Authenticate because OIDC
// auth is redirect-based (not synchronous credential check). The identity
// layer (IdentityProvider interface) is for password login only. SSO login
// result (user_id) is produced directly by the handler → store flow.
type OAuthProvider struct {
	config   OAuthProviderConfig
	provider *oidc.Provider
	verifier *oidc.IDTokenVerifier
	oauth2   *oauth2.Config
}

// oidcDiscoveryTimeout is the maximum time allowed for OIDC provider discovery.
// Prevents config hot-reload from hanging when an IdP endpoint is unreachable.
const oidcDiscoveryTimeout = 10 * time.Second

// NewOAuthProvider discovers the OIDC provider endpoints and constructs a
// verified client. Returns error if discovery fails (IdP unreachable, invalid
// issuer URL, malformed discovery document).
//
// A dedicated HTTP client with oidcDiscoveryTimeout is injected via
// oidc.ClientContext to prevent unbounded blocking on slow/dead IdPs.
func NewOAuthProvider(ctx context.Context, cfg OAuthProviderConfig, callbackURL string) (*OAuthProvider, error) {
	httpClient := &http.Client{Timeout: oidcDiscoveryTimeout}
	discoveryCtx := oidc.ClientContext(ctx, httpClient)
	provider, err := oidc.NewProvider(discoveryCtx, cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("oauth provider %q: discovery failed for issuer %q: %w", cfg.Name, cfg.Issuer, err)
	}

	oauth2Cfg := &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  callbackURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       cfg.Scopes,
	}

	verifier := provider.Verifier(&oidc.Config{ClientID: cfg.ClientID})

	return &OAuthProvider{
		config:   cfg,
		provider: provider,
		verifier: verifier,
		oauth2:   oauth2Cfg,
	}, nil
}

// Name returns the provider's unique name.
func (p *OAuthProvider) Name() string { return p.config.Name }

// DisplayName returns the provider's display name (falls back to Name).
func (p *OAuthProvider) DisplayName() string {
	if p.config.DisplayName != "" {
		return p.config.DisplayName
	}
	return p.config.Name
}

// Config returns the provider configuration (read-only by convention).
func (p *OAuthProvider) Config() OAuthProviderConfig { return p.config }

// AuthURLOption holds PKCE + state parameters for a single OAuth flow attempt.
type AuthURLOption struct {
	State         string
	CodeChallenge string
}

// BuildAuthURL constructs the OIDC authorization redirect URL with PKCE.
func (p *OAuthProvider) BuildAuthURL(opts AuthURLOption) string {
	return p.oauth2.AuthCodeURL(opts.State,
		oauth2.SetAuthURLParam("code_challenge", opts.CodeChallenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	)
}

// ExchangeResult holds the output of a successful token exchange.
type ExchangeResult struct {
	IDToken      string
	AccessToken  string
	RefreshToken string // may be empty if IdP doesn't return one
}

// ExchangeCode exchanges the authorization code for tokens. The codeVerifier
// must match the code_challenge sent in BuildAuthURL.
func (p *OAuthProvider) ExchangeCode(ctx context.Context, code, codeVerifier string) (*ExchangeResult, error) {
	token, err := p.oauth2.Exchange(ctx, code,
		oauth2.SetAuthURLParam("code_verifier", codeVerifier),
	)
	if err != nil {
		return nil, fmt.Errorf("oauth provider %q: token exchange failed: %w", p.config.Name, err)
	}

	result := &ExchangeResult{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
	}

	// Extract raw ID Token from the token response.
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return nil, fmt.Errorf("oauth provider %q: no id_token in token response", p.config.Name)
	}
	result.IDToken = rawIDToken

	return result, nil
}

// VerifyAndExtractClaims verifies the ID Token signature and extracts user claims.
// It validates: signature (JWKS), issuer, audience, expiration.
func (p *OAuthProvider) VerifyAndExtractClaims(ctx context.Context, rawIDToken string) (*OIDCClaims, error) {
	idToken, err := p.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("oauth provider %q: id_token verification failed: %w", p.config.Name, err)
	}

	claims := &OIDCClaims{Subject: idToken.Subject}

	// Extract custom claims from the ID Token payload.
	// Use configured claim names with OIDC standard fallbacks.
	var raw map[string]any
	if err := idToken.Claims(&raw); err != nil {
		return nil, fmt.Errorf("oauth provider %q: parse claims: %w", p.config.Name, err)
	}

	claims.Username = claimString(raw, p.config.UsernameClaim, "preferred_username")
	claims.DisplayName = claimString(raw, p.config.DisplayNameClaim, "name")
	claims.Email = claimString(raw, p.config.EmailClaim, "email")

	if claims.Username == "" {
		// Fallback: use subject as username if preferred_username not present.
		claims.Username = idToken.Subject
	}

	return claims, nil
}

// claimString extracts a string claim from the raw claims map, trying the
// configured name first, then the default name, then returning "".
func claimString(claims map[string]any, configuredName, defaultName string) string {
	name := defaultName
	if configuredName != "" {
		name = configuredName
	}
	if v, ok := claims[name]; ok {
		if s, ok := v.(string); ok {
			return strings.TrimSpace(s)
		}
	}
	// Fallback to default if configured name didn't match.
	if configuredName != "" && configuredName != defaultName {
		if v, ok := claims[defaultName]; ok {
			if s, ok := v.(string); ok {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

// OAuthError wraps OIDC flow errors with a provider name for diagnostics.
type OAuthError struct {
	Provider string
	Err      error
}

func (e *OAuthError) Error() string {
	return fmt.Sprintf("oauth[%s]: %v", e.Provider, e.Err)
}

func (e *OAuthError) Unwrap() error { return e.Err }

// IsDiscoveryError returns true if the error is caused by OIDC discovery failure
// (IdP unreachable or misconfigured).
func IsDiscoveryError(err error) bool {
	if err == nil {
		return false
	}
	var oe *OAuthError
	if errors.As(err, &oe) {
		err = oe.Err
	}
	return strings.Contains(err.Error(), "discovery failed") ||
		strings.Contains(err.Error(), "oidc:") ||
		strings.Contains(err.Error(), "unsuccessful")
}
