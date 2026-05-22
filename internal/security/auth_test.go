package security

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hrygo/hotplex/internal/config"

	"github.com/stretchr/testify/require"
)

// ─── Authenticator ─────────────────────────────────────────────────────────────

func TestNewAuthenticator(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  *config.SecurityConfig
		want int
	}{
		{
			name: "empty api keys",
			cfg:  &config.SecurityConfig{APIKeys: []string{}},
			want: 0,
		},
		{
			name: "single api key",
			cfg:  &config.SecurityConfig{APIKeys: []string{"key1"}},
			want: 1,
		},
		{
			name: "multiple api keys",
			cfg:  &config.SecurityConfig{APIKeys: []string{"key1", "key2", "key3"}},
			want: 3,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			auth := NewAuthenticator(tt.cfg)
			require.NotNil(t, auth)
			require.Equal(t, tt.want, len(auth.validKey))
		})
	}
}

func TestAuthenticateRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		apiKeys    []string
		headerName string
		requestKey string
		wantUserID string
		wantErr    bool
	}{
		{
			name:       "no keys configured dev mode",
			apiKeys:    []string{},
			requestKey: "any-key",
			wantUserID: "anonymous",
			wantErr:    false,
		},
		{
			name:       "missing api key header",
			apiKeys:    []string{"secret1"},
			requestKey: "",
			wantErr:    true,
		},
		{
			name:       "valid api key",
			apiKeys:    []string{"secret1", "secret2"},
			requestKey: "secret1",
			wantUserID: "api_user",
			wantErr:    false,
		},
		{
			name:       "invalid api key",
			apiKeys:    []string{"secret1"},
			requestKey: "wrong-key",
			wantErr:    true,
		},
		{
			name:       "custom header name",
			apiKeys:    []string{"secret1"},
			headerName: "X-Custom-Auth",
			requestKey: "secret1",
			wantUserID: "api_user",
			wantErr:    false,
		},
		{
			name:       "custom header missing",
			apiKeys:    []string{"secret1"},
			headerName: "X-Custom-Auth",
			requestKey: "",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := &config.SecurityConfig{
				APIKeys:      tt.apiKeys,
				APIKeyHeader: tt.headerName,
			}
			auth := NewAuthenticator(cfg)

			req := httptest.NewRequest("GET", "/test", nil)
			if tt.requestKey != "" {
				header := tt.headerName
				if header == "" {
					header = "X-API-Key"
				}
				req.Header.Set(header, tt.requestKey)
			}

			userID, _, err := auth.AuthenticateRequest(req)
			if tt.wantErr {
				require.Error(t, err)
				require.Equal(t, ErrUnauthorized, err)
				require.Empty(t, userID)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.wantUserID, userID)
			}
		})
	}
}

func TestBotIDFromRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		header    string
		query     string
		wantBotID string
	}{
		{
			name:      "X-Bot-ID header",
			header:    "bot_001",
			query:     "",
			wantBotID: "bot_001",
		},
		{
			name:      "bot_id query param fallback",
			header:    "",
			query:     "bot_002",
			wantBotID: "bot_002",
		},
		{
			name:      "header takes precedence over query",
			header:    "bot_header",
			query:     "bot_query",
			wantBotID: "bot_header",
		},
		{
			name:      "no bot id provided",
			header:    "",
			query:     "",
			wantBotID: "",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			url := "/test"
			if tt.query != "" {
				url += "?bot_id=" + tt.query
			}
			req := httptest.NewRequest("GET", url, nil)
			if tt.header != "" {
				req.Header.Set("X-Bot-ID", tt.header)
			}

			botID := BotIDFromRequest(req)
			require.Equal(t, tt.wantBotID, botID)
		})
	}
}

func TestAuthenticateRequest_BotIDFromRequest(t *testing.T) {
	t.Parallel()

	apiKey := "secret-api-key"
	cfg := &config.SecurityConfig{
		APIKeys:      []string{apiKey},
		APIKeyHeader: "X-API-Key",
	}
	auth := NewAuthenticator(cfg)

	t.Run("X-Bot-ID header", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("X-API-Key", apiKey)
		req.Header.Set("X-Bot-ID", "bot_001")

		userID, botID, err := auth.AuthenticateRequest(req)
		require.NoError(t, err)
		require.Equal(t, "api_user", userID)
		require.Equal(t, "bot_001", botID)
	})

	t.Run("bot_id query param", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest("GET", "/test?bot_id=bot_002", nil)
		req.Header.Set("X-API-Key", apiKey)

		_, botID, err := auth.AuthenticateRequest(req)
		require.NoError(t, err)
		require.Equal(t, "bot_002", botID)
	})

	t.Run("no bot id", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("X-API-Key", apiKey)

		_, botID, err := auth.AuthenticateRequest(req)
		require.NoError(t, err)
		require.Empty(t, botID)
	})
}

func TestMiddleware(t *testing.T) {
	t.Parallel()

	cfg := &config.SecurityConfig{APIKeys: []string{"secret123"}}
	auth := NewAuthenticator(cfg)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	})

	tests := []struct {
		name       string
		apiKey     string
		wantStatus int
	}{
		{
			name:       "unauthorized missing key",
			apiKey:     "",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "unauthorized wrong key",
			apiKey:     "wrong",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "authorized",
			apiKey:     "secret123",
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest("GET", "/protected", nil)
			if tt.apiKey != "" {
				req.Header.Set("X-API-Key", tt.apiKey)
			}

			rec := httptest.NewRecorder()
			auth.Middleware(handler).ServeHTTP(rec, req)

			require.Equal(t, tt.wantStatus, rec.Code)
		})
	}
}

func TestMiddleware_DevMode(t *testing.T) {
	t.Parallel()

	cfg := &config.SecurityConfig{APIKeys: []string{}}
	auth := NewAuthenticator(cfg)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/protected", nil)

	rec := httptest.NewRecorder()
	auth.Middleware(handler).ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

// ─── Claims context ───────────────────────────────────────────────────────────

func TestWithClaims_ClaimsFrom(t *testing.T) {
	t.Parallel()

	claims := Claims{
		UserID: "user123",
		APIKey: "secret",
	}

	ctx := context.Background()
	ctxWithClaims := WithClaims(ctx, claims)

	extracted, ok := ClaimsFrom(ctxWithClaims)
	require.True(t, ok)
	require.Equal(t, claims, extracted)
}

func TestClaimsFrom_NoClaims(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	claims, ok := ClaimsFrom(ctx)

	require.False(t, ok)
	require.Equal(t, Claims{}, claims)
}

func TestClaimsFrom_WrongType(t *testing.T) {
	t.Parallel()

	ctx := context.WithValue(context.Background(), claimsKey, "not-claims")

	claims, ok := ClaimsFrom(ctx)
	require.False(t, ok)
	require.Equal(t, Claims{}, claims)
}

func TestReloadKeys(t *testing.T) {
	t.Parallel()

	cfg := &config.SecurityConfig{APIKeys: []string{"key1"}}
	auth := NewAuthenticator(cfg)

	userID, ok := auth.AuthenticateKey(context.Background(), "key1")
	require.True(t, ok)
	require.Equal(t, "api_user", userID)

	auth.ReloadKeys(&config.SecurityConfig{APIKeys: []string{"key2", "key3"}})

	_, ok = auth.AuthenticateKey(context.Background(), "key1")
	require.False(t, ok)

	userID, ok = auth.AuthenticateKey(context.Background(), "key2")
	require.True(t, ok)
	require.Equal(t, "api_user", userID)
}

func TestExtractAPIKey(t *testing.T) {
	t.Parallel()

	cfg := &config.SecurityConfig{APIKeys: []string{"test"}}
	auth := NewAuthenticator(cfg)

	t.Run("from header", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("X-API-Key", "my-key")
		key, ok := auth.ExtractAPIKey(req)
		require.True(t, ok)
		require.Equal(t, "my-key", key)
	})

	t.Run("from query param", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest("GET", "/?api_key=query-key", nil)
		key, ok := auth.ExtractAPIKey(req)
		require.True(t, ok)
		require.Equal(t, "query-key", key)
	})

	t.Run("header takes precedence", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest("GET", "/?api_key=query-key", nil)
		req.Header.Set("X-API-Key", "header-key")
		key, ok := auth.ExtractAPIKey(req)
		require.True(t, ok)
		require.Equal(t, "header-key", key)
	})

	t.Run("missing key", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest("GET", "/", nil)
		_, ok := auth.ExtractAPIKey(req)
		require.False(t, ok)
	})
}

func TestAuthenticateKey(t *testing.T) {
	t.Parallel()

	t.Run("valid key", func(t *testing.T) {
		t.Parallel()
		auth := NewAuthenticator(&config.SecurityConfig{APIKeys: []string{"secret"}})
		userID, ok := auth.AuthenticateKey(context.Background(), "secret")
		require.True(t, ok)
		require.Equal(t, "api_user", userID)
	})

	t.Run("invalid key", func(t *testing.T) {
		t.Parallel()
		auth := NewAuthenticator(&config.SecurityConfig{APIKeys: []string{"secret"}})
		_, ok := auth.AuthenticateKey(context.Background(), "wrong")
		require.False(t, ok)
	})

	t.Run("dev mode", func(t *testing.T) {
		t.Parallel()
		auth := NewAuthenticator(&config.SecurityConfig{APIKeys: []string{}})
		userID, ok := auth.AuthenticateKey(context.Background(), "anything")
		require.True(t, ok)
		require.Equal(t, "anonymous", userID)
	})
}

func TestRegisterCommand(t *testing.T) {
	// Do NOT use t.Parallel() — RegisterCommand mutates the global allowedCommands map.
	t.Run("valid command", func(t *testing.T) {
		err := RegisterCommand("custom-worker")
		require.NoError(t, err)
		require.NoError(t, ValidateCommand("custom-worker"))
	})

	t.Run("empty name", func(t *testing.T) {
		err := RegisterCommand("")
		require.Error(t, err)
	})

	t.Run("path separator", func(t *testing.T) {
		err := RegisterCommand("foo/bar")
		require.Error(t, err)
	})

	t.Run("dangerous chars", func(t *testing.T) {
		err := RegisterCommand("foo;bar")
		require.Error(t, err)
	})
}

// ─── API Key Resolver Integration ─────────────────────────────────────────────

func TestAuthenticator_WithMapResolver(t *testing.T) {
	t.Parallel()

	cfg := &config.SecurityConfig{
		APIKeys: []string{"sk-alice", "sk-bob", "sk-orphan"},
	}
	auth := NewAuthenticator(cfg)
	auth.SetKeyResolver(NewMapResolver(map[string]string{
		"sk-alice": "alice",
		"sk-bob":   "bob",
	}))

	tests := []struct {
		name       string
		key        string
		wantUserID string
	}{
		{"mapped alice", "sk-alice", "alice"},
		{"mapped bob", "sk-bob", "bob"},
		{"unmapped falls back", "sk-orphan", "api_user"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			uid, ok := auth.AuthenticateKey(context.Background(), tt.key)
			require.True(t, ok)
			require.Equal(t, tt.wantUserID, uid)
		})
	}
}

func TestAuthenticator_SetKeyResolver_Nil(t *testing.T) {
	t.Parallel()

	cfg := &config.SecurityConfig{APIKeys: []string{"sk-test"}}
	auth := NewAuthenticator(cfg)

	auth.SetKeyResolver(NewMapResolver(map[string]string{"sk-test": "mapped"}))
	uid, ok := auth.AuthenticateKey(context.Background(), "sk-test")
	require.True(t, ok)
	require.Equal(t, "mapped", uid)

	auth.SetKeyResolver(nil)
	uid, ok = auth.AuthenticateKey(context.Background(), "sk-test")
	require.True(t, ok)
	require.Equal(t, "api_user", uid)
}

func TestAuthenticator_WithResolver_AuthenticateRequest(t *testing.T) {
	t.Parallel()

	cfg := &config.SecurityConfig{APIKeys: []string{"sk-alice"}}
	auth := NewAuthenticator(cfg)
	auth.SetKeyResolver(NewMapResolver(map[string]string{"sk-alice": "alice"}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-API-Key", "sk-alice")

	userID, _, err := auth.AuthenticateRequest(req)
	require.NoError(t, err)
	require.Equal(t, "alice", userID)
}

func TestAuthenticator_ResolverWithBotIDHeader(t *testing.T) {
	t.Parallel()

	apiKey := "sk-alice"
	cfg := &config.SecurityConfig{APIKeys: []string{apiKey}}
	auth := NewAuthenticator(cfg)
	auth.SetKeyResolver(NewMapResolver(map[string]string{apiKey: "alice-resolved"}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-API-Key", apiKey)
	req.Header.Set("X-Bot-ID", "bot-007")

	userID, botID, err := auth.AuthenticateRequest(req)
	require.NoError(t, err)
	require.Equal(t, "alice-resolved", userID, "resolver userID should override default")
	require.Equal(t, "bot-007", botID, "X-Bot-ID header should be extracted")
}

func TestAuthenticator_ChainResolver(t *testing.T) {
	t.Parallel()

	cfg := &config.SecurityConfig{APIKeys: []string{"sk-1", "sk-2"}}
	auth := NewAuthenticator(cfg)

	dbResolver := NewMapResolver(map[string]string{"sk-1": "db-user"})
	configResolver := NewMapResolver(map[string]string{"sk-1": "config-user", "sk-2": "config-only"})
	auth.SetKeyResolver(NewChainResolver(dbResolver, configResolver))

	uid, ok := auth.AuthenticateKey(context.Background(), "sk-1")
	require.True(t, ok)
	require.Equal(t, "db-user", uid, "DB resolver should take priority over config")

	uid, ok = auth.AuthenticateKey(context.Background(), "sk-2")
	require.True(t, ok)
	require.Equal(t, "config-only", uid, "Config resolver should be fallback when DB has no entry")
}
