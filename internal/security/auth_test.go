package security

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/hrygo/hotplex/internal/audit"
	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/dbutil"

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
			require.Equal(t, tt.want, auth.numValidKeys)
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
		authHeader string
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
			name:       "valid bearer api key",
			apiKeys:    []string{"secret1"},
			authHeader: "Bearer secret1",
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
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
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

	t.Run("from bearer authorization", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("Authorization", "Bearer bearer-key")
		key, ok := auth.ExtractAPIKey(req)
		require.True(t, ok)
		require.Equal(t, "bearer-key", key)
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

// ─── DB-sourced Keys (AddKey / RemoveKey) ──────────────────────────────────────

func TestAddKey(t *testing.T) {
	t.Parallel()

	auth := NewAuthenticator(&config.SecurityConfig{APIKeys: []string{"config-key"}})

	// Before AddKey, DB key is rejected.
	_, ok := auth.AuthenticateKey(context.Background(), "db-key-1")
	require.False(t, ok)

	auth.AddKey("db-key-1")

	// After AddKey, DB key authenticates.
	userID, ok := auth.AuthenticateKey(context.Background(), "db-key-1")
	require.True(t, ok)
	require.Equal(t, "api_user", userID)

	// Config key still works.
	userID, ok = auth.AuthenticateKey(context.Background(), "config-key")
	require.True(t, ok)
	require.Equal(t, "api_user", userID)
}

func TestRemoveKey(t *testing.T) {
	t.Parallel()

	auth := NewAuthenticator(&config.SecurityConfig{APIKeys: []string{"config-key"}})
	auth.AddKey("db-key-removeme")

	userID, ok := auth.AuthenticateKey(context.Background(), "db-key-removeme")
	require.True(t, ok)
	require.Equal(t, "api_user", userID)

	auth.RemoveKey("db-key-removeme")

	_, ok = auth.AuthenticateKey(context.Background(), "db-key-removeme")
	require.False(t, ok)

	// Config key still works after DB key removal.
	_, ok = auth.AuthenticateKey(context.Background(), "config-key")
	require.True(t, ok)
}

func TestAddKey_AuthenticateRequest(t *testing.T) {
	t.Parallel()

	auth := NewAuthenticator(&config.SecurityConfig{APIKeys: []string{"config-key"}})
	auth.AddKey("hpk_abc123")

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-API-Key", "hpk_abc123")

	userID, _, err := auth.AuthenticateRequest(req)
	require.NoError(t, err)
	require.Equal(t, "api_user", userID)
}

func TestReloadKeys_PreservesDBKeys(t *testing.T) {
	t.Parallel()

	auth := NewAuthenticator(&config.SecurityConfig{APIKeys: []string{"old-config"}})

	// Add a DB key.
	auth.AddKey("db-persistent")

	// Verify both work before reload.
	_, ok := auth.AuthenticateKey(context.Background(), "old-config")
	require.True(t, ok)
	_, ok = auth.AuthenticateKey(context.Background(), "db-persistent")
	require.True(t, ok)

	// Hot-reload config keys — old-config replaced by new-config.
	auth.ReloadKeys(&config.SecurityConfig{APIKeys: []string{"new-config"}})

	// Old config key gone.
	_, ok = auth.AuthenticateKey(context.Background(), "old-config")
	require.False(t, ok)

	// New config key works.
	_, ok = auth.AuthenticateKey(context.Background(), "new-config")
	require.True(t, ok)

	// DB key survives reload.
	_, ok = auth.AuthenticateKey(context.Background(), "db-persistent")
	require.True(t, ok)
}

func TestDevMode_DisabledByDBKeys(t *testing.T) {
	t.Parallel()

	// No config keys → dev mode active.
	auth := NewAuthenticator(&config.SecurityConfig{APIKeys: []string{}})
	userID, ok := auth.AuthenticateKey(context.Background(), "anything")
	require.True(t, ok)
	require.Equal(t, "anonymous", userID)

	// Adding a DB key disables dev mode.
	auth.AddKey("hpk_first")
	_, ok = auth.AuthenticateKey(context.Background(), "anything")
	require.False(t, ok, "dev mode should be disabled when dbKeys is non-empty")

	// The DB key itself works.
	userID, ok = auth.AuthenticateKey(context.Background(), "hpk_first")
	require.True(t, ok)
	require.Equal(t, "api_user", userID)

	// Removing the DB key does NOT re-enable dev mode (devModeLocked).
	auth.RemoveKey("hpk_first")
	_, ok = auth.AuthenticateKey(context.Background(), "anything")
	require.False(t, ok, "dev mode should stay disabled after RemoveKey (devModeLocked)")

	// No key works at all — must restart gateway or add a new key.
	_, ok = auth.AuthenticateKey(context.Background(), "hpk_first")
	require.False(t, ok, "removed key should no longer authenticate")
}

type mockIDP struct {
	LookupFunc func(ctx context.Context, userID string) (*User, error)
}

func (m *mockIDP) Authenticate(ctx context.Context, creds Credentials) (string, error) {
	return "", nil
}

func (m *mockIDP) Lookup(ctx context.Context, userID string) (*User, error) {
	if m.LookupFunc != nil {
		return m.LookupFunc(ctx, userID)
	}
	return nil, ErrUserNotFound
}

func TestAuthenticator_DisabledUser(t *testing.T) {
	t.Parallel()

	// 1. AuthenticateKey
	auth := NewAuthenticator(&config.SecurityConfig{
		APIKeys: []string{"sk-test"},
	})

	idp := &mockIDP{
		LookupFunc: func(ctx context.Context, userID string) (*User, error) {
			if userID == "active_uid" {
				return &User{ID: "active_uid", Status: "active"}, nil
			}
			if userID == "disabled_uid" {
				return &User{ID: "disabled_uid", Status: "disabled"}, nil
			}
			return nil, ErrUserNotFound
		},
	}
	auth.SetIdentityProvider(idp)

	// Stub a key resolver using NewMapResolver
	auth.SetKeyResolver(NewMapResolver(map[string]string{
		"sk-test":   "disabled_uid",
		"sk-active": "active_uid",
	}))

	// Try AuthenticateKey with a key that maps to disabled_uid
	_, ok := auth.AuthenticateKey(context.Background(), "sk-test")
	require.False(t, ok, "disabled user key authentication should fail")

	// Try AuthenticateKey with a key that maps to active_uid
	auth.ReloadKeys(&config.SecurityConfig{
		APIKeys: []string{"sk-test", "sk-active"},
	})
	uid, ok := auth.AuthenticateKey(context.Background(), "sk-active")
	require.True(t, ok)
	require.Equal(t, "active_uid", uid)

	// 2. AuthenticateRequest (API Key)
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-API-Key", "sk-test")
	_, _, err := auth.AuthenticateRequest(req)
	require.ErrorIs(t, err, ErrUnauthorized, "disabled user request should be unauthorized")

	reqActive := httptest.NewRequest("GET", "/test", nil)
	reqActive.Header.Set("X-API-Key", "sk-active")
	uidRes, _, err := auth.AuthenticateRequest(reqActive)
	require.NoError(t, err)
	require.Equal(t, "active_uid", uidRes)

	// 3. AuthenticateRequest (Cookie)
	ca, err := NewCookieAuth("")
	require.NoError(t, err)
	auth.SetCookieAuth(ca)

	// Setup requests
	w1 := httptest.NewRecorder()
	r1 := httptest.NewRequest("GET", "/", nil)
	err = ca.SetCookie(w1, r1, "disabled_uid")
	require.NoError(t, err)

	cookies1 := w1.Result().Cookies()
	r2 := httptest.NewRequest("GET", "/", nil)
	r2.AddCookie(cookies1[0])

	_, _, err = auth.AuthenticateRequest(r2)
	require.ErrorIs(t, err, ErrUnauthorized, "disabled user cookie authentication should fail")

	w2 := httptest.NewRecorder()
	r3 := httptest.NewRequest("GET", "/", nil)
	err = ca.SetCookie(w2, r3, "active_uid")
	require.NoError(t, err)

	cookies2 := w2.Result().Cookies()
	r4 := httptest.NewRequest("GET", "/", nil)
	r4.AddCookie(cookies2[0])

	uidRes, _, err = auth.AuthenticateRequest(r4)
	require.NoError(t, err)
	require.Equal(t, "active_uid", uidRes)
}

// TestAuthenticateActiveCookie covers the cookie-path disabled-user enforcement
// shared by Hub.HandleHTTP (WS upgrade) and WorkspaceHandlers.requireAuth. A
// valid cookie alone must not grant access to a user disabled by an admin.
func TestAuthenticateActiveCookie(t *testing.T) {
	t.Parallel()

	auth := NewAuthenticator(&config.SecurityConfig{APIKeys: []string{"sk-test"}})
	auth.SetIdentityProvider(&mockIDP{
		LookupFunc: func(ctx context.Context, userID string) (*User, error) {
			switch userID {
			case "active_uid":
				return &User{ID: "active_uid", Status: "active"}, nil
			case "disabled_uid":
				return &User{ID: "disabled_uid", Status: "disabled"}, nil
			}
			return nil, ErrUserNotFound
		},
	})

	ca, err := NewCookieAuth("")
	require.NoError(t, err)
	auth.SetCookieAuth(ca)

	cookieFor := func(uid string) *http.Cookie {
		t.Helper()
		w := httptest.NewRecorder()
		require.NoError(t, ca.SetCookie(w, httptest.NewRequest("GET", "/", nil), uid))
		return w.Result().Cookies()[0]
	}
	reqWithCookie := func(c *http.Cookie) *http.Request {
		r := httptest.NewRequest("GET", "/", nil)
		r.AddCookie(c)
		return r
	}

	// Active user — passes.
	uid, ok := auth.AuthenticateActiveCookie(reqWithCookie(cookieFor("active_uid")))
	require.True(t, ok)
	require.Equal(t, "active_uid", uid)

	// Disabled user — rejected (the fix: cookie alone must not bypass disable).
	_, ok = auth.AuthenticateActiveCookie(reqWithCookie(cookieFor("disabled_uid")))
	require.False(t, ok, "disabled user cookie must be rejected")

	// No cookie — rejected.
	_, ok = auth.AuthenticateActiveCookie(httptest.NewRequest("GET", "/", nil))
	require.False(t, ok)

	// Invalid cookie (right name, bad value) — rejected.
	rBad := httptest.NewRequest("GET", "/", nil)
	rBad.AddCookie(&http.Cookie{Name: cookieName, Value: "garbage"})
	_, ok = auth.AuthenticateActiveCookie(rBad)
	require.False(t, ok)

	// Nil CookieAuth (cookie auth not configured) — rejected.
	authNoCookie := NewAuthenticator(&config.SecurityConfig{APIKeys: []string{"sk-test"}})
	_, ok = authNoCookie.AuthenticateActiveCookie(httptest.NewRequest("GET", "/", nil))
	require.False(t, ok)

	// Dev mode (nil idp) — valid cookie passes without status lookup.
	authDev := NewAuthenticator(&config.SecurityConfig{APIKeys: []string{"sk-test"}})
	authDev.SetCookieAuth(ca)
	uid, ok = authDev.AuthenticateActiveCookie(reqWithCookie(cookieFor("anyone")))
	require.True(t, ok)
	require.Equal(t, "anyone", uid)
}

// ─── Audit event emission ──────────────────────────────────────────────────────

// mockAuditStore is a minimal audit.Store implementation for testing.
// It discards all writes — we only need the collector's channel to accept events.
type mockAuditStore struct{}

func (mockAuditStore) BeginTx(_ context.Context) (audit.Tx, error) { return mockAuditTx{}, nil }
func (mockAuditStore) Query(_ context.Context, _ audit.Query) ([]audit.UserActivity, error) {
	return nil, nil
}
func (mockAuditStore) Stats(_ context.Context, _ audit.Query) (audit.ActivityStats, error) {
	return audit.ActivityStats{ByOutcome: map[string]int64{}, ByPlatform: map[string]int64{}}, nil
}
func (mockAuditStore) QueryAsc(_ context.Context, _ int64, _ int) ([]audit.UserActivity, error) {
	return nil, nil // security tests don't exercise the verifier path
}
func (mockAuditStore) DeleteBefore(_ context.Context, _ time.Time) (int64, error) { return 0, nil }
func (mockAuditStore) SaveCheckpoint(_ context.Context, _ audit.Checkpoint) error { return nil }
func (mockAuditStore) LatestCheckpoint(_ context.Context) (*audit.Checkpoint, error) {
	return nil, nil
}
func (mockAuditStore) ListIdentityLinks(_ context.Context, _ string) ([]audit.IdentityLink, error) {
	return nil, nil
}
func (mockAuditStore) UpsertIdentityLink(_ context.Context, _ audit.IdentityLink) error { return nil }
func (mockAuditStore) DeleteIdentityLink(_ context.Context, _ string) error             { return nil }
func (mockAuditStore) Close() error                                                     { return nil }
func (mockAuditStore) Dialect() dbutil.Dialect                                          { return dbutil.DialectSQLite }

type mockAuditTx struct{}

func (mockAuditTx) Append(_ context.Context, _ *audit.UserActivity) error { return nil }
func (mockAuditTx) AppendBatch(_ context.Context, _ []*audit.UserActivity) error {
	return nil
}
func (mockAuditTx) SaveCheckpoint(_ context.Context, _ audit.Checkpoint) error { return nil }
func (mockAuditTx) TailHash(_ context.Context) (string, error)                 { return "", nil }
func (mockAuditTx) LastRowBefore(_ context.Context, _ time.Time) (int64, string, error) {
	return 0, "", nil
}
func (mockAuditTx) DeleteByIDLEQ(_ context.Context, _ int64) (int64, error) { return 0, nil }
func (mockAuditTx) RowCount(_ context.Context) (int64, error)               { return 0, nil }
func (mockAuditTx) Commit() error                                           { return nil }
func (mockAuditTx) Rollback() error                                         { return nil }

func newTestAuditCollector(t *testing.T) *audit.Collector {
	t.Helper()
	c := audit.NewCollector(mockAuditStore{}, nil, nil, nil, audit.CollectorConfig{
		ChannelCap: 64,
		BatchSize:  10,
	})
	c.Start(context.Background())
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	return c
}

// capturingAuditStore (security test variant) records every appended audit row
// so tests can assert Platform/UserIDType tagging (review I2). The collector
// batches asynchronously, so callers poll via snapshot() + require.Eventually.
type capturingAuditStore struct {
	mu       sync.Mutex
	recorded []audit.UserActivity
}

func (s *capturingAuditStore) BeginTx(context.Context) (audit.Tx, error) {
	return &capturingAuditTx{store: s}, nil
}
func (s *capturingAuditStore) Query(context.Context, audit.Query) ([]audit.UserActivity, error) {
	return nil, nil
}
func (s *capturingAuditStore) Stats(context.Context, audit.Query) (audit.ActivityStats, error) {
	return audit.ActivityStats{ByOutcome: map[string]int64{}, ByPlatform: map[string]int64{}}, nil
}
func (s *capturingAuditStore) QueryAsc(context.Context, int64, int) ([]audit.UserActivity, error) {
	return nil, nil
}
func (s *capturingAuditStore) DeleteBefore(context.Context, time.Time) (int64, error) { return 0, nil }
func (s *capturingAuditStore) SaveCheckpoint(context.Context, audit.Checkpoint) error { return nil }
func (s *capturingAuditStore) LatestCheckpoint(context.Context) (*audit.Checkpoint, error) {
	return nil, nil
}
func (s *capturingAuditStore) ListIdentityLinks(context.Context, string) ([]audit.IdentityLink, error) {
	return nil, nil
}
func (s *capturingAuditStore) UpsertIdentityLink(context.Context, audit.IdentityLink) error {
	return nil
}
func (s *capturingAuditStore) DeleteIdentityLink(context.Context, string) error { return nil }
func (s *capturingAuditStore) Close() error                                     { return nil }
func (s *capturingAuditStore) Dialect() dbutil.Dialect                          { return dbutil.DialectSQLite }
func (s *capturingAuditStore) snapshot() []audit.UserActivity {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]audit.UserActivity, len(s.recorded))
	copy(out, s.recorded)
	return out
}

type capturingAuditTx struct {
	store *capturingAuditStore
}

func (t *capturingAuditTx) Append(_ context.Context, ua *audit.UserActivity) error {
	t.store.mu.Lock()
	t.store.recorded = append(t.store.recorded, *ua)
	t.store.mu.Unlock()
	return nil
}
func (t *capturingAuditTx) AppendBatch(ctx context.Context, uas []*audit.UserActivity) error {
	for _, ua := range uas {
		if err := t.Append(ctx, ua); err != nil {
			return err
		}
	}
	return nil
}
func (t *capturingAuditTx) SaveCheckpoint(context.Context, audit.Checkpoint) error { return nil }
func (t *capturingAuditTx) TailHash(context.Context) (string, error)               { return "", nil }
func (t *capturingAuditTx) LastRowBefore(context.Context, time.Time) (int64, string, error) {
	return 0, "", nil
}
func (t *capturingAuditTx) DeleteByIDLEQ(context.Context, int64) (int64, error) { return 0, nil }
func (t *capturingAuditTx) RowCount(context.Context) (int64, error)             { return 0, nil }
func (t *capturingAuditTx) Commit() error                                       { return nil }
func (t *capturingAuditTx) Rollback() error                                     { return nil }

// newCapturingCollector builds a Collector backed by capturingAuditStore and
// flushes aggressively (BatchSize=1) so the appended row lands quickly.
func newCapturingCollector(t *testing.T, store *capturingAuditStore) *audit.Collector {
	t.Helper()
	c := audit.NewCollector(store, nil, nil, nil, audit.CollectorConfig{
		ChannelCap:    64,
		BatchSize:     1,
		BatchInterval: 5 * time.Millisecond,
	})
	c.Start(context.Background())
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	return c
}

func TestEmitAuthEvent_NilCollector(t *testing.T) {
	t.Parallel()
	auth := NewAuthenticator(&config.SecurityConfig{APIKeys: []string{"k"}})
	auth.emitAuthEvent(audit.ActionAuthDenied, audit.OutcomeDenied, "anonymous",
		audit.PlatformAPI, audit.UserIDTypeAnonymous, "1.2.3.4", "test-agent", "/api/test", "GET")
}

func TestAuthenticateRequest_AuditEvents(t *testing.T) {
	t.Parallel()

	t.Run("dev mode emits auth.login success", func(t *testing.T) {
		t.Parallel()
		ac := newTestAuditCollector(t)
		auth := NewAuthenticator(&config.SecurityConfig{APIKeys: []string{}})
		auth.SetAuditCollector(ac)

		req := httptest.NewRequest("GET", "/api/test", nil)
		req.Header.Set("X-API-Key", "any-key")
		req.RemoteAddr = "192.168.1.1:12345"
		req.Header.Set("User-Agent", "test-ua/1.0")
		uid, _, err := auth.AuthenticateRequest(req)
		require.NoError(t, err)
		require.Equal(t, "anonymous", uid)
		require.Equal(t, int64(1), ac.Enqueued())
	})

	t.Run("denied emits auth.denied", func(t *testing.T) {
		t.Parallel()
		ac := newTestAuditCollector(t)
		auth := NewAuthenticator(&config.SecurityConfig{APIKeys: []string{"secret"}})
		auth.SetAuditCollector(ac)

		req := httptest.NewRequest("GET", "/admin/keys", nil)
		req.RemoteAddr = "10.0.0.1:9999"
		_, _, err := auth.AuthenticateRequest(req)
		require.ErrorIs(t, err, ErrUnauthorized)
		require.Equal(t, int64(1), ac.Enqueued())
	})

	t.Run("invalid key emits auth.apikey_used failure", func(t *testing.T) {
		t.Parallel()
		ac := newTestAuditCollector(t)
		auth := NewAuthenticator(&config.SecurityConfig{APIKeys: []string{"secret"}})
		auth.SetAuditCollector(ac)

		req := httptest.NewRequest("GET", "/api/test", nil)
		req.Header.Set("X-API-Key", "wrong-key")
		req.RemoteAddr = "10.0.0.2:8080"
		_, _, err := auth.AuthenticateRequest(req)
		require.ErrorIs(t, err, ErrUnauthorized)
		require.Equal(t, int64(1), ac.Enqueued())
	})

	t.Run("valid key emits auth.apikey_used success", func(t *testing.T) {
		t.Parallel()
		ac := newTestAuditCollector(t)
		auth := NewAuthenticator(&config.SecurityConfig{APIKeys: []string{"secret"}})
		auth.SetAuditCollector(ac)

		req := httptest.NewRequest("GET", "/api/test", nil)
		req.Header.Set("X-API-Key", "secret")
		req.RemoteAddr = "10.0.0.3:7070"
		uid, _, err := auth.AuthenticateRequest(req)
		require.NoError(t, err)
		require.Equal(t, "api_user", uid)
		require.Equal(t, int64(1), ac.Enqueued())
	})

	t.Run("nil collector does not panic", func(t *testing.T) {
		t.Parallel()
		auth := NewAuthenticator(&config.SecurityConfig{APIKeys: []string{"secret"}})

		req := httptest.NewRequest("GET", "/api/test", nil)
		req.Header.Set("X-API-Key", "secret")
		uid, _, err := auth.AuthenticateRequest(req)
		require.NoError(t, err)
		require.Equal(t, "api_user", uid)
	})

	// ─── I2: webchat cookie path must tag PlatformWebChat + registered ───
	t.Run("cookie auth tags webchat + registered (I2)", func(t *testing.T) {
		t.Parallel()
		store := &capturingAuditStore{}
		ac := newCapturingCollector(t, store)

		auth := NewAuthenticator(&config.SecurityConfig{APIKeys: []string{"sk-test"}})
		ca, err := NewCookieAuth("")
		require.NoError(t, err)
		auth.SetCookieAuth(ca)
		auth.SetAuditCollector(ac)

		// Mint a cookie for a registered user and attach it to the request.
		w := httptest.NewRecorder()
		require.NoError(t, ca.SetCookie(w, httptest.NewRequest("GET", "/api/test", nil), "user-uuid-123"))
		req := httptest.NewRequest("GET", "/api/test", nil)
		req.AddCookie(w.Result().Cookies()[0])
		req.RemoteAddr = "192.168.1.1:12345"

		uid, _, err := auth.AuthenticateRequest(req)
		require.NoError(t, err)
		require.Equal(t, "user-uuid-123", uid)

		// The audit row MUST be tagged webchat + registered (spec §5.4),
		// NOT api + platform (the pre-I2 behavior that mis-tagged every
		// webchat UUID as an opaque platform handle).
		require.Eventually(t, func() bool {
			for _, ua := range store.snapshot() {
				if ua.Action == audit.ActionAuthLogin && ua.Outcome == audit.OutcomeSuccess {
					return ua.Platform == audit.PlatformWebChat &&
						ua.UserIDType == audit.UserIDTypeRegistered &&
						ua.UserID == "user-uuid-123"
				}
			}
			return false
		}, 2*time.Second, 5*time.Millisecond,
			"cookie/webchat auth must be tagged PlatformWebChat + UserIDTypeRegistered (I2)")
	})

	// ─── I2: API-key path with resolver must tag registered ───
	t.Run("api key + resolver tags registered (I2)", func(t *testing.T) {
		t.Parallel()
		store := &capturingAuditStore{}
		ac := newCapturingCollector(t, store)

		auth := NewAuthenticator(&config.SecurityConfig{APIKeys: []string{"sk-alice"}})
		// Resolver maps the key to a real user id → registered.
		auth.SetKeyResolver(NewMapResolver(map[string]string{"sk-alice": "alice-uuid"}))
		auth.SetAuditCollector(ac)

		req := httptest.NewRequest("GET", "/api/test", nil)
		req.Header.Set("X-API-Key", "sk-alice")
		req.RemoteAddr = "10.0.0.3:7070"
		uid, _, err := auth.AuthenticateRequest(req)
		require.NoError(t, err)
		require.Equal(t, "alice-uuid", uid)

		// Resolved API-key auth: PlatformAPI + registered (the uid is a real
		// users.id, not the opaque "api_user" handle).
		require.Eventually(t, func() bool {
			for _, ua := range store.snapshot() {
				if ua.Action == audit.ActionAuthAPIKeyUsed && ua.Outcome == audit.OutcomeSuccess {
					return ua.Platform == audit.PlatformAPI &&
						ua.UserIDType == audit.UserIDTypeRegistered &&
						ua.UserID == "alice-uuid"
				}
			}
			return false
		}, 2*time.Second, 5*time.Millisecond,
			"resolved api-key auth must be tagged PlatformAPI + UserIDTypeRegistered (I2)")
	})

	// ─── I2: API-key path WITHOUT resolver tags platform ───
	t.Run("api key without resolver tags platform (I2)", func(t *testing.T) {
		t.Parallel()
		store := &capturingAuditStore{}
		ac := newCapturingCollector(t, store)

		auth := NewAuthenticator(&config.SecurityConfig{APIKeys: []string{"secret"}})
		// No resolver → uid is the opaque "api_user" handle → platform.
		auth.SetAuditCollector(ac)

		req := httptest.NewRequest("GET", "/api/test", nil)
		req.Header.Set("X-API-Key", "secret")
		req.RemoteAddr = "10.0.0.4:7070"
		uid, _, err := auth.AuthenticateRequest(req)
		require.NoError(t, err)
		require.Equal(t, "api_user", uid)

		require.Eventually(t, func() bool {
			for _, ua := range store.snapshot() {
				if ua.Action == audit.ActionAuthAPIKeyUsed && ua.Outcome == audit.OutcomeSuccess {
					return ua.Platform == audit.PlatformAPI &&
						ua.UserIDType == audit.UserIDTypePlatform &&
						ua.UserID == "api_user"
				}
			}
			return false
		}, 2*time.Second, 5*time.Millisecond,
			"unresolved api-key auth must be tagged PlatformAPI + UserIDTypePlatform (I2)")
	})
}
