package session

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/events"
)

func ocsTerminatedInfo(id, remoteID string, now time.Time) *SessionInfo {
	return &SessionInfo{
		ID:              id,
		UserID:          "user-1",
		WorkerType:      worker.TypeOpenCodeSrv,
		WorkerSessionID: remoteID,
		State:           events.StateTerminated,
		CreatedAt:       now.Add(-time.Hour),
		UpdatedAt:       now.Add(-time.Hour),
	}
}

func TestSQLiteStore_RetentionCleanupTombstoneBlocksStaleUpsert(t *testing.T) {
	t.Parallel()
	store, _ := helperDB(t)
	ctx := context.Background()
	now := time.Now()
	info := ocsTerminatedInfo("sess-retention-race", "ocs-old", now)
	require.NoError(t, store.Upsert(ctx, info))

	deleted, err := store.DeleteTerminated(ctx, now, now)
	require.NoError(t, err)
	require.Len(t, deleted, 1)
	require.Equal(t, "ocs-old", deleted[0].WorkerSessionID)

	_, err = store.Get(ctx, info.ID)
	require.ErrorIs(t, err, ErrSessionCleanupPending)

	stale := *info
	stale.State = events.StateRunning
	stale.UpdatedAt = now.Add(time.Second)
	require.ErrorIs(t, store.Upsert(ctx, &stale), ErrSessionCleanupPending,
		"a resume snapshot must not recreate a retention-deleted OCS session")

	claimNow := time.Now().Add(time.Second)
	tasks, err := store.ClaimCleanupTasks(ctx, claimNow, claimNow.Add(time.Minute), 1)
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	require.Equal(t, info.ID, tasks[0].SessionID)
	require.Equal(t, "ocs-old", tasks[0].WorkerSessionID)
}

func TestSQLiteStore_MarkDeletedEnqueuesCleanupTask(t *testing.T) {
	t.Parallel()
	store, _ := helperDB(t)
	ctx := context.Background()
	now := time.Now()
	info := ocsTerminatedInfo("sess-logical-delete", "ocs-logical-delete", now)
	info.State = events.StateRunning
	require.NoError(t, store.Upsert(ctx, info))

	deleted := *info
	deleted.State = events.StateDeleted
	deleted.UpdatedAt = now.Add(time.Second)
	require.NoError(t, store.MarkDeletedWithCleanup(ctx, &deleted))

	stored, err := store.Get(ctx, info.ID)
	require.NoError(t, err)
	require.Equal(t, events.StateDeleted, stored.State)

	claimNow := time.Now().Add(time.Second)
	tasks, err := store.ClaimCleanupTasks(ctx, claimNow, claimNow.Add(time.Minute), 1)
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	require.Equal(t, "ocs-logical-delete", tasks[0].WorkerSessionID)
}

func TestCleanupRunner_RunReturnsOnCancellation(t *testing.T) {
	t.Parallel()
	store, _ := helperDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runner := NewCleanupRunner(nil, store, func(context.Context, worker.WorkerType, string) error { return nil })
	runner.Run(ctx)
	require.True(t, isCleanupPendingError(ErrSessionCleanupPending))
	require.True(t, containsCleanupPending("session cleanup pending"))
}

func TestSQLiteStore_DeletePhysicalWithCleanupNotFound(t *testing.T) {
	t.Parallel()
	store, _ := helperDB(t)
	deleted, err := store.DeletePhysicalWithCleanup(context.Background(), "missing-session")
	require.NoError(t, err)
	require.Nil(t, deleted)
}

func TestCleanupRunner_RetriesAndCompletesDurably(t *testing.T) {
	t.Parallel()
	store, _ := helperDB(t)
	ctx := context.Background()
	now := time.Now()
	info := ocsTerminatedInfo("sess-outbox-retry", "ocs-retry", now)
	require.NoError(t, store.Upsert(ctx, info))
	_, err := store.DeletePhysicalWithCleanup(ctx, info.ID)
	require.NoError(t, err)

	var calls atomic.Int32
	runner := NewCleanupRunner(nil, store, func(_ context.Context, workerType worker.WorkerType, remoteID string) error {
		require.Equal(t, worker.TypeOpenCodeSrv, workerType)
		require.Equal(t, "ocs-retry", remoteID)
		if calls.Add(1) == 1 {
			return errors.New("ocs temporarily unavailable")
		}
		return nil
	})
	base := time.Now().Add(time.Second)
	runner.now = func() time.Time { return base }
	runner.RunOnce(ctx)

	tasks, err := store.ClaimCleanupTasks(ctx, base, base.Add(time.Minute), 1)
	require.NoError(t, err)
	require.Empty(t, tasks, "failure must back off instead of losing or immediately re-leasing the task")

	runner.now = func() time.Time { return base.Add(2 * time.Second) }
	runner.RunOnce(ctx)
	require.EqualValues(t, 2, calls.Load())

	_, err = store.Get(ctx, info.ID)
	require.ErrorIs(t, err, ErrSessionNotFound)
	var count int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM session_cleanup_tasks WHERE session_id = ?`, info.ID).Scan(&count))
	require.Zero(t, count)
}

func TestCleanupRunner_ReclaimsExpiredLeaseAfterRestart(t *testing.T) {
	t.Parallel()
	store, _ := helperDB(t)
	ctx := context.Background()
	now := time.Now()
	info := ocsTerminatedInfo("sess-outbox-lease", "ocs-lease", now)
	require.NoError(t, store.Upsert(ctx, info))
	_, err := store.DeletePhysicalWithCleanup(ctx, info.ID)
	require.NoError(t, err)

	claimNow := time.Now().Add(time.Second)
	leaseUntil := claimNow.Add(time.Minute)
	leased, err := store.ClaimCleanupTasks(ctx, claimNow, leaseUntil, 1)
	require.NoError(t, err)
	require.Len(t, leased, 1)

	var calls atomic.Int32
	runner := NewCleanupRunner(nil, store, func(context.Context, worker.WorkerType, string) error {
		calls.Add(1)
		return nil
	})
	runner.now = func() time.Time { return leaseUntil.Add(time.Second) }
	runner.RunOnce(ctx)
	require.EqualValues(t, 1, calls.Load())
}

func TestCleanupTask_ExpiredLeaseCannotCompleteOrRetryNewLease(t *testing.T) {
	t.Parallel()
	store, _ := helperDB(t)
	ctx := context.Background()
	now := time.Now()
	info := ocsTerminatedInfo("sess-outbox-fence", "ocs-fence", now)
	require.NoError(t, store.Upsert(ctx, info))
	_, err := store.DeletePhysicalWithCleanup(ctx, info.ID)
	require.NoError(t, err)

	firstNow := time.Now().Add(time.Second)
	firstLeaseUntil := firstNow.Add(time.Minute)
	first, err := store.ClaimCleanupTasks(ctx, firstNow, firstLeaseUntil, 1)
	require.NoError(t, err)
	require.Len(t, first, 1)

	secondNow := firstLeaseUntil.Add(time.Second)
	second, err := store.ClaimCleanupTasks(ctx, secondNow, secondNow.Add(time.Minute), 1)
	require.NoError(t, err)
	require.Len(t, second, 1)
	require.NotEqual(t, first[0].LeaseToken, second[0].LeaseToken)

	require.ErrorIs(t, store.CompleteCleanupTask(ctx, first[0].ID, first[0].LeaseToken), ErrCleanupLeaseLost)
	require.ErrorIs(t, store.RetryCleanupTask(ctx, first[0].ID, first[0].LeaseToken, secondNow.Add(time.Minute), "late failure"), ErrCleanupLeaseLost)
	require.NoError(t, store.CompleteCleanupTask(ctx, second[0].ID, second[0].LeaseToken))
}
