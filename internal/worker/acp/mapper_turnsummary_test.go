package acp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/pkg/events"
)

// A1: buildStats emits model_usage (Claude Code format) so
// SessionAccumulator.mergePerTurnStats can populate ModelName. Model sourced
// from a usage_update notification that carries a model field.
func TestBuildStats_ModelUsage_FromUsageUpdate(t *testing.T) {
	t.Parallel()
	m := newTestMapper()
	m.MapNotification(context.Background(), &JSONRPCNotification{
		JSONRPC: "2.0",
		Method:  "session/update",
		Params: mustMarshal(map[string]any{
			"sessionId": "s1",
			"update": map[string]any{
				"sessionUpdate": "usage_update",
				"model":         "claude-sonnet-4-6",
				"inputTokens":   100,
			},
		}),
	})

	stats := m.buildStats(&PromptResult{StopReason: "end_turn"})
	mu, ok := stats["model_usage"].(map[string]any)
	require.True(t, ok, "model_usage must be emitted when model is known")
	require.Contains(t, mu, "claude-sonnet-4-6")
}

// A1: model from session/set_model is reflected in buildStats even when the
// agent never emits usage_update.
func TestBuildStats_ModelUsage_FromSetModel(t *testing.T) {
	t.Parallel()
	m := newTestMapper()
	m.SetModel("gpt-4o")

	stats := m.buildStats(&PromptResult{StopReason: "end_turn"})
	mu := stats["model_usage"].(map[string]any)
	require.Contains(t, mu, "gpt-4o")
}

// A1: when no model is known, model_usage is omitted rather than guessed.
func TestBuildStats_NoModelUsage_WhenUnknown(t *testing.T) {
	t.Parallel()
	m := newTestMapper()
	stats := m.buildStats(&PromptResult{StopReason: "end_turn"})
	_, ok := stats["model_usage"]
	require.False(t, ok, "model_usage must be omitted when model is unknown")
}

// A1: SetModel with empty input is a no-op (does not clear a known model).
func TestSetModel_EmptyIsNoOp(t *testing.T) {
	t.Parallel()
	m := newTestMapper()
	m.SetModel("gpt-4o")
	m.SetModel("")
	stats := m.buildStats(&PromptResult{StopReason: "end_turn"})
	require.Contains(t, stats["model_usage"].(map[string]any), "gpt-4o")
}

// A2: non-ClaudeCode agent tool name resolves via the fallback chain
// _meta.claudeCode.toolName → kind → title, so it never aggregates to an
// empty string.
func TestToolCall_NameFallback_KindThenTitle(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		kind     string
		title    string
		content  map[string]any
		wantName string
	}{
		{
			name:     "kind present",
			kind:     "read",
			title:    "read: main.go",
			wantName: "read",
		},
		{
			name:     "kind empty falls back to title",
			kind:     "",
			title:    "Search the web",
			wantName: "Search the web",
		},
		{
			name:     "claudeCode toolName takes precedence over kind",
			kind:     "read",
			title:    "read: x",
			content:  map[string]any{"_meta": map[string]any{"claudeCode": map[string]any{"toolName": "Read"}}},
			wantName: "Read",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := newTestMapper()
			update := map[string]any{
				"sessionUpdate": "tool_call",
				"toolCallId":    "tc-1",
				"kind":          tc.kind,
				"title":         tc.title,
			}
			if tc.content != nil {
				update["content"] = tc.content
			}
			envs := m.MapNotification(context.Background(), &JSONRPCNotification{
				JSONRPC: "2.0",
				Method:  "session/update",
				Params: mustMarshal(map[string]any{
					"sessionId": "s1",
					"update":    update,
				}),
			})
			require.Len(t, envs, 1)
			data := envs[0].Event.Data.(events.ToolCallData)
			require.Equal(t, tc.wantName, data.Name)
		})
	}
}

// A3: handleContextUsage surfaces model alongside context fill/window. Context
// values remain zero when usage_update is absent (documented protocol limit;
// ACP has no session/info).
func TestLastUsage_ReflectsSetModel(t *testing.T) {
	t.Parallel()
	m := newTestMapper()
	m.SetModel("claude-haiku-4-5")
	snap := m.LastUsage()
	require.Equal(t, "claude-haiku-4-5", snap.Model)
}
