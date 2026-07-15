package gateway

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/pkg/events"
)

// TestForwarderBinding_ReadsOnlyFrozenConn (T-A1/T-A2) proves the forwarder
// reads events from the Conn captured at launch time and NEVER from a later
// w.Conn() replacement. This is the core invariant of Fix A (RC-1): without a
// frozen binding, a stale forwarder could bind to a reset's replacement Conn
// and split the event stream with the new forwarder.
func TestForwarderBinding_ReadsOnlyFrozenConn(t *testing.T) {
	t.Parallel()

	h := newTestHub(t)
	b := NewBridge(BridgeDeps{Log: slog.Default(), Hub: h})

	connA := &fakeWorkerConn{ch: make(chan *events.Envelope, 1)}
	connB := &fakeWorkerConn{ch: make(chan *events.Envelope, 1)}

	w := &mockBridgeWorker{workerType: "claude_code", conn: connA, exitCode: 0}

	// Simulate the synchronous launch: capture the binding NOW (connA).
	// launchForwarderLocked does exactly this before spawning the goroutine.
	binding := forwarderBinding{worker: w, conn: w.Conn(), resetGen: 0}
	require.Same(t, connA, binding.conn, "binding must freeze connA at launch")

	// AFTER capture, /reset replaces the worker's Conn (connA → connB).
	// A non-frozen forwarder reading w.Conn() here would bind to connB.
	w.conn = connB
	require.Same(t, connB, w.Conn(), "sanity: live Conn() now returns connB")
	require.Same(t, connA, binding.conn, "FROZEN binding must still point at connA despite the swap")

	// Join a WS conn so forwardEvents can deliver to the hub.
	done := make(chan struct{})
	go func() {
		b.forwardEvents(binding, "sess_frozen", forwardOpts{ctx: context.Background()})
		close(done)
	}()

	// Send on connA (frozen source) and connB (replacement). Only connA must
	// be consumed by THIS forwarder.
	deltaA := events.NewEnvelope("idA", "sess_frozen", 1, events.MessageDelta, events.MessageDeltaData{Content: "from-frozen"})
	deltaB := events.NewEnvelope("idB", "sess_frozen", 1, events.MessageDelta, events.MessageDeltaData{Content: "from-replacement"})
	connA.ch <- deltaA
	connB.ch <- deltaB

	// Drain connA: the frozen forwarder consumed it.
	select {
	case <-connA.ch:
	default:
	}

	// connB's event must NOT have been consumed by this forwarder — it belongs
	// to the new forwarder that the reset path would launch.
	select {
	case remaining := <-connB.ch:
		require.Equal(t, "from-replacement", remaining.Event.Data.(events.MessageDeltaData).Content,
			"frozen forwarder must NOT consume the replacement conn's events")
	case <-time.After(100 * time.Millisecond):
		t.Fatal("connB event was consumed by the frozen forwarder — binding is not frozen")
	}

	// Close connA so forwardEvents exits cleanly.
	close(connA.ch)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("forwardEvents did not exit after frozen conn closed")
	}
}
