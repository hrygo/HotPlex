package hotplex

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"
)

func TestEngine_createEventBridge(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	engine := &Engine{
		opts:   EngineOptions{Namespace: "test"},
		logger: logger,
	}

	cfg := &Config{
		WorkDir:   "/tmp",
		SessionID: "test-session",
	}

	stats := &SessionStats{SessionID: "test-session"}
	doneChan := make(chan struct{})

	// Create event bridge
	cb := engine.createEventBridge(cfg, nil, stats, doneChan)

	if cb == nil {
		t.Fatal("createEventBridge returned nil")
	}

	// Test runner_exit event
	err := cb("runner_exit", nil)
	if err != nil {
		t.Errorf("runner_exit callback error: %v", err)
	}

	// doneChan should be closed after runner_exit
	select {
	case <-doneChan:
		// Expected
	default:
		t.Error("doneChan should be closed after runner_exit")
	}
}

func TestEngine_createEventBridge_RawLine(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	engine := &Engine{
		opts:   EngineOptions{Namespace: "test"},
		logger: logger,
	}

	cfg := &Config{
		WorkDir:   "/tmp",
		SessionID: "test-session",
	}

	stats := &SessionStats{SessionID: "test-session"}
	doneChan := make(chan struct{})

	var received string
	userCb := func(eventType string, data any) error {
		if eventType == "answer" {
			received = data.(string)
		}
		return nil
	}

	cb := engine.createEventBridge(cfg, userCb, stats, doneChan)

	// Test raw_line event with invalid JSON (should be passed as answer)
	err := cb("raw_line", "not valid json")
	if err != nil {
		t.Errorf("raw_line callback error: %v", err)
	}

	if received != "not valid json" {
		t.Errorf("received = %q, want 'not valid json'", received)
	}
}

func TestEngine_createEventBridge_ResultMessage(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// Create mock manager with session
	mockMgr := &mockSessionManager{sessions: make(map[string]*Session)}
	mockMgr.sessions["test-session"] = &Session{
		Status:       SessionStatusBusy,
		statusChange: make(chan SessionStatus, 10),
	}

	engine := &Engine{
		opts:    EngineOptions{Namespace: "test"},
		logger:  logger,
		manager: mockMgr,
	}

	cfg := &Config{
		WorkDir:   "/tmp",
		SessionID: "test-session",
	}

	stats := &SessionStats{SessionID: "test-session"}
	doneChan := make(chan struct{})

	var sessionStatsReceived bool
	userCb := func(eventType string, data any) error {
		if eventType == "session_stats" {
			sessionStatsReceived = true
		}
		return nil
	}

	cb := engine.createEventBridge(cfg, userCb, stats, doneChan)

	// Test result message
	msg := StreamMessage{
		Type:     "result",
		Duration: 1000,
		Usage: &UsageStats{
			InputTokens:  100,
			OutputTokens: 50,
		},
	}

	err := cb("result", msg)
	if err != nil {
		t.Errorf("result callback error: %v", err)
	}

	// doneChan should be closed after result
	select {
	case <-doneChan:
		// Expected
	default:
		t.Error("doneChan should be closed after result message")
	}

	if !sessionStatsReceived {
		t.Error("session_stats event should be sent")
	}
}

func TestEngine_createEventBridge_SystemMessage(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	engine := &Engine{
		opts:   EngineOptions{Namespace: "test"},
		logger: logger,
	}

	cfg := &Config{
		WorkDir:   "/tmp",
		SessionID: "test-session",
	}

	stats := &SessionStats{SessionID: "test-session"}
	doneChan := make(chan struct{})

	var called bool
	userCb := func(eventType string, data any) error {
		called = true
		return nil
	}

	cb := engine.createEventBridge(cfg, userCb, stats, doneChan)

	// Test system message - should be silently ignored
	msg := StreamMessage{Type: "system"}
	err := cb("pre-parsed", msg)
	if err != nil {
		t.Errorf("system message callback error: %v", err)
	}

	if called {
		t.Error("system message should not trigger user callback")
	}

	// doneChan should NOT be closed for system message
	select {
	case <-doneChan:
		t.Error("doneChan should NOT be closed for system message")
	default:
		// Expected
	}
}

func TestEngine_createEventBridge_NonStreamMessage(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	engine := &Engine{
		opts:   EngineOptions{Namespace: "test"},
		logger: logger,
	}

	cfg := &Config{
		WorkDir:   "/tmp",
		SessionID: "test-session",
	}

	stats := &SessionStats{SessionID: "test-session"}
	doneChan := make(chan struct{})

	var received string
	userCb := func(eventType string, data any) error {
		received = eventType
		return nil
	}

	cb := engine.createEventBridge(cfg, userCb, stats, doneChan)

	// Test non-StreamMessage data (legacy path)
	err := cb("custom_event", "some data")
	if err != nil {
		t.Errorf("non-StreamMessage callback error: %v", err)
	}

	if received != "custom_event" {
		t.Errorf("received = %q, want 'custom_event'", received)
	}
}

func TestEngine_createEventBridge_WithCallback(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	mockMgr := &mockSessionManager{sessions: make(map[string]*Session)}
	mockMgr.sessions["test-session"] = &Session{
		Status:       SessionStatusBusy,
		statusChange: make(chan SessionStatus, 10),
	}

	engine := &Engine{
		opts:    EngineOptions{Namespace: "test"},
		logger:  logger,
		manager: mockMgr,
	}

	cfg := &Config{
		WorkDir:   "/tmp",
		SessionID: "test-session",
	}

	stats := &SessionStats{SessionID: "test-session"}
	doneChan := make(chan struct{})

	var receivedType string
	userCb := func(eventType string, data any) error {
		receivedType = eventType
		return nil
	}

	cb := engine.createEventBridge(cfg, userCb, stats, doneChan)

	// Test with a message that goes through dispatchCallback
	msg := StreamMessage{
		Type: "thinking",
		Content: []ContentBlock{
			{Type: "text", Text: "thinking..."},
		},
	}

	err := cb("pre-parsed", msg)
	if err != nil {
		t.Errorf("thinking message callback error: %v", err)
	}

	if receivedType != "thinking" {
		t.Errorf("receivedType = %q, want 'thinking'", receivedType)
	}
}

func TestEngine_createEventBridge_RawLineNotString(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	engine := &Engine{
		opts:   EngineOptions{Namespace: "test"},
		logger: logger,
	}

	cfg := &Config{
		WorkDir:   "/tmp",
		SessionID: "test-session",
	}

	stats := &SessionStats{SessionID: "test-session"}
	doneChan := make(chan struct{})

	cb := engine.createEventBridge(cfg, nil, stats, doneChan)

	// Test raw_line with non-string data - should be silently ignored
	err := cb("raw_line", 12345)
	if err != nil {
		t.Errorf("raw_line with non-string error: %v", err)
	}
}

func TestEngine_waitForSession(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	engine := &Engine{
		opts:   EngineOptions{Namespace: "test"},
		logger: logger,
	}

	t.Run("session ready", func(t *testing.T) {
		sess := &Session{
			Status:       SessionStatusReady,
			statusChange: make(chan SessionStatus, 10),
		}

		ctx := context.Background()
		err := engine.waitForSession(ctx, sess, "test-session")
		if err != nil {
			t.Errorf("waitForSession error: %v", err)
		}
	})

	t.Run("session busy", func(t *testing.T) {
		sess := &Session{
			Status:       SessionStatusBusy,
			statusChange: make(chan SessionStatus, 10),
		}

		ctx := context.Background()
		err := engine.waitForSession(ctx, sess, "test-session")
		if err != nil {
			t.Errorf("waitForSession error: %v", err)
		}
	})

	t.Run("session dead", func(t *testing.T) {
		sess := &Session{
			Status:       SessionStatusDead,
			statusChange: make(chan SessionStatus, 10),
		}

		ctx := context.Background()
		err := engine.waitForSession(ctx, sess, "test-session")
		if err == nil {
			t.Error("waitForSession should fail for dead session")
		}
	})

	t.Run("context cancelled", func(t *testing.T) {
		sess := &Session{
			Status:       SessionStatusStarting,
			statusChange: make(chan SessionStatus, 10),
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		err := engine.waitForSession(ctx, sess, "test-session")
		if err != context.Canceled {
			t.Errorf("waitForSession error = %v, want context.Canceled", err)
		}
	})
}

func TestEngine_waitForSession_StatusChange(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	engine := &Engine{
		opts:   EngineOptions{Namespace: "test"},
		logger: logger,
	}

	sess := &Session{
		Status:       SessionStatusStarting,
		statusChange: make(chan SessionStatus, 10),
	}

	ctx := context.Background()

	// Send status change in goroutine
	go func() {
		time.Sleep(10 * time.Millisecond)
		sess.SetStatus(SessionStatusReady)
	}()

	err := engine.waitForSession(ctx, sess, "test-session")
	if err != nil {
		t.Errorf("waitForSession error: %v", err)
	}
}
