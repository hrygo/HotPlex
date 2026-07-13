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

func TestSQLStore_AcceptIsIdempotentAndRejectsPayloadConflict(t *testing.T) {
	t.Parallel()
	store, _ := newTestSQLStore(t)
	ctx := context.Background()
	request := AcceptRequest{SessionID: "session-1", ClientMessageID: "message-1", PayloadHash: "hash-a"}

	first, duplicate, err := store.Accept(ctx, request)
	require.NoError(t, err)
	require.False(t, duplicate)
	require.Equal(t, StatusAccepted, first.Status)
	require.Contains(t, first.ExecutionID, "exec_")

	second, duplicate, err := store.Accept(ctx, request)
	require.NoError(t, err)
	require.True(t, duplicate)
	require.Equal(t, first.ExecutionID, second.ExecutionID)

	_, duplicate, err = store.Accept(ctx, AcceptRequest{
		SessionID:       request.SessionID,
		ClientMessageID: request.ClientMessageID,
		PayloadHash:     "hash-b",
	})
	require.ErrorIs(t, err, ErrPayloadConflict)
	require.True(t, duplicate)
}

func TestSQLStore_ConcurrentAcceptCreatesOneExecution(t *testing.T) {
	t.Parallel()
	store, _ := newTestSQLStore(t)
	request := AcceptRequest{SessionID: "session-1", ClientMessageID: "message-race", PayloadHash: "hash"}

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
	record, _, err := store.Accept(context.Background(), AcceptRequest{
		SessionID: "session-1", ClientMessageID: "message-2", PayloadHash: "hash",
	})
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

func TestSQLStore_RestartMarksAcceptedUnknown(t *testing.T) {
	t.Parallel()
	store, sessionStore := newTestSQLStore(t)
	record, _, err := store.Accept(context.Background(), AcceptRequest{
		SessionID: "session-1", ClientMessageID: "message-3", PayloadHash: "hash",
	})
	require.NoError(t, err)
	require.Equal(t, StatusAccepted, record.Status)

	restarted, err := NewSQLStore(context.Background(), sessionStore.DB(), dbutil.DialectSQLite, store.writeMu, nil)
	require.NoError(t, err)
	recovered, duplicate, err := restarted.Accept(context.Background(), AcceptRequest{
		SessionID: "session-1", ClientMessageID: "message-3", PayloadHash: "hash",
	})
	require.NoError(t, err)
	require.True(t, duplicate)
	require.Equal(t, StatusUnknown, recovered.Status)
	require.Equal(t, "GATEWAY_RESTART", recovered.ErrorCode)
}
