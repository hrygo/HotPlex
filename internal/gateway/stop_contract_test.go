package gateway_test

// TestWorkerLifecycleContract pins the Gateway stop/next-turn single-terminal
// contract (Task 5, issue #954) per worker type through the REAL gateway stack
// (contracttest harness: SQLite stores + session manager + hub + bridge +
// handler + WorkerProbe driving each worker's REAL parser/mapper).
//
// C04-double-stop: two same-owner control.stops on one active turn must admit
// exactly ONE effective StopCurrentTurn (the turn stop fence) and produce
// exactly ONE terminal — a done with reason stopped_by_user — with the
// execution runtime finishing exactly once and no crash fallback error/done.
// The probe blocks its fixture terminal behind a channel gate so the stops
// deterministically arrive while the turn is live (no time.Sleep).
//
// C05-next-turn: after a stopped turn, the next input on the SAME session must
// clear the per-turn stopped state (probe Input reset + gateway fence
// BeginTurn) and complete normally — two inputs total, the second terminal's
// reason is NOT stopped_by_user, session ID unchanged.
//
// C04-stop-failure-retry: a real StopCurrentTurn failure (worker abort error)
// must roll the fence back and send NO stopped done; a manual retry then
// claims again and converges to exactly one stopped_by_user terminal.

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/e2econtract"
	"github.com/hrygo/hotplex/internal/gateway/contracttest"
	"github.com/hrygo/hotplex/pkg/aep"
	"github.com/hrygo/hotplex/pkg/events"
)

const (
	lifecycleWaitTimeout = 5 * time.Second
	lifecycleWaitPoll    = 10 * time.Millisecond
)

func TestWorkerLifecycleContract(t *testing.T) {
	for _, profile := range e2econtract.WorkerProfiles() {
		profile := profile
		t.Run(string(profile.Type)+"/C04-double-stop", func(t *testing.T) {
			runC04DoubleStop(t, profile)
		})
		t.Run(string(profile.Type)+"/C05-next-turn", func(t *testing.T) {
			runC05NextTurn(t, profile)
		})
		t.Run(string(profile.Type)+"/C04-stop-failure-retry", func(t *testing.T) {
			runC04StopFailureRetry(t, profile)
		})
	}
}

// runC04DoubleStop implements the C04 contract: one effective stop, one
// stopped_by_user done, runtime terminal once, no crash fallback, Seq strictly
// increasing.
func runC04DoubleStop(t *testing.T, profile e2econtract.WorkerProfile) {
	h := contracttest.NewHarness(t, e2econtract.PlatformWebChat, profile)
	probe := h.Worker()
	probe.EnableBlocking()
	// Never leave a turn gated: a failing assertion must not hang the suite.
	defer probe.ReleaseTerminal()

	sessionID := h.SessionID()

	// Turn 1: input; the probe emits start/delta and holds its terminal.
	inputEnv := events.NewEnvelope(aep.NewID(), sessionID, 1, events.Input, map[string]any{"content": "c04 content"})
	inputEnv.OwnerID = "contract-user"
	inputDone := make(chan error, 1)
	go func() { inputDone <- h.Handler.Handle(context.Background(), inputEnv) }()

	waitEnteredTurn(t, probe, "C04")

	// Two same-owner stops while the turn is live.
	for i := 0; i < 2; i++ {
		stopEnv := events.NewEnvelope(aep.NewID(), sessionID, 1, events.Control, events.ControlData{Action: events.ControlActionStop})
		stopEnv.OwnerID = "contract-user"
		require.NoError(t, h.Handler.Handle(context.Background(), stopEnv),
			"C04: stop %d must be accepted", i+1)
	}

	// Release the probe: the stopped turn must NOT complete on the wire.
	probe.ReleaseTerminal()
	require.NoError(t, <-inputDone, "C04: input delivery must settle")

	// Effective StopCurrentTurn exactly once — the turn fence admits one stop.
	require.Equal(t, 1, probe.StopCalls(), "C04: exactly one effective StopCurrentTurn")

	log := h.WaitForKinds(t, events.Done)

	// Exactly one terminal, a done with reason stopped_by_user.
	dones, errs := lifecycleTerminals(log)
	require.Empty(t, errs, "C04: no crash fallback error may reach the client")
	require.Len(t, dones, 1, "C04: exactly one done, got %d", len(dones))
	require.Equal(t, "stopped_by_user", lifecycleDoneReason(dones[0]),
		"C04: the stopped done reason must be stopped_by_user")

	// Execution runtime finishes exactly once (failed), never completed.
	require.Equal(t, 1, lifecycleCount(log, events.RuntimeExecutionFailed),
		"C04: execution runtime must finish exactly once (failed)")
	require.Zero(t, lifecycleCount(log, events.RuntimeExecutionCompleted),
		"C04: a stopped turn must not report a completed runtime")

	// Seq strictly increasing on non-delta envelopes.
	requireStrictSeq(t, log, "C04")
}

// runC05NextTurn implements the C05 contract: after a stopped turn, the next
// input on the same session clears the per-turn stopped state and completes
// normally.
func runC05NextTurn(t *testing.T, profile e2econtract.WorkerProfile) {
	h := contracttest.NewHarness(t, e2econtract.PlatformWebChat, profile)
	probe := h.Worker()
	probe.EnableBlocking()
	defer probe.ReleaseTerminal()

	sessionID := h.SessionID()

	// Turn 1: input + stop (same shape as C04) — sets the per-turn stopped
	// state that the next turn must clear.
	input1 := events.NewEnvelope(aep.NewID(), sessionID, 1, events.Input, map[string]any{"content": "c05 first"})
	input1.OwnerID = "contract-user"
	input1Done := make(chan error, 1)
	go func() { input1Done <- h.Handler.Handle(context.Background(), input1) }()
	waitEnteredTurn(t, probe, "C05")

	stopEnv := events.NewEnvelope(aep.NewID(), sessionID, 1, events.Control, events.ControlData{Action: events.ControlActionStop})
	stopEnv.OwnerID = "contract-user"
	require.NoError(t, h.Handler.Handle(context.Background(), stopEnv), "C05: stop must be accepted")

	probe.ReleaseTerminal()
	require.NoError(t, <-input1Done, "C05: first input must settle")
	waitTerminalCount(t, h, 1, "C05: the stopped turn must produce exactly one terminal")

	// Turn 2: a NEW input ID on the same session; no stop this turn — the
	// probe's per-turn stopped state was cleared by the new primary turn, so
	// the fixture turn completes normally.
	input2 := events.NewEnvelope(aep.NewID(), sessionID, 1, events.Input, map[string]any{"content": "c05 second"})
	input2.OwnerID = "contract-user"
	input2Done := make(chan error, 1)
	go func() { input2Done <- h.Handler.Handle(context.Background(), input2) }()
	waitEnteredTurn(t, probe, "C05")
	probe.ReleaseTerminal()
	require.NoError(t, <-input2Done, "C05: second input must deliver")

	log := waitTerminalCount(t, h, 2, "C05: two turns must produce exactly two terminals")

	// Worker input total 2 across the two turns.
	require.Equal(t, 2, probe.InputCalls(), "C05: Worker input total must be 2")

	// Session identity is stable across both turns.
	for _, env := range log {
		require.Equal(t, sessionID, env.SessionID, "C05: session must stay stable within the scenario")
	}

	// The second turn's terminal is a normal done, not stopped_by_user.
	dones, errs := lifecycleTerminals(log)
	require.Empty(t, errs, "C05: no error terminal")
	require.Len(t, dones, 2, "C05: exactly two dones, got %d", len(dones))
	require.Equal(t, "stopped_by_user", lifecycleDoneReason(dones[0]), "C05: turn 1 done is the stopped done")
	require.NotEqual(t, "stopped_by_user", lifecycleDoneReason(dones[1]),
		"C05: the next turn must complete normally, not stopped")
}

// runC04StopFailureRetry implements the failed-abort convergence: a real
// StopCurrentTurn failure rolls the fence back and sends no stopped done; the
// manual retry claims again and converges to exactly one stopped_by_user
// terminal.
func runC04StopFailureRetry(t *testing.T, profile e2econtract.WorkerProfile) {
	h := contracttest.NewHarness(t, e2econtract.PlatformWebChat, profile)
	probe := h.Worker()
	probe.EnableBlocking()
	defer probe.ReleaseTerminal()

	sessionID := h.SessionID()

	inputEnv := events.NewEnvelope(aep.NewID(), sessionID, 1, events.Input, map[string]any{"content": "c04-fail content"})
	inputEnv.OwnerID = "contract-user"
	inputDone := make(chan error, 1)
	go func() { inputDone <- h.Handler.Handle(context.Background(), inputEnv) }()
	waitEnteredTurn(t, probe, "C04-fail")

	// Stop 1: the worker abort fails (e.g. OCS 500/timeout). The fence must
	// roll back and no stopped done may be sent.
	probe.FailNextStop()
	stop1 := events.NewEnvelope(aep.NewID(), sessionID, 1, events.Control, events.ControlData{Action: events.ControlActionStop})
	stop1.OwnerID = "contract-user"
	err := h.Handler.Handle(context.Background(), stop1)
	require.Error(t, err, "C04-fail: the failed stop must surface an error")

	// Stop 2: the manual retry claims again (the failed claim was rolled back)
	// and converges to exactly one stopped done.
	stop2 := events.NewEnvelope(aep.NewID(), sessionID, 1, events.Control, events.ControlData{Action: events.ControlActionStop})
	stop2.OwnerID = "contract-user"
	require.NoError(t, h.Handler.Handle(context.Background(), stop2), "C04-fail: the retry stop must be accepted")

	probe.ReleaseTerminal()
	require.NoError(t, <-inputDone, "C04-fail: input delivery must settle")

	// Two raw stop invocations (one failed, one effective).
	require.Equal(t, 2, probe.StopCalls(), "C04-fail: two raw stop attempts")

	log := h.WaitForKinds(t, events.Done)

	// The failed attempt surfaced exactly one error; the retry produced
	// exactly one stopped_by_user done and one failed runtime finish.
	require.Equal(t, 1, lifecycleCount(log, events.Error), "C04-fail: exactly one stop-failure error")
	dones, _ := lifecycleTerminals(log)
	require.Len(t, dones, 1, "C04-fail: exactly one done, got %d", len(dones))
	require.Equal(t, "stopped_by_user", lifecycleDoneReason(dones[0]), "C04-fail: done reason must be stopped_by_user")
	require.Equal(t, 1, lifecycleCount(log, events.RuntimeExecutionFailed),
		"C04-fail: execution runtime must finish exactly once (failed)")
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func waitEnteredTurn(t *testing.T, probe *contracttest.WorkerProbe, prefix string) {
	t.Helper()
	select {
	case <-probe.EnteredTurn():
	case <-time.After(lifecycleWaitTimeout):
		t.Fatalf("%s: probe never entered its turn (terminal gate not reached)", prefix)
	}
}

// waitTerminalCount polls the harness observer until it has seen exactly n
// terminal envelopes (done/error), then returns the full event log.
func waitTerminalCount(t *testing.T, h *contracttest.Harness, n int, format string) []*events.Envelope {
	t.Helper()
	var log []*events.Envelope
	require.Eventually(t, func() bool {
		log = h.WaitForKinds(t, events.Done)
		dones, errs := lifecycleTerminals(log)
		return len(dones)+len(errs) >= n
	}, lifecycleWaitTimeout, lifecycleWaitPoll, "%s", format)
	return log
}

// lifecycleTerminals splits the log into done and error terminals, in order.
func lifecycleTerminals(log []*events.Envelope) (dones, errs []*events.Envelope) {
	for _, env := range log {
		switch env.Event.Type {
		case events.Done:
			dones = append(dones, env)
		case events.Error:
			errs = append(errs, env)
		}
	}
	return dones, errs
}

func lifecycleDoneReason(env *events.Envelope) string {
	switch d := env.Event.Data.(type) {
	case events.DoneData:
		return d.Reason
	case map[string]any:
		r, _ := d["reason"].(string)
		return r
	default:
		return ""
	}
}

func lifecycleCount(log []*events.Envelope, kind events.Kind) int {
	n := 0
	for _, env := range log {
		if env.Event.Type == kind {
			n++
		}
	}
	return n
}

// requireStrictSeq asserts strictly increasing seq on non-delta envelopes.
func requireStrictSeq(t *testing.T, log []*events.Envelope, prefix string) {
	t.Helper()
	var prev int64
	first := true
	for _, env := range log {
		if env.Event.Type == events.MessageDelta {
			continue
		}
		if first {
			require.Positive(t, env.Seq, "%s: first non-delta seq must be positive, got %d", prefix, env.Seq)
			first = false
		} else {
			require.Greater(t, env.Seq, prev, "%s: seq must strictly increase (%d after %d)", prefix, env.Seq, prev)
		}
		prev = env.Seq
	}
}
