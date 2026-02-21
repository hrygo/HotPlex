package hotplex

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestIsExpectedCloseError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{"EOF", io.EOF, true},
		{"file already closed", errors.New("read |0: file already closed"), true},
		{"other error", errors.New("some other error"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isExpectedCloseError(tt.err)
			if result != tt.expected {
				t.Errorf("isExpectedCloseError(%v) = %v, want %v", tt.err, result, tt.expected)
			}
		})
	}
}

func TestSetupCmdPipes(t *testing.T) {
	// This test creates actual pipes, which is safe
	cmd := createTestCommand()

	stdin, stdout, stderr, err := setupCmdPipes(cmd)
	if err != nil {
		t.Fatalf("setupCmdPipes error: %v", err)
	}

	if stdin == nil {
		t.Error("stdin should not be nil")
	}
	if stdout == nil {
		t.Error("stdout should not be nil")
	}
	if stderr == nil {
		t.Error("stderr should not be nil")
	}

	// Cleanup
	_ = stdin.Close()
	_ = stdout.Close()
	_ = stderr.Close()
}

func TestMonitorStartup_Success(t *testing.T) {
	ctx, cancel := createTestContext()
	defer cancel()

	startedCh := make(chan error, 1)
	startedCh <- nil // Simulate successful start

	// Create a cancel function to verify it's NOT called on success
	cancelCalled := false
	testCancel := func() {
		cancelCalled = true
	}

	monitorStartup(ctx, startedCh, testCancel)

	if cancelCalled {
		t.Error("cancel should not be called on success")
	}
}

func TestMonitorStartup_Error(t *testing.T) {
	ctx, cancel := createTestContext()
	defer cancel()

	startedCh := make(chan error, 1)
	startedCh <- errors.New("startup failed")

	// Create a cancel function to verify it IS called on error
	cancelCalled := false
	testCancel := func() {
		cancelCalled = true
	}

	monitorStartup(ctx, startedCh, testCancel)

	if !cancelCalled {
		t.Error("cancel should be called on error")
	}
}

func TestMonitorStartup_Timeout(t *testing.T) {
	ctx, cancel := createTestContextWithTimeout(1 * 1000000) // 1ms timeout
	defer cancel()

	startedCh := make(chan error, 1)
	// Don't send anything - simulate timeout

	cancelCalled := false
	testCancel := func() {
		cancelCalled = true
	}

	monitorStartup(ctx, startedCh, testCancel)

	if !cancelCalled {
		t.Error("cancel should be called on timeout")
	}
}

func TestSessionPool_buildCLIArgs(t *testing.T) {
	logger := newTestLogger()
	pool := NewSessionPool(logger, 30*1000000000, EngineOptions{
		Namespace:        "test",
		PermissionMode:   "bypass-permissions",
		AllowedTools:     []string{"bash", "edit"},
		DisallowedTools:  []string{"dangerous"},
		BaseSystemPrompt: "You are helpful",
	}, "/tmp/claude")

	args := pool.buildCLIArgs("test-session-id", logger)

	// Check essential args
	if !containsInSlice(args, "--print") {
		t.Error("args should contain --print")
	}
	if !containsInSlice(args, "--verbose") {
		t.Error("args should contain --verbose")
	}
	if !containsInSlice(args, "--output-format") {
		t.Error("args should contain --output-format")
	}
	if !containsInSlice(args, "stream-json") {
		t.Error("args should contain stream-json")
	}
	if !containsInSlice(args, "--permission-mode") {
		t.Error("args should contain --permission-mode")
	}
	if !containsInSlice(args, "--allowed-tools") {
		t.Error("args should contain --allowed-tools")
	}
}

func TestSessionPool_buildCLIArgs_Resume(t *testing.T) {
	logger := newTestLogger()

	// Create pool with marker directory
	pool := NewSessionPool(logger, 30*1000000000, EngineOptions{
		Namespace: "test",
	}, "/tmp/claude")

	// Create a marker file to simulate existing session
	markerPath := pool.markerDir + "/existing-session.lock"
	if err := osWriteFile(markerPath, []byte(""), 0644); err != nil {
		t.Fatalf("Failed to create marker file: %v", err)
	}
	defer func() { _ = osRemove(markerPath) }()

	args := pool.buildCLIArgs("existing-session", logger)

	// Should have --resume for existing sessions
	if !containsInSlice(args, "--resume") {
		t.Error("args should contain --resume for existing session")
	}
}

// Helper functions
func createTestCommand() *exec.Cmd {
	return exec.Command("echo", "test")
}

func createTestContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 5*1000000000)
}

func createTestContextWithTimeout(timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), timeout)
}

// Avoid importing os and context in test file
var osWriteFile = func(name string, data []byte, perm uint32) error {
	return os.WriteFile(name, data, os.FileMode(perm))
}

var osRemove = func(name string) error {
	return os.Remove(name)
}
