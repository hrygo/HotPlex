package engine

import (
	"context"
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
	"github.com/hrygo/hotplex/internal/sys"
)

// SessionPool implements the SessionManager as a thread-safe singleton.
// It orchestrates the lifecycle of multiple CLI processes, ensuring that
// idle processes are garbage collected to conserve system memory.
type SessionPool struct {
	sessions     map[string]*Session
	mu           sync.RWMutex
	logger       *slog.Logger
	timeout      time.Duration // Time after which an idle session is eligible for termination
	opts         EngineOptions // Global constraints shared by all sessions in the pool
	cliPath      string        // Resolved path to the CLI binary
	done         chan struct{} // Internal signal for shutting down background workers
	shutdownOnce sync.Once     // Ensures Shutdown is only executed once
	markerDir    string        // Local filesystem path storing session persistence markers
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

	// Start idle session cleanup goroutine
	go sm.cleanupLoop()

	return sm
}

// GetOrCreateSession returns an existing session or starts a new one.
func (sm *SessionPool) GetOrCreateSession(ctx context.Context, sessionID string, cfg SessionConfig) (*Session, error) {
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
			// Creation finished, recurse to check result
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

	// Hold session lock to prevent race with WriteInput
	sess.mu.Lock()
	sess.close()
	sess.mu.Unlock()

	// Cancel context to kill process if using CommandContext
	if sess.cancel != nil {
		sess.cancel()
	}

	// Force kill if needed
	sys.KillProcessGroup(sess.cmd)

	return nil
}

// startSession initializes the OS process (Cold Start).
func (sm *SessionPool) startSession(ctx context.Context, sessionID string, cfg SessionConfig) (*Session, error) {
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
	sys.SetupCmdSysProcAttr(cmd)

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
		statusChange: make(chan SessionStatus, 10),
		logger:       sessLog,
	}

	go sess.ReadStdout()
	go sess.ReadStderr()

	go func() {
		err := cmd.Wait()
		if sess.GetStatus() != SessionStatusDead && sessLog != nil {
			sessLog.Warn("Session OS process exited unexpectedly", "exit_error", err)
		}
	}()

	sess.waitForReady(sessCtx, DefaultReadyTimeout)
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

// cleanupLoop runs periodic cleanup of idle sessions.
func (sm *SessionPool) cleanupLoop() {
	ticker := time.NewTicker(CleanupCheckInterval)
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
			_ = sm.cleanupSessionLocked(sessionID)
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

	// Mark all sessions as Dead and signal runner_exit
	for _, sess := range sm.sessions {
		sess.mu.Lock()
		sess.Status = SessionStatusDead
		if sess.callback != nil {
			_ = sess.callback("runner_exit", nil)
		}
		sess.mu.Unlock()
	}

	// Terminate all sessions
	for sessionID := range sm.sessions {
		_ = sm.cleanupSessionLocked(sessionID)
	}
}
