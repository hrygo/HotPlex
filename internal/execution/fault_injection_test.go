package execution

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/pkg/events"
)

func TestFaultInjection_SameClientMessageIDNeverProducesDuplicate(t *testing.T) {
	t.Parallel()
	store, sessionStore := newTestSQLStore(t)
	ctx := context.Background()

	now := time.Now()
	require.NoError(t, sessionStore.Upsert(ctx, &session.SessionInfo{
		ID: "session-fi1", UserID: "u1", WorkerType: "claude_code",
		State: events.StateRunning, CreatedAt: now, UpdatedAt: now,
	}))

	var acceptCount int64
	var wg sync.WaitGroup
	const goroutines = 16
	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, dup, err := store.Accept(ctx, AcceptRequest{
				SessionID:       "session-fi1",
				ClientMessageID: "msg-fi-dedup",
				PayloadHash:     "h-fi",
				OwnerInstanceID: "gw-fi",
				WorkerRunID:     "run-fi",
			})
			if err == nil && !dup {
				atomic.AddInt64(&acceptCount, 1)
			}
		}()
	}
	wg.Wait()

	require.Equal(t, int64(1), atomic.LoadInt64(&acceptCount),
		"exactly one Accept must succeed for same session+client_message_id")
}

func TestFaultInjection_LeaseExpiryConvergesToUnknownThenLateDoneRefines(t *testing.T) {
	t.Parallel()
	store, sessionStore := newTestSQLStore(t)
	ctx := context.Background()

	now := time.Now()
	require.NoError(t, sessionStore.Upsert(ctx, &session.SessionInfo{
		ID: "session-fi2", UserID: "u1", WorkerType: "claude_code",
		State: events.StateRunning, CreatedAt: now, UpdatedAt: now,
	}))

	rec, _, err := store.Accept(ctx, AcceptRequest{
		SessionID: "session-fi2", ClientMessageID: "msg-fi2", PayloadHash: "h",
		OwnerInstanceID: "gw-fi2", WorkerRunID: "run-fi2",
	})
	require.NoError(t, err)
	require.NoError(t, store.MarkRunning(ctx, rec.ExecutionID, "gw-fi2", "run-fi2"))

	_, err = store.db.ExecContext(ctx, `UPDATE execution_inputs SET lease_until = ? WHERE execution_id = ?`,
		time.Now().Add(-time.Minute).UnixMilli(), rec.ExecutionID)
	require.NoError(t, err)

	recovered, err := store.RecoverExpiredLeases(ctx, time.Now().UnixMilli())
	require.NoError(t, err)
	require.Equal(t, int64(1), recovered)

	stored, err := store.getByID(ctx, rec.ExecutionID)
	require.NoError(t, err)
	require.Equal(t, RuntimeUnknown, stored.RuntimeStatus)
	require.Equal(t, "GATEWAY_LEASE_EXPIRED", stored.FenceReason)

	require.NoError(t, store.FinishRuntime(ctx, rec.ExecutionID, "run-fi2", RuntimeCompleted, ""),
		"late Done with matching worker_run_id must refine unknown→completed")

	stored, err = store.getByID(ctx, rec.ExecutionID)
	require.NoError(t, err)
	require.Equal(t, RuntimeCompleted, stored.RuntimeStatus)
	require.Empty(t, stored.FenceReason, "convergence must clear fence")
}

func TestFaultInjection_StaleWorkerRunIDRejected(t *testing.T) {
	t.Parallel()
	store, sessionStore := newTestSQLStore(t)
	ctx := context.Background()

	now := time.Now()
	require.NoError(t, sessionStore.Upsert(ctx, &session.SessionInfo{
		ID: "session-fi3", UserID: "u1", WorkerType: "claude_code",
		State: events.StateRunning, CreatedAt: now, UpdatedAt: now,
	}))

	rec, _, err := store.Accept(ctx, AcceptRequest{
		SessionID: "session-fi3", ClientMessageID: "msg-fi3", PayloadHash: "h",
		OwnerInstanceID: "gw-fi3", WorkerRunID: "run-old",
	})
	require.NoError(t, err)

	err = store.FinishRuntime(ctx, rec.ExecutionID, "run-stale", RuntimeCompleted, "")
	require.ErrorIs(t, err, ErrRunMismatch,
		"FinishRuntime with wrong worker_run_id must be rejected")
}

func TestFaultInjection_SequentialExecutionsAfterTerminal(t *testing.T) {
	t.Parallel()
	store, sessionStore := newTestSQLStore(t)
	ctx := context.Background()

	now := time.Now()
	require.NoError(t, sessionStore.Upsert(ctx, &session.SessionInfo{
		ID: "session-fi4", UserID: "u1", WorkerType: "claude_code",
		State: events.StateRunning, CreatedAt: now, UpdatedAt: now,
	}))

	for i := range 3 {
		msgID := "msg-fi4-" + string(rune('a'+i))
		rec, _, err := store.Accept(ctx, AcceptRequest{
			SessionID: "session-fi4", ClientMessageID: msgID, PayloadHash: "h",
			OwnerInstanceID: "gw-fi4", WorkerRunID: "run-fi4-" + string(rune('a'+i)),
		})
		require.NoError(t, err, "iteration %d: Accept must succeed", i)
		require.NoError(t, store.MarkRunning(ctx, rec.ExecutionID, "gw-fi4", rec.WorkerRunID))
		require.NoError(t, store.FinishRuntime(ctx, rec.ExecutionID, rec.WorkerRunID, RuntimeCompleted, ""))
		require.NoError(t, store.SetDelivery(ctx, rec.ExecutionID, "gw-fi4", StatusDelivered, ""))
	}

	_, err := store.ActiveBySession(ctx, "session-fi4")
	require.ErrorIs(t, err, ErrNotFound, "no active execution after all terminals")
}

func TestFaultInjection_FenceBlocksNewInputUntilCleared(t *testing.T) {
	t.Parallel()
	store, sessionStore := newTestSQLStore(t)
	ctx := context.Background()

	now := time.Now()
	require.NoError(t, sessionStore.Upsert(ctx, &session.SessionInfo{
		ID: "session-fi5", UserID: "u1", WorkerType: "claude_code",
		State: events.StateRunning, CreatedAt: now, UpdatedAt: now,
	}))

	rec, _, err := store.Accept(ctx, AcceptRequest{
		SessionID: "session-fi5", ClientMessageID: "msg-fi5", PayloadHash: "h",
		OwnerInstanceID: "gw-fi5", WorkerRunID: "run-fi5",
	})
	require.NoError(t, err)
	require.NoError(t, store.MarkRunning(ctx, rec.ExecutionID, "gw-fi5", "run-fi5"))
	require.NoError(t, store.FinishRuntime(ctx, rec.ExecutionID, "run-fi5", RuntimeUnknown, "TIMEOUT"))

	fenced, err := store.FenceBySession(ctx, "session-fi5")
	require.NoError(t, err)
	require.NotEmpty(t, fenced.FenceReason)

	_, _, err = store.Accept(ctx, AcceptRequest{
		SessionID: "session-fi5", ClientMessageID: "msg-fi5-next", PayloadHash: "h2",
		OwnerInstanceID: "gw-fi5", WorkerRunID: "run-fi5-next",
	})
	require.ErrorIs(t, err, ErrSessionBusy,
		"fenced session must block new input via active gate")

	require.NoError(t,
		store.ClearFenceAfterFreshStart(ctx, rec.ExecutionID, fenced.FenceReason, "run-fi5-fresh"))

	_, _, err = store.Accept(ctx, AcceptRequest{
		SessionID: "session-fi5", ClientMessageID: "msg-fi5-next", PayloadHash: "h2",
		OwnerInstanceID: "gw-fi5", WorkerRunID: "run-fi5-fresh",
	})
	require.NoError(t, err, "new input must succeed after fence cleared via fresh start")
}
