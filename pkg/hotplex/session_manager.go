package hotplex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// SessionStatus defines the current state of a session.
type SessionStatus string

const (
	SessionStatusStarting SessionStatus = "starting"
	SessionStatusReady    SessionStatus = "ready"
	SessionStatusBusy     SessionStatus = "busy"
	SessionStatusDead     SessionStatus = "dead"
)

// Scanner buffer sizes for CLI output parsing.
const (
	scannerInitialBufSize = 256 * 1024       // 256 KB
	scannerMaxBufSize     = 10 * 1024 * 1024 // 10 MB
)

// Session lifecycle constants.
const (
	defaultReadyTimeout  = 10 * time.Second // Maximum time to wait for session to be ready
	statusBusyDuration   = 2 * time.Second  // Duration to keep session in Busy state after input
	cleanupCheckInterval = 1 * time.Minute  // Interval between idle session cleanup checks
)

// Session represents a persistent, long-lived process of the Claude Code CLI.
// It wraps the OS process, manages standard I/O pipes for real-time multiplexing,
// and tracks the session's readiness and lifecycle status.
type Session struct {
	ID          string // Internal SDK identifier (provided by the user)
	CCSessionID string // The deterministic UUID (v5) passed to Claude CLI for persistent DB storage
	Config      Config // Snapshot of the configuration used to initialize the session
	cmd         *exec.Cmd
	stdin       io.WriteCloser
	stdout      io.ReadCloser
	stderr      io.ReadCloser
	cancel      context.CancelFunc
	CreatedAt   time.Time     // When the process was first spawned
	LastActive  time.Time     // When the process was last used (used for LRU/Idle GC)
	Status      SessionStatus // Runtime state: starting, ready, busy, or dead

	mu               sync.RWMutex
	statusResetTimer *time.Timer // Timer to revert Busy status to Ready after predictable CLI inactivity

	callback Callback     // Active stream event handler for the current turn
	logger   *slog.Logger // Context-aware logger initialized with session metadata
}

// SessionManager defines the behavioral interface for managing a process pool.
type SessionManager interface {
	// GetOrCreateSession retrieves an active session or performs a Cold Start if none exists.
	GetOrCreateSession(ctx context.Context, sessionID string, cfg Config) (*Session, error)
	// GetSession performs a non-side-effect lookup of an active session.
	GetSession(sessionID string) (*Session, bool)
	// TerminateSession kills the OS process group and removes the session from the pool.
	TerminateSession(sessionID string) error
	// ListActiveSessions provides a snapshot of all tracked sessions.
	ListActiveSessions() []*Session
	// Shutdown performing a total cleanup of the pool and its background workers.
	Shutdown()
}

// SessionPool implements the SessionManager as a thread-safe singleton.
// It orchestrates the lifecycle of multiple CLI processes, ensuring that
// idle processes are garbage collected to conserve system memory.
type SessionPool struct {
	sessions     map[string]*Session
	mu           sync.RWMutex
	logger       *slog.Logger
	timeout      time.Duration // Time after which an idle session is eligible for termination
	opts         EngineOptions // Global constraints shared by all sessions in the pool
	cliPath      string        // Resolved path to the CLI binary (avoids redundant LookPath calls)
	done         chan struct{} // Internal signal for shutting down background workers
	shutdownOnce sync.Once     // Ensures Shutdown is only executed once
	markerDir    string        // Local filesystem path storing session persistence markers (.lock files)
}

// NewSessionPool creates a new session manager.
func NewSessionPool(logger *slog.Logger, timeout time.Duration, opts EngineOptions, cliPath string) *SessionPool {
	if logger == nil {
		logger = slog.Default()
	}
	// Initialize Marker Directory
	homeDir, err := os.UserHomeDir()
	var markerDir string
	if err == nil {
		markerDir = filepath.Join(homeDir, ".hotplex", "sessions")
		os.MkdirAll(markerDir, 0755) //nolint:errcheck // best effort
	} else {
		markerDir = filepath.Join(os.TempDir(), "hotplex_sessions")
		os.MkdirAll(markerDir, 0755) //nolint:errcheck // fallback
	}

	sm := &SessionPool{
		sessions:  make(map[string]*Session),
		logger:    logger,
		timeout:   timeout,
		opts:      opts,
		cliPath:   cliPath,
		done:      make(chan struct{}),
		markerDir: markerDir,
	}

	// Start idle session cleanup goroutine (per spec 6: 30m idle timeout)
	go sm.cleanupLoop()

	return sm
}

// GetOrCreateSession returns an existing session or starts a new one.
func (sm *SessionPool) GetOrCreateSession(ctx context.Context, sessionID string, cfg Config) (*Session, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Check if session exists and is alive
	if sess, ok := sm.sessions[sessionID]; ok {
		if sess.IsAlive() {
			sess.Touch()
			return sess, nil
		}
		// If dead, cleanup and recreate
		_ = sm.cleanupSessionLocked(sessionID) //nolint:errcheck // cleanup on dead session
	}

	// Create new session
	sess, err := sm.startSession(ctx, sessionID, cfg)
	if err != nil {
		return nil, err
	}

	sm.sessions[sessionID] = sess
	return sess, nil
}

// GetSession retrieves an active session.
func (sm *SessionPool) GetSession(sessionID string) (*Session, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	sess, ok := sm.sessions[sessionID]
	return sess, ok
}

// TerminateSession stops and removes a session.
func (sm *SessionPool) TerminateSession(sessionID string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.cleanupSessionLocked(sessionID)
}

// ListActiveSessions returns all active sessions.
func (sm *SessionPool) ListActiveSessions() []*Session {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	list := make([]*Session, 0, len(sm.sessions))
	for _, s := range sm.sessions {
		list = append(list, s)
	}
	return list
}

// cleanupSessionLocked stops the process and removes from map. Caller must hold lock.
func (sm *SessionPool) cleanupSessionLocked(sessionID string) error {
	sess, ok := sm.sessions[sessionID]
	if !ok {
		return nil
	}

	delete(sm.sessions, sessionID)

	sm.logger.Info("Terminating session and sweeping OS process group",
		"namespace", sm.opts.Namespace,
		"session_id", sessionID,
		"cc_session_id", sess.CCSessionID)

	// Stop the status reset timer and clean up session resources
	// Hold session lock to prevent race with WriteInput
	sess.mu.Lock()
	sess.close()
	sess.mu.Unlock()

	// Cancel context to kill process if using CommandContext
	if sess.cancel != nil {
		sess.cancel()
	}

	// Force kill if needed
	killProcessGroup(sess.cmd)

	return nil
}

// startSession initializes the OS process (Cold Start). Caller must hold lock.
func (sm *SessionPool) startSession(ctx context.Context, sessionID string, cfg Config) (*Session, error) {
	// Early exit if request context is already cancelled
	if ctx.Err() != nil {
		return nil, fmt.Errorf("request context cancelled: %w", ctx.Err())
	}

	cliPath := sm.cliPath

	// Prepare context with cancellation.
	// We intentionally use context.Background() instead of the request ctx
	// because the session should outlive the HTTP request that created it.
	sessCtx, cancel := context.WithCancel(context.Background())
	// Ensure cancel is called only on error paths to keep the process alive on success
	var success bool
	defer func() {
		if !success {
			cancel()
		}
	}()

	// Use a startup timeout to prevent indefinite hangs during process start
	// We monitor startup in a goroutine and cancel if it takes too long
	startupCtx, startupCancel := context.WithTimeout(ctx, 30*time.Second)
	defer startupCancel()

	// Channel to signal successful startup or failure
	startedCh := make(chan error, 1)

	// Ensure we signal completion even on early return
	// This prevents goroutine leak if function returns before startup completes
	defer close(startedCh)

	// Goroutine to monitor startup timeout
	// If startup takes longer than the timeout, cancel the session
	go func() {
		select {
		case err := <-startedCh:
			if err != nil {
				cancel()
			}
		case <-startupCtx.Done():
			select {
			case err := <-startedCh:
				if err != nil {
					cancel()
				}
			default:
				// Startup timed out and no success signal was sent
				cancel()
			}
		}
	}()

	// Deterministic UUID generation mapping from the string sessionID
	// We mix in the Engine's Namespace to ensure proper isolation across applications.
	// This ensures that Claude CLI always receives a valid UUID format,
	// while callers can use arbitrary strings.
	uniqueStr := fmt.Sprintf("%s:session:%s", sm.opts.Namespace, sessionID)
	ccSessionID := uuid.NewSHA1(uuid.NameSpaceURL, []byte(uniqueStr)).String()

	// Build arguments
	// We always force --output-format stream-json and --print
	args := []string{
		"--print",
		"--verbose",
		"--output-format", "stream-json",
		"--input-format", "stream-json",
	}

	// Create a contextual logger with standard fields for all subsequent logs
	sessLog := sm.logger.With(
		"namespace", sm.opts.Namespace,
		"session_id", sessionID,
		"cc_session_id", ccSessionID,
	)

	// 0. Session Persistence Logic (Marker Lock Files)
	// Claude CLI throws "already in use" if we pass --session-id for an existing session in its DB.
	// We use an external marker file to track if HotPlex has created this ccSessionID before.
	markerPath := filepath.Join(sm.markerDir, ccSessionID+".lock")
	if _, err := os.Stat(markerPath); err == nil {
		// Marker exists -> Session was previously created in Claude's local DB. We MUST resume.
		args = append(args, "--resume", ccSessionID)
		sessLog.Info("Resuming existing persistent CLI session")
	} else {
		// Marker missing -> Brand new session.
		args = append(args, "--session-id", ccSessionID)
		// Touch the lock file for future restarts
		_ = os.WriteFile(markerPath, []byte(""), 0644)
		sessLog.Info("Creating new persistent CLI session")
	}

	// 1. Permission Mode (Strictly enforced from EngineOptions)
	if sm.opts.PermissionMode != "" {
		args = append(args, "--permission-mode", sm.opts.PermissionMode)
	}

	// 2. Allowed Tools (Engine-level constraint)
	if len(sm.opts.AllowedTools) > 0 {
		args = append(args, "--allowed-tools", strings.Join(sm.opts.AllowedTools, ","))
	}

	// 3. Disallowed Tools (Engine-level constraint)
	if len(sm.opts.DisallowedTools) > 0 {
		args = append(args, "--disallowed-tools", strings.Join(sm.opts.DisallowedTools, ","))
	}

	// 4. System Prompt (Base Engine Persona Only)
	if sm.opts.BaseSystemPrompt != "" {
		args = append(args, "--append-system-prompt", sm.opts.BaseSystemPrompt)
	}

	cmd := exec.CommandContext(sessCtx, cliPath, args...)
	cmd.Dir = cfg.WorkDir
	cmd.Env = append(os.Environ(), "CLAUDE_DISABLE_TELEMETRY=1")
	setupCmdSysProcAttr(cmd)

	// Create pipes with proper cleanup on error paths
	var stdin io.WriteCloser
	var stdout, stderr io.ReadCloser

	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}

	stdout, err = cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close() //nolint:errcheck // cleanup on error path
		cancel()
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	stderr, err = cmd.StderrPipe()
	if err != nil {
		_ = stdout.Close() //nolint:errcheck // cleanup on error path
		_ = stdin.Close()  //nolint:errcheck // cleanup on error path
		cancel()
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		startedCh <- err // Signal startup failed
		return nil, fmt.Errorf("cmd start: %w", err)
	}

	// Signal that startup succeeded
	startedCh <- nil

	sessLog.Info("OS Process started (Cold Start)",
		"pid", cmd.Process.Pid,
		"pgid", cmd.Process.Pid) // PGID is the same as PID since we use Setpgid: true

	sess := &Session{
		ID:          sessionID,
		CCSessionID: ccSessionID,
		Config:      cfg,
		cmd:         cmd,
		stdin:       stdin,
		stdout:      stdout,
		stderr:      stderr,
		cancel:      cancel,
		CreatedAt:   time.Now(),
		LastActive:  time.Now(),
		Status:      SessionStatusStarting,
		logger:      sessLog, // Pre-bound contextual logger with namespace/session_id/cc_session_id
	}

	// Start background readers for multiplexing
	go sess.readStdout()
	go sess.readStderr()

	// Monitor process exit to prevent zombies and log unexpected crashes
	go func() {
		err := cmd.Wait()
		// Check if the exit was unexpected (session not already closing/dead)
		if sess.GetStatus() != SessionStatusDead && sessLog != nil {
			sessLog.Warn("Session OS process exited unexpectedly",
				"exit_error", err)
		}
	}()

	// Start status transition monitor: Starting -> Ready
	sess.waitForReady(sessCtx, defaultReadyTimeout)

	success = true
	return sess, nil
}

// isAliveLocked checks if the process is still running. Caller must hold lock.
func (s *Session) isAliveLocked() bool {
	if s.cmd == nil || s.cmd.Process == nil || s.Status == SessionStatusDead {
		return false
	}
	return isProcessAlive(s.cmd.Process)
}

// IsAlive checks if the process is still running.
func (s *Session) IsAlive() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.isAliveLocked()
}

// Touch updates LastActive time.
func (s *Session) Touch() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.LastActive = time.Now()
}

// SetStatus updates the session status with proper locking.
func (s *Session) SetStatus(status SessionStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Status = status
}

// GetStatus returns the current session status.
func (s *Session) GetStatus() SessionStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Status
}

// waitForReady monitors the session and transitions from Starting to Ready
// when the process is confirmed alive and responsive.
// The context parameter allows cancellation if the session is terminated early.
func (s *Session) waitForReady(ctx context.Context, timeout time.Duration) {
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		deadline := time.Now().Add(timeout)
		for time.Now().Before(deadline) {
			select {
			case <-ctx.Done():
				// Context cancelled - session terminated or request cancelled
				return
			case <-ticker.C:
				s.mu.Lock()
				if s.Status == SessionStatusDead {
					s.mu.Unlock()
					return
				}
				if s.isAliveLocked() {
					s.Status = SessionStatusReady
					s.mu.Unlock()
					return
				}
				s.mu.Unlock()
			}
		}
		// Timeout - mark as dead if still not alive
		s.mu.Lock()
		if s.Status == SessionStatusStarting {
			s.Status = SessionStatusDead
		}
		s.mu.Unlock()
	}()
}

// WriteInput injects a JSON message to Stdin.
// Transitions session to Busy during write, back to Ready after completion.
func (s *Session) WriteInput(msg map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Set status to Busy while processing input
	// Must be done under lock to prevent race with cleanup
	s.Status = SessionStatusBusy

	// Reset existing timer if any (prevents goroutine accumulation)
	if s.statusResetTimer != nil {
		// Stop the timer and check if it was already fired
		if !s.statusResetTimer.Stop() {
			// Timer already fired - callback may be running or about to run
			// Release lock briefly to allow callback to complete if it's holding lock
			s.mu.Unlock()
			time.Sleep(50 * time.Millisecond) // Give callback time to complete
			s.mu.Lock()
		}
	}

	// Schedule status recovery to Ready after a short delay
	// This allows the session to be marked busy while the CLI processes the input
	// Callback acquires lock to prevent race with WriteInput
	s.statusResetTimer = time.AfterFunc(statusBusyDuration, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.isAliveLocked() {
			s.Status = SessionStatusReady
		}
	})

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	// Append newline as protocol often requires it (JSONL)
	data = append(data, '\n')

	_, err = s.stdin.Write(data)
	if err != nil {
		return err
	}

	s.LastActive = time.Now()
	return nil
}

// close releases resources held by the session.
// Must be called with session lock held.
func (s *Session) close() {
	// Stop the status reset timer if exists
	// Use a local copy to avoid holding lock during Stop()
	if s.statusResetTimer != nil {
		timer := s.statusResetTimer
		s.statusResetTimer = nil
		// Timer.Stop is safe to call multiple times and from different goroutines
		timer.Stop()
	}
}

// SetCallback registers the callback to handle stream events for the current turn.
func (s *Session) SetCallback(cb Callback) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.callback = cb
}

// readStdout asynchronously reads CLI stdout, parses JSON, and dispatches callbacks.
func (s *Session) readStdout() {
	if s.stdout == nil {
		return
	}

	scanner := bufio.NewScanner(s.stdout)
	buf := make([]byte, 0, scannerInitialBufSize)
	scanner.Buffer(buf, scannerMaxBufSize)

	// Ensure doneChan is closed on exit to prevent callers from hanging indefinitely
	// This handles cases where the scanner aborts due to ErrTooLong, process crash, or EOF.
	defer func() {
		s.mu.RLock()
		cb := s.callback
		s.mu.RUnlock()

		if cb != nil {
			_ = cb("runner_exit", nil)
		}

		// If scanner exited with error, the process is likely dead or in a bad state
		if err := scanner.Err(); err != nil {
			if s.logger != nil {
				s.logger.Error("Session stdout scanner error", "error", err)
			}
			s.mu.Lock()
			s.Status = SessionStatusDead
			s.mu.Unlock()
		}
	}()

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		s.mu.RLock()
		cb := s.callback
		s.mu.RUnlock()

		if cb != nil {
			if err := cb("raw_line", line); err != nil {
				s.logger.Debug("readStdout: dispatch callback error", "error", err)
			}
		}
	}

	if err := scanner.Err(); err != nil && s.logger != nil {
		s.logger.Error("Session stdout scanner error", "error", err)
	}
}

// readStderr asynchronously reads CLI stderr to prevent buffer deadlocks.
func (s *Session) readStderr() {
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

	if err := scanner.Err(); err != nil && s.logger != nil {
		s.logger.Error("Session stderr scanner error", "error", err)
	}
}

// cleanupLoop runs periodic cleanup of idle sessions.
// Runs every minute and terminates sessions that have been idle longer than timeout.
func (sm *SessionPool) cleanupLoop() {
	ticker := time.NewTicker(cleanupCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			sm.cleanupIdleSessions()
		case <-sm.done:
			return
		}
	}
}

// cleanupIdleSessions removes sessions that have exceeded the idle timeout.
func (sm *SessionPool) cleanupIdleSessions() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	now := time.Now()
	for sessionID, sess := range sm.sessions {
		idleTime := now.Sub(sess.LastActive)
		if idleTime > sm.timeout {
			sm.logger.Info("Session idle timeout, terminating",
				"namespace", sm.opts.Namespace,
				"session_id", sessionID,
				"cc_session_id", sess.CCSessionID,
				"idle_duration", idleTime,
				"timeout", sm.timeout)
			_ = sm.cleanupSessionLocked(sessionID) //nolint:errcheck // cleanup on idle timeout
		}
	}
}

// Shutdown gracefully stops the session manager and all active sessions.
func (sm *SessionPool) Shutdown() {
	sm.shutdownOnce.Do(func() {
		close(sm.done)
	})

	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Mark all sessions as Dead and signal runner_exit to unblock waiting callers
	for _, sess := range sm.sessions {
		sess.mu.Lock()
		sess.Status = SessionStatusDead
		if sess.callback != nil {
			_ = sess.callback("runner_exit", nil)
		}
		sess.mu.Unlock()
	}

	// Terminate all sessions (kill processes, cancel contexts)
	for sessionID := range sm.sessions {
		_ = sm.cleanupSessionLocked(sessionID) //nolint:errcheck // cleanup on shutdown
	}
}
