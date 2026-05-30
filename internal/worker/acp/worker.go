package acp

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/security"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/internal/worker/base"
	"github.com/hrygo/hotplex/internal/worker/proc"
)

// Compile-time interface compliance checks.
var (
	_ worker.Worker                 = (*Worker)(nil)
	_ worker.WorkerSessionIDHandler = (*Worker)(nil)
	_ base.MetadataHandler          = (*Worker)(nil)
)

// commandParts stores the space-split command (binary + optional prefix args).
var commandParts atomic.Value // []string

// autoApprove controls whether permission requests are auto-approved.
var autoApprove atomic.Value // bool

func init() {
	commandParts.Store([]string{"hermes-acp"})
	autoApprove.Store(false)

	worker.Register(worker.TypeACP, func() (worker.Worker, error) {
		return &Worker{BaseWorker: base.NewBaseWorker(slog.Default(), nil)}, nil
	})
}

// InitConfig applies ACP worker configuration from the config file.
func InitConfig(cfg config.ACPConfig) {
	cmd := cfg.Command
	if cmd == "" {
		cmd = "hermes-acp"
	}
	parts := strings.Fields(cmd)
	commandParts.Store(parts)
	autoApprove.Store(cfg.AutoApprove)
	if err := security.RegisterCommand(parts[0]); err != nil {
		slog.Default().Error("acp: failed to register command", "command", parts[0], "err", err)
	}
}

// acpEnvBlocklist blocks gateway-internal secrets from leaking to ACP agents.
var acpEnvBlocklist = []string{
	"CLAUDECODE",
	"HOTPLEX_",
}

// Worker implements the ACP (Agent Client Protocol) worker adapter.
type Worker struct {
	*base.BaseWorker

	mu           sync.Mutex
	sessionID    string
	projectDir   string
	acpSessionID string // ACP agent's internal session ID

	client *ACPClient
	mapper *ACPMapper
	conn   *acpConn
	cancel context.CancelFunc
	ctx    context.Context // lifecycle context for background operations

	// pendingPerm stores the permission mapping for in-flight permission requests.
	pendingPerm sync.Map // requestID (string) → *PermissionMapResult

	// testHooks for injection-based testing.
	testClient *ACPClient
	testMapper *ACPMapper
}

// ─── Capabilities ────────────────────────────────────────────────────────────

func (w *Worker) Type() worker.WorkerType { return worker.TypeACP }
func (w *Worker) SupportsResume() bool    { return true }
func (w *Worker) SupportsStreaming() bool { return true }
func (w *Worker) SupportsTools() bool     { return true }
func (w *Worker) EnvBlocklist() []string  { return append([]string{}, acpEnvBlocklist...) }
func (w *Worker) SessionStoreDir() string { return "" }
func (w *Worker) MaxTurns() int           { return 0 }
func (w *Worker) Modalities() []string    { return []string{"text", "code"} }

// ─── WorkerSessionIDHandler ──────────────────────────────────────────────────

func (w *Worker) SetWorkerSessionID(id string) {
	w.mu.Lock()
	w.acpSessionID = id
	w.mu.Unlock()
}

func (w *Worker) GetWorkerSessionID() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.acpSessionID
}

// ─── Start ───────────────────────────────────────────────────────────────────

func (w *Worker) Start(ctx context.Context, session worker.SessionInfo) error {
	// Phase 1: Assign fields under lock (fast, no I/O).
	w.mu.Lock()
	w.sessionID = session.SessionID
	w.projectDir = session.ProjectDir
	w.mu.Unlock()

	// Phase 2: I/O-heavy operations outside the lock.

	// Resolve command.
	parts, _ := commandParts.Load().([]string)
	binary := parts[0]
	args := make([]string, len(parts)-1, len(parts)-1+len(session.Args))
	copy(args, parts[1:])
	args = append(args, session.Args...)

	// Build environment using shared security layer.
	env := base.BuildEnv(session, acpEnvBlocklist, "acp")

	// Create process manager.
	w.Proc = proc.New(proc.Opts{Logger: w.Log})

	stdin, stdout, _, err := w.Proc.Start(ctx, binary, args, env, session.ProjectDir)
	if err != nil {
		return fmt.Errorf("acp: failed to start process: %w", err)
	}
	if stdin == nil || stdout == nil {
		return fmt.Errorf("acp: failed to start process: %s", binary)
	}

	w.StartTime = time.Now()
	w.SetLastIO(time.Now())

	// Create client.
	client := w.testClient
	if client == nil {
		client = NewACPClient(stdin, bufio.NewReader(stdout), w.Log)
	}
	w.client = client

	// Handshake.
	hctx, hcancel := context.WithTimeout(ctx, 30*time.Second)
	defer hcancel()

	clientInfo := map[string]string{"name": "hotplex", "version": "1.0"}
	if _, err := client.Initialize(hctx, clientInfo); err != nil {
		_ = w.Proc.Kill()
		return fmt.Errorf("acp: initialize handshake: %w", err)
	}

	// Create or load session.
	var acpSessID string
	if session.WorkerSessionID != "" {
		// Resume existing session.
		sessResult, loadErr := client.LoadSession(hctx, session.WorkerSessionID, session.ProjectDir, nil)
		if loadErr != nil {
			w.Log.Warn("acp: load session failed, creating new", "error", loadErr)
			newSess, newErr := client.NewSession(hctx, session.ProjectDir, nil)
			if newErr != nil {
				_ = w.Proc.Kill()
				return fmt.Errorf("acp: new session: %w", newErr)
			}
			acpSessID = newSess.SessionID
		} else {
			acpSessID = sessResult.SessionID
		}
	} else {
		sessResult, sessErr := client.NewSession(hctx, session.ProjectDir, nil)
		if sessErr != nil {
			_ = w.Proc.Kill()
			return fmt.Errorf("acp: new session: %w", sessErr)
		}
		acpSessID = sessResult.SessionID
	}
	w.SetWorkerSessionID(acpSessID)

	// Create mapper.
	mapper := w.testMapper
	if mapper == nil {
		mapper = NewACPMapper(session.SessionID, session.UserID)
	}
	w.mapper = mapper

	// Create connection.
	w.mu.Lock()
	w.conn = newACPConn(session.UserID, session.SessionID)
	w.mu.Unlock()
	w.SetConnLocked(nil) // base.Conn not used; acpConn is returned via Conn() override.

	// Start read loop.
	childCtx, cancel := context.WithCancel(ctx)
	w.cancel = cancel
	w.ctx = childCtx
	client.StartReadLoop(childCtx)
	go w.readLoop(childCtx)

	return nil
}

// ─── Input ───────────────────────────────────────────────────────────────────

func (w *Worker) Input(ctx context.Context, content string, metadata map[string]any) error {
	// Check for control responses (permission/question/elicitation).
	handled, err := base.DispatchMetadata(ctx, metadata, w)
	if handled {
		return err
	}

	// Regular user input → send prompt.
	w.mapper.Reset()
	w.mapper.SetTurnActive()

	// Emit state(running).
	w.conn.TrySend(w.mapper.MapStateRunning())
	w.SetLastIO(time.Now())

	pctx, pcancel := context.WithTimeout(ctx, 30*time.Minute)
	defer pcancel()

	result, promptErr := w.client.Prompt(pctx, w.GetWorkerSessionID(), content)
	if promptErr != nil {
		// Always emit error sequence to client.
		envs := w.mapper.MapPromptError(promptErr)
		for _, env := range envs {
			w.conn.TrySend(env)
		}
		// JSONRPCError is an expected agent error — don't wrap.
		if _, ok := errors.AsType[*JSONRPCError](promptErr); ok {
			return nil
		}
		return fmt.Errorf("acp: prompt: %w", promptErr)
	}

	// Emit done sequence.
	envs := w.mapper.MapPromptResponse(result)
	for _, env := range envs {
		w.conn.TrySend(env)
	}

	w.SetLastIO(time.Now())
	return nil
}

// ─── Resume ───────────────────────────────────────────────────────────────────

func (w *Worker) Resume(ctx context.Context, session worker.SessionInfo) error {
	return w.Start(ctx, session)
}

// ─── Terminate ───────────────────────────────────────────────────────────────

func (w *Worker) Terminate(ctx context.Context) error {
	if w.cancel != nil {
		w.cancel()
	}

	// Close conn first so forwardEvents goroutine exits promptly.
	w.mu.Lock()
	conn := w.conn
	w.mu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}

	// Try graceful cancel (nil-safe for pre-Start Terminate).
	if w.client != nil {
		cancelCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = w.client.Cancel(cancelCtx, w.GetWorkerSessionID())
		cancel()
	}

	return w.BaseWorker.Terminate(ctx)
}

// ─── Conn ────────────────────────────────────────────────────────────────────

// Conn returns the acpConn as a SessionConn (overrides base.BaseWorker.Conn).
func (w *Worker) Conn() worker.SessionConn {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.conn == nil {
		return nil
	}
	return w.conn
}

// ─── Health ──────────────────────────────────────────────────────────────────

func (w *Worker) Health() worker.WorkerHealth {
	return w.BaseWorker.Health(worker.TypeACP)
}

// ─── ResetContext ────────────────────────────────────────────────────────────

func (w *Worker) ResetContext(_ context.Context) error {
	// ACP doesn't support in-place reset → terminate + restart handled by Bridge.
	return worker.ErrNotImplemented
}

// ─── MetadataHandler ─────────────────────────────────────────────────────────

func (w *Worker) HandlePermissionResponse(_ context.Context, reqID string, allowed bool, _ string) error {
	result, ok := w.pendingPerm.LoadAndDelete(reqID)
	if !ok {
		return fmt.Errorf("acp: no pending permission request: %s", reqID)
	}
	pm, ok := result.(*PermissionMapResult)
	if !ok {
		return fmt.Errorf("acp: invalid permission map result type")
	}

	var outcome any
	if allowed {
		outcome = pm.FormatAllowedOutcome()
	} else {
		outcome = pm.FormatDeniedOutcome()
	}

	return w.client.RespondPermission(w.lifecycleCtx(), pm.RequestID, outcome)
}

func (w *Worker) HandleQuestionResponse(_ context.Context, _ string, _ map[string]string) error {
	return worker.ErrNotImplemented
}

func (w *Worker) HandleElicitationResponse(_ context.Context, _, _ string, _ map[string]any) error {
	return worker.ErrNotImplemented
}

// lifecycleCtx returns a context for background operations (permission responses, etc.).
// Falls back to context.Background() if the worker hasn't started yet.
func (w *Worker) lifecycleCtx() context.Context {
	if w.ctx != nil {
		return w.ctx
	}
	return context.Background()
}

// ─── readLoop ────────────────────────────────────────────────────────────────

func (w *Worker) readLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case notif, ok := <-w.client.NotificationCh:
			if !ok {
				return
			}
			w.SetLastIO(time.Now())
			envelopes := w.mapper.MapNotification(notif)
			for _, env := range envelopes {
				w.conn.TrySend(env)
			}
		case req, ok := <-w.client.RequestCh:
			if !ok {
				return
			}
			w.handleServerRequest(req)
		}
	}
}

func (w *Worker) handleServerRequest(req *JSONRPCRequest) {
	switch req.Method {
	case "session/request_permission":
		pm := w.mapper.MapPermissionRequest(req)
		if pm == nil {
			w.Log.Warn("acp: failed to map permission request")
			return
		}

		// Check auto-approve.
		if val, _ := autoApprove.Load().(bool); val {
			_ = w.client.RespondPermission(w.lifecycleCtx(), req.ID, pm.FormatAllowedOutcome())
			return
		}

		// Store mapping and send permission_request to client.
		w.pendingPerm.Store(string(req.ID), pm)
		w.conn.TrySend(pm.Envelope)

	default:
		w.Log.Warn("acp: unhandled server request", "method", req.Method)
	}
}
