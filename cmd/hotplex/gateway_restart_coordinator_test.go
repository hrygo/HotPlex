package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
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
		Actor:         "ou_actor",
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
	require.Equal(t, "ou_actor", receipt.Actor)
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

func TestGatewayRestartCoordinator_ConflictReplyIncludesRequestID(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	leaseStore := newRestartLeaseStore(filepath.Join(dir, "gateway.restart"), time.Now, func(int) bool { return true })
	lease, err := leaseStore.Acquire(1234)
	require.NoError(t, err)

	coordinator := &gatewayRestartCoordinator{
		leaseStore: leaseStore,
		allowFeishu: func(string, string) bool {
			return true
		},
		findInstance: func() (*gatewayInstance, error) {
			return &gatewayInstance{PID: 4321, Source: sourcePID}, nil
		},
	}
	var reply string
	err = coordinator.HandleGatewayCommand(context.Background(), messaging.GatewayCommand{Kind: messaging.GatewayCommandRestart}, messaging.GatewayRestartRequest{
		ActorID: "ou_operator",
		BotName: "ops",
		ChatID:  "oc_chat",
		Reply: func(_ context.Context, text string) error {
			reply = text
			return nil
		},
	})
	require.NoError(t, err)
	require.Contains(t, reply, lease.RequestID)
}

func TestGatewayRestartCoordinator_AuditCarriesFeishuActorAndState(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	dir := t.TempDir()
	coordinator := &gatewayRestartCoordinator{
		log:        logger,
		leaseStore: newRestartLeaseStore(filepath.Join(dir, "gateway.restart"), time.Now, func(int) bool { return true }),
		receipts:   newRestartReceiptStore(filepath.Join(dir, "receipt.json")),
		findInstance: func() (*gatewayInstance, error) {
			return &gatewayInstance{PID: 4321, Source: sourcePID}, nil
		},
		spawnHelper: func(*gatewayRestartTicket) (int, error) { return 9876, nil },
	}

	ticket, err := coordinator.Prepare(context.Background(), gatewayRestartRequest{
		Platform:    string(messaging.PlatformFeishu),
		Actor:       "ou_operator",
		BotName:     "ops",
		ChatID:      "oc_chat",
		PlatformKey: map[string]string{"chat_id": "oc_chat"},
	})
	require.NoError(t, err)
	require.NoError(t, coordinator.Commit(ticket))

	output := logs.String()
	for _, expected := range []string{
		`"action":"gateway.restart"`,
		`"source":"feishu"`,
		`"actor":"ou_operator"`,
		`"bot_name":"ops"`,
		`"chat_id":"oc_chat"`,
		`"result":"prepared"`,
		`"result":"helper_started"`,
		`"helper_pid":9876`,
		`"new_pid":0`,
		`"request_id":"` + ticket.RequestID + `"`,
	} {
		require.Truef(t, strings.Contains(output, expected), "missing audit field %s in %s", expected, output)
	}
}

func TestGatewayRestartCoordinator_RetriesPendingReceiptBeforeNewFeishuRestart(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	leaseStore := newRestartLeaseStore(filepath.Join(dir, "gateway.restart"), time.Now, func(int) bool { return true })
	receiptStore := newRestartReceiptStore(filepath.Join(dir, "receipt.json"))
	oldReceipt := testGatewayRestartReceipt("req_0123456789abcdef0123456789abcdef")
	require.NoError(t, receiptStore.Write(oldReceipt))
	var retried string
	coordinator := &gatewayRestartCoordinator{
		leaseStore: leaseStore,
		receipts:   receiptStore,
		findInstance: func() (*gatewayInstance, error) {
			return &gatewayInstance{PID: 4321, Source: sourcePID}, nil
		},
		retryReceipt: func(_ context.Context, receipt *gatewayRestartReceipt) error {
			retried = receipt.RequestID
			return nil
		},
	}

	ticket, err := coordinator.Prepare(context.Background(), gatewayRestartRequest{
		Platform:    string(messaging.PlatformFeishu),
		Actor:       "ou_operator",
		BotName:     "ops",
		ChatID:      "oc_new",
		PlatformKey: map[string]string{"chat_id": "oc_new"},
	})
	require.NoError(t, err)
	require.Equal(t, oldReceipt.RequestID, retried)
	require.NotEqual(t, oldReceipt.RequestID, ticket.RequestID)
	current, err := receiptStore.Read()
	require.NoError(t, err)
	require.Equal(t, ticket.RequestID, current.RequestID)
}

func TestGatewayRestartCoordinator_PendingReceiptFailureReleasesNewLease(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	leaseStore := newRestartLeaseStore(filepath.Join(dir, "gateway.restart"), time.Now, func(int) bool { return true })
	receiptStore := newRestartReceiptStore(filepath.Join(dir, "receipt.json"))
	oldReceipt := testGatewayRestartReceipt("req_0123456789abcdef0123456789abcdef")
	require.NoError(t, receiptStore.Write(oldReceipt))
	coordinator := &gatewayRestartCoordinator{
		leaseStore: leaseStore,
		receipts:   receiptStore,
		findInstance: func() (*gatewayInstance, error) {
			return &gatewayInstance{PID: 4321, Source: sourcePID}, nil
		},
		retryReceipt: func(context.Context, *gatewayRestartReceipt) error {
			return errors.New("feishu unavailable")
		},
	}

	_, err := coordinator.Prepare(context.Background(), gatewayRestartRequest{
		Platform:    string(messaging.PlatformFeishu),
		PlatformKey: map[string]string{"chat_id": "oc_new"},
	})
	var pending *restartReceiptPendingError
	require.ErrorAs(t, err, &pending)
	require.Equal(t, oldReceipt.RequestID, pending.RequestID)
	_, err = leaseStore.Read()
	require.ErrorIs(t, err, os.ErrNotExist)
	remaining, err := receiptStore.Read()
	require.NoError(t, err)
	require.Equal(t, oldReceipt.RequestID, remaining.RequestID)
}
