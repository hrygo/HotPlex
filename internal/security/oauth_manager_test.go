package security

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/config"
)

func TestOAuthManager_Empty(t *testing.T) {
	t.Parallel()

	ca, err := NewCookieAuth("")
	require.NoError(t, err)

	m := NewOAuthManager(ca)

	require.False(t, m.HasProviders(), "fresh manager has no providers")
	require.Empty(t, m.List(), "fresh manager lists nothing")
	_, ok := m.Lookup("anything")
	require.False(t, ok, "fresh manager lookup misses")
	require.Empty(t, m.ExternalURL(), "fresh manager has no external URL")
	require.Same(t, ca, m.CookieAuth(), "CookieAuth returns the injected signer")
}

func TestOAuthManager_ReloadSuccess(t *testing.T) {
	t.Parallel()

	mock := newMockOIDCServer(t)
	t.Cleanup(mock.close)

	ca, err := NewCookieAuth("")
	require.NoError(t, err)
	m := NewOAuthManager(ca)

	cfg := config.OAuthConfig{
		ExternalURL: "https://hotplex.example.com",
		Providers: []config.OAuthProviderConfig{{
			Name:        "keycloak",
			DisplayName: "企业 SSO",
			Issuer:      mock.issuer(),
			ClientID:    mock.clientID,
		}},
	}

	count, err := m.Reload(context.Background(), cfg)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	require.True(t, m.HasProviders())

	p, ok := m.Lookup("keycloak")
	require.True(t, ok)
	require.NotNil(t, p)
	require.Equal(t, "keycloak", p.Name())

	list := m.List()
	require.Len(t, list, 1)
	require.Equal(t, "keycloak", list[0].Name)
	require.Equal(t, "企业 SSO", list[0].DisplayName)

	require.Equal(t, "https://hotplex.example.com", m.ExternalURL())
}

func TestOAuthManager_ReloadEmptyClearsProviders(t *testing.T) {
	t.Parallel()

	mock := newMockOIDCServer(t)
	t.Cleanup(mock.close)

	m := NewOAuthManager(mustCookieAuth(t))

	loaded, err := m.Reload(context.Background(), config.OAuthConfig{
		ExternalURL: "https://hotplex.example.com",
		Providers: []config.OAuthProviderConfig{{
			Name: "keycloak", Issuer: mock.issuer(), ClientID: mock.clientID,
		}},
	})
	require.NoError(t, err)
	require.Equal(t, 1, loaded)
	require.True(t, m.HasProviders())

	// Empty providers list must clear existing providers and set externalURL.
	count, err := m.Reload(context.Background(), config.OAuthConfig{ExternalURL: "https://changed.example.com"})
	require.NoError(t, err)
	require.Equal(t, 0, count)
	require.False(t, m.HasProviders(), "reload with empty providers must clear registry")
	require.Empty(t, m.List())
	require.Equal(t, "https://changed.example.com", m.ExternalURL())
}

func TestOAuthManager_ReloadDiscoveryError(t *testing.T) {
	t.Parallel()

	m := NewOAuthManager(mustCookieAuth(t))

	// Unreachable issuer → discovery fails for every provider.
	cfg := config.OAuthConfig{
		ExternalURL: "https://hotplex.example.com",
		Providers: []config.OAuthProviderConfig{{
			Name: "bad", Issuer: "http://127.0.0.1:0", ClientID: "x", ClientSecret: "y",
		}},
	}

	count, err := m.Reload(context.Background(), cfg)
	require.Error(t, err)
	require.Equal(t, 0, count, "no provider loaded when all fail discovery")
	require.False(t, m.HasProviders())
}

func TestOAuthManager_ReloadPartialError(t *testing.T) {
	t.Parallel()

	mock := newMockOIDCServer(t)
	t.Cleanup(mock.close)

	m := NewOAuthManager(mustCookieAuth(t))

	// One good provider + one unreachable: good one loads, error still returned.
	cfg := config.OAuthConfig{
		ExternalURL: "https://hotplex.example.com",
		Providers: []config.OAuthProviderConfig{
			{Name: "good", Issuer: mock.issuer(), ClientID: mock.clientID},
			{Name: "bad", Issuer: "http://127.0.0.1:0", ClientID: "x", ClientSecret: "y"},
		},
	}

	count, err := m.Reload(context.Background(), cfg)
	require.Error(t, err)
	require.Equal(t, 1, count, "good provider must still load on partial failure")
	require.True(t, m.HasProviders())

	_, ok := m.Lookup("good")
	require.True(t, ok)
}

func TestOAuthManager_ReloadPreservesUnchangedProvider(t *testing.T) {
	t.Parallel()

	mock := newMockOIDCServer(t)
	t.Cleanup(mock.close)

	m := NewOAuthManager(mustCookieAuth(t))

	cfg := config.OAuthConfig{
		ExternalURL: "https://hotplex.example.com",
		Providers: []config.OAuthProviderConfig{{
			Name: "keycloak", Issuer: mock.issuer(), ClientID: mock.clientID, ClientSecret: "s",
		}},
	}

	first, err := m.Reload(context.Background(), cfg)
	require.NoError(t, err)
	require.Equal(t, 1, first)
	original, _ := m.Lookup("keycloak")

	// Reload identical issuer+client_id → provider preserved (no re-discovery).
	second, err := m.Reload(context.Background(), cfg)
	require.NoError(t, err)
	require.Equal(t, 1, second)

	preserved, ok := m.Lookup("keycloak")
	require.True(t, ok)
	require.Same(t, original, preserved, "unchanged provider must be preserved by identity, not re-discovered")
}

func TestOAuthManager_ReloadRediscoverOnClientIDChange(t *testing.T) {
	t.Parallel()

	mock := newMockOIDCServer(t)
	t.Cleanup(mock.close)

	m := NewOAuthManager(mustCookieAuth(t))

	cfg := config.OAuthConfig{
		ExternalURL: "https://hotplex.example.com",
		Providers: []config.OAuthProviderConfig{{
			Name: "keycloak", Issuer: mock.issuer(), ClientID: "client-v1", ClientSecret: "s",
		}},
	}
	_, err := m.Reload(context.Background(), cfg)
	require.NoError(t, err)
	original, _ := m.Lookup("keycloak")

	// Same name, different client_id → must re-discover a new provider.
	cfg.Providers[0].ClientID = "client-v2"
	count, err := m.Reload(context.Background(), cfg)
	require.NoError(t, err)
	require.Equal(t, 1, count)

	rediscovered, ok := m.Lookup("keycloak")
	require.True(t, ok)
	require.NotSame(t, original, rediscovered, "changed provider must be re-discovered into a new instance")
}

func mustCookieAuth(t *testing.T) *CookieAuth {
	t.Helper()
	ca, err := NewCookieAuth("")
	require.NoError(t, err)
	return ca
}
