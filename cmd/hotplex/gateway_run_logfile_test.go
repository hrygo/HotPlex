package main

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/config"
)

// TestBuildLogWriter_DisabledReturnsStderr verifies the default (file logging
// off) keeps the historical stderr-only writer, preserving daemon-mode
// stderr redirection behavior.
func TestBuildLogWriter_DisabledReturnsStderr(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.Log.File.Enabled = false

	w := buildLogWriter(cfg)
	require.Same(t, os.Stderr, w, "disabled file logging must return os.Stderr unchanged")
}

// TestBuildLogWriter_EnabledWritesToFile verifies that enabling file logging
// produces a writer that actually persists to the configured path. It does not
// assert on the MultiWriter-vs-lumberjack distinction (which depends on whether
// the test runner's stderr is a TTY); it only asserts the file receives data.
func TestBuildLogWriter_EnabledWritesToFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	logPath := filepath.Join(dir, "sub", "gateway.log") // nested dir to exercise MkdirAll

	cfg := config.Default()
	cfg.Log.File.Enabled = true
	cfg.Log.File.Path = logPath
	cfg.Log.File.MaxSize = 1
	cfg.Log.File.MaxBackups = 1

	w := buildLogWriter(cfg)

	// Should not be plain stderr once file logging is enabled.
	require.NotSame(t, os.Stderr, w)

	// Write through the writer and verify the bytes land on disk. slog handlers
	// write whole lines, so emulate that.
	_, err := io.WriteString(w, `{"level":"INFO","msg":"boot"}`+"\n")
	require.NoError(t, err)

	// lumberjack buffers via its own Write which calls the underlying file
	// directly (no user-facing buffer to flush); reading after Write is safe.
	data, err := os.ReadFile(logPath)
	require.NoError(t, err)
	require.Contains(t, string(data), "boot")
}

// TestBuildLogWriter_EnabledEmptyPathResolvesDefault verifies that an empty
// path is resolved to the default under the configured HotPlex home, and that
// the parent directory is created. We point HOTPLEX_HOME at a temp dir to keep
// the test hermetic. Not parallel: t.Setenv mutates process env.
func TestBuildLogWriter_EnabledEmptyPathResolvesDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOTPLEX_HOME", home)

	cfg := config.Default()
	cfg.Log.File.Enabled = true
	cfg.Log.File.Path = "" // expect default under $HOTPLEX_HOME/logs/gateway.log

	w := buildLogWriter(cfg)
	require.NotSame(t, os.Stderr, w)

	_, err := io.WriteString(w, `{"level":"INFO","msg":"hi"}`+"\n")
	require.NoError(t, err)

	defaultPath := filepath.Join(home, "logs", "gateway.log")
	data, err := os.ReadFile(defaultPath)
	require.NoError(t, err)
	require.Contains(t, string(data), "hi")
}

// TestBuildLogWriter_UncreatableDirFallsBackToStderr verifies that if the log
// directory cannot be created, buildLogWriter falls back to stderr rather than
// aborting (startup resilience).
func TestBuildLogWriter_UncreatableDirFallsBackToStderr(t *testing.T) {
	// Not parallel: relies on a path under a file (a POSIX trick to force
	// MkdirAll failure). Some platforms may allow this, so skip gracefully.

	// Create a regular file and try to use a path *inside* it as a directory.
	base := t.TempDir()
	blocker := filepath.Join(base, "blocker")
	require.NoError(t, os.WriteFile(blocker, nil, 0o644))

	cfg := config.Default()
	cfg.Log.File.Enabled = true
	cfg.Log.File.Path = filepath.Join(blocker, "nested", "gateway.log")

	w := buildLogWriter(cfg)
	require.Same(t, os.Stderr, w, "uncreatable log dir must fall back to stderr")
}

// TestStderrIsTTY is a smoke test; it only asserts the helper does not panic
// and returns a bool. The actual value depends on the test runner environment.
func TestStderrIsTTY(t *testing.T) {
	t.Parallel()
	_ = stderrIsTTY() // must not panic; result is environment-dependent
}
