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

	pgConfig := getPostgreConfig(pluginConfig)

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

	pgConfig := getPostgreConfig(pluginConfig)

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
