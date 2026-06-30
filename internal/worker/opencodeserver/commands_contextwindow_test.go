package opencodeserver

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// O1: queryContextUsage returns maxTokens from the configurable fallback
// (worker.opencode_server.context_window), independent of model — OCS's HTTP
// API does not expose the real context window.
func TestServerCommanderQueryContextUsage_ContextWindowFromConfig(t *testing.T) {
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
	c.contextWindow = 200000

	resp, err := c.SendControlRequest(context.Background(), "get_context_usage", nil)
	require.NoError(t, err)
	require.EqualValues(t, 200000, resp["maxTokens"], "maxTokens must mirror configured context_window regardless of model")
}

// O1: context_window=0 leaves maxTokens at 0 — context_pct stays unset rather
// than silently reporting a fabricated window.
func TestServerCommanderQueryContextUsage_ZeroWindowUnset(t *testing.T) {
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
	c.contextWindow = 0

	resp, err := c.SendControlRequest(context.Background(), "get_context_usage", nil)
	require.NoError(t, err)
	require.EqualValues(t, 0, resp["maxTokens"], "zero context_window must not fabricate a window")
}
