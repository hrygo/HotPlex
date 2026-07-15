package execution

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/pkg/events"
)

func fastLeaseConfig() LeaseConfig {
	return LeaseConfig{
		RenewInterval:   50 * time.Millisecond,
		RecoverInterval: 50 * time.Millisecond,
		ShutdownTimeout: time.Second,
	}
}

func newLeaseTestStore(t *testing.T) (*SQLStore, *session.SQLiteStore) {
	t.Helper()
	return newTestSQLStore(t)
}

func TestLeaseManager_RenewExtendsLease(t *testing.T) {
	t.Parallel()
	store, sessionStore := newLeaseTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	now := time.Now()
	require.NoError(t, sessionStore.Upsert(ctx, &session.SessionInfo{
		ID: "session-lr", UserID: "u1", WorkerType: "claude_code",
		State: events.StateRunning, CreatedAt: now, UpdatedAt: now,
	}))

	rec, _, err := store.Accept(ctx, AcceptRequest{
		SessionID: "session-lr", ClientMessageID: "msg-lr", PayloadHash: "h",
		OwnerInstanceID: testOwner, WorkerRunID: testRun,
	})
	require.NoError(t, err)
	originalLease := rec.LeaseUntil

	mgr := NewLeaseManager(store, testOwner, fastLeaseConfig(), nil)
	mgr.Start(ctx)
	defer func() { _ = mgr.Shutdown(context.Background()) }()

	require.Eventually(t, func() bool {
		r, err := store.getByID(context.Background(), rec.ExecutionID)
		if err != nil {
			return false
		}
		return r.LeaseUntil > originalLease
	}, 2*time.Second, 10*time.Millisecond, "lease must be renewed beyond original value")
}

func TestLeaseManager_NoActiveExecutionsNoop(t *testing.T) {
	t.Parallel()
	store, _ := newLeaseTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgr := NewLeaseManager(store, testOwner, fastLeaseConfig(), nil)
	mgr.Start(ctx)

	// Confirm the renew loop stays a no-op while there are no active executions.
	require.Eventually(t, func() bool {
		renewed, err := store.RenewLeases(context.Background(), testOwner, 60, nil)
		return err == nil && renewed == 0
	}, 2*time.Second, 10*time.Millisecond, "no active executions should produce no renew writes")

	_ = mgr.Shutdown(context.Background())
}

func TestLeaseManager_RecoverExpiredLeases(t *testing.T) {
	t.Parallel()
	store, sessionStore := newLeaseTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	now := time.Now()
	require.NoError(t, sessionStore.Upsert(ctx, &session.SessionInfo{
		ID: "session-lrec", UserID: "u1", WorkerType: "claude_code",
		State: events.StateRunning, CreatedAt: now, UpdatedAt: now,
	}))

	rec, _, err := store.Accept(ctx, AcceptRequest{
		SessionID: "session-lrec", ClientMessageID: "msg-lrec", PayloadHash: "h",
		OwnerInstanceID: "gw-crashed", WorkerRunID: "run-old",
	})
	require.NoError(t, err)

	_, err = store.db.ExecContext(ctx, `UPDATE execution_inputs SET lease_until = ? WHERE execution_id = ?`,
		time.Now().Add(-time.Minute).UnixMilli(), rec.ExecutionID)
	require.NoError(t, err)

	repairer := NewRepairer(store, fastRepairConfig(), nil)
	repairer.abandoned[rec.ExecutionID] = struct{}{}
	mgr := NewLeaseManager(store, "gw-new", fastLeaseConfig(), nil, repairer)
	mgr.Start(ctx)
	defer func() { _ = mgr.Shutdown(context.Background()) }()

	require.Eventually(t, func() bool {
		r, err := store.getByID(context.Background(), rec.ExecutionID)
		if err != nil {
			return false
		}
		return r.RuntimeStatus == RuntimeUnknown && r.FenceReason == "GATEWAY_LEASE_EXPIRED"
	}, 2*time.Second, 10*time.Millisecond, "expired lease must be recovered to unknown+fenced")
	require.Eventually(t, func() bool {
		return len(repairer.AbandonedExecutionIDs()) == 0
	}, 2*time.Second, 10*time.Millisecond, "recovered executions must be removed from renewal exclusions")
}

func TestLeaseManager_ShutdownTerminatesOwnLeases(t *testing.T) {
	t.Parallel()
	store, sessionStore := newLeaseTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	now := time.Now()
	require.NoError(t, sessionStore.Upsert(ctx, &session.SessionInfo{
		ID: "session-ls", UserID: "u1", WorkerType: "claude_code",
		State: events.StateRunning, CreatedAt: now, UpdatedAt: now,
	}))

	rec, _, err := store.Accept(ctx, AcceptRequest{
		SessionID: "session-ls", ClientMessageID: "msg-ls", PayloadHash: "h",
		OwnerInstanceID: testOwner, WorkerRunID: testRun,
	})
	require.NoError(t, err)
	require.NoError(t, store.MarkRunning(ctx, rec.ExecutionID, testOwner, testRun))

	mgr := NewLeaseManager(store, testOwner, fastLeaseConfig(), nil)
	mgr.Start(ctx)

	require.NoError(t, mgr.Shutdown(context.Background()))

	stored, err := store.getByID(context.Background(), rec.ExecutionID)
	require.NoError(t, err)
	require.Equal(t, RuntimeUnknown, stored.RuntimeStatus)
	require.Equal(t, "GATEWAY_SHUTDOWN", stored.FenceReason)
	require.NotNil(t, stored.FinishedAt)
}

func TestLeaseManager_ShutdownCompletesWithinTimeout(t *testing.T) {
	t.Parallel()
	store, _ := newLeaseTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := fastLeaseConfig()
	cfg.ShutdownTimeout = 200 * time.Millisecond

	mgr := NewLeaseManager(store, testOwner, cfg, nil)
	mgr.Start(ctx)

	start := time.Now()
	require.NoError(t, mgr.Shutdown(context.Background()))
	elapsed := time.Since(start)
	require.Less(t, elapsed, 500*time.Millisecond, "shutdown must complete well within budget")
}
