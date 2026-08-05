// Shared C01–C08 scenario runner for the platform-worker E2E alignment suite.
//
// RunCoreScenarios orchestrates the eight core contract scenarios against a
// PlatformDriver. The runner ONLY orchestrates and asserts through the driver
// interface — it never touches platform or Gateway private functions. Each
// combination gets one harness (created by the caller); every scenario uses a
// fresh session/client identity via driver.BeginScenario, and EndScenario
// waits for goroutine/conn convergence. When a scenario (or its EndScenario)
// fails, the runner stops the remaining scenarios of the combination so no
// misleading results are produced on polluted state.
package contracttest

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/hrygo/hotplex/internal/e2econtract"
	"github.com/hrygo/hotplex/pkg/events"
)

// errScenarioFault is the fault the runner arms into the driver for
// delivery/terminal fault scenarios (C03/C07). Drivers surface the same error
// instance back from the faulted call so errors.Is matches.
var errScenarioFault = errors.New("contracttest: injected scenario fault")

// PlatformDriver is the platform-side surface the shared runner drives and
// asserts against. A driver is bound to exactly one harness (one SQLite/gateway
// stack per combination); BeginScenario creates unique session/client IDs and
// clears counters and fault state, EndScenario waits for goroutine/conn
// convergence. Drivers may add package-private extensions, but must not remove
// any method or use empty implementations to make scenarios pass.
type PlatformDriver interface {
	BeginScenario(t testing.TB, scenarioID string)
	EndScenario(t testing.TB)
	SendRawInput(ctx context.Context, id, content string) error
	SendRawDuplicate(ctx context.Context, id, content string) error
	FailNextDelivery(err error)
	SendStopTwice(ctx context.Context) error
	Reset(ctx context.Context) error
	FailNextTerminal(stage TerminalFailureStage, err error)
	SaturateDeltaQueue(ctx context.Context) error
	WaitForTerminal(t testing.TB) []*events.Envelope
	DeliveredInputs() int
	VisibleTerminals() int
}

// TerminalFailureStage identifies where a terminal delivery fault is injected
// (C07): before the terminal reaches the platform (increment), at the terminal
// channel close (close), or after the primary channel fails (fallback).
type TerminalFailureStage string

const (
	TerminalIncrement TerminalFailureStage = "increment"
	TerminalClose     TerminalFailureStage = "close"
	TerminalFallback  TerminalFailureStage = "fallback"
)

// scenarioSpec is one row of the frozen C01–C08 contract matrix.
type scenarioSpec struct {
	id  string // subtest suffix, e.g. "C01-ack-seq-content-done"
	run func(t *testing.T, combo e2econtract.Combination, driver PlatformDriver)
}

// coreScenarios is the frozen eight-row C01–C08 orchestration list. The
// exact-eight test pins its order verbatim.
var coreScenarios = []scenarioSpec{
	{id: "C01-ack-seq-content-done", run: scenarioC01},
	{id: "C02-dedup-conflict", run: scenarioC02},
	{id: "C03-delivery-fault-retry", run: scenarioC03},
	{id: "C04-stop", run: scenarioC04},
	{id: "C05-next-turn", run: scenarioC05},
	{id: "C06-reset-reconnect", run: scenarioC06},
	{id: "C07-terminal-fault", run: scenarioC07},
	{id: "C08-delta-saturation", run: scenarioC08},
}

// RunCoreScenarios runs the eight shared core scenarios (C01–C08) against
// driver for combo. Each scenario runs as a subtest named
// `combo/platform/worker/C0N-...`; BeginScenario creates unique
// session/client IDs and clears counters and fault state, EndScenario waits
// for convergence. When a scenario or its EndScenario fails, the remaining
// scenarios of the combination are stopped immediately: continuing on polluted
// state would only produce misleading results.
func RunCoreScenarios(t *testing.T, combo e2econtract.Combination, driver PlatformDriver) {
	for _, sc := range coreScenarios {
		name := subtestName(combo, sc.id)
		if !t.Run(name, func(t *testing.T) {
			driver.BeginScenario(t, sc.id)
			defer driver.EndScenario(t)
			sc.run(t, combo, driver)
		}) {
			// A scenario (or its EndScenario) failed — stop the remaining
			// scenarios of this combination immediately: continuing on polluted
			// state would only produce misleading results (brief Step 3).
			//
			// This branch cannot be unit-pinned by a genuinely failing scenario
			// inside a green test: Go marks the whole ancestor chain failed on
			// any subtest failure (Fail() propagates via parent.Fail()), so a
			// failing run can never be contained in a passing test — verified
			// empirically with a throwaway failing-C04 run (evidence in
			// .superpowers/.../task-4-report.md). Real platform drivers
			// (Tasks 6–8) exercise this branch whenever a scenario assertion or
			// EndScenario genuinely fails.
			return
		}
	}
}

func subtestName(combo e2econtract.Combination, scenarioID string) string {
	return fmt.Sprintf("%s/%s/%s/%s", combo.ID, combo.Platform, combo.Worker, scenarioID)
}

// ─── C01: ACK once, strictly increasing Seq, content visible, done once ─────

func scenarioC01(t *testing.T, _ e2econtract.Combination, driver PlatformDriver) {
	requireNoErr(t, driver.SendRawInput(t.Context(), "c01-id-1", "c01 content"), "C01: send raw input")

	log := driver.WaitForTerminal(t)
	requireNotEmpty(t, log, "C01: the platform must observe the turn")

	// Done exactly once.
	terms := terminalsIn(log)
	requireOne(t, terms, "C01: exactly one terminal (done) expected, got %d", len(terms))
	requireEq(t, events.Done, terms[0].Event.Type, "C01: the single terminal must be a done")
	requireEq(t, 1, driver.VisibleTerminals(), "C01: VisibleTerminals must report exactly one terminal")

	// ACK exactly once: the input is durably accepted exactly once. Merged
	// deltas and delivery-outcome echoes are not acceptance acks.
	requireEq(t, 1, acceptanceAcks(log), "C01: the input must be durably accepted exactly once")

	// Seq strictly increasing on non-delta envelopes. Merged deltas carry
	// Seq 0 (the hub's pcEntry coalesces deltas before the platform observer
	// sees them), so delta envelopes are excluded.
	var prev int64
	first := true
	for _, env := range log {
		if env.Event.Type == events.MessageDelta {
			continue
		}
		if first {
			requireTrue(t, env.Seq > 0, "C01: first non-delta seq must be positive, got %d", env.Seq)
			first = false
		} else {
			requireTrue(t, env.Seq > prev, "C01: seq must strictly increase on non-delta envelopes (%d after %d)", env.Seq, prev)
		}
		prev = env.Seq
	}

	// Worker content is visible to the platform.
	requireTrue(t, anyContent(log), "C01: worker content must be visible to the platform")

	// Nothing may follow the terminal.
	requireEq(t, events.Done, log[len(log)-1].Event.Type, "C01: nothing may follow the terminal")
}

// ─── C02: same id+payload replay adds no input; same id+different payload
// is a stable conflict and does not execute ──────────────────────────────────

func scenarioC02(t *testing.T, _ e2econtract.Combination, driver PlatformDriver) {
	const id = "c02-id-1"

	requireNoErr(t, driver.SendRawInput(t.Context(), id, "c02 first"), "C02: send raw input")
	driver.WaitForTerminal(t)
	requireEq(t, 1, driver.DeliveredInputs(), "C02: the first delivery must reach the worker once")

	requireNoErr(t, driver.SendRawDuplicate(t.Context(), id, "c02 first"), "C02: same id+payload replay must be suppressed, not rejected")
	requireEq(t, 1, driver.DeliveredInputs(), "C02: replay must not increase worker input")

	requireErr(t, driver.SendRawDuplicate(t.Context(), id, "c02 different"), "C02: same id with a different payload must surface a stable conflict")
	requireEq(t, 1, driver.DeliveredInputs(), "C02: a conflicting duplicate must not execute")
}

// ─── C03: after a delivery fault, same-id retry is allowed only at the safe
// stage; the input reaches the worker exactly once ───────────────────────────

func scenarioC03(t *testing.T, _ e2econtract.Combination, driver PlatformDriver) {
	const id = "c03-id-1"

	driver.FailNextDelivery(errScenarioFault)
	err := driver.SendRawInput(t.Context(), id, "c03 content")
	requireErrIs(t, err, errScenarioFault, "C03: the armed delivery fault must fail the first attempt")
	requireEq(t, 0, driver.DeliveredInputs(), "C03: a faulted delivery must not reach the worker")

	// The fault fired before the worker accepted the input, so the same id is
	// retryable at the safe stage and delivers exactly once.
	requireNoErr(t, driver.SendRawInput(t.Context(), id, "c03 content"), "C03: same-id retry must be allowed after a delivery fault")
	requireEq(t, 1, driver.DeliveredInputs(), "C03: exactly one input must reach the worker")

	log := driver.WaitForTerminal(t)
	requireOne(t, terminalsIn(log), "C03: the retried turn must produce exactly one terminal")
}

// ─── C04: double stop on an active turn — one effective stop, one stopped
// done, no crash fallback, no replay ─────────────────────────────────────────

func scenarioC04(t *testing.T, _ e2econtract.Combination, driver PlatformDriver) {
	requireNoErr(t, driver.SendRawInput(t.Context(), "c04-id-1", "c04 content"), "C04: send raw input")
	requireNoErr(t, driver.SendStopTwice(t.Context()), "C04: both stop requests must be accepted")

	log := driver.WaitForTerminal(t)
	terms := terminalsIn(log)
	requireOne(t, terms, "C04: one effective stop must produce exactly one terminal, got %d", len(terms))
	requireEq(t, events.Done, terms[0].Event.Type, "C04: the terminal must be a done, not an error or crash fallback")
	requireEq(t, "stopped_by_user", doneReason(terms[0]), "C04: the stopped done reason must be stopped_by_user")
	requireEq(t, 1, driver.VisibleTerminals(), "C04: exactly one visible terminal")
	requireEq(t, 1, driver.DeliveredInputs(), "C04: stopping must not re-deliver the input")
	requireEq(t, events.Done, log[len(log)-1].Event.Type, "C04: nothing may follow the stopped done")
}

// ─── C05: the next input on the same session completes normally ─────────────

func scenarioC05(t *testing.T, _ e2econtract.Combination, driver PlatformDriver) {
	requireNoErr(t, driver.SendRawInput(t.Context(), "c05-id-1", "c05 first"), "C05: first input")
	log1 := driver.WaitForTerminal(t)

	requireNoErr(t, driver.SendRawInput(t.Context(), "c05-id-2", "c05 second"), "C05: next-turn input on the same session")
	log2 := driver.WaitForTerminal(t)
	terms := terminalsIn(log2)
	requireOne(t, terms, "C05: the next turn must produce exactly one terminal, got %d", len(terms))
	requireEq(t, events.Done, terms[0].Event.Type, "C05: the next turn must complete with a done")
	requireTrue(t, doneReason(terms[0]) != "stopped_by_user", "C05: the next turn must complete normally, not stopped")
	requireEq(t, 2, driver.DeliveredInputs(), "C05: two inputs across two turns")
	// The next turn must reuse the session of the previous turn — a driver
	// that starts a fresh session for the second input must fail here.
	requireNotEmpty(t, log1, "C05: the first turn must be observed")
	requireNotEmpty(t, log2, "C05: the next turn must be observed")
	requireEq(t, log1[0].SessionID, log2[0].SessionID, "C05: the next turn must reuse the same session")
	for _, env := range log2 {
		requireEq(t, log2[0].SessionID, env.SessionID, "C05: the session must stay stable within the scenario")
	}
}

// ─── C06: after reset/reconnect, old-forwarder events are not visible ───────

func scenarioC06(t *testing.T, _ e2econtract.Combination, driver PlatformDriver) {
	requireNoErr(t, driver.SendRawInput(t.Context(), "c06-id-1", "c06 first"), "C06: first input")
	driver.WaitForTerminal(t)

	requireNoErr(t, driver.Reset(t.Context()), "C06: reset/reconnect")

	requireNoErr(t, driver.SendRawInput(t.Context(), "c06-id-2", "c06 second"), "C06: post-reset input")
	log := driver.WaitForTerminal(t)
	terms := terminalsIn(log)
	requireOne(t, terms, "C06: the post-reset turn must produce exactly one terminal — old forwarder events must not surface, got %d", len(terms))
	requireEq(t, events.Done, terms[0].Event.Type, "C06: the post-reset turn must complete with a done")
	requireEq(t, events.Done, log[len(log)-1].Event.Type, "C06: nothing may follow the post-reset terminal")
	requireEq(t, 2, driver.DeliveredInputs(), "C06: reset must not re-deliver or lose inputs")
}

// ─── C07: increment/close/fallback terminal faults stay observable; worker
// input is never replayed ────────────────────────────────────────────────────

func scenarioC07(t *testing.T, _ e2econtract.Combination, driver PlatformDriver) {
	for i, stage := range []TerminalFailureStage{TerminalIncrement, TerminalClose, TerminalFallback} {
		driver.FailNextTerminal(stage, errScenarioFault)
		requireNoErr(t, driver.SendRawInput(t.Context(), "c07-"+string(stage)+"-1", "c07 content"), "C07 %s: input", stage)

		log := driver.WaitForTerminal(t)
		requireOne(t, terminalsIn(log), "C07 %s: the terminal must be observed exactly once despite the fault, got %d", stage, len(terminalsIn(log)))
		requireEq(t, i+1, driver.DeliveredInputs(), "C07 %s: worker input must not be replayed", stage)
	}
}

// ─── C08: deltas are droppable; state/done/error are retained ───────────────

func scenarioC08(t *testing.T, _ e2econtract.Combination, driver PlatformDriver) {
	requireNoErr(t, driver.SaturateDeltaQueue(t.Context()), "C08: saturate the delta queue")
	requireNoErr(t, driver.SendRawInput(t.Context(), "c08-id-1", "c08 content"), "C08: input under saturation")

	log := driver.WaitForTerminal(t)
	terms := terminalsIn(log)
	requireOne(t, terms, "C08: the terminal must be retained under delta saturation, got %d", len(terms))
	requireEq(t, events.Done, terms[0].Event.Type, "C08: the retained terminal must be a done")
	requireEq(t, 1, driver.DeliveredInputs(), "C08: input delivered exactly once")
	// Deltas are droppable by contract: presence/absence of message.delta is
	// intentionally not asserted.
}

// ─── assertion helpers (require.* on *testing.T, subtest-fatal) ─────────────

func requireNoErr(t *testing.T, err error, format string, args ...any) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", fmt.Sprintf(format, args...), err)
	}
}

func requireErr(t *testing.T, err error, format string, args ...any) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: expected an error, got nil", fmt.Sprintf(format, args...))
	}
}

func requireErrIs(t *testing.T, err, target error, format string, args ...any) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Fatalf("%s: got %v, want errors.Is(%v)", fmt.Sprintf(format, args...), err, target)
	}
}

func requireEq(t *testing.T, want, got any, format string, args ...any) {
	t.Helper()
	if want != got {
		t.Fatalf("%s: got %v, want %v", fmt.Sprintf(format, args...), got, want)
	}
}

func requireTrue(t *testing.T, cond bool, format string, args ...any) {
	t.Helper()
	if !cond {
		t.Fatalf("%s", fmt.Sprintf(format, args...))
	}
}

// requireOne asserts len(got) == 1. Every shared scenario asserts a single
// terminal per turn, so the expected length is not a parameter.
func requireOne(t *testing.T, got any, format string, args ...any) {
	t.Helper()
	n := 0
	switch v := got.(type) {
	case []*events.Envelope:
		n = len(v)
	case []events.Envelope:
		n = len(v)
	default:
		t.Fatalf("requireOne: unsupported type %T", got)
	}
	if n != 1 {
		t.Fatalf("%s", fmt.Sprintf(format, args...))
	}
}

func requireNotEmpty(t *testing.T, got []*events.Envelope, format string, args ...any) {
	t.Helper()
	if len(got) == 0 {
		t.Fatalf("%s", fmt.Sprintf(format, args...))
	}
}

// ─── envelope helpers ────────────────────────────────────────────────────────

// terminalsIn returns the terminal envelopes (done/error) of log in order.
func terminalsIn(log []*events.Envelope) []*events.Envelope {
	var terms []*events.Envelope
	for _, env := range log {
		if env.Event.Type == events.Done || env.Event.Type == events.Error {
			terms = append(terms, env)
		}
	}
	return terms
}

// doneReason extracts the Done reason from an envelope, accepting both the
// typed DoneData and map-decoded data.
func doneReason(env *events.Envelope) string {
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

// acceptanceAcks counts InputAck envelopes reporting the durable acceptance
// (Status == accepted) of the input.
func acceptanceAcks(log []*events.Envelope) int {
	n := 0
	for _, env := range log {
		if env.Event.Type != events.InputAck {
			continue
		}
		switch d := env.Event.Data.(type) {
		case events.InputAckData:
			if d.Status == events.ExecutionStatusAccepted {
				n++
			}
		case map[string]any:
			if s, _ := d["status"].(string); s == string(events.ExecutionStatusAccepted) {
				n++
			}
		}
	}
	return n
}

// anyContent reports whether any envelope carries non-empty user-visible
// content (delta/message).
func anyContent(log []*events.Envelope) bool {
	for _, env := range log {
		switch d := env.Event.Data.(type) {
		case events.MessageDeltaData:
			if d.Content != "" {
				return true
			}
		case events.MessageData:
			if d.Content != "" {
				return true
			}
		case map[string]any:
			if c, _ := d["content"].(string); c != "" {
				return true
			}
		}
	}
	return false
}
