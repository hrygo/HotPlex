package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/internal/worker/base"
	"github.com/hrygo/hotplex/pkg/events"
)

// ─── FR-06: Question/Elicitation Response ─────────────────────────────────────

func TestHandleServerRequest_UnknownMethod_RawPassthrough(t *testing.T) {
	t.Parallel()
	w := &Worker{BaseWorker: base.NewBaseWorker(nil, nil)}
	w.mapper = newTestMapper()
	conn := newACPConn("u1", "s1", nil)

	req := &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      mustMarshal(42),
		Method:  "session/request_question",
		Params:  mustMarshal(map[string]any{"question": "What is 2+2?"}),
	}

	w.handleServerRequest(context.Background(), req, conn)

	// Should forward as Raw event.
	env := <-conn.Recv()
	require.Equal(t, events.Raw, env.Event.Type)

	rawBytes, err := json.Marshal(env.Event.Data)
	require.NoError(t, err)
	var raw events.RawData
	require.NoError(t, json.Unmarshal(rawBytes, &raw))
	require.Equal(t, "acp.server_request.session/request_question", raw.Kind)

	rm, ok := raw.Raw.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "42", rm["id"])
	require.Equal(t, "session/request_question", rm["method"])

	// Should be stored in pendingRequests.
	_, ok = w.pendingRequests.Load("42")
	require.True(t, ok)
}

func TestHandleQuestionResponse_ForwardsToAgent(t *testing.T) {
	t.Parallel()

	req := &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      mustMarshal(99),
		Method:  "session/request_question",
	}

	// agentStdinR/W: client writes requests here, agent reads them.
	// agentStdoutR/W: agent writes responses here, client reads them.
	agentStdinR, agentStdinW := io.Pipe()
	agentStdoutR, agentStdoutW := io.Pipe()
	defer agentStdinW.Close()
	defer agentStdoutW.Close()

	client := NewACPClient(agentStdinW, agentStdoutR, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client.StartReadLoop(ctx)

	w := &Worker{BaseWorker: base.NewBaseWorker(nil, nil)}
	w.client = client
	w.pendingRequests.Store("99", req)

	// Agent goroutine: read the JSON-RPC response that the worker writes.
	go func() {
		scanner := NewScanner(agentStdinR)
		scanner.Scan()
		_ = scanner.Bytes()
		agentStdoutW.Close()
	}()

	err := w.HandleQuestionResponse(ctx, "99", map[string]string{"answer": "42"})
	require.NoError(t, err)
}

func TestHandleQuestionResponse_NoPendingRequest(t *testing.T) {
	t.Parallel()
	w := &Worker{BaseWorker: base.NewBaseWorker(nil, nil)}
	err := w.HandleQuestionResponse(context.Background(), "nonexistent", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no pending question request")
}

func TestHandleElicitationResponse_ForwardsToAgent(t *testing.T) {
	t.Parallel()

	req := &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      mustMarshal(77),
		Method:  "session/request_elicitation",
	}

	agentStdinR, agentStdinW := io.Pipe()
	agentStdoutR, agentStdoutW := io.Pipe()
	defer agentStdinW.Close()
	defer agentStdoutW.Close()

	client := NewACPClient(agentStdinW, agentStdoutR, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client.StartReadLoop(ctx)

	w := &Worker{BaseWorker: base.NewBaseWorker(nil, nil)}
	w.client = client
	w.pendingRequests.Store("77", req)

	go func() {
		scanner := NewScanner(agentStdinR)
		scanner.Scan()
		_ = scanner.Bytes()
		agentStdoutW.Close()
	}()

	err := w.HandleElicitationResponse(ctx, "77", "accept", map[string]any{"file": "main.go"})
	require.NoError(t, err)
}

func TestHandleElicitationResponse_NoPendingRequest(t *testing.T) {
	t.Parallel()
	w := &Worker{BaseWorker: base.NewBaseWorker(nil, nil)}
	err := w.HandleElicitationResponse(context.Background(), "missing", "accept", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no pending elicitation request")
}

// ─── FR-07: ResetContext + InPlaceReset ────────────────────────────────────────

func TestInPlaceReset_ReturnsTrue(t *testing.T) {
	t.Parallel()
	w := &Worker{BaseWorker: base.NewBaseWorker(nil, nil)}
	require.True(t, w.InPlaceReset())
}

func TestResetContext_DelegatesToResetSession(t *testing.T) {
	t.Parallel()
	// Verify ResetContext no longer returns ErrNotImplemented.
	w := &Worker{BaseWorker: base.NewBaseWorker(nil, nil)}
	err := w.ResetContext(context.Background())
	require.Error(t, err)
	// Should be "worker not started" not ErrNotImplemented.
	require.False(t, errors.Is(err, worker.ErrNotImplemented))
	require.Contains(t, err.Error(), "reset: worker not started")
}

func TestClear_DelegatesToResetSession(t *testing.T) {
	t.Parallel()
	w := &Worker{BaseWorker: base.NewBaseWorker(nil, nil)}
	err := w.Clear(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "reset: worker not started")
}

// ─── U-02: Agent Discovery ────────────────────────────────────────────────────

func TestHealth_IncludesAgentInfo(t *testing.T) {
	t.Parallel()
	w := &Worker{BaseWorker: base.NewBaseWorker(nil, nil)}
	w.initResult = &InitializeResult{
		ProtocolVersion: 1,
		AgentInfo:       AgentInfo{Name: "hermes", Version: "0.3.0"},
	}

	h := w.Health()
	require.Equal(t, "hermes", h.AgentName)
	require.Equal(t, "0.3.0", h.AgentVersion)
	require.Equal(t, 1, h.ProtocolVersion)
}

func TestHealth_NoAgentInfo(t *testing.T) {
	t.Parallel()
	w := &Worker{BaseWorker: base.NewBaseWorker(nil, nil)}
	h := w.Health()
	require.Empty(t, h.AgentName)
	require.Empty(t, h.AgentVersion)
	require.Equal(t, 0, h.ProtocolVersion)
}

func TestSupportsCapability_AllVariants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		init   *InitializeResult
		cap    string
		expect bool
	}{
		{
			name:   "nil init result assumes supported",
			init:   nil,
			cap:    "loadSession",
			expect: true,
		},
		{
			name:   "nil capabilities assumes supported",
			init:   &InitializeResult{AgentCapabilities: nil},
			cap:    "loadSession",
			expect: true,
		},
		{
			name:   "missing capability assumes supported",
			init:   &InitializeResult{AgentCapabilities: map[string]any{}},
			cap:    "loadSession",
			expect: true,
		},
		{
			name:   "explicitly true",
			init:   &InitializeResult{AgentCapabilities: map[string]any{"loadSession": true}},
			cap:    "loadSession",
			expect: true,
		},
		{
			name:   "explicitly false",
			init:   &InitializeResult{AgentCapabilities: map[string]any{"loadSession": false}},
			cap:    "loadSession",
			expect: false,
		},
		{
			name:   "non-bool value assumes supported",
			init:   &InitializeResult{AgentCapabilities: map[string]any{"loadSession": "yes"}},
			cap:    "loadSession",
			expect: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := &Worker{BaseWorker: base.NewBaseWorker(nil, nil)}
			w.initResult = tc.init
			require.Equal(t, tc.expect, w.supportsCapability(tc.cap))
		})
	}
}

// ─── U-03: Error Messages ─────────────────────────────────────────────────────

func TestFmtStartError_BinaryNotFound(t *testing.T) {
	t.Parallel()
	w := &Worker{BaseWorker: base.NewBaseWorker(nil, nil)}
	err := w.fmtStartError("hermes", errors.New("exec: executable file not found in $PATH"))
	require.Contains(t, err.Error(), "not found in PATH")
	require.Contains(t, err.Error(), "Install the agent")
}

func TestFmtStartError_PermissionDenied(t *testing.T) {
	t.Parallel()
	w := &Worker{BaseWorker: base.NewBaseWorker(nil, nil)}
	err := w.fmtStartError("hermes", errors.New("fork/exec: permission denied"))
	require.Contains(t, err.Error(), "not executable")
	require.Contains(t, err.Error(), "execute permission")
}

func TestFmtStartError_Other(t *testing.T) {
	t.Parallel()
	w := &Worker{BaseWorker: base.NewBaseWorker(nil, nil)}
	err := w.fmtStartError("hermes", errors.New("some other error"))
	require.Contains(t, err.Error(), "failed to start process")
}

func TestFmtHandshakeError_Timeout(t *testing.T) {
	t.Parallel()
	w := &Worker{BaseWorker: base.NewBaseWorker(nil, nil)}
	err := w.fmtHandshakeError(context.DeadlineExceeded)
	require.Contains(t, err.Error(), "timed out after 30s")
	require.Contains(t, err.Error(), "agent is running")
}

func TestFmtHandshakeError_Other(t *testing.T) {
	t.Parallel()
	w := &Worker{BaseWorker: base.NewBaseWorker(nil, nil)}
	err := w.fmtHandshakeError(errors.New("connection refused"))
	require.Contains(t, err.Error(), "initialize handshake")
}

// ─── T-03: Client Extended Tests ───────────────────────────────────────────────

func TestACPClient_Cancel_NoActiveTurn(t *testing.T) {
	t.Parallel()

	agentStdinR, agentStdinW := io.Pipe()
	agentStdoutR, agentStdoutW := io.Pipe()
	defer agentStdinW.Close()
	defer agentStdoutW.Close()

	client := NewACPClient(agentStdinW, agentStdoutR, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client.StartReadLoop(ctx)

	// Agent goroutine: read the cancel request, respond with empty result.
	go func() {
		scanner := bufio.NewScanner(agentStdinR)
		if scanner.Scan() {
			var req struct {
				ID json.RawMessage `json:"id"`
			}
			_ = json.Unmarshal(scanner.Bytes(), &req)
			resp := &JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  json.RawMessage(`{}`),
			}
			_ = WriteMessage(agentStdoutW, resp)
		}
	}()

	err := client.Cancel(ctx, "nonexistent_session")
	require.NoError(t, err)
}

func TestACPClient_ServerRequest_UnknownMethod(t *testing.T) {
	t.Parallel()

	// Simulate agent sending a request: agent writes to agentStdoutW, client reads from agentStdoutR.
	agentStdoutR, agentStdoutW := io.Pipe()
	client := NewACPClient(io.Discard, agentStdoutR, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client.StartReadLoop(ctx)

	req := &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      mustMarshal(1),
		Method:  "custom/unknown_method",
		Params:  mustMarshal(map[string]any{"key": "value"}),
	}
	require.NoError(t, WriteMessage(agentStdoutW, req))

	select {
	case got := <-client.RequestCh:
		require.Equal(t, "custom/unknown_method", got.Method)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for request")
	}
	agentStdoutW.Close()
}

func TestACPClient_Initialize_VersionMismatch(t *testing.T) {
	t.Parallel()

	agentStdinR, agentStdinW := io.Pipe()
	agentStdoutR, agentStdoutW := io.Pipe()
	defer agentStdinW.Close()
	defer agentStdoutW.Close()

	client := NewACPClient(agentStdinW, agentStdoutR, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client.StartReadLoop(ctx)

	// Agent goroutine: read the initialize request, respond with protocol v2.
	go func() {
		scanner := bufio.NewScanner(agentStdinR)
		if scanner.Scan() {
			msg, _ := ReadMessage(bufio.NewScanner(strings.NewReader(string(scanner.Bytes()) + "\n")))
			_ = msg
			// Parse the request to get its ID.
			var req struct {
				ID json.RawMessage `json:"id"`
			}
			_ = json.Unmarshal(scanner.Bytes(), &req)

			resp := &JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result: mustMarshal(InitializeResult{
					ProtocolVersion: 2,
					AgentInfo:       AgentInfo{Name: "test", Version: "1.0"},
				}),
			}
			_ = WriteMessage(agentStdoutW, resp)
		}
	}()

	result, err := client.Initialize(ctx, map[string]string{"name": "test"})
	require.NoError(t, err)
	require.Equal(t, 2, result.ProtocolVersion)
}

func TestACPClient_Prompt_LargeContent(t *testing.T) {
	t.Parallel()

	agentStdinR, agentStdinW := io.Pipe()
	agentStdoutR, agentStdoutW := io.Pipe()
	defer agentStdinW.Close()
	defer agentStdoutW.Close()

	client := NewACPClient(agentStdinW, agentStdoutR, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client.StartReadLoop(ctx)

	largeContent := strings.Repeat("x", 101*1024)

	go func() {
		scanner := bufio.NewScanner(agentStdinR)
		scanner.Buffer(make([]byte, 256*1024), 256*1024)
		if scanner.Scan() {
			var req struct {
				ID json.RawMessage `json:"id"`
			}
			_ = json.Unmarshal(scanner.Bytes(), &req)

			resp := &JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  mustMarshal(PromptResult{StopReason: "end_turn"}),
			}
			_ = WriteMessage(agentStdoutW, resp)
		}
	}()

	result, err := client.Prompt(ctx, "sess_1", largeContent)
	require.NoError(t, err)
	require.Equal(t, "end_turn", result.StopReason)
}

// ─── T-02: Mapper Extended Tests ───────────────────────────────────────────────

func TestMapNotification_UnknownUpdateType_Skipped(t *testing.T) {
	t.Parallel()
	m := newTestMapper()

	notif := &JSONRPCNotification{
		JSONRPC: "2.0",
		Method:  "session/update",
		Params: mustMarshal(map[string]any{
			"sessionId": "s1",
			"update": map[string]any{
				"sessionUpdate": "future_unknown_type",
				"data":          "some data",
			},
		}),
	}

	envs := m.MapNotification(notif)
	require.Nil(t, envs)
}

func TestMapNotification_NonSessionUpdate_Skipped(t *testing.T) {
	t.Parallel()
	m := newTestMapper()

	notif := &JSONRPCNotification{
		JSONRPC: "2.0",
		Method:  "some/other/method",
		Params:  mustMarshal(map[string]any{}),
	}

	envs := m.MapNotification(notif)
	require.Nil(t, envs)
}

func TestMapNotification_MalformedParams_Skipped(t *testing.T) {
	t.Parallel()
	m := newTestMapper()

	notif := &JSONRPCNotification{
		JSONRPC: "2.0",
		Method:  "session/update",
		Params:  json.RawMessage(`{invalid}`),
	}

	envs := m.MapNotification(notif)
	require.Nil(t, envs)
}

// ─── Compile-time interface checks ─────────────────────────────────────────────

func TestWorkerImplementsInPlaceReseter(t *testing.T) {
	t.Parallel()
	var _ worker.InPlaceReseter = (*Worker)(nil)
}

func TestWorkerImplementsMetadataHandler(t *testing.T) {
	t.Parallel()
	var _ interface {
		HandlePermissionResponse(ctx context.Context, reqID string, allowed bool, reason string) error
		HandleQuestionResponse(ctx context.Context, reqID string, answers map[string]string) error
		HandleElicitationResponse(ctx context.Context, reqID string, action string, content map[string]any) error
	} = (*Worker)(nil)
}

// ─── Fix #8: resetSession happy path tests ──────────────────────────────────────

func TestResetSession_UpdatesSessionIDAndClearsPending(t *testing.T) {
	t.Parallel()

	agentStdinR, agentStdinW := io.Pipe()
	agentStdoutR, agentStdoutW := io.Pipe()
	defer agentStdinW.Close()
	defer agentStdoutW.Close()

	client := NewACPClient(agentStdinW, agentStdoutR, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client.StartReadLoop(ctx)

	w := &Worker{BaseWorker: base.NewBaseWorker(nil, nil)}
	w.client = client
	w.acpSessionID = "old_session"
	w.projectDir = "/tmp"
	w.mcpServers = nil
	w.mapper = newTestMapper()

	// Store stale entries that should be cleared.
	w.pendingRequests.Store("stale_req", &JSONRPCRequest{ID: mustMarshal(1)})
	w.pendingPerm.Store("stale_perm", nil)

	// Agent goroutine: handle Cancel then NewSession.
	go func() {
		scanner := bufio.NewScanner(agentStdinR)
		for i := 0; i < 2; i++ {
			if !scanner.Scan() {
				return
			}
			var req struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
			}
			_ = json.Unmarshal(scanner.Bytes(), &req)

			var result any
			switch req.Method {
			case "session/cancel":
				result = json.RawMessage(`{}`)
			case "session/new":
				result = SessionResult{SessionID: "new_session_42"}
			default:
				result = json.RawMessage(`{}`)
			}
			resp := &JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  mustMarshal(result),
			}
			_ = WriteMessage(agentStdoutW, resp)
		}
	}()

	err := w.resetSession(ctx)
	require.NoError(t, err)

	// Verify acpSessionID was updated.
	w.Mu.Lock()
	sid := w.acpSessionID
	w.Mu.Unlock()
	require.Equal(t, "new_session_42", sid)

	// Verify pending maps were cleared.
	_, ok := w.pendingRequests.Load("stale_req")
	require.False(t, ok, "pendingRequests should be cleared after resetSession")
	_, ok = w.pendingPerm.Load("stale_perm")
	require.False(t, ok, "pendingPerm should be cleared after resetSession")
}

func TestClear_IncResetGeneration(t *testing.T) {
	t.Parallel()

	agentStdinR, agentStdinW := io.Pipe()
	agentStdoutR, agentStdoutW := io.Pipe()
	defer agentStdinW.Close()
	defer agentStdoutW.Close()

	client := NewACPClient(agentStdinW, agentStdoutR, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client.StartReadLoop(ctx)

	w := &Worker{BaseWorker: base.NewBaseWorker(nil, nil)}
	w.client = client
	w.acpSessionID = "old"
	w.projectDir = "/tmp"
	w.mcpServers = nil
	w.mapper = newTestMapper()

	genBefore := w.LoadResetGeneration()

	go func() {
		scanner := bufio.NewScanner(agentStdinR)
		for i := 0; i < 2; i++ {
			if !scanner.Scan() {
				return
			}
			var req struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
			}
			_ = json.Unmarshal(scanner.Bytes(), &req)
			var result any = json.RawMessage(`{}`)
			if req.Method == "session/new" {
				result = SessionResult{SessionID: "new_sess"}
			}
			_ = WriteMessage(agentStdoutW, &JSONRPCResponse{
				JSONRPC: "2.0", ID: req.ID, Result: mustMarshal(result),
			})
		}
	}()

	err := w.Clear(ctx)
	require.NoError(t, err)

	genAfter := w.LoadResetGeneration()
	require.Equal(t, genBefore+1, genAfter, "Clear should increment reset generation exactly once")
}

func TestResetContext_DoesNotIncResetGeneration(t *testing.T) {
	t.Parallel()

	agentStdinR, agentStdinW := io.Pipe()
	agentStdoutR, agentStdoutW := io.Pipe()
	defer agentStdinW.Close()
	defer agentStdoutW.Close()

	client := NewACPClient(agentStdinW, agentStdoutR, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client.StartReadLoop(ctx)

	w := &Worker{BaseWorker: base.NewBaseWorker(nil, nil)}
	w.client = client
	w.acpSessionID = "old"
	w.projectDir = "/tmp"
	w.mcpServers = nil
	w.mapper = newTestMapper()

	genBefore := w.LoadResetGeneration()

	go func() {
		scanner := bufio.NewScanner(agentStdinR)
		for i := 0; i < 2; i++ {
			if !scanner.Scan() {
				return
			}
			var req struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
			}
			_ = json.Unmarshal(scanner.Bytes(), &req)
			var result any = json.RawMessage(`{}`)
			if req.Method == "session/new" {
				result = SessionResult{SessionID: "new_sess"}
			}
			_ = WriteMessage(agentStdoutW, &JSONRPCResponse{
				JSONRPC: "2.0", ID: req.ID, Result: mustMarshal(result),
			})
		}
	}()

	err := w.ResetContext(ctx)
	require.NoError(t, err)

	genAfter := w.LoadResetGeneration()
	require.Equal(t, genBefore, genAfter, "ResetContext should NOT increment generation (Bridge does it)")
}

func TestFmtStartError_ExecError(t *testing.T) {
	t.Parallel()
	w := &Worker{BaseWorker: base.NewBaseWorker(nil, nil)}
	err := w.fmtStartError("myagent", &exec.Error{Name: "myagent", Err: errors.New("executable file not found")})
	require.Contains(t, err.Error(), "not found in PATH")
	require.Contains(t, err.Error(), "Install the agent")
}

func TestFmtStartError_PathErrorPermission(t *testing.T) {
	t.Parallel()
	w := &Worker{BaseWorker: base.NewBaseWorker(nil, nil)}
	err := w.fmtStartError("myagent", &os.PathError{Op: "fork/exec", Path: "/usr/local/bin/myagent", Err: os.ErrPermission})
	require.Contains(t, err.Error(), "not executable")
	require.Contains(t, err.Error(), "execute permission")
}
