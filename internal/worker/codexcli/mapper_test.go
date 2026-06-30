package codexcli

import (
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/config"
)

func mustMarshalJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

// tokenUsageNotif builds a thread/tokenUsage/updated notification payload.
func tokenUsageNotif(t *testing.T, turnID string, tokenUsage map[string]any) json.RawMessage {
	t.Helper()
	return mustMarshalJSON(t, map[string]any{"turnId": turnID, "tokenUsage": tokenUsage})
}

// C1: context usage is sourced from thread/tokenUsage/updated (modelContextWindow
// + last input), not thread/read (which carries no token counts).
func TestMapper_ContextUsage_FromTokenUsageNotification(t *testing.T) {
	t.Parallel()
	m := NewMapper("sess")
	m.turnID = "t1"
	m.MapNotification("thread/tokenUsage/updated", tokenUsageNotif(t, "t1", map[string]any{
		"last":               map[string]any{"inputTokens": 100, "cachedInputTokens": 50, "outputTokens": 30},
		"modelContextWindow": 200000,
	}))

	cu := m.LastContextUsage()
	// totalTokens (context fill approximation) = input + cached input.
	require.EqualValues(t, 150, cu["totalTokens"])
	require.EqualValues(t, 200000, cu["maxTokens"])
}

// C1: usage from a different (stale) turn must not leak into the snapshot.
func TestMapper_ContextUsage_StaleTurnIgnored(t *testing.T) {
	t.Parallel()
	m := NewMapper("sess")
	m.turnID = "current"
	m.MapNotification("thread/tokenUsage/updated", tokenUsageNotif(t, "stale", map[string]any{
		"last": map[string]any{"inputTokens": 999},
	}))
	cu := m.LastContextUsage()
	_, ok := cu["totalTokens"]
	require.False(t, ok, "stale-turn usage must be ignored")
}

// C2a: trackedUsageStats must use the per-turn "last" delta, NOT the cumulative
// "total". SessionAccumulator.mergePerTurnStats does TotalInput += usage, so a
// per-turn delta accumulates correctly; feeding the cumulative total would
// double-count. This test guards against regressing to "total".
func TestMapper_TrackedUsageStats_UsesLastNotTotal(t *testing.T) {
	t.Parallel()
	m := NewMapper("sess")
	m.turnID = "t1"
	m.MapNotification("thread/tokenUsage/updated", tokenUsageNotif(t, "t1", map[string]any{
		"last":  map[string]any{"inputTokens": 100, "outputTokens": 20, "cachedInputTokens": 15},
		"total": map[string]any{"inputTokens": 500, "outputTokens": 80, "cachedInputTokens": 200},
	}))

	stats := m.trackedUsageStats()
	require.NotNil(t, stats)
	usage := stats["usage"].(map[string]any)
	require.EqualValues(t, 100, usage["input_tokens"], "must use per-turn last, not cumulative total")
	require.EqualValues(t, 20, usage["output_tokens"])
	// cachedInputTokens maps to Anthropic cache_read_input_tokens.
	require.EqualValues(t, 15, usage["cache_read_input_tokens"])
}

// C2b: cache_creation_input_tokens is intentionally absent — codex's
// TokenUsageBreakdown exposes only cachedInputTokens (cache_read) and no
// cache-creation dimension (protocol limitation).
func TestMapper_TrackedUsageStats_NoCacheCreation(t *testing.T) {
	t.Parallel()
	m := NewMapper("sess")
	m.turnID = "t1"
	m.MapNotification("thread/tokenUsage/updated", tokenUsageNotif(t, "t1", map[string]any{
		"last": map[string]any{"inputTokens": 100, "cachedInputTokens": 15, "outputTokens": 20},
	}))
	usage := m.trackedUsageStats()["usage"].(map[string]any)
	_, hasCreation := usage["cache_creation_input_tokens"]
	require.False(t, hasCreation, "codex protocol exposes no cache-creation dimension")
}

// model_usage carries contextWindow so SessionAccumulator can populate
// ContextWindow from the Done stats path (mirrors claudecode's format).
func TestMapper_TrackedUsageStats_ModelUsageCarriesContextWindow(t *testing.T) {
	t.Parallel()
	m := NewMapper("sess")
	m.SetModel("gpt-5.1-codex")
	m.turnID = "t1"
	m.MapNotification("thread/tokenUsage/updated", tokenUsageNotif(t, "t1", map[string]any{
		"last":               map[string]any{"inputTokens": 10, "outputTokens": 5},
		"modelContextWindow": 128000,
	}))
	mu := m.trackedUsageStats()["model_usage"].(map[string]any)
	inner := mu["gpt-5.1-codex"].(map[string]any)
	require.EqualValues(t, 128000, inner["contextWindow"])
}

// C3: model is seeded by SetModel (thread/start config) for normal turns.
func TestMapper_Model_SeededBySetModel(t *testing.T) {
	t.Parallel()
	m := NewMapper("sess")
	m.SetModel("gpt-5.1-codex")
	require.Equal(t, "gpt-5.1-codex", m.LastContextUsage()["model"])
}

// C3: model/rerouted updates the tracked model.
func TestMapper_Model_FromRerouted(t *testing.T) {
	t.Parallel()
	m := NewMapper("sess")
	m.MapNotification("model/rerouted", mustMarshalJSON(t, map[string]any{"toModel": "o3"}))
	require.Equal(t, "o3", m.LastContextUsage()["model"])
}

// C3: SetModel is a no-op for empty input.
func TestMapper_SetModel_EmptyIsNoOp(t *testing.T) {
	t.Parallel()
	m := NewMapper("sess")
	m.SetModel("gpt-5.1-codex")
	m.SetModel("")
	require.Equal(t, "gpt-5.1-codex", m.LastContextUsage()["model"])
}

func TestMapper_Reset_ClearsTrackedState(t *testing.T) {
	t.Parallel()
	m := NewMapper("sess")
	m.SetModel("gpt-5.1-codex")
	m.turnID = "t1"
	m.MapNotification("thread/tokenUsage/updated", tokenUsageNotif(t, "t1", map[string]any{
		"last":               map[string]any{"inputTokens": 10, "outputTokens": 5},
		"modelContextWindow": 200000,
	}))
	m.Reset()
	require.Empty(t, m.LastContextUsage(), "Reset must clear all context-usage state")
}

func TestManager_ContextUsage_DelegatesToConverter(t *testing.T) {
	t.Parallel()
	mgr := NewCodexAppServerManager(slog.Default(), config.CodexCLIConfig{})
	mgr.Subscribe("thr-delegate", "sess-delegate")
	require.Empty(t, mgr.LastContextUsage("thr-delegate"), "fresh manager has no usage")
	mgr.SetCurrentModel("thr-delegate", "gpt-5.1-codex")
	require.Equal(t, "gpt-5.1-codex", mgr.LastContextUsage("thr-delegate")["model"])
}
