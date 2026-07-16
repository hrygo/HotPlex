package gateway

import (
	"strings"
	"sync/atomic"
	"time"

	"github.com/hrygo/hotplex/pkg/events"
)

// sessionAccumulator tracks session-level statistics across turns.
// One instance per session, stored in Bridge.accum.
type sessionAccumulator struct {
	Generation      atomic.Int64 // session reset generation (monotonic), atomic for concurrent access
	AppliedResetGen atomic.Int64 // last worker resetGen applied to Generation (idempotent guard)
	TurnCount       atomic.Int32 // generation-scoped turn counter (atomic for concurrent access)
	ToolCallCount   atomic.Int32
	genInitialized  atomic.Bool // CAS guard: only one goroutine initializes Generation from DB
	TotalCostUSD    float64
	TotalInput      int64 // cumulative input tokens consumed across turns
	TotalOutput     int64
	ContextWindow   int64  // from modelUsage.contextWindow or get_context_usage.maxTokens (0 = unknown)
	ContextFill     int64  // context window fill from get_context_usage control channel (0 if unavailable)
	ModelName       string // first model seen
	StartedAt       time.Time
	WorkDir         string // session working directory
	GitBranch       string // current git branch (captured once at start)

	// Cache token tracking (cumulative across turns).
	TotalCacheWrite   int64
	TotalCacheRead    int64
	PrevCacheWrite    int64
	PrevCacheRead     int64
	PerTurnCacheWrite int64
	PerTurnCacheRead  int64

	// Per-turn tracking (reset after each done).
	ToolNames      map[string]int // tool name -> call count this turn
	PerTurnInput   int64
	PerTurnOutput  int64
	PerTurnCost    float64
	TurnDurationMs int64 // current turn duration in milliseconds

	// turnStartUnixMilli is the wall-clock start of the CURRENT turn, recorded
	// by the input path when a turn is about to be delivered to the worker and
	// consumed by the forwarder on Done. It excludes inter-turn idle time, which
	// the previous "reset clock at Done" approach incorrectly billed to the
	// next turn (Turn-Integrity spec RC-4 / Fix D). 0 = not set this turn.
	turnStartUnixMilli atomic.Int64

	// Cumulative totals at the end of the previous turn (for delta computation).
	PrevTotalIn   int64
	PrevTotalOut  int64
	PrevTotalCost float64
}

// mergePerTurnStats handles both Claude Code and OpenCode worker stat formats.
func (a *sessionAccumulator) mergePerTurnStats(data events.DoneData) {
	if data.Stats == nil {
		return
	}

	// Claude Code format: input_tokens, cache_creation_input_tokens, and
	// cache_read_input_tokens are separate additive fields (Anthropic API).
	// Total input = input_tokens + cache_creation_input_tokens + cache_read_input_tokens.
	if usage, ok := data.Stats["usage"].(map[string]any); ok {
		inputTokens := events.ToInt64(usage["input_tokens"])
		if inputTokens == 0 {
			inputTokens = events.ToInt64(usage["inputTokens"])
		}
		cacheWrite := events.ToInt64(usage["cache_creation_input_tokens"])
		if cacheWrite == 0 {
			cacheWrite = events.ToInt64(usage["cacheCreationInputTokens"])
		}
		cacheRead := events.ToInt64(usage["cache_read_input_tokens"])
		if cacheRead == 0 {
			cacheRead = events.ToInt64(usage["cacheReadInputTokens"])
		}
		outputTokens := events.ToInt64(usage["output_tokens"])
		if outputTokens == 0 {
			outputTokens = events.ToInt64(usage["outputTokens"])
		}

		a.TotalInput += inputTokens + cacheWrite + cacheRead
		a.TotalOutput += outputTokens
		a.TotalCacheWrite += cacheWrite
		a.TotalCacheRead += cacheRead
	} else if tokens, ok := data.Stats["tokens"].(map[string]any); ok {
		// OpenCode format: input/cache_read/cache_write are separate additive fields.
		input := events.ToInt64(tokens["input"])
		output := events.ToInt64(tokens["output"])
		cacheRead := events.ToInt64(tokens["cache_read"])
		if cacheRead == 0 {
			cacheRead = events.ToInt64(tokens["cacheRead"])
		}
		cacheWrite := events.ToInt64(tokens["cache_write"])
		if cacheWrite == 0 {
			cacheWrite = events.ToInt64(tokens["cacheWrite"])
		}

		a.TotalInput += input + cacheRead + cacheWrite
		a.TotalOutput += output
		a.TotalCacheWrite += cacheWrite
		a.TotalCacheRead += cacheRead
	}

	// Claude Code modelUsage: extract model name + contextWindow
	if modelUsage, ok := data.Stats["model_usage"].(map[string]any); ok {
		for modelName, v := range modelUsage {
			mu, ok := v.(map[string]any)
			if !ok {
				continue
			}
			if a.ModelName == "" {
				a.ModelName = shortModelName(modelName)
			}
			if cw := events.ToInt64(mu["contextWindow"]); cw > 0 {
				a.ContextWindow = cw
			}
		}
	}

	// Cost: Claude Code uses "total_cost_usd", OpenCode uses "cost"
	a.TotalCostUSD += events.ToFloat64(data.Stats["total_cost_usd"])
	a.TotalCostUSD += events.ToFloat64(data.Stats["cost"])
}

// Workers report cumulative totals, so deltas are derived by subtracting
// the previous baseline.
func (a *sessionAccumulator) computePerTurnDeltas() {
	a.PerTurnInput = a.TotalInput - a.PrevTotalIn
	a.PerTurnOutput = a.TotalOutput - a.PrevTotalOut
	a.PerTurnCost = a.TotalCostUSD - a.PrevTotalCost
	a.PerTurnCacheWrite = a.TotalCacheWrite - a.PrevCacheWrite
	a.PerTurnCacheRead = a.TotalCacheRead - a.PrevCacheRead
	a.PerTurnInput = max(a.PerTurnInput, 0)
	a.PerTurnOutput = max(a.PerTurnOutput, 0)
	a.PerTurnCost = max(a.PerTurnCost, 0)
	a.PerTurnCacheWrite = max(a.PerTurnCacheWrite, 0)
	a.PerTurnCacheRead = max(a.PerTurnCacheRead, 0)
}

// resetPerTurn must be called after computePerTurnDeltas and the record is written.
func (a *sessionAccumulator) resetPerTurn() {
	a.PrevTotalIn = a.TotalInput
	a.PrevTotalOut = a.TotalOutput
	a.PrevTotalCost = a.TotalCostUSD
	a.PrevCacheWrite = a.TotalCacheWrite
	a.PrevCacheRead = a.TotalCacheRead
	a.ToolNames = nil
	a.ToolCallCount.Store(0)
	a.PerTurnInput = 0
	a.PerTurnOutput = 0
	a.PerTurnCost = 0
	a.PerTurnCacheWrite = 0
	a.PerTurnCacheRead = 0
	a.TurnDurationMs = 0
	a.ContextFill = 0
}

// recordTurnStart stamps the start of the current turn in unix milliseconds.
// Called by the input path right before delivering the user's input to the
// worker (Turn-Integrity Fix D).
func (a *sessionAccumulator) recordTurnStart(now time.Time) {
	a.turnStartUnixMilli.Store(now.UnixMilli())
}

// clearTurnStart clears the turn start stamp. Called when input delivery fails
// so a subsequent Done does not bill a turn that never started (Fix D).
func (a *sessionAccumulator) clearTurnStart() {
	a.turnStartUnixMilli.Store(0)
}

// consumeTurnStartMs atomically reads and clears the turn start stamp, returning
// it in unix milliseconds. Returns 0 (and leaves it cleared) when no start was
// recorded for this turn — the caller falls back to the first worker event time.
func (a *sessionAccumulator) consumeTurnStartMs() int64 {
	return a.turnStartUnixMilli.Swap(0)
}

// mergeContextUsage sets ContextFill, ContextWindow, and ModelName from the worker's
// get_context_usage control channel response. This is the sole source for ContextFill.
// Supports partial data: updates ModelName even when MaxTokens is 0 (OCS scenario).
func (a *sessionAccumulator) mergeContextUsage(cu *events.ContextUsageData) {
	if cu == nil {
		return
	}
	if cu.TotalTokens > 0 {
		a.ContextFill = int64(cu.TotalTokens)
	}
	if cu.MaxTokens > 0 {
		a.ContextWindow = int64(cu.MaxTokens)
	}
	if cu.Model != "" && a.ModelName == "" {
		a.ModelName = shortModelName(cu.Model)
	}
}

// computeContextPct returns context window usage percentage (0-100).
// Returns 0 if either ContextFill or ContextWindow is unset (control channel unavailable).
func (a *sessionAccumulator) computeContextPct() float64 {
	if a.ContextWindow <= 0 || a.ContextFill <= 0 {
		return 0
	}
	return float64(a.ContextFill) / float64(a.ContextWindow) * 100
}

// snapshot returns the current accumulator state as a map for injection into DoneData.Stats["_session"].
func (a *sessionAccumulator) snapshot() map[string]any {
	ctxPct := a.computeContextPct()
	elapsed := time.Since(a.StartedAt)
	var toolNames map[string]any
	if len(a.ToolNames) > 0 {
		toolNames = make(map[string]any, len(a.ToolNames))
		for k, v := range a.ToolNames {
			toolNames[k] = v
		}
	}
	return map[string]any{
		"turn_count":       a.TurnCount.Load(),
		"tool_call_count":  a.ToolCallCount.Load(),
		"duration":         elapsed.Round(time.Second).String(),
		"duration_seconds": elapsed.Seconds(),
		"total_input_tok":  a.TotalInput,
		"total_output_tok": a.TotalOutput,
		"context_fill":     a.ContextFill,
		"context_window":   a.ContextWindow,
		"context_pct":      ctxPct,
		"total_cost_usd":   a.TotalCostUSD,
		"model_name":       a.ModelName,
		"turn_duration_ms": a.TurnDurationMs,
		"turn_input_tok":   a.PerTurnInput,
		"turn_output_tok":  a.PerTurnOutput,
		"turn_cost_usd":    a.PerTurnCost,
		"tool_names":       toolNames,
		"work_dir":         a.WorkDir,
		"git_branch":       a.GitBranch,
	}
}

// asDoneData extracts DoneData from Event.Data, handling both the original typed
// struct and the map[string]any produced by events.Clone JSON round-tripping.
func asDoneData(data any) (events.DoneData, bool) {
	return events.DecodeAs[events.DoneData](data)
}

// asToolCallData extracts ToolCallData from Event.Data, handling both the original
// typed struct and the map[string]any produced by events.Clone JSON round-tripping.
func asToolCallData(data any) (events.ToolCallData, bool) {
	return events.DecodeAs[events.ToolCallData](data)
}

// shortModelName returns a human-readable model name.
func shortModelName(full string) string {
	switch {
	case strings.Contains(full, "opus"):
		return "Opus"
	case strings.Contains(full, "sonnet"):
		return "Sonnet"
	case strings.Contains(full, "haiku"):
		return "Haiku"
	default:
		return full
	}
}
