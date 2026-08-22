package acp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/worker/base"
)

// ─── parseMCPServers ──────────────────────────────────────────────────────────

func TestParseMCPServers_Empty(t *testing.T) {
	t.Parallel()
	require.Nil(t, parseMCPServers(""))
}

func TestParseMCPServers_InvalidJSON(t *testing.T) {
	t.Parallel()
	require.Nil(t, parseMCPServers("{invalid"))
}

func TestParseMCPServers_NoMCPKey(t *testing.T) {
	t.Parallel()
	require.Nil(t, parseMCPServers(`{"other":{}}`))
}

func TestParseMCPServers_MapInput(t *testing.T) {
	t.Parallel()
	result := parseMCPServers(`{"mcpServers":{"fs":{"type":"filesystem","path":"/tmp"}}}`)
	require.Len(t, result, 1)

	// Verify name was injected.
	m, ok := result[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "fs", m["name"])
	require.Equal(t, "filesystem", m["type"])
}

func TestParseMCPServers_ArrayInput(t *testing.T) {
	t.Parallel()
	result := parseMCPServers(`{"mcpServers":[{"type":"filesystem"}]}`)
	require.Len(t, result, 1)
}

// ─── normalizeMCPServersToArray ───────────────────────────────────────────────

func TestNormalizeMCPServersToArray_Nil(t *testing.T) {
	t.Parallel()
	require.Nil(t, normalizeMCPServersToArray(nil))
}

func TestNormalizeMCPServersToArray_ArrayPassthrough(t *testing.T) {
	t.Parallel()
	arr := []any{"a", "b"}
	require.Equal(t, arr, normalizeMCPServersToArray(arr))
}

func TestNormalizeMCPServersToArray_InvalidType(t *testing.T) {
	t.Parallel()
	require.Nil(t, normalizeMCPServersToArray(42))
}

func TestNormalizeMCPServersToArray_MapConversion(t *testing.T) {
	t.Parallel()
	input := map[string]any{
		"server1": map[string]any{"url": "http://a"},
		"server2": "not-a-map", // should be skipped
	}
	result := normalizeMCPServersToArray(input)
	require.Len(t, result, 1)

	m, ok := result[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "server1", m["name"])
	require.Equal(t, "http://a", m["url"])
}

// ─── LastInput ────────────────────────────────────────────────────────────────

func TestACPConn_LastInput_Empty(t *testing.T) {
	t.Parallel()
	c := newACPConn("u", "s", nil)
	require.Equal(t, "", c.LastInput())
}

func TestACPConn_LastInput_StoreAndLoad(t *testing.T) {
	t.Parallel()
	c := newACPConn("u", "s", nil)

	content := "hello world"
	c.lastInput.Store(&content)
	require.Equal(t, "hello world", c.LastInput())
}

func TestACPConn_LastInput_Overwrite(t *testing.T) {
	t.Parallel()
	c := newACPConn("u", "s", nil)

	first := "first message"
	c.lastInput.Store(&first)
	second := "second message"
	c.lastInput.Store(&second)
	require.Equal(t, "second message", c.LastInput())
}

func TestACPFirstOrdinaryInputExcludesFullSystemPrompt(t *testing.T) {
	t.Parallel()

	agentStdinR, agentStdinW := io.Pipe()
	agentStdoutR, agentStdoutW := io.Pipe()
	defer agentStdinW.Close()
	defer agentStdoutW.Close()

	client := NewACPClient(agentStdinW, agentStdoutR, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client.StartReadLoop(ctx)

	receivedPrompt := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(agentStdinR)
		if !scanner.Scan() {
			return
		}
		var req struct {
			ID     json.RawMessage `json:"id"`
			Params struct {
				Prompt []struct {
					Text string `json:"text"`
				} `json:"prompt"`
			} `json:"params"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil || len(req.Params.Prompt) == 0 {
			return
		}
		receivedPrompt <- req.Params.Prompt[0].Text
		_ = WriteMessage(agentStdoutW, &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  mustMarshal(PromptResult{StopReason: "end_turn"}),
		})
	}()

	w := &Worker{BaseWorker: base.NewBaseWorker(nil, nil)}
	w.client = client
	w.mapper = newTestMapper()
	w.conn = newACPConn("user-1", "session-1", nil)
	w.SetWorkerSessionID("acp-session-1")
	w.drainCh = make(chan struct{}, 1)
	w.drainDoneCh = make(chan struct{})
	close(w.drainDoneCh)
	w.systemPrompt = "PRIVATE_PROMPT_SENTINEL"

	require.NoError(t, w.Input(ctx, "hello", nil))
	received := <-receivedPrompt
	require.Equal(t, acpCompatibilityRules+"\n\nhello", received)
	require.NotContains(t, received, "PRIVATE_PROMPT_SENTINEL")
}

func TestACPSystemPromptUnsupportedDiagnosticIsBounded(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	w := &Worker{BaseWorker: base.NewBaseWorker(logger, nil)}

	w.recordSystemPromptUnsupported(1)
	w.recordSystemPromptUnsupported(1)

	require.Contains(t, logs.String(), "ACP_SYSTEM_PROMPT_UNSUPPORTED")
	require.Equal(t, 1, strings.Count(logs.String(), "ACP_SYSTEM_PROMPT_UNSUPPORTED"))
	require.NotContains(t, logs.String(), "PRIVATE")
}

// ─── usageSnapshot / updateUsage / LastUsage / Reset ──────────────────────────

func TestUpdateUsage_AccumulatesTokens(t *testing.T) {
	t.Parallel()
	m := newTestMapper()

	// First usage_update.
	raw := mustMarshal(map[string]any{
		"sessionUpdate": "usage_update",
		"inputTokens":   100,
		"outputTokens":  50,
		"totalTokens":   150,
		"contextSize":   200000,
		"contextUsed":   5000,
	})
	m.updateUsage(context.Background(), raw)

	snap := m.LastUsage()
	require.Equal(t, 100, snap.InputTokens)
	require.Equal(t, 50, snap.OutputTokens)
	require.Equal(t, 150, snap.TotalTokens)
	require.Equal(t, 200000, snap.ContextSize)
	require.Equal(t, 5000, snap.ContextUsed)
}

func TestUpdateUsage_AccumulatesAcrossMultiple(t *testing.T) {
	t.Parallel()
	m := newTestMapper()

	for _, tc := range []struct {
		input  int
		output int
		total  int
	}{
		{100, 50, 150},
		{200, 100, 300},
	} {
		raw := mustMarshal(map[string]any{
			"sessionUpdate": "usage_update",
			"inputTokens":   tc.input,
			"outputTokens":  tc.output,
			"totalTokens":   tc.total,
		})
		m.updateUsage(context.Background(), raw)
	}

	snap := m.LastUsage()
	require.Equal(t, 300, snap.InputTokens)  // 100 + 200
	require.Equal(t, 150, snap.OutputTokens) // 50 + 100
	require.Equal(t, 450, snap.TotalTokens)  // 150 + 300
}

func TestUpdateUsage_CostAccumulation(t *testing.T) {
	t.Parallel()
	m := newTestMapper()

	raw := mustMarshal(map[string]any{
		"sessionUpdate": "usage_update",
		"cost": map[string]any{
			"amount":   0.05,
			"currency": "USD",
		},
	})
	m.updateUsage(context.Background(), raw)

	snap := m.LastUsage()
	require.NotNil(t, snap.Cost)
	require.InDelta(t, 0.05, snap.Cost.Amount, 0.001)
	require.Equal(t, "USD", snap.Cost.Currency)
}

func TestUpdateUsage_CostMultipleAccumulates(t *testing.T) {
	t.Parallel()
	m := newTestMapper()

	for _, amt := range []float64{0.03, 0.07} {
		raw := mustMarshal(map[string]any{
			"sessionUpdate": "usage_update",
			"cost":          map[string]any{"amount": amt, "currency": "USD"},
		})
		m.updateUsage(context.Background(), raw)
	}

	snap := m.LastUsage()
	require.InDelta(t, 0.10, snap.Cost.Amount, 0.001)
}

func TestUpdateUsage_InvalidJSON_Skipped(t *testing.T) {
	t.Parallel()
	m := newTestMapper()
	// Should not panic on malformed update.
	m.updateUsage(context.Background(), json.RawMessage(`{bad json`))
	snap := m.LastUsage()
	require.Equal(t, 0, snap.InputTokens)
}

func TestUpdateUsage_ContextSizeOverwrites(t *testing.T) {
	t.Parallel()
	m := newTestMapper()

	// Context size is absolute snapshot, not cumulative.
	raw1 := mustMarshal(map[string]any{"sessionUpdate": "usage_update", "contextSize": 100})
	m.updateUsage(context.Background(), raw1)
	require.Equal(t, 100, m.LastUsage().ContextSize)

	raw2 := mustMarshal(map[string]any{"sessionUpdate": "usage_update", "contextSize": 200})
	m.updateUsage(context.Background(), raw2)
	require.Equal(t, 200, m.LastUsage().ContextSize)
}

func TestReset_ClearsUsage(t *testing.T) {
	t.Parallel()
	m := newTestMapper()

	raw := mustMarshal(map[string]any{
		"sessionUpdate": "usage_update",
		"inputTokens":   500,
	})
	m.updateUsage(context.Background(), raw)
	require.Equal(t, 500, m.LastUsage().InputTokens)

	m.Reset()
	require.Equal(t, 0, m.LastUsage().InputTokens)
}

func TestReset_ClearsTurnState(t *testing.T) {
	t.Parallel()
	m := newTestMapper()
	m.msgActive.Store(true)
	m.turnActive.Store(true)

	m.Reset()
	require.False(t, m.msgActive.Load())
	require.False(t, m.turnActive.Load())
}

// ─── buildStats includes usage_update data ───────────────────────────────────

func TestBuildStats_IncludesUsageSnapshot(t *testing.T) {
	t.Parallel()
	m := newTestMapper()

	// Feed usage_update data.
	raw := mustMarshal(map[string]any{
		"sessionUpdate": "usage_update",
		"contextSize":   128000,
		"contextUsed":   25000,
		"cost":          map[string]any{"amount": 0.42, "currency": "USD"},
	})
	m.updateUsage(context.Background(), raw)

	// Build stats from a prompt result.
	stats := m.buildStats(&PromptResult{
		StopReason: "end_turn",
		Usage:      PromptUsage{InputTokens: 100, OutputTokens: 50, TotalTokens: 150},
	})

	require.Equal(t, 128000, stats["context_size"])
	require.Equal(t, 25000, stats["context_used"])
	// Cost stored as float64 ("total_cost_usd") for gateway compatibility.
	require.InDelta(t, 0.42, stats["total_cost_usd"], 0.001)

	// Token data nested in "usage" map (Claude Code format).
	usage, ok := stats["usage"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, 100, usage["input_tokens"])
	require.Equal(t, 50, usage["output_tokens"])
	require.Equal(t, 150, usage["total_tokens"])
}

func TestBuildStats_NoUsageSnapshot(t *testing.T) {
	t.Parallel()
	m := newTestMapper()

	stats := m.buildStats(&PromptResult{
		StopReason: "end_turn",
		Usage:      PromptUsage{InputTokens: 100},
	})

	require.Nil(t, stats["context_size"])
	require.Nil(t, stats["context_used"])
	require.Nil(t, stats["total_cost_usd"])
	// usage map still present with token data.
	usage, ok := stats["usage"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, 100, usage["input_tokens"])
}

// ─── MapNotification routes usage_update ──────────────────────────────────────

func TestMapNotification_UsageUpdate_NoEnvelopes(t *testing.T) {
	t.Parallel()
	m := newTestMapper()

	notif := &JSONRPCNotification{
		JSONRPC: "2.0",
		Method:  "session/update",
		Params: mustMarshal(map[string]any{
			"sessionId": "s1",
			"update": map[string]any{
				"sessionUpdate": "usage_update",
				"inputTokens":   100,
			},
		}),
	}

	envs := m.MapNotification(context.Background(), notif)
	require.Nil(t, envs) // usage_update returns nil envelopes, data is internal.

	// But usage was accumulated.
	require.Equal(t, 100, m.LastUsage().InputTokens)
}
