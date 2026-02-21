package hotplex

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
	cleanupCheckInterval = 1 * time.Minute  // Interval between idle session cleanup checks
)

// Session represents a persistent, hot-multiplexed instance of an AI CLI agent (e.g., Claude Code).
// It manages the underlying OS process group, handles streaming I/O via full-duplex pipes,
// and tracks the operational readiness and lifecycle status of the agent sandbox.
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

	mu           sync.RWMutex
	statusChange chan SessionStatus // Channel to broadcast status changes
	closed       bool               // Track if statusChange channel is closed (#11)

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
	pending      map[string]chan struct{}
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
		pending:   make(map[string]chan struct{}),
	}

	// Start idle session cleanup goroutine (per spec 6: 30m idle timeout)
	go sm.cleanupLoop()

	return sm
}

// GetOrCreateSession returns an existing session or starts a new one.
func (sm *SessionPool) GetOrCreateSession(ctx context.Context, sessionID string, cfg Config) (*Session, error) {
	// 1. Check existing
	sm.mu.RLock()
	if sess, ok := sm.sessions[sessionID]; ok {
		if sess.IsAlive() {
			sm.mu.RUnlock()
			sess.Touch()
			return sess, nil
		}
	}
	sm.mu.RUnlock()

	// 2. Slow path: Handle creation or wait for pending
	sm.mu.Lock()
	// Double check
	if sess, ok := sm.sessions[sessionID]; ok {
		if sess.IsAlive() {
			sm.mu.Unlock()
			sess.Touch()
			return sess, nil
		}
		_ = sm.cleanupSessionLocked(sessionID)
	}

	// Check if already being created
	if ch, ok := sm.pending[sessionID]; ok {
		sm.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ch:
			// Creation finished (success or fail), recurse to check result
			return sm.GetOrCreateSession(ctx, sessionID, cfg)
		}
	}

	// Not being created, start it
	ch := make(chan struct{})
	sm.pending[sessionID] = ch
	sm.mu.Unlock()

	// Ensure we cleanup the pending marker on exit
	defer func() {
		sm.mu.Lock()
		delete(sm.pending, sessionID)
		close(ch)
		sm.mu.Unlock()
	}()

	// startSession is heavy, but now doesn't block other sessionIDs
	sess, err := sm.startSession(ctx, sessionID, cfg)
	if err != nil {
		return nil, err
	}

	sm.mu.Lock()
	sm.sessions[sessionID] = sess
	sm.mu.Unlock()

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
	if ctx.Err() != nil {
		return nil, fmt.Errorf("request context cancelled: %w", ctx.Err())
	}

	sessCtx, cancel := context.WithCancel(context.Background())
	var success bool
	defer func() {
		if !success {
			cancel()
		}
	}()

	startupCtx, startupCancel := context.WithTimeout(ctx, 30*time.Second)
	defer startupCancel()

	startedCh := make(chan error, 1)
	defer close(startedCh)

	go monitorStartup(startupCtx, startedCh, cancel)

	uniqueStr := fmt.Sprintf("%s:session:%s", sm.opts.Namespace, sessionID)
	ccSessionID := uuid.NewSHA1(uuid.NameSpaceURL, []byte(uniqueStr)).String()
	sessLog := sm.logger.With(
		"namespace", sm.opts.Namespace,
		"session_id", sessionID,
		"cc_session_id", ccSessionID,
	)

	args := sm.buildCLIArgs(ccSessionID, sessLog)
	cmd := exec.CommandContext(sessCtx, sm.cliPath, args...)
	cmd.Dir = cfg.WorkDir
	cmd.Env = append(os.Environ(), "CLAUDE_DISABLE_TELEMETRY=1")
	setupCmdSysProcAttr(cmd)

	stdin, stdout, stderr, err := setupCmdPipes(cmd)
	if err != nil {
		cancel()
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		startedCh <- err
		return nil, fmt.Errorf("cmd start: %w", err)
	}

	startedCh <- nil

	sessLog.Info("OS Process started (Cold Start)",
		"pid", cmd.Process.Pid,
		"pgid", cmd.Process.Pid)

	sess := &Session{
		ID:           sessionID,
		CCSessionID:  ccSessionID,
		Config:       cfg,
		cmd:          cmd,
		stdin:        stdin,
		stdout:       stdout,
		stderr:       stderr,
		cancel:       cancel,
		CreatedAt:    time.Now(),
		LastActive:   time.Now(),
		Status:       SessionStatusStarting,
		statusChange: make(chan SessionStatus, 10), // Buffered to prevent deadlock
		logger:       sessLog,
	}

	go sess.readStdout()
	go sess.readStderr()

	go func() {
		err := cmd.Wait()
		if sess.GetStatus() != SessionStatusDead && sessLog != nil {
			sessLog.Warn("Session OS process exited unexpectedly", "exit_error", err)
		}
	}()

	sess.waitForReady(sessCtx, defaultReadyTimeout)
	success = true
	return sess, nil
}

func (sm *SessionPool) buildCLIArgs(ccSessionID string, sessLog *slog.Logger) []string {
	args := []string{
		"--print",
		"--verbose",
		"--output-format", "stream-json",
		"--input-format", "stream-json",
	}

	markerPath := filepath.Join(sm.markerDir, ccSessionID+".lock")
	if _, err := os.Stat(markerPath); err == nil {
		args = append(args, "--resume", ccSessionID)
		sessLog.Info("Resuming existing persistent CLI session")
	} else {
		args = append(args, "--session-id", ccSessionID)
		_ = os.WriteFile(markerPath, []byte(""), 0644)
		sessLog.Info("Creating new persistent CLI session")
	}

	if sm.opts.PermissionMode != "" {
		args = append(args, "--permission-mode", sm.opts.PermissionMode)
	}
	if len(sm.opts.AllowedTools) > 0 {
		args = append(args, "--allowed-tools", strings.Join(sm.opts.AllowedTools, ","))
	}
	if len(sm.opts.DisallowedTools) > 0 {
		args = append(args, "--disallowed-tools", strings.Join(sm.opts.DisallowedTools, ","))
	}
	if sm.opts.BaseSystemPrompt != "" {
		args = append(args, "--append-system-prompt", sm.opts.BaseSystemPrompt)
	}

	return args
}

func setupCmdPipes(cmd *exec.Cmd) (io.WriteCloser, io.ReadCloser, io.ReadCloser, error) {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, nil, nil, fmt.Errorf("stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = stdout.Close()
		_ = stdin.Close()
		return nil, nil, nil, fmt.Errorf("stderr pipe: %w", err)
	}

	return stdin, stdout, stderr, nil
}

func monitorStartup(startupCtx context.Context, startedCh <-chan error, cancel context.CancelFunc) {
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
			cancel()
		}
	}
}

// isAliveLocked checks if the process is still running. Caller must hold lock.
// Note: We intentionally do NOT check cmd.ProcessState here to avoid data race
// with cmd.Wait() which writes to ProcessState in a separate goroutine.
// The isProcessAlive() function uses process.Signal(0) which is thread-safe.
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
	s.Status = status
	// Check if channel is closed before sending (#11)
	if s.closed {
		s.mu.Unlock()
		return
	}
	// Non-blocking send to statusChange channel
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

// waitForReady monitors the session and transitions from Starting to Ready
// when the process is confirmed alive and responsive.
// The context parameter allows cancellation if the session is terminated early.
func (s *Session) waitForReady(ctx context.Context, timeout time.Duration) {
	go func() {
		// Use a temporary timer for the overall deadline
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
	}()
}

// WriteInput injects a JSON message to Stdin.
// Transitions session to Busy during write, back to Ready after completion.
func (s *Session) WriteInput(msg map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Set status to Busy while processing input
	s.Status = SessionStatusBusy
	// Broadcast status change immediately
	select {
	case s.statusChange <- SessionStatusBusy:
	default:
	}

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
// Must be called with session lock held (prevents deadlock with SetStatus).
func (s *Session) close() {
	// Set status directly to avoid deadlock - caller already holds s.mu.Lock()
	s.Status = SessionStatusDead

	// Close statusChange channel to signal all waiting goroutines (#11)
	// Use closed flag to prevent double close panic
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

// isExpectedCloseError checks if the error is an expected pipe closure during normal shutdown.
func isExpectedCloseError(err error) bool {
	if err == nil {
		return false
	}
	// Check for common pipe closure errors
	if errors.Is(err, io.EOF) {
		return true
	}
	// "read |0: file already closed" - pipe closed during session termination
	if strings.Contains(err.Error(), "file already closed") {
		return true
	}
	return false
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
		// Note: "file already closed" is expected during normal session shutdown
		if err := scanner.Err(); err != nil && !isExpectedCloseError(err) {
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

	if err := scanner.Err(); err != nil && s.logger != nil && !isExpectedCloseError(err) {
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

	if err := scanner.Err(); err != nil && s.logger != nil && !isExpectedCloseError(err) {
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
