package main

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/messaging"
	"github.com/hrygo/hotplex/internal/session"
)

func TestLifecycleBroadcast_ConfiguredFeishuOperatorReceivesStopping(t *testing.T) {
	t.Parallel()

	messages := make(chan map[string]string, 1)
	b, _ := newConfiguredFeishuLifecycleBroadcaster(t, "ou_operator", messages)

	summary := b.BroadcastStopping()

	require.Equal(t, lifecycleBroadcastSummary{
		Phase:       lifecyclePhaseStopping,
		TargetCount: 1,
		SentCount:   1,
	}, summary)
	require.Equal(t, map[string]string{"open_id": "ou_operator"}, <-messages)
}

func TestLifecycleBroadcast_ConfiguredFeishuOperatorReceivesStartedWithoutSnapshot(t *testing.T) {
	t.Parallel()

	messages := make(chan map[string]string, 1)
	b, _ := newConfiguredFeishuLifecycleBroadcaster(t, "ou_operator", messages)

	summary := b.BroadcastStarted()

	require.Equal(t, lifecycleBroadcastSummary{
		Phase:       lifecyclePhaseStarted,
		TargetCount: 1,
		SentCount:   1,
	}, summary)
	require.Equal(t, map[string]string{"open_id": "ou_operator"}, <-messages)
}

func TestLifecycleBroadcast_UsesHotReloadedFeishuOperators(t *testing.T) {
	t.Parallel()

	messages := make(chan map[string]string, 1)
	b, store := newConfiguredFeishuLifecycleBroadcaster(t, "ou_old", messages)
	next := configuredFeishuLifecycleConfig("ou_new")
	store.Swap(next)

	summary := b.BroadcastStopping()

	require.Equal(t, 1, summary.SentCount)
	require.Equal(t, map[string]string{"open_id": "ou_new"}, <-messages)
}

func TestLifecycleBroadcast_RestartReceiptSuppressesConfiguredOperatorDuplicate(t *testing.T) {
	t.Parallel()

	messages := make(chan map[string]string, 2)
	b, _ := newConfiguredFeishuLifecycleBroadcaster(t, "ou_operator", messages)
	receipt := testGatewayRestartReceipt("req_0123456789abcdef0123456789abcdef")
	receipt.BotName = "ops"
	receipt.PlatformKey = map[string]string{"chat_id": "oc_request_chat", "message_id": "om_request"}
	require.NoError(t, b.receipts.Write(receipt))

	stopping := b.BroadcastStopping()
	started := b.BroadcastStarted()

	require.Equal(t, 1, stopping.SentCount)
	require.Equal(t, 1, started.SentCount)
	for range 2 {
		require.Equal(t, "oc_request_chat", (<-messages)["chat_id"])
	}
	select {
	case duplicate := <-messages:
		require.Failf(t, "duplicate configured operator notification", "got platform key %#v", duplicate)
	default:
	}
}

func TestLifecycleBroadcast_ConfiguredFeishuTargetsInheritOverrideAndDeduplicate(t *testing.T) {
	t.Parallel()

	cfg := configuredFeishuLifecycleConfig(" ou_platform ")
	cfg.Messaging.Feishu.GatewayRestartAllowFrom = []string{" ou_platform ", "ou_platform", ""}
	cfg.Messaging.Feishu.Bots = append(cfg.Messaging.Feishu.Bots,
		config.FeishuBotConfig{Name: "disabled", GatewayRestartAllowFrom: []string{}},
		config.FeishuBotConfig{Name: "dedicated", GatewayRestartAllowFrom: []string{"ou_dedicated"}},
	)
	b := &lifecycleBroadcaster{config: cfg}

	targets := b.configuredFeishuTargets()

	require.Len(t, targets, 2)
	require.Equal(t, "ops", targets[0].BotName)
	require.Equal(t, map[string]string{"open_id": "ou_platform"}, targets[0].PlatformKey)
	require.Equal(t, "dedicated", targets[1].BotName)
	require.Equal(t, map[string]string{"open_id": "ou_dedicated"}, targets[1].PlatformKey)
	for _, target := range targets {
		require.NotContains(t, target.ID, target.PlatformKey["open_id"])
		_, ok := lifecycleTargetKey(target)
		require.True(t, ok)
	}
}

func newConfiguredFeishuLifecycleBroadcaster(
	t *testing.T,
	openID string,
	messages chan<- map[string]string,
) (*lifecycleBroadcaster, *config.ConfigStore) {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := configuredFeishuLifecycleConfig(openID)
	store := config.NewConfigStore(cfg, logger)
	adapter := &lifecycleFakeAdapter{
		platform: messaging.PlatformFeishu,
		botName:  "ops",
		botID:    "bot-1",
		send: func(_ context.Context, _ string, platformKey map[string]string) error {
			messages <- platformKey
			return nil
		},
	}
	b := newLifecycleBroadcaster(&GatewayDeps{
		Log:         logger,
		Config:      cfg,
		ConfigStore: store,
	})
	b.sessions = &lifecycleFakeSessions{byID: make(map[string]*session.SessionInfo)}
	b.connections = &lifecycleFakeConnections{active: make(map[string]bool)}
	b.bots = &lifecycleFakeBotRegistry{entries: []*messaging.BotEntry{{
		Name:     "ops",
		Platform: messaging.PlatformFeishu,
		BotID:    "bot-1",
		Status:   messaging.BotStatusRunning,
		Adapter:  adapter,
	}}}
	dir := t.TempDir()
	b.snapshots = newLifecycleSnapshotStore(filepath.Join(dir, "snapshot.json"), 32, time.Now)
	b.receipts = newRestartReceiptStore(filepath.Join(dir, "receipt.json"))
	return b, store
}

func configuredFeishuLifecycleConfig(openID string) *config.Config {
	cfg := config.Default()
	cfg.Messaging.Feishu.Enabled = true
	cfg.Messaging.Feishu.GatewayRestartAllowFrom = []string{openID}
	cfg.Messaging.Feishu.Bots = []config.FeishuBotConfig{{Name: "ops"}}
	return cfg
}
