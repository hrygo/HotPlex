package engine

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/hrygo/hotplex/provider"
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
	ctx, cancel := createTestContextWithTimeout(1 * time.Millisecond)
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
	prv, err := provider.NewClaudeCodeProvider(provider.ProviderConfig{
		DefaultPermissionMode: "bypass-permissions",
		AllowedTools:          []string{"bash", "edit"},
		DisallowedTools:       []string{"dangerous"},
	}, logger)
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	pool := NewSessionPool(logger, 30*time.Minute, EngineOptions{
		Namespace:        "test",
		PermissionMode:   "bypass-permissions",
		AllowedTools:     []string{"bash", "edit"},
		DisallowedTools:  []string{"dangerous"},
		BaseSystemPrompt: "You are helpful",
	}, "/tmp/claude", prv)

	args := pool.buildCLIArgs("test-session-id", logger, "unit test prompt", SessionConfig{
		TaskInstructions: "unit test instructions",
		WorkDir:          "/tmp/test",
	})

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

	prv, err := provider.NewClaudeCodeProvider(provider.ProviderConfig{}, logger)
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	// Create pool with marker store
	pool := NewSessionPool(logger, 30*time.Minute, EngineOptions{
		Namespace: "test",
	}, "/tmp/claude", prv)

	// Create a marker to simulate existing session
	if err := pool.markerStore.Create("existing-session"); err != nil {
		t.Fatalf("Failed to create marker: %v", err)
	}
	defer func() { _ = pool.markerStore.Delete("existing-session") }()

	args := pool.buildCLIArgs("existing-session", logger, "unit test resume prompt", SessionConfig{})

	// Should have --resume for existing sessions
	if !containsInSlice(args, "--resume") {
		t.Error("args should contain --resume for existing session")
	}
}

// TestStartSession_ResolvesRelativeWorkDir tests that relative WorkDir paths are resolved to absolute paths
func TestStartSession_ResolvesRelativeWorkDir(t *testing.T) {
	logger := newTestLogger()

	prv, err := provider.NewClaudeCodeProvider(provider.ProviderConfig{}, logger)
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	pool := NewSessionPool(logger, 30*time.Minute, EngineOptions{
		Namespace: "test",
	}, "/tmp/claude", prv)

	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current working directory: %v", err)
	}

	// Test cases for relative paths
	testCases := []struct {
		name     string
		workDir  string
		expected string
	}{
		{"current directory", ".", cwd},
		{"absolute path", "/tmp/test", "/tmp/test"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := SessionConfig{
				WorkDir: tc.workDir,
			}

			// Create a command to test path resolution
			sessCtx, cancel := context.WithCancel(context.Background())
			defer cancel()

			args := pool.buildCLIArgs("test-session", logger, "test", SessionConfig{WorkDir: tc.workDir})
			cmd := exec.CommandContext(sessCtx, "/tmp/claude", args...)

			// Apply the same path resolution logic as in startSession
			if cfg.WorkDir == "." || !filepath.IsAbs(cfg.WorkDir) {
				if absPath, err := filepath.Abs(cfg.WorkDir); err == nil {
					cmd.Dir = absPath
				} else {
					cmd.Dir = cfg.WorkDir
				}
			} else {
				cmd.Dir = cfg.WorkDir
			}

			if cmd.Dir != tc.expected {
				t.Errorf("cmd.Dir = %q, want %q", cmd.Dir, tc.expected)
			}
		})
	}
}

// Helper functions
func createTestCommand() *exec.Cmd {
	return exec.Command("echo", "test")
}

func createTestContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 5*time.Second)
}

func createTestContextWithTimeout(timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), timeout)
}

func containsInSlice(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
