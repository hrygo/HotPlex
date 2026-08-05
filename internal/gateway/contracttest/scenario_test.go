package contracttest

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/e2econtract"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/aep"
	"github.com/hrygo/hotplex/pkg/events"
)

// expectedCoreScenarioIDs is the frozen eight-row C01–C08 orchestration order.
// It is written by hand (not derived from the runner's list) so a scenario
// dropped from the runner's orchestration still fails this test.
var expectedCoreScenarioIDs = []string{
	"C01-ack-seq-content-done",
	"C02-dedup-conflict",
	"C03-delivery-fault-retry",
	"C04-stop",
	"C05-next-turn",
	"C06-reset-reconnect",
	"C07-terminal-fault",
	"C08-delta-saturation",
}

// errScenarioConflict is the canned conflict error the recording driver
// surfaces for a same-id/different-payload replay (C02).
var errScenarioConflict = errors.New("contracttest: duplicate payload conflict (canned)")

// TestRunCoreScenarios_ExactEight pins the shared runner's orchestration:
//  1. the execution order is exactly C01..C08 (an eight-row literal), each
//     scenario's subtest named combo/platform/worker/C0N-...;
//  2. when the C04 scenario returns an error, only that scenario fails (the
//     failure is observed through the driver's record — a genuinely failing
//     subtest would fail this whole test, so the demonstration is
//     driver-recorded per the Task 4 coordinator decision).
func TestRunCoreScenarios_ExactEight(t *testing.T) {
	combo := e2econtract.Combination{
		ID:       "F-C",
		Platform: e2econtract.PlatformFeishu,
		Worker:   worker.TypeClaudeCode,
	}

	// (1) Execution order and subtest names — happy recording driver.
	happy := newRecordingDriver("")
	RunCoreScenarios(t, combo, happy)

	require.Equal(t, expectedCoreScenarioIDs, happy.ids(), "execution order must be exactly C01..C08")
	require.Len(t, happy.names(), len(expectedCoreScenarioIDs), "one subtest per scenario")
	for i, id := range expectedCoreScenarioIDs {
		require.Contains(t, happy.names()[i], "F-C/feishu/claude_code/"+id,
			"subtest name must form combo/platform/worker/%s", id)
	}

	// (2) C04 fails, and only C04 — recording driver simulates the C04
	// failure and records it; the runner must still deliver every scenario.
	failing := newRecordingDriver("C04-stop")
	RunCoreScenarios(t, combo, failing)

	require.Equal(t, []string{"C04-stop"}, failing.failedScenarios(),
		"only the C04 scenario may fail")
	require.Contains(t, failing.nameFor("C04-stop"), "F-C/feishu/claude_code/C04-stop",
		"the failed scenario's subtest name must contain F-C/feishu/claude_code/C04-stop")
}

// recordingDriver is a scripted PlatformDriver that records the runner's
// orchestration and serves contract-faithful canned observations, so every
// shared C01–C08 assertion passes. The driver can be configured to simulate a
// scenario failure: it records the failed scenario when its scenario-specific
// method is invoked (SendStopTwice for C04) without failing the test
// framework — the exact-eight test observes failure isolation through the
// record, not through a genuinely failing subtest.
type recordingDriver struct {
	mu sync.Mutex

	simulatedFailure string // scenario ID whose methods record a failure
	scenario         string // current scenario ID

	scenarioIDs  []string // scenario IDs in BeginScenario order
	subtestNames []string // subtest names recorded from BeginScenario's t

	inputs       int
	visibleTerms int
	log          []*events.Envelope // per-turn observed envelopes
	payloads     map[string]string  // client id -> payload (C02 dedup)
	faultArmed   error              // FailNextDelivery armed fault

	failedSet []string
}

var _ PlatformDriver = (*recordingDriver)(nil)

func newRecordingDriver(simulatedFailure string) *recordingDriver {
	return &recordingDriver{
		simulatedFailure: simulatedFailure,
		payloads:         make(map[string]string),
	}
}

// ─── PlatformDriver implementation ───────────────────────────────────────────

func (d *recordingDriver) BeginScenario(t testing.TB, scenarioID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.scenario = scenarioID
	d.scenarioIDs = append(d.scenarioIDs, scenarioID)
	d.subtestNames = append(d.subtestNames, t.Name())
	d.inputs = 0
	d.visibleTerms = 0
	d.log = nil
	d.payloads = make(map[string]string)
	d.faultArmed = nil
}

func (d *recordingDriver) EndScenario(testing.TB) {}

func (d *recordingDriver) SendRawInput(_ context.Context, id, content string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.faultArmed != nil {
		err := d.faultArmed
		d.faultArmed = nil
		return err
	}
	d.payloads[id] = content
	d.inputs++
	d.log = d.turnLog()
	d.visibleTerms = len(terminalsIn(d.log))
	return nil
}

func (d *recordingDriver) SendRawDuplicate(_ context.Context, id, content string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if prev, ok := d.payloads[id]; ok && prev != content {
		return errScenarioConflict
	}
	return nil
}

func (d *recordingDriver) FailNextDelivery(err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.faultArmed = err
}

func (d *recordingDriver) SendStopTwice(context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.simulatedFailure == d.scenario && !contains(d.failedSet, d.scenario) {
		d.failedSet = append(d.failedSet, d.scenario)
	}
	// A stop settles the active turn with a single stopped done.
	if d.scenario == "C04-stop" {
		d.log = d.turnLog()
		d.visibleTerms = len(terminalsIn(d.log))
	}
	return nil
}

func (d *recordingDriver) Reset(context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.log = nil // the post-reset turn is observed from a fresh snapshot
	d.visibleTerms = 0
	return nil
}

func (d *recordingDriver) FailNextTerminal(TerminalFailureStage, error) {}

func (d *recordingDriver) SaturateDeltaQueue(context.Context) error { return nil }

func (d *recordingDriver) WaitForTerminal(testing.TB) []*events.Envelope {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]*events.Envelope, len(d.log))
	copy(out, d.log)
	return out
}

func (d *recordingDriver) DeliveredInputs() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.inputs
}

func (d *recordingDriver) VisibleTerminals() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.visibleTerms
}

// ─── recording accessors ─────────────────────────────────────────────────────

func (d *recordingDriver) ids() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.scenarioIDs...)
}

func (d *recordingDriver) names() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.subtestNames...)
}

func (d *recordingDriver) nameFor(scenarioID string) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	for i, id := range d.scenarioIDs {
		if id == scenarioID {
			return d.subtestNames[i]
		}
	}
	return ""
}

func (d *recordingDriver) failedScenarios() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.failedSet...)
}

// turnLog builds the contract-faithful per-turn observation for the current
// scenario: one durable acceptance ack, one coalesced delta (Seq 0 — the
// pcEntry merges deltas before the platform observer sees them), and one
// terminal. C04's turn ends with the single stopped done; C08's saturated
// turn drops the delta entirely.
func (d *recordingDriver) turnLog() []*events.Envelope {
	sid := "session-" + d.scenario
	switch d.scenario {
	case "C04-stop":
		return []*events.Envelope{
			ackEnvelope(sid, 1, events.ExecutionStatusAccepted, ""),
			deltaEnvelope(sid, "stopping c04"),
			doneEnvelope(sid, 2, "stopped_by_user"),
		}
	case "C08-delta-saturation":
		return []*events.Envelope{
			doneEnvelope(sid, 1, ""),
		}
	default:
		return []*events.Envelope{
			ackEnvelope(sid, 1, events.ExecutionStatusAccepted, "exec-canned"),
			deltaEnvelope(sid, "hello from "+d.scenario),
			doneEnvelope(sid, 2, ""),
		}
	}
}

func ackEnvelope(sid string, seq int64, status events.ExecutionStatus, executionID string) *events.Envelope {
	return &events.Envelope{
		Version:   events.Version,
		ID:        aep.NewID(),
		Seq:       seq,
		SessionID: sid,
		Event: events.Event{
			Type: events.InputAck,
			Data: events.InputAckData{
				ClientMessageID: "canned",
				ExecutionID:     executionID,
				Status:          status,
			},
		},
	}
}

func deltaEnvelope(sid, content string) *events.Envelope {
	return &events.Envelope{
		Version:   events.Version,
		ID:        aep.NewID(),
		Seq:       0, // merged deltas carry Seq 0 (pcEntry coalescing)
		SessionID: sid,
		Event: events.Event{
			Type: events.MessageDelta,
			Data: events.MessageDeltaData{Content: content},
		},
	}
}

func doneEnvelope(sid string, seq int64, reason string) *events.Envelope {
	return &events.Envelope{
		Version:   events.Version,
		ID:        aep.NewID(),
		Seq:       seq,
		SessionID: sid,
		Event: events.Event{
			Type: events.Done,
			Data: events.DoneData{Success: true, Reason: reason},
		},
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
