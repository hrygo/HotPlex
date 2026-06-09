package codexcli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/internal/worker/base"
	"github.com/hrygo/hotplex/pkg/events"
)

func TestTypeRegistration(t *testing.T) {
	types := []string{}
	for _, wt := range worker.RegisteredTypes() {
		types = append(types, string(wt))
	}
	require.Contains(t, types, string(worker.TypeCodexCLI))
}

// ─── v2 MapNotification Tests ───────────────────────────────────────────

func TestMapNotificationAgentMessageStateMachine(t *testing.T) {
	t.Parallel()

	m := NewMapper("session-1")

	// Step 1: item/started (agent_message) → message.start
	startedParams := json.RawMessage(`{"item":{"id":"msg_1","type":"agent_message"}}`)
	envs := m.MapNotification("item/started", startedParams)
	require.Len(t, envs, 1)
	require.Equal(t, events.MessageStart, envs[0].Event.Type)
	ms, ok := envs[0].Event.Data.(events.MessageStartData)
	require.True(t, ok)
	require.Equal(t, "assistant", ms.Role)
	require.NotEmpty(t, ms.ID)
	msgID := ms.ID

	// Step 2: item/agentMessage/delta (x3) → message.delta
	for i, word := range []string{"Hello", " world", "!"} {
		deltaParams := json.RawMessage(fmt.Sprintf(`{"itemId":"msg_1","delta":%q}`, word))
		envs = m.MapNotification("item/agentMessage/delta", deltaParams)
		require.Len(t, envs, 1, "delta %d", i)
		require.Equal(t, events.MessageDelta, envs[0].Event.Type)
		md, ok := envs[0].Event.Data.(events.MessageDeltaData)
		require.True(t, ok)
		require.Equal(t, msgID, md.MessageID)
		require.Equal(t, word, md.Content)
	}

	// Step 3: item/completed (agent_message) → message.end
	completedParams := json.RawMessage(`{"item":{"id":"msg_1","type":"agent_message","text":"Hello world!"}}`)
	envs = m.MapNotification("item/completed", completedParams)
	require.Len(t, envs, 1)
	require.Equal(t, events.MessageEnd, envs[0].Event.Type)
	me, ok := envs[0].Event.Data.(events.MessageEndData)
	require.True(t, ok)
	require.Equal(t, msgID, me.MessageID)
}

func TestMapNotificationTurnFailed(t *testing.T) {
	t.Parallel()

	m := NewMapper("session-1")
	envs := m.MapNotification("turn/failed", json.RawMessage(`{"turn":{}}`))
	require.Len(t, envs, 2)
	require.Equal(t, events.Error, envs[0].Event.Type)
	require.Equal(t, events.Done, envs[1].Event.Type)
	dd, ok := envs[1].Event.Data.(events.DoneData)
	require.True(t, ok)
	require.False(t, dd.Success)
}

func TestMapNotificationTurnCompleted(t *testing.T) {
	t.Parallel()

	m := NewMapper("session-1")

	// turn/started sets the current turn ID.
	m.MapNotification("turn/started", json.RawMessage(
		`{"threadId":"thr-1","turn":{"id":"turn-1","status":"inProgress"}}`))

	// Simulate token usage tracking from thread/tokenUsage/updated.
	tokenParams := json.RawMessage(`{"threadId":"thr-1","turnId":"turn-1","tokenUsage":{"last":{"totalTokens":250,"inputTokens":150,"outputTokens":75,"cachedInputTokens":20,"reasoningOutputTokens":5},"total":{"totalTokens":500,"inputTokens":300,"outputTokens":150}}}`)
	envs := m.MapNotification("thread/tokenUsage/updated", tokenParams)
	require.Nil(t, envs, "token usage tracking produces no envelopes")

	// Simulate model tracking from model/rerouted.
	modelParams := json.RawMessage(`{"threadId":"thr-1","turnId":"turn-1","fromModel":"gpt-4","toModel":"o3","reason":"highRiskCyberActivity"}`)
	envs = m.MapNotification("model/rerouted", modelParams)
	require.Nil(t, envs, "model rerouted tracking produces no envelopes")

	// turn/completed uses tracked usage and model.
	envs = m.MapNotification("turn/completed", json.RawMessage(`{"threadId":"thr-1","turn":{"id":"turn-1","status":"completed"}}`))
	require.Len(t, envs, 1)
	require.Equal(t, events.Done, envs[0].Event.Type)
	dd, ok := envs[0].Event.Data.(events.DoneData)
	require.True(t, ok)
	require.True(t, dd.Success)
	usage, ok := dd.Stats["usage"].(map[string]any)
	require.True(t, ok, "stats should have nested 'usage' map")
	require.Equal(t, int64(150), usage["input_tokens"])
	require.Equal(t, int64(75), usage["output_tokens"])
	require.Equal(t, int64(20), usage["cache_read_input_tokens"])
	modelUsage, ok := dd.Stats["model_usage"].(map[string]any)
	require.True(t, ok, "stats should have nested 'model_usage' map")
	require.Contains(t, modelUsage, "o3")
}

func TestMapNotificationApproval(t *testing.T) {
	t.Parallel()

	m := NewMapper("session-1")
	params := json.RawMessage(`{"requestId":"req_1","toolName":"Bash"}`)
	envs := m.MapNotification("serverRequest/approval", params)
	require.Len(t, envs, 1)
	require.Equal(t, events.PermissionRequest, envs[0].Event.Type)
	pr, ok := envs[0].Event.Data.(events.PermissionRequestData)
	require.True(t, ok)
	require.Equal(t, "req_1", pr.ID)
	require.Equal(t, "Bash", pr.ToolName)
}

func TestMapNotificationCommandExecution(t *testing.T) {
	t.Parallel()

	m := NewMapper("session-1")

	// Started
	started := json.RawMessage(`{"item":{"id":"cmd_1","type":"command_execution","command":"ls -la","cwd":"/tmp"}}`)
	envs := m.MapNotification("item/started", started)
	require.Len(t, envs, 1)
	require.Equal(t, events.ToolCall, envs[0].Event.Type)
	tc, ok := envs[0].Event.Data.(events.ToolCallData)
	require.True(t, ok)
	require.Equal(t, "Bash", tc.Name)
	require.Equal(t, "ls -la", tc.Input["command"])

	// Completed
	completed := json.RawMessage(`{"item":{"id":"cmd_1","type":"command_execution","stdout":"file1\nfile2","stderr":""}}`)
	envs = m.MapNotification("item/completed", completed)
	require.Len(t, envs, 1)
	require.Equal(t, events.ToolResult, envs[0].Event.Type)
	tr, ok := envs[0].Event.Data.(events.ToolResultData)
	require.True(t, ok)
	require.Equal(t, "file1\nfile2", tr.Output)
}

func TestMapNotificationReasoning(t *testing.T) {
	t.Parallel()

	m := NewMapper("session-1")
	params := json.RawMessage(`{"item":{"id":"r_1","type":"reasoning","summary_text":["Step 1","Step 2"]}}`)
	envs := m.MapNotification("item/completed", params)
	require.Len(t, envs, 1)
	require.Equal(t, events.Reasoning, envs[0].Event.Type)
	rd, ok := envs[0].Event.Data.(events.ReasoningData)
	require.True(t, ok)
	require.Equal(t, "Step 1\nStep 2", rd.Content)
}

func TestMapNotificationUnknownMethod(t *testing.T) {
	t.Parallel()

	m := NewMapper("session-1")
	envs := m.MapNotification("thread/started", json.RawMessage(`{}`))
	require.Nil(t, envs)
}

func TestMapNotificationOutputDelta(t *testing.T) {
	t.Parallel()

	m := NewMapper("session-1")

	// Simulate a command started first.
	started := json.RawMessage(`{"item":{"id":"cmd_1","type":"command_execution","command":"ls","cwd":"/tmp"}}`)
	envs := m.MapNotification("item/started", started)
	require.Len(t, envs, 1)
	require.Equal(t, events.ToolCall, envs[0].Event.Type)

	// Streaming output deltas.
	delta1 := json.RawMessage(`{"threadId":"thr-1","turnId":"turn-1","itemId":"cmd_1","delta":"file1\n"}`)
	envs = m.MapNotification("item/commandExecution/outputDelta", delta1)
	require.Len(t, envs, 1)
	require.Equal(t, events.ToolResult, envs[0].Event.Type)
	tr, ok := envs[0].Event.Data.(events.ToolResultData)
	require.True(t, ok)
	require.Equal(t, "cmd_1", tr.ID)
	require.Equal(t, "file1\n", tr.Output)

	delta2 := json.RawMessage(`{"threadId":"thr-1","turnId":"turn-1","itemId":"cmd_1","delta":"file2\n"}`)
	envs = m.MapNotification("item/commandExecution/outputDelta", delta2)
	require.Len(t, envs, 1)
	tr = envs[0].Event.Data.(events.ToolResultData)
	require.Equal(t, "file2\n", tr.Output)

	// Final item/completed overwrites with full output.
	completed := json.RawMessage(`{"item":{"id":"cmd_1","type":"command_execution","stdout":"file1\nfile2\n","exitCode":0}}`)
	envs = m.MapNotification("item/completed", completed)
	require.Len(t, envs, 1)
	require.Equal(t, events.ToolResult, envs[0].Event.Type)
	tr = envs[0].Event.Data.(events.ToolResultData)
	require.Equal(t, "file1\nfile2\n", tr.Output)
}

func TestMapNotificationOutputDeltaEmpty(t *testing.T) {
	t.Parallel()

	m := NewMapper("session-1")
	envs := m.MapNotification("item/commandExecution/outputDelta", json.RawMessage(
		`{"threadId":"thr-1","turnId":"turn-1","itemId":"","delta":"text"}`))
	require.Nil(t, envs, "empty itemId should produce no envelopes")
}

func TestMapNotificationReasoningDelta(t *testing.T) {
	t.Parallel()

	m := NewMapper("session-1")

	envs := m.MapNotification("item/reasoning/summaryTextDelta", json.RawMessage(
		`{"threadId":"thr-1","turnId":"turn-1","itemId":"r_1","delta":"Analyzing the code structure","summaryIndex":0}`))
	require.Len(t, envs, 1)
	require.Equal(t, events.Reasoning, envs[0].Event.Type)
	rd, ok := envs[0].Event.Data.(events.ReasoningData)
	require.True(t, ok)
	require.Equal(t, "Analyzing the code structure", rd.Content)
	require.Equal(t, "r_1", rd.ID)
}

func TestMapNotificationReasoningDeltaEmpty(t *testing.T) {
	t.Parallel()

	m := NewMapper("session-1")

	envs := m.MapNotification("item/reasoning/summaryTextDelta", json.RawMessage(
		`{"threadId":"thr-1","itemId":"r_1","delta":""}`))
	require.Nil(t, envs, "empty delta should produce no envelopes")
}

func TestMapNotificationWarning(t *testing.T) {
	t.Parallel()

	m := NewMapper("session-1")

	envs := m.MapNotification("warning", json.RawMessage(
		`{"threadId":"thr-1","message":"Rate limit approaching"}`))
	require.Len(t, envs, 1)
	require.Equal(t, events.Step, envs[0].Event.Type)
	sd, ok := envs[0].Event.Data.(events.StepData)
	require.True(t, ok)
	require.Equal(t, "warning", sd.StepType)
	require.Equal(t, "Rate limit approaching", sd.Name)
}

func TestMapNotificationError(t *testing.T) {
	t.Parallel()

	m := NewMapper("session-1")

	envs := m.MapNotification("error", json.RawMessage(
		`{"threadId":"thr-1","error":{"message":"API request failed: 502"}}`))
	require.Len(t, envs, 1)
	require.Equal(t, events.Error, envs[0].Event.Type)
	ed, ok := envs[0].Event.Data.(events.ErrorData)
	require.True(t, ok)
	require.Equal(t, events.ErrorCode("CODEX_ERROR"), ed.Code)
	require.Equal(t, "API request failed: 502", ed.Message)
}

func TestMapNotificationErrorEmptyMessage(t *testing.T) {
	t.Parallel()

	m := NewMapper("session-1")

	envs := m.MapNotification("error", json.RawMessage(`{"threadId":"thr-1","error":{}}`))
	require.Len(t, envs, 1)
	ed, _ := envs[0].Event.Data.(events.ErrorData)
	require.Equal(t, "unknown error", ed.Message)
}

func TestMapNotificationMCPProgress(t *testing.T) {
	t.Parallel()

	m := NewMapper("session-1")

	envs := m.MapNotification("item/mcpToolCall/progress", json.RawMessage(
		`{"threadId":"thr-1","turnId":"turn-1","itemId":"mcp_1","message":"Searching files..."}`))
	require.Len(t, envs, 1)
	require.Equal(t, events.Step, envs[0].Event.Type)
	sd, ok := envs[0].Event.Data.(events.StepData)
	require.True(t, ok)
	require.Equal(t, "mcp_progress", sd.StepType)
	require.Equal(t, "Searching files...", sd.Name)
	require.Equal(t, "mcp_1", sd.ID)
}

func TestMapNotificationTurnStartedResetsUsage(t *testing.T) {
	t.Parallel()

	m := NewMapper("session-1")

	// Track usage first.
	m.MapNotification("thread/tokenUsage/updated", json.RawMessage(
		`{"tokenUsage":{"last":{"totalTokens":100,"inputTokens":60,"outputTokens":40}}}`))
	require.NotNil(t, m.lastUsage)

	// turn/started resets tracked usage.
	envs := m.MapNotification("turn/started", json.RawMessage(
		`{"threadId":"thr-1","turn":{"id":"turn-2","status":"inProgress"}}`))
	require.Nil(t, envs)
	require.Nil(t, m.lastUsage)
	require.Equal(t, "turn-2", m.turnID)
}

func TestMapNotificationTurnCompletedNoUsage(t *testing.T) {
	t.Parallel()

	m := NewMapper("session-1")

	// turn/completed without prior token usage tracking → nil stats.
	envs := m.MapNotification("turn/completed", json.RawMessage(
		`{"threadId":"thr-1","turn":{"id":"turn-1","status":"completed"}}`))
	require.Len(t, envs, 1)
	dd, ok := envs[0].Event.Data.(events.DoneData)
	require.True(t, ok)
	require.True(t, dd.Success)
	require.Nil(t, dd.Stats)
}

func TestMapNotificationStaleTokenUsageRejected(t *testing.T) {
	t.Parallel()

	m := NewMapper("session-1")

	// Set up turn-1 with usage.
	m.MapNotification("turn/started", json.RawMessage(
		`{"threadId":"thr-1","turn":{"id":"turn-1","status":"inProgress"}}`))
	m.MapNotification("thread/tokenUsage/updated", json.RawMessage(
		`{"turnId":"turn-1","tokenUsage":{"last":{"totalTokens":100,"inputTokens":60,"outputTokens":40}}}`))
	require.NotNil(t, m.lastUsage)

	// Advance to turn-2 — lastUsage is cleared.
	m.MapNotification("turn/started", json.RawMessage(
		`{"threadId":"thr-1","turn":{"id":"turn-2","status":"inProgress"}}`))
	require.Nil(t, m.lastUsage)
	require.Equal(t, "turn-2", m.turnID)

	// Late token usage for turn-1 should be rejected.
	m.MapNotification("thread/tokenUsage/updated", json.RawMessage(
		`{"turnId":"turn-1","tokenUsage":{"last":{"totalTokens":100,"inputTokens":60,"outputTokens":40}}}`))
	require.Nil(t, m.lastUsage, "stale turn-1 usage should be rejected")
}

// ─── ParseNotification Tests ────────────────────────────────────────────

func TestParseNotification(t *testing.T) {
	t.Parallel()

	p := NewParser()

	t.Run("valid notification", func(t *testing.T) {
		t.Parallel()
		method, params, err := p.ParseNotification(
			`{"jsonrpc":"2.0","method":"item/started","params":{"item":{"id":"x","type":"agent_message"}}}`)
		require.NoError(t, err)
		require.Equal(t, "item/started", method)
		require.NotNil(t, params)
	})

	t.Run("invalid json", func(t *testing.T) {
		t.Parallel()
		_, _, err := p.ParseNotification("not json")
		require.Error(t, err)
	})

	t.Run("missing method", func(t *testing.T) {
		t.Parallel()
		_, _, err := p.ParseNotification(`{"jsonrpc":"2.0","params":{}}`)
		require.Error(t, err)
	})
}

func TestParseResponse(t *testing.T) {
	t.Parallel()

	p := NewParser()

	t.Run("valid response", func(t *testing.T) {
		t.Parallel()
		id, result, rpcErr, err := p.ParseResponse(
			`{"jsonrpc":"2.0","id":42,"result":{"thread":{"id":"thr_123"}}}`)
		require.NoError(t, err)
		require.Equal(t, int64(42), id)
		require.NotNil(t, result)
		require.Nil(t, rpcErr)
	})

	t.Run("error response", func(t *testing.T) {
		t.Parallel()
		id, result, rpcErr, err := p.ParseResponse(
			`{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"Method not found"}}`)
		require.NoError(t, err)
		require.Equal(t, int64(1), id)
		require.Nil(t, result)
		require.NotNil(t, rpcErr)
		require.Equal(t, -32601, rpcErr.Code)
	})
}

// ─── Manager Tests ──────────────────────────────────────────────────────

func TestManagerSubscribeUnsubscribe(t *testing.T) {
	t.Parallel()

	cfg := config.CodexCLIConfig{IdleDrainPeriod: time.Minute}
	mgr := NewCodexAppServerManager(slog.Default(), cfg)

	ch := mgr.Subscribe("thread-1", "session-thread-1")
	require.NotNil(t, ch)

	// Second subscribe returns same channel
	ch2 := mgr.Subscribe("thread-1", "session-thread-1")
	require.Equal(t, ch, ch2)

	// Different thread gets different channel
	ch3 := mgr.Subscribe("thread-2", "session-thread-2")
	require.NotNil(t, ch3)
	require.NotEqual(t, ch, ch3)

	// Unsubscribe removes from map (channel is closed by appConn or monitorProcess).
	mgr.Unsubscribe("thread-1")

	// Re-subscribe after unsubscribe creates new channel
	ch4 := mgr.Subscribe("thread-1", "session-thread-1")
	require.NotNil(t, ch4)
	require.NotEqual(t, ch, ch4)
}

func TestManagerReleaseIdleDrain(t *testing.T) {
	t.Parallel()
	cfg := config.CodexCLIConfig{IdleDrainPeriod: 10 * time.Millisecond}
	mgr := NewCodexAppServerManager(slog.Default(), cfg)

	mgr.mu.Lock()
	mgr.state = stateRunning
	mgr.refs = 1
	mgr.mu.Unlock()

	mgr.Release() // refs → 0, starts idle drain

	time.Sleep(30 * time.Millisecond)
	// Idle drain should have fired by now
	mgr.mu.Lock()
	require.Equal(t, stateRunning, mgr.state, "state should still be running since proc is nil (no actual kill)")
	mgr.mu.Unlock()
}

func TestManagerAcquireRejectsWhenStopped(t *testing.T) {
	t.Parallel()

	cfg := config.CodexCLIConfig{IdleDrainPeriod: time.Minute}
	mgr := NewCodexAppServerManager(slog.Default(), cfg)

	mgr.mu.Lock()
	mgr.state = stateStopped
	mgr.mu.Unlock()

	_, err := mgr.Acquire(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "stopped")
}

func TestManagerShutdown(t *testing.T) {
	t.Parallel()

	cfg := config.CodexCLIConfig{IdleDrainPeriod: time.Minute}
	mgr := NewCodexAppServerManager(slog.Default(), cfg)

	// Add some subscribers
	ch1 := mgr.Subscribe("thread-1", "session-thread-1")
	ch2 := mgr.Subscribe("thread-2", "session-thread-2")

	mgr.Shutdown(context.Background())

	// Verify state is stopped
	mgr.mu.Lock()
	require.Equal(t, stateStopped, mgr.state)
	mgr.mu.Unlock()

	// Subscriber channels should be closed
	_, ok1 := <-ch1
	require.False(t, ok1)
	_, ok2 := <-ch2
	require.False(t, ok2)
}

// ─── AppServerWorker Tests ──────────────────────────────────────────────

func TestAppServerWorkerCapabilities(t *testing.T) {
	t.Parallel()

	cfg := config.CodexCLIConfig{IdleDrainPeriod: time.Minute}
	mgr := NewCodexAppServerManager(slog.Default(), cfg)
	w := &AppServerWorker{
		BaseWorker: base.NewBaseWorker(slog.Default(), nil),
		manager:    mgr,
	}

	require.Equal(t, worker.TypeCodexCLI, w.Type())
	require.True(t, w.SupportsResume())
	require.True(t, w.SupportsStreaming())
	require.True(t, w.SupportsTools())
	require.NotEmpty(t, w.EnvBlocklist())
	require.Contains(t, w.Modalities(), "text")
	require.Contains(t, w.Modalities(), "code")
}

func TestAppServerWorkerConnNilBeforeStart(t *testing.T) {
	t.Parallel()

	cfg := config.CodexCLIConfig{IdleDrainPeriod: time.Minute}
	mgr := NewCodexAppServerManager(slog.Default(), cfg)
	w := &AppServerWorker{
		BaseWorker: base.NewBaseWorker(slog.Default(), nil),
		manager:    mgr,
	}

	require.Nil(t, w.Conn())
}

// ─── AppServerWorker Lifecycle Tests ─────────────────────────────────────

func newTestAppServerWorker(t *testing.T) *AppServerWorker {
	t.Helper()
	cfg := config.CodexCLIConfig{IdleDrainPeriod: time.Minute, StartupTimeout: time.Second, CallTimeout: time.Second}
	mgr := NewCodexAppServerManager(slog.Default(), cfg)
	return &AppServerWorker{
		BaseWorker: base.NewBaseWorker(slog.Default(), nil),
		manager:    mgr,
	}
}

func TestAppServerWorkerTerminate(t *testing.T) {
	t.Parallel()

	w := newTestAppServerWorker(t)
	err := w.Terminate(context.Background())
	require.NoError(t, err)
}

func TestAppServerWorkerKill(t *testing.T) {
	t.Parallel()

	w := newTestAppServerWorker(t)
	err := w.Kill()
	require.NoError(t, err)
}

func TestAppServerWorkerWaitNoCrashSub(t *testing.T) {
	t.Parallel()

	w := newTestAppServerWorker(t)
	code, err := w.Wait()
	require.NoError(t, err)
	require.Equal(t, 0, code)
}

func TestAppServerWorkerResume(t *testing.T) {
	t.Parallel()

	w := newTestAppServerWorker(t)
	// Resume delegates to Start, which will fail without manager running
	err := w.Resume(context.Background(), worker.SessionInfo{
		SessionID:  "sess-1",
		ProjectDir: t.TempDir(),
	})
	require.Error(t, err)
}

func TestAppServerWorkerHealthAndLastIO(t *testing.T) {
	t.Parallel()

	w := newTestAppServerWorker(t)
	health := w.Health()
	require.Equal(t, worker.TypeCodexCLI, health.Type)

	lastIO := w.LastIO()
	_ = lastIO // can be zero
}

func TestAppServerWorkerHandlePermissionResponse(t *testing.T) {
	t.Parallel()

	w := newTestAppServerWorker(t)
	err := w.HandlePermissionResponse(context.Background(), "req-1", true, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "no pending server request")
}

func TestAppServerWorkerHandleQuestionResponse(t *testing.T) {
	t.Parallel()

	w := newTestAppServerWorker(t)
	err := w.HandleQuestionResponse(context.Background(), "req-1", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no pending server request")
}

func TestAppServerWorkerHandleElicitationResponse(t *testing.T) {
	t.Parallel()

	w := newTestAppServerWorker(t)
	err := w.HandleElicitationResponse(context.Background(), "req-1", "", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no pending server request")
}

func TestAppServerWorkerInputNoThreadID(t *testing.T) {
	t.Parallel()

	w := newTestAppServerWorker(t)
	err := w.Input(context.Background(), "hello", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not started")
}

// ─── appConn tests ────────────────────────────────────────────────────────

func TestAppConnSendRecvClose(t *testing.T) {
	t.Parallel()

	cfg := config.CodexCLIConfig{IdleDrainPeriod: time.Minute}
	mgr := NewCodexAppServerManager(slog.Default(), cfg)
	ch := make(chan *events.Envelope, 5)
	conn := &appConn{
		userID:    "user-1",
		sessionID: "sess-1",
		recvCh:    ch,
		manager:   mgr,
	}

	require.Equal(t, "user-1", conn.UserID())
	require.Equal(t, "sess-1", conn.SessionID())
	require.Equal(t, (<-chan *events.Envelope)(ch), conn.Recv())

	// TrySend
	env := events.NewEnvelope("id-1", "sess-1", 1, events.MessageDelta, events.MessageDeltaData{Content: "hi"})
	require.True(t, conn.TrySend(env))
	got := <-ch
	require.Equal(t, events.MessageDelta, got.Event.Type)

	// Close
	require.NoError(t, conn.Close())
	require.NoError(t, conn.Close()) // idempotent
	_, ok := <-ch
	require.False(t, ok) // closed
}

func TestAppConnTrySendFull(t *testing.T) {
	t.Parallel()

	cfg := config.CodexCLIConfig{IdleDrainPeriod: time.Minute}
	mgr := NewCodexAppServerManager(slog.Default(), cfg)
	ch := make(chan *events.Envelope, 1)
	conn := &appConn{
		userID:    "user-1",
		sessionID: "sess-1",
		recvCh:    ch,
		manager:   mgr,
	}

	// Fill channel
	conn.TrySend(events.NewEnvelope("id-1", "sess-1", 1, events.Done, events.DoneData{}))
	// Next send should fail
	require.False(t, conn.TrySend(events.NewEnvelope("id-2", "sess-1", 2, events.Done, events.DoneData{})))
}

// ─── ParseNotification/ParseResponse edge cases ───────────────────────────

func TestParseNotificationMissingParams(t *testing.T) {
	t.Parallel()

	p := NewParser()
	method, params, err := p.ParseNotification(`{"jsonrpc":"2.0","method":"item/started"}`)
	require.NoError(t, err)
	require.Equal(t, "item/started", method)
	require.Nil(t, params)
}

func TestParseResponseMissingResult(t *testing.T) {
	t.Parallel()

	p := NewParser()
	id, result, rpcErr, err := p.ParseResponse(`{"jsonrpc":"2.0","id":5}`)
	require.NoError(t, err)
	require.Equal(t, int64(5), id)
	require.Nil(t, result)
	require.Nil(t, rpcErr)
}

// ─── config init tests ────────────────────────────────────────────────────

func TestInitConfigAndGetConfig(t *testing.T) {
	InitConfig(config.CodexCLIConfig{
		Command: "/usr/local/bin/codex",
		Model:   "o3",
	})
	cfg := GetConfig()
	require.Equal(t, "/usr/local/bin/codex", cfg.Command)
	require.Equal(t, "o3", cfg.Model)
}

func TestAppServerWorkerResetContext(t *testing.T) {
	t.Parallel()

	w := newTestAppServerWorker(t)
	// Don't set threadID to avoid triggering Notify on a nil manager connection
	_, err := w.ResetContext(context.Background())
	require.NoError(t, err)
	require.Empty(t, w.threadID)
	require.Nil(t, w.recvCh)
}

// ─── Manager Lifecycle Tests ──────────────────────────────────────────────

func TestManagerIsRunning(t *testing.T) {
	t.Parallel()

	cfg := config.CodexCLIConfig{IdleDrainPeriod: time.Minute}
	mgr := NewCodexAppServerManager(slog.Default(), cfg)

	require.False(t, mgr.IsRunning())

	mgr.mu.Lock()
	mgr.state = stateRunning
	mgr.mu.Unlock()
	require.True(t, mgr.IsRunning())
}

func TestManagerAcquireStartsProcess(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping: requires codex binary")
	}

	cfg := config.CodexCLIConfig{IdleDrainPeriod: time.Minute, StartupTimeout: 5 * time.Second, CallTimeout: 5 * time.Second}
	mgr := NewCodexAppServerManager(slog.Default(), cfg)
	t.Cleanup(func() { mgr.Shutdown(context.Background()) })

	crashCh, err := mgr.Acquire(context.Background())

	if err != nil {
		// Codex binary not available (CI or fresh environment).
		t.Skipf("codex binary not available: %v", err)
	}

	// Codex binary available — verify handshake completed and process is running.
	require.NoError(t, err)
	require.NotNil(t, crashCh)
	mgr.Release()
}

// ─── dispatchFrame / dispatchServerRequest / RespondServerRequest ──────

func TestDispatchFrameServerRequest(t *testing.T) {
	t.Parallel()

	cfg := config.CodexCLIConfig{IdleDrainPeriod: time.Minute}
	mgr := NewCodexAppServerManager(slog.Default(), cfg)

	// Subscribe to receive events for the thread.
	ch := mgr.Subscribe("thr-1", "sess-1")

	// Simulate a server-initiated approval request (has both ID and Method).
	frame := []byte(`{"jsonrpc":"2.0","id":99,"method":"serverRequest/approval","params":{"threadId":"thr-1","requestId":"req-42","toolName":"Bash","reason":"run ls"}}`)
	mgr.dispatchFrame(frame)

	// Should have stored requestID → frameID mapping.
	v, ok := mgr.serverReqIDs.Load("req-42")
	require.True(t, ok, "requestID should be stored in serverReqIDs")
	require.Equal(t, int64(99), v)

	// Subscriber should receive a PermissionRequest envelope.
	select {
	case env := <-ch:
		require.Equal(t, events.PermissionRequest, env.Event.Type)
		pr, ok := env.Event.Data.(events.PermissionRequestData)
		require.True(t, ok)
		require.Equal(t, "req-42", pr.ID)
		require.Equal(t, "Bash", pr.ToolName)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for envelope")
	}
}

func TestDispatchFrameClientResponse(t *testing.T) {
	t.Parallel()

	cfg := config.CodexCLIConfig{IdleDrainPeriod: time.Minute}
	mgr := NewCodexAppServerManager(slog.Default(), cfg)

	// Register a pending request.
	respCh := make(chan *JSONRPCResponse, 1)
	mgr.pending.Store(int64(7), respCh)
	defer mgr.pending.Delete(int64(7))

	// Simulate a client response (has ID, no Method).
	frame := []byte(`{"jsonrpc":"2.0","id":7,"result":{"thread":{"id":"thr-1"}}}`)
	mgr.dispatchFrame(frame)

	select {
	case resp := <-respCh:
		require.Equal(t, int64(7), resp.ID)
		require.NotNil(t, resp.Result)
		require.Nil(t, resp.Error)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for response")
	}
}

func TestDispatchFrameNotification(t *testing.T) {
	t.Parallel()

	cfg := config.CodexCLIConfig{IdleDrainPeriod: time.Minute}
	mgr := NewCodexAppServerManager(slog.Default(), cfg)

	ch := mgr.Subscribe("thr-1", "sess-1")

	// Notification: ID=0, Method present, no Error.
	frame := []byte(`{"jsonrpc":"2.0","method":"turn/failed","params":{"threadId":"thr-1","turn":{}}}`)
	mgr.dispatchFrame(frame)

	select {
	case env := <-ch:
		require.Equal(t, events.Error, env.Event.Type)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for notification envelope")
	}
}

func TestDispatchFrameErrorWithZeroID(t *testing.T) {
	t.Parallel()

	cfg := config.CodexCLIConfig{IdleDrainPeriod: time.Minute}
	mgr := NewCodexAppServerManager(slog.Default(), cfg)

	// Error frame with ID=0 should be dropped silently (no panic).
	frame := []byte(`{"jsonrpc":"2.0","id":0,"error":{"code":-32600,"message":"Invalid params"}}`)
	mgr.dispatchFrame(frame) // should not panic
}

func TestDispatchFrameNoMethodNoID(t *testing.T) {
	t.Parallel()

	cfg := config.CodexCLIConfig{IdleDrainPeriod: time.Minute}
	mgr := NewCodexAppServerManager(slog.Default(), cfg)

	// Bare frame with no method, no ID — should be dropped.
	frame := []byte(`{"jsonrpc":"2.0"}`)
	mgr.dispatchFrame(frame) // should not panic
}

func TestDispatchFrameInvalidJSON(t *testing.T) {
	t.Parallel()

	cfg := config.CodexCLIConfig{IdleDrainPeriod: time.Minute}
	mgr := NewCodexAppServerManager(slog.Default(), cfg)

	// Invalid JSON should be handled gracefully.
	mgr.dispatchFrame([]byte(`not json at all`)) // should not panic
}

func TestDispatchServerRequestWithoutThreadID(t *testing.T) {
	t.Parallel()

	cfg := config.CodexCLIConfig{IdleDrainPeriod: time.Minute}
	mgr := NewCodexAppServerManager(slog.Default(), cfg)

	// Server request without threadId should be dropped without storing requestID.
	frame := &JSONRPCFrame{
		JSONRPC: "2.0",
		ID:      50,
		Method:  "serverRequest/approval",
		Params:  json.RawMessage(`{"requestId":"req-no-thread"}`),
	}
	mgr.dispatchServerRequest(frame)

	_, ok := mgr.serverReqIDs.Load("req-no-thread")
	require.False(t, ok, "requestID should NOT be stored for threadless request")
}

func TestRespondServerRequest(t *testing.T) {
	t.Parallel()

	cfg := config.CodexCLIConfig{IdleDrainPeriod: time.Minute}
	mgr := NewCodexAppServerManager(slog.Default(), cfg)

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		// Pre-store a requestID → frameID mapping.
		mgr.serverReqIDs.Store("req-100", int64(42))

		// Capture what gets written to stdin.
		var buf strings.Builder
		mgr.stdin = struct {
			io.Writer
			io.Closer
		}{
			Writer: &buf,
			Closer: io.NopCloser(nil),
		}

		err := mgr.RespondServerRequest("req-100", map[string]string{"decision": "accept"})
		require.NoError(t, err)

		// Entry should be deleted.
		_, ok := mgr.serverReqIDs.Load("req-100")
		require.False(t, ok)

		// Written JSON should contain the frame ID and result.
		require.Contains(t, buf.String(), `"id":42`)
		require.Contains(t, buf.String(), "accept")
	})

	t.Run("unknown_request", func(t *testing.T) {
		t.Parallel()

		err := mgr.RespondServerRequest("nonexistent", map[string]string{"decision": "decline"})
		require.Error(t, err)
		require.Contains(t, err.Error(), "no pending server request")
	})
}

// ─── Approval Method Name Coverage ─────────────────────────────────────

func TestMapNotificationApprovalMethodNames(t *testing.T) {
	t.Parallel()

	methods := []string{
		"serverRequest/approval",
		"item/commandExecution/requestApproval",
		"item/fileChange/requestApproval",
	}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			t.Parallel()

			m := NewMapper("session-1")
			params := json.RawMessage(fmt.Sprintf(
				`{"requestId":"r1","toolName":"%s","reason":"test"}`, method))
			envs := m.MapNotification(method, params)
			require.Len(t, envs, 1, "method %s should produce 1 envelope", method)
			require.Equal(t, events.PermissionRequest, envs[0].Event.Type, "method %s", method)
			pr, ok := envs[0].Event.Data.(events.PermissionRequestData)
			require.True(t, ok)
			require.Equal(t, "r1", pr.ID)
		})
	}
}

func TestMapNotificationElicitation(t *testing.T) {
	t.Parallel()

	m := NewMapper("session-1")
	params := json.RawMessage(`{"requestId":"el_1","mcpServerName":"fs","message":"Allow file access?","mode":"confirm"}`)
	envs := m.MapNotification("mcpServer/elicitation/request", params)
	require.Len(t, envs, 1)
	require.Equal(t, events.ElicitationRequest, envs[0].Event.Type)
	el, ok := envs[0].Event.Data.(events.ElicitationRequestData)
	require.True(t, ok)
	require.Equal(t, "el_1", el.ID)
	require.Equal(t, "fs", el.MCPServerName)
	require.Equal(t, "Allow file access?", el.Message)
	require.Equal(t, "confirm", el.Mode)
}

func TestMapNotificationElicitationBadJSON(t *testing.T) {
	t.Parallel()

	m := NewMapper("session-1")
	envs := m.MapNotification("mcpServer/elicitation/request", json.RawMessage(`{bad`))
	require.Nil(t, envs)
}

// ─── Issue #575: Reset kills session + Zombie process fix tests ──────────

// testConfigWithDefaults sets a known-good config for integration tests and
// returns a restore function that should be deferred.
func testConfigWithDefaults() func() {
	prev := GetConfig()
	InitConfig(config.CodexCLIConfig{
		Sandbox:         "workspace-write",
		ApprovalMode:    "never",
		Personality:     "friendly",
		IdleDrainPeriod: time.Minute,
		StartupTimeout:  10 * time.Second,
		CallTimeout:     10 * time.Second,
	})
	return func() { InitConfig(prev) }
}

// ---------- Unit tests (no codex binary required) ----------

func TestResetContextClearsStateAndResetsOnce(t *testing.T) {
	// Verify lightweight ResetContext closes old conn and unsubscribes old thread
	// without touching closed/doneCh/releaseOnce (no Terminate+Start cycle).
	// We test with origSession.SessionID="" so thread/start is skipped.
	mgr := NewCodexAppServerManager(slog.Default(), config.CodexCLIConfig{
		IdleDrainPeriod: time.Minute,
	})

	recvCh := make(chan *events.Envelope, 1)
	w := &AppServerWorker{
		BaseWorker: base.NewBaseWorker(slog.Default(), nil),
		manager:    mgr,
		doneCh:     make(chan struct{}),
		conn:       &appConn{recvCh: recvCh},
	}
	w.origSession = worker.SessionInfo{}

	_, err := w.ResetContext(context.Background())
	require.NoError(t, err)

	// Old recvCh should be closed by appConn.Close().
	_, ok := <-recvCh
	require.False(t, ok, "old recvCh should be closed")
}

func TestResetContextRestartsFromSavedSession(t *testing.T) {
	// Verify ResetContext performs lightweight thread swap: closes old conn,
	// resets lifecycle state, then attempts thread/start on same manager.
	// No real codex process — provide fake stdin so writeFrame won't panic,
	// thread/start will fail with write/timeout error.
	mgr := NewCodexAppServerManager(slog.Default(), config.CodexCLIConfig{
		IdleDrainPeriod: time.Minute,
		CallTimeout:     20 * time.Millisecond,
	})
	r, w := io.Pipe()
	mgr.stdin = w
	mgr.mu.Lock()
	mgr.refs = 1
	mgr.state = stateRunning
	mgr.mu.Unlock()
	// Drain pipe in background so Notify/Call writes don't block.
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = io.Copy(io.Discard, r)
	}()
	t.Cleanup(func() { _ = w.Close(); <-done })

	oldRecvCh := make(chan *events.Envelope, 1)
	wk := &AppServerWorker{
		BaseWorker:  base.NewBaseWorker(slog.Default(), nil),
		manager:     mgr,
		origSession: worker.SessionInfo{SessionID: "sess-575", UserID: "u1", ProjectDir: "/tmp"},
		threadID:    "old-thread",
		recvCh:      make(chan *events.Envelope, 1),
		doneCh:      make(chan struct{}),
		conn:        &appConn{recvCh: oldRecvCh},
	}

	_, err := wk.ResetContext(context.Background())

	// Old conn should be closed.
	_, ok := <-oldRecvCh
	require.False(t, ok, "old recvCh should be closed")

	// thread/start fails because no real process responds.
	require.Error(t, err)
	require.Contains(t, err.Error(), "codexcli: reset thread/start:")
}

func TestKillCallsKillIfIdle(t *testing.T) {
	// Verify Kill() calls manager.KillIfIdle() after release().
	mgr := NewCodexAppServerManager(slog.Default(), config.CodexCLIConfig{
		IdleDrainPeriod: time.Minute,
	})

	// Simulate a running process with refs=1 and stateRunning.
	// Use pgid=0 so KillIfIdle's shouldKill guard is false (pgid must be >0),
	// which avoids calling ForceKill on a real PGID in CI while still
	// testing the stateRunning code path.
	mgr.mu.Lock()
	mgr.state = stateRunning
	mgr.refs = 1
	mgr.pgid = 0
	mgr.mu.Unlock()

	w := &AppServerWorker{
		BaseWorker: base.NewBaseWorker(slog.Default(), nil),
		manager:    mgr,
		doneCh:     make(chan struct{}),
	}
	// threadID left empty so release() won't call Notify (no real process).

	err := w.Kill()
	require.NoError(t, err)

	// release() was called (closed=true, doneCh closed).
	w.mu.Lock()
	require.True(t, w.closed, "closed should be true after Kill")
	w.mu.Unlock()

	// KillIfIdle was called: idleTimer should be nil (stopped).
	mgr.mu.Lock()
	timer := mgr.idleTimer
	mgr.mu.Unlock()
	require.Nil(t, timer, "idle timer should be stopped by KillIfIdle")
}

func TestKillIfIdleKillsWhenIdle(t *testing.T) {
	// Verify KillIfIdle actually triggers the kill path when all conditions
	// are met: refs==0, stateRunning, pgid>0. Use pgid=-1 so ForceKill
	// sends SIGKILL to PGID -1 which returns ESRCH (no such process) —
	// harmless but confirms the code path is exercised.
	mgr := NewCodexAppServerManager(slog.Default(), config.CodexCLIConfig{
		IdleDrainPeriod: time.Minute,
	})
	mgr.mu.Lock()
	mgr.state = stateRunning
	mgr.refs = 0
	mgr.pgid = -1 // ForceKill(-(-1)) = SIGKILL to PGID 1 → ESRCH, harmless
	mgr.mu.Unlock()

	// Should not panic or deadlock.
	mgr.KillIfIdle()

	mgr.mu.Lock()
	require.Nil(t, mgr.idleTimer, "idle timer should be stopped")
	mgr.mu.Unlock()
}

func TestKillIfIdleNoOpWhenRefsPositive(t *testing.T) {
	mgr := NewCodexAppServerManager(slog.Default(), config.CodexCLIConfig{
		IdleDrainPeriod: 30 * time.Minute,
	})

	// Simulate running with refs=1.
	mgr.mu.Lock()
	mgr.state = stateRunning
	mgr.refs = 1
	mgr.pgid = 0 // pgid=0: shouldKill false, no real ForceKill
	mgr.mu.Unlock()

	// KillIfIdle should be no-op because refs > 0.
	mgr.KillIfIdle()

	mgr.mu.Lock()
	require.Equal(t, stateRunning, mgr.state, "process should stay running when refs > 0")
	mgr.mu.Unlock()
}

// ---------- Integration tests (require codex binary) ----------

func TestIntegrationStartSavesSessionAndResetRestarts(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping: requires codex binary")
	}
	defer testConfigWithDefaults()()

	w := newTestAppServerWorker(t)
	session := worker.SessionInfo{
		SessionID:  "sess-integ-575",
		UserID:     "user-1",
		ProjectDir: t.TempDir(),
	}
	err := w.Start(context.Background(), session)
	if err != nil {
		t.Skipf("codex binary not available: %v", err)
	}
	t.Cleanup(func() { _ = w.Terminate(context.Background()) })

	w.mu.Lock()
	saved := w.origSession
	w.mu.Unlock()
	require.Equal(t, "sess-integ-575", saved.SessionID)
	require.Equal(t, "user-1", saved.UserID)

	// Test ResetContext restarts with a new thread.
	w.mu.Lock()
	firstThreadID := w.threadID
	w.mu.Unlock()
	require.NotEmpty(t, firstThreadID)

	_, err = w.ResetContext(context.Background())
	require.NoError(t, err)

	w.mu.Lock()
	secondThreadID := w.threadID
	w.mu.Unlock()
	require.NotEmpty(t, secondThreadID)
	require.NotEqual(t, firstThreadID, secondThreadID, "threadID should change after reset")
}

// ─── appConn.Send Tests ──────────────────────────────────────────────────

func TestAppConnSendReturnsErrNotImplemented(t *testing.T) {
	t.Parallel()

	cfg := config.CodexCLIConfig{IdleDrainPeriod: time.Minute}
	mgr := NewCodexAppServerManager(slog.Default(), cfg)
	ch := make(chan *events.Envelope, 1)
	conn := &appConn{
		userID:    "user-1",
		sessionID: "sess-1",
		recvCh:    ch,
		manager:   mgr,
	}

	env := events.NewEnvelope("id-1", "sess-1", 1, events.MessageStart, nil)
	err := conn.Send(context.Background(), env)
	require.ErrorIs(t, err, worker.ErrNotImplemented)
}

// ─── Compact Tests ────────────────────────────────────────────────────────

func TestCompactNoThread(t *testing.T) {
	t.Parallel()

	w := newTestAppServerWorker(t)
	err := w.Compact(context.Background(), nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no active thread")
}

func TestCompactDelegatesToManager(t *testing.T) {
	t.Parallel()

	// Set up a manager with a fake stdin pipe so Call won't panic on write,
	// but will timeout since there's no real process responding.
	mgr := NewCodexAppServerManager(slog.Default(), config.CodexCLIConfig{
		IdleDrainPeriod: time.Minute,
		CallTimeout:     20 * time.Millisecond,
	})
	r, w := io.Pipe()
	mgr.stdin = w
	mgr.mu.Lock()
	mgr.refs = 1
	mgr.state = stateRunning
	mgr.mu.Unlock()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = io.Copy(io.Discard, r)
	}()
	t.Cleanup(func() { _ = w.Close(); <-done })

	wk := &AppServerWorker{
		BaseWorker: base.NewBaseWorker(slog.Default(), nil),
		manager:    mgr,
		threadID:   "thr-compact",
	}

	err := wk.Compact(context.Background(), nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "codexcli: compact:")
}

// ─── Clear Tests ──────────────────────────────────────────────────────────

func TestClearNoThread(t *testing.T) {
	t.Parallel()

	w := newTestAppServerWorker(t)
	err := w.Clear(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "no active thread")
}

func TestClearDelegatesToResetContext(t *testing.T) {
	t.Parallel()

	// Clear calls ResetContext internally.
	// ResetContext → cleanupOldThread → Notify(thread/unsubscribe) needs stdin.
	mgr := NewCodexAppServerManager(slog.Default(), config.CodexCLIConfig{
		IdleDrainPeriod: time.Minute,
	})
	r, w := io.Pipe()
	mgr.stdin = w
	mgr.mu.Lock()
	mgr.refs = 1
	mgr.state = stateRunning
	mgr.mu.Unlock()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = io.Copy(io.Discard, r)
	}()
	t.Cleanup(func() { _ = w.Close(); <-done })

	recvCh := make(chan *events.Envelope, 1)
	wk := &AppServerWorker{
		BaseWorker: base.NewBaseWorker(slog.Default(), nil),
		manager:    mgr,
		threadID:   "thr-clear",
		doneCh:     make(chan struct{}),
		conn:       &appConn{recvCh: recvCh},
	}

	err := wk.Clear(context.Background())
	require.NoError(t, err)

	_, ok := <-recvCh
	require.False(t, ok)
}

func TestClearWithActiveTurn(t *testing.T) {
	t.Parallel()

	// Clear with a turnID should attempt InterruptTurn before ResetContext.
	// InterruptTurn's Notify needs a stdin writer. Provide a fake pipe.
	mgr := NewCodexAppServerManager(slog.Default(), config.CodexCLIConfig{
		IdleDrainPeriod: time.Minute,
	})
	r, w := io.Pipe()
	mgr.stdin = w
	mgr.mu.Lock()
	mgr.refs = 1
	mgr.state = stateRunning
	mgr.mu.Unlock()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = io.Copy(io.Discard, r)
	}()
	t.Cleanup(func() { _ = w.Close(); <-done })

	recvCh := make(chan *events.Envelope, 1)
	wk := &AppServerWorker{
		BaseWorker: base.NewBaseWorker(slog.Default(), nil),
		manager:    mgr,
		threadID:   "thr-clear2",
		turnID:     "turn-active",
		doneCh:     make(chan struct{}),
		conn:       &appConn{recvCh: recvCh},
	}

	err := wk.Clear(context.Background())
	require.NoError(t, err)
}

// ─── Rewind Tests ─────────────────────────────────────────────────────────

func TestRewindNoThread(t *testing.T) {
	t.Parallel()

	w := newTestAppServerWorker(t)
	err := w.Rewind(context.Background(), "1")
	require.Error(t, err)
	require.Contains(t, err.Error(), "no active thread")
}

func TestRewindDefaultOne(t *testing.T) {
	t.Parallel()

	mgr := NewCodexAppServerManager(slog.Default(), config.CodexCLIConfig{
		IdleDrainPeriod: time.Minute,
		CallTimeout:     20 * time.Millisecond,
	})
	r, w := io.Pipe()
	mgr.stdin = w
	mgr.mu.Lock()
	mgr.refs = 1
	mgr.state = stateRunning
	mgr.mu.Unlock()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = io.Copy(io.Discard, r)
	}()
	t.Cleanup(func() { _ = w.Close(); <-done })

	wk := &AppServerWorker{
		BaseWorker: base.NewBaseWorker(slog.Default(), nil),
		manager:    mgr,
		threadID:   "thr-rewind",
	}

	// Empty targetID defaults to 1 turn.
	err := wk.Rewind(context.Background(), "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "codexcli: rewind:")
}

func TestRewindExplicitCount(t *testing.T) {
	t.Parallel()

	mgr := NewCodexAppServerManager(slog.Default(), config.CodexCLIConfig{
		IdleDrainPeriod: time.Minute,
		CallTimeout:     20 * time.Millisecond,
	})
	r, w := io.Pipe()
	mgr.stdin = w
	mgr.mu.Lock()
	mgr.refs = 1
	mgr.state = stateRunning
	mgr.mu.Unlock()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = io.Copy(io.Discard, r)
	}()
	t.Cleanup(func() { _ = w.Close(); <-done })

	wk := &AppServerWorker{
		BaseWorker: base.NewBaseWorker(slog.Default(), nil),
		manager:    mgr,
		threadID:   "thr-rewind2",
	}

	// Valid count.
	err := wk.Rewind(context.Background(), "3")
	require.Error(t, err)
	require.Contains(t, err.Error(), "codexcli: rewind:")
}

func TestRewindInvalidCount(t *testing.T) {
	t.Parallel()

	mgr := NewCodexAppServerManager(slog.Default(), config.CodexCLIConfig{
		IdleDrainPeriod: time.Minute,
		CallTimeout:     20 * time.Millisecond,
	})
	r, w := io.Pipe()
	mgr.stdin = w
	mgr.mu.Lock()
	mgr.refs = 1
	mgr.state = stateRunning
	mgr.mu.Unlock()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = io.Copy(io.Discard, r)
	}()
	t.Cleanup(func() { _ = w.Close(); <-done })

	wk := &AppServerWorker{
		BaseWorker: base.NewBaseWorker(slog.Default(), nil),
		manager:    mgr,
		threadID:   "thr-rewind3",
	}

	// Invalid count string falls back to default 1.
	err := wk.Rewind(context.Background(), "abc")
	require.Error(t, err)
	require.Contains(t, err.Error(), "codexcli: rewind:")
}

// ─── Input Expanded Tests ─────────────────────────────────────────────────

func TestInputPermissionResponseHandled(t *testing.T) {
	t.Parallel()

	w := newTestAppServerWorker(t)
	err := w.Input(context.Background(), "hello", map[string]any{
		"permission_response": map[string]any{
			"request_id": "req-1",
			"allowed":    true,
		},
	})
	// HandlePermissionResponse returns error (no pending server request),
	// but Input returns the error from DispatchMetadata.
	require.Error(t, err)
	require.Contains(t, err.Error(), "no pending server request")
}

func TestInputQuestionResponseHandled(t *testing.T) {
	t.Parallel()

	w := newTestAppServerWorker(t)
	err := w.Input(context.Background(), "hello", map[string]any{
		"question_response": map[string]any{
			"id": "req-2",
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "no pending server request")
}

func TestInputElicitationResponseHandled(t *testing.T) {
	t.Parallel()

	w := newTestAppServerWorker(t)
	err := w.Input(context.Background(), "hello", map[string]any{
		"elicitation_response": map[string]any{
			"id":     "req-3",
			"action": "accept",
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "no pending server request")
}

func TestInputMetadataNilPassesThrough(t *testing.T) {
	t.Parallel()

	w := newTestAppServerWorker(t)
	// nil metadata → DispatchMetadata returns (false, nil) → falls through
	// to threadID check → "not started" error.
	err := w.Input(context.Background(), "hello", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not started")
}

func TestInputCallsTurnStart(t *testing.T) {
	t.Parallel()

	// Set up a manager with fake stdin so Call can write but will timeout.
	mgr := NewCodexAppServerManager(slog.Default(), config.CodexCLIConfig{
		IdleDrainPeriod: time.Minute,
		CallTimeout:     20 * time.Millisecond,
	})
	r, w := io.Pipe()
	mgr.stdin = w
	mgr.mu.Lock()
	mgr.refs = 1
	mgr.state = stateRunning
	mgr.mu.Unlock()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = io.Copy(io.Discard, r)
	}()
	t.Cleanup(func() { _ = w.Close(); <-done })

	wk := &AppServerWorker{
		BaseWorker: base.NewBaseWorker(slog.Default(), nil),
		manager:    mgr,
		threadID:   "thr-input",
	}

	err := wk.Input(context.Background(), "hello world", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "codexcli: turn/start:")
}

// ─── Wait Expanded Tests ──────────────────────────────────────────────────

func TestWaitDoneChPath(t *testing.T) {
	t.Parallel()

	doneCh := make(chan struct{})
	w := &AppServerWorker{
		BaseWorker: base.NewBaseWorker(slog.Default(), nil),
		doneCh:     doneCh,
	}

	// Close doneCh to unblock Wait.
	go func() { close(doneCh) }()

	code, err := w.Wait()
	require.NoError(t, err)
	require.Equal(t, 0, code)
}

func TestWaitCrashSubPath(t *testing.T) {
	t.Parallel()

	crashCh := make(chan struct{})
	w := &AppServerWorker{
		BaseWorker: base.NewBaseWorker(slog.Default(), nil),
		crashSub:   crashCh,
		doneCh:     make(chan struct{}),
	}

	// Close crashCh to simulate process crash.
	go func() { close(crashCh) }()

	code, err := w.Wait()
	require.NoError(t, err)
	require.Equal(t, 1, code, "crash should return exit code 1")
}

func TestWaitNilCrashSubReturnsImmediately(t *testing.T) {
	t.Parallel()

	w := &AppServerWorker{
		BaseWorker: base.NewBaseWorker(slog.Default(), nil),
		crashSub:   nil,
	}

	code, err := w.Wait()
	require.NoError(t, err)
	require.Equal(t, 0, code)
}

// ─── SendControlRequest Tests ─────────────────────────────────────────────

func TestWaitAfterReleaseReturnsImmediately(t *testing.T) {
	t.Parallel()

	// Regression test for #691: Wait() must not block after release() nil'd doneCh.
	w := &AppServerWorker{
		BaseWorker: base.NewBaseWorker(slog.Default(), nil),
		doneCh:     make(chan struct{}),
		crashSub:   make(chan struct{}),
	}

	// Simulate the zombie GC path: Terminate -> shutdown -> release().
	w.release()

	// Wait() must return immediately, not block on nil channel.
	code, err := w.Wait()
	require.NoError(t, err)
	require.Equal(t, 0, code)
}

func TestWaitBlocksUntilRelease(t *testing.T) {
	t.Parallel()

	w := &AppServerWorker{
		BaseWorker: base.NewBaseWorker(slog.Default(), nil),
		doneCh:     make(chan struct{}),
		crashSub:   make(chan struct{}),
	}

	waitDone := make(chan int, 1)
	go func() {
		code, _ := w.Wait()
		waitDone <- code
	}()

	// Wait should be blocked.
	select {
	case <-waitDone:
		t.Fatal("Wait should block before release")
	case <-time.After(50 * time.Millisecond):
	}

	// Release unblocks Wait.
	w.release()

	select {
	case code := <-waitDone:
		require.Equal(t, 0, code)
	case <-time.After(time.Second):
		t.Fatal("Wait should unblock after release")
	}
}

func TestTerminateThenKillNoPanic(t *testing.T) {
	t.Parallel()

	// Verify double release (Terminate + Kill) does not panic from double-close.
	w := &AppServerWorker{
		BaseWorker: base.NewBaseWorker(slog.Default(), nil),
		doneCh:     make(chan struct{}),
		crashSub:   make(chan struct{}),
	}

	require.NotPanics(t, func() {
		_ = w.Terminate(context.Background())
		_ = w.Kill()
	})

	// Wait still works after double release.
	code, err := w.Wait()
	require.NoError(t, err)
	require.Equal(t, 0, code)
}

func TestSendControlRequestNotStarted(t *testing.T) {
	t.Parallel()

	w := newTestAppServerWorker(t)
	_, err := w.SendControlRequest(context.Background(), "set_model", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not started")
}

func TestSendControlRequestSetModel(t *testing.T) {
	t.Parallel()

	w := newTestAppServerWorker(t)
	w.commands = NewServerCommander(w.manager, "thr-1")

	_, err := w.SendControlRequest(context.Background(), "set_model", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "set_model not supported")
}

// ─── Conn Tests ───────────────────────────────────────────────────────────

func TestConnAfterStartReturnsAppConn(t *testing.T) {
	t.Parallel()

	recvCh := make(chan *events.Envelope, 1)
	w := &AppServerWorker{
		BaseWorker: base.NewBaseWorker(slog.Default(), nil),
		manager:    NewCodexAppServerManager(slog.Default(), config.CodexCLIConfig{IdleDrainPeriod: time.Minute}),
		conn: &appConn{
			userID:    "u1",
			sessionID: "s1",
			recvCh:    recvCh,
		},
	}

	c := w.Conn()
	require.NotNil(t, c)
	require.Equal(t, "u1", c.UserID())
	require.Equal(t, "s1", c.SessionID())
}

// ─── sandboxFromSession Tests ─────────────────────────────────────────────

func TestSandboxFromSession(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		session  worker.SessionInfo
		default_ string
		want     string
	}{
		{name: "session override", session: worker.SessionInfo{Sandbox: "docker"}, default_: "workspace-write", want: "docker"},
		{name: "fallback to default", session: worker.SessionInfo{}, default_: "workspace-write", want: "workspace-write"},
		{name: "empty both", session: worker.SessionInfo{}, default_: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := sandboxFromSession(tt.session, tt.default_)
			require.Equal(t, tt.want, got)
		})
	}
}

// ─── buildThreadStartParams Tests ─────────────────────────────────────────

func TestBuildThreadStartParams(t *testing.T) {
	t.Parallel()

	t.Run("minimal config", func(t *testing.T) {
		t.Parallel()
		params := buildThreadStartParams(worker.SessionInfo{ProjectDir: "/tmp"}, Config{})
		require.Equal(t, "/tmp", params["cwd"])
		_, hasModel := params["model"]
		require.False(t, hasModel)
		_, hasEphemeral := params["ephemeral"]
		require.False(t, hasEphemeral)
	})

	t.Run("full config", func(t *testing.T) {
		t.Parallel()
		params := buildThreadStartParams(
			worker.SessionInfo{
				ProjectDir:      "/home/user/project",
				Sandbox:         "docker",
				SkipPermissions: true,
				Images:          []string{"img1.png"},
				JSONSchema:      `{"type":"object"}`,
				AllowedDirs:     []string{"/data"},
			},
			Config{
				Model:            "o3",
				Sandbox:          "workspace-write",
				Ephemeral:        true,
				ApprovalMode:     "on-request",
				Personality:      "friendly",
				Color:            true,
				StrictConfig:     true,
				SkipGitRepoCheck: true,
				IgnoreUserConfig: true,
				IgnoreRules:      true,
				LocalProvider:    true,
				BypassHookTrust:  true,
				OutputFile:       "/tmp/out.md",
				ConfigProfile:    "dev",
			},
		)
		require.Equal(t, "o3", params["model"])
		require.Equal(t, "docker", params["sandbox"]) // session override
		require.Equal(t, true, params["ephemeral"])
		require.Equal(t, "never", params["approvalPolicy"]) // SkipPermissions → "never"
		require.Equal(t, "friendly", params["personality"])
		require.Equal(t, true, params["color"])
		require.Equal(t, true, params["strictConfig"])
		require.Equal(t, true, params["skipGitRepoCheck"])
		require.Equal(t, true, params["ignoreUserConfig"])
		require.Equal(t, true, params["ignoreRules"])
		require.Equal(t, true, params["localProvider"])
		require.Equal(t, true, params["bypassHookTrust"])
		require.Equal(t, "/tmp/out.md", params["outputFile"])
		require.Equal(t, "dev", params["profile"])
		require.NotNil(t, params["images"])
		require.Equal(t, `{"type":"object"}`, params["outputSchema"])
		require.NotNil(t, params["additionalDirectories"])
	})

	t.Run("no skip permissions uses config approval", func(t *testing.T) {
		t.Parallel()
		params := buildThreadStartParams(
			worker.SessionInfo{ProjectDir: "/tmp"},
			Config{ApprovalMode: "on-failure"},
		)
		require.Equal(t, "on-failure", params["approvalPolicy"])
	})
}

// ─── resolveConfig Tests ──────────────────────────────────────────────────

func TestResolveConfig(t *testing.T) {
	t.Parallel()

	prev := GetConfig()
	defer InitConfig(prev)

	InitConfig(config.CodexCLIConfig{
		Command:      "/usr/local/bin/codex",
		Model:        "o3",
		Sandbox:      "docker",
		ApprovalMode: "never",
		Personality:  "friendly",
		Ephemeral:    true,
		Color:        true,
	})

	cfg := resolveConfig()
	require.Equal(t, "/usr/local/bin/codex", cfg.Command)
	require.Equal(t, "o3", cfg.Model)
	require.Equal(t, "docker", cfg.Sandbox)
	require.Equal(t, "never", cfg.ApprovalMode)
	require.Equal(t, "friendly", cfg.Personality)
	require.True(t, cfg.Ephemeral)
	require.True(t, cfg.Color)
}

// ─── Singleton Tests ─────────────────────────────────────────────────────

func TestSingletonLifecycle(t *testing.T) {
	InitSingleton(slog.Default(), config.CodexCLIConfig{
		IdleDrainPeriod: time.Minute,
		Command:         "codex",
	})
	t.Cleanup(func() { ShutdownSingleton(context.Background()) })

	s := GetSingleton()
	require.NotNil(t, s)
	require.False(t, s.IsRunning())
}

func TestShutdownSingletonNil(t *testing.T) {
	// Shutdown with nil singleton should not panic.
	ShutdownSingleton(context.Background())
}

// ─── ServerCommander Tests ───────────────────────────────────────────────

func TestServerCommanderCompact(t *testing.T) {
	t.Parallel()

	mgr := NewCodexAppServerManager(slog.Default(), config.CodexCLIConfig{
		IdleDrainPeriod: time.Minute,
		CallTimeout:     20 * time.Millisecond,
	})
	r, w := io.Pipe()
	mgr.stdin = w
	mgr.mu.Lock()
	mgr.refs = 1
	mgr.state = stateRunning
	mgr.mu.Unlock()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = io.Copy(io.Discard, r)
	}()
	t.Cleanup(func() { _ = w.Close(); <-done })

	sc := NewServerCommander(mgr, "thr-sc")
	err := sc.Compact(context.Background(), nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "codexcli: compact:")
}

func TestServerCommanderUnknownSubtype(t *testing.T) {
	t.Parallel()

	mgr := NewCodexAppServerManager(slog.Default(), config.CodexCLIConfig{
		IdleDrainPeriod: time.Minute,
	})
	sc := NewServerCommander(mgr, "thr-sc")

	_, err := sc.SendControlRequest(context.Background(), "unknown_subtype", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown control subtype")
}

func TestServerCommanderMCPOAuthMissingName(t *testing.T) {
	t.Parallel()

	mgr := NewCodexAppServerManager(slog.Default(), config.CodexCLIConfig{
		IdleDrainPeriod: time.Minute,
	})
	sc := NewServerCommander(mgr, "thr-sc")

	_, err := sc.SendControlRequest(context.Background(), "mcp_oauth", map[string]any{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing server_name")
}

func TestIntegrationKillImmediatelyTerminatesIdleProcess(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping: requires codex binary")
	}
	defer testConfigWithDefaults()()

	cfg := config.CodexCLIConfig{
		IdleDrainPeriod: 10 * time.Second,
		StartupTimeout:  10 * time.Second,
		CallTimeout:     5 * time.Second,
	}
	mgr := NewCodexAppServerManager(slog.Default(), cfg)

	_, err := mgr.Acquire(context.Background())
	if err != nil {
		t.Skipf("codex binary not available: %v", err)
	}
	require.True(t, mgr.IsRunning())

	mgr.Release()
	mgr.KillIfIdle()

	require.Eventually(t, func() bool {
		return !mgr.IsRunning()
	}, 5*time.Second, 100*time.Millisecond, "process should be killed immediately, not after idle drain")
}
