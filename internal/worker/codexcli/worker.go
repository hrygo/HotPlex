package codexcli

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/internal/worker/base"
	"github.com/hrygo/hotplex/pkg/events"
)

func init() {
	worker.Register(worker.TypeCodexCLI, func() (worker.Worker, error) {
		s := GetSingleton()
		if s == nil {
			return nil, fmt.Errorf("codexcli: app-server singleton not initialized")
		}
		return &AppServerWorker{
			BaseWorker: base.NewBaseWorker(slog.Default(), nil),
			manager:    s,
		}, nil
	})
}

type Config struct {
	Command string
	Model   string
	// Sandbox mode: read-only, workspace-write, danger-full-access.
	// Default YOLO mode (danger-full-access) grants full filesystem + network access.
	// Existing deployments without explicit config will gain elevated permissions.
	Sandbox string
	// Approval mode: untrusted, on-request, never.
	// Default YOLO mode (never) skips all approval prompts — commands run immediately.
	ApprovalMode     string
	Ephemeral        bool
	Personality      string
	StartupTimeout   time.Duration
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

// ─── AppServerWorker ──────────────────────────────────────────────────────

var _ worker.Worker = (*AppServerWorker)(nil)
var _ worker.WorkerCommander = (*AppServerWorker)(nil)
var _ worker.ControlRequester = (*AppServerWorker)(nil)

type appState int

const (
	appStateNew appState = iota
	appStateStarting
	appStateReady
	appStateTerminated
)

type AppServerWorker struct {
	*base.BaseWorker

	manager   *CodexAppServerManager
	threadID  string
	turnID    string
	userID    string
	crashSub  <-chan struct{}
	doneCh    chan struct{}
	mu        sync.Mutex
	recvCh    chan *events.Envelope
	commands  *ServerCommander
	closed    bool
	released  bool
	state     appState
	sessionID string
	conn      *appConn

	// origSession preserves the SessionInfo from the most recent Start()
	// call so that ResetContext can re-establish a fresh thread after cleanup.
	origSession worker.SessionInfo

	// pendingHistory stores ConversationHistory from SessionInfo for injection
	// into the first user input of a new thread. Cleared after injection.
	pendingHistory  []worker.ConversationTurn
	historyInjected bool
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

func (w *AppServerWorker) Type() worker.WorkerType   { return worker.TypeCodexCLI }
func (w *AppServerWorker) SupportsResume() bool      { return true }
func (w *AppServerWorker) CanResumeTerminated() bool { return false }
func (w *AppServerWorker) SupportsStreaming() bool   { return true }
func (w *AppServerWorker) SupportsTools() bool       { return true }
func (w *AppServerWorker) EnvBlocklist() []string    { return EnvBlocklist }
func (w *AppServerWorker) SessionStoreDir() string   { return "" }
func (w *AppServerWorker) MaxTurns() int             { return 0 }
func (w *AppServerWorker) Modalities() []string      { return []string{"text", "code", "image"} }

func (w *AppServerWorker) SendControlRequest(ctx context.Context, subtype string, body map[string]any) (map[string]any, error) {
	if w.commands == nil {
		return nil, fmt.Errorf("codexcli: not started")
	}
	return w.commands.SendControlRequest(ctx, subtype, body)
}

func (w *AppServerWorker) Start(ctx context.Context, session worker.SessionInfo) error {
	w.mu.Lock()
	if w.state != appStateNew {
		w.mu.Unlock()
		return fmt.Errorf("codexcli: app-server already started (state=%d)", w.state)
	}
	w.state = appStateStarting

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
	w.mu.Lock()
	w.state = appStateReady
	w.mu.Unlock()
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

	content = w.injectHistoryPrefix(content)

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
// Guarded by w.closed; safe after release() which sets w.closed=true under the same mutex.
//
// Unlike release() which closes doneCh but keeps it non-nil (Wait() needs
// to receive from a closed channel), this path additionally nils doneCh
// because it is only called on Start error paths (before Wait() is invoked)
// and resetLifecycleState() expects doneCh == nil to create a fresh channel.
// Caller must hold w.mu.
func (w *AppServerWorker) closeAndMarkDone() {
	if w.closed {
		return
	}
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
	w.pendingHistory = nil
	w.historyInjected = false
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
// Always creates a fresh doneCh: after release(), doneCh may be closed-but-non-nil
// (release() no longer nils it to fix #691), which would cause Wait() to return
// immediately instead of blocking for the new thread's lifecycle.
func (w *AppServerWorker) resetLifecycleState() {
	w.mu.Lock()
	w.closed = false
	w.released = false
	w.doneCh = make(chan struct{})
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
	w.pendingHistory = slices.Clone(session.ConversationHistory)
	w.historyInjected = false
	w.mu.Unlock()

	return nil
}

// injectHistoryPrefix prepends conversation history to the first user
// input of a new thread. After injection, pendingHistory is cleared.
// A unique boundary ID is generated per injection call so that the
// sentinel markers cannot collide with real content, eliminating the
// need for destructive content sanitization.
func (w *AppServerWorker) injectHistoryPrefix(content string) string {
	w.mu.Lock()
	if w.historyInjected || len(w.pendingHistory) == 0 {
		w.mu.Unlock()
		return content
	}
	history := w.pendingHistory
	w.pendingHistory = nil
	w.historyInjected = true
	w.mu.Unlock()

	boundary := generateBoundaryID()

	var sb strings.Builder
	sb.WriteString("---\n")
	fmt.Fprintf(&sb, "CONVERSATION_HISTORY_%s_START\n", boundary)
	sb.WriteString("Below is the conversation history from a previous session. ")
	sb.WriteString("Use it as context to maintain continuity.\n\n")
	for _, turn := range history {
		switch turn.Role {
		case "user":
			sb.WriteString("[User]: ")
		case "assistant":
			sb.WriteString("[Assistant]: ")
		default:
			continue
		}
		sb.WriteString(turn.Content)
		sb.WriteString("\n\n")
	}
	fmt.Fprintf(&sb, "CONVERSATION_HISTORY_%s_END\n", boundary)
	sb.WriteString("---\n\n")
	sb.WriteString(content)
	return sb.String()
}

// generateBoundaryID returns a cryptographically random 8-char hex string
// used to make history sentinel markers unique per injection call.
// Falls back to timestamp-based ID if the system entropy source is unavailable.
func generateBoundaryID() string {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// Fallback: use timestamp-derived bytes. crypto/rand.Read only fails
		// in extreme sandbox environments; acceptable collision resistance for non-cryptographic use.
		binary.LittleEndian.PutUint32(buf[:], uint32(time.Now().UnixNano()))
	}
	return hex.EncodeToString(buf[:])
}

// ─── AppServerWorker lifecycle ────────────────────────────────────────

func (w *AppServerWorker) Resume(ctx context.Context, session worker.SessionInfo) error {
	// CodexCLI uses an ephemeral singleton process with no persistent
	// thread state — Resume on a fresh/terminated worker is semantically
	// identical to Start. Delegate to Start which handles Acquire + thread
	// creation, then report fallback so Bridge adjusts bookkeeping.
	//
	// Acquire/Release lifecycle: Start() calls Acquire() (increments ref
	// count on the singleton manager). Terminate/Kill calls release() which
	// calls Release() (decrements). Resume→Start re-acquires a slot, which
	// is correct — the old slot was released on Terminate.
	//
	// appStateNew is reachable when Bridge creates a fresh AppServerWorker
	// via the factory (init() + worker.Register) and then calls Resume()
	// instead of Start() — e.g., when startOrResumeOnInUse reuses a worker
	// that was registered but never started.
	w.mu.Lock()
	if w.state == appStateNew || w.state == appStateTerminated {
		// Reset to appStateNew so Start()'s guard (state != appStateNew) passes.
		// For terminated workers, release() already cleaned up manager refs;
		// resetLifecycleState re-creates the doneCh channel.
		w.state = appStateNew
		w.mu.Unlock()
		w.resetLifecycleState()
		if err := w.Start(ctx, session); err != nil {
			return err
		}
		return worker.ErrFellBackToFreshStart
	}
	w.state = appStateStarting
	w.mu.Unlock()

	w.cleanupOldThread()
	w.resetLifecycleState()
	if err := w.startNewThread(session, "resume"); err != nil {
		return err
	}
	w.mu.Lock()
	w.state = appStateReady
	w.mu.Unlock()
	return nil
}

func (w *AppServerWorker) Terminate(ctx context.Context) error {
	w.shutdown()
	w.mu.Lock()
	w.state = appStateTerminated
	w.mu.Unlock()
	return nil
}

func (w *AppServerWorker) Kill() error {
	w.shutdown()
	w.mu.Lock()
	w.state = appStateTerminated
	w.mu.Unlock()
	return nil
}

// shutdown releases the worker's manager subscription and decrements the
// singleton ref count. Unlike per-session workers, the shared AppServer
// singleton process is NOT killed here — it stops via idle drain or
// explicit ShutdownSingleton(). This prevents GC from killing a shared
// process when reclaiming sessions.
func (w *AppServerWorker) shutdown() {
	w.release()
}

func (w *AppServerWorker) Wait() (int, error) {
	w.mu.Lock()
	crashSub := w.crashSub
	doneCh := w.doneCh
	w.mu.Unlock()

	if doneCh == nil && crashSub == nil {
		return 0, nil
	}
	select {
	case <-crashSub:
		return 1, nil
	case <-doneCh:
		return 0, nil
	}
}

func (w *AppServerWorker) release() {
	w.mu.Lock()
	if w.released || w.closed {
		w.mu.Unlock()
		return
	}
	w.released = true
	w.closed = true
	doneCh := w.doneCh
	tid := w.threadID
	conn := w.conn
	mgr := w.manager
	w.mu.Unlock()

	if doneCh != nil {
		// Close but do NOT nil: Wait() relies on receiving from a closed
		// channel (returns immediately). closeAndMarkDone() additionally
		// nils for error-path reuse via resetLifecycleState().
		close(doneCh)
	}

	if mgr != nil && tid != "" {
		_ = mgr.Notify("thread/unsubscribe", ThreadUnsubscribeParams{
			ThreadID: tid,
		})
		mgr.Unsubscribe(tid)
		// Close recvCh so forwardEvents exits its range loop.
		// Must happen after Unsubscribe (removes from dispatch map) to
		// avoid racing with in-flight dispatchNotification sends.
		if conn != nil {
			_ = conn.Close()
		}
		mgr.Release()
	}
}

func (w *AppServerWorker) ResetContext(ctx context.Context) (worker.ResetResult, error) {
	// Increment reset generation so the OLD forwardEvents goroutine can detect
	// it's stale and skip cleanupCrashedWorker (which would detach the NEW worker).
	w.IncResetGeneration()

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
		return worker.ResetResult{ConnReplaced: true}, nil
	}
	origSess.ConversationHistory = nil // /reset clears context, do not re-inject old history
	if err := w.startNewThread(origSess, "reset"); err != nil {
		return worker.ResetResult{}, err
	}
	return worker.ResetResult{ConnReplaced: true}, nil
}

func (w *AppServerWorker) Conn() worker.SessionConn {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.conn
}

func (w *AppServerWorker) Health() worker.WorkerHealth {
	return w.BaseWorker.Health(worker.TypeCodexCLI)
}

// UpdateSystemPrompt updates the stored origSession.SystemPrompt so that
// the next ResetContext uses the reloaded agent config.
func (w *AppServerWorker) UpdateSystemPrompt(prompt string) {
	w.mu.Lock()
	w.origSession.SystemPrompt = prompt
	w.mu.Unlock()
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
	if err != nil {
		return fmt.Errorf("codexcli: compact: %w", err)
	}
	return nil
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
	// ConnReplaced is ignored: Clear is invoked over WorkerCommander (HTTP REST),
	// bridge doesn't restart forwardEvents on Clear.
	_, err := w.ResetContext(ctx)
	return err
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
	if err != nil {
		return fmt.Errorf("codexcli: rewind: %w", err)
	}
	return nil
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
//
// Default YOLO mode: danger-full-access sandbox + never approval.
// DangerFullAccess grants full network + filesystem access (Codex source).
// ⚠ This is a security-sensitive default — deployments should explicitly
// set sandbox to workspace-write or read-only for restricted environments.
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
