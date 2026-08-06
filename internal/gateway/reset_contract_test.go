package gateway

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/aep"
	"github.com/hrygo/hotplex/pkg/events"
)

// connReplacingWorker is a mockBridgeWorker whose ResetContext swaps its Conn to
// a fresh channel and (optionally) reports ConnReplaced=true, bumping a reset
// generation like real per-process workers (Claude Code / Codex CLI).
type connReplacingWorker struct {
	mockBridgeWorker
	resetGen atomic.Int64
	replaced bool
}

func (w *connReplacingWorker) LoadResetGeneration() int64 { return w.resetGen.Load() }
func (w *connReplacingWorker) IncResetGeneration() int64  { return w.resetGen.Add(1) }

func (w *connReplacingWorker) ResetContext(context.Context) (worker.ResetResult, error) {
	w.resetGen.Add(1)
	res := worker.ResetResult{}
	if w.replaced {
		// Per-process workers rebuild their transport so the new forwarder has
		// its own channel. In-place workers keep consuming the original conn.
		w.conn = &fakeWorkerConn{ch: make(chan *events.Envelope, 8)}
		res.ConnReplaced = true
	}
	return res, nil
}

var _ worker.ResetGenerationer = (*connReplacingWorker)(nil)

// readN reads up to n WS messages within a deadline, returning the contents.
func readN(t *testing.T, server interface {
	ReadMessage() (int, []byte, error)
	SetReadDeadline(time.Time) error
}, n int, timeout time.Duration) []string {
	t.Helper()
	var out []string
	deadline := time.Now().Add(timeout)
	for len(out) < n {
		_ = server.SetReadDeadline(deadline)
		_, data, err := server.ReadMessage()
		if err != nil {
			break
		}
		out = append(out, string(data))
	}
	_ = server.SetReadDeadline(time.Time{})
	return out
}

// TestResetSession_ConnReplaced_LaunchesSingleForwarder (T-A5, §9 跨 Worker 验收):
// a per-process /reset (ConnReplaced=true) must launch exactly ONE new forwarder
// on the replacement Conn. Events injected on the new Conn are delivered in order
// and processed exactly once — no dual consumer splits or duplicates them.
func TestResetSession_ConnReplaced_LaunchesSingleForwarder(t *testing.T) {
	t.Parallel()
	hub := newTestHub(t)
	conn, server := newTestWSConnPair(t)
	t.Cleanup(func() { _ = conn.Close(); _ = server.Close() })
	c := newConn(hub, conn, "sess-reset-replaced", nil)
	hub.JoinSession("sess-reset-replaced", c)

	w := &connReplacingWorker{replaced: true}
	w.workerType = worker.TypeClaudeCode
	w.conn = &fakeWorkerConn{ch: make(chan *events.Envelope, 8)} // original connA
	sm := new(mockBridgeSM)
	sm.On("GetWorker", "sess-reset-replaced").Return(w)
	sm.On("Get", "sess-reset-replaced").Return(&session.SessionInfo{ID: "sess-reset-replaced", Platform: "webchat"}, nil)
	b := NewBridge(BridgeDeps{Log: slog.Default(), Hub: hub, SM: sm})
	b.workerRuns.Store("sess-reset-replaced", workerRunBinding{worker: w, id: "run-old"})

	// Drive the reset. ResetContext swaps the conn (connA→connB) and reports
	// ConnReplaced=true, so the bridge must launch one new forwarder on connB.
	require.NoError(t, b.ResetSession(context.Background(), "sess-reset-replaced"))

	// Inject three ordered events on the NEW conn (connB).
	newConn := w.Conn().(*fakeWorkerConn)
	for i, txt := range []string{"delta-1", "delta-2", "delta-3"} {
		newConn.ch <- events.NewEnvelope(aep.NewID(), "sess-reset-replaced", 0,
			events.MessageDelta, events.MessageDeltaData{Content: txt, MessageID: "msg_new"})
		_ = i
	}

	got := readN(t, server, 3, 2*time.Second)
	require.Len(t, got, 3, "all three events on the replacement conn must be delivered (single consumer, in order)")
	require.Contains(t, got[0], "delta-1")
	require.Contains(t, got[1], "delta-2")
	require.Contains(t, got[2], "delta-3")

	// No fifth message arrives — proves there is no second consumer duplicating.
	if extra := readN(t, server, 1, 250*time.Millisecond); len(extra) > 0 {
		t.Fatalf("unexpected duplicate/extra event from a second consumer: %v", extra)
	}
}

// TestResetSession_InPlace_NoNewForwarder (T-A6, §9 跨 Worker 验收): an in-place
// reset (OCS / ACP, ConnReplaced=false) must NOT launch a second forwarder. The
// existing forwarder keeps consuming the same Conn.
func TestResetSession_InPlace_NoNewForwarder(t *testing.T) {
	t.Parallel()
	hub := newTestHub(t)
	conn, server := newTestWSConnPair(t)
	t.Cleanup(func() { _ = conn.Close(); _ = server.Close() })
	c := newConn(hub, conn, "sess-reset-inplace", nil)
	hub.JoinSession("sess-reset-inplace", c)

	w := &connReplacingWorker{replaced: false} // in-place: ConnReplaced=false
	w.workerType = worker.TypeOpenCodeSrv
	origConn := &fakeWorkerConn{ch: make(chan *events.Envelope, 8)}
	w.conn = origConn
	sm := new(mockBridgeSM)
	sm.On("GetWorker", "sess-reset-inplace").Return(w)
	sm.On("Get", "sess-reset-inplace").Return(&session.SessionInfo{ID: "sess-reset-inplace", Platform: "webchat"}, nil)
	b := NewBridge(BridgeDeps{Log: slog.Default(), Hub: hub, SM: sm})
	b.workerRuns.Store("sess-reset-inplace", workerRunBinding{worker: w, id: "run-stable"})

	// Start the single forwarder the in-place worker is supposed to keep.
	done := make(chan struct{})
	go func() {
		b.forwardEvents(forwarderBinding{worker: w, conn: origConn}, "sess-reset-inplace", forwardOpts{ctx: context.Background()})
		close(done)
	}()

	// In-place reset: ConnReplaced=false → no new forwarder, conn unchanged.
	require.NoError(t, b.ResetSession(context.Background(), "sess-reset-inplace"))

	// The SAME conn still has exactly one consumer: events still deliver in order.
	for _, txt := range []string{"inplace-1", "inplace-2"} {
		origConn.ch <- events.NewEnvelope(aep.NewID(), "sess-reset-inplace", 0,
			events.MessageDelta, events.MessageDeltaData{Content: txt, MessageID: "msg_same"})
	}
	got := readN(t, server, 2, 2*time.Second)
	require.Len(t, got, 2, "in-place reset must not disrupt the single existing forwarder")
	require.Contains(t, got[0], "inplace-1")
	require.Contains(t, got[1], "inplace-2")

	close(origConn.ch)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("in-place forwarder did not exit after conn closed")
	}
}

func TestResetContract_AllWorkers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		workerType   worker.WorkerType
		connReplaced bool
	}{
		{name: "W-C", workerType: worker.TypeClaudeCode, connReplaced: true},
		{name: "W-O", workerType: worker.TypeOpenCodeSrv, connReplaced: false},
		{name: "W-X", workerType: worker.TypeCodexCLI, connReplaced: true},
		{name: "W-A", workerType: worker.TypeACP, connReplaced: false},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			hub := newTestHub(t)
			conn, server := newTestWSConnPair(t)
			t.Cleanup(func() {
				_ = conn.Close()
				_ = server.Close()
			})
			sessionID := "sess-reset-" + tc.name
			c := newConn(hub, conn, sessionID, nil)
			hub.JoinSession(sessionID, c)

			oldConn := &fakeWorkerConn{ch: make(chan *events.Envelope, 8)}
			w := &connReplacingWorker{replaced: tc.connReplaced}
			w.workerType = tc.workerType
			w.conn = oldConn
			sm := new(mockBridgeSM)
			sm.On("GetWorker", sessionID).Return(w)
			sm.On("Get", sessionID).Return(&session.SessionInfo{ID: sessionID, Platform: "webchat"}, nil)
			b := NewBridge(BridgeDeps{Log: slog.Default(), Hub: hub, SM: sm})
			b.workerRuns.Store(sessionID, workerRunBinding{worker: w, id: "run-" + tc.name})

			oldForwarderDone := make(chan struct{})
			go func() {
				b.forwardEvents(forwarderBinding{worker: w, conn: oldConn}, sessionID, forwardOpts{ctx: context.Background()})
				close(oldForwarderDone)
			}()

			require.NoError(t, b.ResetSession(context.Background(), sessionID))
			if tc.connReplaced {
				require.NotSame(t, oldConn, w.Conn(), "replacement worker must expose a new connection")
				close(oldConn.ch)
				select {
				case <-oldForwarderDone:
				case <-time.After(2 * time.Second):
					t.Fatal("stale forwarder did not exit after the replaced connection closed")
				}

				newConn := w.Conn().(*fakeWorkerConn)
				for _, txt := range []string{"replacement-1", "replacement-2"} {
					newConn.ch <- events.NewEnvelope(aep.NewID(), sessionID, 0,
						events.MessageDelta, events.MessageDeltaData{Content: txt, MessageID: "msg_new"})
				}
				// Read one slot beyond the expected replacement events. If the stale
				// forwarder triggered crash recovery, an extra error/retry envelope
				// would arrive in this same read window.
				got := readN(t, server, 3, 2*time.Second)
				require.Len(t, got, 2, "reset must deliver only the replacement events")
				require.Contains(t, got[0], "replacement-1")
				require.Contains(t, got[1], "replacement-2")
				return
			}

			require.Same(t, oldConn, w.Conn(), "in-place worker must keep its connection")
			for _, txt := range []string{"inplace-1", "inplace-2"} {
				oldConn.ch <- events.NewEnvelope(aep.NewID(), sessionID, 0,
					events.MessageDelta, events.MessageDeltaData{Content: txt, MessageID: "msg_same"})
			}
			got := readN(t, server, 2, 2*time.Second)
			require.Len(t, got, 2)
			require.Contains(t, got[0], "inplace-1")
			require.Contains(t, got[1], "inplace-2")

			close(oldConn.ch)
			select {
			case <-oldForwarderDone:
			case <-time.After(2 * time.Second):
				t.Fatal("in-place forwarder did not exit after conn closed")
			}
		})
	}
}
