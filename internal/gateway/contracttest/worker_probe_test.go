package contracttest

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/e2econtract"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/internal/worker/base"
	"github.com/hrygo/hotplex/internal/worker/codexcli"
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
// inputCalls and emits a turn; StopCurrentTurn records EVERY invocation (the
// single-effective-stop guarantee is owned by the gateway's turn fence, not by
// the probe) and flips the per-turn stopped marker; the next Input resets the
// marker so the following turn can complete normally (production
// BaseWorker.BeginTurn semantics).
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
	require.Equal(t, 2, probe.StopCalls(), "the probe counts raw StopCurrentTurn invocations")
	require.True(t, probe.IsStopped())

	// The next primary turn resets the per-turn stopped marker.
	require.NoError(t, probe.Input(context.Background(), "hello again", nil))
	require.Equal(t, 2, probe.InputCalls())
	require.False(t, probe.IsStopped(), "a new primary turn must clear the per-turn stopped marker")
}

// TestWorkerProbe_TurnGate verifies the blocking turn gate used by the C04/C05
// lifecycle contract tests: with the gate armed, EmitBasicTurn signals
// enteredTurn after the pre-terminal content and Input returns immediately
// (production delivery semantics — the turn keeps running off the Input call);
// the terminal stays off the wire until the release, and a stop before the
// release suppresses it (the interrupted turn never completes on the wire).
func TestWorkerProbe_TurnGate(t *testing.T) {
	t.Parallel()

	probe := NewWorkerProbe(e2econtract.WorkerProfiles()[0], "session-contract")
	probe.EnableBlocking()

	inputDone := make(chan error, 1)
	go func() {
		inputDone <- probe.Input(context.Background(), "hello", nil)
	}()

	select {
	case <-probe.EnteredTurn():
	case <-time.After(5 * time.Second):
		t.Fatal("probe: enteredTurn not signaled before the gate")
	}
	require.NoError(t, <-inputDone, "probe: gated Input must return after the pre-terminal content")

	// While the terminal is held, only the pre-terminal content is visible.
	envs := probe.Conn().(*probeConn).Events()
	for _, env := range envs {
		require.NotEqual(t, events.Done, env.Event.Type, "probe: terminal must be held behind the gate")
	}

	require.NoError(t, probe.StopCurrentTurn(context.Background()))
	probe.ReleaseTerminal()
	// The release goroutine consumes the gate and suppresses the held terminal;
	// require.Never proves the suppression stays (no late emission).
	require.Never(t, func() bool {
		for _, env := range probe.Conn().(*probeConn).Events() {
			if env.Event.Type == events.Done {
				return true
			}
		}
		return false
	}, 200*time.Millisecond, 10*time.Millisecond, "probe: stopped turn must not emit its terminal")
}

// TestWorkerProbe_TurnGateReleased verifies that a released (non-stopped) turn
// does emit its held terminal.
func TestWorkerProbe_TurnGateReleased(t *testing.T) {
	t.Parallel()

	probe := NewWorkerProbe(e2econtract.WorkerProfiles()[0], "session-contract")
	probe.EnableBlocking()

	inputDone := make(chan error, 1)
	go func() {
		inputDone <- probe.Input(context.Background(), "hello", nil)
	}()

	select {
	case <-probe.EnteredTurn():
	case <-time.After(5 * time.Second):
		t.Fatal("probe: enteredTurn not signaled before the gate")
	}
	require.NoError(t, <-inputDone, "probe: gated Input must return after the pre-terminal content")

	probe.ReleaseTerminal()
	// The released terminal is emitted by the release goroutine.
	require.Eventually(t, func() bool {
		for _, env := range probe.Conn().(*probeConn).Events() {
			if env.Event.Type == events.Done {
				return true
			}
		}
		return false
	}, 5*time.Second, 10*time.Millisecond, "probe: released turn must emit its terminal")

	var terminals int
	for _, env := range probe.Conn().(*probeConn).Events() {
		if env.Event.Type == events.Done {
			terminals++
		}
	}
	require.Equal(t, 1, terminals, "released turn must emit exactly one terminal")
}

// TestWorkerProbe_CapabilitiesMatchAdapters locks the frozen capability manifest
// (e2econtract.WorkerProfiles) to the concrete adapter facts for all four matrix
// workers. Each row instantiates the real registered adapter and asserts its
// actual behavior — SupportsResume(), the worker.MidTurnInjector type assertion
// (Claude Code/Codex yes, OCS/ACP no), and the base.MetadataHandler
// (permission/question/elicitation) type assertion — against the manifest
// claim. If an adapter's capability ever diverges from the manifest, this test
// fails and the manifest must be re-aligned deliberately instead of the worker
// being silently changed to fit.
func TestWorkerProbe_CapabilitiesMatchAdapters(t *testing.T) {
	t.Parallel()

	// The codexcli builder requires the app-server singleton. Constructing the
	// manager is lightweight (no process is spawned) and the worker built from
	// it is only used for interface assertions.
	codexcli.InitSingleton(slog.Default(), config.CodexCLIConfig{Command: "codex"})
	t.Cleanup(func() { codexcli.ShutdownSingleton(context.Background()) })

	for _, profile := range e2econtract.WorkerProfiles() {
		profile := profile
		t.Run(string(profile.Type), func(t *testing.T) {
			t.Parallel()

			w, err := worker.NewWorker(profile.Type)
			require.NoError(t, err, "registry must build %s", profile.Type)
			require.NotNil(t, w, "registry returned a nil worker for %s", profile.Type)

			// Resume: all four adapters implement SupportsResume()==true; the
			// manifest claims Native.
			require.True(t, w.SupportsResume(), "%s must implement SupportsResume()==true", profile.Type)

			// Mid-turn input: Claude Code and Codex satisfy worker.MidTurnInjector
			// (Native — the worker owns the channel); OCS and ACP do not
			// (GatewayFallback — the gateway holds the session data).
			_, midTurn := w.(worker.MidTurnInjector)
			require.Equal(t, profile.MidTurnInput == e2econtract.Native, midTurn,
				"worker.MidTurnInjector assertion for %s diverges from manifest MidTurnInput=%s",
				profile.Type, profile.MidTurnInput)

			// Interaction: all four adapters implement the permission/question/
			// elicitation handler interface; the manifest claims Native.
			_, interaction := w.(base.MetadataHandler)
			require.True(t, interaction, "%s must implement base.MetadataHandler (permission/question/elicitation)", profile.Type)
			require.Equal(t, e2econtract.Native, profile.Interaction,
				"manifest Interaction claim for %s", profile.Type)
		})
	}
}
