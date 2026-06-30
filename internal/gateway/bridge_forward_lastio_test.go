package gateway

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/aep"
	"github.com/hrygo/hotplex/pkg/events"
)

// TestBridge_ForwardEvents_RefreshesLastIO verifies that forwardEvents bumps
// the worker's LastIO on each non-Done event. Without this, a long codex turn
// (>30m) is wrongly reaped by the zombie scanner because LastIO stays frozen
// at the input time (issue #815, spec Fix C).
func TestBridge_ForwardEvents_RefreshesLastIO(t *testing.T) {
	t.Parallel()

	ch := make(chan *events.Envelope, 1)
	deltaEnv := events.NewEnvelope(aep.NewID(), "", 0, events.MessageDelta,
		events.MessageDeltaData{MessageID: aep.NewID(), Content: "chunk"})
	ch <- deltaEnv
	close(ch)

	fw := &mockBridgeWorker{
		workerType: worker.TypeClaudeCode,
		conn:       &fakeWorkerConn{ch: ch},
	}
	// Sentinel: freeze LastIO far in the past so any refresh is detectable.
	frozen := time.Now().Add(-2 * time.Hour)
	fw.SetLastIO(frozen)
	require.Equal(t, frozen, fw.LastIO())

	h := newTestHub(t)
	conn, server := newTestWSConnPair(t)
	defer conn.Close()
	defer server.Close()
	c := newConn(h, conn, "sess_lastio", nil)
	h.JoinSession("sess_lastio", c)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	b := NewBridge(BridgeDeps{Log: slog.Default(), Hub: h})
	done := make(chan struct{})
	go func() {
		b.forwardEvents(fw, "sess_lastio", forwardOpts{ctx: ctx})
		close(done)
	}()

	// Drain the forwarded event so forwardEvents processes the delta and exits.
	_ = server.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, _ = server.ReadMessage()
	<-done

	// LastIO must have advanced well past the frozen sentinel.
	require.True(t, fw.LastIO().After(frozen.Add(time.Hour)),
		"forwardEvents must refresh worker LastIO on non-Done events (Fix C); got %v, frozen at %v",
		fw.LastIO(), frozen)
}
