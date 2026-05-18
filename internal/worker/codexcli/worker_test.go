package codexcli

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/internal/worker/base"
	"github.com/hrygo/hotplex/pkg/events"
	"github.com/stretchr/testify/require"
)

func TestParserParseLine(t *testing.T) {
	t.Parallel()

	p := NewParser()

	t.Run("agent_message", func(t *testing.T) {
		t.Parallel()
		line := `{"type":"item.completed","item":{"id":"item_1","type":"agent_message","text":"Hello world"}}`
		event, err := p.ParseLine(line)
		require.NoError(t, err)
		require.Equal(t, EventItemCompleted, event.Type)
		require.NotNil(t, event.Item)
		require.Equal(t, "agent_message", event.Item.Type)
		require.Equal(t, "Hello world", event.Item.Text)
	})

	t.Run("thread_started", func(t *testing.T) {
		t.Parallel()
		line := `{"type":"thread.started","thread_id":"thread-123"}`
		event, err := p.ParseLine(line)
		require.NoError(t, err)
		require.Equal(t, EventThreadStarted, event.Type)
		require.Equal(t, "thread-123", event.ThreadID)
	})

	t.Run("turn_completed_with_usage", func(t *testing.T) {
		t.Parallel()
		line := `{"type":"turn.completed","usage":{"input_tokens":100,"output_tokens":50}}`
		event, err := p.ParseLine(line)
		require.NoError(t, err)
		require.Equal(t, EventTurnCompleted, event.Type)
		require.NotNil(t, event.Usage)
		require.Equal(t, 100, event.Usage.InputTokens)
		require.Equal(t, 50, event.Usage.OutputTokens)
	})

	t.Run("error_event", func(t *testing.T) {
		t.Parallel()
		line := `{"type":"error","message":"something went wrong"}`
		event, err := p.ParseLine(line)
		require.NoError(t, err)
		require.Equal(t, EventError, event.Type)
		require.Equal(t, "something went wrong", event.Message)
	})

	t.Run("invalid_json", func(t *testing.T) {
		t.Parallel()
		_, err := p.ParseLine("not json")
		require.Error(t, err)
	})

	t.Run("missing_type", func(t *testing.T) {
		t.Parallel()
		_, err := p.ParseLine(`{"item":{"id":"1","type":"agent_message"}}`)
		require.Error(t, err)
	})
}

func TestMapperMap(t *testing.T) {
	t.Parallel()

	m := NewMapper("session-1", func() int64 { return 0 })

	t.Run("agent_message_to_delta", func(t *testing.T) {
		t.Parallel()
		event := &CodexEvent{
			Type: EventItemCompleted,
			Item: &CodexItem{
				ID:   "item_1",
				Type: "agent_message",
				Text: "Hello, I found the bug",
			},
		}
		envs := m.Map(event)
		require.Len(t, envs, 1)
		require.Equal(t, events.MessageDelta, envs[0].Event.Type)
		md, ok := envs[0].Event.Data.(events.MessageDeltaData)
		require.True(t, ok)
		require.Equal(t, "Hello, I found the bug", md.Content)
	})

	t.Run("reasoning_to_reasoning", func(t *testing.T) {
		t.Parallel()
		event := &CodexEvent{
			Type: EventItemCompleted,
			Item: &CodexItem{
				ID:          "item_1",
				Type:        "reasoning",
				SummaryText: []string{"Step 1: analyze", "Step 2: fix"},
			},
		}
		envs := m.Map(event)
		require.Len(t, envs, 1)
		require.Equal(t, events.Reasoning, envs[0].Event.Type)
		rd, ok := envs[0].Event.Data.(events.ReasoningData)
		require.True(t, ok)
		require.Equal(t, "Step 1: analyze\nStep 2: fix", rd.Content)
	})

	t.Run("command_execution_started_to_toolcall", func(t *testing.T) {
		t.Parallel()
		event := &CodexEvent{
			Type: EventItemStarted,
			Item: &CodexItem{
				ID:      "item_1",
				Type:    "command_execution",
				Command: "ls -la",
				CWD:     "/home/user",
			},
		}
		envs := m.Map(event)
		require.Len(t, envs, 1)
		require.Equal(t, events.ToolCall, envs[0].Event.Type)
		tc, ok := envs[0].Event.Data.(events.ToolCallData)
		require.True(t, ok)
		require.Equal(t, "shell", tc.Name)
		require.Equal(t, "ls -la", tc.Input["command"])
	})

	t.Run("command_execution_completed_to_toolresult", func(t *testing.T) {
		t.Parallel()
		event := &CodexEvent{
			Type: EventItemCompleted,
			Item: &CodexItem{
				ID:       "item_1",
				Type:     "command_execution",
				Stdout:   "file1\nfile2",
				ExitCode: 0,
			},
		}
		envs := m.Map(event)
		require.Len(t, envs, 1)
		require.Equal(t, events.ToolResult, envs[0].Event.Type)
		tr, ok := envs[0].Event.Data.(events.ToolResultData)
		require.True(t, ok)
		require.Equal(t, "file1\nfile2", tr.Output)
	})

	t.Run("turn_completed_to_done", func(t *testing.T) {
		t.Parallel()
		event := &CodexEvent{
			Type: EventTurnCompleted,
			Usage: &CodexUsage{
				InputTokens:  100,
				OutputTokens: 50,
			},
		}
		envs := m.Map(event)
		require.Len(t, envs, 1)
		require.Equal(t, events.Done, envs[0].Event.Type)
		dd, ok := envs[0].Event.Data.(events.DoneData)
		require.True(t, ok)
		require.True(t, dd.Success)
		require.Equal(t, 100, dd.Stats["input_tokens"])
		require.Equal(t, 50, dd.Stats["output_tokens"])
	})

	t.Run("turn_failed_to_error_and_done", func(t *testing.T) {
		t.Parallel()
		event := &CodexEvent{Type: EventTurnFailed}
		envs := m.Map(event)
		require.Len(t, envs, 2)
		require.Equal(t, events.Error, envs[0].Event.Type)
		require.Equal(t, events.Done, envs[1].Event.Type)
		dd, ok := envs[1].Event.Data.(events.DoneData)
		require.True(t, ok)
		require.False(t, dd.Success)
	})

	t.Run("error_event_to_error_and_done", func(t *testing.T) {
		t.Parallel()
		event := &CodexEvent{
			Type:    EventError,
			Message: "API rate limit exceeded",
		}
		envs := m.Map(event)
		require.Len(t, envs, 2)
		require.Equal(t, events.Error, envs[0].Event.Type)
		ed, ok := envs[0].Event.Data.(events.ErrorData)
		require.True(t, ok)
		require.Equal(t, "API rate limit exceeded", ed.Message)
	})

	t.Run("file_change_started_to_toolcall", func(t *testing.T) {
		t.Parallel()
		event := &CodexEvent{
			Type: EventItemStarted,
			Item: &CodexItem{
				ID:   "item_1",
				Type: "file_change",
				Changes: map[string]CodexFileChange{
					"main.go": {FilePath: "main.go"},
				},
			},
		}
		envs := m.Map(event)
		require.Len(t, envs, 1)
		require.Equal(t, events.ToolCall, envs[0].Event.Type)
		tc, ok := envs[0].Event.Data.(events.ToolCallData)
		require.True(t, ok)
		require.Equal(t, "file_edit", tc.Name)
	})

	t.Run("mcp_tool_call_started_to_toolcall", func(t *testing.T) {
		t.Parallel()
		args := json.RawMessage(`{"query":"test"}`)
		event := &CodexEvent{
			Type: EventItemStarted,
			Item: &CodexItem{
				ID:        "item_1",
				Type:      "mcp_tool_call",
				Server:    "github",
				Tool:      "search",
				Arguments: args,
			},
		}
		envs := m.Map(event)
		require.Len(t, envs, 1)
		require.Equal(t, events.ToolCall, envs[0].Event.Type)
		tc, ok := envs[0].Event.Data.(events.ToolCallData)
		require.True(t, ok)
		require.Equal(t, "mcp:search", tc.Name)
	})

	t.Run("nil_item_returns_nil", func(t *testing.T) {
		t.Parallel()
		event := &CodexEvent{
			Type: EventItemCompleted,
			Item: nil,
		}
		envs := m.Map(event)
		require.Nil(t, envs)
	})
}

func TestTypeRegistration(t *testing.T) {
	types := []string{}
	for _, wt := range worker.RegisteredTypes() {
		types = append(types, string(wt))
	}
	require.Contains(t, types, string(worker.TypeCodexCLI))
}

func TestCapabilities(t *testing.T) {
	t.Parallel()

	w := &ExecWorker{BaseWorker: base.NewBaseWorker(nil, nil)}

	require.Equal(t, worker.TypeCodexCLI, w.Type())
	require.True(t, w.SupportsResume())
	require.True(t, w.SupportsStreaming())
	require.True(t, w.SupportsTools())
	require.NotEmpty(t, w.EnvBlocklist())
	require.Contains(t, w.Modalities(), "text")
	require.Contains(t, w.Modalities(), "code")
}

// ─── v2 MapNotification Tests ───────────────────────────────────────────

func TestMapNotificationAgentMessageStateMachine(t *testing.T) {
	t.Parallel()

	m := NewMapper("session-1", func() int64 { return 0 })

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
		deltaParams := json.RawMessage(fmt.Sprintf(`{"itemId":"msg_1","textDelta":%q}`, word))
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

	m := NewMapper("session-1", func() int64 { return 0 })
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

	m := NewMapper("session-1", func() int64 { return 0 })
	params := json.RawMessage(`{"turn":{"usage":{"input_tokens":150,"output_tokens":75}}}`)
	envs := m.MapNotification("turn/completed", params)
	require.Len(t, envs, 1)
	require.Equal(t, events.Done, envs[0].Event.Type)
	dd, ok := envs[0].Event.Data.(events.DoneData)
	require.True(t, ok)
	require.True(t, dd.Success)
	require.Equal(t, 150, dd.Stats["input_tokens"])
	require.Equal(t, 75, dd.Stats["output_tokens"])
}

func TestMapNotificationApproval(t *testing.T) {
	t.Parallel()

	m := NewMapper("session-1", func() int64 { return 0 })
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

	m := NewMapper("session-1", func() int64 { return 0 })

	// Started
	started := json.RawMessage(`{"item":{"id":"cmd_1","type":"command_execution","command":"ls -la","cwd":"/tmp"}}`)
	envs := m.MapNotification("item/started", started)
	require.Len(t, envs, 1)
	require.Equal(t, events.ToolCall, envs[0].Event.Type)
	tc, ok := envs[0].Event.Data.(events.ToolCallData)
	require.True(t, ok)
	require.Equal(t, "shell", tc.Name)
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

	m := NewMapper("session-1", func() int64 { return 0 })
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

	m := NewMapper("session-1", func() int64 { return 0 })
	envs := m.MapNotification("thread/started", json.RawMessage(`{}`))
	require.Nil(t, envs)
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

	ch := mgr.Subscribe("thread-1")
	require.NotNil(t, ch)

	// Second subscribe returns same channel
	ch2 := mgr.Subscribe("thread-1")
	require.Equal(t, ch, ch2)

	// Different thread gets different channel
	ch3 := mgr.Subscribe("thread-2")
	require.NotNil(t, ch3)
	require.NotEqual(t, ch, ch3)

	// Unsubscribe closes channel and removes it
	mgr.Unsubscribe("thread-1")
	_, ok := <-ch
	require.False(t, ok) // channel closed

	// Re-subscribe after unsubscribe creates new channel
	ch4 := mgr.Subscribe("thread-1")
	require.NotNil(t, ch4)
	require.NotEqual(t, ch, ch4)
}

func TestManagerReleaseIdleDrain(t *testing.T) {
	cfg := config.CodexCLIConfig{IdleDrainPeriod: 50 * time.Millisecond}
	mgr := NewCodexAppServerManager(slog.Default(), cfg)

	// Simulate acquire without starting process (set state manually via reflection or test helper)
	// This test validates the idle drain timer logic only.
	mgr.mu.Lock()
	mgr.state = stateRunning
	mgr.refs = 1
	mgr.mu.Unlock()

	mgr.Release() // refs → 0, starts idle drain

	time.Sleep(100 * time.Millisecond)
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
	ch1 := mgr.Subscribe("thread-1")
	ch2 := mgr.Subscribe("thread-2")

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
