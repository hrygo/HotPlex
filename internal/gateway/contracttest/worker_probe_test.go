package contracttest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/e2econtract"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/events"
)

// TestWorkerProbe_UsesRealParserAndMapper drives a hand-written protocol fixture
// through each worker type's REAL parser/mapper (not hand-built AEP envelopes)
// and asserts the mapper products: at least one message-family event
// (message.start / message.delta / message), exactly the expected turn terminator
// (done), and that every envelope carries the literal session ID.
func TestWorkerProbe_UsesRealParserAndMapper(t *testing.T) {
	t.Parallel()

	const sessionID = "session-contract"

	for _, profile := range e2econtract.WorkerProfiles() {
		profile := profile
		t.Run(string(profile.Type), func(t *testing.T) {
			t.Parallel()

			probe := NewWorkerProbe(profile, sessionID)
			require.NoError(t, probe.EmitBasicTurn(context.Background()))

			envs := probe.Conn().(*probeConn).Events()
			require.NotEmpty(t, envs, "mapper produced no envelopes for %s", profile.Type)

			var hasMessage, hasDone bool
			for _, env := range envs {
				require.Equal(t, sessionID, env.SessionID, "envelope session mismatch")
				switch env.Event.Type {
				case events.Message, events.MessageStart, events.MessageDelta:
					hasMessage = true
				case events.Done:
					hasDone = true
				}
			}
			require.True(t, hasMessage, "expected a message-family event (message.start/message.delta/message)")
			require.True(t, hasDone, "expected a done event")
		})
	}
}

// TestWorkerProbe_StateMachine verifies the probe's own counters: Input advances
// inputCalls and emits a turn; StopCurrentTurn is CompareAndSwap-guarded so the
// effective stop counts exactly once and IsStopped flips.
func TestWorkerProbe_StateMachine(t *testing.T) {
	t.Parallel()

	probe := NewWorkerProbe(e2econtract.WorkerProfiles()[0], "session-contract")

	require.Equal(t, worker.TypeClaudeCode, probe.Type())
	require.Equal(t, 0, probe.InputCalls())
	require.Equal(t, 0, probe.StopCalls())
	require.False(t, probe.IsStopped())

	require.NoError(t, probe.Input(context.Background(), "hello", nil))
	require.Equal(t, 1, probe.InputCalls())
	require.NotEmpty(t, probe.Conn().(*probeConn).Events())

	require.NoError(t, probe.StopCurrentTurn(context.Background()))
	require.NoError(t, probe.StopCurrentTurn(context.Background()))
	require.Equal(t, 1, probe.StopCalls())
	require.True(t, probe.IsStopped())
}
