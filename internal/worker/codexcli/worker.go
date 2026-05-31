package codexcli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/internal/worker/base"
	"github.com/hrygo/hotplex/internal/worker/proc"
	"github.com/hrygo/hotplex/pkg/events"
)

var _ worker.Worker = (*ExecWorker)(nil)

func init() {
	worker.Register(worker.TypeCodexCLI, func() (worker.Worker, error) {
		cfg := GetConfig()
		if cfg.UseAppServer {
			s := GetSingleton()
			if s == nil {
				return nil, fmt.Errorf("codexcli: app-server singleton not initialized")
			}
			return &AppServerWorker{
				BaseWorker: base.NewBaseWorker(slog.Default(), nil),
				manager:    s,
			}, nil
		}
		return &ExecWorker{BaseWorker: base.NewBaseWorker(slog.Default(), nil)}, nil
	})
}

type ExecWorker struct {
	*base.BaseWorker

	cfg         Config
	mu          sync.Mutex
	started     bool
	sessionID   string
	projectDir  string
	origSession worker.SessionInfo

	threadID string

	parser *Parser
	mapper *Mapper
	cancel context.CancelFunc

	readLineFn func() (string, error)
	testConn   worker.SessionConn
}

type Config struct {
	Command          string
	Model            string
	Sandbox          string
	ApprovalMode     string
	Ephemeral        bool
	Personality      string
	StartupTimeout   time.Duration
	CallTimeout      time.Duration
	Color            bool
	OutputFile       string
	StrictConfig     bool
	SkipGitRepoCheck bool
	IgnoreUserConfig bool
	IgnoreRules      bool
	LocalProvider    bool
	ConfigProfile    string
	BypassHookTrust  bool
}

func (w *ExecWorker) Type() worker.WorkerType { return worker.TypeCodexCLI }
func (w *ExecWorker) SupportsResume() bool    { return true }
func (w *ExecWorker) SupportsStreaming() bool { return true }
func (w *ExecWorker) SupportsTools() bool     { return true }
func (w *ExecWorker) EnvBlocklist() []string  { return EnvBlocklist }
func (w *ExecWorker) SessionStoreDir() string { return "" }
func (w *ExecWorker) MaxTurns() int           { return 0 }
func (w *ExecWorker) Modalities() []string    { return []string{"text", "code", "image"} }

func (w *ExecWorker) Start(ctx context.Context, session worker.SessionInfo) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.startLocked(session)
}

func (w *ExecWorker) startLocked(session worker.SessionInfo) error {
	if w.started {
		return fmt.Errorf("codexcli: already started")
	}

	w.cfg = resolveConfig()
	if w.cfg.Sandbox == "" || w.cfg.Sandbox == "danger-full-access" {
		w.Log.Warn("codexcli: sandbox is not restricted; commands run with full permissions", "sandbox", w.cfg.Sandbox)
	}
	w.sessionID = session.SessionID
	w.projectDir = session.ProjectDir

	if w.origSession.SessionID == "" {
		w.origSession = session
	}

	w.parser = NewParser()
	w.mapper = NewMapper(session.SessionID)

	w.started = true
	return nil
}

func resolveConfig() Config {
	gc := GetConfig()
	return Config{
		Command:          gc.Command,
		Model:            gc.Model,
		Sandbox:          gc.Sandbox,
		ApprovalMode:     gc.ApprovalMode,
		Ephemeral:        gc.Ephemeral,
		Personality:      gc.Personality,
		StartupTimeout:   gc.StartupTimeout,
		CallTimeout:      gc.CallTimeout,
		Color:            gc.Color,
		OutputFile:       gc.OutputFile,
		StrictConfig:     gc.StrictConfig,
		SkipGitRepoCheck: gc.SkipGitRepoCheck,
		IgnoreUserConfig: gc.IgnoreUserConfig,
		IgnoreRules:      gc.IgnoreRules,
		LocalProvider:    gc.LocalProvider,
		ConfigProfile:    gc.ConfigProfile,
		BypassHookTrust:  gc.BypassHookTrust,
	}
}

func (w *ExecWorker) buildArgs(session worker.SessionInfo, prompt string) []string {
	// Session-aware approval: session.SkipPermissions overrides config default.
	approvalMode := w.cfg.ApprovalMode
	if session.SkipPermissions {
		approvalMode = "never"
	}

	args := []string{
		"exec", "--json",
		"--sandbox", sandboxFromSession(session, w.cfg.Sandbox),
		"--ask-for-approval", approvalMode,
		"--cd", session.ProjectDir,
	}

	for _, img := range session.Images {
		args = append(args, "--image", img)
	}

	if session.JSONSchema != "" {
		args = append(args, "--output-schema", session.JSONSchema)
	}

	for _, dir := range session.AllowedDirs {
		args = append(args, "--add-dir", dir)
	}

	if w.cfg.Color {
		args = append(args, "--color")
	}

	if w.cfg.OutputFile != "" {
		args = append(args, "--output-last-message", w.cfg.OutputFile)
	}

	if w.cfg.StrictConfig {
		args = append(args, "--strict-config")
	}

	if w.cfg.SkipGitRepoCheck {
		args = append(args, "--skip-git-repo-check")
	}

	if w.cfg.IgnoreUserConfig {
		args = append(args, "--ignore-user-config")
	}

	if w.cfg.IgnoreRules {
		args = append(args, "--ignore-rules")
	}

	if w.cfg.LocalProvider {
		args = append(args, "--local-provider")
	}

	if w.cfg.ConfigProfile != "" {
		args = append(args, "--profile", w.cfg.ConfigProfile)
	}

	if w.cfg.BypassHookTrust {
		args = append(args, "--dangerously-bypass-hook-trust")
	}

	if w.cfg.Ephemeral {
		args = append(args, "--ephemeral")
	}

	if w.cfg.Model != "" {
		args = append(args, "-m", w.cfg.Model)
	}

	if session.ResumeSessionID != "" {
		args = append(args, "resume", session.ResumeSessionID)
	} else if w.threadID != "" {
		args = append(args, "resume", w.threadID)
	}

	if prompt != "" {
		args = append(args, prompt)
	}

	return args
}

func (w *ExecWorker) spawn(ctx context.Context, prompt string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.Proc != nil {
		return fmt.Errorf("%w: codex exec is one-shot per process; use Resume for follow-up",
			worker.ErrNotImplemented)
	}

	session := w.origSession
	if session.SessionID == "" {
		session.SessionID = w.sessionID
		session.ProjectDir = w.projectDir
	}

	args := w.buildArgs(session, prompt)

	w.Proc = proc.New(proc.Opts{
		Logger:       w.Log,
		AllowedTools: session.AllowedTools,
	})
	w.Proc.SetPIDKey(session.SessionID)

	env := base.BuildEnv(session, w.EnvBlocklist(), "codex-cli")

	timeout := w.cfg.StartupTimeout
	if timeout <= 0 {
		timeout = defaultStartupTimeout
	}
	startCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	stdin, stdout, _, err := w.Proc.Start(startCtx, w.cfg.Command, args, env, session.ProjectDir)
	if err != nil {
		w.Proc = nil
		return fmt.Errorf("codexcli: start: %w", err)
	}

	childCtx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel

	if w.readLineFn == nil {
		w.readLineFn = w.Proc.ReadLine
	}

	conn := base.NewConn(w.Log, stdin, session.UserID, session.SessionID)
	w.SetConnLocked(conn)

	w.StartTime = time.Now()
	w.SetLastIO(w.StartTime)

	go w.readOutput(childCtx, stdout, conn)
	return nil
}

func (w *ExecWorker) Input(ctx context.Context, content string, metadata map[string]any) error {
	handled, err := base.DispatchMetadata(ctx, metadata, w)
	if err != nil {
		return err
	}
	if handled {
		w.SetLastIO(time.Now())
		return nil
	}

	w.mu.Lock()
	if w.Proc != nil {
		w.mu.Unlock()
		return fmt.Errorf("%w: codex exec is one-shot per process; use Resume for follow-up",
			worker.ErrNotImplemented)
	}
	w.mu.Unlock()

	return w.spawn(ctx, content)
}

func (w *ExecWorker) Resume(ctx context.Context, session worker.SessionInfo) error {
	w.mu.Lock()
	w.started = false
	w.threadID = ""
	w.mu.Unlock()

	if err := w.BaseWorker.Terminate(ctx); err != nil {
		w.Log.Warn("codexcli: resume terminate", "error", err)
	}

	return w.Start(ctx, session)
}

func (w *ExecWorker) ResetContext(ctx context.Context) error {
	if err := w.BaseWorker.Terminate(ctx); err != nil {
		w.Log.Warn("codexcli: reset terminate", "error", err)
	}
	w.mu.Lock()
	w.started = false
	w.threadID = ""
	w.readLineFn = nil
	w.mu.Unlock()
	return nil
}

func (w *ExecWorker) Terminate(ctx context.Context) error {
	if w.cancel != nil {
		w.cancel()
	}
	return w.BaseWorker.Terminate(ctx)
}

func (w *ExecWorker) Conn() worker.SessionConn {
	if w.testConn != nil {
		return w.testConn
	}
	return w.BaseWorker.Conn()
}

func (w *ExecWorker) Health() worker.WorkerHealth {
	return w.BaseWorker.Health(worker.TypeCodexCLI)
}

func (w *ExecWorker) LastIO() time.Time {
	return w.BaseWorker.LastIO()
}

func (w *ExecWorker) readOutput(ctx context.Context, stdout io.Reader, entryConn *base.Conn) {
	defer func() {
		if r := recover(); r != nil {
			w.Log.Error("codexcli: readOutput panic",
				"session_id", w.sessionID, "panic", r)
		}
		if entryConn != nil {
			_ = entryConn.Close()
		}
	}()
	// Drain remaining stdout data in background so the pipe writer (child
	// process) is never blocked. The goroutine exits when proc.Manager.Close
	// closes the read end of the pipe.
	defer func() {
		go func() { _, _ = io.Copy(io.Discard, stdout) }()
	}()

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)

	var turnFailed bool

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line := scanner.Text()
		if line == "" {
			continue
		}

		event, err := w.parser.ParseLine(line)
		if err != nil {
			w.Log.Warn("codexcli: parse error", "line", line, "error", err)
			continue
		}

		if event.Type == EventThreadStarted {
			w.mu.Lock()
			w.threadID = event.ThreadID
			w.mu.Unlock()
		}

		envelopes := w.mapper.Map(event)
		for _, env := range envelopes {
			if env == nil {
				continue
			}
			w.trySend(env)
		}

		switch event.Type {
		case EventTurnFailed:
			turnFailed = true
		case EventTurnCompleted:
			return
		case EventError:
			if !turnFailed {
				return
			}
		}
	}

	if err := scanner.Err(); err != nil {
		w.Log.Error("codexcli: stdout read error", "error", err)
	}
}

func (w *ExecWorker) trySend(env *events.Envelope) {
	conn := w.Conn()
	if conn == nil {
		return
	}
	switch env.Event.Type {
	case events.Done, events.Error, events.State:
		sendCtx, cancel := context.WithTimeout(context.Background(), criticalEventSendTimeout)
		_ = conn.Send(sendCtx, env)
		cancel()
	default:
		ts, ok := conn.(interface{ TrySend(*events.Envelope) bool })
		if !ok {
			return
		}
		if !ts.TrySend(env) {
			w.Log.Warn("codexcli: recv channel full, dropping",
				"session_id", w.sessionID, "event_type", env.Event.Type)
		}
	}
}

func (w *ExecWorker) HandlePermissionResponse(_ context.Context, reqID string, allowed bool, reason string) error {
	return fmt.Errorf("codexcli: permission responses not supported in one-shot mode")
}

func (w *ExecWorker) HandleQuestionResponse(_ context.Context, reqID string, answers map[string]string) error {
	return fmt.Errorf("codexcli: question responses not supported in one-shot mode")
}

func (w *ExecWorker) HandleElicitationResponse(_ context.Context, reqID, action string, content map[string]any) error {
	return fmt.Errorf("codexcli: elicitation responses not supported in one-shot mode")
}

func (w *ExecWorker) Compact(ctx context.Context, args map[string]any) error {
	return fmt.Errorf("codexcli: compact not supported in exec mode; use app-server mode")
}

func (w *ExecWorker) Clear(ctx context.Context) error {
	return fmt.Errorf("codexcli: clear not supported in exec mode; use app-server mode")
}

func (w *ExecWorker) Rewind(ctx context.Context, targetID string) error {
	return fmt.Errorf("codexcli: rewind not supported in exec mode; use app-server mode")
}

func (w *ExecWorker) SetTestConn(c worker.SessionConn) {
	w.testConn = c
}

func (w *ExecWorker) SetReadLineFn(fn func() (string, error)) {
	w.readLineFn = fn
}

// ─── AppServerWorker (v2 persistent mode) ───────────────────────────────

var _ worker.Worker = (*AppServerWorker)(nil)
var _ worker.WorkerCommander = (*AppServerWorker)(nil)
var _ worker.ControlRequester = (*AppServerWorker)(nil)

type AppServerWorker struct {
	*base.BaseWorker

	manager     *CodexAppServerManager
	threadID    string
	turnID      string
	userID      string
	releaseOnce sync.Once
	crashSub    <-chan struct{}
	doneCh      chan struct{}
	mu          sync.Mutex
	recvCh      chan *events.Envelope
	commands    *ServerCommander
	closed      bool
	started     bool
	sessionID   string
	conn        *appConn

	// origSession preserves the SessionInfo from the most recent Start()
	// call so that ResetContext can re-establish a fresh thread after cleanup.
	origSession worker.SessionInfo
}

// appConn implements worker.SessionConn for the app-server mode.
type appConn struct {
	userID    string
	sessionID string
	recvCh    chan *events.Envelope
	mu        sync.Mutex
	closed    bool
	manager   *CodexAppServerManager
}

// Send returns ErrNotImplemented because in app-server mode the manager
// handles all communication via JSON-RPC. Writing AEP envelopes directly
// to stdin would bypass the JSON-RPC protocol and break the codex process.
func (c *appConn) Send(ctx context.Context, msg *events.Envelope) error {
	return worker.ErrNotImplemented
}
func (c *appConn) Recv() <-chan *events.Envelope { return c.recvCh }
func (c *appConn) TrySend(env *events.Envelope) bool {
	select {
	case c.recvCh <- env:
		return true
	default:
		return false
	}
}
func (c *appConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	close(c.recvCh)
	return nil
}
func (c *appConn) UserID() string    { return c.userID }
func (c *appConn) SessionID() string { return c.sessionID }

func (w *AppServerWorker) Type() worker.WorkerType { return worker.TypeCodexCLI }
func (w *AppServerWorker) SupportsResume() bool    { return true }
func (w *AppServerWorker) SupportsStreaming() bool { return true }
func (w *AppServerWorker) SupportsTools() bool     { return true }
func (w *AppServerWorker) EnvBlocklist() []string  { return EnvBlocklist }
func (w *AppServerWorker) SessionStoreDir() string { return "" }
func (w *AppServerWorker) MaxTurns() int           { return 0 }
func (w *AppServerWorker) Modalities() []string    { return []string{"text", "code", "image"} }

func (w *AppServerWorker) SendControlRequest(ctx context.Context, subtype string, body map[string]any) (map[string]any, error) {
	return w.commands.SendControlRequest(ctx, subtype, body)
}

func (w *AppServerWorker) Start(ctx context.Context, session worker.SessionInfo) error {
	w.mu.Lock()
	if w.recvCh != nil {
		w.mu.Unlock()
		return fmt.Errorf("codexcli: app-server already started")
	}
	// Sentinel: mark started under lock to close TOCTOU window between
	// unlock and re-lock. Bridge serializes Start per session, but this
	// is defensive against future callers.
	w.started = true

	if w.doneCh == nil {
		w.doneCh = make(chan struct{})
	}
	w.mu.Unlock()

	// RPC calls outside lock to avoid blocking other goroutines (P1 fix).
	crashCh, err := w.manager.Acquire(ctx)
	if err != nil {
		// Close doneCh so Wait() does not block on this error path.
		w.mu.Lock()
		w.closeAndMarkDone()
		w.mu.Unlock()
		return fmt.Errorf("codexcli: acquire: %w", err)
	}
	w.mu.Lock()
	w.crashSub = crashCh
	w.mu.Unlock()

	if err := w.startNewThread(session, "start"); err != nil {
		w.manager.Release()
		return err
	}
	return nil
}

func (w *AppServerWorker) Input(ctx context.Context, content string, metadata map[string]any) error {
	handled, err := base.DispatchMetadata(ctx, metadata, w)
	if err != nil {
		return err
	}
	if handled {
		w.SetLastIO(time.Now())
		return nil
	}

	w.mu.Lock()
	tid := w.threadID
	w.mu.Unlock()
	if tid == "" {
		return fmt.Errorf("codexcli: app-server not started")
	}

	params := TurnStartParams{
		ThreadID: tid,
		Input: []TurnInputItem{
			{Type: "text", Text: content},
		},
	}

	resp, err := w.manager.Call("turn/start", params)
	if err != nil {
		return fmt.Errorf("codexcli: turn/start: %w", err)
	}

	var tr struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if err := json.Unmarshal(resp, &tr); err != nil {
		w.Log.Debug("codexcli: turn/start response parse error", "error", err)
	} else if tr.Turn.ID == "" {
		w.Log.Debug("codexcli: turn/start response missing turn.id")
	} else {
		w.mu.Lock()
		w.turnID = tr.Turn.ID
		w.mu.Unlock()
	}

	w.SetLastIO(time.Now())
	return nil
}

// ─── AppServerWorker lifecycle helpers ────────────────────────────────

// closeAndMarkDone closes doneCh and marks the worker as closed.
// This ensures Wait() is always unblocked even on error paths (P1 fix).
// Caller must hold w.mu.
func (w *AppServerWorker) closeAndMarkDone() {
	w.closed = true
	if w.doneCh != nil {
		close(w.doneCh)
		w.doneCh = nil
	}
}

// cleanupOldThread closes the old connection and unsubscribes from the
// old thread. Shared by Resume and ResetContext.
func (w *AppServerWorker) cleanupOldThread() {
	w.mu.Lock()
	oldConn := w.conn
	oldThreadID := w.threadID
	w.recvCh = nil
	w.conn = nil
	w.mu.Unlock()

	if oldConn != nil {
		_ = oldConn.Close()
	}
	if oldThreadID != "" && w.manager != nil {
		_ = w.manager.Notify("thread/unsubscribe", ThreadUnsubscribeParams{ThreadID: oldThreadID})
		w.manager.Unsubscribe(oldThreadID)
	}
}

// resetLifecycleState resets lifecycle state for a new thread attempt.
// After calling this, Terminate/Kill can release the new thread cleanly.
func (w *AppServerWorker) resetLifecycleState() {
	w.mu.Lock()
	w.closed = false
	w.releaseOnce = sync.Once{}
	if w.doneCh == nil {
		w.doneCh = make(chan struct{})
	}
	w.mu.Unlock()
}

// startNewThread starts a fresh thread on the manager and wires up worker
// state. errPrefix is used in error messages for caller identification.
func (w *AppServerWorker) startNewThread(session worker.SessionInfo, errPrefix string) error {
	if w.manager != nil && !w.manager.IsRunning() {
		w.mu.Lock()
		w.closeAndMarkDone()
		w.mu.Unlock()
		return fmt.Errorf("codexcli: %s: manager process not running", errPrefix)
	}

	cfg := resolveConfig()
	params := buildThreadStartParams(session, cfg)

	resp, err := w.manager.Call("thread/start", params)
	if err != nil {
		w.mu.Lock()
		w.closeAndMarkDone()
		w.mu.Unlock()
		return fmt.Errorf("codexcli: %s thread/start: %w", errPrefix, err)
	}

	var result ThreadStartResult
	if err := json.Unmarshal(resp, &result); err != nil {
		w.mu.Lock()
		w.closeAndMarkDone()
		w.mu.Unlock()
		return fmt.Errorf("codexcli: %s parse thread/start: %w", errPrefix, err)
	}

	w.mu.Lock()
	w.threadID = result.Thread.ID
	w.turnID = ""
	w.sessionID = session.SessionID
	w.userID = session.UserID
	w.origSession = session
	w.recvCh = w.manager.Subscribe(result.Thread.ID, session.SessionID)
	w.commands = NewServerCommander(w.manager, result.Thread.ID)
	w.conn = &appConn{
		userID:    session.UserID,
		sessionID: session.SessionID,
		recvCh:    w.recvCh,
		manager:   w.manager,
	}
	w.StartTime = time.Now()
	w.SetLastIO(w.StartTime)
	w.mu.Unlock()

	return nil
}

// ─── AppServerWorker lifecycle ────────────────────────────────────────

func (w *AppServerWorker) Resume(ctx context.Context, session worker.SessionInfo) error {
	w.cleanupOldThread()
	w.resetLifecycleState()
	return w.startNewThread(session, "resume")
}

func (w *AppServerWorker) Terminate(ctx context.Context) error {
	w.shutdown()
	return nil
}

func (w *AppServerWorker) Kill() error {
	w.shutdown()
	return nil
}

// shutdown releases the worker's manager subscription and kills the singleton
// process if no other sessions hold refs. Called by both Terminate and Kill.
func (w *AppServerWorker) shutdown() {
	w.release()
	if w.manager != nil {
		w.manager.KillIfIdle()
	}
}

func (w *AppServerWorker) Wait() (int, error) {
	if w.crashSub == nil {
		return 0, nil
	}
	select {
	case <-w.crashSub:
		return 1, nil
	case <-w.doneCh:
		return 0, nil
	}
}

func (w *AppServerWorker) release() {
	w.releaseOnce.Do(func() {
		w.mu.Lock()
		if w.closed {
			w.mu.Unlock()
			return
		}
		w.closed = true
		doneCh := w.doneCh
		w.doneCh = nil
		tid := w.threadID
		w.mu.Unlock()

		if doneCh != nil {
			close(doneCh)
		}

		if w.manager != nil && tid != "" {
			_ = w.manager.Notify("thread/unsubscribe", ThreadUnsubscribeParams{
				ThreadID: tid,
			})
			w.manager.Unsubscribe(tid)
			// Close recvCh so forwardEvents exits its range loop.
			// Must happen after Unsubscribe (removes from dispatch map) to
			// avoid racing with in-flight dispatchNotification sends.
			if w.conn != nil {
				_ = w.conn.Close()
			}
			w.manager.Release()
		}
	})
}

func (w *AppServerWorker) ResetContext(ctx context.Context) error {
	w.mu.Lock()
	origSess := w.origSession
	w.mu.Unlock()

	w.cleanupOldThread()
	w.resetLifecycleState()

	// If the process crashed between turns, bail out — bridge will Terminate+Start.
	if origSess.SessionID == "" {
		w.mu.Lock()
		w.closeAndMarkDone()
		w.mu.Unlock()
		return nil
	}
	return w.startNewThread(origSess, "reset")
}

func (w *AppServerWorker) Conn() worker.SessionConn {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.conn
}

func (w *AppServerWorker) Health() worker.WorkerHealth {
	return w.BaseWorker.Health(worker.TypeCodexCLI)
}

func (w *AppServerWorker) LastIO() time.Time {
	return w.BaseWorker.LastIO()
}

func (w *AppServerWorker) HandlePermissionResponse(_ context.Context, reqID string, allowed bool, reason string) error {
	decision := "decline"
	if allowed {
		decision = "accept"
	}
	result := map[string]any{"decision": decision}
	if reason != "" {
		result["reason"] = reason
	}
	return w.manager.RespondServerRequest(reqID, result)
}

func (w *AppServerWorker) HandleQuestionResponse(ctx context.Context, reqID string, answers map[string]string) error {
	result := map[string]any{
		"behavior": "allow",
		"updatedInput": map[string]any{
			"answers": answers,
		},
	}
	return w.manager.RespondServerRequest(reqID, result)
}

func (w *AppServerWorker) HandleElicitationResponse(ctx context.Context, reqID, action string, content map[string]any) error {
	result := map[string]any{
		"action":  action,
		"content": content,
	}
	return w.manager.RespondServerRequest(reqID, result)
}

func (w *AppServerWorker) Compact(ctx context.Context, args map[string]any) error {
	w.mu.Lock()
	tid := w.threadID
	w.mu.Unlock()
	if tid == "" {
		return fmt.Errorf("codexcli: no active thread")
	}
	_, err := w.manager.CompactThread(tid)
	return fmt.Errorf("codexcli: compact: %w", err)
}

func (w *AppServerWorker) Clear(ctx context.Context) error {
	w.mu.Lock()
	tid := w.threadID
	turnID := w.turnID
	w.mu.Unlock()
	if tid == "" {
		return fmt.Errorf("codexcli: no active thread")
	}
	// Only interrupt when a turn is in flight; turn/interrupt requires a turnId.
	if turnID != "" {
		_ = w.manager.InterruptTurn(tid, turnID)
	}
	return w.ResetContext(ctx)
}

// Rewind drops turns from the end of the thread. targetID is interpreted as the
// number of turns to drop (parseable uint); empty or invalid defaults to 1.
func (w *AppServerWorker) Rewind(ctx context.Context, targetID string) error {
	w.mu.Lock()
	tid := w.threadID
	w.mu.Unlock()
	if tid == "" {
		return fmt.Errorf("codexcli: no active thread")
	}
	numTurns := uint32(1)
	if targetID != "" {
		if n, perr := strconv.ParseUint(targetID, 10, 32); perr == nil && n >= 1 {
			numTurns = uint32(n)
		}
	}
	_, err := w.manager.RollbackThread(tid, numTurns)
	return fmt.Errorf("codexcli: rewind: %w", err)
}

// sandboxFromSession returns the session-level sandbox override if set,
// otherwise falls back to the config default.
func sandboxFromSession(session worker.SessionInfo, defaultSandbox string) string {
	if session.Sandbox != "" {
		return session.Sandbox
	}
	return defaultSandbox
}

// buildThreadStartParams constructs the JSON-RPC params for "thread/start".
// Shared by Start() and ResetContext() to avoid duplication.
func buildThreadStartParams(session worker.SessionInfo, cfg Config) map[string]any {
	approvalMode := cfg.ApprovalMode
	if session.SkipPermissions {
		approvalMode = "never"
	}
	params := map[string]any{
		"cwd":            session.ProjectDir,
		"sandbox":        sandboxFromSession(session, cfg.Sandbox),
		"personality":    cfg.Personality,
		"approvalPolicy": approvalMode,
	}
	if cfg.Model != "" {
		params["model"] = cfg.Model
	}
	if cfg.Ephemeral {
		params["ephemeral"] = true
	}
	if len(session.Images) > 0 {
		params["images"] = session.Images
	}
	if session.JSONSchema != "" {
		params["outputSchema"] = session.JSONSchema
	}
	if len(session.AllowedDirs) > 0 {
		params["additionalDirectories"] = session.AllowedDirs
	}
	if cfg.Color {
		params["color"] = true
	}
	if cfg.StrictConfig {
		params["strictConfig"] = true
	}
	if cfg.SkipGitRepoCheck {
		params["skipGitRepoCheck"] = true
	}
	if cfg.IgnoreRules {
		params["ignoreRules"] = true
	}
	if cfg.IgnoreUserConfig {
		params["ignoreUserConfig"] = true
	}
	if cfg.LocalProvider {
		params["localProvider"] = true
	}
	if cfg.BypassHookTrust {
		params["bypassHookTrust"] = true
	}
	if cfg.OutputFile != "" {
		params["outputFile"] = cfg.OutputFile
	}
	if cfg.ConfigProfile != "" {
		params["profile"] = cfg.ConfigProfile
	}
	return params
}
