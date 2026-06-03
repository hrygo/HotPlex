package groupchat

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTerminateCheck_ShouldTerminate(t *testing.T) {
	t.Parallel()

	t.Run("context cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		g := &TerminateCheck{MaxTurns: 10, CostLimitUSD: 1.0}
		gs := &GroupSession{TurnCount: 0, CostAccumulated: 0, BotIDs: []string{"a", "b"}}
		reason := g.ShouldTerminate(ctx, gs, nil)
		require.Equal(t, EndUserStopped, reason)
	})

	t.Run("max turns reached", func(t *testing.T) {
		g := &TerminateCheck{MaxTurns: 3, CostLimitUSD: 10}
		gs := &GroupSession{TurnCount: 3, CostAccumulated: 0, BotIDs: []string{"a", "b"}}
		reason := g.ShouldTerminate(context.Background(), gs, nil)
		require.Equal(t, EndMaxTurns, reason)
	})

	t.Run("max turns not reached", func(t *testing.T) {
		g := &TerminateCheck{MaxTurns: 10, CostLimitUSD: 10}
		gs := &GroupSession{TurnCount: 5, CostAccumulated: 0, BotIDs: []string{"a", "b"}}
		reason := g.ShouldTerminate(context.Background(), gs, nil)
		require.Equal(t, EndReason(""), reason)
	})

	t.Run("cost limit exceeded", func(t *testing.T) {
		g := &TerminateCheck{MaxTurns: 100, CostLimitUSD: 1.0}
		gs := &GroupSession{TurnCount: 1, CostAccumulated: 1.5, BotIDs: []string{"a", "b"}}
		reason := g.ShouldTerminate(context.Background(), gs, nil)
		require.Equal(t, EndCostLimit, reason)
	})

	t.Run("cost limit exactly met", func(t *testing.T) {
		g := &TerminateCheck{MaxTurns: 100, CostLimitUSD: 1.0}
		gs := &GroupSession{TurnCount: 1, CostAccumulated: 1.0, BotIDs: []string{"a", "b"}}
		reason := g.ShouldTerminate(context.Background(), gs, nil)
		require.Equal(t, EndCostLimit, reason)
	})

	t.Run("all skipped", func(t *testing.T) {
		g := &TerminateCheck{MaxTurns: 100, CostLimitUSD: 10}
		gs := &GroupSession{TurnCount: 4, CostAccumulated: 0, BotIDs: []string{"a", "b"}}
		turns := []*TurnRecord{
			{BotID: "a", Skipped: true},
			{BotID: "b", Skipped: true},
			{BotID: "a", Skipped: true},
			{BotID: "b", Skipped: true},
		}
		reason := g.ShouldTerminate(context.Background(), gs, turns)
		require.Equal(t, EndAllSkip, reason)
	})

	t.Run("not all skipped", func(t *testing.T) {
		g := &TerminateCheck{MaxTurns: 100, CostLimitUSD: 10}
		gs := &GroupSession{TurnCount: 4, CostAccumulated: 0, BotIDs: []string{"a", "b"}}
		turns := []*TurnRecord{
			{BotID: "a", Skipped: true},
			{BotID: "b", Skipped: true},
			{BotID: "a", Skipped: false}, // last 2: one not skipped
			{BotID: "b", Skipped: true},
		}
		reason := g.ShouldTerminate(context.Background(), gs, turns)
		require.Equal(t, EndReason(""), reason)
	})

	t.Run("insufficient turns for all-skip check", func(t *testing.T) {
		g := &TerminateCheck{MaxTurns: 100, CostLimitUSD: 10}
		gs := &GroupSession{TurnCount: 1, CostAccumulated: 0, BotIDs: []string{"a", "b"}}
		turns := []*TurnRecord{
			{BotID: "a", Skipped: true},
		}
		reason := g.ShouldTerminate(context.Background(), gs, turns)
		require.Equal(t, EndReason(""), reason)
	})

	t.Run("zero limits skip checks", func(t *testing.T) {
		g := &TerminateCheck{MaxTurns: 0, CostLimitUSD: 0}
		gs := &GroupSession{TurnCount: 999, CostAccumulated: 999, BotIDs: []string{"a"}}
		reason := g.ShouldTerminate(context.Background(), gs, nil)
		require.Equal(t, EndReason(""), reason)
	})
}

func TestTerminateCheck_ShouldEvictBot(t *testing.T) {
	t.Parallel()

	t.Run("no timeouts", func(t *testing.T) {
		g := &TerminateCheck{MaxConsecutiveTMO: 2}
		turns := []*TurnRecord{
			{BotID: "a", TimeoutCount: 0},
			{BotID: "a", TimeoutCount: 0},
		}
		require.False(t, g.ShouldEvictBot("a", turns))
	})

	t.Run("single timeout not enough", func(t *testing.T) {
		g := &TerminateCheck{MaxConsecutiveTMO: 2}
		turns := []*TurnRecord{
			{BotID: "a", TimeoutCount: 1},
		}
		require.False(t, g.ShouldEvictBot("a", turns))
	})

	t.Run("two consecutive timeouts triggers eviction", func(t *testing.T) {
		g := &TerminateCheck{MaxConsecutiveTMO: 2}
		turns := []*TurnRecord{
			{BotID: "a", TimeoutCount: 1},
			{BotID: "a", TimeoutCount: 1},
		}
		require.True(t, g.ShouldEvictBot("a", turns))
	})

	t.Run("interleaved bots skip non-target turns", func(t *testing.T) {
		g := &TerminateCheck{MaxConsecutiveTMO: 2}
		turns := []*TurnRecord{
			{BotID: "a", TimeoutCount: 1},
			{BotID: "b", TimeoutCount: 1},
			{BotID: "a", TimeoutCount: 1},
		}
		// ShouldEvictBot iterates backwards and skips non-target bot turns.
		// bot_a has 2 consecutive timeouts (turns[2] and turns[0]), ignoring bot_b in between.
		require.True(t, g.ShouldEvictBot("a", turns))
	})

	t.Run("non-consecutive resets counter", func(t *testing.T) {
		g := &TerminateCheck{MaxConsecutiveTMO: 2}
		turns := []*TurnRecord{
			{BotID: "a", TimeoutCount: 1},
			{BotID: "a", TimeoutCount: 0},
			{BotID: "a", TimeoutCount: 1},
		}
		require.False(t, g.ShouldEvictBot("a", turns))
	})

	t.Run("three consecutive with threshold 3", func(t *testing.T) {
		g := &TerminateCheck{MaxConsecutiveTMO: 3}
		turns := []*TurnRecord{
			{BotID: "a", TimeoutCount: 1},
			{BotID: "a", TimeoutCount: 1},
			{BotID: "a", TimeoutCount: 1},
		}
		require.True(t, g.ShouldEvictBot("a", turns))
	})

	t.Run("default threshold when zero", func(t *testing.T) {
		g := &TerminateCheck{MaxConsecutiveTMO: 0}
		turns := []*TurnRecord{
			{BotID: "a", TimeoutCount: 1},
			{BotID: "a", TimeoutCount: 1},
		}
		require.True(t, g.ShouldEvictBot("a", turns))
	})
}
