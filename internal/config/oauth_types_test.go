package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOAuthConfig_Validate_Empty(t *testing.T) {
	t.Parallel()
	var cfg OAuthConfig
	require.NoError(t, cfg.Validate())
}

func TestOAuthConfig_Validate_Valid(t *testing.T) {
	t.Parallel()
	cfg := OAuthConfig{
		Providers: []OAuthProviderConfig{
			{Name: "keycloak", Issuer: "https://sso.example.com", ClientID: "id", ClientSecret: "secret"},
			{Name: "authing", Issuer: "https://xxx.authing.cn", ClientID: "id2", ClientSecret: "secret2"},
		},
	}
	require.NoError(t, cfg.Validate())
}

func TestOAuthConfig_Validate_MissingName(t *testing.T) {
	t.Parallel()
	cfg := OAuthConfig{
		Providers: []OAuthProviderConfig{
			{Issuer: "https://sso.example.com", ClientID: "id", ClientSecret: "secret"},
		},
	}
	err := cfg.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "name is required")
}

func TestOAuthConfig_Validate_InvalidName(t *testing.T) {
	t.Parallel()
	cfg := OAuthConfig{
		Providers: []OAuthProviderConfig{
			{Name: "Bad Name!", Issuer: "https://sso.example.com", ClientID: "id", ClientSecret: "secret"},
		},
	}
	err := cfg.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "[a-z0-9-]+")
}

func TestOAuthConfig_Validate_DuplicateName(t *testing.T) {
	t.Parallel()
	cfg := OAuthConfig{
		Providers: []OAuthProviderConfig{
			{Name: "kc", Issuer: "https://a", ClientID: "id", ClientSecret: "s"},
			{Name: "kc", Issuer: "https://b", ClientID: "id2", ClientSecret: "s2"},
		},
	}
	err := cfg.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate")
}

func TestOAuthConfig_Validate_MissingIssuer(t *testing.T) {
	t.Parallel()
	cfg := OAuthConfig{
		Providers: []OAuthProviderConfig{
			{Name: "kc", ClientID: "id", ClientSecret: "s"},
		},
	}
	err := cfg.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "issuer is required")
}

func TestOAuthProviderConfig_DefaultScopes(t *testing.T) {
	t.Parallel()
	p := OAuthProviderConfig{Scopes: nil}
	require.Equal(t, []string{"openid", "profile"}, p.DefaultScopes())

	p2 := OAuthProviderConfig{Scopes: []string{"openid", "email", "groups"}}
	require.Equal(t, []string{"openid", "email", "groups"}, p2.DefaultScopes())
}

func TestOAuthProviderConfig_ClaimDefaults(t *testing.T) {
	t.Parallel()
	p := OAuthProviderConfig{}
	require.Equal(t, "preferred_username", p.UsernameClaimName())
	require.Equal(t, "name", p.DisplayNameClaimName())
	require.Equal(t, "email", p.EmailClaimName())

	p2 := OAuthProviderConfig{UsernameClaim: "uid", DisplayNameClaim: "nick", EmailClaim: "mail"}
	require.Equal(t, "uid", p2.UsernameClaimName())
	require.Equal(t, "nick", p2.DisplayNameClaimName())
	require.Equal(t, "mail", p2.EmailClaimName())
}

func TestOAuthConfig_CallbackURL(t *testing.T) {
	t.Parallel()
	cfg := OAuthConfig{ExternalURL: "https://hotplex.example.com"}
	url := cfg.CallbackURL("https://hotplex.example.com", "keycloak")
	require.Equal(t, "https://hotplex.example.com/api/auth/oauth/keycloak/callback", url)

	// Trailing slash on external_url should be trimmed.
	url2 := cfg.CallbackURL("https://hotplex.example.com/", "kc")
	require.Equal(t, "https://hotplex.example.com/api/auth/oauth/kc/callback", url2)
}
