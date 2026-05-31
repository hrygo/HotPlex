package acp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
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
	_ worker.InPlaceReseter         = (*Worker)(nil)
	_ base.MetadataHandler          = (*Worker)(nil)
)

// commandParts stores the space-split command (binary + optional prefix args).
var commandParts atomic.Value // []string

// autoApproveDefault is the global default for auto-approve set from config.
// Per-session overrides live on Worker.autoApprove.
var autoApproveDefault atomic.Bool

func init() {
	commandParts.Store([]string{"hermes", "acp"})
	autoApproveDefault.Store(false)

	worker.Register(worker.TypeACP, func() (worker.Worker, error) {
		w := &Worker{BaseWorker: base.NewBaseWorker(slog.Default(), nil)}
		w.autoApprove.Store(autoApproveDefault.Load())
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

	// pendingRequests stores non-permission server-initiated requests (questions, elicitations)
	// for forwarding client responses back to the agent as JSON-RPC responses.
	pendingRequests sync.Map // requestID (string) → *JSONRPCRequest

	// initResult caches the ACP initialize handshake result for agent discovery and capability checks.
	initResult *InitializeResult

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
		return w.fmtStartError(binary, err)
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
	w.Mu.Lock()
	w.client = client
	w.Mu.Unlock()

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
	initResult, err := client.Initialize(hctx, clientInfo)
	if err != nil {
		_ = w.Proc.Kill()
		return w.fmtHandshakeError(err)
	}
	w.Mu.Lock()
	w.initResult = initResult
	w.Mu.Unlock()
	w.Log.Info("acp: agent discovered",
		"agent", initResult.AgentInfo.Name,
		"version", initResult.AgentInfo.Version,
		"protocol", initResult.ProtocolVersion)

	// Create or load session.
	mcpServers := parseMCPServers(session.MCPConfig)
	w.mcpServers = mcpServers // cache for Clear()
	var acpSessID string
	var historyLost bool
	if session.WorkerSessionID != "" && w.supportsCapability("loadSession") {
		// Resume existing session (only if agent supports load).
		sessResult, loadErr := client.LoadSession(hctx, session.WorkerSessionID, session.ProjectDir, mcpServers)
		if loadErr != nil {
			w.Log.Error("acp: session load failed, falling back to new session (history lost)",
				"session_id", session.SessionID, "acp_session_id", session.WorkerSessionID,
				"error", loadErr,
				"hint", "check agent session storage and persistence")
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
	if w.systemPrompt != "" && w.systemPromptInjected.CompareAndSwap(false, true) {
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

	// Clear stale pending permission/request entries (session teardown cleanup).
	w.pendingPerm.Range(func(key, _ any) bool {
		w.pendingPerm.Delete(key)
		return true
	})
	w.pendingRequests.Range(func(key, _ any) bool {
		w.pendingRequests.Delete(key)
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
	w.Mu.Lock()
	if w.conn != nil {
		h.SessionID = w.conn.SessionID()
	}
	ir := w.initResult
	w.Mu.Unlock()
	if ir != nil {
		h.AgentName = ir.AgentInfo.Name
		h.AgentVersion = ir.AgentInfo.Version
		h.ProtocolVersion = ir.ProtocolVersion
	}
	return h
}

// supportsCapability checks whether the agent declared a capability in the
// initialize handshake. Returns true if the capability is absent (assume supported)
// or explicitly true. Returns false only if explicitly set to false.
func (w *Worker) supportsCapability(name string) bool {
	w.Mu.Lock()
	ir := w.initResult
	w.Mu.Unlock()
	if ir == nil {
		return true // no handshake result, assume supported
	}
	caps := ir.AgentCapabilities
	if caps == nil {
		return true // no capabilities declared, assume all supported
	}
	v, ok := caps[name]
	if !ok {
		return true // not listed, assume supported
	}
	if b, ok := v.(bool); ok {
		return b
	}
	return true
}

// ─── ResetContext ────────────────────────────────────────────────────────────

func (w *Worker) ResetContext(ctx context.Context) error {
	// Reuse the same process: cancel current turn, create new session.
	// Equivalent to Clear() but called by Bridge.ResetSession.
	return w.resetSession(ctx)
}

// InPlaceReset tells Bridge not to rebuild the forwardEvents goroutine.
// The existing goroutine detects generation changes via resetGenerationer.
func (w *Worker) InPlaceReset() bool { return true }

// resetSession is the shared logic for Clear() and ResetContext():
// cancel current turn, create new ACP session within the same process.
func (w *Worker) resetSession(ctx context.Context) error {
	w.Mu.Lock()
	client := w.client
	projectDir := w.projectDir
	mcpServers := w.mcpServers
	sessionID := w.acpSessionID
	w.Mu.Unlock()

	if client == nil {
		return fmt.Errorf("acp: reset: worker not started")
	}

	// Cancel any in-flight turn with a short, independent timeout
	// so a slow Cancel doesn't eat into the NewSession budget.
	cctx, ccancel := context.WithTimeout(ctx, 3*time.Second)
	_ = client.Cancel(cctx, sessionID)
	ccancel()

	hctx, hcancel := context.WithTimeout(ctx, 10*time.Second)
	defer hcancel()

	result, err := client.NewSession(hctx, projectDir, mcpServers)
	if err != nil {
		return fmt.Errorf("acp: reset: new session: %w", err)
	}

	w.Mu.Lock()
	w.acpSessionID = result.SessionID
	w.mapper.Reset()
	w.systemPromptInjected.Store(false)
	// Clear stale pending entries from the old session.
	w.pendingPerm.Range(func(key, _ any) bool {
		w.pendingPerm.Delete(key)
		return true
	})
	w.pendingRequests.Range(func(key, _ any) bool {
		w.pendingRequests.Delete(key)
		return true
	})
	w.Mu.Unlock()
	return nil
}

// ─── WorkerCommander ─────────────────────────────────────────────────────────

// Compact returns ErrNotImplemented — ACP has no compact method.
// Terminate+Start is too expensive; let Bridge fallback to Input passthrough.
func (w *Worker) Compact(_ context.Context, _ map[string]any) error {
	return worker.ErrNotImplemented
}

// Clear creates a new ACP session within the same process (equivalent to /clear).
// The PID stays the same; only the acpSessionID changes.
// IncResetGeneration is called here because the /clear path does NOT go through
// Bridge.ResetSession (which calls it separately for the reset path).
func (w *Worker) Clear(ctx context.Context) error {
	if err := w.resetSession(ctx); err != nil {
		return err
	}
	w.IncResetGeneration()
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
	// Return context usage snapshot; zero values mean no data yet (consistent with OCS).
	return map[string]any{
		"maxTokens":   snapshot.ContextSize,
		"totalTokens": snapshot.ContextUsed,
	}, nil
}

func (w *Worker) handleSetModel(ctx context.Context, body map[string]any) (map[string]any, error) {
	modelID, _ := body["model"].(string)
	if modelID == "" {
		return nil, fmt.Errorf("acp: set_model: missing model")
	}
	w.Mu.Lock()
	client := w.client
	w.Mu.Unlock()
	if client == nil {
		return nil, fmt.Errorf("acp: set_model: worker not started")
	}
	if err := client.SetSessionModel(ctx, w.GetWorkerSessionID(), modelID); err != nil {
		return nil, fmt.Errorf("acp: set_model: %w", err)
	}
	return map[string]any{"model": modelID}, nil
}

func (w *Worker) handleSetPermissionMode(body map[string]any) (map[string]any, error) {
	mode, _ := body["mode"].(string)
	switch mode {
	case "auto-accept":
		w.autoApprove.Store(true)
	case "default", "":
		w.autoApprove.Store(false)
	default:
		return nil, fmt.Errorf("acp: set_permission_mode: unsupported mode: %s", mode)
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

	w.Mu.Lock()
	client := w.client
	w.Mu.Unlock()
	if client == nil {
		return fmt.Errorf("acp: permission response: worker not started")
	}
	return client.RespondRequest(ctx, pm.RequestID, outcome)
}

func (w *Worker) HandleQuestionResponse(ctx context.Context, reqID string, answers map[string]string) error {
	return w.respondToServerRequest(ctx, reqID, "question", answers)
}

func (w *Worker) HandleElicitationResponse(ctx context.Context, reqID, action string, content map[string]any) error {
	outcome := map[string]any{"action": action}
	if content != nil {
		outcome["content"] = content
	}
	return w.respondToServerRequest(ctx, reqID, "elicitation", outcome)
}

// respondToServerRequest is the shared logic for forwarding client responses
// back to the agent for non-permission server-initiated requests.
func (w *Worker) respondToServerRequest(ctx context.Context, reqID, kind string, outcome any) error {
	req, ok := w.pendingRequests.LoadAndDelete(reqID)
	if !ok {
		return fmt.Errorf("acp: no pending %s request: %s", kind, reqID)
	}
	w.Mu.Lock()
	client := w.client
	w.Mu.Unlock()
	if client == nil {
		return fmt.Errorf("acp: %s response: worker not started", kind)
	}
	acpReq, ok := req.(*JSONRPCRequest)
	if !ok {
		return fmt.Errorf("acp: %s response: invalid request type %T", kind, req)
	}
	return client.RespondRequest(ctx, acpReq.ID, outcome)
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
		// Clean up stale pending permission/request entries when read loop exits
		// (agent disconnected, context cancelled, or panic recovered).
		w.pendingPerm.Range(func(key, _ any) bool {
			w.pendingPerm.Delete(key)
			return true
		})
		w.pendingRequests.Range(func(key, _ any) bool {
			w.pendingRequests.Delete(key)
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
			_ = w.client.RespondRequest(ctx, req.ID, pm.FormatAllowedOutcome())
			metrics.ACPPermissionRequestsTotal.WithLabelValues("approved").Inc()
			return
		}

		// Store mapping and send permission_request to client.
		w.pendingPerm.Store(string(req.ID), pm)
		conn.TrySend(pm.Envelope)

	default:
		// Non-permission server-initiated request (e.g. question, elicitation).
		// Forward as Raw event so the client can display and respond.
		w.pendingRequests.Store(string(req.ID), req)
		var params any
		if err := json.Unmarshal(req.Params, &params); err != nil {
			w.Log.Warn("acp: malformed params in server request",
				"method", req.Method, "error", err)
		}
		conn.TrySend(w.mapper.newEnvelope(events.Raw, events.RawData{
			Kind: "acp.server_request." + req.Method,
			Raw: map[string]any{
				"id":     string(req.ID),
				"method": req.Method,
				"params": params,
			},
		}))
	}
}

// Error Formatting (U-03)

// fmtStartError produces an actionable error when the agent binary cannot be started.
func (w *Worker) fmtStartError(binary string, err error) error {
	// Prefer structured error checks (cross-platform) over substring matching.
	var execErr *exec.Error
	if errors.As(err, &execErr) {
		return fmt.Errorf("acp: agent %q not found in PATH. Install the agent or set acp.command in config.yaml: %w", binary, err)
	}
	var pathErr *os.PathError
	if errors.As(err, &pathErr) && os.IsPermission(pathErr.Err) {
		return fmt.Errorf("acp: agent %q is not executable. Run: chmod +x $(which %s): %w", binary, binary, err)
	}
	// Fallback: substring matching for wrapped errors that lose type info.
	errMsg := err.Error()
	if strings.Contains(errMsg, "executable file not found") ||
		strings.Contains(errMsg, "no such file") ||
		strings.Contains(errMsg, "command not found") ||
		strings.Contains(errMsg, "cannot find the file") {
		return fmt.Errorf("acp: agent %q not found in PATH. Install the agent or set acp.command in config.yaml: %w", binary, err)
	}
	if strings.Contains(errMsg, "permission denied") {
		return fmt.Errorf("acp: agent %q is not executable. Run: chmod +x $(which %s): %w", binary, binary, err)
	}
	return fmt.Errorf("acp: failed to start process %q: %w", binary, err)
}

// fmtHandshakeError produces an actionable error when the ACP handshake fails.
func (w *Worker) fmtHandshakeError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("acp: handshake timed out after 30s. Check: 1) agent is running 2) API keys are configured 3) network connectivity: %w", err)
	}
	return fmt.Errorf("acp: initialize handshake: %w", err)
}
