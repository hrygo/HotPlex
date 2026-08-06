package slack

// TestPlatformWorkerContractMatrix_Slack drives the four Slack worker
// combinations (S-C, S-O, S-X, S-A) through the REAL raw ingress: every
// scenario enters via handleMessageEvent with a complete slackevents.MessageEvent
// (client_msg_id, ts, channel, user, text) and flows through the adapter's
// messaging Bridge into a contracttest harness (real Hub/Bridge/Handler +
// SQLite session/execution stores + WorkerProbe). The shared C01–C08 scenario
// runner asserts the frozen contract matrix (issue #954).
//
// Observation has two layers:
//   - The driver joins its own recording PlatformConn to the derived session
//     (Hub.JoinPlatformSession) — the envelope-level stream the scenarios
//     assert on (acks, delta, terminals).
//   - The REAL SlackConn is backed by a fake slack API HTTP client (complete
//     response structures), so the mapper stream reaching the real conn is
//     verified by the messages it sends (asserted per combination after the
//     scenarios).
//
// Slack-specific seam notes (the only ones expressible through the real slack
// path — see the task report):
//   - The raw ingress is VOID (handleMessageEvent returns nothing): every
//     gateway-side failure is swallowed by design (logged + ephemeral message),
//     so C03's delivery fault is armed PRE-ingress — the platform→gateway
//     delivery failed, the adapter is never entered, and the same id stays
//     retryable at the safe stage.
//   - The adapter has no chat queue: the entire turn (worker input, mapper
//     stream, terminal) completes synchronously inside handleMessageEvent.
//   - C02 conflict: the adapter's in-memory dedup is reset (simulating a
//     platform restart — the only real-world way slack can re-deliver the same
//     client_msg_id); the execution store then rejects the different payload
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
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
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

// discardLogger silences adapter log output during contract tests.
var discardLogger = slog.New(slog.DiscardHandler)

// literalSlackComboIDs is the hand-written four-row expected set. The loop
// filters e2econtract.Combinations() to PlatformSlack and diffs against this
// literal, so a dropped manifest row (or a silently-skipped loop) fails the
// test instead of passing with fewer subtests.
var literalSlackComboIDs = []string{"S-C", "S-O", "S-X", "S-A"}

func TestPlatformWorkerContractMatrix_Slack(t *testing.T) {
	var combos []e2econtract.Combination
	for _, c := range e2econtract.Combinations() {
		if c.Platform == e2econtract.PlatformSlack {
			combos = append(combos, c)
		}
	}
	require.Equal(t, literalSlackComboIDs, comboIDs(combos),
		"the slack rows of the manifest must exactly match the literal expected set")

	for _, combo := range combos {
		combo := combo
		t.Run(combo.ID, func(t *testing.T) {
			h := contracttest.NewHarness(t, e2econtract.PlatformSlack, workerProfile(t, combo.Worker))
			d := newSlackContractDriver(t, h, combo)
			contracttest.RunCoreScenarios(t, combo, d)

			// The mapper stream must reach the real SlackConn: the fake slack
			// API server observed a message body carrying the worker fixture
			// content (the coalesced delta's first write starts the stream
			// with the full text, so it is always in a recorded body).
			want := fixtureContent(combo.Worker)
			require.Eventually(t, d.fake.sawMessageWith(want), driverWaitTimeout, driverWaitPoll,
				"slack: no message containing %q reached the fake slack API (real SlackConn stream)", want)
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
// contracttest.worker_probe.go) so the real-conn message check is exact.
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

// ─── slackAPIFake ─────────────────────────────────────────────────────────────

// slackAPIFake is a RoundTripper backing the adapter's real slack client. It
// records every request body and answers every endpoint with a complete
// success structure — the same response shape the slack-go SDK parses for the
// message/stream/reaction/user endpoints the SlackConn actually calls. The
// recorded bodies prove the real SlackConn delivered the mapper stream.
type slackAPIFake struct {
	mu     sync.Mutex
	bodies []string
	seq    int
}

func newSlackAPIFake() *slackAPIFake {
	return &slackAPIFake{bodies: make([]string, 0, 16)}
}

func (f *slackAPIFake) RoundTrip(req *http.Request) (*http.Response, error) {
	if strings.HasPrefix(req.URL.Path, "/api/") {
		body, _ := io.ReadAll(req.Body)
		f.mu.Lock()
		f.bodies = append(f.bodies, string(body))
		f.seq++
		n := f.seq
		f.mu.Unlock()

		resp := `{"ok":true}`
		switch {
		case strings.Contains(req.URL.Path, "reactions."):
			// reactions.add/remove — plain ok.
		case strings.Contains(req.URL.Path, "auth.test"):
			resp = `{"ok":true,"user_id":"B_TEST","team_id":"T_TEST"}`
		case strings.Contains(req.URL.Path, "users.info"):
			resp = `{"ok":true,"user":{"id":"U_TEST","name":"test-user"}}`
		case strings.Contains(req.URL.Path, "files.upload"):
			resp = `{"ok":true,"file":{"id":"F_TEST"}}`
		default:
			// chat.postMessage / chat.postEphemeral / chat.update /
			// chat.startStream / chat.appendStream / chat.stopStream — the
			// message endpoints need channel + ts (complete response).
			resp = fmt.Sprintf(`{"ok":true,"channel":"D-contract","ts":"1600000000.00000%d","message":{"ts":"1600000000.00000%d"}}`, n, n)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(resp)),
			Request:    req,
		}, nil
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{}`)),
		Request:    req,
	}, nil
}

// sawMessageWith returns a predicate reporting whether a recorded request body
// contains text. Bodies are form-encoded, so they are unescaped before the
// contains check (a "+" encodes a space).
func (f *slackAPIFake) sawMessageWith(text string) func() bool {
	return func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		for _, body := range f.bodies {
			decoded, err := url.QueryUnescape(body)
			if err != nil {
				decoded = body
			}
			if strings.Contains(decoded, text) {
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
	terminalFaults    atomic.Int32          // count of armed terminal faults that fired (C07 observability)
	dropDeltas        atomic.Bool           // armed delta drop (C08)
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
			// still completes the turn. terminalFaults makes the fire observable
			// (the runner's single-terminal/no-replay assertions cannot tell an
			// armed-and-fired fault from an inert arm). The fault is reported as
			// body-presented: the hub must keep this writer registered, otherwise
			// the same-turn delivered ack (sent after the terminal) finds an
			// empty writer snapshot and is silently dropped (C07 driver timeout).
			r.terminalFaults.Add(1)
			r.record(env)
			return &terminalPresentedError{cause: *f}
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

// terminalPresentedError wraps a C07 armed fault as body-presented (the hub's
// contentPresentedError contract): the terminal was recorded by this conn
// before the fault fired, so the hub must NOT detach the writer. Detaching
// would drop the same-turn delivered ack, which the hub sends after the
// terminal via the writer snapshot.
type terminalPresentedError struct{ cause error }

func (e *terminalPresentedError) Error() string       { return e.cause.Error() }
func (e *terminalPresentedError) Unwrap() error       { return e.cause }
func (e *terminalPresentedError) BodyPresented() bool { return true }

// ─── slackContractDriver ──────────────────────────────────────────────────────

// slackContractDriver implements contracttest.PlatformDriver against the real
// slack adapter: every Send* method constructs a complete MessageEvent and
// enters via handleMessageEvent.
type slackContractDriver struct {
	h       *contracttest.Harness
	a       *Adapter
	fake    *slackAPIFake
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

func newSlackContractDriver(t *testing.T, h *contracttest.Harness, combo e2econtract.Combination) *slackContractDriver {
	t.Helper()
	profile := workerProfile(t, combo.Worker)

	a := newTestAdapter(t)
	// The messaging Bridge routes the adapter into the harness gateway stack.
	bridge := messaging.NewBridge(discardLogger, messaging.PlatformSlack,
		h.Hub, h.Handler, h.Bridge, string(combo.Worker), "", "", "")
	require.NoError(t, a.ConfigureWith(messaging.AdapterConfig{Bridge: bridge}))
	require.NoError(t, bridge.SetAdapter(a))
	// newTestAdapter wires slog.Default() into the adapter; route the matrix's
	// expected control-plane warnings (payload conflicts, stopped turns) to the
	// discard logger like the feishu matrix does.
	a.Log = discardLogger

	fake := newSlackAPIFake()
	// The real SlackConn renders the mapper stream through this client; the
	// fake answers with complete success structures (same shape the slack-go
	// SDK parses for every endpoint the conn calls).
	a.client = slack.New("xoxb-test", slack.OptionHTTPClient(&http.Client{Transport: fake}))

	return &slackContractDriver{
		h:        h,
		a:        a,
		fake:     fake,
		profile:  profile,
		chatID:   "D-contract",
		payloads: make(map[string]string),
	}
}

func (d *slackContractDriver) BeginScenario(t testing.TB, scenarioID string) {
	d.t = t
	d.scenarioID = scenarioID
	// Unique client/session identity per scenario: the derived session key is
	// deterministic in (user, worker, slack channel), so a fresh user per
	// scenario isolates sessions while C05's within-scenario reuse still holds.
	// The key inputs replicate the adapter's makeEnvelope exactly (botID from
	// the test adapter, teamID passed to handleMessageEvent, DM channel, no
	// thread, no workdir).
	d.userID = "user-" + scenarioID
	d.sessionID = session.DerivePlatformSessionKey(d.userID, d.profile.Type, session.PlatformContext{
		Platform:  string(e2econtract.PlatformSlack),
		BotID:     d.a.botID,
		TeamID:    d.a.teamID,
		ChannelID: d.chatID,
		UserID:    d.userID,
	})
	d.rec = newRecordingConn()
	d.h.Hub.JoinPlatformSession(d.sessionID, d.rec)
	d.payloads = make(map[string]string)
	d.worker = nil
	d.nextDeliveryFault.Store(nil)
	d.rec.nextTerminalFault.Store(nil)
	d.rec.dropDeltas.Store(false)
}

func (d *slackContractDriver) EndScenario(t testing.TB) {
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
	// latest probe. MarkExitIntentional precedes the close so the bridge treats
	// the exit as a user stop (session → idle, no crash cleanup, no
	// recovery-worker spawn) — without it the bridge's crash path races the
	// next scenario's input with a recovery attach (matrix flake root cause).
	if d.worker != nil {
		d.worker.MarkExitIntentional()
		_ = d.worker.Conn().Close()
		d.worker = nil
	}
}

func (d *slackContractDriver) SendRawInput(ctx context.Context, id, content string) error {
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
	// fresh conn.
	//
	// Any fault armed on the previous conn (FailNextTerminal / SaturateDeltaQueue,
	// which the runner calls BEFORE SendRawInput) is MOVED to the fresh conn —
	// otherwise the arm would sit on the orphaned conn and the fault half of
	// C07/C08 would never fire on the stream the assertions observe (the
	// single-terminal/no-replay assertions alone cannot tell an armed-and-fired
	// fault from an inert arm).
	// Re-join a fresh observation conn per input: after a faulted terminal
	// write (C07) the hub detaches the failing writer, so the driver reconnects
	// like the real platform would. The per-turn snapshot is observed on the
	// fresh conn.
	//
	// Any fault armed on the previous conn (FailNextTerminal / SaturateDeltaQueue,
	// which the runner calls BEFORE SendRawInput) is MOVED to the fresh conn —
	// otherwise the arm would sit on the orphaned conn and the fault half of
	// C07/C08 would never fire on the stream the assertions observe (the
	// single-terminal/no-replay assertions alone cannot tell an armed-and-fired
	// fault from an inert arm).
	prev := d.rec
	d.rec = newRecordingConn()
	if prev != nil {
		if f := prev.nextTerminalFault.Swap(nil); f != nil {
			d.rec.nextTerminalFault.Store(f)
		}
		if prev.dropDeltas.Swap(false) {
			d.rec.dropDeltas.Store(true)
		}
	}
	d.h.Hub.JoinPlatformSession(d.sessionID, d.rec)
	d.payloads[id] = content
	d.send(ctx, id, content)
	// The adapter's ingress is synchronous, but the delivery-outcome ack is
	// written to the recording conn's writeLoop asynchronously; wait for it
	// (status delivered/failed) so DeliveredInputs is settled when the runner
	// asserts it.
	var outcome error
	lastCount := -1
	require.Eventually(d.t, func() bool {
		// Diagnostic for the CI-only flake: log on every change of the rec
		// conn's event sequence so the timeout trace shows whether the input
		// entered the handler (any envelope present) and which events arrived
		// without the expected InputAck. (Variadic msg args are evaluated at
		// call time, so the snapshot must be logged inside the condition.)
		if evs := d.rec.Events(); len(evs) != lastCount {
			lastCount = len(evs)
			d.t.Logf("input %s diag: rec_events=%v session=%s", id, recEventTypes(evs), d.sessionID)
		}
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
				outcome = fmt.Errorf("slack contract driver: input %s failed delivery: %s", id, ack.ErrorCode)
				return true
			}
		}
		return false
	}, driverWaitTimeout, driverWaitPoll, "slack driver: input %s never reached the worker", id)
	return outcome
}

// recEventTypes returns the event-type sequence observed on a recording conn,
// for the CI-only flake diagnostics in SendRawInput.
func recEventTypes(envs []*events.Envelope) []string {
	out := make([]string, 0, len(envs))
	for _, env := range envs {
		out = append(out, string(env.Event.Type))
	}
	return out
}

func (d *slackContractDriver) SendRawDuplicate(ctx context.Context, id, content string) error {
	if prev, ok := d.payloads[id]; ok && prev != content {
		// C02: a different payload with the same id. The adapter's in-memory
		// dedup would silently suppress the replay — reset it (simulating a
		// platform restart, the only real-world re-delivery path) so the
		// execution store rejects the conflict.
		d.a.mu.Lock()
		d.a.Dedup = messaging.NewDedup(100, time.Hour)
		d.a.mu.Unlock()
		d.send(ctx, id, content)
		return d.waitConflict(id)
	}
	// Same payload: the adapter's dedup suppresses the replay (return nil).
	d.send(ctx, id, content)
	return nil
}

func (d *slackContractDriver) FailNextDelivery(err error) {
	d.nextDeliveryFault.Store(&err)
}

func (d *slackContractDriver) SendStopTwice(ctx context.Context) error {
	// "/stop" is the real platform command: ParseControlCommand resolves it to
	// control.stop, which the gateway's per-turn fence admits exactly once.
	d.send(ctx, "c04-stop-1", "/stop")
	d.send(ctx, "c04-stop-2", "/stop")
	// Both stops are processed asynchronously (events dispatch in goroutines):
	// wait for the effective stop to land on the probe before releasing the
	// gated terminal, so the release deterministically observes stopped=true
	// and suppresses the fixture terminal.
	require.Eventually(d.t, func() bool {
		if p := d.h.Worker(); p != nil && p.StopCalls() >= 1 {
			return true
		}
		return false
	}, driverWaitTimeout, driverWaitPoll, "slack driver: stop never landed on the probe")
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
func (d *slackContractDriver) sendRawInputBlocked(ctx context.Context, id, content string) error {
	if f := d.nextDeliveryFault.Swap(nil); f != nil {
		return *f
	}
	prev := d.rec
	d.rec = newRecordingConn()
	if prev != nil {
		if f := prev.nextTerminalFault.Swap(nil); f != nil {
			d.rec.nextTerminalFault.Store(f)
		}
		if prev.dropDeltas.Swap(false) {
			d.rec.dropDeltas.Store(true)
		}
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
	}, driverWaitTimeout, driverWaitPoll, "slack driver: C04 turn gate never reached")
	d.worker = probe
	return nil
}

func (d *slackContractDriver) Reset(ctx context.Context) error {
	return d.send(ctx, "c06-reset", "/reset")
}

func (d *slackContractDriver) FailNextTerminal(stage contracttest.TerminalFailureStage, err error) {
	// The three stages (increment/close/fallback) collapse onto the recording
	// conn's single terminal-delivery point; the armed error is returned from
	// the terminal write while the terminal stays observable once.
	_ = stage
	d.rec.nextTerminalFault.Store(&err)
}

func (d *slackContractDriver) SaturateDeltaQueue(_ context.Context) error {
	// The turn produces a single coalesced delta; the saturation effect is the
	// droppable delta being discarded while the terminal is retained.
	d.rec.dropDeltas.Store(true)
	return nil
}

func (d *slackContractDriver) WaitForTerminal(t testing.TB) []*events.Envelope {
	t.Helper()
	var log []*events.Envelope
	require.Eventually(t, func() bool {
		log = d.snapshot()
		return len(terminalsIn(log)) >= 1
	}, driverWaitTimeout, driverWaitPoll, "slack driver: no terminal observed")
	return log
}

func (d *slackContractDriver) DeliveredInputs() int {
	if d.worker == nil {
		return 0
	}
	return d.worker.InputCalls()
}

func (d *slackContractDriver) VisibleTerminals() int {
	return len(terminalsIn(d.snapshot()))
}

// ─── driver internals ─────────────────────────────────────────────────────────

// send enters the REAL raw ingress with a complete MessageEvent (client_msg_id,
// fresh ts, DM channel, user, text).
func (d *slackContractDriver) send(ctx context.Context, msgID, content string) error {
	evt := &slackevents.MessageEvent{
		ClientMsgID: msgID,
		User:        d.userID,
		Text:        content,
		TimeStamp:   fmt.Sprintf("%d.000000", time.Now().Unix()),
		Channel:     d.chatID,
	}
	evt.Message = &slack.Msg{Text: content}
	d.a.handleMessageEvent(ctx, evt, d.a.teamID)
	return nil
}

// waitConflict waits for the gateway's stable payload-conflict rejection
// (error envelope with INVALID_MESSAGE, emitted by the execution store's
// Accept) and surfaces it as a driver error.
func (d *slackContractDriver) waitConflict(id string) error {
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
			conflictErr = fmt.Errorf("slack contract driver: payload conflict for message %s: %s", id, ed.Message)
			return true
		}
		return false
	}, driverWaitTimeout, driverWaitPoll, "slack driver: no conflict error observed for message %s", id)
	return conflictErr
}

// snapshot returns the platform-visible turn stream. Control-plane artifacts
// the real SlackConn renders nothing for are excluded:
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
//     SlackConn renders nothing for, hence excluded from the stream.
//   - anything after the first terminal — the stream is anchored at the
//     terminal (AEP: done is the last S→C event of a turn).
func (d *slackContractDriver) snapshot() []*events.Envelope {
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
