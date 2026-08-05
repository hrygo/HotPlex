package gateway_test

// TestPlatformWorkerContractMatrix_WebChat drives the four webchat worker
// combinations (W-C, W-O, W-X, W-A) through the REAL WebSocket/AEP ingress:
// every scenario dials the real gateway WS endpoint (Hub.HandleHTTP with the
// real Conn/ReadPump), completes the real AEP init handshake, and sends
// init/input/control envelopes over the wire into a contracttest harness (real
// Hub/Bridge/Handler + SQLite session/execution stores + WorkerProbe). The
// shared C01–C08 scenario runner asserts the frozen contract matrix (issue
// #954). Each scenario gets a fresh client key and therefore a fresh
// server-derived session, created by the bridge through the harness's
// probeFactory (which rejects any worker type not matching the combo profile).
//
// Observation has two layers:
//   - The driver joins its own recording PlatformConn to the WS-derived session
//     (Hub.JoinPlatformSession) — the envelope-level stream the scenarios
//     assert on (acks, delta, terminals), hosting the C07/C08 fault seams.
//   - The REAL WebSocket client runs a reader goroutine draining the socket, so
//     the stream that actually reaches a webchat client over the wire is
//     verified per combination after the scenarios.
//
// Driver-side fault seams (the only ones expressible through the real WS path):
//   - C03 delivery fault: armed pre-ingress — the client's send failed, so the
//     envelope never entered the gateway and the same id is retryable at the
//     safe stage.
//   - C02 conflict: the execution store rejects the same id with a different
//     payload and the driver surfaces the observed INVALID_MESSAGE error
//     envelope. The WS path has no adapter-level dedup to reset, so a
//     same-id/same-payload replay is suppressed by the execution store itself
//     (duplicate ack, no worker call).
//   - C07 terminal faults: armed on the recording conn's terminal write; the
//     terminal stays observable exactly once (mirrors the platform
//     terminal-delivery fallback semantics). The three stages share the single
//     terminal-delivery point of a recording conn.
//   - C08 delta saturation: armed delta-drop on the recording conn (droppable
//     by contract); the terminal is retained.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/e2econtract"
	"github.com/hrygo/hotplex/internal/gateway"
	"github.com/hrygo/hotplex/internal/gateway/contracttest"
	"github.com/hrygo/hotplex/internal/security"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/aep"
	"github.com/hrygo/hotplex/pkg/events"
)

const (
	driverWaitTimeout = 5 * time.Second
	driverWaitPoll    = 10 * time.Millisecond
	// wsTestAPIKey authenticates the WS upgrade at HTTP time (HandleHTTP's
	// AuthenticateKey), so the init envelope needs no auth token.
	wsTestAPIKey = "webchat-contract-test-key"
)

// literalWebChatComboIDs is the hand-written four-row expected set. The loop
// filters e2econtract.Combinations() to PlatformWebChat and diffs against this
// literal, so a dropped manifest row (or a silently-skipped loop) fails the
// test instead of passing with fewer subtests.
var literalWebChatComboIDs = []string{"W-C", "W-O", "W-X", "W-A"}

func TestPlatformWorkerContractMatrix_WebChat(t *testing.T) {
	var combos []e2econtract.Combination
	for _, c := range e2econtract.Combinations() {
		if c.Platform == e2econtract.PlatformWebChat {
			combos = append(combos, c)
		}
	}
	require.Equal(t, literalWebChatComboIDs, comboIDs(combos),
		"the webchat rows of the manifest must exactly match the literal expected set")

	for _, combo := range combos {
		combo := combo
		t.Run(combo.ID, func(t *testing.T) {
			h := contracttest.NewHarness(t, e2econtract.PlatformWebChat, workerProfile(t, combo.Worker))
			d := newWebChatContractDriver(t, h, combo)
			contracttest.RunCoreScenarios(t, combo, d)

			// The mapper stream must reach the REAL WebSocket client: the reader
			// goroutine must have observed the worker fixture content on the wire.
			want := fixtureContent(combo.Worker)
			require.Eventually(t, d.wsSawContent(want), driverWaitTimeout, driverWaitPoll,
				"webchat: no envelope containing %q reached the real WS client", want)
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
// contracttest.worker_probe.go) so the real-WS-client check is exact.
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

// ─── recordingConn ────────────────────────────────────────────────────────────

// recordingConn is the driver's envelope-level observation of the WS-derived
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
			// still completes the turn.
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

// ─── webChatContractDriver ────────────────────────────────────────────────────

// webChatContractDriver implements contracttest.PlatformDriver against the real
// WebSocket/AEP ingress: every Send* method writes a real envelope over the WS
// connection of the current scenario, and BeginScenario completes a real AEP
// init handshake (the init_ack's authoritative session ID anchors the
// observation).
type webChatContractDriver struct {
	h       *contracttest.Harness
	profile e2econtract.WorkerProfile
	wsURL   string
	rec     *recordingConn

	t testing.TB // stashed from BeginScenario for bounded waits

	clientKey string
	ws        *websocket.Conn // the scenario's real WS client conn
	sessionID string          // authoritative session ID from init_ack
	worker    *contracttest.WorkerProbe

	readDone chan struct{}
	wsLogMu  sync.Mutex
	wsLog    []*events.Envelope // everything the real WS client received on the wire

	payloads map[string]string // client message id -> content (C02 conflict detection)

	nextDeliveryFault atomic.Pointer[error] // error armed for the next SendRawInput (C03)
}

func newWebChatContractDriver(t *testing.T, h *contracttest.Harness, combo e2econtract.Combination) *webChatContractDriver {
	t.Helper()
	profile := workerProfile(t, combo.Worker)

	auth := security.NewAuthenticator(&config.Default().Security)
	auth.AddKey(wsTestAPIKey)

	// The real gateway WS entry: HTTP upgrade + real Conn/ReadPump (init
	// handshake, seq stamping, owner checks, async dispatch) wired into the
	// harness's Hub/Bridge/Handler. No API key header means deferred init
	// auth; the driver always presents the key, so userID is resolved at the
	// HTTP layer exactly like an API client.
	srv := httptest.NewServer(h.Hub.HandleHTTP(auth, h.Handler, h.Bridge, nil))
	t.Cleanup(srv.Close)

	return &webChatContractDriver{
		h:         h,
		profile:   profile,
		wsURL:     "ws" + srv.URL[4:],
		payloads:  make(map[string]string),
		wsLog:     make([]*events.Envelope, 0, 64),
		clientKey: "unset",
	}
}

func (d *webChatContractDriver) BeginScenario(t testing.TB, scenarioID string) {
	d.t = t
	// Unique client identity per scenario: the init envelope's session_id is a
	// client key, so the server derives a fresh session per scenario while
	// C05's within-scenario reuse still holds (same WS conn, same session).
	d.clientKey = "ws-" + scenarioID
	ws, sessionID, err := d.dialAndInit()
	require.NoError(t, err, "webchat: real AEP init handshake must succeed")
	d.ws = ws
	d.sessionID = sessionID
	d.startWSReader()
	d.rec = newRecordingConn()
	d.h.Hub.JoinPlatformSession(d.sessionID, d.rec)
	d.payloads = make(map[string]string)
	d.worker = nil
	d.nextDeliveryFault.Store(nil)
	d.rec.nextTerminalFault.Store(nil)
	d.rec.dropDeltas.Store(false)
}

func (d *webChatContractDriver) EndScenario(t testing.TB) {
	// Clear fault state so a stale arm cannot leak into the next scenario.
	d.nextDeliveryFault.Store(nil)
	d.rec.nextTerminalFault.Store(nil)
	d.rec.dropDeltas.Store(false)
	// Close the real WS conn: the server-side ReadPump then releases the
	// webchat owner, unregisters the conn and transitions the session. The
	// reader goroutine exits on the close error.
	if d.ws != nil {
		_ = d.ws.Close()
		select {
		case <-d.readDone:
		case <-time.After(driverWaitTimeout):
		}
		d.ws = nil
	}
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

func (d *webChatContractDriver) SendRawInput(ctx context.Context, id, content string) error {
	if f := d.nextDeliveryFault.Swap(nil); f != nil {
		// C03: the client's send failed before the envelope entered the
		// gateway; nothing was accepted, so the same id is retryable at the
		// safe stage.
		return *f
	}
	// Re-join a fresh observation conn per input: after a faulted terminal
	// write (C07) the hub detaches the failing writer, so the driver
	// reconnects like the real platform would. The per-turn snapshot is
	// observed on the fresh conn. The runner arms scenario faults (C07
	// terminal fault, C08 delta saturation) BEFORE SendRawInput, so the arms
	// must be transferred from the previous conn to the fresh one — otherwise
	// the fault fires on the orphaned conn while the driver observes an
	// un-armed stream.
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
	// Wait for the gateway's delivery-outcome ack (sent after the worker
	// accepted the input) so DeliveredInputs is settled when the runner
	// asserts it.
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
				outcome = fmt.Errorf("webchat contract driver: input %s failed delivery: %s", id, ack.ErrorCode)
				return true
			}
		}
		return false
	}, driverWaitTimeout, driverWaitPoll, "webchat driver: input %s never reached the worker", id)
	return outcome
}

func (d *webChatContractDriver) SendRawDuplicate(ctx context.Context, id, content string) error {
	if prev, ok := d.payloads[id]; ok && prev != content {
		// C02: a different payload with the same id. There is no adapter-level
		// dedup on the WS path — the execution store rejects the conflict and
		// the gateway emits an INVALID_MESSAGE error envelope; surface it.
		if err := d.send(ctx, id, content); err != nil {
			return err
		}
		return d.waitConflict(id)
	}
	// Same payload: the execution store suppresses the replay (duplicate ack,
	// no worker call) — return nil once the replay ack is observed so
	// DeliveredInputs is settled.
	if err := d.send(ctx, id, content); err != nil {
		return err
	}
	return d.waitReplayAck(id)
}

func (d *webChatContractDriver) FailNextDelivery(err error) {
	d.nextDeliveryFault.Store(&err)
}

func (d *webChatContractDriver) SendStopTwice(ctx context.Context) error {
	if err := d.sendControl(ctx, events.ControlActionStop); err != nil {
		return err
	}
	return d.sendControl(ctx, events.ControlActionStop)
}

func (d *webChatContractDriver) Reset(ctx context.Context) error {
	// The real webchat client sends a control.reset envelope; the post-reset
	// turn is observed from a fresh snapshot (SendRawInput replaces the
	// recording conn anyway), and the reset's context_reset State envelope is
	// not a terminal, so either position is harmless.
	return d.sendControl(ctx, events.ControlActionReset)
}

func (d *webChatContractDriver) FailNextTerminal(stage contracttest.TerminalFailureStage, err error) {
	// The three stages (increment/close/fallback) collapse onto the recording
	// conn's single terminal-delivery point; the armed error is returned from
	// the terminal write while the terminal stays observable once.
	_ = stage
	d.rec.nextTerminalFault.Store(&err)
}

func (d *webChatContractDriver) SaturateDeltaQueue(_ context.Context) error {
	// The turn produces a single coalesced delta; the saturation effect is the
	// droppable delta being discarded while the terminal is retained.
	d.rec.dropDeltas.Store(true)
	return nil
}

func (d *webChatContractDriver) WaitForTerminal(t testing.TB) []*events.Envelope {
	t.Helper()
	var log []*events.Envelope
	require.Eventually(t, func() bool {
		log = d.snapshot()
		return len(terminalsIn(log)) >= 1
	}, driverWaitTimeout, driverWaitPoll, "webchat driver: no terminal observed")
	return log
}

func (d *webChatContractDriver) DeliveredInputs() int {
	if d.worker == nil {
		return 0
	}
	return d.worker.InputCalls()
}

func (d *webChatContractDriver) VisibleTerminals() int {
	return len(terminalsIn(d.snapshot()))
}

// ─── driver internals ─────────────────────────────────────────────────────────

// dialAndInit dials the real gateway WS endpoint and completes the real AEP
// init handshake: it writes an init envelope carrying the combo's worker_type
// and the scenario client key, and reads the init_ack, whose authoritative
// session ID anchors all observation.
func (d *webChatContractDriver) dialAndInit() (*websocket.Conn, string, error) {
	conn, _, err := websocket.DefaultDialer.Dial(d.wsURL, http.Header{"X-API-Key": []string{wsTestAPIKey}})
	if err != nil {
		return nil, "", fmt.Errorf("webchat driver: dial: %w", err)
	}
	initEnvelope := map[string]any{
		"version": events.Version,
		"id":      aep.NewID(),
		"event": map[string]any{
			"type": string(events.Init),
			"data": map[string]any{
				"version":     events.Version,
				"worker_type": string(d.profile.Type),
				"session_id":  d.clientKey,
			},
		},
	}
	if err := conn.WriteJSON(initEnvelope); err != nil {
		_ = conn.Close()
		return nil, "", fmt.Errorf("webchat driver: write init: %w", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(driverWaitTimeout))
	var resp map[string]any
	if err := conn.ReadJSON(&resp); err != nil {
		_ = conn.Close()
		return nil, "", fmt.Errorf("webchat driver: read init_ack: %w", err)
	}
	_ = conn.SetReadDeadline(time.Time{})
	evt, _ := resp["event"].(map[string]any)
	if evt == nil || evt["type"] != string(gateway.InitAck) {
		_ = conn.Close()
		return nil, "", fmt.Errorf("webchat driver: expected init_ack, got %v", resp)
	}
	data, _ := evt["data"].(map[string]any)
	if data == nil || data["session_id"] == "" {
		_ = conn.Close()
		return nil, "", fmt.Errorf("webchat driver: init_ack missing session_id: %v", resp)
	}
	if code, _ := data["code"].(string); code != "" {
		_ = conn.Close()
		return nil, "", fmt.Errorf("webchat driver: init rejected with %s: %v", code, resp)
	}
	sessionID, _ := data["session_id"].(string)
	return conn, sessionID, nil
}

// startWSReader drains the real socket so the server's write path never
// backs up, accumulating every envelope the webchat client receives on the
// wire (init_ack was already consumed by dialAndInit).
func (d *webChatContractDriver) startWSReader() {
	d.readDone = make(chan struct{})
	go func() {
		defer close(d.readDone)
		for {
			_, data, err := d.ws.ReadMessage()
			if err != nil {
				return
			}
			var env events.Envelope
			if err := json.Unmarshal(data, &env); err != nil {
				continue
			}
			d.wsLogMu.Lock()
			d.wsLog = append(d.wsLog, &env)
			d.wsLogMu.Unlock()
		}
	}()
}

// wsSawContent returns a predicate reporting whether the real WS client
// received an envelope whose content contains text (the worker fixture text
// over the actual wire).
func (d *webChatContractDriver) wsSawContent(text string) func() bool {
	return func() bool {
		d.wsLogMu.Lock()
		defer d.wsLogMu.Unlock()
		for _, env := range d.wsLog {
			if data, ok := env.Event.Data.(map[string]any); ok {
				if c, _ := data["content"].(string); strings.Contains(c, text) {
					return true
				}
			}
		}
		return false
	}
}

// send enters the REAL raw ingress: an AEP input envelope over the scenario's
// WebSocket connection, with the client message id in env.id (the execution
// store's idempotency key).
func (d *webChatContractDriver) send(ctx context.Context, id, content string) error {
	env := map[string]any{
		"version": events.Version,
		"id":      id,
		"event": map[string]any{
			"type": string(events.Input),
			"data": map[string]any{"content": content},
		},
	}
	return d.ws.WriteJSON(env)
}

// sendControl writes a control envelope (stop/reset) over the real socket.
func (d *webChatContractDriver) sendControl(_ context.Context, action events.ControlAction) error {
	env := map[string]any{
		"version": events.Version,
		"id":      aep.NewID(),
		"event": map[string]any{
			"type": string(events.Control),
			"data": map[string]any{"action": string(action)},
		},
	}
	return d.ws.WriteJSON(env)
}

// waitReplayAck waits for the duplicate suppression ack (same id + same
// payload) so DeliveredInputs is settled before the runner asserts it.
func (d *webChatContractDriver) waitReplayAck(id string) error {
	require.Eventually(d.t, func() bool {
		for _, env := range d.rec.Events() {
			ack, ok := env.Event.Data.(events.InputAckData)
			if ok && ack.ClientMessageID == id && ack.Duplicate {
				return true
			}
		}
		return false
	}, driverWaitTimeout, driverWaitPoll, "webchat driver: no replay ack observed for message %s", id)
	return nil
}

// waitConflict waits for the gateway's stable payload-conflict rejection
// (error envelope with INVALID_MESSAGE, emitted by the execution store's
// Accept) and surfaces it as a driver error.
func (d *webChatContractDriver) waitConflict(id string) error {
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
			conflictErr = fmt.Errorf("webchat contract driver: payload conflict for message %s: %s", id, ed.Message)
			return true
		}
		return false
	}, driverWaitTimeout, driverWaitPoll, "webchat driver: no conflict error observed for message %s", id)
	return conflictErr
}

// snapshot returns the client-visible turn stream. Control-plane artifacts the
// real webchat client renders nothing for are excluded:
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
//     client renders nothing for, hence excluded from the stream.
//   - anything after the first terminal — the stream is anchored at the
//     terminal (AEP: done is the last S→C event of a turn).
func (d *webChatContractDriver) snapshot() []*events.Envelope {
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
