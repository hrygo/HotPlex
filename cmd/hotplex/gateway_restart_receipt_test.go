package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRestartReceiptStore_AtomicWriteAndPermissions(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "gateway.restart.receipt.json")
	store := newRestartReceiptStore(path)
	receipt := &gatewayRestartReceipt{
		SchemaVersion: gatewayRestartReceiptSchemaVersion,
		RequestID:     "req_0123456789abcdef0123456789abcdef",
		Platform:      "feishu",
		BotName:       "ops",
		PlatformKey:   map[string]string{"chat_id": "oc_chat", "thread_ts": "om_thread"},
		RequestedAt:   time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC),
		OldVersion:    "v1.42.1",
		OldPID:        1234,
	}

	require.NoError(t, store.Write(receipt))
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	got, err := store.Read()
	require.NoError(t, err)
	require.Equal(t, receipt, got)
	require.ErrorIs(t, store.Write(receipt), errRestartReceiptExists)
}

func TestRestartReceiptStore_TicketFencingAndComplete(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "gateway.restart.receipt.json")
	store := newRestartReceiptStore(path)
	receipt := &gatewayRestartReceipt{
		SchemaVersion: gatewayRestartReceiptSchemaVersion,
		RequestID:     "req_0123456789abcdef0123456789abcdef",
		Platform:      "feishu",
		PlatformKey:   map[string]string{"chat_id": "oc_chat"},
		RequestedAt:   time.Now().UTC(),
		OldVersion:    "v1.42.1",
		OldPID:        1234,
	}
	require.NoError(t, store.Write(receipt))

	require.ErrorIs(t, store.Complete("req_wrong"), errRestartReceiptTicketMismatch)
	require.NoError(t, store.Complete(receipt.RequestID))
	_, err := os.Stat(path)
	require.True(t, errors.Is(err, os.ErrNotExist))
}

func TestRestartReceiptStore_CorruptReceiptCanBeQuarantined(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "gateway.restart.receipt.json")
	store := newRestartReceiptStore(path)
	require.NoError(t, os.WriteFile(path, []byte(`{"schema_version":99}`), 0o600))

	_, err := store.Read()
	require.Error(t, err)
	quarantined, err := store.Quarantine()
	require.NoError(t, err)
	require.NotEmpty(t, quarantined)
	_, err = os.Stat(path)
	require.True(t, errors.Is(err, os.ErrNotExist))
	_, err = os.Stat(quarantined)
	require.NoError(t, err)
}
