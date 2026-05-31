package codexcli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/internal/worker/base"
	"github.com/hrygo/hotplex/internal/worker/proc"
	"github.com/hrygo/hotplex/pkg/events"
)

// managerState represents the lifecycle state of the CodexAppServerManager.
type managerState int

const (
	stateIdle     managerState = iota // no process
	stateStarting                     // process launching, waiting for handshake
	stateRunning                      // process serving JSON-RPC requests
	stateStopped                      // gateway shutdown
)

const (
	defaultCallTimeout       = 30 * time.Second
	defaultStartupTimeout    = 30 * time.Second
	criticalEventSendTimeout = 5 * time.Second
	scannerInitSize          = 64 * 1024        // 64 KB
	scannerMaxSize           = 10 * 1024 * 1024 // 10 MB
)

// CodexAppServerManager manages a single shared `codex app-server` process
// via stdio JSON-RPC across all Codex CLI sessions. The process is lazily
// started on first Acquire and shut down when the last session releases.
//
// # Lifecycle
//
//	idle → starting → running → (crash → idle by monitorProcess) → stopped
//
// # Concurrency
//
// All public methods are safe for concurrent use. Acquire serializes process
// startup via mutex so only the first caller starts the process.
type CodexAppServerManager struct {
	log *slog.Logger
	cfg config.CodexCLIConfig

	mu      sync.Mutex
	proc    *proc.Manager
	stdin   io.WriteCloser
	stdout  io.Reader
	refs    int
	state   managerState
	crashCh chan struct{} // closed when process exits unexpectedly

	// pending maps JSON-RPC request IDs to response channels.
	pending sync.Map // map[int64]chan *JSONRPCResponse

	// serverReqIDs maps approval requestID → JSON-RPC frame ID for server-initiated
	// requests, so the worker can respond via RespondServerRequest.
	serverReqIDs sync.Map // map[string]int64

	nextReqID atomic.Int64

	// subMu protects subscribers for thread event routing.
	subMu       sync.Mutex
	subscribers map[string]chan *events.Envelope
	subSessions map[string]string // threadID → sessionID mapping for envelope population
	subsClosed  atomic.Bool       // set when subscribers have been closed (prevents double-close)

	// writeMu serializes writes to stdin from concurrent Call/Notify.
	writeMu sync.Mutex

	// cancel cancels the background readNotifications goroutine.
	cancel context.CancelFunc

	// converter maps JSON-RPC notification methods + params to AEP envelopes.
	converter *Mapper

	idleTimer *time.Timer
	pgid      int // cached PGID from proc.Manager, set in startProcessLocked
}

func NewCodexAppServerManager(log *slog.Logger, cfg config.CodexCLIConfig) *CodexAppServerManager {
	if cfg.IdleDrainPeriod <= 0 {
		cfg.IdleDrainPeriod = 30 * time.Minute
	}
	return &CodexAppServerManager{
		log:         log.With("component", "codex-app-server"),
		cfg:         cfg,
		crashCh:     make(chan struct{}),
		subscribers: make(map[string]chan *events.Envelope),
		subSessions: make(map[string]string),
		converter:   NewMapper(""),
	}
}

// Acquire increments the reference count and starts the process if needed.
// Returns a crash notification channel that is closed when the process exits
// unexpectedly. Workers should check this channel in their Wait() implementation.
func (m *CodexAppServerManager) Acquire(ctx context.Context) (<-chan struct{}, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.state == stateStopped {
		return nil, fmt.Errorf("codex-app-server: stopped")
	}

	if m.idleTimer != nil {
		m.idleTimer.Stop()
		m.idleTimer = nil
	}

	if m.state == stateIdle {
		if err := m.startProcessLocked(ctx); err != nil {
			return nil, err
		}
	}

	if m.state != stateRunning {
		return nil, fmt.Errorf("codex-app-server: unexpected state %d", m.state)
	}

	m.refs++
	m.log.Debug("codex-app-server: acquire", "refs", m.refs)
	return m.crashCh, nil
}

// Release decrements the reference count. When refs reach zero, an idle drain
// timer starts. If no new Acquire arrives within idleDrainPeriod, the process
// is killed.
func (m *CodexAppServerManager) Release() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.refs <= 0 {
		m.log.Warn("codex-app-server: release with no active refs")
		return
	}

	m.refs--
	m.log.Debug("codex-app-server: release", "refs", m.refs)

	if m.refs == 0 && m.state == stateRunning {
		m.startIdleDrainLocked()
	}
}

// Subscribe returns a channel that receives AEP events for the given thread ID.
func (m *CodexAppServerManager) Subscribe(threadID, sessionID string) chan *events.Envelope {
	m.subMu.Lock()
	defer m.subMu.Unlock()

	if ch, ok := m.subscribers[threadID]; ok {
		return ch
	}

	ch := make(chan *events.Envelope, 256)
	m.subscribers[threadID] = ch
	m.subSessions[threadID] = sessionID
	m.log.Debug("codex-app-server: subscribed", "thread_id", threadID, "session_id", sessionID)
	return ch
}

// Unsubscribe removes the subscriber channel for the given thread.
// Precondition: the corresponding appConn must be closed before or after this
// call to ensure the channel is eventually cleaned up by monitorProcess.
func (m *CodexAppServerManager) Unsubscribe(threadID string) {
	m.subMu.Lock()
	defer m.subMu.Unlock()

	if _, ok := m.subscribers[threadID]; ok {
		delete(m.subscribers, threadID)
		delete(m.subSessions, threadID)
		m.log.Debug("codex-app-server: unsubscribed", "thread_id", threadID)
	}
}

// Call sends a JSON-RPC request to the app-server process and waits for a response.
// The params argument is marshaled as JSON. If nil, no params field is sent.
func (m *CodexAppServerManager) Call(method string, params any) (json.RawMessage, error) {
	id := m.nextReqID.Add(1)

	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
	}

	if params != nil {
		raw, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("codex-app-server: marshal params: %w", err)
		}
		req.Params = raw
	}

	respCh := make(chan *JSONRPCResponse, 1)
	m.pending.Store(id, respCh)
	defer m.pending.Delete(id)

	if err := m.writeRequest(&req); err != nil {
		return nil, fmt.Errorf("codex-app-server: write request: %w", err)
	}

	callTimeout := m.cfg.CallTimeout
	if callTimeout <= 0 {
		callTimeout = defaultCallTimeout
	}
	timer := time.NewTimer(callTimeout)
	defer timer.Stop()

	select {
	case resp := <-respCh:
		if resp.Error != nil {
			return nil, fmt.Errorf("codex-app-server: %s: %s (code %d)",
				method, resp.Error.Message, resp.Error.Code)
		}
		return resp.Result, nil
	case <-timer.C:
		return nil, fmt.Errorf("codex-app-server: %s: timeout after %v",
			method, callTimeout)
	}
}

// Notify sends a JSON-RPC notification (no response expected) to the app-server process.
func (m *CodexAppServerManager) Notify(method string, params any) error {
	notif := JSONRPCNotification{
		JSONRPC: "2.0",
		Method:  method,
	}

	if params != nil {
		raw, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("codex-app-server: marshal notification params: %w", err)
		}
		notif.Params = raw
	}

	return m.writeNotification(&notif)
}

// Shutdown forcefully terminates the process regardless of reference count.
func (m *CodexAppServerManager) Shutdown(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.state = stateStopped

	if m.idleTimer != nil {
		m.idleTimer.Stop()
		m.idleTimer = nil
	}

	if m.cancel != nil {
		m.cancel()
	}

	if m.proc != nil {
		m.log.Info("codex-app-server: shutdown, killing process")
		// Use ForceKill(pgid) instead of proc.Kill() to avoid deadlock
		// with monitorProcess's Wait() (see KillIfIdle for rationale).
		if m.pgid > 0 {
			_ = proc.ForceKill(m.pgid)
		} else {
			_ = m.proc.Kill()
		}
		// Do not clear m.proc here — monitorProcess will handle cleanup
		// after Wait() observes the exit. Only clear if no monitorProcess
		// goroutine exists (stateStopped set above prevents double cleanup).
	}

	// Close all active subscriptions if not already closed by monitorProcess.
	if !m.subsClosed.Load() {
		m.subsClosed.Store(true)
		m.subMu.Lock()
		for id, ch := range m.subscribers {
			close(ch)
			delete(m.subscribers, id)
		}
		m.subSessions = make(map[string]string)
		m.subMu.Unlock()
	}
}

// IsRunning reports whether the singleton process is currently running.
func (m *CodexAppServerManager) IsRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state == stateRunning
}

// ─── internal ─────────────────────────────────────────────────────────────

func (m *CodexAppServerManager) startProcessLocked(ctx context.Context) error {
	m.state = stateStarting
	m.subsClosed.Store(false)
	m.log.Info("codex-app-server: starting codex app-server process")

	args := []string{"app-server"}

	parts := strings.Fields(m.cfg.Command)
	if len(parts) == 0 {
		parts = []string{"codex"}
	}
	binary := parts[0]
	fullArgs := make([]string, 0, len(parts)-1+len(args))
	fullArgs = append(fullArgs, parts[1:]...)
	fullArgs = append(fullArgs, args...)

	env := m.buildEnv()
	m.proc = proc.New(proc.Opts{Logger: m.log})

	// Use context.Background() for the long-lived codex process.
	// exec.CommandContext kills the process when the context is done,
	// so a timeout context would kill the process immediately after
	// startProcessLocked returns. The handshake timeout is already
	// enforced by the Call method's own timer.
	stdin, stdout, _, err := m.proc.Start(context.Background(), binary, fullArgs, env, "")
	if err != nil {
		m.proc = nil
		m.state = stateIdle
		return fmt.Errorf("codex-app-server: start process: %w", err)
	}

	m.stdin = stdin
	m.stdout = stdout
	m.pgid = m.proc.PGID()

	bgCtx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	go m.readNotifications(bgCtx, stdout)
	go m.monitorProcess()

	if err := m.handshake(ctx); err != nil {
		cancel()
		_ = m.proc.Kill()
		m.proc = nil
		m.pgid = 0
		m.stdin = nil
		m.stdout = nil
		m.state = stateIdle
		return fmt.Errorf("codex-app-server: handshake: %w", err)
	}

	m.state = stateRunning
	m.log.Info("codex-app-server: process started")
	return nil
}

// handshake performs the JSON-RPC initialize/initialized handshake.
func (m *CodexAppServerManager) handshake(_ context.Context) error {
	type initializeResult struct {
		Capabilities json.RawMessage `json:"capabilities"`
	}

	resp, err := m.Call("initialize", map[string]any{
		"clientInfo": map[string]string{
			"name":    "hotplex",
			"title":   "HotPlex Gateway",
			"version": "1.0.0",
		},
		"capabilities": map[string]any{
			"experimentalApi": true,
		},
	})
	if err != nil {
		return fmt.Errorf("initialize: %w", err)
	}

	var result initializeResult
	if err := json.Unmarshal(resp, &result); err != nil {
		return fmt.Errorf("parse initialize result: %w", err)
	}

	if err := m.Notify("initialized", map[string]any{}); err != nil {
		return fmt.Errorf("initialized notification: %w", err)
	}

	m.log.Info("codex-app-server: handshake complete",
		"capabilities", string(result.Capabilities))
	return nil
}

// writeFrame serializes a JSON-RPC frame to stdin. Caller must not hold m.mu.
func (m *CodexAppServerManager) writeFrame(v any) error {
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	return json.NewEncoder(m.stdin).Encode(v)
}

// writeRequest marshals and writes a JSON-RPC request to stdin.
func (m *CodexAppServerManager) writeRequest(req *JSONRPCRequest) error {
	return m.writeFrame(req)
}

// writeNotification marshals and writes a JSON-RPC notification to stdin.
func (m *CodexAppServerManager) writeNotification(notif *JSONRPCNotification) error {
	return m.writeFrame(notif)
}

// readNotifications reads JSON-RPC frames from stdout and routes them to
// pending response channels or subscriber notification channels.
// reader is passed in to avoid acquiring m.mu at startup (caller holds it).
func (m *CodexAppServerManager) readNotifications(ctx context.Context, reader io.Reader) {
	defer func() {
		if r := recover(); r != nil {
			m.log.Error("codex-app-server: readNotifications panic",
				"panic", r, "stack", string(debug.Stack()))
		}
	}()

	if reader == nil {
		return
	}

	scanner := bufio.NewScanner(reader)
	buf := make([]byte, scannerInitSize)
	scanner.Buffer(buf, scannerMaxSize)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				m.log.Warn("codex-app-server: stdout read error", "err", err)
			}
			return
		}

		data := scanner.Bytes()
		if len(data) == 0 {
			continue
		}

		m.dispatchFrame(data)
	}
}

// dispatchFrame parses a single JSON-RPC frame and routes to the correct handler.
//
// Routing logic (in order):
//  1. Method != "" && ID != 0 → server-initiated request (e.g. approval)
//  2. ID != 0 && Method == "" → response to a client request
//  3. ID == 0 && Error != nil → error with no ID, dropped
//  4. ID == 0 && Method != "" → notification
func (m *CodexAppServerManager) dispatchFrame(data []byte) {
	var frame JSONRPCFrame
	if err := json.Unmarshal(data, &frame); err != nil {
		m.log.Warn("codex-app-server: unmarshal frame", "err", err)
		return
	}

	// Server-initiated request (has both ID and Method, e.g. approval).
	if frame.Method != "" && frame.ID != 0 {
		m.dispatchServerRequest(&frame)
		return
	}

	if frame.ID != 0 {
		resp := &JSONRPCResponse{
			JSONRPC: frame.JSONRPC,
			ID:      frame.ID,
			Result:  frame.Result,
			Error:   frame.Error,
		}
		m.dispatchResponse(resp)
		return
	}

	// ID == 0: notification or unknown.
	if frame.Error != nil {
		m.log.Debug("codex-app-server: error frame with ID=0, dropping", "error", frame.Error.Message)
		return
	}

	if frame.Method != "" {
		notif := &JSONRPCNotification{
			JSONRPC: frame.JSONRPC,
			Method:  frame.Method,
			Params:  frame.Params,
		}
		m.dispatchNotification(notif)
	} else {
		m.log.Debug("codex-app-server: frame with ID=0, no method, no error — dropped")
	}
}

// dispatchResponse routes a JSON-RPC response to the pending request channel.
func (m *CodexAppServerManager) dispatchResponse(resp *JSONRPCResponse) {
	if v, ok := m.pending.Load(resp.ID); ok {
		if ch, ok := v.(chan *JSONRPCResponse); ok {
			select {
			case ch <- resp:
			default:
				m.log.Warn("codex-app-server: response channel full, dropping",
					"id", resp.ID)
			}
		}
	}
}

// dispatchServerRequest handles server-initiated JSON-RPC requests (e.g. approvals).
// It extracts the thread ID, maps the request to AEP envelopes via the converter,
// and stores the request ID → frame ID mapping so the worker can respond later.
func (m *CodexAppServerManager) dispatchServerRequest(frame *JSONRPCFrame) {
	var params struct {
		ThreadID  string `json:"threadId"`
		RequestID string `json:"requestId"`
	}
	if frame.Params != nil {
		if err := json.Unmarshal(frame.Params, &params); err != nil {
			m.log.Warn("codex-app-server: unmarshal server request params", "method", frame.Method, "err", err)
			return
		}
	}

	if params.ThreadID == "" {
		m.log.Debug("codex-app-server: server request without threadId, dropping",
			"method", frame.Method, "id", frame.ID)
		return
	}

	// Store the JSON-RPC frame ID so HandlePermissionResponse can reply.
	if params.RequestID != "" {
		m.serverReqIDs.Store(params.RequestID, frame.ID)
	}

	// Map and deliver as notification to the subscriber.
	notif := &JSONRPCNotification{
		JSONRPC: frame.JSONRPC,
		Method:  frame.Method,
		Params:  frame.Params,
	}
	m.dispatchNotification(notif)
}

// RespondServerRequest sends a JSON-RPC response to a server-initiated request.
// reqID is the approval request's requestId; result is the response payload,
// which should contain a "decision" or "behavior" key for server request handling.
func (m *CodexAppServerManager) RespondServerRequest(reqID string, result any) error {
	v, ok := m.serverReqIDs.LoadAndDelete(reqID)
	if !ok {
		return fmt.Errorf("codex-app-server: no pending server request for %q", reqID)
	}
	rpcID, ok := v.(int64)
	if !ok {
		return fmt.Errorf("codex-app-server: invalid rpc id type for %q", reqID)
	}

	raw, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("codex-app-server: marshal server response: %w", err)
	}

	// Validate the response shape. Permission/approval responses carry
	// "behavior" or "decision"; elicitation responses carry "action".
	var check map[string]any
	if err := json.Unmarshal(raw, &check); err == nil {
		_, hasBehavior := check["behavior"]
		_, hasDecision := check["decision"]
		_, hasAction := check["action"]
		if !hasBehavior && !hasDecision && !hasAction {
			m.log.Error("codex-app-server: server response missing expected key",
				"req_id", reqID, "expected", "behavior, decision, or action")
		}
	}

	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      rpcID,
		Result:  raw,
	}
	return m.writeFrame(resp)
}

// ─── Thread Lifecycle ─────────────────────────────────────────────────────

// ResumeThread resumes a paused or interrupted thread.
func (m *CodexAppServerManager) ResumeThread(threadID string) (json.RawMessage, error) {
	resp, err := m.Call("thread/resume", map[string]any{"threadId": threadID})
	if err != nil {
		return nil, fmt.Errorf("codex-app-server: thread/resume: %w", err)
	}
	return resp, nil
}

// ForkThread forks a thread at the current state with additional parameters.
func (m *CodexAppServerManager) ForkThread(threadID string, params map[string]any) (json.RawMessage, error) {
	merged := map[string]any{"threadId": threadID}
	maps.Copy(merged, params)
	merged["threadId"] = threadID // ensure authoritative value wins
	resp, err := m.Call("thread/fork", merged)
	if err != nil {
		return nil, fmt.Errorf("codex-app-server: thread/fork: %w", err)
	}
	return resp, nil
}

// ArchiveThread archives a thread so it no longer appears in the active list.
func (m *CodexAppServerManager) ArchiveThread(threadID string) error {
	err := m.Notify("thread/archive", map[string]any{"threadId": threadID})
	if err != nil {
		return fmt.Errorf("codex-app-server: thread/archive: %w", err)
	}
	return nil
}

// UnarchiveThread restores a previously archived thread.
func (m *CodexAppServerManager) UnarchiveThread(threadID string) error {
	err := m.Notify("thread/unarchive", map[string]any{"threadId": threadID})
	if err != nil {
		return fmt.Errorf("codex-app-server: thread/unarchive: %w", err)
	}
	return nil
}

// SetThreadName updates the display name of a thread.
func (m *CodexAppServerManager) SetThreadName(threadID, name string) error {
	err := m.Notify("thread/name/set", map[string]any{
		"threadId": threadID,
		"name":     name,
	})
	if err != nil {
		return fmt.Errorf("codex-app-server: thread/name/set: %w", err)
	}
	return nil
}

// SetThreadGoal sets a goal for the specified thread.
// Upstream ThreadGoalSetParams uses "objective" (thread.rs:746), not "goal".
func (m *CodexAppServerManager) SetThreadGoal(threadID, objective string) error {
	err := m.Notify("thread/goal/set", map[string]any{
		"threadId":  threadID,
		"objective": objective,
	})
	if err != nil {
		return fmt.Errorf("codex-app-server: thread/goal/set: %w", err)
	}
	return nil
}

// GetThreadGoal retrieves the current goal for the specified thread.
func (m *CodexAppServerManager) GetThreadGoal(threadID string) (json.RawMessage, error) {
	resp, err := m.Call("thread/goal/get", map[string]any{"threadId": threadID})
	if err != nil {
		return nil, fmt.Errorf("codex-app-server: thread/goal/get: %w", err)
	}
	return resp, nil
}

// ClearThreadGoal removes the goal from the specified thread.
func (m *CodexAppServerManager) ClearThreadGoal(threadID string) error {
	err := m.Notify("thread/goal/clear", map[string]any{"threadId": threadID})
	if err != nil {
		return fmt.Errorf("codex-app-server: thread/goal/clear: %w", err)
	}
	return nil
}

// UpdateThreadSettings updates the settings configuration for a thread.
// Upstream ThreadSettingsUpdateParams (thread.rs:229) is a flat struct: settings
// keys (cwd, approvalPolicy, sandboxPolicy, model, ...) sit alongside threadId,
// not nested under a "settings" wrapper.
func (m *CodexAppServerManager) UpdateThreadSettings(threadID string, settings map[string]any) error {
	params := map[string]any{"threadId": threadID}
	maps.Copy(params, settings)
	params["threadId"] = threadID // ensure authoritative value wins
	err := m.Notify("thread/settings/update", params)
	if err != nil {
		return fmt.Errorf("codex-app-server: thread/settings/update: %w", err)
	}
	return nil
}

// CompactThread starts context compaction for the specified thread, returning
// the compaction result.
func (m *CodexAppServerManager) CompactThread(threadID string) (json.RawMessage, error) {
	resp, err := m.Call("thread/compact/start", map[string]any{"threadId": threadID})
	if err != nil {
		return nil, fmt.Errorf("codex-app-server: thread/compact/start: %w", err)
	}
	return resp, nil
}

// RollbackThread drops numTurns turns from the end of the thread.
// Upstream ThreadRollbackParams (thread.rs:956) requires num_turns (>= 1),
// not a target ID. numTurns < 1 is clamped to 1.
func (m *CodexAppServerManager) RollbackThread(threadID string, numTurns uint32) (json.RawMessage, error) {
	if numTurns < 1 {
		numTurns = 1
	}
	resp, err := m.Call("thread/rollback", map[string]any{
		"threadId": threadID,
		"numTurns": numTurns,
	})
	if err != nil {
		return nil, fmt.Errorf("codex-app-server: thread/rollback: %w", err)
	}
	return resp, nil
}

// ─── Turn Control ──────────────────────────────────────────────────────────

// SteerTurn injects mid-turn input into the active turn of a thread.
// Upstream TurnSteerParams (turn.rs:160) requires input (Vec<UserInput>) and
// expectedTurnId (the currently-active turn; the request fails if it does not
// match). The text is wrapped as a single UserInput text item.
func (m *CodexAppServerManager) SteerTurn(threadID, expectedTurnID, text string) (json.RawMessage, error) {
	resp, err := m.Call("turn/steer", map[string]any{
		"threadId":       threadID,
		"expectedTurnId": expectedTurnID,
		"input": []map[string]any{
			{"type": "text", "text": text},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("codex-app-server: turn/steer: %w", err)
	}
	return resp, nil
}

// InterruptTurn interrupts the running turn in the specified thread.
// Upstream TurnInterruptParams (turn.rs:188) requires both threadId and turnId.
func (m *CodexAppServerManager) InterruptTurn(threadID, turnID string) error {
	err := m.Notify("turn/interrupt", map[string]any{
		"threadId": threadID,
		"turnId":   turnID,
	})
	if err != nil {
		return fmt.Errorf("codex-app-server: turn/interrupt: %w", err)
	}
	return nil
}

// ─── Environment ───────────────────────────────────────────────────────────

// AddEnvironment registers a remote execution environment with the app-server.
// Upstream EnvironmentAddParams (environment.rs:9) requires environmentId and
// execServerUrl — it is not a key/value env-var setter.
func (m *CodexAppServerManager) AddEnvironment(environmentID, execServerURL string) error {
	err := m.Notify("environment/add", map[string]any{
		"environmentId": environmentID,
		"execServerUrl": execServerURL,
	})
	if err != nil {
		return fmt.Errorf("codex-app-server: environment/add: %w", err)
	}
	return nil
}

// ─── MCP ───────────────────────────────────────────────────────────────────

// ListMCPServerStatus returns the status of all configured MCP servers.
func (m *CodexAppServerManager) ListMCPServerStatus() (json.RawMessage, error) {
	resp, err := m.Call("mcpServerStatus/list", nil)
	if err != nil {
		return nil, fmt.Errorf("codex-app-server: mcpServerStatus/list: %w", err)
	}
	return resp, nil
}

// ReadMCPResource reads a resource from a configured MCP server.
// Upstream McpResourceReadParams (mcp.rs:77) requires both server and uri.
func (m *CodexAppServerManager) ReadMCPResource(server, uri string) (json.RawMessage, error) {
	resp, err := m.Call("mcpServer/resource/read", map[string]any{
		"server": server,
		"uri":    uri,
	})
	if err != nil {
		return nil, fmt.Errorf("codex-app-server: mcpServer/resource/read: %w", err)
	}
	return resp, nil
}

// CallMCPTool invokes a tool on a configured MCP server.
// Upstream McpServerToolCallParams (mcp.rs:94) requires threadId, server, tool;
// arguments is optional. The args map is omitted from params if nil.
func (m *CodexAppServerManager) CallMCPTool(threadID, server, tool string, args map[string]any) (json.RawMessage, error) {
	params := map[string]any{
		"threadId": threadID,
		"server":   server,
		"tool":     tool,
	}
	if args != nil {
		params["arguments"] = args
	}
	resp, err := m.Call("mcpServer/tool/call", params)
	if err != nil {
		return nil, fmt.Errorf("codex-app-server: mcpServer/tool/call: %w", err)
	}
	return resp, nil
}

// RefreshMCPServer reloads the global MCP server registry.
// Upstream McpServerRefresh (common.rs:874) takes no params (undefined/None).
func (m *CodexAppServerManager) RefreshMCPServer() error {
	err := m.Notify("config/mcpServer/reload", nil)
	if err != nil {
		return fmt.Errorf("codex-app-server: config/mcpServer/reload: %w", err)
	}
	return nil
}

// MCPServerOAuthLogin initiates an OAuth login flow for the named MCP server.
// Upstream McpServerOauthLoginParams (mcp.rs:187) uses "name", not "serverName".
func (m *CodexAppServerManager) MCPServerOAuthLogin(name string) (json.RawMessage, error) {
	resp, err := m.Call("mcpServer/oauth/login", map[string]any{"name": name})
	if err != nil {
		return nil, fmt.Errorf("codex-app-server: mcpServer/oauth/login: %w", err)
	}
	return resp, nil
}

// ─── Review ────────────────────────────────────────────────────────────────

// StartReview initiates a code review on the given thread.
// Upstream ReviewStartParams (review.rs:17) requires both threadId and target.
func (m *CodexAppServerManager) StartReview(threadID string, target map[string]any) (json.RawMessage, error) {
	resp, err := m.Call("review/start", map[string]any{
		"threadId": threadID,
		"target":   target,
	})
	if err != nil {
		return nil, fmt.Errorf("codex-app-server: review/start: %w", err)
	}
	return resp, nil
}

// dispatchNotification extracts the thread ID, converts via the mapper, and
// delivers envelopes to the subscriber channel. Locks subMu once per notification.
func (m *CodexAppServerManager) dispatchNotification(notif *JSONRPCNotification) {
	var params struct {
		ThreadID string `json:"threadId"`
	}
	if notif.Params != nil {
		if err := json.Unmarshal(notif.Params, &params); err != nil {
			m.log.Warn("codex-app-server: unmarshal notification params", "err", err)
			return
		}
	}

	if params.ThreadID == "" {
		// Also try nested thread.id format (codex sends some notifications with
		// params.thread.id instead of params.threadId).
		var nested struct {
			Thread struct {
				ID string `json:"id"`
			} `json:"thread"`
		}
		if notif.Params != nil {
			_ = json.Unmarshal(notif.Params, &nested)
		}
		if nested.Thread.ID != "" {
			params.ThreadID = nested.Thread.ID
		}
	}

	if params.ThreadID == "" {
		m.log.Debug("codex-app-server: notification without threadId, skipping", "method", notif.Method)
		return
	}

	// Only log lifecycle events; skip high-frequency deltas to avoid log flooding.
	switch notif.Method {
	case "item/agentMessage/delta", "item/reasoning/summaryTextDelta",
		"item/reasoning/textDelta", "item/commandExecution/outputDelta",
		"thread/tokenUsage/updated":
	default:
		m.log.Debug("codex-app-server: dispatching notification", "method", notif.Method, "threadId", params.ThreadID)
	}

	m.subMu.Lock()
	sessionID := m.subSessions[params.ThreadID]
	ch, ok := m.subscribers[params.ThreadID]
	m.subMu.Unlock()
	if !ok {
		return
	}

	envs := m.converter.MapNotification(notif.Method, notif.Params)
	for _, env := range envs {
		if env != nil {
			env.SessionID = sessionID
			m.sendEnvelope(ch, env)
		}
	}
}

// sendEnvelope delivers a single envelope to a subscriber channel with backpressure.
// Delta events are dropped silently when full; critical events block with a 5s timeout.
func (m *CodexAppServerManager) sendEnvelope(ch chan *events.Envelope, env *events.Envelope) {
	// Recover from send on closed channel — release() may close ch
	// concurrently with Unsubscribe+conn.Close after we released subMu.
	defer func() {
		if r := recover(); r != nil {
			m.log.Debug("codex-app-server: send on closed channel, subscriber gone")
		}
	}()
	if env.Event.Type == events.MessageDelta {
		select {
		case ch <- env:
		default:
		}
		return
	}
	timer := time.NewTimer(criticalEventSendTimeout)
	defer timer.Stop()
	select {
	case ch <- env:
	case <-timer.C:
		m.log.Warn("codex-app-server: critical event send timeout, dropping",
			"event_type", env.Event.Type)
	}
}

// monitorProcess waits for the process to exit and handles crash recovery.
func (m *CodexAppServerManager) monitorProcess() {
	m.mu.Lock()
	pm := m.proc
	m.mu.Unlock()
	if pm == nil {
		return
	}
	code, _ := pm.Wait()

	m.mu.Lock()
	wasRunning := m.state == stateRunning
	refs := m.refs
	m.state = stateIdle
	m.proc = nil
	m.pgid = 0
	m.stdin = nil
	m.stdout = nil

	if m.idleTimer != nil {
		m.idleTimer.Stop()
		m.idleTimer = nil
	}

	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}

	if wasRunning && refs > 0 {
		m.log.Warn("codex-app-server: process crashed", "exit_code", code, "refs", refs)
		close(m.crashCh)
		m.crashCh = make(chan struct{})
	} else {
		m.log.Info("codex-app-server: process exited", "exit_code", code, "refs", refs)
	}
	m.mu.Unlock()

	if wasRunning {
		m.converter.Reset()
		m.subsClosed.Store(true)
		m.subMu.Lock()
		for id, ch := range m.subscribers {
			close(ch)
			delete(m.subscribers, id)
		}
		m.subSessions = make(map[string]string)
		m.subMu.Unlock()
	}
}

// startIdleDrainLocked starts a timer to kill the process when idle.
// Caller must hold m.mu.
func (m *CodexAppServerManager) startIdleDrainLocked() {
	m.log.Info("codex-app-server: starting idle drain timer",
		"period", m.cfg.IdleDrainPeriod)
	m.idleTimer = time.AfterFunc(m.cfg.IdleDrainPeriod, func() {
		m.mu.Lock()
		defer m.mu.Unlock()

		if m.refs == 0 && m.state == stateRunning && m.proc != nil {
			m.log.Info("codex-app-server: idle drain expired, killing process")
			_ = m.proc.Kill()
			// monitorProcess will set state to stateIdle and clean up.
		}
		m.idleTimer = nil
	})
}

// KillIfIdle immediately kills the singleton process when no sessions hold
// references (refs == 0). Used by both Kill() and Terminate() to avoid the
// 30-minute idle drain wait.
//
// This skips the graceful SIGTERM→5s→SIGKILL protocol used by Shutdown()
// because an idle process has no active sessions — there is nothing to
// drain or notify.
//
// We use ForceKill(pgid) directly instead of proc.Manager.Kill() to avoid
// a deadlock: Manager.Kill() acquires proc.mu and then calls cmd.Wait(),
// which blocks until the child exits; meanwhile monitorProcess's Wait()
// (via waitOnce) is already holding proc.mu inside its own cmd.Wait()
// call. These two concurrent cmd.Wait() acquisitions would deadlock.
// ForceKill sends SIGKILL by PGID without touching proc.mu, so the
// already-running monitorProcess.Wait() observes the exit and runs cleanup.
//
// NOTE: There is a benign TOCTOU window between the shouldKill check (under
// m.mu) and the ForceKill(pgid) call (after unlock). If the process exits
// in this window, ForceKill returns ESRCH, which is harmless and ignored.
func (m *CodexAppServerManager) KillIfIdle() {
	m.mu.Lock()
	if m.idleTimer != nil {
		m.idleTimer.Stop()
		m.idleTimer = nil
	}
	shouldKill := m.refs == 0 && m.state == stateRunning && m.pgid > 0
	pgid := m.pgid
	m.mu.Unlock()

	if shouldKill {
		m.log.Info("codex-app-server: killing idle process immediately", "pgid", pgid)
		_ = proc.ForceKill(pgid)
		// monitorProcess will observe the exit, set state to stateIdle, and clean up.
	}
}

func (m *CodexAppServerManager) buildEnv() []string {
	return base.BuildEnv(worker.SessionInfo{}, EnvBlocklist, "codex-app-server")
}

var _ interface{ IsRunning() bool } = (*CodexAppServerManager)(nil)
