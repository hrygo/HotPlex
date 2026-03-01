package chatapps

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/hrygo/hotplex/chatapps/base"
	"github.com/hrygo/hotplex/event"
	"github.com/hrygo/hotplex/types"
)

// =============================================================================
// Interface Compliance Tests
// =============================================================================

// TestEngineInterface_Compliance verifies Engine interface compliance at compile time
func TestEngineInterface_Compliance(t *testing.T) {
	var _ Engine = (*MockEngine)(nil)
}

// TestMessageOperationsInterface_Compliance verifies MessageOperations compliance
func TestMessageOperationsInterface_Compliance(t *testing.T) {
	var _ MessageOperations = (*MockMessageOperations)(nil)
}

// TestSessionOperationsInterface_Compliance verifies SessionOperations compliance
func TestSessionOperationsInterface_Compliance(t *testing.T) {
	var _ SessionOperations = (*MockSessionOperations)(nil)
}

// =============================================================================
// Mock Implementations for Testing
// =============================================================================

// MockEngine implements Engine interface for testing
type MockEngine struct {
	ExecuteFunc              func(ctx context.Context, cfg *types.Config, prompt string, callback event.Callback) error
	GetSessionFunc           func(sessionID string) (Session, bool)
	CloseFunc                func() error
	GetSessionStatsFunc      func(sessionID string) *SessionStats
	ValidateConfigFunc       func(cfg *types.Config) error
	StopSessionFunc          func(sessionID string, reason string) error
	ResetSessionProviderFunc func(sessionID string)
	SetDangerAllowPathsFunc  func(paths []string)
	SetDangerBypassEnabledFunc func(token string, enabled bool) error
	SetAllowedToolsFunc      func(tools []string)
	SetDisallowedToolsFunc   func(tools []string)
	GetAllowedToolsFunc      func() []string
	GetDisallowedToolsFunc   func() []string
}

func (m *MockEngine) Execute(ctx context.Context, cfg *types.Config, prompt string, callback event.Callback) error {
	if m.ExecuteFunc != nil {
		return m.ExecuteFunc(ctx, cfg, prompt, callback)
	}
	return nil
}

func (m *MockEngine) GetSession(sessionID string) (Session, bool) {
	if m.GetSessionFunc != nil {
		return m.GetSessionFunc(sessionID)
	}
	return nil, false
}

func (m *MockEngine) Close() error {
	if m.CloseFunc != nil {
		return m.CloseFunc()
	}
	return nil
}

func (m *MockEngine) GetSessionStats(sessionID string) *SessionStats {
	if m.GetSessionStatsFunc != nil {
		return m.GetSessionStatsFunc(sessionID)
	}
	return nil
}

func (m *MockEngine) ValidateConfig(cfg *types.Config) error {
	if m.ValidateConfigFunc != nil {
		return m.ValidateConfigFunc(cfg)
	}
	return nil
}

func (m *MockEngine) StopSession(sessionID string, reason string) error {
	if m.StopSessionFunc != nil {
		return m.StopSessionFunc(sessionID, reason)
	}
	return nil
}

func (m *MockEngine) ResetSessionProvider(sessionID string) {
	if m.ResetSessionProviderFunc != nil {
		m.ResetSessionProviderFunc(sessionID)
	}
}

func (m *MockEngine) SetDangerAllowPaths(paths []string) {
	if m.SetDangerAllowPathsFunc != nil {
		m.SetDangerAllowPathsFunc(paths)
	}
}

func (m *MockEngine) SetDangerBypassEnabled(token string, enabled bool) error {
	if m.SetDangerBypassEnabledFunc != nil {
		return m.SetDangerBypassEnabledFunc(token, enabled)
	}
	return nil
}

func (m *MockEngine) SetAllowedTools(tools []string) {
	if m.SetAllowedToolsFunc != nil {
		m.SetAllowedToolsFunc(tools)
	}
}

func (m *MockEngine) SetDisallowedTools(tools []string) {
	if m.SetDisallowedToolsFunc != nil {
		m.SetDisallowedToolsFunc(tools)
	}
}

func (m *MockEngine) GetAllowedTools() []string {
	if m.GetAllowedToolsFunc != nil {
		return m.GetAllowedToolsFunc()
	}
	return nil
}

func (m *MockEngine) GetDisallowedTools() []string {
	if m.GetDisallowedToolsFunc != nil {
		return m.GetDisallowedToolsFunc()
	}
	return nil
}

// MockMessageOperations implements MessageOperations for testing
type MockMessageOperations struct {
	DeleteMessageFunc   func(ctx context.Context, channelID, messageTS string) error
	AddReactionFunc     func(ctx context.Context, reaction base.Reaction) error
	RemoveReactionFunc  func(ctx context.Context, reaction base.Reaction) error
	UpdateMessageFunc   func(ctx context.Context, channelID, messageTS string, msg *base.ChatMessage) error
}

func (m *MockMessageOperations) DeleteMessage(ctx context.Context, channelID, messageTS string) error {
	if m.DeleteMessageFunc != nil {
		return m.DeleteMessageFunc(ctx, channelID, messageTS)
	}
	return nil
}

func (m *MockMessageOperations) AddReaction(ctx context.Context, reaction base.Reaction) error {
	if m.AddReactionFunc != nil {
		return m.AddReactionFunc(ctx, reaction)
	}
	return nil
}

func (m *MockMessageOperations) RemoveReaction(ctx context.Context, reaction base.Reaction) error {
	if m.RemoveReactionFunc != nil {
		return m.RemoveReactionFunc(ctx, reaction)
	}
	return nil
}

func (m *MockMessageOperations) UpdateMessage(ctx context.Context, channelID, messageTS string, msg *base.ChatMessage) error {
	if m.UpdateMessageFunc != nil {
		return m.UpdateMessageFunc(ctx, channelID, messageTS, msg)
	}
	return nil
}

// MockSessionOperations implements SessionOperations for testing
type MockSessionOperations struct {
	GetSessionFunc              func(key string) (*base.Session, bool)
	FindSessionByUserAndChannelFunc func(userID, channelID string) *base.Session
}

func (m *MockSessionOperations) GetSession(key string) (*base.Session, bool) {
	if m.GetSessionFunc != nil {
		return m.GetSessionFunc(key)
	}
	return nil, false
}

func (m *MockSessionOperations) FindSessionByUserAndChannel(userID, channelID string) *base.Session {
	if m.FindSessionByUserAndChannelFunc != nil {
		return m.FindSessionByUserAndChannelFunc(userID, channelID)
	}
	return nil
}

// =============================================================================
// Dependency Injection Tests
// =============================================================================

// TestStreamCallback_WithNilMessageOps verifies graceful handling of nil messageOps
func TestStreamCallback_WithNilMessageOps(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)
	adapters := NewAdapterManager(logger)

	callback := NewStreamCallback(
		ctx, "test-session", "test-platform",
		adapters, logger, nil,
		nil, nil,
	)

	if callback == nil {
		t.Fatal("Expected non-nil callback")
	}

	if callback.messageOps != nil {
		t.Error("Expected messageOps to be nil")
	}
}

// TestStreamCallback_WithMessageOps verifies callback with injected messageOps
func TestStreamCallback_WithMessageOps(t *testing.T) {
	ctx := context.Background()
	logger := newTestLogger(t)
	adapters := NewAdapterManager(logger)

	mockOps := &MockMessageOperations{
		DeleteMessageFunc: func(ctx context.Context, channelID, messageTS string) error {
			return nil
		},
	}

	callback := NewStreamCallback(
		ctx, "test-session", "slack",
		adapters, logger, map[string]any{},
		mockOps,
		nil,
	)

	if callback == nil {
		t.Fatal("Expected non-nil callback")
	}

	if callback.messageOps == nil {
		t.Error("Expected messageOps to be injected")
	}
}

// TestAdapterManager_GetMessageOperations verifies interface retrieval
func TestAdapterManager_GetMessageOperations(t *testing.T) {
	logger := newTestLogger(t)
	manager := NewAdapterManager(logger)

	ops := manager.GetMessageOperations("nonexistent")
	if ops != nil {
		t.Error("Expected nil for non-existent platform")
	}
}

// TestAdapterManager_GetSessionOperations verifies session operations retrieval
func TestAdapterManager_GetSessionOperations(t *testing.T) {
	logger := newTestLogger(t)
	manager := NewAdapterManager(logger)

	ops := manager.GetSessionOperations("nonexistent")
	if ops != nil {
		t.Error("Expected nil for non-existent platform")
	}
}

// =============================================================================
// Helper Functions
// =============================================================================

func newTestLogger(t *testing.T) *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
