package opencodeserver

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContextWindowForModel(t *testing.T) {
	t.Parallel()
	cases := []struct {
		model string
		want  int64
	}{
		{"anthropic/claude-sonnet-4-6", 200000},
		{"anthropic/claude-opus-4-6", 200000},
		{"anthropic/claude-3-5-haiku-20241022", 200000},
		{"openai/gpt-4o", 128000},
		{"openai/gpt-4.1", 1047576},
		{"google/gemini-2.5-pro", 1048576},
		{"openai/o3-mini", 200000},
		{"deepseek/deepseek-chat", 64000},
		{"unknown/future-model-9", 0}, // unknown → 0, never guessed
		{"", 0},
		{"Anthropic/Claude-Sonnet-4", 200000}, // case-insensitive
	}
	for _, tc := range cases {
		got := contextWindowForModel(tc.model)
		require.Equal(t, tc.want, got, "model=%q", tc.model)
	}
}

// O1: queryContextUsage returns a non-zero maxTokens for known models (sourced
// from the static map, since OCS's HTTP API does not expose context window).
func TestServerCommanderQueryContextUsage_ContextWindow(t *testing.T) {
	t.Parallel()
	c, _ := newTestCommander(t, func(w http.ResponseWriter, _ *http.Request) {
		messages := []any{
			map[string]any{"info": map[string]any{
				"role":   "assistant",
				"tokens": map[string]any{"input": 100, "output": 200, "reasoning": 0, "cache": map[string]any{"read": 30, "write": 20}},
				"model":  map[string]any{"providerID": "anthropic", "modelID": "claude-sonnet-4-6"},
			}},
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(messages)
	})

	resp, err := c.SendControlRequest(context.Background(), "get_context_usage", nil)
	require.NoError(t, err)
	require.EqualValues(t, 200000, resp["maxTokens"], "known model must map to its context window")
}

// O1: unknown models leave maxTokens at 0 — context_pct stays unset rather
// than silently reporting a fabricated window.
func TestServerCommanderQueryContextUsage_UnknownModelZeroWindow(t *testing.T) {
	t.Parallel()
	c, _ := newTestCommander(t, func(w http.ResponseWriter, _ *http.Request) {
		messages := []any{
			map[string]any{"info": map[string]any{
				"role":   "assistant",
				"tokens": map[string]any{"input": 100, "output": 200, "cache": map[string]any{"read": 0, "write": 0}},
				"model":  map[string]any{"providerID": "acme", "modelID": "future-model-9"},
			}},
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(messages)
	})

	resp, err := c.SendControlRequest(context.Background(), "get_context_usage", nil)
	require.NoError(t, err)
	require.EqualValues(t, 0, resp["maxTokens"], "unknown model must not guess a window")
}
