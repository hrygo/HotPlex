package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRestartHelperFailure_ReleasesArtifactsWhenOldGatewayIsAlive(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	leaseStore := newRestartLeaseStore(filepath.Join(dir, "gateway.restart"), time.Now, func(pid int) bool {
		return pid == 4321
	})
	receiptStore := newRestartReceiptStore(filepath.Join(dir, "receipt.json"))
	lease, err := leaseStore.Acquire(4321)
	require.NoError(t, err)
	require.NoError(t, leaseStore.Update(lease.RequestID, func(current *restartLease) error {
		current.Phase = restartLeaseHelperStarted
		current.HelperPID = 9876
		return nil
	}))
	require.NoError(t, receiptStore.Write(testGatewayRestartReceipt(lease.RequestID)))

	cause := errors.New("supervisor rejected restart")
	err = restartHelperFailure(leaseStore, receiptStore, lease.RequestID, 4321, cause)
	require.ErrorIs(t, err, cause)
	_, err = leaseStore.Read()
	require.ErrorIs(t, err, os.ErrNotExist)
	require.NoFileExists(t, receiptStore.path)
}

func TestRestartHelperFailure_RetainsArtifactsAfterOldGatewayExits(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	leaseStore := newRestartLeaseStore(filepath.Join(dir, "gateway.restart"), time.Now, func(int) bool { return false })
	receiptStore := newRestartReceiptStore(filepath.Join(dir, "receipt.json"))
	lease, err := leaseStore.Acquire(4321)
	require.NoError(t, err)
	require.NoError(t, leaseStore.Update(lease.RequestID, func(current *restartLease) error {
		current.Phase = restartLeaseWaitingForReady
		current.HelperPID = 9876
		return nil
	}))
	require.NoError(t, receiptStore.Write(testGatewayRestartReceipt(lease.RequestID)))

	cause := errors.New("new gateway failed to start")
	err = restartHelperFailure(leaseStore, receiptStore, lease.RequestID, 4321, cause)
	require.ErrorIs(t, err, cause)
	current, err := leaseStore.Read()
	require.NoError(t, err)
	require.Equal(t, lease.RequestID, current.RequestID)
	require.FileExists(t, receiptStore.path)
}

func testGatewayRestartReceipt(requestID string) *gatewayRestartReceipt {
	return &gatewayRestartReceipt{
		SchemaVersion: gatewayRestartReceiptSchemaVersion,
		RequestID:     requestID,
		Platform:      "feishu",
		BotName:       "ops",
		PlatformKey:   map[string]string{"chat_id": "oc_chat"},
		RequestedAt:   time.Now().UTC(),
		OldVersion:    "v1.42.1",
		OldPID:        4321,
	}
}
