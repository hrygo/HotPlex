package codexcli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
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
	// defaultCallTimeout is the maximum time to wait for a JSON-RPC response.
	defaultCallTimeout = 30 * time.Second

	// scannerInitSize is the initial buffer size for stdout scanning.
	scannerInitSize = 64 * 1024 // 64 KB

	// scannerMaxSize is the maximum buffer size per line.
	scannerMaxSize = 10 * 1024 * 1024 // 10 MB
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

func (m *CodexAppServerManager) Unsubscribe(threadID string) {
	m.subMu.Lock()
	defer m.subMu.Unlock()

	if ch, ok := m.subscribers[threadID]; ok {
		delete(m.subscribers, threadID)
		delete(m.subSessions, threadID)
		close(ch)
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

	timer := time.NewTimer(defaultCallTimeout)
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
			method, defaultCallTimeout)
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
		_ = m.proc.Kill()
		m.proc = nil
		m.stdin = nil
		m.stdout = nil
		m.refs = 0
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

	stdin, stdout, _, err := m.proc.Start(context.Background(), binary, fullArgs, env, "")
	if err != nil {
		m.proc = nil
		m.state = stateIdle
		return fmt.Errorf("codex-app-server: start process: %w", err)
	}

	m.stdin = stdin
	m.stdout = stdout

	bgCtx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	go m.readNotifications(bgCtx)
	go m.monitorProcess()

	if err := m.handshake(ctx); err != nil {
		cancel()
		_ = m.proc.Kill()
		m.proc = nil
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

// writeRequest marshals and writes a JSON-RPC request to stdin.
func (m *CodexAppServerManager) writeRequest(req *JSONRPCRequest) error {
	m.writeMu.Lock()
	defer m.writeMu.Unlock()

	return json.NewEncoder(m.stdin).Encode(req)
}

// writeNotification marshals and writes a JSON-RPC notification to stdin.
func (m *CodexAppServerManager) writeNotification(notif *JSONRPCNotification) error {
	m.writeMu.Lock()
	defer m.writeMu.Unlock()

	return json.NewEncoder(m.stdin).Encode(notif)
}

// readNotifications reads JSON-RPC frames from stdout and routes them to
// pending response channels or subscriber notification channels.
func (m *CodexAppServerManager) readNotifications(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			m.log.Error("codex-app-server: readNotifications panic",
				"panic", r, "stack", string(debug.Stack()))
		}
	}()

	m.mu.Lock()
	reader := m.stdout
	m.mu.Unlock()

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

// dispatchFrame parses a single JSON-RPC frame once and routes to response or notification.
//
// Routing logic (in order):
//  1. ID != 0 → this is a response (request IDs start at 1, monotonically increasing)
//  2. ID == 0 && Error != nil → error notification or response to ID 0 (which never
//     exists in pending — dropped silently, logged as debug)
//  3. ID == 0 && Error == nil → try to parse as notification by method presence
func (m *CodexAppServerManager) dispatchFrame(data []byte) {
	var resp JSONRPCResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		m.log.Warn("codex-app-server: unmarshal frame", "err", err)
		return
	}

	if resp.ID != 0 {
		m.dispatchResponse(&resp)
		return
	}

	// ID == 0: either a notification or a response to an unknown request.
	if resp.Error != nil {
		// Error with no ID — treat as notification-like (e.g. server-side error).
		// Since no pending request has ID 0, dispatchResponse will drop it silently.
		m.log.Debug("codex-app-server: response with ID=0, dropping", "error", resp.Error.Message)
		m.dispatchResponse(&resp)
		return
	}

	var notif JSONRPCNotification
	if err := json.Unmarshal(data, &notif); err != nil {
		m.log.Warn("codex-app-server: unmarshal notification", "err", err)
		return
	}
	if notif.Method != "" {
		m.dispatchNotification(&notif)
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
		m.log.Debug("codex-app-server: notification without threadId, skipping", "method", notif.Method)
		return
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
// Delta events are dropped silently when full; critical events block until delivered.
func (m *CodexAppServerManager) sendEnvelope(ch chan *events.Envelope, env *events.Envelope) {
	if env.Event.Type == events.MessageDelta {
		select {
		case ch <- env:
		default:
		}
		return
	}
	ch <- env
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

func (m *CodexAppServerManager) buildEnv() []string {
	return base.BuildEnv(worker.SessionInfo{}, EnvBlocklist, "codex-app-server")
}

var _ interface{ IsRunning() bool } = (*CodexAppServerManager)(nil)
