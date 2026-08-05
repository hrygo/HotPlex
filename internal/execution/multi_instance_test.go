package execution

import (
	"context"
	"sync"
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

// fenceMultiInstanceSetup fences an execution owned by gw-A and returns two
// store views of the same database (two gateway instances) plus the record.
func fenceMultiInstanceSetup(t *testing.T, sessionID, msgID string) (*SQLStore, *SQLStore, *Record) {
	t.Helper()
	storeA, sessionStore := newTestSQLStore(t)
	ctx := context.Background()

	now := time.Now()
	require.NoError(t, sessionStore.Upsert(ctx, &session.SessionInfo{
		ID: sessionID, UserID: "user-1", WorkerType: "claude_code",
		State: events.StateRunning, CreatedAt: now, UpdatedAt: now,
	}))

	rec, _, err := storeA.Accept(ctx, AcceptRequest{
		SessionID: sessionID, ClientMessageID: msgID, PayloadHash: "h-" + msgID,
		OwnerInstanceID: "gw-A", WorkerRunID: "run-A",
	})
	require.NoError(t, err)
	require.NoError(t, storeA.MarkRunning(ctx, rec.ExecutionID, "gw-A", "run-A"))
	require.NoError(t, storeA.FinishRuntime(ctx, rec.ExecutionID, "run-A", RuntimeUnknown, "TIMEOUT"))

	fenced, err := storeA.FenceBySession(ctx, sessionID)
	require.NoError(t, err)
	require.NotEmpty(t, fenced.FenceReason)

	storeB, err := NewSQLStore(ctx, sessionStore.DB(), storeA.dialect, storeA.writeMu, nil)
	require.NoError(t, err)
	return storeA, storeB, fenced
}

func TestMultiInstance_FenceDecisionExactlyOnceAcrossInstances(t *testing.T) {
	t.Parallel()
	storeA, storeB, fenced := fenceMultiInstanceSetup(t, "session-frace", "msg-frace")
	ctx := context.Background()

	// Both instances inspect the same fence version, then race their decisions.
	fenceB, err := storeB.FenceBySession(ctx, "session-frace")
	require.NoError(t, err)
	require.Equal(t, fenced.FenceVersion, fenceB.FenceVersion)

	const instances = 8
	results := make(chan error, instances)
	var wg sync.WaitGroup
	for i := range instances {
		wg.Add(1)
		store := storeA
		if i%2 == 1 {
			store = storeB
		}
		go func() {
			defer wg.Done()
			_, err := store.ApplyFenceDecision(ctx, FenceActionRequest{
				ExecutionID:          fenced.ExecutionID,
				ExpectedFenceVersion: fenced.FenceVersion,
				Decision:             FenceDecisionResolve,
			})
			results <- err
		}()
	}
	wg.Wait()
	close(results)

	var successes int
	for err := range results {
		if err == nil {
			successes++
			continue
		}
		require.ErrorIs(t, err, ErrFenceConflict, "losers must see a conflict, not a silent no-op")
	}
	require.Equal(t, 1, successes, "exactly one operator decision may win")

	stored, err := storeA.getByID(ctx, fenced.ExecutionID)
	require.NoError(t, err)
	require.Empty(t, stored.FenceReason)
	require.Equal(t, RuntimeUnknown, stored.RuntimeStatus)
}

func TestMultiInstance_StaleFenceVersionAfterOtherInstanceActs(t *testing.T) {
	t.Parallel()
	storeA, storeB, fenced := fenceMultiInstanceSetup(t, "session-fstale", "msg-fstale")
	ctx := context.Background()

	// Operator on instance A inspects; before acting, instance B resolves.
	_, err := storeB.ApplyFenceDecision(ctx, FenceActionRequest{
		ExecutionID:          fenced.ExecutionID,
		ExpectedFenceVersion: fenced.FenceVersion,
		Decision:             FenceDecisionResolve,
	})
	require.NoError(t, err)

	// A's action with the stale version must conflict; no auto-retry.
	_, err = storeA.ApplyFenceDecision(ctx, FenceActionRequest{
		ExecutionID:          fenced.ExecutionID,
		ExpectedFenceVersion: fenced.FenceVersion,
		Decision:             FenceDecisionAbandon,
	})
	require.ErrorIs(t, err, ErrFenceConflict)

	stored, err := storeA.getByID(ctx, fenced.ExecutionID)
	require.NoError(t, err)
	require.Equal(t, RuntimeUnknown, stored.RuntimeStatus, "abandon must not apply after resolve")
	require.Empty(t, stored.FenceReason)
}

func TestFenceDecision_LateDoneAfterResolveConverges(t *testing.T) {
	t.Parallel()
	storeA, _, fenced := fenceMultiInstanceSetup(t, "session-flate-ok", "msg-flate-ok")
	ctx := context.Background()

	_, err := storeA.ApplyFenceDecision(ctx, FenceActionRequest{
		ExecutionID:          fenced.ExecutionID,
		ExpectedFenceVersion: fenced.FenceVersion,
		Decision:             FenceDecisionResolve,
	})
	require.NoError(t, err)

	require.NoError(t, storeA.FinishRuntime(ctx, fenced.ExecutionID, "run-A", RuntimeCompleted, ""),
		"late Done after resolve must still converge unknown->completed")

	stored, err := storeA.getByID(ctx, fenced.ExecutionID)
	require.NoError(t, err)
	require.Equal(t, RuntimeCompleted, stored.RuntimeStatus)
	require.Empty(t, stored.FenceReason)
	require.Equal(t, fenced.FenceVersion, stored.FenceVersion)
}

func TestFenceDecision_LateDoneAfterAbandonRejected(t *testing.T) {
	t.Parallel()
	storeA, _, fenced := fenceMultiInstanceSetup(t, "session-flate-no", "msg-flate-no")
	ctx := context.Background()

	_, err := storeA.ApplyFenceDecision(ctx, FenceActionRequest{
		ExecutionID:          fenced.ExecutionID,
		ExpectedFenceVersion: fenced.FenceVersion,
		Decision:             FenceDecisionAbandon,
	})
	require.NoError(t, err)

	err = storeA.FinishRuntime(ctx, fenced.ExecutionID, "run-A", RuntimeCompleted, "")
	require.Error(t, err, "late Done must not regress an abandoned execution")

	stored, err := storeA.getByID(ctx, fenced.ExecutionID)
	require.NoError(t, err)
	require.Equal(t, RuntimeFailed, stored.RuntimeStatus)
	require.Equal(t, RuntimeErrorCodeOperatorAbandoned, stored.RuntimeErrorCode)
}

func TestFenceDecision_NewInputAcceptedAfterDecision(t *testing.T) {
	t.Parallel()

	for _, decision := range []FenceDecision{FenceDecisionResolve, FenceDecisionAbandon} {
		t.Run(string(decision), func(t *testing.T) {
			t.Parallel()
			sessionID := "session-fnew-" + string(decision)
			storeA, _, fenced := fenceMultiInstanceSetup(t, sessionID, "msg-fnew")
			ctx := context.Background()

			_, _, err := storeA.Accept(ctx, AcceptRequest{
				SessionID: sessionID, ClientMessageID: "msg-fnew-next", PayloadHash: "h-next",
				OwnerInstanceID: "gw-B", WorkerRunID: "run-B",
			})
			require.ErrorIs(t, err, ErrSessionBusy, "fenced session must reject new input")

			_, err = storeA.ApplyFenceDecision(ctx, FenceActionRequest{
				ExecutionID:          fenced.ExecutionID,
				ExpectedFenceVersion: fenced.FenceVersion,
				Decision:             decision,
			})
			require.NoError(t, err)

			next, _, err := storeA.Accept(ctx, AcceptRequest{
				SessionID: sessionID, ClientMessageID: "msg-fnew-next", PayloadHash: "h-next",
				OwnerInstanceID: "gw-B", WorkerRunID: "run-B",
			})
			require.NoError(t, err, "%q must release the session for fresh input", decision)
			require.NotEqual(t, fenced.ExecutionID, next.ExecutionID)
		})
	}
}
