package gateway

import (
	"context"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/execution"
	"github.com/hrygo/hotplex/pkg/events"
)

// fakeReplayer is a test double for PendingReplayer. calls counts
// DeliverReplay invocations; lastContent captures the content of the most
// recent replay envelope. When callCh is non-nil, it is closed on the first
// call so the negative test can detect a spurious replay from a t.Cleanup
// select — without any time.Sleep.
type fakeReplayer struct {
	calls       atomic.Int32
	lastContent atomic.Value // string
	callCh      chan struct{}
}

func (r *fakeReplayer) DeliverReplay(_ context.Context, env *events.Envelope) error {
	r.calls.Add(1)
	if data, ok := env.Event.Data.(map[string]any); ok {
		if c, _ := data["content"].(string); c != "" {
			r.lastContent.Store(c)
		}
	}
	if r.callCh != nil {
		select {
		case <-r.callCh:
		default:
			close(r.callCh)
		}
	}
	return nil
}

// newDoneBridgeWithOpenExec builds a Bridge + forwardContext + Done envelope
// backed by a fake execution store returning an open Delivered record that
// FinishRuntime accepts. Mirrors the harness in
// TestFinishRuntimeOnDone_UsesEmittingForwarderRunID; the pending buffer is
// initialized (the production Bridge literal leaves it nil).
func newDoneBridgeWithOpenExec(t *testing.T, sessionID string) (*Bridge, *forwardContext, *events.Envelope, *fakeExecutionStore) {
	t.Helper()
	rec := testExecutionRecord(execution.StatusDelivered)
	rec.SessionID = sessionID
	rec.WorkerRunID = "run-current"
	store := &fakeExecutionStore{openRecord: rec}
	b := &Bridge{
		executionStore: store,
		hub:            newTestHub(t),
		log:            testLogger(t),
		pending:        NewPendingBuffer(),
	}
	fc := &forwardContext{sessionID: sessionID, workerRunID: "run-current"}
	env := events.NewEnvelope("evt-done", sessionID, 1, events.Done, events.DoneData{Success: true})
	return b, fc, env, store
}

// TestFinishRuntimeOnDone_ReplaysPending verifies that when Done arrives and
// the active gate is released, a buffered supplement is drained, merged, and
// replayed exactly once via the PendingReplayer. The replay goroutine runs
// async (seq-lease safety), so we poll via require.Eventually — no sleep.
func TestFinishRuntimeOnDone_ReplaysPending(t *testing.T) {
	t.Parallel()
	b, fc, env, _ := newDoneBridgeWithOpenExec(t, "s-replay")
	rp := &fakeReplayer{}
	b.SetPendingReplayer(rp)
	// Buffer a supplement that the Done path must drain + replay.
	b.pending.Append("s-replay", "追问", newInputEnvelope(t, "s-replay", "x"))

	b.finishRuntimeOnDone("s-replay", fc, env)

	// Poll the actual asserted condition (call count AND replayed content) in a
	// single predicate, rather than checking calls then content separately:
	// calls.Add(1) happens before lastContent.Store inside DeliverReplay, so a
	// standalone calls==1 gate can observe the call before the Store is visible,
	// racing the subsequent content assertion under -race load.
	require.Eventually(t, func() bool {
		c, _ := rp.lastContent.Load().(string)
		return rp.calls.Load() == 1 && c == "追问"
	}, time.Second, 10*time.Millisecond)
	require.Equal(t, "追问", rp.lastContent.Load().(string))
}

// TestFinishRuntimeOnDone_NoPendingNoReplay verifies that with an empty
// pending buffer, finishRuntimeOnDone does NOT spawn a replay goroutine.
// Detection uses NO time.Sleep: the fakeReplayer closes callCh if ever
// invoked, and t.Cleanup's select catches the spurious call. A runtime.Gosched
// yields the test goroutine so any spawned replay has a chance to fire before
// cleanup runs — this is a scheduler hint, not a sleep.
func TestFinishRuntimeOnDone_NoPendingNoReplay(t *testing.T) {
	t.Parallel()
	b, fc, env, _ := newDoneBridgeWithOpenExec(t, "s-nopending")
	rp := &fakeReplayer{callCh: make(chan struct{})}
	b.SetPendingReplayer(rp)

	b.finishRuntimeOnDone("s-nopending", fc, env)
	// Yield so a (buggy) spawned goroutine can fire and close callCh before
	// cleanup's select. Not a sleep — just a scheduling hint.
	runtime.Gosched()

	t.Cleanup(func() {
		select {
		case <-rp.callCh:
			t.Errorf("unexpected replay call")
		default:
		}
	})
}
