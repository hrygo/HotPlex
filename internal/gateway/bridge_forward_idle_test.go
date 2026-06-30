package gateway

import (
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"

	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/aep"
	"github.com/hrygo/hotplex/pkg/events"
)

// TestMaybeTransitionIdleAfterDone verifies Fix A (issue #815): after a turn
// completes, messaging sessions transition to IDLE (avoiding the 30m zombie
// reaper that would otherwise force codex into a fresh-start losing ephemeral
// thread context). webchat is excluded — its IDLE is driven by WS close
// (conn.go:146), and transitioning here would break Fast Reconnect (requires
// State==Running per session.md). Empty platform is skipped conservatively.
func TestMaybeTransitionIdleAfterDone(t *testing.T) {
	t.Parallel()

	t.Run("messaging_feishu_transitions_to_idle", func(t *testing.T) {
		t.Parallel()
		sm := new(mockBridgeSM)
		sm.On("Transition", mock.Anything, "s1", events.StateIdle).Return(nil)
		b := &Bridge{log: slog.Default(), sm: sm}

		b.maybeTransitionIdleAfterDone("s1", &forwardContext{sessionID: "s1", sessPlatform: "feishu"})

		sm.AssertCalled(t, "Transition", mock.Anything, "s1", events.StateIdle)
	})

	t.Run("messaging_slack_transitions_to_idle", func(t *testing.T) {
		t.Parallel()
		sm := new(mockBridgeSM)
		sm.On("Transition", mock.Anything, "s2", events.StateIdle).Return(nil)
		b := &Bridge{log: slog.Default(), sm: sm}

		b.maybeTransitionIdleAfterDone("s2", &forwardContext{sessionID: "s2", sessPlatform: "slack"})

		sm.AssertCalled(t, "Transition", mock.Anything, "s2", events.StateIdle)
	})

	t.Run("webchat_skipped_fast_reconnect_preserved", func(t *testing.T) {
		t.Parallel()
		sm := new(mockBridgeSM)
		b := &Bridge{log: slog.Default(), sm: sm}

		b.maybeTransitionIdleAfterDone("s3", &forwardContext{sessionID: "s3", sessPlatform: platformWebChat})

		sm.AssertNotCalled(t, "Transition", mock.Anything, mock.Anything, mock.Anything)
	})

	t.Run("empty_platform_skipped_conservative", func(t *testing.T) {
		t.Parallel()
		sm := new(mockBridgeSM)
		b := &Bridge{log: slog.Default(), sm: sm}

		b.maybeTransitionIdleAfterDone("s4", &forwardContext{sessionID: "s4", sessPlatform: ""})

		sm.AssertNotCalled(t, "Transition", mock.Anything, mock.Anything, mock.Anything)
	})
}

// TestProcessForwardedEvent_DoneMessagingTransitionsIdle is the end-to-end
// check that the done branch of processForwardedEvent actually invokes the
// IDLE transition for a messaging-platform session (Fix A wiring).
func TestProcessForwardedEvent_DoneMessagingTransitionsIdle(t *testing.T) {
	t.Parallel()

	sm := new(mockBridgeSM)
	sm.On("Transition", mock.Anything, "s1", events.StateIdle).Return(nil)

	h := newTestHub(t)
	conn, server := newTestWSConnPair(t)
	defer conn.Close()
	defer server.Close()
	c := newConn(h, conn, "s1", nil)
	h.JoinSession("s1", c)

	b := NewBridge(BridgeDeps{Log: slog.Default(), Hub: h, SM: sm})
	fw := &mockBridgeWorker{workerType: worker.TypeCodexCLI}
	fc := &forwardContext{
		sessionID:     "s1",
		workerType:    worker.TypeCodexCLI,
		sessPlatform:  "feishu",
		turnStartTime: time.Now(),
	}
	env := events.NewEnvelope(aep.NewID(), "", 0, events.Done, events.DoneData{Success: true})

	b.processForwardedEvent(env, fw, forwardOpts{}, fc)

	sm.AssertCalled(t, "Transition", mock.Anything, "s1", events.StateIdle)
}
