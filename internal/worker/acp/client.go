package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
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

	writeMu sync.Mutex // serializes stdin writes (call + RespondPermission) // id → response channel

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
		NotificationCh: make(chan *JSONRPCNotification, 64),
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
	params := map[string]any{"cwd": cwd}
	if mcpServers != nil {
		params["mcpServers"] = mcpServers
	}
	return c.callSessionMethod(ctx, "session/new", params, "new session")
}

// LoadSession restores an existing ACP session.
func (c *ACPClient) LoadSession(ctx context.Context, sessionID, cwd string, mcpServers any) (*SessionResult, error) {
	params := map[string]any{"sessionId": sessionID, "cwd": cwd}
	if mcpServers != nil {
		params["mcpServers"] = mcpServers
	}
	return c.callSessionMethod(ctx, "session/load", params, "load session")
}

// ResumeSession resumes an interrupted ACP session.
func (c *ACPClient) ResumeSession(ctx context.Context, sessionID, cwd string, mcpServers any) (*SessionResult, error) {
	params := map[string]any{"sessionId": sessionID, "cwd": cwd}
	if mcpServers != nil {
		params["mcpServers"] = mcpServers
	}
	return c.callSessionMethod(ctx, "session/resume", params, "resume session")
}

// Prompt sends a user message to the active session and waits for the response.
// Notifications received during the prompt are dispatched to NotificationCh.
func (c *ACPClient) Prompt(ctx context.Context, sessionID, content string) (*PromptResult, error) {
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

// RespondPermission sends a response to a server-initiated request_permission.
// No pendingMu needed (does not access the pending map); writeMu serializes stdin writes.
func (c *ACPClient) RespondPermission(ctx context.Context, id json.RawMessage, outcome any) error {
	req := &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  mustMarshal(outcome),
	}
	c.writeMu.Lock()
	err := WriteMessage(c.stdin, req)
	c.writeMu.Unlock()
	if err != nil {
		return fmt.Errorf("acp respond permission: %w", err)
	}
	return nil
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
			c.log.Error("acp client: read error", "error", err)
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
				c.log.Warn("acp client: notification channel full, dropping", "method", m.Method)
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

	c.writeMu.Lock()
	err := WriteMessage(c.stdin, req)
	c.writeMu.Unlock()
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
	c.mu.Lock()
	ch, ok := c.pending[string(resp.ID)]
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
