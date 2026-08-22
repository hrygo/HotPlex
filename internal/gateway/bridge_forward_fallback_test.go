package gateway

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/aep"
	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/pkg/events"
)

// orderedProbeWriter blocks the first routed write while still accepting a
// direct WriteCtx call. This makes the pre-fix control-path overtake
// deterministic: Hub.Run is stopped in RouteWriteData for seq=1, while the
// PriorityControl seq=2 is written directly by SendToSession.
type orderedProbeWriter struct {
	routeEntered chan struct{}
	releaseRoute chan struct{}
	controlWrite chan struct{}
	routeOnce    sync.Once
	controlOnce  sync.Once

	mu     sync.Mutex
	writes []*events.Envelope
}

func (w *orderedProbeWriter) WriteCtx(_ context.Context, env *events.Envelope) error {
	w.controlOnce.Do(func() { close(w.controlWrite) })
	w.record(env)
	return nil
}

func (w *orderedProbeWriter) RouteWrite(_ context.Context, env *events.Envelope) error {
	w.record(env)
	return nil
}

func (w *orderedProbeWriter) RouteWriteData(data []byte, _ events.Kind) error {
	w.routeOnce.Do(func() {
		close(w.routeEntered)
		<-w.releaseRoute
	})
	env, err := aep.DecodeLine(data)
	if err != nil {
		return err
	}
	w.record(env)
	return nil
}

func (w *orderedProbeWriter) Close() error { return nil }

func (w *orderedProbeWriter) PreferEnvelope() bool { return false }

func (w *orderedProbeWriter) record(env *events.Envelope) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.writes = append(w.writes, events.Clone(env))
}

func (w *orderedProbeWriter) snapshot() []*events.Envelope {
	w.mu.Lock()
	defer w.mu.Unlock()
	result := make([]*events.Envelope, len(w.writes))
	copy(result, w.writes)
	return result
}

// TestHub_SequenceOrderIncludesControlEvents proves the exact race behind
// C04's intermittent seq regressions. A queued seq=1 event is held inside the
// Hub router; the old PriorityControl fast path writes seq=2 immediately,
// producing [2,1]. All sequence-bearing events must share the ordered queue.
func TestHub_SequenceOrderIncludesControlEvents(t *testing.T) {
	t.Parallel()
	h := newTestHub(t)
	w := &orderedProbeWriter{
		routeEntered: make(chan struct{}),
		releaseRoute: make(chan struct{}),
		controlWrite: make(chan struct{}),
	}
	const sessionID = "seq-order-control"
	h.mu.Lock()
	h.sessions[sessionID] = map[SessionWriter]bool{w: true}
	h.everHadConn[sessionID] = true
	h.mu.Unlock()

	first := events.NewEnvelope(aep.NewID(), sessionID, 0, events.State, events.StateData{State: events.StateRunning})
	firstDone := make(chan error, 1)
	go func() { firstDone <- h.SendToSession(context.Background(), first) }()
	select {
	case <-w.routeEntered:
	case <-time.After(time.Second):
		t.Fatal("Hub router did not reach the deterministic gate")
	}

	control := events.NewEnvelope(aep.NewID(), sessionID, 0, events.InputAck, events.InputAckData{Status: events.ExecutionStatusAccepted})
	control.Priority = events.PriorityControl
	controlDone := make(chan error, 1)
	go func() { controlDone <- h.SendToSession(context.Background(), control) }()

	// Give the old direct path a chance to write before releasing the router.
	// The fixed path remains blocked here because control is now queued behind
	// the held seq=1 event; the bounded timeout keeps this test deterministic.
	select {
	case <-w.controlWrite:
	case <-time.After(200 * time.Millisecond):
	}

	// Let both producers settle after the router gate is released. The old
	// implementation records the direct control write before the queued event.
	close(w.releaseRoute)
	require.NoError(t, <-firstDone)
	require.NoError(t, <-controlDone)

	var writes []*events.Envelope
	require.Eventually(t, func() bool {
		writes = w.snapshot()
		return len(writes) == 2
	}, time.Second, time.Millisecond, "both ordered writes must reach the platform")
	require.Equal(t, int64(1), writes[0].Seq)
	require.Equal(t, int64(2), writes[1].Seq)
}

// TestBridge_ForwardEventsPanicCompletesTurn proves that a forwarder panic
// cannot strand a turn after the worker stream has started. Before the
// recovery path emits an incomplete-stream terminal, the goroutine simply
// logs and returns, leaving the client with no Done/Error at all.
func TestBridge_ForwardEventsPanicCompletesTurn(t *testing.T) {
	t.Parallel()
	h := newTestHub(t)
	conn, server := newTestWSConnPair(t)
	t.Cleanup(func() { _ = conn.Close(); _ = server.Close() })
	h.JoinSession("panic-terminal", newConn(h, conn, "panic-terminal", nil))

	// A durable forwarding operation consults this hook before publishing. The
	// injected panic is deterministic and models any unexpected stream-path
	// panic without changing production dependencies or worker fixtures.
	var panicOnce atomic.Bool
	h.SetSeqSessionExists(func(string) bool {
		if panicOnce.CompareAndSwap(false, true) {
			panic("forced forwarder panic")
		}
		return true
	})
	b, _ := newBridgeWithCollector(t)
	b.hub = h
	workerEvents := make(chan *events.Envelope, 1)
	workerEvents <- events.NewEnvelope(aep.NewID(), "", 0, events.State, events.StateData{State: events.StateRunning})
	close(workerEvents)
	fw := &mockBridgeWorker{
		workerType: worker.TypeClaudeCode,
		conn:       &fakeWorkerConn{ch: workerEvents},
	}

	done := make(chan struct{})
	go func() {
		b.forwardEvents(forwarderBinding{worker: fw, conn: fw.Conn()}, "panic-terminal", forwardOpts{})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("forwardEvents did not return after the forced panic")
	}

	env := tryReadEnvelope(t, server)
	require.NotNil(t, env, "a forwarder panic must produce an incomplete-stream terminal")
	require.Equal(t, events.Error, env.Event.Type)
}

// TestBridge_ProcessForwardedEventSendsExactlyOneTerminal protects the
// terminal fence independently of worker-exit handling: a duplicate worker
// Done must not produce two client-visible terminals or duplicate runtime
// completion side effects.
func TestBridge_ProcessForwardedEventSendsExactlyOneTerminal(t *testing.T) {
	t.Parallel()
	h := newTestHub(t)
	conn, server := newTestWSConnPair(t)
	t.Cleanup(func() { _ = conn.Close(); _ = server.Close() })
	const sessionID = "duplicate-terminal"
	h.JoinSession(sessionID, newConn(h, conn, sessionID, nil))
	b := NewBridge(BridgeDeps{Log: slog.Default(), Hub: h})
	fw := &mockBridgeWorker{
		workerType: worker.TypeClaudeCode,
		conn:       &fakeWorkerConn{ch: make(chan *events.Envelope)},
	}
	fc := &forwardContext{sessionID: sessionID, workerType: worker.TypeClaudeCode, firstEvent: true}
	fc.turnText.WriteString("reply")
	done := events.NewEnvelope(aep.NewID(), sessionID, 0, events.Done, events.DoneData{Success: true})

	b.processForwardedEvent(done, fw, forwardOpts{}, fc)
	b.processForwardedEvent(events.Clone(done), fw, forwardOpts{}, fc)

	first := tryReadEnvelope(t, server)
	require.NotNil(t, first)
	require.Equal(t, events.Done, first.Event.Type)
	require.Nil(t, tryReadEnvelope(t, server), "duplicate Done must be suppressed")
}

// tryReadEnvelope reads one envelope from the WS conn with a short deadline.
// Returns nil if nothing arrived (used to assert "no message was sent").
func tryReadEnvelope(t *testing.T, server interface {
	ReadMessage() (int, []byte, error)
	SetReadDeadline(time.Time) error
}) *events.Envelope {
	t.Helper()
	require.NoError(t, server.SetReadDeadline(time.Now().Add(300*time.Millisecond)))
	defer func() { _ = server.SetReadDeadline(time.Time{}) }()
	_, data, err := server.ReadMessage()
	if err != nil {
		return nil
	}
	var env events.Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return &env
}

// TestMaybeSendDoneFallback verifies a synthetic Message is injected when a
// turn did tool work but produced no assistant text, so feishu/slack don't
// show an empty reply. Mirrors maybeTransitionIdleAfterDone test pattern.
func TestMaybeSendDoneFallback(t *testing.T) {
	t.Parallel()

	setup := func(t *testing.T, platform string) (*Bridge, *sessionAccumulator, *forwardContext, interface {
		ReadMessage() (int, []byte, error)
		SetReadDeadline(time.Time) error
	}) {
		t.Helper()
		h := newTestHub(t)
		conn, server := newTestWSConnPair(t)
		t.Cleanup(func() { _ = conn.Close(); _ = server.Close() })
		c := newConn(h, conn, "s1", nil)
		h.JoinSession("s1", c)
		b := NewBridge(BridgeDeps{Log: slog.Default(), Hub: h})
		acc := &sessionAccumulator{}
		fc := &forwardContext{
			sessionID:     "s1",
			sessPlatform:  platform,
			turnStartTime: time.Now().Add(-5 * time.Second),
		}
		acc.TurnDurationMs = 5000
		return b, acc, fc, server
	}

	t.Run("triggers_when_no_text_with_tools", func(t *testing.T) {
		t.Parallel()
		b, acc, fc, server := setup(t, "feishu")
		acc.ToolCallCount.Store(3)
		acc.ToolNames = map[string]int{"Bash": 2, "Read": 1}

		b.maybeSendDoneFallback("s1", acc, fc)

		env := tryReadEnvelope(t, server)
		require.NotNil(t, env, "expected a fallback Message envelope")
		require.Equal(t, events.Message, env.Event.Type)
		data, ok := env.Event.Data.(map[string]any)
		require.True(t, ok, "event data should deserialize to map")
		content, _ := data["content"].(string)
		require.Contains(t, content, "3")
		require.Contains(t, content, "Bash×2")
		require.Contains(t, content, "Read×1")
	})

	t.Run("skipped_when_text_present", func(t *testing.T) {
		t.Parallel()
		b, acc, fc, server := setup(t, "feishu")
		acc.ToolCallCount.Store(3)
		fc.turnText.WriteString("real reply text")

		b.maybeSendDoneFallback("s1", acc, fc)

		require.Nil(t, tryReadEnvelope(t, server), "no fallback expected when turn has text")
	})

	t.Run("empty_success_emits_terminal_on_feishu", func(t *testing.T) {
		t.Parallel()
		b, acc, fc, server := setup(t, "feishu")
		// No text AND no tool calls → empty-success integrity failure. The turn
		// must NOT end silently leaving a placeholder; emit a retryable terminal
		// (Turn-Integrity Fix C, invariant I-5).
		b.maybeSendDoneFallback("s1", acc, fc)

		env := tryReadEnvelope(t, server)
		require.NotNil(t, env, "empty-success must emit a terminal Message")
		require.Equal(t, events.Message, env.Event.Type)
		data, ok := env.Event.Data.(map[string]any)
		require.True(t, ok)
		content, _ := data["content"].(string)
		require.NotEmpty(t, content, "terminal text must be non-empty")
		require.Greater(t, fc.turnText.Len(), 0, "turnText must be backfilled for the turns table")
	})

	t.Run("empty_success_emits_terminal_on_webchat", func(t *testing.T) {
		t.Parallel()
		b, acc, fc, server := setup(t, platformWebChat)
		// WebChat is skipped for tool-only fallbacks, but empty-success still
		// needs an explicit terminal so the assistant turn is not left blank.
		b.maybeSendDoneFallback("s1", acc, fc)

		env := tryReadEnvelope(t, server)
		require.NotNil(t, env, "webchat empty-success must emit a terminal Message, not a blank assistant turn")
		require.Equal(t, events.Message, env.Event.Type)
	})

	t.Run("skipped_for_webchat_tool_only", func(t *testing.T) {
		t.Parallel()
		b, acc, fc, server := setup(t, platformWebChat)
		acc.ToolCallCount.Store(3)
		// Tool-only turn on webchat: UI renders the tool list independently, no fallback.
		b.maybeSendDoneFallback("s1", acc, fc)

		require.Nil(t, tryReadEnvelope(t, server), "no tool-only fallback expected for webchat")
	})
}
