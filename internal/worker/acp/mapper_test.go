package acp

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/pkg/events"
)

func newTestMapper() *ACPMapper {
	return NewACPMapper("sess_123", "user_1", slog.Default())
}

// ─── Agent Message Chunk ─────────────────────────────────────────────────────

func TestMapNotification_AgentMessageChunk(t *testing.T) {
	t.Parallel()
	m := newTestMapper()

	notif := &JSONRPCNotification{
		JSONRPC: "2.0",
		Method:  "session/update",
		Params: mustMarshal(map[string]any{
			"sessionId": "s1",
			"update": map[string]any{
				"sessionUpdate": "agent_message_chunk",
				"content":       map[string]string{"type": "text", "text": "Hello"},
			},
		}),
	}

	envs := m.MapNotification(context.Background(), notif)
	// First chunk: message.start + message.delta
	require.Len(t, envs, 2)
	require.Equal(t, events.MessageStart, envs[0].Event.Type)
	require.Equal(t, events.MessageDelta, envs[1].Event.Type)

	delta := envs[1].Event.Data.(events.MessageDeltaData)
	require.Equal(t, "Hello", delta.Content)

	// Second chunk: only message.delta (no message.start)
	envs2 := m.MapNotification(context.Background(), notif)
	require.Len(t, envs2, 1)
	require.Equal(t, events.MessageDelta, envs2[0].Event.Type)
}

// ─── Agent Thought Chunk ─────────────────────────────────────────────────────

func TestMapNotification_AgentThoughtChunk(t *testing.T) {
	t.Parallel()
	m := newTestMapper()

	notif := &JSONRPCNotification{
		JSONRPC: "2.0",
		Method:  "session/update",
		Params: mustMarshal(map[string]any{
			"sessionId": "s1",
			"update": map[string]any{
				"sessionUpdate": "agent_thought_chunk",
				"content":       map[string]string{"type": "text", "text": "thinking..."},
			},
		}),
	}

	envs := m.MapNotification(context.Background(), notif)
	require.Len(t, envs, 1)
	require.Equal(t, events.Reasoning, envs[0].Event.Type)

	data := envs[0].Event.Data.(events.ReasoningData)
	require.Equal(t, "thinking...", data.Content)
}

// ─── Tool Call ───────────────────────────────────────────────────────────────

func TestMapNotification_ToolCall(t *testing.T) {
	t.Parallel()
	m := newTestMapper()

	notif := &JSONRPCNotification{
		JSONRPC: "2.0",
		Method:  "session/update",
		Params: mustMarshal(map[string]any{
			"sessionId": "s1",
			"update": map[string]any{
				"sessionUpdate": "tool_call",
				"toolCallId":    "tc-1",
				"title":         "read: main.go",
				"kind":          "read",
				"rawInput":      map[string]string{"path": "main.go"},
			},
		}),
	}

	envs := m.MapNotification(context.Background(), notif)
	require.Len(t, envs, 1)
	require.Equal(t, events.ToolCall, envs[0].Event.Type)

	data := envs[0].Event.Data.(events.ToolCallData)
	require.Equal(t, "tc-1", data.ID)
	require.Equal(t, "read: main.go", data.Title)
	require.Equal(t, "read", data.Kind)
}

// ─── Tool Call Update ────────────────────────────────────────────────────────

func TestMapNotification_ToolCallUpdate_InProgress(t *testing.T) {
	t.Parallel()
	m := newTestMapper()

	notif := &JSONRPCNotification{
		JSONRPC: "2.0",
		Method:  "session/update",
		Params: mustMarshal(map[string]any{
			"sessionId": "s1",
			"update": map[string]any{
				"sessionUpdate": "tool_call_update",
				"toolCallId":    "tc-1",
				"status":        "in_progress",
			},
		}),
	}

	envs := m.MapNotification(context.Background(), notif)
	require.Len(t, envs, 1)
	require.Equal(t, events.ToolUpdate, envs[0].Event.Type)
}

func TestMapNotification_ToolCallUpdate_Completed(t *testing.T) {
	t.Parallel()
	m := newTestMapper()

	notif := &JSONRPCNotification{
		JSONRPC: "2.0",
		Method:  "session/update",
		Params: mustMarshal(map[string]any{
			"sessionId": "s1",
			"update": map[string]any{
				"sessionUpdate": "tool_call_update",
				"toolCallId":    "tc-1",
				"status":        "completed",
				"rawOutput":     "file contents here",
			},
		}),
	}

	envs := m.MapNotification(context.Background(), notif)
	require.Len(t, envs, 1)
	require.Equal(t, events.ToolResult, envs[0].Event.Type)

	data := envs[0].Event.Data.(events.ToolResultData)
	require.Equal(t, "tc-1", data.ID)
	require.Equal(t, "completed", data.Status)
}

// ─── Plan ────────────────────────────────────────────────────────────────────

func TestMapNotification_Plan(t *testing.T) {
	t.Parallel()
	m := newTestMapper()

	notif := &JSONRPCNotification{
		JSONRPC: "2.0",
		Method:  "session/update",
		Params: mustMarshal(map[string]any{
			"sessionId": "s1",
			"update": map[string]any{
				"sessionUpdate": "plan",
				"entries": []map[string]string{
					{"content": "Read file", "priority": "high", "status": "pending"},
					{"content": "Write tests", "priority": "medium", "status": "pending"},
				},
			},
		}),
	}

	envs := m.MapNotification(context.Background(), notif)
	require.Len(t, envs, 1)
	require.Equal(t, events.Plan, envs[0].Event.Type)

	data := envs[0].Event.Data.(events.PlanData)
	require.Len(t, data.Items, 2)
	require.Equal(t, "Read file", data.Items[0].Content)
	require.Equal(t, "high", data.Items[0].Priority)
}

// ─── Mode Update ─────────────────────────────────────────────────────────────

func TestMapNotification_ModeUpdate(t *testing.T) {
	t.Parallel()
	m := newTestMapper()

	notif := &JSONRPCNotification{
		JSONRPC: "2.0",
		Method:  "session/update",
		Params: mustMarshal(map[string]any{
			"sessionId": "s1",
			"update": map[string]any{
				"sessionUpdate": "current_mode_update",
				"currentModeId": "code",
			},
		}),
	}

	envs := m.MapNotification(context.Background(), notif)
	require.Len(t, envs, 1)
	require.Equal(t, events.ModeUpdate, envs[0].Event.Type)

	data := envs[0].Event.Data.(events.ModeUpdateData)
	require.Equal(t, "code", data.CurrentModeID)
}

// ─── Raw Passthrough ─────────────────────────────────────────────────────────

func TestMapNotification_ConfigOptionUpdate(t *testing.T) {
	t.Parallel()
	m := newTestMapper()

	notif := &JSONRPCNotification{
		JSONRPC: "2.0",
		Method:  "session/update",
		Params: mustMarshal(map[string]any{
			"sessionId": "s1",
			"update": map[string]any{
				"sessionUpdate": "config_option_update",
			},
		}),
	}

	envs := m.MapNotification(context.Background(), notif)
	require.Len(t, envs, 1)
	require.Equal(t, events.Raw, envs[0].Event.Type)

	data := envs[0].Event.Data.(events.RawData)
	require.Equal(t, "acp.config_option_update", data.Kind)
}

// ─── User Message Chunk (ignored) ────────────────────────────────────────────

func TestMapNotification_UserMessageChunk_Ignored(t *testing.T) {
	t.Parallel()
	m := newTestMapper()

	notif := &JSONRPCNotification{
		JSONRPC: "2.0",
		Method:  "session/update",
		Params: mustMarshal(map[string]any{
			"sessionId": "s1",
			"update": map[string]any{
				"sessionUpdate": "user_message_chunk",
			},
		}),
	}

	envs := m.MapNotification(context.Background(), notif)
	require.Nil(t, envs)
}

// ─── Prompt Response ─────────────────────────────────────────────────────────

func TestMapPromptResponse_EndTurn(t *testing.T) {
	t.Parallel()
	m := newTestMapper()
	m.msgActive.Store(true)

	envs := m.MapPromptResponse(&PromptResult{
		StopReason: "end_turn",
		Usage:      PromptUsage{InputTokens: 100, OutputTokens: 50, TotalTokens: 150},
	})

	// message.end + done
	require.Len(t, envs, 2)
	require.Equal(t, events.MessageEnd, envs[0].Event.Type)
	require.Equal(t, events.Done, envs[1].Event.Type)

	done := envs[1].Event.Data.(events.DoneData)
	require.True(t, done.Success)
	usage := done.Stats["usage"].(map[string]any)
	require.Equal(t, 100, usage["input_tokens"])
	require.Equal(t, "end_turn", done.Stats["stop_reason"])
}

func TestMapPromptResponse_MaxTokens(t *testing.T) {
	t.Parallel()
	m := newTestMapper()

	envs := m.MapPromptResponse(&PromptResult{StopReason: "max_tokens"})
	require.Len(t, envs, 1) // no message.end (no message stream active)
	require.Equal(t, events.Done, envs[0].Event.Type)

	done := envs[0].Event.Data.(events.DoneData)
	require.False(t, done.Success)
}

// ─── Prompt Error ────────────────────────────────────────────────────────────

func TestMapPromptError(t *testing.T) {
	t.Parallel()
	m := newTestMapper()
	m.msgActive.Store(true)

	envs := m.MapPromptError(errors.New("something failed"))
	require.Len(t, envs, 3) // message.end + error + done
	require.Equal(t, events.MessageEnd, envs[0].Event.Type)
	require.Equal(t, events.Error, envs[1].Event.Type)
	require.Equal(t, events.Done, envs[2].Event.Type)

	done := envs[2].Event.Data.(events.DoneData)
	require.False(t, done.Success)
}

// ─── Permission Request ──────────────────────────────────────────────────────

func TestMapPermissionRequest(t *testing.T) {
	t.Parallel()
	m := newTestMapper()

	req := &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      mustMarshal(10),
		Method:  "session/request_permission",
		Params: mustMarshal(map[string]any{
			"sessionId": "s1",
			"toolCall": map[string]any{
				"toolCallId": "tc-perm",
				"title":      "Write file",
				"kind":       "edit",
				"rawInput":   map[string]string{"path": "/tmp/test.go"},
			},
			"options": []map[string]any{
				{"optionId": "opt-allow", "name": "Allow Once", "kind": "allow_once"},
				{"optionId": "opt-deny", "name": "Deny", "kind": "reject_once"},
			},
		}),
	}

	pm := m.MapPermissionRequest(req)
	require.NotNil(t, pm)
	require.Equal(t, events.PermissionRequest, pm.Envelope.Event.Type)
	require.Equal(t, "opt-allow", pm.AllowOptionID)
	require.Equal(t, "opt-deny", pm.DenyOptionID)

	// Test outcome formatting.
	allowed := pm.FormatAllowedOutcome()
	require.Equal(t, "selected", allowed["outcome"])
	require.Equal(t, "opt-allow", allowed["optionId"])

	denied := pm.FormatDeniedOutcome()
	require.Equal(t, "opt-deny", denied["optionId"])
}

// ─── Non-session/update ignored ──────────────────────────────────────────────

func TestMapNotification_NonSessionUpdate(t *testing.T) {
	t.Parallel()
	m := newTestMapper()

	notif := &JSONRPCNotification{
		JSONRPC: "2.0",
		Method:  "other/method",
		Params:  mustMarshal(map[string]any{}),
	}

	envs := m.MapNotification(context.Background(), notif)
	require.Nil(t, envs)
}

// ─── AEP extension omitempty ─────────────────────────────────────────────────

func TestToolCallData_OmitEmpty(t *testing.T) {
	t.Parallel()

	data := events.ToolCallData{ID: "c1", Name: "read", Input: map[string]any{"path": "main.go"}}
	raw, err := json.Marshal(data)
	require.NoError(t, err)
	require.NotContains(t, string(raw), `"title"`)
	require.NotContains(t, string(raw), `"kind"`)
	require.NotContains(t, string(raw), `"locations"`)
}

func TestToolResultData_OmitEmpty(t *testing.T) {
	t.Parallel()

	data := events.ToolResultData{ID: "c1", Output: "ok"}
	raw, err := json.Marshal(data)
	require.NoError(t, err)
	require.NotContains(t, string(raw), `"status"`)
	require.NotContains(t, string(raw), `"diff"`)
}
