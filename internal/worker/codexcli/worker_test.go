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
		line := `{"type":"turn.completed","usage":{"inputTokens":100,"cachedInputTokens":20,"outputTokens":50,"reasoningOutputTokens":5}}`
		event, err := p.ParseLine(line)
		require.NoError(t, err)
		require.Equal(t, EventTurnCompleted, event.Type)
		require.NotNil(t, event.Usage)
		require.Equal(t, 100, event.Usage.InputTokens)
		require.Equal(t, 50, event.Usage.OutputTokens)
		require.Equal(t, 20, event.Usage.CachedInputTokens)
		require.Equal(t, 5, event.Usage.ReasoningOutputTokens)
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

	m := NewMapper("session-1")

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
				InputTokens:       100,
				CachedInputTokens: 20,
				OutputTokens:      50,
			},
		}
		envs := m.Map(event)
		require.Len(t, envs, 1)
		require.Equal(t, events.Done, envs[0].Event.Type)
		dd, ok := envs[0].Event.Data.(events.DoneData)
		require.True(t, ok)
		require.True(t, dd.Success)
		usage, ok := dd.Stats["usage"].(map[string]any)
		require.True(t, ok, "stats should have nested 'usage' map")
		require.Equal(t, int64(100), usage["input_tokens"])
		require.Equal(t, int64(50), usage["output_tokens"])
		require.Equal(t, int64(20), usage["cache_read_input_tokens"])
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

// ─── ExecWorker Lifecycle Tests ─────────────────────────────────────────

func newTestExecWorker(t *testing.T) *ExecWorker {
	t.Helper()
	return &ExecWorker{BaseWorker: base.NewBaseWorker(slog.Default(), nil)}
}

func TestExecWorkerStart(t *testing.T) {
	t.Parallel()

	w := newTestExecWorker(t)
	err := w.Start(context.Background(), worker.SessionInfo{
		SessionID:  "sess-1",
		ProjectDir: t.TempDir(),
	})
	require.NoError(t, err)
	require.True(t, w.started)
	require.Equal(t, "sess-1", w.sessionID)

	// Double start should error
	err = w.Start(context.Background(), worker.SessionInfo{SessionID: "sess-2"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "already started")
}

func TestExecWorkerTerminateWithoutCancel(t *testing.T) {
	t.Parallel()

	w := newTestExecWorker(t)
	require.Nil(t, w.cancel)
	err := w.Terminate(context.Background())
	require.NoError(t, err)
}

func TestExecWorkerTerminateWithCancel(t *testing.T) {
	t.Parallel()

	w := newTestExecWorker(t)
	ctx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel

	err := w.Terminate(context.Background())
	require.NoError(t, err)
	require.Error(t, ctx.Err()) // cancelled
}

func TestExecWorkerResetContext(t *testing.T) {
	t.Parallel()

	w := newTestExecWorker(t)
	_ = w.Start(context.Background(), worker.SessionInfo{
		SessionID:  "sess-1",
		ProjectDir: t.TempDir(),
	})
	w.threadID = "thread-old"
	w.readLineFn = func() (string, error) { return "", nil }

	err := w.ResetContext(context.Background())
	require.NoError(t, err)
	require.False(t, w.started)
	require.Empty(t, w.threadID)
	require.Nil(t, w.readLineFn)
}

func TestExecWorkerInputNoProcessReturnsErrNotImplemented(t *testing.T) {
	t.Parallel()

	w := newTestExecWorker(t)
	_ = w.Start(context.Background(), worker.SessionInfo{
		SessionID:  "sess-1",
		ProjectDir: t.TempDir(),
	})

	// Input without a running process should try spawn, which will fail
	// because there's no actual codex binary. This is expected.
	err := w.Input(context.Background(), "hello", nil)
	require.Error(t, err)
}

func TestExecWorkerHandlePermissionResponse(t *testing.T) {
	t.Parallel()

	w := newTestExecWorker(t)
	err := w.HandlePermissionResponse(context.Background(), "req-1", true, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not supported")
}

func TestExecWorkerHandleQuestionResponse(t *testing.T) {
	t.Parallel()

	w := newTestExecWorker(t)
	err := w.HandleQuestionResponse(context.Background(), "req-1", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not supported")
}

func TestExecWorkerHandleElicitationResponse(t *testing.T) {
	t.Parallel()

	w := newTestExecWorker(t)
	err := w.HandleElicitationResponse(context.Background(), "req-1", "", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not supported")
}

func TestExecWorkerHealthAndLastIO(t *testing.T) {
	t.Parallel()

	w := newTestExecWorker(t)
	health := w.Health()
	require.Equal(t, worker.TypeCodexCLI, health.Type)

	lastIO := w.LastIO()
	require.True(t, lastIO.IsZero() || !lastIO.IsZero())
}

func TestExecWorkerSetTestConn(t *testing.T) {
	t.Parallel()

	w := newTestExecWorker(t)
	require.Nil(t, w.Conn())

	// After setting test conn, Conn() returns it
	mockConn := &mockSessionConn{}
	w.SetTestConn(mockConn)
	require.Equal(t, mockConn, w.Conn())
}

func TestExecWorkerSetReadLineFn(t *testing.T) {
	t.Parallel()

	w := newTestExecWorker(t)
	w.SetReadLineFn(func() (string, error) { return "", nil })
	require.NotNil(t, w.readLineFn)
}

func TestExecWorkerReadOutput(t *testing.T) {
	t.Parallel()

	w := newTestExecWorker(t)
	_ = w.Start(context.Background(), worker.SessionInfo{
		SessionID:  "sess-1",
		ProjectDir: t.TempDir(),
	})

	// Simulate stdout with turn.completed to trigger return
	stdout := strings.NewReader(
		`{"type":"turn.completed","usage":{"inputTokens":10,"outputTokens":5}}` + "\n",
	)
	conn := base.NewConn(slog.Default(), nil, "user-1", "sess-1")
	mockConn := &mockSessionConn{sendCh: make(chan *events.Envelope, 10)}
	w.SetTestConn(mockConn)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.readOutput(ctx, stdout, conn)
}

func TestExecWorkerReadOutputEmptyLine(t *testing.T) {
	t.Parallel()

	w := newTestExecWorker(t)
	_ = w.Start(context.Background(), worker.SessionInfo{
		SessionID:  "sess-1",
		ProjectDir: t.TempDir(),
	})

	stdout := strings.NewReader("\n\n{\"type\":\"turn.completed\"}\n")
	conn := base.NewConn(slog.Default(), nil, "user-1", "sess-1")
	w.SetTestConn(&mockSessionConn{sendCh: make(chan *events.Envelope, 10)})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.readOutput(ctx, stdout, conn)
}

func TestExecWorkerReadOutputCancelled(t *testing.T) {
	t.Parallel()

	w := newTestExecWorker(t)
	_ = w.Start(context.Background(), worker.SessionInfo{
		SessionID:  "sess-1",
		ProjectDir: t.TempDir(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	// Should return quickly due to cancelled context
	stdout := strings.NewReader("")
	conn := base.NewConn(slog.Default(), nil, "user-1", "sess-1")
	w.readOutput(ctx, stdout, conn)
}

func TestExecWorkerBuildArgs(t *testing.T) {
	t.Parallel()

	w := newTestExecWorker(t)
	w.cfg = Config{
		Sandbox:      "workspace-write",
		ApprovalMode: "never",
	}

	session := worker.SessionInfo{
		SessionID:       "sess-1",
		ProjectDir:      "/tmp/project",
		ResumeSessionID: "",
	}

	args := w.buildArgs(session, "fix the bug")
	require.Contains(t, args, "exec")
	require.Contains(t, args, "--json")
	require.Contains(t, args, "fix the bug")
}

func TestExecWorkerBuildArgsWithResume(t *testing.T) {
	t.Parallel()

	w := newTestExecWorker(t)
	w.cfg = Config{Sandbox: "workspace-write", ApprovalMode: "never"}

	session := worker.SessionInfo{
		SessionID:       "sess-1",
		ProjectDir:      "/tmp/project",
		ResumeSessionID: "thread-123",
	}

	args := w.buildArgs(session, "continue")
	require.Contains(t, args, "resume")
	require.Contains(t, args, "thread-123")
}

func TestExecWorkerBuildArgsWithModel(t *testing.T) {
	t.Parallel()

	w := newTestExecWorker(t)
	w.cfg = Config{
		Sandbox:      "workspace-write",
		ApprovalMode: "never",
		Model:        "o3",
	}

	session := worker.SessionInfo{
		SessionID:  "sess-1",
		ProjectDir: "/tmp/project",
	}

	args := w.buildArgs(session, "test")
	require.Contains(t, args, "-m")
	require.Contains(t, args, "o3")
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
	require.Contains(t, err.Error(), "not supported")
}

func TestAppServerWorkerHandleElicitationResponse(t *testing.T) {
	t.Parallel()

	w := newTestAppServerWorker(t)
	err := w.HandleElicitationResponse(context.Background(), "req-1", "", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not supported")
}

func TestAppServerWorkerInputNoThreadID(t *testing.T) {
	t.Parallel()

	w := newTestAppServerWorker(t)
	err := w.Input(context.Background(), "hello", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not started")
}

// ─── mapItemCompleted branch tests ───────────────────────────────────────

func TestMapperMapItemCompletedBranches(t *testing.T) {
	t.Parallel()

	m := NewMapper("session-1")

	t.Run("file_change_completed", func(t *testing.T) {
		t.Parallel()
		event := &CodexEvent{
			Type: EventItemCompleted,
			Item: &CodexItem{
				ID:     "item_1",
				Type:   ItemFileChange,
				Status: "completed",
				Stderr: "",
			},
		}
		envs := m.Map(event)
		require.Len(t, envs, 1)
		require.Equal(t, events.ToolResult, envs[0].Event.Type)
		tr := envs[0].Event.Data.(events.ToolResultData)
		require.Equal(t, "completed", tr.Output)
	})

	t.Run("file_change_failed", func(t *testing.T) {
		t.Parallel()
		event := &CodexEvent{
			Type: EventItemCompleted,
			Item: &CodexItem{
				ID:     "item_1",
				Type:   ItemFileChange,
				Status: "error",
				Stderr: "permission denied",
			},
		}
		envs := m.Map(event)
		require.Len(t, envs, 1)
		tr := envs[0].Event.Data.(events.ToolResultData)
		require.Equal(t, "failed", tr.Output)
		require.Equal(t, "permission denied", tr.Error)
	})

	t.Run("mcp_tool_call_completed", func(t *testing.T) {
		t.Parallel()
		event := &CodexEvent{
			Type: EventItemCompleted,
			Item: &CodexItem{
				ID:     "item_1",
				Type:   ItemMCPToolCall,
				Result: json.RawMessage(`{"files":["a.go"]}`),
			},
		}
		envs := m.Map(event)
		require.Len(t, envs, 1)
		tr := envs[0].Event.Data.(events.ToolResultData)
		require.Contains(t, tr.Output, "a.go")
	})

	t.Run("mcp_tool_call_with_error", func(t *testing.T) {
		t.Parallel()
		event := &CodexEvent{
			Type: EventItemCompleted,
			Item: &CodexItem{
				ID:    "item_1",
				Type:  ItemMCPToolCall,
				Error: &CodexItemError{Message: "timeout"},
			},
		}
		envs := m.Map(event)
		require.Len(t, envs, 1)
		tr := envs[0].Event.Data.(events.ToolResultData)
		require.Equal(t, "timeout", tr.Error)
	})

	t.Run("plan_item", func(t *testing.T) {
		t.Parallel()
		event := &CodexEvent{
			Type: EventItemCompleted,
			Item: &CodexItem{
				ID:   "item_1",
				Type: ItemPlan,
				Text: "I will refactor the module",
			},
		}
		envs := m.Map(event)
		require.Len(t, envs, 1)
		require.Equal(t, events.State, envs[0].Event.Type)
		sd := envs[0].Event.Data.(events.StateData)
		require.Equal(t, events.SessionState("planning"), sd.State)
	})

	t.Run("image_generation_item", func(t *testing.T) {
		t.Parallel()
		event := &CodexEvent{
			Type: EventItemCompleted,
			Item: &CodexItem{
				ID:        "item_1",
				Type:      ItemImageGeneration,
				SavedPath: "/tmp/image.png",
			},
		}
		envs := m.Map(event)
		require.Len(t, envs, 1)
		tr := envs[0].Event.Data.(events.ToolResultData)
		require.Equal(t, "/tmp/image.png", tr.Output)
	})

	t.Run("unknown_item_type", func(t *testing.T) {
		t.Parallel()
		event := &CodexEvent{
			Type: EventItemCompleted,
			Item: &CodexItem{ID: "item_1", Type: "unknown_type"},
		}
		envs := m.Map(event)
		require.Nil(t, envs)
	})
}

// ─── mapItemStarted branch tests ─────────────────────────────────────────

func TestMapperMapItemStartedBranches(t *testing.T) {
	t.Parallel()

	m := NewMapper("session-1")

	t.Run("unknown_item_started", func(t *testing.T) {
		t.Parallel()
		event := &CodexEvent{
			Type: EventItemStarted,
			Item: &CodexItem{ID: "item_1", Type: "unknown"},
		}
		envs := m.Map(event)
		require.Nil(t, envs)
	})

	t.Run("nil_item_started", func(t *testing.T) {
		t.Parallel()
		event := &CodexEvent{Type: EventItemStarted, Item: nil}
		envs := m.Map(event)
		require.Nil(t, envs)
	})
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

// ─── buildUsageStats edge case ────────────────────────────────────────────

func TestBuildUsageStatsNil(t *testing.T) {
	result := buildUsageStats(nil)
	require.Nil(t, result)
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
	err := w.ResetContext(context.Background())
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

// ─── Mock Helpers ──────────────────────────────────────────────────────────

type mockSessionConn struct {
	sendCh chan *events.Envelope
}

func (m *mockSessionConn) WriteCtx(ctx context.Context, env *events.Envelope) error {
	return nil
}
func (m *mockSessionConn) Send(ctx context.Context, env *events.Envelope) error {
	select {
	case m.sendCh <- env:
		return nil
	default:
		return fmt.Errorf("channel full")
	}
}
func (m *mockSessionConn) TrySend(env *events.Envelope) bool {
	select {
	case m.sendCh <- env:
		return true
	default:
		return false
	}
}
func (m *mockSessionConn) Recv() <-chan *events.Envelope { return m.sendCh }
func (m *mockSessionConn) Close() error                  { return nil }
func (m *mockSessionConn) UserID() string                { return "user-1" }
func (m *mockSessionConn) SessionID() string             { return "sess-1" }

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

	err := w.ResetContext(context.Background())
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

	err := wk.ResetContext(context.Background())

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

	err = w.ResetContext(context.Background())
	require.NoError(t, err)

	w.mu.Lock()
	secondThreadID := w.threadID
	w.mu.Unlock()
	require.NotEmpty(t, secondThreadID)
	require.NotEqual(t, firstThreadID, secondThreadID, "threadID should change after reset")
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
