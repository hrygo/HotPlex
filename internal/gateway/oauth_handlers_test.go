package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/security"
)

func TestOAuthHandlers_ProvidersReturnsStableEnvelopeWhenEmpty(t *testing.T) {
	t.Parallel()

	ca, err := security.NewCookieAuth("")
	require.NoError(t, err)
	h := NewOAuthHandlers(security.NewOAuthManager(ca), ca, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/auth/oauth/providers", nil)
	w := httptest.NewRecorder()
	h.Providers(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Providers []security.ProviderInfo `json:"providers"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Empty(t, resp.Providers)
}

func TestOAuthHandlers_LoginDerivesCallbackURLFromForwardedHeaders(t *testing.T) {
	t.Parallel()

	idp := newDiscoveryOnlyOIDCServer(t)
	defer idp.Close()

	ca, err := security.NewCookieAuth("")
	require.NoError(t, err)
	mgr := security.NewOAuthManager(ca)
	loaded, err := mgr.Reload(context.Background(), config.OAuthConfig{
		Providers: []config.OAuthProviderConfig{{
			Name:         "keycloak",
			DisplayName:  "Enterprise SSO",
			Issuer:       idp.URL,
			ClientID:     "hotplex-webchat",
			ClientSecret: "secret",
		}},
	})
	require.NoError(t, err)
	require.Equal(t, 1, loaded)

	h := NewOAuthHandlers(mgr, ca, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/auth/oauth/keycloak/login", nil)
	req.SetPathValue("provider", "keycloak")
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "hotplex.example.com")
	w := httptest.NewRecorder()

	h.Login(w, req)

	require.Equal(t, http.StatusFound, w.Code)
	location := w.Header().Get("Location")
	require.NotEmpty(t, location)
	authURL, err := url.Parse(location)
	require.NoError(t, err)
	require.Equal(t, idp.URL+"/oauth/authorize", authURL.Scheme+"://"+authURL.Host+authURL.Path)
	require.Equal(t, "https://hotplex.example.com/api/auth/oauth/keycloak/callback", authURL.Query().Get("redirect_uri"))
	require.Equal(t, "S256", authURL.Query().Get("code_challenge_method"))
}

func newDiscoveryOnlyOIDCServer(t *testing.T) *httptest.Server {
	t.Helper()
	var server *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"issuer":                 server.URL,
			"authorization_endpoint": server.URL + "/oauth/authorize",
			"token_endpoint":         server.URL + "/oauth/token",
			"jwks_uri":               server.URL + "/oauth/jwks",
		})
	})
	server = httptest.NewServer(mux)
	return server
}
