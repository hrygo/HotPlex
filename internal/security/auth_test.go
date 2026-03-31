package security

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"hotplex-worker/internal/config"
	"hotplex-worker/pkg/events"

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
		name        string
		apiKeys     []string
		headerName  string
		requestKey  string
		wantUserID  string
		wantErr     bool
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

			userID, err := auth.AuthenticateRequest(req)
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

func TestAuthenticateEnvelope(t *testing.T) {
	t.Parallel()

	auth := NewAuthenticator(&config.SecurityConfig{APIKeys: []string{"test"}})

	tests := []struct {
		name    string
		env     *events.Envelope
		wantErr bool
	}{
		{
			name:    "empty session id",
			env:     &events.Envelope{SessionID: ""},
			wantErr: true,
		},
		{
			name:    "valid session id",
			env:     &events.Envelope{SessionID: "sess_123"},
			wantErr: false,
		},
		{
			name:    "valid envelope with data",
			env:     &events.Envelope{SessionID: "sess_abc", Seq: 42},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := auth.AuthenticateEnvelope(tt.env)
			if tt.wantErr {
				require.Error(t, err)
				require.Equal(t, ErrUnauthorized, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestMiddleware(t *testing.T) {
	t.Parallel()

	cfg := &config.SecurityConfig{APIKeys: []string{"secret123"}}
	auth := NewAuthenticator(cfg)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
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

	// Dev mode: no keys configured
	cfg := &config.SecurityConfig{APIKeys: []string{}}
	auth := NewAuthenticator(cfg)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// In dev mode (no keys configured), any request without API key still gets 401
	// because AuthenticateRequest checks if key header exists
	req := httptest.NewRequest("GET", "/protected", nil)

	rec := httptest.NewRecorder()
	auth.Middleware(handler).ServeHTTP(rec, req)

	// Dev mode allows access with any key, but still requires the header
	// Since no header is provided, it should be unauthorized
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

	// Context with wrong type value
	ctx := context.WithValue(context.Background(), claimsKey, "not-claims")

	claims, ok := ClaimsFrom(ctx)
	require.False(t, ok)
	require.Equal(t, Claims{}, claims)
}

// ─── InputValidator ───────────────────────────────────────────────────────────

func TestNewInputValidator(t *testing.T) {
	t.Parallel()

	cfg := &config.WorkerConfig{}
	v := NewInputValidator(cfg)

	require.NotNil(t, v)
	require.Equal(t, 1<<20, v.maxLen) // 1MB
}

func TestInputValidator_ValidateInput(t *testing.T) {
	t.Parallel()

	v := NewInputValidator(&config.WorkerConfig{})

	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "valid input",
			input:   "hello world",
			wantErr: false,
		},
		{
			name:    "empty input",
			input:   "",
			wantErr: false,
		},
		{
			name:    "null byte rejected",
			input:   "hello\x00world",
			wantErr: true,
		},
		{
			name:    "multiple null bytes",
			input:   "\x00\x00\x00",
			wantErr: true,
		},
		{
			name:    "unicode allowed",
			input:   "hello 世界 🌍",
			wantErr: false,
		},
		{
			name:    "exactly max length",
			input:   string(make([]byte, 1<<20)),
			wantErr: true, // All-zero bytes includes null byte
		},
		{
			name:    "exceeds max length",
			input:   string(make([]byte, (1<<20)+1)),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := v.ValidateInput(tt.input)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// ─── EnvValidator ─────────────────────────────────────────────────────────────

func TestNewEnvValidator(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		whitelist []string
		wantLen   int
	}{
		{
			name:      "empty whitelist",
			whitelist: []string{},
			wantLen:   0,
		},
		{
			name:      "single key",
			whitelist: []string{"HOME"},
			wantLen:   1,
		},
		{
			name:      "multiple keys",
			whitelist: []string{"HOME", "PATH", "USER"},
			wantLen:   3,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			v := NewEnvValidator(tt.whitelist)
			require.NotNil(t, v)
			require.Equal(t, tt.wantLen, len(v.whitelist))
		})
	}
}

func TestEnvValidator_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		whitelist []string
		env       map[string]string
		want      map[string]string
	}{
		{
			name:      "empty whitelist allows all",
			whitelist: []string{},
			env: map[string]string{
				"HOME":   "/home/user",
				"SECRET": "value",
			},
			want: map[string]string{
				"HOME":   "/home/user",
				"SECRET": "value",
			},
		},
		{
			name:      "filter to whitelist",
			whitelist: []string{"HOME", "PATH"},
			env: map[string]string{
				"HOME":   "/home/user",
				"PATH":   "/usr/bin",
				"SECRET": "should-be-removed",
			},
			want: map[string]string{
				"HOME": "/home/user",
				"PATH": "/usr/bin",
			},
		},
		{
			name:      "no matching keys",
			whitelist: []string{"SAFE_VAR"},
			env: map[string]string{
				"SECRET1": "value1",
				"SECRET2": "value2",
			},
			want: map[string]string{},
		},
		{
			name:      "nil env",
			whitelist: []string{"HOME"},
			env:       nil,
			want:      map[string]string{},
		},
		{
			name:      "all keys whitelisted",
			whitelist: []string{"HOME", "PATH", "USER"},
			env: map[string]string{
				"HOME": "/home/user",
				"PATH": "/usr/bin",
				"USER": "testuser",
			},
			want: map[string]string{
				"HOME": "/home/user",
				"PATH": "/usr/bin",
				"USER": "testuser",
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			v := NewEnvValidator(tt.whitelist)
			result := v.Validate(tt.env)
			require.Equal(t, tt.want, result)
		})
	}
}
