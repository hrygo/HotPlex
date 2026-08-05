package contracttest

import (
	"context"
	"log/slog"
	"testing"

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
