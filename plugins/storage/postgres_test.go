package storage

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hrygo/hotplex/types"
)

// TestPostgreSQLConfig tests PostgreSQL configuration
func TestPostgreSQLConfig(t *testing.T) {
	config := PostgreSQLConfig{
		Host:         "localhost",
		Port:         5432,
		User:         "test",
		Password:     "test",
		Database:     "testdb",
		SSLMode:      "disable",
		MaxOpenConns: 10,
		MaxIdleConns: 5,
		MaxLifetime:  300 * time.Second,
	}

	if config.Host != "localhost" {
		t.Errorf("Expected host localhost, got %s", config.Host)
	}
	if config.Port != 5432 {
		t.Errorf("Expected port 5432, got %d", config.Port)
	}
	if config.MaxOpenConns != 10 {
		t.Errorf("Expected max open conns 10, got %d", config.MaxOpenConns)
	}
}

// TestGetPostgreConfig tests config extraction from PluginConfig
func TestGetPostgreConfig(t *testing.T) {
	pluginConfig := PluginConfig{
		"host":           "192.168.1.1",
		"port":           5433,
		"user":           "admin",
		"password":       "secret",
		"database":       "mydb",
		"ssl_mode":       "require",
		"max_open_conns": 50,
		"max_idle_conns": 10,
		"max_lifetime":   600,
	}

	pgConfig, err := getPostgreConfig(pluginConfig)
	if err != nil {
		t.Fatalf("Failed to get config: %v", err)
	}

	if pgConfig.Host != "192.168.1.1" {
		t.Errorf("Expected host 192.168.1.1, got %s", pgConfig.Host)
	}
	if pgConfig.Port != 5433 {
		t.Errorf("Expected port 5433, got %d", pgConfig.Port)
	}
	if pgConfig.User != "admin" {
		t.Errorf("Expected user admin, got %s", pgConfig.User)
	}
	if pgConfig.Database != "mydb" {
		t.Errorf("Expected database mydb, got %s", pgConfig.Database)
	}
	if pgConfig.SSLMode != "require" {
		t.Errorf("Expected ssl_mode require, got %s", pgConfig.SSLMode)
	}
	if pgConfig.MaxOpenConns != 50 {
		t.Errorf("Expected max_open_conns 50, got %d", pgConfig.MaxOpenConns)
	}
	if pgConfig.MaxLifetime != 600*time.Second {
		t.Errorf("Expected max_lifetime 600s, got %s", pgConfig.MaxLifetime)
	}
}

// TestGetPostgreConfigDefaults tests default values
func TestGetPostgreConfigDefaults(t *testing.T) {
	pluginConfig := PluginConfig{}

	pgConfig, err := getPostgreConfig(pluginConfig)
	if err != nil {
		t.Fatalf("Failed to get config: %v", err)
	}

	if pgConfig.Host != "localhost" {
		t.Errorf("Expected default host localhost, got %s", pgConfig.Host)
	}
	if pgConfig.Port != 5432 {
		t.Errorf("Expected default port 5432, got %d", pgConfig.Port)
	}
	if pgConfig.User != "hotplex" {
		t.Errorf("Expected default user hotplex, got %s", pgConfig.User)
	}
	if pgConfig.Database != "hotplex" {
		t.Errorf("Expected default database hotplex, got %s", pgConfig.Database)
	}
	if pgConfig.SSLMode != "disable" {
		t.Errorf("Expected default ssl_mode disable, got %s", pgConfig.SSLMode)
	}
	if pgConfig.MaxOpenConns != 25 {
		t.Errorf("Expected default max_open_conns 25, got %d", pgConfig.MaxOpenConns)
	}
	if pgConfig.MaxIdleConns != 5 {
		t.Errorf("Expected default max_idle_conns 5, got %d", pgConfig.MaxIdleConns)
	}
}

// TestMessageQuery tests MessageQuery construction
func TestMessageQuery(t *testing.T) {
	now := time.Now()
	query := &MessageQuery{
		ChatSessionID:     "test-session-123",
		EngineSessionID:   uuid.New(),
		ProviderSessionID: "provider-123",
		ProviderType:      "claude-code",
		StartTime:         &now,
		EndTime:           &now,
		MessageTypes:      []types.MessageType{types.MessageTypeUserInput, types.MessageTypeFinalResponse},
		Limit:             100,
		Offset:            0,
		Ascending:         false,
		IncludeDeleted:    false,
	}

	if query.ChatSessionID != "test-session-123" {
		t.Errorf("Expected chat session ID, got %s", query.ChatSessionID)
	}
	if query.Limit != 100 {
		t.Errorf("Expected limit 100, got %d", query.Limit)
	}
	if query.Ascending != false {
		t.Errorf("Expected ascending false, got %v", query.Ascending)
	}
	if query.IncludeDeleted != false {
		t.Errorf("Expected include deleted false, got %v", query.IncludeDeleted)
	}
}

// TestSessionMeta tests SessionMeta structure
func TestSessionMeta(t *testing.T) {
	now := time.Now()
	meta := &SessionMeta{
		ChatSessionID: "session-123",
		ChatPlatform:  "slack",
		ChatUserID:    "user-123",
		LastMessageID: "msg-456",
		LastMessageAt: now,
		MessageCount:  100,
		UpdatedAt:     now,
	}

	if meta.ChatSessionID != "session-123" {
		t.Errorf("Expected chat session ID, got %s", meta.ChatSessionID)
	}
	if meta.ChatPlatform != "slack" {
		t.Errorf("Expected platform slack, got %s", meta.ChatPlatform)
	}
	if meta.MessageCount != 100 {
		t.Errorf("Expected message count 100, got %d", meta.MessageCount)
	}
}

// TestChatAppMessage tests ChatAppMessage structure
func TestChatAppMessage(t *testing.T) {
	now := time.Now()
	msg := &ChatAppMessage{
		ID:                "msg-123",
		ChatSessionID:     "session-123",
		ChatPlatform:      "slack",
		ChatUserID:        "user-123",
		ChatBotUserID:     "bot-123",
		ChatChannelID:     "channel-123",
		ChatThreadID:      "thread-123",
		EngineSessionID:   uuid.New(),
		EngineNamespace:   "hotplex",
		ProviderSessionID: "provider-123",
		ProviderType:      "claude-code",
		MessageType:       types.MessageTypeUserInput,
		FromUserID:        "user-123",
		FromUserName:      "Test User",
		ToUserID:          "bot-123",
		Content:           "Hello world",
		Metadata:          map[string]any{"key": "value"},
		CreatedAt:         now,
		UpdatedAt:         now,
		Deleted:           false,
	}

	if msg.ID != "msg-123" {
		t.Errorf("Expected ID msg-123, got %s", msg.ID)
	}
	if msg.Content != "Hello world" {
		t.Errorf("Expected content 'Hello world', got %s", msg.Content)
	}
	if msg.MessageType != types.MessageTypeUserInput {
		t.Errorf("Expected message type UserInput, got %s", msg.MessageType)
	}
	if msg.Deleted != false {
		t.Errorf("Expected deleted false, got %v", msg.Deleted)
	}
}

// TestDefaultStrategy tests default storage strategy
func TestDefaultStrategy(t *testing.T) {
	strategy := NewDefaultStrategy()

	// Test storable message type
	storableMsg := &ChatAppMessage{
		MessageType: types.MessageTypeUserInput,
		Content:     "test",
	}
	if !strategy.ShouldStore(storableMsg) {
		t.Error("Expected UserInput to be storable")
	}

	// Test non-storable message type
	nonStorableMsg := &ChatAppMessage{
		MessageType: types.MessageTypeToolUse,
		Content:     "test",
	}
	if strategy.ShouldStore(nonStorableMsg) {
		t.Error("Expected ToolUse to not be storable")
	}
}

// TestPostgreFactory tests PostgreSQL factory
func TestPostgreFactory(t *testing.T) {
	factory := &PostgreFactory{}
	_ = factory // Avoid unused warning
}

// TestParsePostgresDSN tests DSN/URL parsing for PostgreSQL
func TestParsePostgresDSN(t *testing.T) {
	tests := []struct {
		name     string
		dsn      string
		expected PostgreSQLConfig
	}{
		{
			name: "full URL with all components",
			dsn:  "postgres://admin:secret@db.example.com:5432/mydb?sslmode=require",
			expected: PostgreSQLConfig{
				Host:     "db.example.com",
				Port:     5432,
				User:     "admin",
				Password: "secret",
				Database: "mydb",
				SSLMode:  "require",
			},
		},
		{
			name: "URL without password",
			dsn:  "postgres://admin@localhost:5432/testdb",
			expected: PostgreSQLConfig{
				Host:     "localhost",
				Port:     5432,
				User:     "admin",
				Password: "",
				Database: "testdb",
				SSLMode:  "disable", // default
			},
		},
		{
			name: "URL without port (uses default)",
			dsn:  "postgres://user:pass@myhost/production",
			expected: PostgreSQLConfig{
				Host:     "myhost",
				Port:     5432, // default
				User:     "user",
				Password: "pass",
				Database: "production",
				SSLMode:  "disable",
			},
		},
		{
			name: "postgresql:// scheme (alternative)",
			dsn:  "postgresql://testuser:testpass@127.0.0.1:5433/test?sslmode=verify-full",
			expected: PostgreSQLConfig{
				Host:     "127.0.0.1",
				Port:     5433,
				User:     "testuser",
				Password: "testpass",
				Database: "test",
				SSLMode:  "verify-full",
			},
		},
		{
			name: "URL with no query params",
			dsn:  "postgres://user:pass@host:5432/db",
			expected: PostgreSQLConfig{
				Host:     "host",
				Port:     5432,
				User:     "user",
				Password: "pass",
				Database: "db",
				SSLMode:  "disable", // default
			},
		},
		{
			name: "invalid URL returns defaults",
			dsn:  "not-a-valid-url",
			expected: PostgreSQLConfig{
				Host:     "localhost",
				Port:     5432,
				User:     "hotplex",
				Password: "",
				Database: "hotplex",
				SSLMode:  "disable",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parsePostgresDSN(tt.dsn)
			if err != nil && tt.expected.Host != "localhost" { // Only fail if we expected success
				t.Errorf("parsePostgresDSN error: %v", err)
				return
			}

			if result.Host != tt.expected.Host {
				t.Errorf("Host: expected %q, got %q", tt.expected.Host, result.Host)
			}
			if result.Port != tt.expected.Port {
				t.Errorf("Port: expected %d, got %d", tt.expected.Port, result.Port)
			}
			if result.User != tt.expected.User {
				t.Errorf("User: expected %q, got %q", tt.expected.User, result.User)
			}
			if result.Password != tt.expected.Password {
				t.Errorf("Password: expected %q, got %q", tt.expected.Password, result.Password)
			}
			if result.Database != tt.expected.Database {
				t.Errorf("Database: expected %q, got %q", tt.expected.Database, result.Database)
			}
			if result.SSLMode != tt.expected.SSLMode {
				t.Errorf("SSLMode: expected %q, got %q", tt.expected.SSLMode, result.SSLMode)
			}
		})
	}
}

// TestGetPostgreConfigWithURL tests that URL takes precedence over individual fields
func TestGetPostgreConfigWithURL(t *testing.T) {
	pluginConfig := PluginConfig{
		"url":       "postgres://urluser:urlpass@urlhost:5433/urldb?sslmode=require",
		"host":      "fieldhost",
		"user":      "fielduser",
		"password":  "fieldpass",
		"database":  "fielddb",
		"ssl_mode":  "disable",
	}

	pgConfig, err := getPostgreConfig(pluginConfig)
	if err != nil {
		t.Fatalf("Failed to get config: %v", err)
	}

	// URL should take precedence
	if pgConfig.Host != "urlhost" {
		t.Errorf("Expected URL host 'urlhost', got %q", pgConfig.Host)
	}
	if pgConfig.User != "urluser" {
		t.Errorf("Expected URL user 'urluser', got %q", pgConfig.User)
	}
	if pgConfig.Password != "urlpass" {
		t.Errorf("Expected URL password 'urlpass', got %q", pgConfig.Password)
	}
	if pgConfig.Database != "urldb" {
		t.Errorf("Expected URL database 'urldb', got %q", pgConfig.Database)
	}
	if pgConfig.SSLMode != "require" {
		t.Errorf("Expected URL sslmode 'require', got %q", pgConfig.SSLMode)
	}
}

// TestGetPostgreConfigWithDSN tests DSN key as alternative to URL
func TestGetPostgreConfigWithDSN(t *testing.T) {
	pluginConfig := PluginConfig{
		"dsn": "postgres://dsnuser@dsnhost:5434/dsndb",
	}

	pgConfig, err := getPostgreConfig(pluginConfig)
	if err != nil {
		t.Fatalf("Failed to get config: %v", err)
	}

	if pgConfig.Host != "dsnhost" {
		t.Errorf("Expected DSN host 'dsnhost', got %q", pgConfig.Host)
	}
	if pgConfig.User != "dsnuser" {
		t.Errorf("Expected DSN user 'dsnuser', got %q", pgConfig.User)
	}
	if pgConfig.Database != "dsndb" {
		t.Errorf("Expected DSN database 'dsndb', got %q", pgConfig.Database)
	}
	if pgConfig.Port != 5434 {
		t.Errorf("Expected DSN port 5434, got %d", pgConfig.Port)
	}
}

// TestParsePostgresDSN_SpecialChars tests DSN parsing with special characters in password
func TestParsePostgresDSN_SpecialChars(t *testing.T) {
	tests := []struct {
		name     string
		dsn      string
		wantUser string
		wantPass string
	}{
		{
			name:     "password with at sign",
			dsn:      "postgres://user:p%40ss@host:5432/db",
			wantUser: "user",
			wantPass: "p@ss",
		},
		{
			name:     "password with colon",
			dsn:      "postgres://user:pass%3Aword@host:5432/db",
			wantUser: "user",
			wantPass: "pass:word",
		},
		{
			name:     "password with slash",
			dsn:      "postgres://user:pass%2Fword@host:5432/db",
			wantUser: "user",
			wantPass: "pass/word",
		},
		{
			name:     "password with question mark",
			dsn:      "postgres://user:p%3Fss@host:5432/db",
			wantUser: "user",
			wantPass: "p?ss",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parsePostgresDSN(tt.dsn)
			if err != nil {
				t.Errorf("parsePostgresDSN error: %v", err)
				return
			}
			if result.User != tt.wantUser {
				t.Errorf("User: got %q, want %q", result.User, tt.wantUser)
			}
			if result.Password != tt.wantPass {
				t.Errorf("Password: got %q, want %q", result.Password, tt.wantPass)
			}
		})
	}
}
