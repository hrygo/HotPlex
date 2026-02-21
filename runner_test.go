package hotplex

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"
)

func TestEngine_ValidateConfig(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	engine := &Engine{
		opts:           EngineOptions{Namespace: "test"},
		logger:         logger,
		dangerDetector: NewDetector(logger),
	}

	tests := []struct {
		name      string
		config    *Config
		wantErr   bool
		errSubstr string
	}{
		{
			name:    "Valid config",
			config:  &Config{WorkDir: "/tmp", SessionID: "test-session"},
			wantErr: false,
		},
		{
			name:      "Missing WorkDir",
			config:    &Config{SessionID: "test-session"},
			wantErr:   true,
			errSubstr: "work_dir is required",
		},
		{
			name:      "Missing SessionID",
			config:    &Config{WorkDir: "/tmp"},
			wantErr:   true,
			errSubstr: "session_id is required",
		},
		{
			name:      "Path traversal with ..",
			config:    &Config{WorkDir: "/tmp/../etc", SessionID: "test-session"},
			wantErr:   true,
			errSubstr: "path traversal",
		},
		{
			name:    "Valid path with . (current dir)",
			config:  &Config{WorkDir: ".", SessionID: "test-session"},
			wantErr: false,
		},
		{
			name:    "Valid nested path",
			config:  &Config{WorkDir: "/tmp/hotplex/sessions/test", SessionID: "test-session"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := engine.ValidateConfig(tt.config)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ValidateConfig() expected error containing %q, got nil", tt.errSubstr)
				} else if tt.errSubstr != "" && !contains(err.Error(), tt.errSubstr) {
					t.Errorf("ValidateConfig() error = %v, want error containing %q", err, tt.errSubstr)
				}
			} else {
				if err != nil {
					t.Errorf("ValidateConfig() unexpected error: %v", err)
				}
			}
		})
	}
}

func TestEngine_ValidateConfig_CleansPath(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	engine := &Engine{
		opts:           EngineOptions{Namespace: "test"},
		logger:         logger,
		dangerDetector: NewDetector(logger),
	}

	// Test that path gets cleaned
	config := &Config{WorkDir: "/tmp/./hotplex//sessions/", SessionID: "test"}
	err := engine.ValidateConfig(config)
	if err != nil {
		t.Fatalf("ValidateConfig() unexpected error: %v", err)
	}

	// WorkDir should be cleaned (no double slashes, no ./ segments)
	expected := "/tmp/hotplex/sessions"
	if config.WorkDir != expected {
		t.Errorf("WorkDir not cleaned: got %q, want %q", config.WorkDir, expected)
	}
}

func TestEngine_GetSessionStats_Nil(t *testing.T) {
	engine := &Engine{
		opts:   EngineOptions{Namespace: "test"},
		logger: slog.New(slog.NewTextHandler(os.Stdout, nil)),
	}

	stats := engine.GetSessionStats()
	if stats != nil {
		t.Errorf("GetSessionStats() on new engine should return nil, got %v", stats)
	}
}

func TestEngine_Execute_DangerBlocked(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	engine := &Engine{
		opts:           EngineOptions{Namespace: "test", Timeout: time.Minute},
		logger:         logger,
		dangerDetector: NewDetector(logger),
	}

	// Dangerous prompt should be blocked before any execution
	ctx := context.Background()
	cfg := &Config{WorkDir: "/tmp", SessionID: "test-session"}

	err := engine.Execute(ctx, cfg, "rm -rf /", nil)
	if err != ErrDangerBlocked {
		t.Errorf("Execute() with dangerous prompt: got err=%v, want ErrDangerBlocked", err)
	}
}

func TestEngine_Execute_InvalidConfig(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	engine := &Engine{
		opts:           EngineOptions{Namespace: "test", Timeout: time.Minute},
		logger:         logger,
		dangerDetector: NewDetector(logger),
	}

	ctx := context.Background()

	// Missing WorkDir
	err := engine.Execute(ctx, &Config{SessionID: "test"}, "safe prompt", nil)
	if err == nil {
		t.Error("Execute() with missing WorkDir should fail")
	}

	// Missing SessionID
	err = engine.Execute(ctx, &Config{WorkDir: "/tmp"}, "safe prompt", nil)
	if err == nil {
		t.Error("Execute() with missing SessionID should fail")
	}
}

func TestEngine_Execute_DangerBlockEvent(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	engine := &Engine{
		opts:           EngineOptions{Namespace: "test", Timeout: time.Minute},
		logger:         logger,
		dangerDetector: NewDetector(logger),
	}

	ctx := context.Background()
	cfg := &Config{WorkDir: "/tmp", SessionID: "test"}

	var dangerBlockReceived bool
	cb := func(eventType string, data any) error {
		if eventType == "danger_block" {
			dangerBlockReceived = true
		}
		return nil
	}

	err := engine.Execute(ctx, cfg, "rm -rf /", cb)
	if err != ErrDangerBlocked {
		t.Errorf("Execute() error = %v, want ErrDangerBlocked", err)
	}
	if !dangerBlockReceived {
		t.Error("danger_block event should be sent")
	}
}

func TestEngine_Execute_ThinkingEvent(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	// Create mock manager that returns error on GetOrCreateSession
	mockMgr := &mockFailingSessionManager{}

	engine := &Engine{
		opts:           EngineOptions{Namespace: "test", Timeout: time.Minute},
		logger:         logger,
		dangerDetector: NewDetector(logger),
		manager:        mockMgr,
	}

	ctx := context.Background()
	cfg := &Config{WorkDir: "/tmp", SessionID: "test"}

	var thinkingReceived bool
	cb := func(eventType string, data any) error {
		if eventType == "thinking" {
			thinkingReceived = true
		}
		return nil
	}

	// This will fail at executeWithMultiplex, but thinking event should be sent first
	_ = engine.Execute(ctx, cfg, "safe prompt", cb)

	if !thinkingReceived {
		t.Error("thinking event should be sent")
	}
}

func TestEngine_Execute_MkdirAllFailure(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	engine := &Engine{
		opts:           EngineOptions{Namespace: "test", Timeout: time.Minute},
		logger:         logger,
		dangerDetector: NewDetector(logger),
	}

	ctx := context.Background()

	// Try to create a directory in a path that requires permission
	// This test may pass if running as root, so we use an invalid path
	cfg := &Config{WorkDir: "/nonexistent\x00invalid/path", SessionID: "test"}

	err := engine.Execute(ctx, cfg, "safe prompt", nil)
	if err == nil {
		t.Error("Execute() with invalid WorkDir should fail")
	}
}

// mockFailingSessionManager always returns error on GetOrCreateSession
type mockFailingSessionManager struct{}

func (m *mockFailingSessionManager) GetOrCreateSession(ctx context.Context, sessionID string, cfg Config) (*Session, error) {
	return nil, fmt.Errorf("mock error: session creation failed")
}

func (m *mockFailingSessionManager) GetSession(sessionID string) (*Session, bool) {
	return nil, false
}

func (m *mockFailingSessionManager) TerminateSession(sessionID string) error {
	return nil
}

func (m *mockFailingSessionManager) ListActiveSessions() []*Session {
	return nil
}

func (m *mockFailingSessionManager) Shutdown() {}

func TestEngine_StopSession(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// Create engine with a mock manager
	mockManager := &mockSessionManager{sessions: make(map[string]*Session)}
	engine := &Engine{
		opts:    EngineOptions{Namespace: "test"},
		logger:  logger,
		manager: mockManager,
	}

	// StopSession should delegate to manager
	err := engine.StopSession("test-session", "test reason")
	// With mock, this should succeed (no actual session to stop)
	if err != nil && err.Error() != "session not found" {
		t.Errorf("StopSession() unexpected error: %v", err)
	}
}

func TestEngine_DangerDetectorMethods(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	detector := NewDetector(logger)
	engine := &Engine{
		opts:           EngineOptions{Namespace: "test"},
		logger:         logger,
		dangerDetector: detector,
	}

	// Test SetDangerAllowPaths
	engine.SetDangerAllowPaths([]string{"/safe/path"})
	if !detector.IsPathAllowed("/safe/path") {
		t.Error("SetDangerAllowPaths() path not allowed after set")
	}

	// Test SetDangerBypassEnabled
	token := "test-token"
	detector.SetAdminToken(token)

	err := engine.SetDangerBypassEnabled(token, true)
	if err != nil {
		t.Errorf("SetDangerBypassEnabled() unexpected error: %v", err)
	}
	// After bypass, dangerous input should not be blocked
	if event := detector.CheckInput("rm -rf /"); event != nil {
		t.Error("Danger should be bypassed")
	}

	// Test GetDangerDetector
	if engine.GetDangerDetector() != detector {
		t.Error("GetDangerDetector() returned different instance")
	}
}

// mockSessionManager for testing
type mockSessionManager struct {
	sessions map[string]*Session
}

func (m *mockSessionManager) GetOrCreateSession(ctx context.Context, sessionID string, cfg Config) (*Session, error) {
	if sess, ok := m.sessions[sessionID]; ok {
		return sess, nil
	}
	return nil, &sessionNotFoundError{}
}

func (m *mockSessionManager) GetSession(sessionID string) (*Session, bool) {
	sess, ok := m.sessions[sessionID]
	return sess, ok
}

func (m *mockSessionManager) TerminateSession(sessionID string) error {
	delete(m.sessions, sessionID)
	return nil
}

func (m *mockSessionManager) ListActiveSessions() []*Session {
	list := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		list = append(list, s)
	}
	return list
}

func (m *mockSessionManager) Shutdown() {
	m.sessions = make(map[string]*Session)
}

type sessionNotFoundError struct{}

func (e *sessionNotFoundError) Error() string {
	return "session not found"
}
