package gateway

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/execution"
	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/events"
)

type fakeExecutionStore struct {
	mu           sync.Mutex
	record       *execution.Record
	duplicate    bool
	acceptErr    error
	statusErr    error
	lastAccept   execution.AcceptRequest
	status       execution.Status
	errorCode    string
	statusCalls  int
	openRecord   *execution.Record
	activeRecord *execution.Record // when set, ActiveBySession reports the gate as held
	markRunID    string
	markErr      error
	finishRunID  string
	finishStatus execution.RuntimeStatus
	finishErr    error
}

func (s *fakeExecutionStore) Accept(_ context.Context, req execution.AcceptRequest) (*execution.Record, bool, error) {
	s.mu.Lock()
	s.lastAccept = req
	s.mu.Unlock()
	return s.record, s.duplicate, s.acceptErr
}

func (s *fakeExecutionStore) SetStatus(_ context.Context, _ string, status execution.Status, errorCode string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = status
	s.errorCode = errorCode
	s.statusCalls++
	return s.statusErr
}

func (s *fakeExecutionStore) SetDelivery(_ context.Context, _ string, _ string, status execution.Status, errorCode string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = status
	s.errorCode = errorCode
	s.statusCalls++
	return s.statusErr
}

func (s *fakeExecutionStore) MarkRunning(_ context.Context, _ string, _ string, runID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.markRunID = runID
	return s.markErr
}
func (s *fakeExecutionStore) FinishRuntime(_ context.Context, _ string, runID string, status execution.RuntimeStatus, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.finishRunID = runID
	s.finishStatus = status
	return s.finishErr
}
func (s *fakeExecutionStore) ActiveBySession(context.Context, string) (*execution.Record, error) {
	if s.activeRecord != nil {
		return s.activeRecord, nil
	}
	return nil, execution.ErrNotFound
}
func (s *fakeExecutionStore) OpenBySession(context.Context, string) (*execution.Record, error) {
	if s.openRecord != nil {
		return s.openRecord, nil
	}
	return nil, execution.ErrNotFound
}
func (s *fakeExecutionStore) FenceBySession(context.Context, string) (*execution.Record, error) {
	return nil, execution.ErrNotFound
}
func (s *fakeExecutionStore) ClearFenceAfterFreshStart(context.Context, string, string, string) error {
	return nil
}
func (s *fakeExecutionStore) RenewLeases(context.Context, string, int64, []string) (int64, error) {
	return 0, nil
}
func (s *fakeExecutionStore) RecoverExpiredLeases(context.Context, []string) (execution.LeaseRecoveryResult, error) {
	return execution.LeaseRecoveryResult{}, nil
}
func (s *fakeExecutionStore) TerminateOwnerLeases(context.Context, string, string) (int64, error) {
	return 0, nil
}

func (s *fakeExecutionStore) snapshot() (execution.Status, string, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status, s.errorCode, s.statusCalls
}

func newExecutionHandler(t *testing.T, store execution.Store, workerInputError error) (*Handler, *mockInputSM, *mockWorkerForHandler, *mockPlatformConn) {
	t.Helper()
	sm := new(mockInputSM)
	w := new(mockWorkerForHandler)
	sm.On("Get", "s-exec").Return(&session.SessionInfo{State: events.StateRunning, Platform: "webchat"}, nil)
	sm.On("GetWorker", "s-exec").Return(w).Maybe()
	w.On("Input", mock.Anything, "hello", mock.Anything).Return(workerInputError).Maybe()

	hub := newTestHub(t)
	conn := &mockPlatformConn{}
	hub.JoinPlatformSession("s-exec", conn)
	return &Handler{log: testLogger(t), hub: hub, sm: sm, executionStore: store}, sm, w, conn
}

func testExecutionRecord(status execution.Status) *execution.Record {
	return &execution.Record{
		ExecutionID:     "exec_test",
		SessionID:       "s-exec",
		ClientMessageID: "evt-client-1",
		PayloadHash:     "hash",
		Status:          status,
		WorkerRunID:     "run-provisional",
	}
}

func requireInputAcks(t *testing.T, conn *mockPlatformConn, statuses ...events.ExecutionStatus) {
	t.Helper()
	require.Eventually(t, func() bool { return len(conn.envelopes()) == len(statuses) }, time.Second, 10*time.Millisecond)
	for i, ack := range conn.envelopes() {
		require.Equal(t, events.InputAck, ack.Event.Type)
		data, ok := ack.Event.Data.(events.InputAckData)
		require.True(t, ok)
		require.Equal(t, "evt-client-1", data.ClientMessageID)
		require.Equal(t, "exec_test", data.ExecutionID)
		require.Equal(t, statuses[i], data.Status)
	}
}

func TestInputExecution_DeliveredAckAfterWorkerAccepts(t *testing.T) {
	t.Parallel()
	store := &fakeExecutionStore{record: testExecutionRecord(execution.StatusAccepted)}
	h, sm, w, conn := newExecutionHandler(t, store, nil)
	env := inputEnvelope("s-exec", "hello")
	env.ID = "evt-client-1"

	require.NoError(t, h.handleInput(context.Background(), env))
	requireInputAcks(t, conn, events.ExecutionStatusAccepted, events.ExecutionStatusDelivered)
	require.False(t, conn.envelopes()[0].Event.Data.(events.InputAckData).Duplicate)
	status, code, calls := store.snapshot()
	require.Equal(t, execution.StatusDelivered, status)
	require.Empty(t, code)
	require.Equal(t, 1, calls)
	sm.AssertExpectations(t)
	w.AssertExpectations(t)
}

func TestAcceptInputExecution_UsesAttachedWorkerRunID(t *testing.T) {
	t.Parallel()
	store := &fakeExecutionStore{record: testExecutionRecord(execution.StatusAccepted)}
	sm := new(mockBridgeSM)
	w := new(mockWorkerForHandler)
	sm.On("GetWorker", "s-exec").Return(w)
	b := &Bridge{sm: sm}
	b.workerRuns.Store("s-exec", workerRunBinding{worker: w, id: "run-attached"})
	h := &Handler{executionStore: store, bridge: b, ownerInstanceID: "gw-test"}
	env := inputEnvelope("s-exec", "hello")
	env.ID = "evt-client-1"

	_, _, err := h.acceptInputExecution(context.Background(), env)
	require.NoError(t, err)
	require.Equal(t, "run-attached", store.lastAccept.WorkerRunID)
}

func TestInputExecution_DispatchUsesAtomicBridgeBinding(t *testing.T) {
	t.Parallel()
	store := &fakeExecutionStore{record: testExecutionRecord(execution.StatusAccepted)}
	handlerSM := new(mockInputSM)
	handlerSM.On("Get", "s-exec").Return(&session.SessionInfo{State: events.StateRunning, Platform: "webchat"}, nil)
	bridgeSM := new(mockBridgeSM)
	boundWorker := new(mockWorkerForHandler)
	bridgeSM.On("GetWorker", "s-exec").Return(boundWorker)
	boundWorker.On("Input", mock.Anything, "hello", mock.Anything).Return(nil)
	b := &Bridge{sm: bridgeSM, log: testLogger(t)}
	b.workerRuns.Store("s-exec", workerRunBinding{worker: boundWorker, id: "run-bound"})

	hub := newTestHub(t)
	conn := &mockPlatformConn{}
	hub.JoinPlatformSession("s-exec", conn)
	h := &Handler{
		log: testLogger(t), hub: hub, sm: handlerSM, bridge: b,
		executionStore: store, ownerInstanceID: "gw-test",
	}
	env := inputEnvelope("s-exec", "hello")
	env.ID = "evt-client-1"

	require.NoError(t, h.handleInput(context.Background(), env))
	require.Equal(t, "run-bound", store.markRunID)
	boundWorker.AssertExpectations(t)
	handlerSM.AssertNotCalled(t, "GetWorker", mock.Anything)
}

func TestInputExecution_RefreshesSessionStateAfterAcceptance(t *testing.T) {
	t.Parallel()
	store := &fakeExecutionStore{record: testExecutionRecord(execution.StatusAccepted)}
	sm := new(mockBridgeSM)
	w := new(mockWorkerForHandler)
	sm.On("Get", "s-exec").Return(&session.SessionInfo{
		ID: "s-exec", State: events.StateTerminated, Platform: "webchat",
	}, nil).Once()
	sm.On("Get", "s-exec").Return(&session.SessionInfo{
		ID: "s-exec", State: events.StateRunning, Platform: "webchat",
	}, nil)
	sm.On("GetWorker", "s-exec").Return(w)
	w.On("Input", mock.Anything, "hello", mock.Anything).Return(nil)
	b := &Bridge{sm: sm, log: testLogger(t)}
	b.workerRuns.Store("s-exec", workerRunBinding{worker: w, id: "run-fresh"})

	hub := newTestHub(t)
	conn := &mockPlatformConn{}
	hub.JoinPlatformSession("s-exec", conn)
	h := &Handler{
		log: testLogger(t), hub: hub, sm: sm, bridge: b,
		executionStore: store, ownerInstanceID: "gw-test",
	}
	env := inputEnvelope("s-exec", "hello")
	env.ID = "evt-client-1"

	require.NoError(t, h.handleInput(context.Background(), env))
	require.Equal(t, "run-fresh", store.markRunID)
	sm.AssertNotCalled(t, "Transition", mock.Anything, mock.Anything, mock.Anything)
	w.AssertExpectations(t)
}

func TestInputExecution_MarkRunningFailureDoesNotDispatch(t *testing.T) {
	t.Parallel()
	store := &fakeExecutionStore{
		record:  testExecutionRecord(execution.StatusAccepted),
		markErr: errors.New("db unavailable"),
	}
	h, sm, w, conn := newExecutionHandler(t, store, nil)
	h.ownerInstanceID = "gw-test"
	env := inputEnvelope("s-exec", "hello")
	env.ID = "evt-client-1"

	require.ErrorContains(t, h.handleInput(context.Background(), env), "execution dispatch registration failed")
	require.Eventually(t, func() bool { return len(conn.envelopes()) == 3 }, time.Second, 10*time.Millisecond)
	for i, status := range []events.ExecutionStatus{events.ExecutionStatusAccepted, events.ExecutionStatusFailed} {
		data, ok := conn.envelopes()[i].Event.Data.(events.InputAckData)
		require.True(t, ok)
		require.Equal(t, status, data.Status)
	}
	w.AssertNotCalled(t, "Input", mock.Anything, mock.Anything, mock.Anything)
	require.Equal(t, "run-provisional", store.finishRunID)
	require.Equal(t, execution.RuntimeFailed, store.finishStatus)
	sm.AssertExpectations(t)
}

func TestFinishRuntimeOnDone_UsesEmittingForwarderRunID(t *testing.T) {
	t.Parallel()
	rec := testExecutionRecord(execution.StatusDelivered)
	rec.WorkerRunID = "run-current"
	store := &fakeExecutionStore{openRecord: rec}
	b := &Bridge{executionStore: store, hub: newTestHub(t), log: testLogger(t)}
	fc := &forwardContext{sessionID: "s-exec", workerRunID: "run-stale"}
	env := events.NewEnvelope("evt-done", "s-exec", 1, events.Done, events.DoneData{Success: true})

	b.finishRuntimeOnDone("s-exec", fc, env)
	require.Equal(t, "run-stale", store.finishRunID)
}

func TestInputExecution_DuplicateReplaysAckWithoutWorkerCall(t *testing.T) {
	t.Parallel()
	store := &fakeExecutionStore{record: testExecutionRecord(execution.StatusDelivered), duplicate: true}
	h, sm, w, conn := newExecutionHandler(t, store, nil)
	retryCancel := make(chan struct{})
	h.bridge = &Bridge{retryCancel: map[string]chan struct{}{"s-exec": retryCancel}}
	env := inputEnvelope("s-exec", "hello")
	env.ID = "evt-client-1"

	require.NoError(t, h.handleInput(context.Background(), env))
	requireInputAcks(t, conn, events.ExecutionStatusDelivered)
	require.True(t, conn.envelopes()[0].Event.Data.(events.InputAckData).Duplicate)
	_, _, calls := store.snapshot()
	require.Zero(t, calls)
	sm.AssertExpectations(t)
	sm.AssertNotCalled(t, "GetWorker", mock.Anything)
	w.AssertNotCalled(t, "Input", mock.Anything, mock.Anything, mock.Anything)
	select {
	case <-retryCancel:
		t.Fatal("duplicate input cancelled the active LLM retry")
	default:
	}
	_, retryStillPending := h.bridge.retryCancel["s-exec"]
	require.True(t, retryStillPending)
}

func TestInputExecution_DuplicateDoesNotResumeTerminatedSession(t *testing.T) {
	t.Parallel()
	store := &fakeExecutionStore{record: testExecutionRecord(execution.StatusDelivered), duplicate: true}
	sm := new(mockInputSM)
	sm.On("Get", "s-exec").Return(&session.SessionInfo{
		State:    events.StateTerminated,
		Platform: "webchat",
	}, nil)
	hub := newTestHub(t)
	conn := &mockPlatformConn{}
	hub.JoinPlatformSession("s-exec", conn)
	h := &Handler{
		log:            testLogger(t),
		hub:            hub,
		sm:             sm,
		bridge:         &Bridge{retryCancel: make(map[string]chan struct{})},
		executionStore: store,
	}
	env := inputEnvelope("s-exec", "hello")
	env.ID = "evt-client-1"

	require.NoError(t, h.handleInput(context.Background(), env))
	requireInputAcks(t, conn, events.ExecutionStatusDelivered)
	require.True(t, conn.envelopes()[0].Event.Data.(events.InputAckData).Duplicate)
	sm.AssertExpectations(t)
	sm.AssertNotCalled(t, "GetWorker", mock.Anything)
}

func TestInputExecution_TimeoutBecomesUnknown(t *testing.T) {
	t.Parallel()
	store := &fakeExecutionStore{record: testExecutionRecord(execution.StatusAccepted)}
	timeoutErr := &worker.WorkerError{Kind: worker.ErrKindTimeout, Message: "worker response timed out"}
	h, sm, w, conn := newExecutionHandler(t, store, timeoutErr)
	env := inputEnvelope("s-exec", "hello")
	env.ID = "evt-client-1"

	require.NoError(t, h.handleInput(context.Background(), env))
	requireInputAcks(t, conn, events.ExecutionStatusAccepted, events.ExecutionStatusUnknown)
	status, code, calls := store.snapshot()
	require.Equal(t, execution.StatusUnknown, status)
	require.Equal(t, string(events.ErrCodeExecutionTimeout), code)
	require.Equal(t, 1, calls)
	sm.AssertExpectations(t)
	w.AssertExpectations(t)
}

// TestInputExecution_AckReflectsIntendedStatusWhenSetStatusFails guards the
// C1 fix: when the durable SetStatus write fails, the terminal input.ack must
// still carry the intended outcome (unknown) rather than a stale 'accepted'.
// Otherwise the client would wait for a terminal ack that never arrives.
func TestInputExecution_AckReflectsIntendedStatusWhenSetStatusFails(t *testing.T) {
	t.Parallel()
	store := &fakeExecutionStore{
		record:    testExecutionRecord(execution.StatusAccepted),
		statusErr: errors.New("db unavailable"),
	}
	timeoutErr := &worker.WorkerError{Kind: worker.ErrKindTimeout, Message: "worker response timed out"}
	h, sm, w, conn := newExecutionHandler(t, store, timeoutErr)
	env := inputEnvelope("s-exec", "hello")
	env.ID = "evt-client-1"

	require.NoError(t, h.handleInput(context.Background(), env))
	requireInputAcks(t, conn, events.ExecutionStatusAccepted, events.ExecutionStatusUnknown)
	_, _, calls := store.snapshot()
	require.Equal(t, 1, calls)
	sm.AssertExpectations(t)
	w.AssertExpectations(t)
}
