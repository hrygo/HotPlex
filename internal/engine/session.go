package engine

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/hrygo/hotplex/internal/panicx"
	"github.com/hrygo/hotplex/internal/sys"
)

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

	callback Callback
	logger   *slog.Logger
	ext      any // Extension payload for consumer packages
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
		if s.logger != nil {
			s.logger.Warn("Session stderr", "stderr", line)
		}
	}

	if err := scanner.Err(); err != nil && s.logger != nil && !isExpectedCloseError(err) {
		s.logger.Error("Session stderr scanner error", "error", err)
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
