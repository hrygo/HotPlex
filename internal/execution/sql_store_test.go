package execution

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/dbutil"
	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/internal/sqlutil"
	"github.com/hrygo/hotplex/pkg/events"
)

const (
	testOwner = "gw-test-instance"
	testRun   = "run-test-001"
)

func newTestSQLStore(t *testing.T) (*SQLStore, *session.SQLiteStore) {
	t.Helper()
	ctx := context.Background()
	cfg := config.Default()
	cfg.DB.Path = filepath.Join(t.TempDir(), "execution.db")
	cfg.DB.SQLite.Path = cfg.DB.Path
	writeMu := sqlutil.NewWriteMu(sqlutil.DialectSQLite)
	sessionStore, err := session.NewSQLiteStore(ctx, cfg, writeMu)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sessionStore.Close()) })

	now := time.Now()
	require.NoError(t, sessionStore.Upsert(ctx, &session.SessionInfo{
		ID:         "session-1",
		UserID:     "user-1",
		WorkerType: "claude_code",
		State:      events.StateRunning,
		CreatedAt:  now,
		UpdatedAt:  now,
	}))

	store, err := NewSQLStore(ctx, sessionStore.DB(), dbutil.DialectSQLite, writeMu, nil)
	require.NoError(t, err)
	return store, sessionStore
}

func testAcceptReq(sessionID, msgID, hash string) AcceptRequest {
	return AcceptRequest{
		SessionID:       sessionID,
		ClientMessageID: msgID,
		PayloadHash:     hash,
		OwnerInstanceID: testOwner,
		WorkerRunID:     testRun,
	}
}

func TestSQLStore_AcceptIsIdempotentAndRejectsPayloadConflict(t *testing.T) {
	t.Parallel()
	store, _ := newTestSQLStore(t)
	ctx := context.Background()
	request := testAcceptReq("session-1", "message-1", "hash-a")

	first, duplicate, err := store.Accept(ctx, request)
	require.NoError(t, err)
	require.False(t, duplicate)
	require.Equal(t, StatusAccepted, first.Status)
	require.Contains(t, first.ExecutionID, "exec_")

	second, duplicate, err := store.Accept(ctx, request)
	require.NoError(t, err)
	require.True(t, duplicate)
	require.Equal(t, first.ExecutionID, second.ExecutionID)

	_, duplicate, err = store.Accept(ctx, testAcceptReq("session-1", "message-1", "hash-b"))
	require.ErrorIs(t, err, ErrPayloadConflict)
	require.True(t, duplicate)
}

func TestSQLStore_ConcurrentAcceptCreatesOneExecution(t *testing.T) {
	t.Parallel()
	store, _ := newTestSQLStore(t)
	request := testAcceptReq("session-1", "message-race", "hash")

	const callers = 16
	var wg sync.WaitGroup
	wg.Add(callers)
	ids := make(chan string, callers)
	newRecords := make(chan bool, callers)
	errs := make(chan error, callers)
	for range callers {
		go func() {
			defer wg.Done()
			record, duplicate, err := store.Accept(context.Background(), request)
			if err != nil {
				errs <- err
				return
			}
			ids <- record.ExecutionID
			newRecords <- !duplicate
		}()
	}
	wg.Wait()
	close(ids)
	close(newRecords)
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}
	var executionID string
	for id := range ids {
		if executionID == "" {
			executionID = id
		}
		require.Equal(t, executionID, id)
	}
	created := 0
	for isNew := range newRecords {
		if isNew {
			created++
		}
	}
	require.Equal(t, 1, created)
}

func TestSQLStore_SetStatusIsIdempotentAndTerminal(t *testing.T) {
	t.Parallel()
	store, _ := newTestSQLStore(t)
	record, _, err := store.Accept(context.Background(), testAcceptReq("session-1", "message-2", "hash"))
	require.NoError(t, err)

	require.NoError(t, store.SetStatus(context.Background(), record.ExecutionID, StatusDelivered, ""))
	first, err := store.getByClientMessage(context.Background(), "session-1", "message-2")
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return time.Now().UnixMilli() > first.UpdatedAt
	}, time.Second, time.Millisecond)
	require.NoError(t, store.SetStatus(context.Background(), record.ExecutionID, StatusDelivered, ""))
	require.ErrorIs(t, store.SetStatus(context.Background(), record.ExecutionID, StatusFailed, "LATE_FAILURE"), ErrNotFound)

	stored, err := store.getByClientMessage(context.Background(), "session-1", "message-2")
	require.NoError(t, err)
	require.Equal(t, StatusDelivered, stored.Status)
	require.NotNil(t, stored.DeliveredAt)
	require.Equal(t, first.UpdatedAt, stored.UpdatedAt)
	require.Equal(t, first.DeliveredAt, stored.DeliveredAt)
}

func TestSQLStore_ConvergeDeliveryFailedConverges(t *testing.T) {
	t.Parallel()
	store, _ := newTestSQLStore(t)
	ctx := context.Background()

	record, _, err := store.Accept(ctx, testAcceptReq("session-1", "converge-1", "hash"))
	require.NoError(t, err)
	require.NoError(t, store.MarkRunning(ctx, record.ExecutionID, testOwner, testRun))
	require.NoError(t, store.SetDelivery(ctx, record.ExecutionID, testOwner, StatusFailed, "INTERNAL_ERROR"))
	require.NoError(t, store.FinishRuntime(ctx, record.ExecutionID, testRun, RuntimeCompleted, ""))

	require.NoError(t, store.ConvergeDeliveryFailed(ctx, record.ExecutionID, testRun))
	stored, err := store.getByClientMessage(ctx, "session-1", "converge-1")
	require.NoError(t, err)
	require.Equal(t, StatusUnknown, stored.Status)
	require.Equal(t, "INTERNAL_ERROR", stored.ErrorCode, "diagnostic error code is retained")

	require.NoError(t, store.ConvergeDeliveryFailed(ctx, record.ExecutionID, testRun), "repeat is a no-op")
	stored, err = store.getByClientMessage(ctx, "session-1", "converge-1")
	require.NoError(t, err)
	require.Equal(t, StatusUnknown, stored.Status)

	require.NoError(t, store.ConvergeDeliveryFailed(ctx, record.ExecutionID, "run-other"), "wrong run is a no-op")
}

func TestSQLStore_ConvergeDeliveryFailedGuards(t *testing.T) {
	t.Parallel()

	t.Run("runtime not completed", func(t *testing.T) {
		store, _ := newTestSQLStore(t)
		record, _, err := store.Accept(context.Background(), testAcceptReq("session-1", "converge-2", "hash"))
		require.NoError(t, err)
		require.NoError(t, store.MarkRunning(context.Background(), record.ExecutionID, testOwner, testRun))
		require.NoError(t, store.SetDelivery(context.Background(), record.ExecutionID, testOwner, StatusFailed, "INTERNAL_ERROR"))
		require.NoError(t, store.ConvergeDeliveryFailed(context.Background(), record.ExecutionID, testRun))
		stored, err := store.getByClientMessage(context.Background(), "session-1", "converge-2")
		require.NoError(t, err)
		require.Equal(t, StatusFailed, stored.Status, "failed delivery must survive until runtime completes")
	})

	t.Run("delivery not failed", func(t *testing.T) {
		store, _ := newTestSQLStore(t)
		record, _, err := store.Accept(context.Background(), testAcceptReq("session-1", "converge-3", "hash"))
		require.NoError(t, err)
		require.NoError(t, store.MarkRunning(context.Background(), record.ExecutionID, testOwner, testRun))
		require.NoError(t, store.SetDelivery(context.Background(), record.ExecutionID, testOwner, StatusUnknown, "EXECUTION_TIMEOUT"))
		require.NoError(t, store.FinishRuntime(context.Background(), record.ExecutionID, testRun, RuntimeCompleted, ""))
		require.NoError(t, store.ConvergeDeliveryFailed(context.Background(), record.ExecutionID, testRun))
		stored, err := store.getByClientMessage(context.Background(), "session-1", "converge-3")
		require.NoError(t, err)
		require.Equal(t, StatusUnknown, stored.Status)
		require.Equal(t, "EXECUTION_TIMEOUT", stored.ErrorCode, "unknown delivery keeps its error code")
	})
}

func TestSQLStore_RecoverExpiredLeases(t *testing.T) {
	t.Parallel()
	store, _ := newTestSQLStore(t)

	record, _, err := store.Accept(context.Background(), testAcceptReq("session-1", "message-3", "hash"))
	require.NoError(t, err)
	require.Equal(t, RuntimePending, record.RuntimeStatus)
	require.NotZero(t, record.LeaseUntil)

	_, err = store.db.ExecContext(context.Background(),
		`UPDATE execution_inputs SET lease_until = 0 WHERE execution_id = ?`, record.ExecutionID)
	require.NoError(t, err)
	recovered, err := store.RecoverExpiredLeases(context.Background(), []string{record.ExecutionID})
	require.NoError(t, err)
	require.Equal(t, int64(1), recovered.Recovered)
	require.Equal(t, []string{record.ExecutionID}, recovered.ConvergedExecutionIDs)

	stored, err := store.getByClientMessage(context.Background(), "session-1", "message-3")
	require.NoError(t, err)
	require.Equal(t, RuntimeUnknown, stored.RuntimeStatus)
	require.Equal(t, "GATEWAY_LEASE_EXPIRED", stored.FenceReason)
	require.Equal(t, "GATEWAY_LEASE_EXPIRED", stored.RuntimeErrorCode)

	recoveredAgain, err := store.RecoverExpiredLeases(context.Background(), []string{record.ExecutionID})
	require.NoError(t, err)
	require.Equal(t, int64(0), recoveredAgain.Recovered, "already recovered — must not double-recover")
	require.Equal(t, []string{record.ExecutionID}, recoveredAgain.ConvergedExecutionIDs,
		"a tracker must clear exclusions even when another recovery already converged the row")
}
