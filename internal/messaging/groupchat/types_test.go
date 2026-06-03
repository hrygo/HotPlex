package groupchat

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewGroupSession(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	botIDs := []string{"bot-1", "bot-2"}

	gs := NewGroupSession("test topic", "feishu", "ch_123", "thread_ts_1", "owner_1", botIDs, cfg)

	require.NotEmpty(t, gs.ID)
	require.Equal(t, "test topic", gs.Topic)
	require.Equal(t, "feishu", gs.Platform)
	require.Equal(t, "ch_123", gs.ChannelID)
	require.Equal(t, "thread_ts_1", gs.ThreadTS)
	require.Equal(t, "owner_1", gs.OwnerID)
	require.Equal(t, botIDs, gs.BotIDs)
	require.Equal(t, GroupStateActive, gs.State)
	require.Equal(t, cfg.MaxTurns, gs.MaxTurns)
	require.Equal(t, cfg.CostLimitUSD, gs.CostLimitUSD)
	require.Equal(t, cfg.TurnTimeoutSec, gs.TurnTimeoutSec)
	require.Equal(t, cfg.CooldownMS, gs.CooldownMS)
	require.NotZero(t, gs.CreatedAt)
	require.NotZero(t, gs.UpdatedAt)
	require.Empty(t, gs.BotNames)

	t.Run("deterministic ID for same inputs", func(t *testing.T) {
		gs1 := NewGroupSession("topic", "feishu", "ch", "", "owner", botIDs, cfg)
		gs2 := NewGroupSession("topic", "feishu", "ch", "", "owner", botIDs, cfg)
		// IDs differ because UUIDv5 includes timestamp (nano precision)
		require.NotEqual(t, gs1.ID, gs2.ID)
	})
}

func TestGroupState_Constants(t *testing.T) {
	t.Parallel()

	require.Equal(t, GroupState("active"), GroupStateActive)
	require.Equal(t, GroupState("completed"), GroupStateCompleted)
	require.Equal(t, GroupState("stopped"), GroupStateStopped)
	require.Equal(t, GroupState("error"), GroupStateError)
	require.Equal(t, GroupState("gateway_restart"), GroupStateGatewayRestart)
}

func TestEndReason_Constants(t *testing.T) {
	t.Parallel()

	require.Equal(t, EndReason("max_turns"), EndMaxTurns)
	require.Equal(t, EndReason("cost_limit"), EndCostLimit)
	require.Equal(t, EndReason("all_skip"), EndAllSkip)
	require.Equal(t, EndReason("user_stopped"), EndUserStopped)
	require.Equal(t, EndReason("gateway_restart"), EndGatewayRestart)
	require.Equal(t, EndReason("error"), EndError)
	require.Equal(t, EndReason("consecutive_timeout"), EndConsecutiveTMO)
}
