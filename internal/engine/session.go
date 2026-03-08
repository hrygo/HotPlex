package engine

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hrygo/hotplex/internal/panicx"
	"github.com/hrygo/hotplex/internal/sys"
)

// SessionLogDir is the directory for session log files.
// Defaults to ~/.hotplex/logs/
var SessionLogDir = filepath.Join(os.Getenv("HOME"), ".hotplex", "logs")

// Session represents a persistent, hot-multiplexed instance of an AI CLI agent.
// It manages the underlying OS process group, handles streaming I/O via full-duplex pipes,
// and tracks the operational readiness and lifecycle status of the agent sandbox.
type Session struct {
	ID                string        // Internal SDK identifier (provided by the user)
	ProviderSessionID string        // The deterministic UUID (v5) passed to CLI for persistent DB storage
	Config            SessionConfig // Snapshot of the configuration used to initialize the session
	TaskInstructions  string        // Persistent instructions for the session

	// Process management
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdout    io.ReadCloser
	stderr    io.ReadCloser
	cancel    context.CancelFunc
	jobHandle uintptr // Windows Job Object handle (0 on Unix)

	CreatedAt    time.Time
	LastActive   time.Time
	Status       SessionStatus
	statusChange chan SessionStatus

	mu     sync.RWMutex
	closed bool

	callback   Callback
	logger     *slog.Logger
	logFile    *os.File // Session-specific log file for stderr persistence
	ext        any      // Extension payload for consumer packages
	IsResuming bool     // True if session was resumed from persistent marker
}

// IsAlive checks if the process is still running.
func (s *Session) IsAlive() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.isAliveLocked()
}

// isAliveLocked checks if the process is still running. Caller must hold lock.
func (s *Session) isAliveLocked() bool {
	if s.cmd == nil || s.cmd.Process == nil || s.Status == SessionStatusDead {
		return false
	}
	return sys.IsProcessAlive(s.cmd.Process)
}

// Touch updates LastActive time.
func (s *Session) Touch() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.LastActive = time.Now()
}

// GetLastActive returns the last active time with proper locking.
func (s *Session) GetLastActive() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.LastActive
}

// SetStatus updates the session status with proper locking.
func (s *Session) SetStatus(status SessionStatus) {
	s.mu.Lock()
	s.Status = status
	if s.closed {
		s.mu.Unlock()
		return
	}
	select {
	case s.statusChange <- status:
	default:
	}
	s.mu.Unlock()
}

// GetStatus returns the current session status.
func (s *Session) GetStatus() SessionStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Status
}

// GetStatusChange returns the status change channel for waiting on status updates.
func (s *Session) GetStatusChange() <-chan SessionStatus {
	return s.statusChange
}

// waitForReady monitors the session and transitions from Starting to Ready
// when the process is confirmed alive and responsive.
func (s *Session) waitForReady(ctx context.Context, timeout time.Duration) {
	panicx.SafeGo(s.logger, func() {
		deadlineTimer := time.NewTimer(timeout)
		defer deadlineTimer.Stop()

		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-deadlineTimer.C:
				s.mu.Lock()
				if s.Status == SessionStatusStarting {
					s.Status = SessionStatusDead
				}
				s.mu.Unlock()
				return
			case <-ticker.C:
				s.mu.Lock()
				if s.Status == SessionStatusDead {
					s.mu.Unlock()
					return
				}
				if s.isAliveLocked() {
					s.mu.Unlock()
					s.SetStatus(SessionStatusReady)
					return
				}
				s.mu.Unlock()
			}
		}
	})
}

// WriteInput injects a JSON message to Stdin.
func (s *Session) WriteInput(msg map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Status = SessionStatusBusy
	select {
	case s.statusChange <- SessionStatusBusy:
	default:
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("json marshal: %w", err)
	}

	data = append(data, '\n')

	_, err = s.stdin.Write(data)
	if err != nil {
		return fmt.Errorf("stdin write: %w", err)
	}

	s.LastActive = time.Now()
	return nil
}

// close releases resources held by the session.
// IMPORTANT: Caller must hold s.mu lock before calling this method.
func (s *Session) close() {
	s.Status = SessionStatusDead

	// Close all pipe resources to prevent file descriptor leaks
	// and allow ReadStdout/ReadStderr goroutines to exit
	if s.stdin != nil {
		_ = s.stdin.Close()
	}
	if s.stdout != nil {
		_ = s.stdout.Close()
	}
	if s.stderr != nil {
		_ = s.stderr.Close()
	}

	// Close session log file
	if s.logFile != nil {
		_ = s.logFile.Close()
		s.logFile = nil
	}

	if !s.closed {
		s.closed = true
		close(s.statusChange)
	}
}

// SetCallback registers the callback to handle stream events for the current turn.
func (s *Session) SetCallback(cb Callback) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.callback = cb
}

// GetCallback returns the current callback.
func (s *Session) GetCallback() Callback {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.callback
}

// SetExt attaches external state to the session.
func (s *Session) SetExt(data any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ext = data
}

// GetExt retrieves the external state attached to the session.
func (s *Session) GetExt() any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ext
}

// isExpectedCloseError checks if the error is an expected pipe closure during normal shutdown.
func isExpectedCloseError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) {
		return true
	}
	if strings.Contains(err.Error(), "file already closed") {
		return true
	}
	return false
}

// ReadStdout asynchronously reads CLI stdout, parses JSON, and dispatches callbacks.
func (s *Session) ReadStdout() {
	defer panicx.Recover(s.logger, "ReadStdout")

	if s.stdout == nil {
		return
	}

	scanner := bufio.NewScanner(s.stdout)
	buf := make([]byte, 0, ScannerInitialBufSize)
	scanner.Buffer(buf, ScannerMaxBufSize)

	defer func() {
		cb := s.GetCallback()
		if cb != nil {
			_ = cb("runner_exit", nil)
		}

		if err := scanner.Err(); err != nil && !isExpectedCloseError(err) {
			if s.logger != nil {
				s.logger.Error("Session stdout scanner error", "error", err)
			}
			s.SetStatus(SessionStatusDead)
		}
	}()

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		cb := s.GetCallback()
		if cb != nil {
			if err := cb("raw_line", line); err != nil {
				s.logger.Debug("ReadStdout: dispatch callback error", "error", err)
			}
		}
	}

	if err := scanner.Err(); err != nil && s.logger != nil && !isExpectedCloseError(err) {
		s.logger.Error("Session stdout scanner error", "error", err)
	}
}

// ReadStderr asynchronously reads CLI stderr to prevent buffer deadlocks.
func (s *Session) ReadStderr() {
	defer panicx.Recover(s.logger, "ReadStderr")

	if s.stderr == nil {
		return
	}

	scanner := bufio.NewScanner(s.stderr)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		// Structured logging with session context
		if s.logger != nil {
			s.logger.Warn("session_stderr",
				"session_id", s.ID,
				"provider_session_id", s.ProviderSessionID,
				"workdir", s.Config.WorkDir,
				"content", line)
		}
		// Write to session log file for persistence
		if s.logFile != nil {
			if _, err := fmt.Fprintf(s.logFile, "[%s] %s\n", time.Now().Format(time.RFC3339), line); err != nil && s.logger != nil {
				s.logger.Warn("Failed to write to session log", "error", err)
			}
		}
	}

	if err := scanner.Err(); err != nil && s.logger != nil && !isExpectedCloseError(err) {
		s.logger.Error("Session stderr scanner error",
			"session_id", s.ID,
			"error", err)
	}
}

// NewTestSession creates a Session for testing purposes.
// This should only be used in test code.
func NewTestSession(id string, status SessionStatus) *Session {
	return &Session{
		ID:                id,
		ProviderSessionID: "test-provider-session",
		Status:            status,
		statusChange:      make(chan SessionStatus, 10),
	}
}

// OpenLogFile opens a log file for this session in SessionLogDir.
func (s *Session) OpenLogFile() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.logFile != nil {
		return nil
	}
	if err := os.MkdirAll(SessionLogDir, 0755); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}
	logPath := filepath.Join(SessionLogDir, s.ID+".log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}
	s.logFile = f
	return nil
}

// GetLogPath returns the path to the session log file.
func (s *Session) GetLogPath() string {
	return filepath.Join(SessionLogDir, s.ID+".log")
}
