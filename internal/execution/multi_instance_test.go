package execution

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/pkg/events"
)

func TestMultiInstance_StartupDoesNotTouchOtherInstancesRecords(t *testing.T) {
	t.Parallel()
	storeA, sessionStore := newTestSQLStore(t)
	ctx := context.Background()

	now := time.Now()
	require.NoError(t, sessionStore.Upsert(ctx, &session.SessionInfo{
		ID: "session-multi", UserID: "user-1", WorkerType: "claude_code",
		State: events.StateRunning, CreatedAt: now, UpdatedAt: now,
	}))

	rec, _, err := storeA.Accept(ctx, AcceptRequest{
		SessionID:       "session-multi",
		ClientMessageID: "msg-multi",
		PayloadHash:     "h-multi",
		OwnerInstanceID: "gw-A",
		WorkerRunID:     "run-A",
	})
	require.NoError(t, err)
	require.Equal(t, RuntimePending, rec.RuntimeStatus)
	require.NoError(t, storeA.MarkRunning(ctx, rec.ExecutionID, "gw-A", "run-A"))

	storeB, err := NewSQLStore(ctx, sessionStore.DB(), storeA.dialect, storeA.writeMu, nil)
	require.NoError(t, err)

	recovered, err := storeB.RecoverExpiredLeases(ctx, nil)
	require.NoError(t, err)
	require.Equal(t, int64(0), recovered.Recovered, "instance B must not recover A's unexpired lease")

	stored, err := storeA.getByID(ctx, rec.ExecutionID)
	require.NoError(t, err)
	require.Equal(t, RuntimeRunning, stored.RuntimeStatus, "A's record must be untouched")
	require.Equal(t, "gw-A", stored.OwnerInstanceID)
	require.Empty(t, stored.FenceReason)
}

func TestMultiInstance_ExpiredLeaseRecoveredExactlyOnce(t *testing.T) {
	t.Parallel()
	storeA, sessionStore := newTestSQLStore(t)
	ctx := context.Background()

	now := time.Now()
	require.NoError(t, sessionStore.Upsert(ctx, &session.SessionInfo{
		ID: "session-exp", UserID: "user-1", WorkerType: "claude_code",
		State: events.StateRunning, CreatedAt: now, UpdatedAt: now,
	}))

	rec, _, err := storeA.Accept(ctx, AcceptRequest{
		SessionID:       "session-exp",
		ClientMessageID: "msg-exp",
		PayloadHash:     "h-exp",
		OwnerInstanceID: "gw-A",
		WorkerRunID:     "run-A",
	})
	require.NoError(t, err)

	storeB, err := NewSQLStore(ctx, sessionStore.DB(), storeA.dialect, storeA.writeMu, nil)
	require.NoError(t, err)

	_, err = storeA.db.ExecContext(ctx, `UPDATE execution_inputs SET lease_until = 0 WHERE execution_id = ?`, rec.ExecutionID)
	require.NoError(t, err)
	recoveredB, err := storeB.RecoverExpiredLeases(ctx, nil)
	require.NoError(t, err)
	require.Equal(t, int64(1), recoveredB.Recovered)

	recoveredA, err := storeA.RecoverExpiredLeases(ctx, nil)
	require.NoError(t, err)
	require.Equal(t, int64(0), recoveredA.Recovered, "already recovered by B — must not double-recover")

	stored, err := storeA.getByID(ctx, rec.ExecutionID)
	require.NoError(t, err)
	require.Equal(t, RuntimeUnknown, stored.RuntimeStatus)
	require.Equal(t, "GATEWAY_LEASE_EXPIRED", stored.FenceReason)
}

func TestMultiInstance_RenewLeasesOnlyAffectsOwnOwner(t *testing.T) {
	t.Parallel()
	storeA, sessionStore := newTestSQLStore(t)
	ctx := context.Background()

	now := time.Now()
	require.NoError(t, sessionStore.Upsert(ctx, &session.SessionInfo{
		ID: "session-ren", UserID: "user-1", WorkerType: "claude_code",
		State: events.StateRunning, CreatedAt: now, UpdatedAt: now,
	}))

	rec, _, err := storeA.Accept(ctx, AcceptRequest{
		SessionID:       "session-ren",
		ClientMessageID: "msg-ren",
		PayloadHash:     "h-ren",
		OwnerInstanceID: "gw-A",
		WorkerRunID:     "run-A",
	})
	require.NoError(t, err)

	storeB, err := NewSQLStore(ctx, sessionStore.DB(), storeA.dialect, storeA.writeMu, nil)
	require.NoError(t, err)

	renewedByB, err := storeB.RenewLeases(ctx, "gw-B", 120, nil)
	require.NoError(t, err)
	require.Equal(t, int64(0), renewedByB, "B must not renew A's leases")

	renewedByA, err := storeA.RenewLeases(ctx, "gw-A", 120, nil)
	require.NoError(t, err)
	require.Equal(t, int64(1), renewedByA)

	stored, err := storeA.getByID(ctx, rec.ExecutionID)
	require.NoError(t, err)
	require.Greater(t, stored.LeaseUntil, rec.LeaseUntil, "A's renew must extend lease")
}
