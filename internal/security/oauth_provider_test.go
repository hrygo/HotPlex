package security

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/require"
)

// mockOIDCServer simulates a full OIDC IdP for integration testing.
type mockOIDCServer struct {
	t              *testing.T
	server         *httptest.Server
	signingKey     *rsa.PrivateKey
	jwksPublicKey  jose.JSONWebKeySet
	clientID       string
	expectedSub    string
	expectedClaims map[string]any
	codeVerifier   string // captured during token exchange
}

func newMockOIDCServer(t *testing.T) *mockOIDCServer {
	t.Helper()
	// Generate EC P-256 signing key (ES256).
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	pubJWK := jose.JSONWebKey{
		Key:       &privKey.PublicKey,
		KeyID:     "test-key-1",
		Algorithm: string(jose.RS256),
		Use:       "sig",
	}

	var jwks jose.JSONWebKeySet
	jwks.Keys = append(jwks.Keys, pubJWK)

	m := &mockOIDCServer{
		t:             t,
		signingKey:    privKey,
		jwksPublicKey: jwks,
		clientID:      "test-client-id",
		expectedSub:   "user-sub-123",
		expectedClaims: map[string]any{
			"sub":                "user-sub-123",
			"preferred_username": "alice",
			"name":               "Alice Wonderland",
			"email":              "alice@example.com",
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", m.handleDiscovery)
	mux.HandleFunc("/oauth/token", m.handleToken)
	mux.HandleFunc("/oauth/jwks", m.handleJWKS)

	m.server = httptest.NewServer(mux)
	return m
}

func (m *mockOIDCServer) close() { m.server.Close() }

func (m *mockOIDCServer) issuer() string { return m.server.URL }

func (m *mockOIDCServer) handleDiscovery(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"issuer":                 m.server.URL,
		"authorization_endpoint": m.server.URL + "/oauth/authorize",
		"token_endpoint":         m.server.URL + "/oauth/token",
		"jwks_uri":               m.server.URL + "/oauth/jwks",
		"userinfo_endpoint":      m.server.URL + "/oauth/userinfo",
	})
}

func (m *mockOIDCServer) handleToken(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	verifier := r.FormValue("code_verifier")
	if verifier == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	m.codeVerifier = verifier

	// Sign an ID Token with the test key.
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: m.signingKey},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "test-key-1"))
	require.NoError(m.t, err)

	now := time.Now()
	claims := map[string]any{
		"iss": m.server.URL,
		"sub": m.expectedSub,
		"aud": m.clientID,
		"exp": now.Add(time.Hour).Unix(),
		"iat": now.Unix(),
	}
	for k, v := range m.expectedClaims {
		claims[k] = v
	}

	payload, _ := json.Marshal(claims)
	jws, err := signer.Sign(payload)
	require.NoError(m.t, err)
	idToken, err := jws.CompactSerialize()
	require.NoError(m.t, err)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token": "fake-access-token",
		"token_type":   "Bearer",
		"id_token":     idToken,
	})
}

func (m *mockOIDCServer) handleJWKS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(m.jwksPublicKey)
}

func TestOAuthProvider_DiscoveryAndClaims(t *testing.T) {
	t.Parallel()

	mock := newMockOIDCServer(t)
	defer mock.close()

	callbackURL := "https://hotplex.example.com/api/auth/oauth/keycloak/callback"

	op, err := NewOAuthProvider(context.Background(), OAuthProviderConfig{
		Name:         "keycloak",
		Issuer:       mock.issuer(),
		ClientID:     mock.clientID,
		ClientSecret: "test-secret",
		Scopes:       []string{"openid", "profile"},
	}, callbackURL)
	require.NoError(t, err, "OIDC discovery must succeed against mock IdP")
	require.Equal(t, "keycloak", op.Name())

	// Build auth URL.
	state, verifier, challenge, err := GenerateStateAndVerifier()
	require.NoError(t, err)

	authURL := op.BuildAuthURL(AuthURLOption{State: state, CodeChallenge: challenge})
	require.Contains(t, authURL, "response_type=code")
	require.Contains(t, authURL, "client_id="+mock.clientID)
	require.Contains(t, authURL, "state="+state)
	require.Contains(t, authURL, "code_challenge_method=S256")
	require.Contains(t, authURL, mock.server.URL+"/oauth/authorize")

	// Exchange code — mock IdP verifies the PKCE code_verifier internally.
	// We can't easily test the full redirect, so we test ExchangeCode + VerifyAndExtractClaims.
	// The mock token endpoint signs a real JWT that go-oidc will verify.
	exchange, err := op.ExchangeCode(context.Background(), "fake-auth-code", verifier)
	require.NoError(t, err)
	require.NotEmpty(t, exchange.IDToken)
	require.Equal(t, verifier, mock.codeVerifier, "PKCE verifier must be sent to token endpoint")

	// Verify ID Token and extract claims.
	claims, err := op.VerifyAndExtractClaims(context.Background(), exchange.IDToken)
	require.NoError(t, err)
	require.Equal(t, "user-sub-123", claims.Subject)
	require.Equal(t, "alice", claims.Username)
	require.Equal(t, "Alice Wonderland", claims.DisplayName)
	require.Equal(t, "alice@example.com", claims.Email)
}

func TestOAuthProvider_DiscoveryFailed(t *testing.T) {
	t.Parallel()
	_, err := NewOAuthProvider(context.Background(), OAuthProviderConfig{
		Name: "bad", Issuer: "http://127.0.0.1:0", // unreachable
		ClientID: "x", ClientSecret: "y",
	}, "https://example.com/callback")
	require.Error(t, err)
	require.True(t, IsDiscoveryError(err), "unreachable IdP should be a discovery error")
}

func TestOAuthProvider_ClaimFallback(t *testing.T) {
	t.Parallel()
	// Test claimString fallback logic directly.
	claims := map[string]any{
		"preferred_username": "bob",
		"name":               "Bob",
	}
	require.Equal(t, "bob", claimString(claims, "", "preferred_username"))
	require.Equal(t, "bob", claimString(claims, "custom", "preferred_username"))
	require.Equal(t, "", claimString(claims, "custom", "default"))
}
