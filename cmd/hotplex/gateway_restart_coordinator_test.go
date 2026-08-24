package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/messaging"
)

func TestGatewayRestartCoordinator_PrepareFencesAndWritesFeishuReceipt(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	leaseStore := newRestartLeaseStore(filepath.Join(dir, "gateway.restart"), time.Now, func(int) bool { return true })
	receiptStore := newRestartReceiptStore(filepath.Join(dir, "gateway.restart.receipt.json"))
	coordinator := &gatewayRestartCoordinator{
		leaseStore: leaseStore,
		receipts:   receiptStore,
		findInstance: func() (*gatewayInstance, error) {
			return &gatewayInstance{PID: 4321, Source: sourcePID, ConfigPath: "/old/config.yaml", DevMode: true}, nil
		},
		configPath: "/requested/config.yaml",
	}

	ticket, err := coordinator.Prepare(context.Background(), gatewayRestartRequest{
		Platform:      string(messaging.PlatformFeishu),
		BotName:       "ops",
		PlatformKey:   map[string]string{"chat_id": "oc_chat", "message_id": "om_message"},
		ConfigChanged: true,
	})
	require.NoError(t, err)
	require.NotNil(t, ticket)
	require.Equal(t, "/requested/config.yaml", ticket.ConfigPath)
	require.True(t, ticket.DevMode)

	_, err = coordinator.Prepare(context.Background(), gatewayRestartRequest{Platform: "cli"})
	require.Error(t, err)
	require.True(t, errors.Is(err, errRestartLeaseInProgress))

	receipt, err := receiptStore.Read()
	require.NoError(t, err)
	require.Equal(t, ticket.RequestID, receipt.RequestID)
	require.Equal(t, "ops", receipt.BotName)
	data, err := json.Marshal(receipt)
	require.NoError(t, err)
	require.NotContains(t, string(data), "prompt")

	require.NoError(t, coordinator.Abort(ticket))
	_, err = leaseStore.Read()
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestGatewayRestartCoordinator_CommitFencesHelperPID(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	leaseStore := newRestartLeaseStore(filepath.Join(dir, "gateway.restart"), time.Now, func(int) bool { return true })
	coordinator := &gatewayRestartCoordinator{
		leaseStore: leaseStore,
		findInstance: func() (*gatewayInstance, error) {
			return &gatewayInstance{PID: 4321, Source: sourcePID}, nil
		},
		spawnHelper: func(*gatewayRestartTicket) (int, error) { return 9876, nil },
	}

	ticket, err := coordinator.Prepare(context.Background(), gatewayRestartRequest{})
	require.NoError(t, err)
	require.NoError(t, coordinator.Commit(ticket))

	lease, err := leaseStore.Read()
	require.NoError(t, err)
	require.Equal(t, restartLeaseHelperStarted, lease.Phase)
	require.Equal(t, 9876, lease.HelperPID)
}

func TestGatewayRestartCoordinator_CommitFailureAbortsLease(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	leaseStore := newRestartLeaseStore(filepath.Join(dir, "gateway.restart"), time.Now, func(int) bool { return true })
	coordinator := &gatewayRestartCoordinator{
		leaseStore: leaseStore,
		findInstance: func() (*gatewayInstance, error) {
			return &gatewayInstance{PID: 4321, Source: sourcePID}, nil
		},
		spawnHelper: func(*gatewayRestartTicket) (int, error) { return 0, errors.New("spawn failed") },
	}

	ticket, err := coordinator.Prepare(context.Background(), gatewayRestartRequest{})
	require.NoError(t, err)
	require.Error(t, coordinator.Commit(ticket))
	_, err = leaseStore.Read()
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestGatewayRestartCoordinator_CompleteReadyReleasesWaitingLease(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	leaseStore := newRestartLeaseStore(filepath.Join(dir, "gateway.restart"), time.Now, func(int) bool { return true })
	lease, err := leaseStore.Acquire(1234)
	require.NoError(t, err)
	require.NoError(t, leaseStore.Update(lease.RequestID, func(current *restartLease) error {
		current.Phase = restartLeaseWaitingForReady
		current.HelperPID = 5678
		return nil
	}))
	coordinator := &gatewayRestartCoordinator{leaseStore: leaseStore}
	require.NoError(t, coordinator.CompleteReady())
	_, err = leaseStore.Read()
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestGatewayRestartCoordinator_FeishuAllowlistReadsHotConfig(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.Messaging.Feishu.GatewayRestartAllowFrom = []string{"ou_platform"}
	cfg.Messaging.Feishu.Bots = []config.FeishuBotConfig{{Name: "ops", GatewayRestartAllowFrom: []string{"ou_bot"}}}
	store := config.NewConfigStore(cfg, nil)
	coordinator := &gatewayRestartCoordinator{configStore: store}

	require.True(t, coordinator.feishuRestartAllowed("other", "ou_platform"))
	require.False(t, coordinator.feishuRestartAllowed("ops", "ou_platform"))
	require.True(t, coordinator.feishuRestartAllowed("ops", "ou_bot"))

	next := config.Default()
	next.Messaging.Feishu.GatewayRestartAllowFrom = []string{"ou_new"}
	store.Swap(next)
	require.False(t, coordinator.feishuRestartAllowed("other", "ou_platform"))
	require.True(t, coordinator.feishuRestartAllowed("other", "ou_new"))
}
