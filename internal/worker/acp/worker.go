package acp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/metrics"
	"github.com/hrygo/hotplex/internal/security"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/internal/worker/base"
	"github.com/hrygo/hotplex/internal/worker/proc"
	"github.com/hrygo/hotplex/pkg/events"
)

// Compile-time interface compliance checks.
var (
	_ worker.Worker                 = (*Worker)(nil)
	_ worker.WorkerSessionIDHandler = (*Worker)(nil)
	_ worker.WorkerCommander        = (*Worker)(nil)
	_ worker.ControlRequester       = (*Worker)(nil)
	_ base.MetadataHandler          = (*Worker)(nil)
)

// commandParts stores the space-split command (binary + optional prefix args).
var commandParts atomic.Value // []string

// autoApproveDefault is the global default for auto-approve set from config.
// Per-session overrides live on Worker.autoApprove.
var autoApproveDefault atomic.Value // bool

func init() {
	commandParts.Store([]string{"hermes", "acp"})
	autoApproveDefault.Store(false)

	worker.Register(worker.TypeACP, func() (worker.Worker, error) {
		w := &Worker{BaseWorker: base.NewBaseWorker(slog.Default(), nil)}
		if v, ok := autoApproveDefault.Load().(bool); ok {
			w.autoApprove.Store(v)
		}
		return w, nil
	})
}

// InitConfig applies ACP worker configuration from the config file.
func InitConfig(cfg config.ACPConfig) {
	cmd := cfg.Command
	if cmd == "" {
		cmd = "hermes acp"
	}
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		slog.Default().Error("acp: empty command after parsing")
		return
	}
	commandParts.Store(parts)
	autoApproveDefault.Store(cfg.AutoApprove)
	if err := security.RegisterCommand(parts[0]); err != nil {
		slog.Default().Error("acp: failed to register command", "command", parts[0], "err", err)
	}
}

// acpEnvBlocklist blocks gateway-internal secrets from leaking to ACP agents.
var acpEnvBlocklist = []string{
	"CLAUDECODE",
	"HOTPLEX_",
}

// parseMCPServers extracts the mcpServers array from the JSON config string
// produced by Bridge's buildWorkerInfo. The input format is {"mcpServers":{...}}.
// Returns an empty slice if the config is empty or unparseable.
func parseMCPServers(mcpConfig string) []any {
	if mcpConfig == "" {
		return nil
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(mcpConfig), &raw); err != nil {
		return nil
	}
	servers, ok := raw["mcpServers"]
	if !ok {
		return nil
	}
	// mcpServers is typically a map → normalize to []any for ACP protocol.
	// normalizeMCPServers in client.go handles nil → []any{}.
	return normalizeMCPServersToArray(servers)
}

// normalizeMCPServersToArray converts the mcpServers value to []any.
// ACP session/new expects an array; config provides either a map or array.
// NOTE: For map input, this mutates the inner maps in-place (sets m["name"]=name).
// Currently safe because input always comes from json.Unmarshal (fresh allocation),
// but callers must not pass shared/persistent maps.
func normalizeMCPServersToArray(servers any) []any {
	if servers == nil {
		return nil
	}
	switch v := servers.(type) {
	case []any:
		return v
	case map[string]any:
		// Convert map to array of named server configs.
		result := make([]any, 0, len(v))
		for name, cfg := range v {
			if m, ok := cfg.(map[string]any); ok {
				m["name"] = name
				result = append(result, m)
			}
		}
		return result
	default:
		return nil
	}
}

// Worker implements the ACP (Agent Client Protocol) worker adapter.
type Worker struct {
	*base.BaseWorker

	sessionID    string
	projectDir   string
	acpSessionID string // ACP agent's internal session ID

	client *ACPClient
	mapper *ACPMapper
	conn   *acpConn
	cancel context.CancelFunc

	// pendingPerm stores the permission mapping for in-flight permission requests.
	pendingPerm sync.Map // requestID (string) → *PermissionMapResult

	// systemPrompt holds the B/C channel agent config from SessionInfo.SystemPrompt.
	// Injected as a prefix on the first user prompt (ACP v1 has no native system prompt).
	systemPrompt         string
	systemPromptInjected atomic.Bool

	// mcpServers caches the parsed MCP servers from Start() so Clear() can reuse them.
	mcpServers []any

	// autoApprove controls per-session permission auto-approval.
	// Initialized from global default, overridable via set_permission_mode.
	autoApprove atomic.Bool

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
func (w *Worker) Modalities() []string    { return []string{"text", "code", "image"} }

// ─── WorkerSessionIDHandler ──────────────────────────────────────────────────

func (w *Worker) SetWorkerSessionID(id string) {
	w.Mu.Lock()
	w.acpSessionID = id
	w.Mu.Unlock()
}

func (w *Worker) GetWorkerSessionID() string {
	w.Mu.Lock()
	defer w.Mu.Unlock()
	return w.acpSessionID
}

// ─── Start ───────────────────────────────────────────────────────────────────

func (w *Worker) Start(ctx context.Context, session worker.SessionInfo) error {
	// Phase 1: Assign fields under lock (fast, no I/O).
	w.Mu.Lock()
	w.sessionID = session.SessionID
	w.projectDir = session.ProjectDir
	w.Mu.Unlock()

	// Cache system prompt for first-input injection (ACP v1 has no native mechanism).
	sp := session.SystemPrompt
	if len(sp) > 32*1024 {
		w.Log.Warn("acp: system prompt exceeds 32KB, truncating",
			"session_id", session.SessionID, "size", len(sp))
		sp = sp[:32*1024]
	}
	w.systemPrompt = sp
	w.systemPromptInjected.Store(false)

	// Phase 2: I/O-heavy operations outside the lock.

	// Resolve command — per-session override takes precedence over global config.
	var parts []string
	if session.ACPCommand != "" {
		parts = strings.Fields(session.ACPCommand)
	} else {
		parts, _ = commandParts.Load().([]string)
	}
	binary := parts[0]
	args := make([]string, len(parts)-1, len(parts)-1+len(session.Args))
	copy(args, parts[1:])
	args = append(args, session.Args...)

	// Build environment using shared security layer.
	env := base.BuildEnv(session, acpEnvBlocklist, "acp")

	// Create process manager â protected by Mu for concurrent Terminate safety.
	w.Mu.Lock()
	w.Proc = proc.New(proc.Opts{Logger: w.Log})
	w.Mu.Unlock()

	stdin, stdout, _, err := w.Proc.Start(context.Background(), binary, args, env, session.ProjectDir)
	if err != nil {
		return fmt.Errorf("acp: failed to start process: %w", err)
	}
	if stdin == nil || stdout == nil {
		return fmt.Errorf("acp: failed to start process: %s", binary)
	}

	now := time.Now()
	w.Mu.Lock()
	w.StartTime = now
	w.Mu.Unlock()
	w.SetLastIO(now)

	// Create client.
	client := w.testClient
	if client == nil {
		client = NewACPClient(stdin, stdout, w.Log)
	}
	w.client = client

	// Start read loop early — must run before handshake to receive responses.
	childCtx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel
	cleanup := true
	defer func() {
		if cleanup {
			cancel()
		}
	}()
	client.StartReadLoop(childCtx)

	// Handshake.
	hctx, hcancel := context.WithTimeout(ctx, 30*time.Second)
	defer hcancel()

	clientInfo := map[string]string{"name": "hotplex", "version": "1.0"}
	if _, err := client.Initialize(hctx, clientInfo); err != nil {
		_ = w.Proc.Kill()
		return fmt.Errorf("acp: initialize handshake: %w", err)
	}

	// Create or load session.
	mcpServers := parseMCPServers(session.MCPConfig)
	w.mcpServers = mcpServers // cache for Clear()
	var acpSessID string
	var historyLost bool
	if session.WorkerSessionID != "" {
		// Resume existing session.
		sessResult, loadErr := client.LoadSession(hctx, session.WorkerSessionID, session.ProjectDir, mcpServers)
		if loadErr != nil {
			w.Log.Error("acp: load session failed, conversation history will be lost",
				"session_id", session.SessionID, "acp_session_id", session.WorkerSessionID, "error", loadErr)
			newSess, newErr := client.NewSession(hctx, session.ProjectDir, mcpServers)
			if newErr != nil {
				_ = w.Proc.Kill()
				return fmt.Errorf("acp: new session: %w", newErr)
			}
			acpSessID = newSess.SessionID
			historyLost = true
		} else {
			acpSessID = sessResult.SessionID
		}
	} else {
		sessResult, sessErr := client.NewSession(hctx, session.ProjectDir, mcpServers)
		if sessErr != nil {
			_ = w.Proc.Kill()
			return fmt.Errorf("acp: new session: %w", sessErr)
		}
		acpSessID = sessResult.SessionID
	}
	w.SetWorkerSessionID(acpSessID)

	// Record handshake latency.
	metrics.ACPHandshakeDuration.Observe(time.Since(now).Seconds())

	// Create mapper.
	mapper := w.testMapper
	if mapper == nil {
		mapper = NewACPMapper(session.SessionID, session.UserID, w.Log)
	}
	w.mapper = mapper

	// Create connection.
	w.Mu.Lock()
	w.conn = newACPConn(session.UserID, session.SessionID, w.Log)
	w.SetConnLocked(nil) // base.Conn not used; acpConn is returned via Conn() override.
	w.Mu.Unlock()

	// Start worker read loop.
	go w.readLoop(childCtx)

	// Notify client if conversation history was lost during resume.
	if historyLost {
		w.conn.TrySend(w.mapper.newEnvelope(events.Error, events.ErrorData{
			Code:    "HISTORY_LOST",
			Message: "Previous conversation history could not be restored; starting a new session.",
		}))
	}

	cleanup = false
	return nil
}

// ─── Input ───────────────────────────────────────────────────────────────────

func (w *Worker) Input(ctx context.Context, content string, metadata map[string]any) error {
	// Check for control responses (permission/question/elicitation).
	handled, err := base.DispatchMetadata(ctx, metadata, w)
	if handled {
		return err
	}

	// Capture conn under lock for consistent access pattern.
	w.Mu.Lock()
	conn := w.conn
	w.Mu.Unlock()
	if conn == nil {
		return fmt.Errorf("acp: worker connection closed")
	}

	// Regular user input → send prompt.
	w.mapper.Reset()
	w.mapper.SetTurnActive()

	// Inject system prompt on first user input (ACP v1 has no native system prompt).
	if w.systemPrompt != "" && !w.systemPromptInjected.Load() {
		w.systemPromptInjected.Store(true)
		content = fmt.Sprintf("[SYSTEM INSTRUCTIONS]\n%s\n[/SYSTEM INSTRUCTIONS]\n\n%s", w.systemPrompt, content)
	}

	// Cache input for crash recovery (InputRecoverer).
	conn.lastInput.Store(&content)

	// Emit state(running).
	conn.TrySend(w.mapper.MapStateRunning())
	w.SetLastIO(time.Now())

	pctx, pcancel := context.WithTimeout(ctx, 30*time.Minute)
	defer pcancel()

	result, promptErr := w.client.Prompt(pctx, w.GetWorkerSessionID(), content)
	if promptErr != nil {
		// Always emit error sequence to client.
		envs := w.mapper.MapPromptError(promptErr)
		for _, env := range envs {
			conn.TrySend(env)
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
		conn.TrySend(env)
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
	w.Mu.Lock()
	conn := w.conn
	w.Mu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}

	// Clear stale pending permission entries (session teardown cleanup).
	w.pendingPerm.Range(func(key, _ any) bool {
		w.pendingPerm.Delete(key)
		return true
	})

	// Try graceful cancel (nil-safe for pre-Start Terminate).
	if w.client != nil {
		cancelCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = w.client.Cancel(cancelCtx, w.GetWorkerSessionID())
		cancel()

		// Wait for readLoop goroutine to fully exit before killing the process,
		// preventing reads from closed pipes during rapid session teardown.
		select {
		case <-w.client.Done():
		case <-time.After(3 * time.Second):
		}
	}

	return w.BaseWorker.Terminate(ctx)
}

// ─── Conn ────────────────────────────────────────────────────────────────────

// Conn returns the acpConn as a SessionConn (overrides base.BaseWorker.Conn).
func (w *Worker) Conn() worker.SessionConn {
	w.Mu.Lock()
	defer w.Mu.Unlock()
	if w.conn == nil {
		return nil
	}
	return w.conn
}

// ─── Health ──────────────────────────────────────────────────────────────────

func (w *Worker) Health() worker.WorkerHealth {
	h := w.BaseWorker.Health(worker.TypeACP)
	// BaseWorker.Health reads base.Conn which is nil for ACP (we use acpConn instead).
	// Populate SessionID from acpConn if available.
	w.Mu.Lock()
	if w.conn != nil {
		h.SessionID = w.conn.SessionID()
	}
	w.Mu.Unlock()
	return h
}

// ─── ResetContext ────────────────────────────────────────────────────────────

func (w *Worker) ResetContext(_ context.Context) error {
	// ACP doesn't support in-place reset → terminate + restart handled by Bridge.
	return worker.ErrNotImplemented
}

// ─── WorkerCommander ─────────────────────────────────────────────────────────

// Compact returns ErrNotImplemented — ACP has no compact method.
// Terminate+Start is too expensive; let Bridge fallback to Input passthrough.
func (w *Worker) Compact(_ context.Context, _ map[string]any) error {
	return worker.ErrNotImplemented
}

// Clear creates a new ACP session within the same process (equivalent to /clear).
// The PID stays the same; only the acpSessionID changes.
// I/O runs outside Mu to avoid blocking Terminate/Health during Cancel+NewSession.
func (w *Worker) Clear(ctx context.Context) error {
	w.Mu.Lock()
	client := w.client
	w.Mu.Unlock()

	if client == nil {
		return fmt.Errorf("acp: clear: worker not started")
	}

	hctx, hcancel := context.WithTimeout(ctx, 10*time.Second)
	defer hcancel()

	// Cancel any in-flight turn before creating new session.
	_ = client.Cancel(hctx, w.GetWorkerSessionID())

	result, err := client.NewSession(hctx, w.projectDir, w.mcpServers)
	if err != nil {
		return fmt.Errorf("acp: clear: new session: %w", err)
	}

	w.Mu.Lock()
	w.acpSessionID = result.SessionID
	w.mapper.Reset()
	w.systemPromptInjected.Store(false)
	w.IncResetGeneration()
	w.Mu.Unlock()
	return nil
}

// Rewind returns ErrNotImplemented — ACP has no rewind method.
func (w *Worker) Rewind(_ context.Context, _ string) error {
	return worker.ErrNotImplemented
}

// ─── ControlRequester ────────────────────────────────────────────────────────

// SendControlRequest handles structured control queries from Bridge/worker_cmds.
// Supported subtypes: get_context_usage, set_model, set_permission_mode, mcp_status.
func (w *Worker) SendControlRequest(ctx context.Context, subtype string, body map[string]any) (map[string]any, error) {
	switch subtype {
	case "get_context_usage":
		return w.handleContextUsage()
	case "set_model":
		return w.handleSetModel(ctx, body)
	case "set_permission_mode":
		return w.handleSetPermissionMode(body)
	case "mcp_status":
		// ACP has no MCP status query method — return empty map.
		return map[string]any{}, nil
	default:
		return nil, fmt.Errorf("acp: unsupported control request: %s", subtype)
	}
}

func (w *Worker) handleContextUsage() (map[string]any, error) {
	snapshot := w.mapper.LastUsage()
	result := map[string]any{
		"maxTokens":   snapshot.ContextSize,
		"totalTokens": snapshot.ContextUsed,
	}
	// Include model if known from last prompt result.
	if snapshot.ContextSize > 0 || snapshot.ContextUsed > 0 {
		return result, nil
	}
	// No usage data yet — return zero-value map (consistent with OCS behavior).
	return map[string]any{
		"maxTokens":   0,
		"totalTokens": 0,
	}, nil
}

func (w *Worker) handleSetModel(ctx context.Context, body map[string]any) (map[string]any, error) {
	modelID, _ := body["modelId"].(string)
	if modelID == "" {
		return nil, fmt.Errorf("acp: set_model: missing modelId")
	}
	if err := w.client.SetSessionModel(ctx, w.GetWorkerSessionID(), modelID); err != nil {
		return nil, fmt.Errorf("acp: set_model: %w", err)
	}
	return map[string]any{"model": modelID}, nil
}

func (w *Worker) handleSetPermissionMode(body map[string]any) (map[string]any, error) {
	mode, _ := body["mode"].(string)
	switch mode {
	case "auto-accept":
		w.autoApprove.Store(true)
	default:
		w.autoApprove.Store(false)
	}
	return map[string]any{"mode": mode}, nil
}

// ─── MetadataHandler ─────────────────────────────────────────────────────────

func (w *Worker) HandlePermissionResponse(ctx context.Context, reqID string, allowed bool, _ string) error {
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
		metrics.ACPPermissionRequestsTotal.WithLabelValues("approved").Inc()
	} else {
		outcome = pm.FormatDeniedOutcome()
		metrics.ACPPermissionRequestsTotal.WithLabelValues("denied").Inc()
	}

	return w.client.RespondPermission(ctx, pm.RequestID, outcome)
}

func (w *Worker) HandleQuestionResponse(_ context.Context, _ string, _ map[string]string) error {
	return worker.ErrNotImplemented
}

func (w *Worker) HandleElicitationResponse(_ context.Context, _, _ string, _ map[string]any) error {
	return worker.ErrNotImplemented
}

// ─── readLoop ────────────────────────────────────────────────────────────────

func (w *Worker) readLoop(ctx context.Context) {
	// Capture conn under lock for consistent access pattern.
	w.Mu.Lock()
	conn := w.conn
	w.Mu.Unlock()

	defer func() {
		if r := recover(); r != nil {
			w.Log.Error("acp: readLoop panic",
				"session_id", w.sessionID, "panic", r,
				"stack", string(debug.Stack()))
		}
		// Clean up stale pending permission entries when read loop exits
		// (agent disconnected, context cancelled, or panic recovered).
		w.pendingPerm.Range(func(key, _ any) bool {
			w.pendingPerm.Delete(key)
			return true
		})
	}()

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
				conn.TrySend(env)
			}
		case req, ok := <-w.client.RequestCh:
			if !ok {
				return
			}
			w.handleServerRequest(ctx, req, conn)
		}
	}
}

func (w *Worker) handleServerRequest(ctx context.Context, req *JSONRPCRequest, conn *acpConn) {
	switch req.Method {
	case "session/request_permission":
		pm := w.mapper.MapPermissionRequest(req)
		if pm == nil {
			w.Log.Warn("acp: failed to map permission request")
			return
		}

		// Check auto-approve.
		if w.autoApprove.Load() {
			_ = w.client.RespondPermission(ctx, req.ID, pm.FormatAllowedOutcome())
			metrics.ACPPermissionRequestsTotal.WithLabelValues("approved").Inc()
			return
		}

		// Store mapping and send permission_request to client.
		w.pendingPerm.Store(string(req.ID), pm)
		conn.TrySend(pm.Envelope)

	default:
		w.Log.Warn("acp: unhandled server request", "method", req.Method)
	}
}
