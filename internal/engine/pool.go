package engine

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/hrygo/hotplex/internal/panicx"
	"github.com/hrygo/hotplex/internal/persistence"
	"github.com/hrygo/hotplex/internal/sys"
	"github.com/hrygo/hotplex/provider"
)

// SessionPool implements the SessionManager as a thread-safe singleton.
// It orchestrates the lifecycle of multiple CLI processes, ensuring that
// idle processes are garbage collected to conserve system memory.
type SessionPool struct {
	sessions      map[string]*Session
	mu            sync.RWMutex
	logger        *slog.Logger
	timeout       time.Duration // Time after which an idle session is eligible for termination
	opts          EngineOptions // Global constraints shared by all sessions in the pool
	cliPath       string        // Resolved path to the CLI binary
	provider      provider.Provider
	done          chan struct{} // Internal signal for shutting down background workers
	shutdownOnce  sync.Once     // Ensures Shutdown is only executed once
	markerStore   persistence.SessionMarkerStore
	pending       map[string]chan struct{}
	resetSessions map[string]bool // Sessions that need new ProviderSessionID on restart (for /clear)
}

// blockedEnvPrefixes contains environment variable prefixes that should be filtered
// out for security reasons to prevent injection attacks via environment variables.

// NewSessionPool creates a new session manager with default file-based marker storage.
func NewSessionPool(logger *slog.Logger, timeout time.Duration, opts EngineOptions, cliPath string, prv provider.Provider) *SessionPool {
	if logger == nil {
		logger = slog.Default()
	}

	sm := &SessionPool{
		sessions:    make(map[string]*Session),
		logger:      logger,
		timeout:     timeout,
		opts:        opts,
		cliPath:     cliPath,
		provider:    prv,
		done:        make(chan struct{}),
		markerStore: persistence.NewDefaultFileMarkerStore(),
		pending:     make(map[string]chan struct{}),
	}

	// Start idle session cleanup goroutine
	go sm.cleanupLoop()

	return sm
}

// GetOrCreateSession returns an existing session or starts a new one.
func (sm *SessionPool) GetOrCreateSession(ctx context.Context, sessionID string, cfg SessionConfig, prompt string) (*Session, bool, error) {
	// 1. Check existing
	sm.mu.RLock()
	if sess, ok := sm.sessions[sessionID]; ok {
		if sess.IsAlive() {
			sm.mu.RUnlock()
			sess.Touch()
			return sess, false, nil
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
			return sess, false, nil
		}
		_ = sm.cleanupSessionLocked(sessionID)
	}

	// Check if already being created
	if ch, ok := sm.pending[sessionID]; ok {
		sm.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, false, ctx.Err()
		case <-ch:
			// Creation finished, recurse to check result
			return sm.GetOrCreateSession(ctx, sessionID, cfg, prompt)
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
	sess, err := sm.startSession(ctx, sessionID, cfg, prompt)
	if err != nil {
		return nil, false, err
	}

	sm.mu.Lock()
	sm.sessions[sessionID] = sess
	sm.mu.Unlock()

	return sess, true, nil
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

// ResetProviderSessionID marks a session to get a new ProviderSessionID on restart.
// This is used for /clear command to force a fresh session with new context.
// The ProviderSessionID will be regenerated as a random UUID instead of deterministic SHA1.
func (sm *SessionPool) ResetProviderSessionID(sessionID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.resetSessions[sessionID] = true
	sm.logger.Debug("Marked session for ProviderSessionID reset",
		"session_id", sessionID)
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

// DeleteMarker removes the HotPlex session marker file, preventing future resumption.
func (sm *SessionPool) DeleteMarker(providerSessionID string) error {
	if providerSessionID == "" {
		return nil
	}
	return sm.markerStore.Delete(providerSessionID)
}

// CleanupSessionFiles proxies the cleanup call to the underlying provider.
func (sm *SessionPool) CleanupSessionFiles(providerSessionID string, workDir string) error {
	if sm.provider != nil {
		return sm.provider.CleanupSession(providerSessionID, workDir)
	}
	return nil
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
		"provider_session_id", sess.ProviderSessionID)

	// Hold session lock to prevent race with WriteInput
	sess.mu.Lock()
	sess.close()
	sess.mu.Unlock()

	// Cancel context to kill process if using CommandContext
	if sess.cancel != nil {
		sess.cancel()
	}

	// Force kill if needed (pass jobHandle for Windows Job Object termination)
	sys.KillProcessGroup(sess.cmd, sess.jobHandle)

	return nil
}

// startSession initializes the OS process (Cold Start).
func (sm *SessionPool) startSession(ctx context.Context, sessionID string, cfg SessionConfig, prompt string) (*Session, error) {
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

	// Check if session needs new ProviderSessionID (for /clear command or failed resume)
	var providerSessionID string
	var oldProviderSessionID string // Track old ID for cleanup
	if sm.resetSessions[sessionID] {
		// Calculate old ID for cleanup
		oldProviderSessionID = uuid.NewSHA1(uuid.NameSpaceURL, []byte(uniqueStr)).String()

		// Generate random UUID for fresh session
		providerSessionID = uuid.New().String()
		delete(sm.resetSessions, sessionID) // Clear the flag
		sm.logger.Info("Generated new random ProviderSessionID for reset session",
			"session_id", sessionID,
			"old_provider_session_id", oldProviderSessionID,
			"new_provider_session_id", providerSessionID)

		// Cleanup old marker and CLI session files to prevent "Session ID is already in use"
		_ = sm.markerStore.Delete(oldProviderSessionID)
		if err := sm.provider.CleanupSession(oldProviderSessionID, cfg.WorkDir); err != nil {
			sm.logger.Warn("Failed to cleanup old CLI session after reset", "error", err)
		}
	} else {
		// Use deterministic SHA1 for consistent session resumption
		providerSessionID = uuid.NewSHA1(uuid.NameSpaceURL, []byte(uniqueStr)).String()
	}
	sessLog := sm.logger.With(
		"namespace", sm.opts.Namespace,
		"session_id", sessionID,
		"provider_session_id", providerSessionID,
	)

	// Check if this is a resume BEFORE building CLI args
	// This is critical because buildCLIArgs may create the marker
	isResuming := sm.markerStore.Exists(providerSessionID)

	args := sm.buildCLIArgs(providerSessionID, sessLog, prompt, cfg)
	cmd := exec.CommandContext(sessCtx, sm.cliPath, args...)

	// Clear CLAUDECODE env var to allow nested CLI sessions
	// CLI refuses to start if it detects it's running inside another Claude Code session
	cmd.Env = slices.DeleteFunc(slices.Clone(os.Environ()), func(env string) bool {
		return strings.HasPrefix(env, "CLAUDECODE=")
	})

	// Resolve relative paths (like ".") to absolute paths
	// First clean the path to resolve . and .. elements, then convert to absolute
	if cfg.WorkDir == "." || !filepath.IsAbs(cfg.WorkDir) {
		cleaned := filepath.Clean(cfg.WorkDir)
		if absPath, err := filepath.Abs(cleaned); err == nil {
			cmd.Dir = absPath
		} else {
			cmd.Dir = cleaned // Fallback to cleaned path if error
		}
	} else {
		// For absolute paths, also clean to resolve . and .. elements
		cmd.Dir = filepath.Clean(cfg.WorkDir)
	}

	// Setup process attributes and get job handle (Windows) or zero (Unix)
	jobHandle, err := sys.SetupCmdSysProcAttr(cmd)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("setup sys proc attr: %w", err)
	}

	stdin, stdout, stderr, err := setupCmdPipes(cmd)
	if err != nil {
		cancel()
		sys.CloseJobHandle(jobHandle) // Cleanup job handle on error
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		startedCh <- err
		sys.CloseJobHandle(jobHandle) // Cleanup job handle on error
		return nil, fmt.Errorf("cmd start: %w", err)
	}

	// Assign process to Job Object on Windows
	if jobHandle != 0 {
		if err := sys.AssignProcessToJob(jobHandle, cmd.Process); err != nil {
			sessLog.Warn("failed to assign process to Job Object", "error", err)
			// Continue anyway - process is still running, will be killed via taskkill fallback
		}
	}

	// Create marker AFTER CLI process successfully starts
	// This prevents creating markers for failed session starts
	if !isResuming {
		if err := sm.markerStore.Create(providerSessionID); err != nil {
			sessLog.Warn("Failed to create session marker after successful start", "error", err)
			// Don't fail the session - marker is optional for operation
		} else {
			sessLog.Info("Created session marker after successful CLI start", "provider_session_id", providerSessionID)
		}
	}

	startedCh <- nil

	sessLog.Info("OS Process started (Cold Start)",
		"pid", cmd.Process.Pid,
		"pgid", cmd.Process.Pid)

	sess := &Session{
		ID:                sessionID,
		ProviderSessionID: providerSessionID,
		Config:            cfg,
		cmd:               cmd,
		stdin:             stdin,
		stdout:            stdout,
		stderr:            stderr,
		cancel:            cancel,
		jobHandle:         jobHandle,
		CreatedAt:         time.Now(),
		LastActive:        time.Now(),
		Status:            SessionStatusStarting,
		TaskInstructions:  cfg.TaskInstructions,
		statusChange:      make(chan SessionStatus, 10),
		logger:            sessLog,
		IsResuming:        isResuming,
	}

	// Open session log file for stderr persistence
	if err := sess.OpenLogFile(); err != nil {
		sessLog.Warn("Failed to open session log file", "error", err)
	}

	go sess.ReadStdout()
	go sess.ReadStderr()

	panicx.SafeGo(sessLog, func() {
		err := cmd.Wait()
		if sess.GetStatus() != SessionStatusDead {
			sessLog.Warn("Session OS process exited unexpectedly", "exit_error", err, "is_resuming", isResuming)
			// If this was a resume attempt that failed, delete the stale marker and CLI session file
			// This allows the next request to create a fresh session instead of retrying with a dead session
			if isResuming {
				// Delete marker first
				if delErr := sm.markerStore.Delete(providerSessionID); delErr != nil {
					sessLog.Warn("Failed to delete stale session marker", "error", delErr)
				} else {
					sessLog.Info("Deleted stale session marker after failed resume", "provider_session_id", providerSessionID)
				}
				// Also clean up the CLI session file to prevent "Session ID is already in use" errors
				// This is critical because the CLI's internal session state may conflict on next retry
				if cleanupErr := sm.provider.CleanupSession(providerSessionID, sess.Config.WorkDir); cleanupErr != nil {
					sessLog.Warn("Failed to cleanup CLI session file after failed resume", "error", cleanupErr)
				} else {
					sessLog.Info("Cleaned up CLI session file after failed resume", "provider_session_id", providerSessionID)
				}
				// Mark this session for fresh provider_session_id regeneration on next retry
				// This ensures we get a completely new CLI session instead of retrying with the same ID
				sm.mu.Lock()
				sm.resetSessions[sessionID] = true
				sm.mu.Unlock()
				sessLog.Info("Marked session for fresh ProviderSessionID after failed resume", "session_id", sessionID)
			}
			// Update status to Dead and notify callback
			sess.SetStatus(SessionStatusDead)
			if cb := sess.GetCallback(); cb != nil {
				_ = cb("runner_exit", nil)
			}
		}
	})

	sess.waitForReady(sessCtx, DefaultReadyTimeout)
	success = true
	return sess, nil
}

func (sm *SessionPool) buildCLIArgs(providerSessionID string, sessLog *slog.Logger, prompt string, cfg SessionConfig) []string {
	// Build ProviderSessionOptions
	opts := &provider.ProviderSessionOptions{
		WorkDir:                    cfg.WorkDir,
		PermissionMode:             sm.opts.PermissionMode,
		DangerouslySkipPermissions: sm.opts.DangerouslySkipPermissions,
		AllowedTools:               sm.opts.AllowedTools,
		DisallowedTools:            sm.opts.DisallowedTools,
		BaseSystemPrompt:           sm.opts.BaseSystemPrompt,
		TaskInstructions:           cfg.TaskInstructions,
		InitialPrompt:              prompt,
		SessionID:                  providerSessionID,
	}

	// Check if we need to resume using marker store
	if sm.markerStore.Exists(providerSessionID) {
		opts.ResumeSession = true
		opts.ProviderSessionID = providerSessionID
		sessLog.Info("Resuming existing persistent CLI session")
	} else {
		opts.ProviderSessionID = providerSessionID

		// Critical: Delete stale CLI session file before starting new session
		// This prevents "Session ID is already in use" errors when:
		// - /reset deleted the marker but not the CLI session file (old bug)
		// - Daemon restart with stale CLI session files on disk
		if err := sm.provider.CleanupSession(providerSessionID, cfg.WorkDir); err != nil {
			sessLog.Warn("Failed to cleanup stale CLI session file", "error", err)
		}

		sessLog.Info("Creating new persistent CLI session")
		// NOTE: Marker will be created AFTER CLI successfully starts in startSession()
	}

	return sm.provider.BuildCLIArgs(providerSessionID, opts)
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
		idleTime := now.Sub(sess.GetLastActive())
		if idleTime > sm.timeout {
			sm.logger.Info("Session idle timeout, terminating",
				"namespace", sm.opts.Namespace,
				"session_id", sessionID,
				"provider_session_id", sess.ProviderSessionID,
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

	// Collect callbacks to invoke outside of locks to prevent deadlock
	type callbackEntry struct {
		cb      Callback
		sessLog *slog.Logger
	}
	callbacks := make([]callbackEntry, 0, len(sm.sessions))

	// Mark all sessions as Dead and collect callbacks
	for _, sess := range sm.sessions {
		sess.mu.Lock()
		sess.Status = SessionStatusDead
		if sess.callback != nil {
			callbacks = append(callbacks, callbackEntry{cb: sess.callback, sessLog: sess.logger})
		}
		sess.mu.Unlock()
	}

	// Release pool lock before invoking callbacks
	sm.mu.Unlock()

	// Invoke callbacks outside of locks
	for _, entry := range callbacks {
		if err := entry.cb("runner_exit", nil); err != nil && entry.sessLog != nil {
			entry.sessLog.Debug("Shutdown: callback error", "error", err)
		}
	}

	// Re-acquire lock for cleanup
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Terminate all sessions
	for sessionID := range sm.sessions {
		_ = sm.cleanupSessionLocked(sessionID)
	}
}
