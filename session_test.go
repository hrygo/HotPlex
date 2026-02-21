package hotplex

import (
	"bytes"
	"io"
	"os/exec"
	"testing"
	"time"
)

func TestSessionStatus_String(t *testing.T) {
	statuses := []SessionStatus{
		SessionStatusStarting,
		SessionStatusReady,
		SessionStatusBusy,
		SessionStatusDead,
	}

	for _, s := range statuses {
		if string(s) == "" {
			t.Errorf("SessionStatus %v has empty string representation", s)
		}
	}
}

func TestSession_IsAlive_NilProcess(t *testing.T) {
	sess := &Session{
		Status: SessionStatusStarting,
	}

	if sess.IsAlive() {
		t.Error("IsAlive() should return false for nil process")
	}
}

func TestSession_Touch(t *testing.T) {
	sess := &Session{
		LastActive: time.Time{}, // Zero time
	}

	before := time.Now()
	sess.Touch()
	after := time.Now()

	if sess.LastActive.Before(before) || sess.LastActive.After(after) {
		t.Errorf("Touch() didn't update LastActive correctly: %v", sess.LastActive)
	}
}

func TestSession_SetStatus(t *testing.T) {
	sess := &Session{
		Status:       SessionStatusStarting,
		statusChange: make(chan SessionStatus, 10),
	}

	sess.SetStatus(SessionStatusReady)

	if sess.Status != SessionStatusReady {
		t.Errorf("Status = %v, want ready", sess.Status)
	}

	// Check status change was broadcast
	select {
	case s := <-sess.statusChange:
		if s != SessionStatusReady {
			t.Errorf("statusChange = %v, want ready", s)
		}
	default:
		t.Error("statusChange channel should have received status update")
	}
}

func TestSession_GetStatus(t *testing.T) {
	sess := &Session{
		Status: SessionStatusBusy,
	}

	if sess.GetStatus() != SessionStatusBusy {
		t.Errorf("GetStatus() = %v, want busy", sess.GetStatus())
	}
}

func TestSession_SetCallback(t *testing.T) {
	sess := &Session{}

	cb := func(eventType string, data any) error { return nil }
	sess.SetCallback(cb)

	if sess.callback == nil {
		t.Error("SetCallback() didn't set callback")
	}
}

func TestSession_WriteInput_InvalidJSON(t *testing.T) {
	sess := &Session{
		Status:       SessionStatusReady,
		statusChange: make(chan SessionStatus, 10),
	}

	// WriteInput should handle invalid types gracefully
	// (functions cannot be marshaled to JSON)
	msg := map[string]any{
		"func": func() {},
	}

	err := sess.WriteInput(msg)
	if err == nil {
		t.Error("WriteInput() should fail for unmarshalable data")
	}
}

func TestSessionPool_GetSession(t *testing.T) {
	logger := newTestLogger()
	pool := NewSessionPool(logger, 30*time.Minute, EngineOptions{Namespace: "test"}, "/tmp")

	// Get nonexistent session
	_, ok := pool.GetSession("nonexistent")
	if ok {
		t.Error("GetSession() should return false for nonexistent session")
	}
}

func TestSessionPool_ListActiveSessions(t *testing.T) {
	logger := newTestLogger()
	pool := NewSessionPool(logger, 30*time.Minute, EngineOptions{Namespace: "test"}, "/tmp")

	// Should return empty list
	sessions := pool.ListActiveSessions()
	if len(sessions) != 0 {
		t.Errorf("ListActiveSessions() = %d sessions, want 0", len(sessions))
	}
}

func TestSessionPool_TerminateSession_Nonexistent(t *testing.T) {
	logger := newTestLogger()
	pool := NewSessionPool(logger, 30*time.Minute, EngineOptions{Namespace: "test"}, "/tmp")

	// Terminating nonexistent session should be a no-op
	err := pool.TerminateSession("nonexistent")
	if err != nil {
		t.Errorf("TerminateSession() error: %v", err)
	}
}

func TestSessionPool_Shutdown(t *testing.T) {
	logger := newTestLogger()
	pool := NewSessionPool(logger, 30*time.Minute, EngineOptions{Namespace: "test"}, "/tmp")

	// Shutdown should be safe to call
	pool.Shutdown()

	// Second shutdown should be safe (idempotent)
	pool.Shutdown()
}

func TestSession_close(t *testing.T) {
	sess := &Session{
		Status:       SessionStatusReady,
		statusChange: make(chan SessionStatus, 10),
	}

	// Call close
	sess.close()

	if sess.Status != SessionStatusDead {
		t.Errorf("Status = %v, want dead", sess.Status)
	}

	// Channel should be closed
	select {
	case _, ok := <-sess.statusChange:
		if ok {
			t.Error("statusChange channel should be closed")
		}
	default:
		t.Error("statusChange channel should be closed")
	}
}

func TestSession_close_Idempotent(t *testing.T) {
	sess := &Session{
		Status:       SessionStatusReady,
		statusChange: make(chan SessionStatus, 10),
	}

	// Call close twice - should not panic
	sess.close()
	sess.close() // Second call should be safe

	if sess.Status != SessionStatusDead {
		t.Errorf("Status = %v, want dead", sess.Status)
	}
}

func TestSession_WriteInput_Valid(t *testing.T) {
	// Create a pipe to capture stdin
	r, w := io.Pipe()
	defer func() { _ = r.Close() }()
	defer func() { _ = w.Close() }()

	sess := &Session{
		Status:       SessionStatusReady,
		statusChange: make(chan SessionStatus, 10),
		stdin:        w,
	}

	// Write valid JSON
	msg := map[string]any{
		"type":    "user",
		"message": "hello",
	}

	// Read in goroutine
	var received []byte
	done := make(chan struct{})
	go func() {
		buf := make([]byte, 1024)
		n, _ := r.Read(buf)
		received = buf[:n]
		close(done)
	}()

	err := sess.WriteInput(msg)
	if err != nil {
		t.Errorf("WriteInput() error: %v", err)
	}

	// Wait for read
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for stdin write")
	}

	// Verify message was written
	if !bytes.Contains(received, []byte("user")) {
		t.Errorf("Received message doesn't contain expected content: %s", received)
	}
}

func TestSession_SetStatus_ClosedChannel(t *testing.T) {
	sess := &Session{
		Status:       SessionStatusReady,
		statusChange: make(chan SessionStatus, 10),
		closed:       true, // Already closed
	}

	// Should not panic or block
	sess.SetStatus(SessionStatusBusy)

	// Status should still be updated
	if sess.Status != SessionStatusBusy {
		t.Errorf("Status = %v, want busy", sess.Status)
	}
}

func TestSession_isAliveLocked(t *testing.T) {
	t.Run("nil cmd", func(t *testing.T) {
		sess := &Session{
			Status: SessionStatusReady,
		}
		if sess.isAliveLocked() {
			t.Error("isAliveLocked should return false for nil cmd")
		}
	})

	t.Run("nil process", func(t *testing.T) {
		sess := &Session{
			Status: SessionStatusReady,
			cmd:    &exec.Cmd{},
		}
		if sess.isAliveLocked() {
			t.Error("isAliveLocked should return false for nil process")
		}
	})

	t.Run("dead status", func(t *testing.T) {
		sess := &Session{
			Status: SessionStatusDead,
		}
		if sess.isAliveLocked() {
			t.Error("isAliveLocked should return false for dead status")
		}
	})
}

func TestSessionPool_Shutdown_WithSessions(t *testing.T) {
	logger := newTestLogger()
	pool := NewSessionPool(logger, 30*time.Minute, EngineOptions{Namespace: "test"}, "/tmp")

	// Add mock sessions directly to the pool
	pool.mu.Lock()
	pool.sessions["session-1"] = &Session{
		Status:       SessionStatusReady,
		statusChange: make(chan SessionStatus, 10),
	}
	pool.sessions["session-2"] = &Session{
		Status:       SessionStatusBusy,
		statusChange: make(chan SessionStatus, 10),
	}
	pool.mu.Unlock()

	// Shutdown should clean up all sessions
	pool.Shutdown()

	// Verify all sessions are removed
	pool.mu.RLock()
	if len(pool.sessions) != 0 {
		t.Errorf("Expected 0 sessions after shutdown, got %d", len(pool.sessions))
	}
	pool.mu.RUnlock()
}

func TestSessionPool_CleanupSessionLocked(t *testing.T) {
	logger := newTestLogger()
	pool := NewSessionPool(logger, 30*time.Minute, EngineOptions{Namespace: "test"}, "/tmp")

	// Add mock session
	pool.mu.Lock()
	pool.sessions["test-session"] = &Session{
		Status:       SessionStatusReady,
		statusChange: make(chan SessionStatus, 10),
	}
	pool.mu.Unlock()

	// Cleanup the session
	pool.mu.Lock()
	err := pool.cleanupSessionLocked("test-session")
	pool.mu.Unlock()

	if err != nil {
		t.Errorf("cleanupSessionLocked error: %v", err)
	}

	// Verify session is removed
	pool.mu.RLock()
	if _, ok := pool.sessions["test-session"]; ok {
		t.Error("Session should be removed after cleanup")
	}
	pool.mu.RUnlock()

	pool.Shutdown()
}

func TestSessionPool_CleanupSessionLocked_NonExistent(t *testing.T) {
	logger := newTestLogger()
	pool := NewSessionPool(logger, 30*time.Minute, EngineOptions{Namespace: "test"}, "/tmp")

	// Cleanup non-existent session should return nil
	pool.mu.Lock()
	err := pool.cleanupSessionLocked("non-existent")
	pool.mu.Unlock()

	if err != nil {
		t.Errorf("cleanupSessionLocked for non-existent session should return nil, got %v", err)
	}

	pool.Shutdown()
}

func TestSessionPool_Shutdown_WithCallback(t *testing.T) {
	logger := newTestLogger()
	pool := NewSessionPool(logger, 30*time.Minute, EngineOptions{Namespace: "test"}, "/tmp")

	callbackCalled := false
	cb := func(eventType string, data any) error {
		if eventType == "runner_exit" {
			callbackCalled = true
		}
		return nil
	}

	// Add mock session with callback
	pool.mu.Lock()
	pool.sessions["test-session"] = &Session{
		Status:       SessionStatusReady,
		statusChange: make(chan SessionStatus, 10),
		callback:     cb,
	}
	pool.mu.Unlock()

	// Shutdown should call runner_exit callback
	pool.Shutdown()

	if !callbackCalled {
		t.Error("Expected runner_exit callback to be called")
	}
}

func TestSessionPool_ListActiveSessions_Multiple(t *testing.T) {
	logger := newTestLogger()
	pool := NewSessionPool(logger, 30*time.Minute, EngineOptions{Namespace: "test"}, "/tmp")

	// Initially empty
	sessions := pool.ListActiveSessions()
	if len(sessions) != 0 {
		t.Errorf("ListActiveSessions() = %d, want 0", len(sessions))
	}

	pool.Shutdown()
}

func TestEngineOptions_Defaults(t *testing.T) {
	opts := EngineOptions{}

	// Check zero values
	if opts.Timeout != 0 {
		t.Errorf("Timeout = %v, want 0", opts.Timeout)
	}
	if opts.IdleTimeout != 0 {
		t.Errorf("IdleTimeout = %v, want 0", opts.IdleTimeout)
	}
	if opts.Namespace != "" {
		t.Errorf("Namespace = %q, want empty", opts.Namespace)
	}
}

func TestConfig_Validation(t *testing.T) {
	tests := []struct {
		name   string
		config Config
		valid  bool
	}{
		{"valid", Config{WorkDir: "/tmp", SessionID: "test"}, true},
		{"missing WorkDir", Config{SessionID: "test"}, false},
		{"missing SessionID", Config{WorkDir: "/tmp"}, false},
		{"empty", Config{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Config itself doesn't have Validate method, but we can check fields
			hasWorkDir := tt.config.WorkDir != ""
			hasSessionID := tt.config.SessionID != ""

			isValid := hasWorkDir && hasSessionID
			if isValid != tt.valid {
				t.Errorf("Config validity = %v, want %v", isValid, tt.valid)
			}
		})
	}
}
