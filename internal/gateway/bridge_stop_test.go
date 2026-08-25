package gateway

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/worker"
	noopworker "github.com/hrygo/hotplex/internal/worker/noop"
	"github.com/hrygo/hotplex/pkg/events"
)

func TestBridge_StopAndDisposeCurrentRun_EventBarrierHonorsTeardownTimeout(t *testing.T) {
	_, mgr, hub, _ := newHandlerWithRealStore(t)

	const sid = "sess_stop_event_barrier_timeout"
	_, err := mgr.Create(context.Background(), sid, "user1", worker.TypeClaudeCode, nil, "", "")
	require.NoError(t, err)
	require.NoError(t, mgr.Transition(context.Background(), sid, events.StateRunning))

	w := new(mockWorkerForHandler)
	w.conn = noopworker.NewConn(sid, "user1")
	w.On("StopCurrentTurn", mock.Anything).Return(errors.New("stop must not be called")).Maybe()
	w.On("Terminate", mock.Anything).Return(nil).Maybe()
	mgr.AttachWorker(context.Background(), sid, w)

	bridge := NewBridge(BridgeDeps{
		Log:                  slog.Default(),
		Hub:                  hub,
		SM:                   mgr,
		StopTeardownTimeout:  50 * time.Millisecond,
		StopForwarderTimeout: 50 * time.Millisecond,
	})
	binding := bridge.bindWorkerRun(sid, w, "run-event-barrier-timeout")
	releaseEvent, admitted := binding.lifecycle.beginEvent()
	require.True(t, admitted)

	done := make(chan error, 1)
	go func() {
		done <- bridge.StopAndDisposeCurrentRun(context.Background(), sid, binding.id)
	}()

	var stopErr error
	select {
	case stopErr = <-done:
	case <-time.After(250 * time.Millisecond):
		releaseEvent()
		stopErr = <-done
		require.FailNow(t, "event barrier acquisition exceeded the teardown budget")
	}
	releaseEvent()

	require.Error(t, stopErr)
	require.Contains(t, stopErr.Error(), "event barrier")
	w.AssertNotCalled(t, "StopCurrentTurn", mock.Anything)
	_, _, stillBound := bridge.CurrentWorkerBinding(sid)
	require.True(t, stillBound, "a pre-cancel barrier timeout must retain the live run")
}
