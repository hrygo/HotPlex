package execution

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/pkg/events"
)

func ensureSession(t *testing.T, sessionStore *session.SQLiteStore, sessionID string) {
	t.Helper()
	if sessionID == "session-1" {
		return
	}
	now := time.Now()
	require.NoError(t, sessionStore.Upsert(context.Background(), &session.SessionInfo{
		ID: sessionID, UserID: "user-1", WorkerType: "claude_code",
		State: events.StateRunning, CreatedAt: now, UpdatedAt: now,
	}))
}

func acceptAndRun(t *testing.T, store *SQLStore, sessionStore *session.SQLiteStore, sessionID, msgID string) *Record {
	t.Helper()
	ensureSession(t, sessionStore, sessionID)
	rec, _, err := store.Accept(context.Background(), testAcceptReq(sessionID, msgID, "h-"+msgID))
	require.NoError(t, err)
	require.NoError(t, store.MarkRunning(context.Background(), rec.ExecutionID, testOwner, testRun))
	return rec
}

func TestSetDelivery_OwnerConditioned(t *testing.T) {
	t.Parallel()
	store, sessionStore := newTestSQLStore(t)
	ensureSession(t, sessionStore, "session-sd")
	rec, _, err := store.Accept(context.Background(), testAcceptReq("session-sd", "msg-sd", "h-sd"))
	require.NoError(t, err)

	require.NoError(t, store.SetDelivery(context.Background(), rec.ExecutionID, testOwner, StatusDelivered, ""))
	stored, err := store.getByClientMessage(context.Background(), "session-sd", "msg-sd")
	require.NoError(t, err)
	require.Equal(t, StatusDelivered, stored.Status)

	require.ErrorIs(t,
		store.SetDelivery(context.Background(), rec.ExecutionID, "wrong-owner", StatusFailed, "X"),
		ErrOwnerMismatch)
}

func TestSetDelivery_IdempotentAndTerminal(t *testing.T) {
	t.Parallel()
	store, sessionStore := newTestSQLStore(t)
	rec := acceptAndRun(t, store, sessionStore, "session-sdi", "msg-sdi")
	require.NoError(t, store.SetDelivery(context.Background(), rec.ExecutionID, testOwner, StatusDelivered, ""))

	require.NoError(t, store.SetDelivery(context.Background(), rec.ExecutionID, testOwner, StatusDelivered, ""),
		"repeated SetDelivery to same terminal must be idempotent")
}

func TestMarkRunning_TransitionsAndGuards(t *testing.T) {
	t.Parallel()
	store, sessionStore := newTestSQLStore(t)
	ensureSession(t, sessionStore, "session-mr")
	rec, _, err := store.Accept(context.Background(), testAcceptReq("session-mr", "msg-mr", "h-mr"))
	require.NoError(t, err)
	require.Equal(t, RuntimePending, rec.RuntimeStatus)

	require.NoError(t, store.MarkRunning(context.Background(), rec.ExecutionID, testOwner, testRun))
	stored, err := store.getByID(context.Background(), rec.ExecutionID)
	require.NoError(t, err)
	require.Equal(t, RuntimeRunning, stored.RuntimeStatus)
	require.Equal(t, testRun, stored.WorkerRunID)
	require.NotNil(t, stored.StartedAt)

	require.ErrorIs(t,
		store.MarkRunning(context.Background(), rec.ExecutionID, "wrong-owner", testRun),
		ErrOwnerMismatch)

	require.NoError(t, store.MarkRunning(context.Background(), rec.ExecutionID, testOwner, testRun),
		"repeated MarkRunning with same run must be idempotent")
}

func TestFinishRuntime_TerminalTransitions(t *testing.T) {
	t.Parallel()
	store, sessionStore := newTestSQLStore(t)
	rec := acceptAndRun(t, store, sessionStore, "session-fr", "msg-fr")

	require.NoError(t, store.FinishRuntime(context.Background(), rec.ExecutionID, testRun, RuntimeCompleted, ""))
	stored, err := store.getByID(context.Background(), rec.ExecutionID)
	require.NoError(t, err)
	require.Equal(t, RuntimeCompleted, stored.RuntimeStatus)
	require.NotNil(t, stored.FinishedAt)
	require.Empty(t, stored.FenceReason, "completed must clear fence")
}

func TestFinishRuntime_FailedSetsErrorCode(t *testing.T) {
	t.Parallel()
	store, sessionStore := newTestSQLStore(t)
	rec := acceptAndRun(t, store, sessionStore, "session-ff", "msg-ff")

	require.NoError(t, store.FinishRuntime(context.Background(), rec.ExecutionID, testRun, RuntimeFailed, "TOOL_ERROR"))
	stored, err := store.getByID(context.Background(), rec.ExecutionID)
	require.NoError(t, err)
	require.Equal(t, RuntimeFailed, stored.RuntimeStatus)
	require.Equal(t, "TOOL_ERROR", stored.RuntimeErrorCode)
}

func TestFinishRuntime_UnknownSetsFence(t *testing.T) {
	t.Parallel()
	store, sessionStore := newTestSQLStore(t)
	rec := acceptAndRun(t, store, sessionStore, "session-fu", "msg-fu")

	require.NoError(t, store.FinishRuntime(context.Background(), rec.ExecutionID, testRun, RuntimeUnknown, "TIMEOUT"))
	stored, err := store.getByID(context.Background(), rec.ExecutionID)
	require.NoError(t, err)
	require.Equal(t, RuntimeUnknown, stored.RuntimeStatus)
	require.Equal(t, "RUNTIME_AMBIGUOUS", stored.FenceReason)
}

func TestFinishRuntime_LateConvergence(t *testing.T) {
	t.Parallel()
	store, sessionStore := newTestSQLStore(t)
	rec := acceptAndRun(t, store, sessionStore, "session-lc", "msg-lc")

	require.NoError(t, store.FinishRuntime(context.Background(), rec.ExecutionID, testRun, RuntimeUnknown, "TIMEOUT"))

	require.NoError(t, store.FinishRuntime(context.Background(), rec.ExecutionID, testRun, RuntimeCompleted, ""),
		"unknown→completed late convergence must succeed")

	stored, err := store.getByID(context.Background(), rec.ExecutionID)
	require.NoError(t, err)
	require.Equal(t, RuntimeCompleted, stored.RuntimeStatus)
	require.Empty(t, stored.FenceReason, "convergence must clear fence")
}

func TestFinishRuntime_RunMismatch(t *testing.T) {
	t.Parallel()
	store, sessionStore := newTestSQLStore(t)
	rec := acceptAndRun(t, store, sessionStore, "session-rm", "msg-rm")

	err := store.FinishRuntime(context.Background(), rec.ExecutionID, "wrong-run", RuntimeCompleted, "")
	require.ErrorIs(t, err, ErrRunMismatch)
}

func TestFinishRuntime_CompletedDoesNotRegress(t *testing.T) {
	t.Parallel()
	store, sessionStore := newTestSQLStore(t)
	rec := acceptAndRun(t, store, sessionStore, "session-nr", "msg-nr")

	require.NoError(t, store.FinishRuntime(context.Background(), rec.ExecutionID, testRun, RuntimeCompleted, ""))

	err := store.FinishRuntime(context.Background(), rec.ExecutionID, testRun, RuntimeFailed, "LATE")
	require.Error(t, err, "completed must not regress to failed")

	stored, err := store.getByID(context.Background(), rec.ExecutionID)
	require.NoError(t, err)
	require.Equal(t, RuntimeCompleted, stored.RuntimeStatus)
}

func TestActiveBySession_FindsPendingAndRunning(t *testing.T) {
	t.Parallel()
	store, sessionStore := newTestSQLStore(t)
	ensureSession(t, sessionStore, "session-ab")

	_, _, err := store.Accept(context.Background(), testAcceptReq("session-ab", "msg-ab", "h-ab"))
	require.NoError(t, err)

	active, err := store.ActiveBySession(context.Background(), "session-ab")
	require.NoError(t, err)
	require.Equal(t, RuntimePending, active.RuntimeStatus)

	require.NoError(t, store.MarkRunning(context.Background(), active.ExecutionID, testOwner, testRun))
	active, err = store.ActiveBySession(context.Background(), "session-ab")
	require.NoError(t, err)
	require.Equal(t, RuntimeRunning, active.RuntimeStatus)
}

func TestActiveBySession_NotFound(t *testing.T) {
	t.Parallel()
	store, _ := newTestSQLStore(t)
	_, err := store.ActiveBySession(context.Background(), "session-empty")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestActiveBySession_ExcludesTerminal(t *testing.T) {
	t.Parallel()
	store, sessionStore := newTestSQLStore(t)
	rec := acceptAndRun(t, store, sessionStore, "session-at", "msg-at")
	require.NoError(t, store.FinishRuntime(context.Background(), rec.ExecutionID, testRun, RuntimeCompleted, ""))

	_, err := store.ActiveBySession(context.Background(), "session-at")
	require.ErrorIs(t, err, ErrNotFound, "completed execution must not be active")
}

func TestFenceBySession_FindsFenced(t *testing.T) {
	t.Parallel()
	store, sessionStore := newTestSQLStore(t)
	rec := acceptAndRun(t, store, sessionStore, "session-fb", "msg-fb")
	require.NoError(t, store.FinishRuntime(context.Background(), rec.ExecutionID, testRun, RuntimeUnknown, "X"))

	fenced, err := store.FenceBySession(context.Background(), "session-fb")
	require.NoError(t, err)
	require.NotEmpty(t, fenced.FenceReason)
}

func TestFenceBySession_NotFound(t *testing.T) {
	t.Parallel()
	store, sessionStore := newTestSQLStore(t)
	acceptAndRun(t, store, sessionStore, "session-nf", "msg-nf")

	_, err := store.FenceBySession(context.Background(), "session-nf")
	require.ErrorIs(t, err, ErrNotFound, "non-fenced execution must not be found")
}

func TestClearFenceAfterFreshStart_MatchingReason(t *testing.T) {
	t.Parallel()
	store, sessionStore := newTestSQLStore(t)
	rec := acceptAndRun(t, store, sessionStore, "session-cf", "msg-cf")
	require.NoError(t, store.FinishRuntime(context.Background(), rec.ExecutionID, testRun, RuntimeUnknown, "X"))

	fenced, err := store.FenceBySession(context.Background(), "session-cf")
	require.NoError(t, err)
	require.NotEmpty(t, fenced.FenceReason)

	require.NoError(t,
		store.ClearFenceAfterFreshStart(context.Background(), rec.ExecutionID, fenced.FenceReason, "fresh-run-001"))

	stored, err := store.getByID(context.Background(), rec.ExecutionID)
	require.NoError(t, err)
	require.Empty(t, stored.FenceReason)
	require.Equal(t, "fresh-run-001", stored.WorkerRunID)
}

func TestClearFenceAfterFreshStart_WrongReason(t *testing.T) {
	t.Parallel()
	store, sessionStore := newTestSQLStore(t)
	rec := acceptAndRun(t, store, sessionStore, "session-wr", "msg-wr")
	require.NoError(t, store.FinishRuntime(context.Background(), rec.ExecutionID, testRun, RuntimeUnknown, "X"))

	err := store.ClearFenceAfterFreshStart(context.Background(), rec.ExecutionID, "WRONG_REASON", "fresh-run")
	require.Error(t, err, "mismatched fence reason must fail")

	stored, err := store.getByID(context.Background(), rec.ExecutionID)
	require.NoError(t, err)
	require.NotEmpty(t, stored.FenceReason, "fence must remain after wrong reason")
}

func TestRenewLeases_BatchByOwner(t *testing.T) {
	t.Parallel()
	store, sessionStore := newTestSQLStore(t)
	ensureSession(t, sessionStore, "session-r1")
	ensureSession(t, sessionStore, "session-r2")

	req1 := testAcceptReq("session-r1", "msg-r1", "h-r1")
	req1.WorkerRunID = "run-r1"
	rec1, _, err := store.Accept(context.Background(), req1)
	require.NoError(t, err)

	req2 := testAcceptReq("session-r2", "msg-r2", "h-r2")
	req2.WorkerRunID = "run-r2"
	rec2, _, err := store.Accept(context.Background(), req2)
	require.NoError(t, err)

	originalLease := rec1.LeaseUntil
	renewed, err := store.RenewLeases(context.Background(), testOwner, 120, nil)
	require.NoError(t, err)
	require.Equal(t, int64(2), renewed)

	r1, err := store.getByID(context.Background(), rec1.ExecutionID)
	require.NoError(t, err)
	require.Greater(t, r1.LeaseUntil, originalLease)

	r2, err := store.getByID(context.Background(), rec2.ExecutionID)
	require.NoError(t, err)
	require.Greater(t, r2.LeaseUntil, originalLease)
}

func TestRenewLeases_NoActiveReturnsZero(t *testing.T) {
	t.Parallel()
	store, _ := newTestSQLStore(t)
	renewed, err := store.RenewLeases(context.Background(), testOwner, 60, nil)
	require.NoError(t, err)
	require.Equal(t, int64(0), renewed)
}

func TestRenewLeases_OtherOwner(t *testing.T) {
	t.Parallel()
	store, sessionStore := newTestSQLStore(t)
	req := testAcceptReq("session-oo", "msg-oo", "h-oo")
	req.OwnerInstanceID = "other-gw"
	ensureSession(t, sessionStore, "session-oo")
	_, _, err := store.Accept(context.Background(), req)
	require.NoError(t, err)

	renewed, err := store.RenewLeases(context.Background(), testOwner, 60, nil)
	require.NoError(t, err)
	require.Equal(t, int64(0), renewed, "must not renew other owner's leases")
}

func TestRenewLeases_LargeExclusionSetIsBounded(t *testing.T) {
	t.Parallel()
	store, sessionStore := newTestSQLStore(t)
	ctx := context.Background()
	now := time.Now()
	for _, sessionID := range []string{"session-renew-large-include", "session-renew-large-exclude"} {
		require.NoError(t, sessionStore.Upsert(ctx, &session.SessionInfo{
			ID: sessionID, UserID: "u1", WorkerType: "claude_code",
			State: events.StateRunning, CreatedAt: now, UpdatedAt: now,
		}))
	}
	included, _, err := store.Accept(ctx, AcceptRequest{
		SessionID: "session-renew-large-include", ClientMessageID: "msg-include", PayloadHash: "h1",
		OwnerInstanceID: testOwner, WorkerRunID: "run-include",
	})
	require.NoError(t, err)
	excluded, _, err := store.Accept(ctx, AcceptRequest{
		SessionID: "session-renew-large-exclude", ClientMessageID: "msg-exclude", PayloadHash: "h2",
		OwnerInstanceID: testOwner, WorkerRunID: "run-exclude",
	})
	require.NoError(t, err)

	exclusions := make([]string, 40_001)
	exclusions[0] = excluded.ExecutionID
	for i := 1; i < len(exclusions); i++ {
		exclusions[i] = fmt.Sprintf("missing-%d", i)
	}
	renewed, err := store.RenewLeases(ctx, testOwner, 120, exclusions)
	require.NoError(t, err)
	require.EqualValues(t, 1, renewed)

	storedIncluded, err := store.getByID(ctx, included.ExecutionID)
	require.NoError(t, err)
	storedExcluded, err := store.getByID(ctx, excluded.ExecutionID)
	require.NoError(t, err)
	require.Greater(t, storedIncluded.LeaseUntil, included.LeaseUntil)
	require.Equal(t, excluded.LeaseUntil, storedExcluded.LeaseUntil)
}

func TestTerminateOwnerLeases_MarksUnknownAndFenced(t *testing.T) {
	t.Parallel()
	store, sessionStore := newTestSQLStore(t)
	rec := acceptAndRun(t, store, sessionStore, "session-tl", "msg-tl")

	terminated, err := store.TerminateOwnerLeases(context.Background(), testOwner, "GATEWAY_SHUTDOWN")
	require.NoError(t, err)
	require.Equal(t, int64(1), terminated)

	stored, err := store.getByID(context.Background(), rec.ExecutionID)
	require.NoError(t, err)
	require.Equal(t, RuntimeUnknown, stored.RuntimeStatus)
	require.Equal(t, StatusUnknown, stored.Status)
	require.Equal(t, "GATEWAY_SHUTDOWN", stored.ErrorCode)
	require.Equal(t, "GATEWAY_SHUTDOWN", stored.FenceReason)
	require.NotNil(t, stored.FinishedAt)
}

func TestAccept_SecondActiveForSameSessionReturnsBusy(t *testing.T) {
	t.Parallel()
	store, sessionStore := newTestSQLStore(t)
	ensureSession(t, sessionStore, "session-busy")

	_, _, err := store.Accept(context.Background(), testAcceptReq("session-busy", "msg-first", "h1"))
	require.NoError(t, err)

	_, _, err = store.Accept(context.Background(), testAcceptReq("session-busy", "msg-second", "h2"))
	require.ErrorIs(t, err, ErrSessionBusy,
		"second active execution for same session must return ErrSessionBusy")
}

func TestAccept_AfterTerminalAllowsNewExecution(t *testing.T) {
	t.Parallel()
	store, sessionStore := newTestSQLStore(t)
	rec := acceptAndRun(t, store, sessionStore, "session-seq", "msg-seq-1")
	require.NoError(t, store.FinishRuntime(context.Background(), rec.ExecutionID, testRun, RuntimeCompleted, ""))
	require.NoError(t, store.SetDelivery(context.Background(), rec.ExecutionID, testOwner, StatusDelivered, ""))

	_, _, err := store.Accept(context.Background(), testAcceptReq("session-seq", "msg-seq-2", "h2"))
	require.NoError(t, err, "new execution after terminal must be allowed")
}
