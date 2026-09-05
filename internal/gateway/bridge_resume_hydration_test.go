package gateway

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/events"
)

type resumeHydrationTestFactory struct {
	calls  atomic.Int32
	err    error
	result worker.Worker
}

func (f *resumeHydrationTestFactory) NewWorker(t worker.WorkerType) (worker.Worker, error) {
	f.calls.Add(1)
	if f.err != nil {
		return nil, f.err
	}
	if f.result != nil {
		return f.result, nil
	}
	return &mockBridgeWorker{
		workerType: t,
		conn:       &fakeWorkerConn{ch: make(chan *events.Envelope)},
	}, nil
}

type replayInputTrackingWorker struct {
	*mockBridgeWorker
	inputCalls atomic.Int32
}

func (w *replayInputTrackingWorker) Input(context.Context, string, map[string]any) error {
	w.inputCalls.Add(1)
	return nil
}

type attemptResumeFallbackTestFactory struct {
	calls   atomic.Int32
	workers []worker.Worker
}

func (f *attemptResumeFallbackTestFactory) NewWorker(t worker.WorkerType) (worker.Worker, error) {
	index := int(f.calls.Add(1)) - 1
	if index >= len(f.workers) {
		return nil, errors.New("unexpected worker factory call")
	}
	return f.workers[index], nil
}

func TestBridge_ResumeSession_HydrationFailurePreservesLifecycle(t *testing.T) {
	tests := []struct {
		name       string
		workerType worker.WorkerType
	}{
		{name: "claudecode", workerType: worker.TypeClaudeCode},
		{name: "codexcli", workerType: worker.TypeCodexCLI},
		{name: "opencodeserver", workerType: worker.TypeOpenCodeSrv},
		{name: "acp", workerType: worker.TypeACP},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sessionID := "resume-hydration-" + tt.name
			hub := newTestHub(t)
			hydrator := &mockSeqHydrator{err: errors.New("event store unavailable")}
			hub.SetSeqHydrator(hydrator)

			oldWorker := &mockBridgeWorker{
				workerType: tt.workerType,
				conn:       &fakeWorkerConn{ch: make(chan *events.Envelope)},
			}
			si := &session.SessionInfo{
				ID:         sessionID,
				UserID:     "u1",
				WorkerType: tt.workerType,
				State:      events.StateRunning,
			}
			sm := new(mockBridgeSM)
			sm.On("Get", sessionID).Return(si, nil).Once()
			sm.On("GetWorker", sessionID).Return(oldWorker).Maybe()
			sm.On("DetachWorker", sessionID).Maybe()

			factory := &resumeHydrationTestFactory{err: errors.New("factory must not be called")}
			b := NewBridge(BridgeDeps{Log: testLogger(t), Hub: hub, SM: sm})
			b.SetWorkerFactory(factory)

			err := b.ResumeSession(context.Background(), sessionID, "")
			require.ErrorIs(t, err, ErrResumeSequenceUnavailable)
			require.ErrorContains(t, err, "event store unavailable")
			require.Equal(t, events.StateRunning, si.State)
			require.False(t, oldWorker.terminated.Load())
			require.Zero(t, factory.calls.Load())
			require.Zero(t, hub.NextSeqPeek(sessionID))
			require.Equal(t, 1, hydrator.calls)
			sm.AssertNumberOfCalls(t, "GetWorker", 0)
			sm.AssertNumberOfCalls(t, "DetachWorker", 0)
			sm.AssertExpectations(t)
		})
	}
}

func TestBridge_StartPlatformSession_HydrationFailureDoesNotFallback(t *testing.T) {
	tests := []struct {
		name  string
		state events.SessionState
	}{
		{name: "idle", state: events.StateIdle},
		{name: "terminated", state: events.StateTerminated},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sessionID := "platform-resume-hydration-" + tt.name
			hub := newTestHub(t)
			hydrator := &mockSeqHydrator{err: errors.New("event store unavailable")}
			hub.SetSeqHydrator(hydrator)

			si := &session.SessionInfo{
				ID:         sessionID,
				UserID:     "u1",
				WorkerType: worker.TypeACP,
				State:      tt.state,
			}
			sm := new(mockBridgeSM)
			sm.On("Get", sessionID).Return(si, nil).Maybe()
			sm.On("GetWorker", sessionID).Return(nil).Maybe()
			// Keep the pre-fix fallback from panicking; the call count below is
			// the assertion that hydration failure must prevent a fresh start.
			sm.On(
				"CreateWithBot",
				mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything,
				mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything,
				mock.Anything, mock.Anything, mock.Anything,
			).Return(nil, errors.New("fresh fallback called")).Maybe()
			sm.On("Transition", mock.Anything, sessionID, events.StateRunning).Return(nil).Maybe()

			factory := &resumeHydrationTestFactory{err: errors.New("factory must not be called")}
			b := NewBridge(BridgeDeps{Log: testLogger(t), Hub: hub, SM: sm})
			b.SetWorkerFactory(factory)

			err := b.StartPlatformSession(context.Background(), worker.SessionStartParams{
				ID:         sessionID,
				UserID:     "u1",
				WorkerType: worker.TypeACP,
				Platform:   "slack",
				PlatformKey: map[string]string{
					"channel_id": "C-test",
				},
			})
			require.ErrorIs(t, err, ErrResumeSequenceUnavailable)
			require.ErrorContains(t, err, "event store unavailable")
			require.Equal(t, tt.state, si.State)
			require.Zero(t, factory.calls.Load())
			require.Zero(t, hub.NextSeqPeek(sessionID))
			require.Equal(t, 1, hydrator.calls)
			sm.AssertNumberOfCalls(t, "GetWorker", 1)
			sm.AssertNumberOfCalls(t, "CreateWithBot", 0)
			sm.AssertExpectations(t)
		})
	}
}

func TestBridge_ResumeSession_HydrationFailureCanRetry(t *testing.T) {
	t.Parallel()
	const sessionID = "resume-hydration-retry"

	hub := newTestHub(t)
	hydrator := &mockSeqHydrator{err: errors.New("event store unavailable")}
	hub.SetSeqHydrator(hydrator)
	si := &session.SessionInfo{
		ID:         sessionID,
		UserID:     "u1",
		WorkerType: worker.TypeClaudeCode,
		State:      events.StateRunning,
	}
	sm := new(mockBridgeSM)
	sm.On("Get", sessionID).Return(si, nil).Maybe()
	sm.On("GetWorker", sessionID).Return(nil).Maybe()
	sm.On("AttachWorker", sessionID, mock.Anything).Return(nil).Once()
	sm.On("ResetExpiry", mock.Anything, sessionID).Return(nil).Once()
	sm.On("DetachWorkerIf", sessionID, mock.Anything).Return(false).Maybe()

	resumedWorker := &mockBridgeWorker{
		workerType: worker.TypeClaudeCode,
		conn:       &fakeWorkerConn{ch: make(chan *events.Envelope)},
	}
	factory := &resumeHydrationTestFactory{err: errors.New("factory must not be called")}
	b := NewBridge(BridgeDeps{Log: testLogger(t), Hub: hub, SM: sm})
	b.SetWorkerFactory(factory)

	err := b.ResumeSession(context.Background(), sessionID, "")
	require.ErrorIs(t, err, ErrResumeSequenceUnavailable)
	require.ErrorContains(t, err, "event store unavailable")
	require.Zero(t, factory.calls.Load())
	require.Equal(t, int64(0), hub.NextSeqPeek(sessionID))
	require.False(t, hub.seqGen.IsHydrated(sessionID))

	hydrator.err = nil
	hydrator.seq = 41
	factory.err = nil
	factory.result = resumedWorker
	require.NoError(t, b.ResumeSession(context.Background(), sessionID, ""))
	require.Equal(t, int32(1), factory.calls.Load())
	require.Equal(t, 2, hydrator.calls)
	require.True(t, hub.seqGen.IsHydrated(sessionID))
	require.GreaterOrEqual(t, hub.NextSeqPeek(sessionID), int64(41))

	sm.AssertExpectations(t)
	b.closed.Store(true)
	close(resumedWorker.conn.ch)
	b.WaitForwarders(context.Background())
}

func TestBridge_AttemptResumeFallback_SequenceHydrationFailureStopsFreshFallback(t *testing.T) {
	t.Parallel()
	const sessionID = "resume-hydration-crash-recovery"

	hub := newTestHub(t)
	hydrator := &mockSeqHydrator{err: errors.New("event store unavailable")}
	hub.SetSeqHydrator(hydrator)
	si := &session.SessionInfo{
		ID:         sessionID,
		UserID:     "u1",
		WorkerType: worker.TypeClaudeCode,
		State:      events.StateRunning,
	}
	sm := new(mockBridgeSM)
	sm.On("Get", sessionID).Return(si, nil).Maybe()
	sm.On("GetWorker", sessionID).Return(nil).Maybe()
	sm.On("DetachWorker", sessionID).Maybe()
	sm.On("Transition", mock.Anything, sessionID, events.StateTerminated).Return(nil).Maybe()
	sm.On("Transition", mock.Anything, sessionID, events.StateRunning).Return(nil).Maybe()
	sm.On("AttachWorker", sessionID, mock.Anything).Return(nil).Maybe()
	sm.On("DetachWorkerIf", sessionID, mock.Anything).Return(false).Maybe()

	resumeWorker := &mockBridgeWorker{
		workerType: worker.TypeClaudeCode,
		conn:       &fakeWorkerConn{ch: make(chan *events.Envelope)},
		resumeErr:  errors.New("resume failed after hydration"),
	}
	freshWorker := &replayInputTrackingWorker{
		mockBridgeWorker: &mockBridgeWorker{
			workerType: worker.TypeClaudeCode,
			conn:       &fakeWorkerConn{ch: make(chan *events.Envelope)},
		},
	}
	factory := &attemptResumeFallbackTestFactory{workers: []worker.Worker{resumeWorker, freshWorker}}
	b := NewBridge(BridgeDeps{Log: testLogger(t), Hub: hub, SM: sm})
	b.SetWorkerFactory(factory)
	t.Cleanup(func() {
		b.closed.Store(true)
		close(resumeWorker.conn.ch)
		close(freshWorker.conn.ch)
		b.WaitForwarders(context.Background())
	})

	recovered := b.attemptResumeFallback(fallbackParams{
		sessionID:  sessionID,
		workerType: worker.TypeClaudeCode,
		exitCode:   1,
		lastInput:  "original input must not be replayed",
	})

	require.False(t, recovered)
	require.Zero(t, factory.calls.Load())
	require.Zero(t, freshWorker.inputCalls.Load())
	require.Zero(t, hub.NextSeqPeek(sessionID))
	require.Equal(t, 1, hydrator.calls)
	sm.AssertNumberOfCalls(t, "GetWorker", 0)
	sm.AssertNumberOfCalls(t, "AttachWorker", 0)
	sm.AssertNumberOfCalls(t, "DetachWorker", 1)
	sm.AssertExpectations(t)
}
