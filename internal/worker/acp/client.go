package acp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/hrygo/hotplex/internal/worker/base"
)

// ─── ACP Client ──────────────────────────────────────────────────────────────

// ACPClient manages the JSON-RPC 2.0 lifecycle with an ACP agent process.
// It owns the read loop goroutine that dispatches incoming messages.
type ACPClient struct {
	stdin   io.Writer
	scanner *bufio.Scanner
	log     *slog.Logger
	nextID  atomic.Int64

	mu      sync.Mutex
	pending map[string]chan *JSONRPCResponse

	writeMu sync.Mutex // serializes stdin writes (call + RespondRequest)

	// NotificationCh delivers session/update notifications to the worker's readLoop.
	NotificationCh chan *JSONRPCNotification

	// RequestCh delivers server-initiated requests (e.g. request_permission) to the worker.
	RequestCh chan *JSONRPCRequest

	done chan struct{} // closed when read loop exits
}

// NewACPClient creates a new ACP client communicating over the given pipes.
func NewACPClient(stdin io.Writer, stdout io.Reader, log *slog.Logger) *ACPClient {
	if log == nil {
		log = slog.Default()
	}
	return &ACPClient{
		stdin:          stdin,
		scanner:        NewScanner(stdout),
		log:            log,
		pending:        make(map[string]chan *JSONRPCResponse),
		NotificationCh: make(chan *JSONRPCNotification, 256),
		RequestCh:      make(chan *JSONRPCRequest, 16),
		done:           make(chan struct{}),
	}
}

// ─── Lifecycle ───────────────────────────────────────────────────────────────

// Initialize sends the ACP initialize request (protocol version negotiation).
func (c *ACPClient) Initialize(ctx context.Context, clientInfo map[string]string) (*InitializeResult, error) {
	params := map[string]any{
		"protocolVersion":    1,
		"clientCapabilities": map[string]any{},
	}
	if len(clientInfo) > 0 {
		params["clientInfo"] = clientInfo
	}
	resp, err := c.call(ctx, "initialize", params)
	if err != nil {
		return nil, fmt.Errorf("acp initialize: %w", err)
	}
	var result InitializeResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("acp initialize: unmarshal result: %w", err)
	}
	return &result, nil
}

// NewSession creates a new ACP session.
func (c *ACPClient) NewSession(ctx context.Context, cwd string, mcpServers any) (*SessionResult, error) {
	params := map[string]any{"cwd": cwd, "mcpServers": normalizeMCPServers(mcpServers)}
	return c.callSessionMethod(ctx, "session/new", params, "new session")
}

// LoadSession restores an existing ACP session.
func (c *ACPClient) LoadSession(ctx context.Context, sessionID, cwd string, mcpServers any) (*SessionResult, error) {
	params := map[string]any{"sessionId": sessionID, "cwd": cwd, "mcpServers": normalizeMCPServers(mcpServers)}
	resp, err := c.call(ctx, "session/load", params)
	if err != nil {
		return nil, fmt.Errorf("acp load session: %w", err)
	}
	var result SessionResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("acp load session: unmarshal: %w", err)
	}
	// ACP spec: LoadSessionResponse has no sessionId field — the loaded
	// session keeps the ID the client passed in. Echo it back so callers
	// (worker.go) get a non-empty SessionID as expected.
	result.SessionID = sessionID
	return &result, nil
}

// ResumeSession resumes an interrupted ACP session.
func (c *ACPClient) ResumeSession(ctx context.Context, sessionID, cwd string, mcpServers any) (*SessionResult, error) {
	params := map[string]any{"sessionId": sessionID, "cwd": cwd, "mcpServers": normalizeMCPServers(mcpServers)}
	resp, err := c.call(ctx, "session/resume", params)
	if err != nil {
		return nil, fmt.Errorf("acp resume session: %w", err)
	}
	var result SessionResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("acp resume session: unmarshal: %w", err)
	}
	// Same as LoadSession: ResumeSessionResponse has no sessionId field.
	result.SessionID = sessionID
	return &result, nil
}

// Prompt sends a user message to the active session and waits for the response.
// Notifications received during the prompt are dispatched to NotificationCh.
func (c *ACPClient) Prompt(ctx context.Context, sessionID, content string) (*PromptResult, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("acp prompt: empty sessionId")
	}
	params := map[string]any{
		"sessionId": sessionID,
		"prompt": []map[string]string{
			{"type": "text", "text": content},
		},
	}
	resp, err := c.call(ctx, "session/prompt", params)
	if err != nil {
		return nil, fmt.Errorf("acp prompt: %w", err)
	}
	var result PromptResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("acp prompt: unmarshal: %w", err)
	}
	return &result, nil
}

// Cancel sends a session/cancel request to abort the current turn.
func (c *ACPClient) Cancel(ctx context.Context, sessionID string) error {
	_, err := c.call(ctx, "session/cancel", map[string]any{
		"sessionId": sessionID,
	})
	if err != nil {
		return fmt.Errorf("acp cancel: %w", err)
	}
	return nil
}

// RespondRequest sends a JSON-RPC response to a server-initiated request
// (permission, question, or elicitation). It is the canonical name;
// RespondPermission is kept as a compatibility alias.
// No pendingMu needed (does not access the pending map); writeMu serializes stdin writes.
func (c *ACPClient) RespondRequest(ctx context.Context, id json.RawMessage, outcome any) error {
	req := &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  mustMarshal(outcome),
	}
	// Lock inside the closure so an orphaned write (ctx cancelled while
	// syscall.Write is blocked) keeps writeMu until the write completes,
	// preventing concurrent writes on the shared stdin fd.
	err := base.WriteWithCtxBounded(ctx, func() error {
		c.writeMu.Lock()
		defer c.writeMu.Unlock()
		return WriteMessage(c.stdin, req)
	})
	if err != nil {
		return fmt.Errorf("acp respond request: %w", err)
	}
	return nil
}

// RespondPermission is a compatibility alias for RespondRequest.
func (c *ACPClient) RespondPermission(ctx context.Context, id json.RawMessage, outcome any) error {
	return c.RespondRequest(ctx, id, outcome)
}

// SetSessionModel switches the model for an active session .
func (c *ACPClient) SetSessionModel(ctx context.Context, sessionID, modelID string) error {
	_, err := c.call(ctx, "session/set_model", map[string]any{
		"sessionId": sessionID,
		"modelId":   modelID,
	})
	if err != nil {
		return fmt.Errorf("acp set model: %w", err)
	}
	return nil
}

// SetSessionMode switches the execution mode for an active session .
func (c *ACPClient) SetSessionMode(ctx context.Context, sessionID, modeID string) error {
	_, err := c.call(ctx, "session/set_mode", map[string]any{
		"sessionId": sessionID,
		"modeId":    modeID,
	})
	if err != nil {
		return fmt.Errorf("acp set mode: %w", err)
	}
	return nil
}

// ForkSession forks an active session .
func (c *ACPClient) ForkSession(ctx context.Context, sessionID string) (*SessionResult, error) {
	resp, err := c.call(ctx, "session/fork", map[string]any{
		"sessionId": sessionID,
	})
	if err != nil {
		return nil, fmt.Errorf("acp fork: %w", err)
	}
	var result SessionResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("acp fork: unmarshal: %w", err)
	}
	return &result, nil
}

// ListSessions lists all sessions .
func (c *ACPClient) ListSessions(ctx context.Context) ([]SessionInfo, error) {
	resp, err := c.call(ctx, "session/list", map[string]any{})
	if err != nil {
		return nil, fmt.Errorf("acp list sessions: %w", err)
	}
	var result []SessionInfo
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("acp list sessions: unmarshal: %w", err)
	}
	return result, nil
}

// ─── Read Loop ───────────────────────────────────────────────────────────────

// StartReadLoop launches the background goroutine that reads from stdout.
func (c *ACPClient) StartReadLoop(ctx context.Context) {
	go c.readLoop(ctx)
}

func (c *ACPClient) readLoop(ctx context.Context) {
	defer close(c.done)
	defer close(c.NotificationCh)
	defer close(c.RequestCh)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		msg, err := ReadMessage(c.scanner)
		if err != nil {
			if ctx.Err() != nil {
				return // context cancelled, expected
			}
			if errors.Is(err, io.EOF) {
				c.log.Debug("acp client: agent stdout closed")
				return
			}
			c.log.Error("acp client: read error", "err", err)
			return
		}
		if msg == nil {
			continue // blank line
		}

		switch m := msg.(type) {
		case *JSONRPCResponse:
			c.dispatchResponse(m)
		case *JSONRPCNotification:
			select {
			case c.NotificationCh <- m:
			case <-ctx.Done():
				return
			default:
				c.log.Warn("acp client: notification channel full, dropping",
					"method", m.Method, "channel_cap", cap(c.NotificationCh))
			}
		case *JSONRPCRequest:
			select {
			case c.RequestCh <- m:
			case <-ctx.Done():
				return
			default:
				c.log.Warn("acp client: request channel full, dropping", "method", m.Method)
			}
		}
	}
}

// Done returns a channel that is closed when the read loop exits.
func (c *ACPClient) Done() <-chan struct{} { return c.done }

// ─── Internal ────────────────────────────────────────────────────────────────

// call sends a JSON-RPC request and blocks until the response arrives or ctx expires.
func (c *ACPClient) call(ctx context.Context, method string, params any) (*JSONRPCResponse, error) {
	id := c.nextID.Add(1)
	idRaw := mustMarshal(id)

	req := &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      idRaw,
		Method:  method,
		Params:  mustMarshal(params),
	}

	ch := make(chan *JSONRPCResponse, 1)
	c.mu.Lock()
	c.pending[string(idRaw)] = ch
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.pending, string(idRaw))
		c.mu.Unlock()
	}()

	// WriteMessage blocks when the pipe buffer is full and the agent process
	// stops reading. Guard with ctx so the caller is not held forever.
	// Lock inside the closure so an orphaned write keeps writeMu until the
	// write completes, preventing concurrent writes on the shared stdin fd.
	err := base.WriteWithCtxBounded(ctx, func() error {
		c.writeMu.Lock()
		defer c.writeMu.Unlock()
		return WriteMessage(c.stdin, req)
	})
	if err != nil {
		return nil, err
	}

	select {
	case resp := <-ch:
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("acp call %q: %w", method, ctx.Err())
	}
}

func (c *ACPClient) dispatchResponse(resp *JSONRPCResponse) {
	key := string(resp.ID)

	c.mu.Lock()
	ch, ok := c.pending[key]
	if !ok {
		// JSON-RPC 2.0 allows integer or string IDs. If the agent returned
		// a different format (e.g., "1" vs 1), try the alternate form.
		if altKey := alternateIDKey(resp.ID); altKey != key {
			ch, ok = c.pending[altKey]
		}
	}
	c.mu.Unlock()

	if !ok {
		c.log.Warn("acp client: unmatched response", "id", string(resp.ID))
		return
	}
	select {
	case ch <- resp:
	default:
		c.log.Warn("acp client: response channel full", "id", string(resp.ID))
	}
}

// alternateIDKey returns the pending-map key for the opposite JSON-RPC ID format.
// If the ID is a JSON number (e.g., 1), it returns the string form ("1").
// If the ID is a JSON string (e.g., "1"), it returns the numeric form (1).
func alternateIDKey(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return ""
	}
	if raw[0] == '"' {
		// String ID → try numeric form: strip quotes.
		var s string
		if json.Unmarshal(raw, &s) == nil {
			if n, err := strconv.ParseInt(s, 10, 64); err == nil {
				return string(mustMarshal(n))
			}
		}
	} else {
		// Numeric ID → try string form.
		var n int64
		if json.Unmarshal(raw, &n) == nil {
			return string(mustMarshal(strconv.FormatInt(n, 10)))
		}
	}
	return string(raw)
}

// callSessionMethod is a helper for session/* RPCs that return SessionResult.
func (c *ACPClient) callSessionMethod(ctx context.Context, method string, params map[string]any, label string) (*SessionResult, error) {
	resp, err := c.call(ctx, method, params)
	if err != nil {
		return nil, fmt.Errorf("acp %s: %w", label, err)
	}
	var result SessionResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("acp %s: unmarshal: %w", label, err)
	}
	if result.SessionID == "" {
		return nil, fmt.Errorf("acp %s: agent returned empty sessionId", label)
	}
	return &result, nil
}

func mustMarshal(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("acp: marshal: %v", err))
	}
	return data
}

// ─── ACP Result Types ────────────────────────────────────────────────────────

// InitializeResult is the result of the ACP initialize handshake.
type InitializeResult struct {
	ProtocolVersion   int            `json:"protocolVersion"`
	AgentInfo         AgentInfo      `json:"agentInfo"`
	AgentCapabilities map[string]any `json:"agentCapabilities,omitempty"`
}

// AgentInfo describes the ACP agent.
type AgentInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// SessionResult is returned by session/new, session/load, session/resume, session/fork.
type SessionResult struct {
	SessionID     string          `json:"sessionId"`
	Models        json.RawMessage `json:"models,omitempty"`
	Modes         json.RawMessage `json:"modes,omitempty"`
	ConfigOptions json.RawMessage `json:"configOptions,omitempty"`
}

// SessionInfo is a summary of an ACP session (returned by session/list).
type SessionInfo struct {
	SessionID string `json:"sessionId"`
}

// PromptResult is the result of a session/prompt call.
type PromptResult struct {
	StopReason string      `json:"stopReason"`
	Usage      PromptUsage `json:"usage"`
}

// PromptUsage carries token usage from a prompt response.
type PromptUsage struct {
	InputTokens       int `json:"inputTokens"`
	OutputTokens      int `json:"outputTokens"`
	ThoughtTokens     int `json:"thoughtTokens,omitempty"`
	CachedReadTokens  int `json:"cachedReadTokens,omitempty"`
	CachedWriteTokens int `json:"cachedWriteTokens,omitempty"`
	TotalTokens       int `json:"totalTokens,omitempty"`
}

// ─── Env blocklist ───────────────────────────────────────────────────────────
// NOTE: Environment construction is handled by base.BuildEnv (base/env.go),
// which provides 7 security layers including prefix stripping, nested agent
// protection, and blocklist enforcement. ACP worker calls it via
// base.BuildEnv(session, acpEnvBlocklist, "acp").

// normalizeMCPServers ensures mcpServers is never nil (null in JSON).
// Some ACP agents require a list, not null.
// Handles Go's typed-nil trap: []any(nil) != nil as interface.
func normalizeMCPServers(mcpServers any) any {
	if mcpServers == nil {
		return []any{}
	}
	if s, ok := mcpServers.([]any); ok && len(s) == 0 {
		return []any{}
	}
	return mcpServers
}
