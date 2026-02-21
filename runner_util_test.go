package hotplex

import (
	"testing"
)

func TestCloseDoneChan(t *testing.T) {
	// Test closing an open channel
	ch := make(chan struct{})
	closeDoneChan(ch)

	// Channel should be closed
	select {
	case <-ch:
		// Expected - channel is closed
	default:
		t.Error("Channel should be closed")
	}

	// Test closing an already closed channel (should not panic)
	closeDoneChan(ch)
	closeDoneChan(ch) // Multiple calls should be safe
}

func TestCloseDoneChan_NonBlocking(t *testing.T) {
	// Create channel with a value already drained
	ch := make(chan struct{})
	close(ch)

	// Should not block or panic
	closeDoneChan(ch)
}

func TestEngine_Close(t *testing.T) {
	logger := newTestLogger()
	mockManager := &mockSessionManager{sessions: make(map[string]*Session)}

	engine := &Engine{
		opts:    EngineOptions{Namespace: "test"},
		logger:  logger,
		manager: mockManager,
	}

	// Close should succeed
	err := engine.Close()
	if err != nil {
		t.Errorf("Close() error: %v", err)
	}
}

func TestEngine_GetCLIVersion(t *testing.T) {
	logger := newTestLogger()

	engine := &Engine{
		opts:    EngineOptions{Namespace: "test"},
		logger:  logger,
		cliPath: "/nonexistent/claude",
	}

	// Should fail with nonexistent CLI
	_, err := engine.GetCLIVersion()
	if err == nil {
		t.Error("GetCLIVersion() should fail for nonexistent CLI")
	}
}

func TestEngine_StopSession_WithMockManager(t *testing.T) {
	logger := newTestLogger()
	mockManager := &mockSessionManager{
		sessions: make(map[string]*Session),
	}

	engine := &Engine{
		opts:    EngineOptions{Namespace: "test"},
		logger:  logger,
		manager: mockManager,
	}

	err := engine.StopSession("test-session", "test reason")
	if err != nil {
		t.Errorf("StopSession() error: %v", err)
	}
}

func TestWrapSafe_WithNilLogger(t *testing.T) {
	// WrapSafe with nil logger should still work
	cb := func(eventType string, data any) error {
		return nil
	}

	wrapped := WrapSafe(nil, cb)
	if wrapped == nil {
		t.Error("WrapSafe should not return nil for non-nil callback")
	}
}

func TestWrapSafe_WithErrorAndNilLogger(t *testing.T) {
	// WrapSafe with nil logger and error callback should not panic
	cb := func(eventType string, data any) error {
		return ErrDangerBlocked
	}

	wrapped := WrapSafe(nil, cb)
	err := wrapped("test", nil)

	// Should suppress error and return nil
	if err != nil {
		t.Errorf("WrapSafe should suppress error, got: %v", err)
	}
}

func TestEventMeta_Defaults(t *testing.T) {
	// Test that EventMeta can be created with zero values
	meta := &EventMeta{}

	if meta.DurationMs != 0 {
		t.Errorf("DurationMs = %d, want 0", meta.DurationMs)
	}
	if meta.Status != "" {
		t.Errorf("Status = %q, want empty", meta.Status)
	}
}

func TestSessionStatsData_JSON(t *testing.T) {
	data := &SessionStatsData{
		SessionID:       "test-session",
		TotalDurationMs: 1000,
		ToolCallCount:   5,
		IsError:         false,
	}

	// Just verify fields are accessible
	if data.SessionID != "test-session" {
		t.Errorf("SessionID = %q, want 'test-session'", data.SessionID)
	}
}

func TestStreamMessage_Defaults(t *testing.T) {
	msg := StreamMessage{}

	if msg.Type != "" {
		t.Errorf("Type = %q, want empty", msg.Type)
	}
	if msg.Usage != nil {
		t.Error("Usage should be nil by default")
	}
}

func TestContentBlock_GetUnifiedToolID_Empty(t *testing.T) {
	block := ContentBlock{}

	// Both ToolUseID and ID are empty, should return empty
	id := block.GetUnifiedToolID()
	if id != "" {
		t.Errorf("GetUnifiedToolID() = %q, want empty", id)
	}
}

func TestConfig_Empty(t *testing.T) {
	cfg := Config{}

	if cfg.WorkDir != "" {
		t.Errorf("WorkDir = %q, want empty", cfg.WorkDir)
	}
	if cfg.SessionID != "" {
		t.Errorf("SessionID = %q, want empty", cfg.SessionID)
	}
}

func TestDangerBlockEvent_Fields(t *testing.T) {
	event := &DangerBlockEvent{
		Operation:      "rm -rf /",
		Reason:         "Delete root directory",
		PatternMatched: "rm\\s+-rf\\s+?/",
		Level:          DangerLevelCritical,
		Category:       "file_delete",
		BypassAllowed:  false,
		Suggestions:    []string{"Use rm -i"},
	}

	if event.Operation != "rm -rf /" {
		t.Errorf("Operation = %q", event.Operation)
	}
	if len(event.Suggestions) != 1 {
		t.Errorf("len(Suggestions) = %d, want 1", len(event.Suggestions))
	}
}

func TestDangerPattern_Description(t *testing.T) {
	// Just verify the struct is accessible
	pattern := DangerPattern{
		Description: "Test pattern",
		Level:       DangerLevelHigh,
		Category:    "test",
	}

	if pattern.Description != "Test pattern" {
		t.Errorf("Description = %q", pattern.Description)
	}
}

func TestEngineOptions_AllFields(t *testing.T) {
	opts := EngineOptions{
		Timeout:          5 * 1000,
		IdleTimeout:      30 * 1000,
		Namespace:        "test-ns",
		PermissionMode:   "bypass-permissions",
		BaseSystemPrompt: "You are helpful",
		AllowedTools:     []string{"bash", "edit"},
		DisallowedTools:  []string{"dangerous"},
	}

	if opts.Namespace != "test-ns" {
		t.Errorf("Namespace = %q", opts.Namespace)
	}
	if len(opts.AllowedTools) != 2 {
		t.Errorf("len(AllowedTools) = %d, want 2", len(opts.AllowedTools))
	}
}
