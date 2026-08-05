package feishu

// TestPlatformWorkerContractMatrix_Feishu drives the four Feishu worker
// combinations (F-C, F-O, F-X, F-A) through the REAL raw ingress: every
// scenario enters via handleMessage with a complete larkim.P2MessageReceiveV1
// event (message ID, create time, chat/user/type/content) and flows through the
// adapter's messaging Bridge into a contracttest harness (real Hub/Bridge/
// Handler + SQLite session/execution stores + WorkerProbe). The shared C01–C08
// scenario runner asserts the frozen contract matrix (issue #954).
//
// Observation has two layers:
//   - The driver joins its own recording PlatformConn to the derived session
//     (Hub.JoinPlatformSession) — the envelope-level stream the scenarios
//     assert on (acks, delta, terminals).
//   - The REAL FeishuConn is backed by a fake lark API HTTP client, so the
//     mapper stream reaching the real conn is verified by the cards it sends
//     (asserted per combination after the scenarios).
//
// Driver-side fault seams (the only ones expressible through the real feishu
// path — see the task report):
//   - C03 delivery fault: armed pre-ingress — the platform→gateway delivery
//     failed, so the message never enters the adapter and the same id is
//     retryable at the safe stage (the adapter's chat queue swallows task
//     errors by design, so no gateway-side fault can propagate).
//   - C02 conflict: the adapter's in-memory dedup is reset (simulating a
//     platform restart — the only real-world way feishu can re-deliver the
//     same message id); the execution store then rejects the different payload
//     and the driver surfaces the observed INVALID_MESSAGE error envelope.
//   - C07 terminal faults: armed on the recording conn's terminal write; the
//     terminal stays observable exactly once (mirrors the real conn's
//     terminal-delivery fallback semantics). The three stages share the single
//     terminal-delivery point of a recording conn.
//   - C08 delta saturation: armed delta-drop on the recording conn (droppable
//     by contract); the terminal is retained.

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/e2econtract"
	"github.com/hrygo/hotplex/internal/gateway/contracttest"
	"github.com/hrygo/hotplex/internal/messaging"
	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/events"
)

const (
	driverWaitTimeout = 5 * time.Second
	driverWaitPoll    = 10 * time.Millisecond
)

// literalFeishuComboIDs is the hand-written four-row expected set. The loop
// filters e2econtract.Combinations() to PlatformFeishu and diffs against this
// literal, so a dropped manifest row (or a silently-skipped loop) fails the
// test instead of passing with fewer subtests.
var literalFeishuComboIDs = []string{"F-C", "F-O", "F-X", "F-A"}

func TestPlatformWorkerContractMatrix_Feishu(t *testing.T) {
	var combos []e2econtract.Combination
	for _, c := range e2econtract.Combinations() {
		if c.Platform == e2econtract.PlatformFeishu {
			combos = append(combos, c)
		}
	}
	require.Equal(t, literalFeishuComboIDs, comboIDs(combos),
		"the feishu rows of the manifest must exactly match the literal expected set")

	for _, combo := range combos {
		combo := combo
		t.Run(combo.ID, func(t *testing.T) {
			h := contracttest.NewHarness(t, e2econtract.PlatformFeishu, workerProfile(t, combo.Worker))
			d := newFeishuContractDriver(t, h, combo)
			contracttest.RunCoreScenarios(t, combo, d)

			// The mapper stream must reach the real FeishuConn: the fake lark
			// API server observed a card carrying the worker fixture content.
			want := fixtureContent(combo.Worker)
			require.Eventually(t, d.fake.sawCardWith(want), driverWaitTimeout, driverWaitPoll,
				"feishu: no card containing %q reached the fake lark API (real FeishuConn stream)", want)
		})
	}
}

func comboIDs(combos []e2econtract.Combination) []string {
	out := make([]string, len(combos))
	for i, c := range combos {
		out[i] = c.ID
	}
	return out
}

func workerProfile(t *testing.T, wt worker.WorkerType) e2econtract.WorkerProfile {
	t.Helper()
	for _, p := range e2econtract.WorkerProfiles() {
		if p.Type == wt {
			return p
		}
	}
	require.FailNow(t, "no worker profile for type %s", wt)
	return e2econtract.WorkerProfile{}
}

// fixtureContent mirrors the WorkerProbe fixture text per worker type (see
// contracttest.worker_probe.go) so the real-conn card check is exact.
func fixtureContent(wt worker.WorkerType) string {
	switch wt {
	case worker.TypeClaudeCode:
		return "Hello from claude code"
	case worker.TypeOpenCodeSrv:
		return "Hello from opencode"
	case worker.TypeCodexCLI:
		return "Hello from codex"
	case worker.TypeACP:
		return "Hello from acp"
	default:
		return ""
	}
}

// ─── larkAPIFake ──────────────────────────────────────────────────────────────

// larkAPIFake is a RoundTripper backing the adapter's real lark client. It
// records every IM message-create request body and answers every endpoint with
// a minimal success payload — the same response shape the existing feishu
// tests use (conn_test.go). The recorded bodies prove the real FeishuConn
// delivered the mapper stream as cards.
type larkAPIFake struct {
	mu     sync.Mutex
	bodies []string
}

func newLarkAPIFake() *larkAPIFake {
	return &larkAPIFake{bodies: make([]string, 0, 16)}
}

func (f *larkAPIFake) roundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Path == "/open-apis/im/v1/messages" && req.Method == http.MethodPost {
		body, _ := io.ReadAll(req.Body)
		f.mu.Lock()
		f.bodies = append(f.bodies, string(body))
		f.mu.Unlock()
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"code":0,"msg":"ok"}`)),
		Request:    req,
	}, nil
}

// sawCardWith returns a predicate reporting whether a recorded message-create
// body contains text (the worker fixture content inside the card JSON).
func (f *larkAPIFake) sawCardWith(text string) func() bool {
	return func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		for _, body := range f.bodies {
			if strings.Contains(body, text) {
				return true
			}
		}
		return false
	}
}

// ─── recordingConn ────────────────────────────────────────────────────────────

// recordingConn is the driver's envelope-level observation of the derived
// session (joined via Hub.JoinPlatformSession). It records every envelope the
// hub delivers and hosts the C07/C08 driver fault seams.
type recordingConn struct {
	mu      sync.Mutex
	entries []*events.Envelope

	nextTerminalFault atomic.Pointer[error] // error armed for the next terminal write (C07)
	dropDeltas        atomic.Bool           // armed delta drop (C08)

	// terminalFaultsFired counts how many times the armed terminal fault was
	// consumed on THIS conn — verification that the arm reached the observed
	// stream (mutation check: without the SendRawInput transfer the fault
	// fires on the orphaned conn and this stays 0).
	terminalFaultsFired atomic.Int32
}

func newRecordingConn() *recordingConn {
	return &recordingConn{entries: make([]*events.Envelope, 0, 32)}
}

func (r *recordingConn) WriteCtx(_ context.Context, env *events.Envelope) error {
	if env.Event.Type == events.MessageDelta && r.dropDeltas.Load() {
		// Saturated queue: droppable deltas are discarded by contract.
		return nil
	}
	if env.Event.Type == events.Done || env.Event.Type == events.Error {
		if f := r.nextTerminalFault.Swap(nil); f != nil {
			// The terminal delivery failed at the armed stage, but the terminal
			// stays observable exactly once — the platform's terminal handling
			// (feishu fallback semantics) still completes the turn.
			r.terminalFaultsFired.Add(1)
			r.record(env)
			return *f
		}
	}
	r.record(env)
	return nil
}

func (r *recordingConn) record(env *events.Envelope) {
	r.mu.Lock()
	r.entries = append(r.entries, env)
	r.mu.Unlock()
}

func (r *recordingConn) Events() []*events.Envelope {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*events.Envelope, len(r.entries))
	copy(out, r.entries)
	return out
}

func (r *recordingConn) Close() error { return nil }

// ─── feishuContractDriver ─────────────────────────────────────────────────────

// feishuContractDriver implements contracttest.PlatformDriver against the real
// feishu adapter: every Send* method constructs a complete P2MessageReceiveV1
// event and enters via handleMessage.
type feishuContractDriver struct {
	h       *contracttest.Harness
	a       *Adapter
	fake    *larkAPIFake
	rec     *recordingConn
	profile e2econtract.WorkerProfile

	t testing.TB // stashed from BeginScenario for bounded waits

	chatID    string
	userID    string
	sessionID string
	worker    *contracttest.WorkerProbe // the scenario's probe, captured after first delivery

	payloads map[string]string // client message id -> content (C02 conflict detection)

	scenarioID string // scenario id stashed from BeginScenario (C04 turn-gate path)

	nextDeliveryFault atomic.Pointer[error] // error armed for the next SendRawInput (C03)
}

func newFeishuContractDriver(t *testing.T, h *contracttest.Harness, combo e2econtract.Combination) *feishuContractDriver {
	t.Helper()
	profile := workerProfile(t, combo.Worker)

	a := newTestAdapter(t)
	// The messaging Bridge routes the adapter into the harness gateway stack.
	bridge := messaging.NewBridge(discardLogger, messaging.PlatformFeishu,
		h.Hub, h.Handler, h.Bridge, string(combo.Worker), "", "", "")
	require.NoError(t, a.ConfigureWith(messaging.AdapterConfig{Bridge: bridge}))
	require.NoError(t, bridge.SetAdapter(a))

	a.chatQueue = NewChatQueue(discardLogger)
	t.Cleanup(a.chatQueue.Close)
	t.Cleanup(func() { _ = a.Close(context.Background()) })
	// handleTextMessage's pending-interaction check requires a non-nil
	// interaction manager (same wiring as the existing flow tests).
	a.Interactions = messaging.NewInteractionManager(discardLogger)

	fake := newLarkAPIFake()
	// The real FeishuConn renders cards through this client; the fake answers
	// with minimal success payloads (same shape as the existing tests).
	a.larkClient = lark.NewClient("test-app", "test-secret", lark.WithHttpClient(mediaHTTPClientFunc(fake.roundTrip)))

	return &feishuContractDriver{
		h:        h,
		a:        a,
		fake:     fake,
		profile:  profile,
		chatID:   "chat-contract",
		payloads: make(map[string]string),
	}
}

func (d *feishuContractDriver) BeginScenario(t testing.TB, scenarioID string) {
	d.t = t
	d.scenarioID = scenarioID
	// Unique client/session identity per scenario: the derived session key is
	// deterministic in (user, worker, feishu chat), so a fresh user per
	// scenario isolates sessions while C05's within-scenario reuse still holds.
	d.userID = "user-" + scenarioID
	d.sessionID = session.DerivePlatformSessionKey(d.userID, d.profile.Type, session.PlatformContext{
		Platform: string(e2econtract.PlatformFeishu),
		ChatID:   d.chatID,
		UserID:   d.userID,
	})
	d.rec = newRecordingConn()
	d.h.Hub.JoinPlatformSession(d.sessionID, d.rec)
	d.payloads = make(map[string]string)
	d.worker = nil
	d.nextDeliveryFault.Store(nil)
	d.rec.nextTerminalFault.Store(nil)
	d.rec.dropDeltas.Store(false)
}

func (d *feishuContractDriver) EndScenario(t testing.TB) {
	// Clear fault state so a stale arm cannot leak into the next scenario.
	d.nextDeliveryFault.Store(nil)
	d.rec.nextTerminalFault.Store(nil)
	d.rec.dropDeltas.Store(false)
	// Release any gated turn (C04): a failed assertion must not strand the
	// probe's Input goroutine on the terminal gate (ReleaseTerminal is
	// idempotent).
	if d.worker != nil {
		d.worker.ReleaseTerminal()
	}
	// The C04 blocking arm is scenario-scoped: clear it so the next scenario's
	// probes run with the synchronous full-turn emission.
	d.h.DisableProbeBlocking()
	// Close the scenario's probe conn so its bridge forwarder drains (the turn
	// already reached its done, so the forwarder exits cleanly). Without this,
	// every per-scenario session's forwarder blocks the harness teardown's
	// WaitForwarders up to its 2s bound — the harness itself only closes the
	// latest probe.
	if d.worker != nil {
		_ = d.worker.Conn().Close()
		d.worker = nil
	}
}

func (d *feishuContractDriver) SendRawInput(ctx context.Context, id, content string) error {
	if d.scenarioID == "C04-stop" {
		// C04: the probe holds its fixture terminal behind the turn gate so the
		// stop deterministically lands while the turn is live; the interrupted
		// turn's terminal is suppressed on the wire. SendStopTwice releases the
		// gate.
		return d.sendRawInputBlocked(ctx, id, content)
	}
	if f := d.nextDeliveryFault.Swap(nil); f != nil {
		// C03: the platform→gateway delivery failed before the adapter was
		// entered; nothing was recorded, so the same id is retryable at the
		// safe stage.
		return *f
	}
	// Re-join a fresh observation conn per input: after a faulted terminal
	// write (C07) the hub detaches the failing writer, so the driver reconnects
	// like the real platform would. The per-turn snapshot is observed on the
	// fresh conn. The runner arms scenario faults (C07 terminal fault, C08
	// delta saturation) BEFORE SendRawInput, so the arms must be transferred
	// from the previous conn to the fresh one — otherwise the fault fires on
	// the orphaned conn while the driver observes an un-armed stream.
	terminalFault := d.rec.nextTerminalFault.Swap(nil)
	dropDeltas := d.rec.dropDeltas.Swap(false)
	d.rec = newRecordingConn()
	if terminalFault != nil {
		d.rec.nextTerminalFault.Store(terminalFault)
	}
	if dropDeltas {
		d.rec.dropDeltas.Store(true)
	}
	d.h.Hub.JoinPlatformSession(d.sessionID, d.rec)
	d.payloads[id] = content
	if err := d.send(ctx, id, content); err != nil {
		return err
	}
	// The chat queue processes asynchronously; wait for the gateway's
	// delivery-outcome ack (sent after the worker accepted the input) so
	// DeliveredInputs is settled when the runner asserts it.
	var outcome error
	require.Eventually(d.t, func() bool {
		for _, env := range d.rec.Events() {
			ack, ok := env.Event.Data.(events.InputAckData)
			if !ok || ack.ClientMessageID != id {
				continue
			}
			switch ack.Status {
			case events.ExecutionStatusDelivered:
				d.worker = d.h.Worker()
				return true
			case events.ExecutionStatusFailed:
				outcome = fmt.Errorf("feishu contract driver: input %s failed delivery: %s", id, ack.ErrorCode)
				return true
			}
		}
		return false
	}, driverWaitTimeout, driverWaitPoll, "feishu driver: input %s never reached the worker", id)
	return outcome
}

func (d *feishuContractDriver) SendRawDuplicate(ctx context.Context, id, content string) error {
	if prev, ok := d.payloads[id]; ok && prev != content {
		// C02: a different payload with the same id. The adapter's in-memory
		// dedup would silently suppress the replay — reset it (simulating a
		// platform restart, the only real-world re-delivery path) so the
		// execution store rejects the conflict.
		d.a.mu.Lock()
		d.a.Dedup = messaging.NewDedup(100, time.Hour)
		d.a.mu.Unlock()
		if err := d.send(ctx, id, content); err != nil {
			return err
		}
		return d.waitConflict(id)
	}
	// Same payload: the adapter's dedup suppresses the replay (return nil).
	return d.send(ctx, id, content)
}

func (d *feishuContractDriver) FailNextDelivery(err error) {
	d.nextDeliveryFault.Store(&err)
}

func (d *feishuContractDriver) SendStopTwice(ctx context.Context) error {
	// "/stop" is the real platform command: ParseControlCommand resolves it to
	// control.stop, which the gateway's per-turn fence admits exactly once.
	if err := d.send(ctx, "c04-stop-1", "/stop"); err != nil {
		return err
	}
	if err := d.send(ctx, "c04-stop-2", "/stop"); err != nil {
		return err
	}
	// Both stops are processed asynchronously (chatQueue serializes per chat):
	// wait for the effective stop to land on the probe before releasing the
	// gated terminal, so the release deterministically observes stopped=true
	// and suppresses the fixture terminal.
	require.Eventually(d.t, func() bool {
		if p := d.h.Worker(); p != nil && p.StopCalls() >= 1 {
			return true
		}
		return false
	}, driverWaitTimeout, driverWaitPoll, "feishu driver: stop never landed on the probe")
	// Release the gated fixture terminal: the turn was stopped, so the probe
	// suppresses the held terminal and the C04 input settles.
	if p := d.h.Worker(); p != nil {
		p.ReleaseTerminal()
	}
	return nil
}

// sendRawInputBlocked is the C04 path: the harness arms the NEXT probe in
// blocking turn-gate mode, the input is delivered asynchronously (the probe's
// Input blocks on the gate until SendStopTwice releases it), and this method
// waits on the probe's enteredTurn signal instead of the delivery-outcome ack.
func (d *feishuContractDriver) sendRawInputBlocked(ctx context.Context, id, content string) error {
	if f := d.nextDeliveryFault.Swap(nil); f != nil {
		return *f
	}
	// Re-join a fresh observation conn (same as the sync path).
	terminalFault := d.rec.nextTerminalFault.Swap(nil)
	dropDeltas := d.rec.dropDeltas.Swap(false)
	d.rec = newRecordingConn()
	if terminalFault != nil {
		d.rec.nextTerminalFault.Store(terminalFault)
	}
	if dropDeltas {
		d.rec.dropDeltas.Store(true)
	}
	d.h.Hub.JoinPlatformSession(d.sessionID, d.rec)
	d.payloads[id] = content

	d.h.EnableProbeBlocking()
	sendDone := make(chan error, 1)
	go func() {
		sendDone <- d.send(ctx, id, content)
	}()

	// The C04 input's session creates a probe (auto-resume may replace it
	// mid-processing) — poll every probe for the enteredTurn signal
	// (pre-terminal content on the wire, terminal gated) and settle on
	// whichever one the turn actually reached.
	var probe *contracttest.WorkerProbe
	require.Eventually(d.t, func() bool {
		for _, p := range d.h.Probes() {
			select {
			case <-p.EnteredTurn():
				probe = p
				return true
			default:
			}
		}
		return false
	}, driverWaitTimeout, driverWaitPoll, "feishu driver: C04 turn gate never reached")
	d.worker = probe
	return nil
}

func (d *feishuContractDriver) Reset(ctx context.Context) error {
	if err := d.send(ctx, "c06-reset", "/reset"); err != nil {
		return err
	}
	// The post-reset turn is observed from a fresh snapshot — SendRawInput
	// replaces the recording conn anyway — and the reset's context_reset State
	// envelope is not a terminal, so either position is harmless.
	return nil
}

func (d *feishuContractDriver) FailNextTerminal(stage contracttest.TerminalFailureStage, err error) {
	// The three stages (increment/close/fallback) collapse onto the recording
	// conn's single terminal-delivery point; the armed error is returned from
	// the terminal write while the terminal stays observable once.
	_ = stage
	d.rec.nextTerminalFault.Store(&err)
}

func (d *feishuContractDriver) SaturateDeltaQueue(_ context.Context) error {
	// The turn produces a single coalesced delta; the saturation effect is the
	// droppable delta being discarded while the terminal is retained.
	d.rec.dropDeltas.Store(true)
	return nil
}

func (d *feishuContractDriver) WaitForTerminal(t testing.TB) []*events.Envelope {
	t.Helper()
	var log []*events.Envelope
	require.Eventually(t, func() bool {
		log = d.snapshot()
		return len(terminalsIn(log)) >= 1
	}, driverWaitTimeout, driverWaitPoll, "feishu driver: no terminal observed")
	return log
}

func (d *feishuContractDriver) DeliveredInputs() int {
	if d.worker == nil {
		return 0
	}
	return d.worker.InputCalls()
}

func (d *feishuContractDriver) VisibleTerminals() int {
	return len(terminalsIn(d.snapshot()))
}

// ─── driver internals ─────────────────────────────────────────────────────────

// send enters the REAL raw ingress with a complete P2MessageReceiveV1 event.
func (d *feishuContractDriver) send(ctx context.Context, msgID, content string) error {
	sender := larkim.NewEventSenderBuilder().
		SenderId(larkim.NewUserIdBuilder().OpenId(d.userID).Build()).
		SenderType("user").
		Build()
	msg := larkim.NewEventMessageBuilder().
		MessageId(msgID).
		MessageType("text").
		Content(`{"text":"` + content + `"}`).
		ChatId(d.chatID).
		ChatType("p2p").
		Build()
	msg.CreateTime = stringPtr(strconv.FormatInt(time.Now().UnixMilli(), 10))
	return d.a.handleMessage(ctx, &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{Sender: sender, Message: msg},
	})
}

// waitConflict waits for the gateway's stable payload-conflict rejection
// (error envelope with INVALID_MESSAGE, emitted by the execution store's
// Accept) and surfaces it as a driver error.
func (d *feishuContractDriver) waitConflict(id string) error {
	var conflictErr error
	require.Eventually(d.t, func() bool {
		for _, env := range d.rec.Events() {
			if env.Event.Type != events.Error {
				continue
			}
			ed, ok := env.Event.Data.(events.ErrorData)
			if !ok || ed.Code != events.ErrCodeInvalidMessage {
				continue
			}
			conflictErr = fmt.Errorf("feishu contract driver: payload conflict for message %s: %s", id, ed.Message)
			return true
		}
		return false
	}, driverWaitTimeout, driverWaitPoll, "feishu driver: no conflict error observed for message %s", id)
	return conflictErr
}

// snapshot returns the platform-visible turn stream. Control-plane artifacts
// the real FeishuConn renders nothing for are excluded:
//   - delivery-outcome input.acks (status delivered/failed — the durable
//     acceptance ack with status accepted is kept: the scenarios assert it).
//     These are the GENUINE overtake: ack.Priority is PriorityControl
//     (handler.go sendInputAck), so the hub writes them directly to the
//     session conns (hub.go sendControlToSession) instead of queueing them
//     behind the broadcast-queued terminal done (hub.go SendToSession's
//     terminal path) — the echo can land after the done and with a higher seq,
//     racing the probe's synchronously-emitted done.
//   - runtime.execution.* correlation events: finishRuntimeOnDone emits them
//     in-order from the same forwarder goroutine immediately before the done's
//     own SendToSession (bridge_forward.go), so their seq and delivery
//     position are consistent — they are control-plane correlation events the
//     FeishuConn renders nothing for, hence excluded from the stream.
//   - anything after the first terminal — the stream is anchored at the
//     terminal (AEP: done is the last S→C event of a turn).
func (d *feishuContractDriver) snapshot() []*events.Envelope {
	var out []*events.Envelope
	for _, env := range d.rec.Events() {
		switch env.Event.Type {
		case events.InputAck:
			if ack, ok := env.Event.Data.(events.InputAckData); ok && ack.Status != events.ExecutionStatusAccepted {
				continue // delivery-outcome echo
			}
		case events.RuntimeExecutionCompleted, events.RuntimeExecutionFailed:
			continue
		case events.Done, events.Error:
			return append(out, env)
		}
		out = append(out, env)
	}
	return out
}

func terminalsIn(log []*events.Envelope) []*events.Envelope {
	var terms []*events.Envelope
	for _, env := range log {
		if env.Event.Type == events.Done || env.Event.Type == events.Error {
			terms = append(terms, env)
		}
	}
	return terms
}
