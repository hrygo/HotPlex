// Package contracttest provides test tooling for the platform-worker E2E
// alignment suite (issue #954). The WorkerProbe implements worker.Worker and
// drives hand-written protocol fixtures through each worker type's REAL
// parser/mapper — the four branches use the production decoding/mapping code,
// not hand-built AEP envelopes — so contract assertions exercise the actual
// protocol surface.
package contracttest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/hrygo/hotplex/internal/e2econtract"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/internal/worker/acp"
	"github.com/hrygo/hotplex/internal/worker/claudecode"
	"github.com/hrygo/hotplex/internal/worker/codexcli"
	"github.com/hrygo/hotplex/internal/worker/noop"
	"github.com/hrygo/hotplex/internal/worker/opencodeserver"
	"github.com/hrygo/hotplex/pkg/aep"
	"github.com/hrygo/hotplex/pkg/events"
)

// Compile-time compliance: a future change to worker.Worker must break this
// tool where it is used, not silently at runtime.
var _ worker.Worker = (*WorkerProbe)(nil)

// ─── Hand-written protocol fixtures ──────────────────────────────────────────

// claudeCodeFixture is a hand-written Claude Code SDK stream sample: one
// stream_event (text delta) followed by one result (turn completion). The tuple
// is exactly what the production readLoop feeds to Parser.ParseLine.
var claudeCodeFixture = []string{
	`{"type":"stream_event","event":{"type":"text","message":{"id":"msg_1","role":"assistant","content":"Hello from claude code"}}}`,
	`{"type":"result","result":"turn complete","is_error":false,"num_turns":1}`,
}

// ocEvent represents one raw OCS BusEvent input to Converter.Convert.
type ocEvent struct {
	EventType string
	Props     json.RawMessage
}

// opencodeServerFixture is a hand-written OCS BusEvent sample: one
// message.part.delta (text) followed by one session.status(idle) turn
// terminator.
var opencodeServerFixture = []ocEvent{
	{EventType: "message.part.delta", Props: json.RawMessage(`{"messageID":"m-1","partID":"p-1","field":"text","delta":"Hello from opencode"}`)},
	{EventType: "session.status", Props: json.RawMessage(`{"status":{"type":"idle"}}`)},
}

// codexFixture is a hand-written Codex app-server JSON-RPC notification
// sample: item/started (agent_message), item/agentMessage/delta, item/completed,
// and turn/completed. Each line goes through Parser.ParseNotification then
// Mapper.MapNotification, mirroring the manager's readNotification loop.
var codexFixture = []string{
	`{"method":"item/started","params":{"item":{"id":"item-1","type":"agent_message","role":"assistant"}}}`,
	`{"method":"item/agentMessage/delta","params":{"itemId":"item-1","delta":"Hello from codex"}}`,
	`{"method":"item/completed","params":{"item":{"id":"item-1","type":"agent_message"}}}`,
	`{"method":"turn/completed","params":{}}`,
}

// acpFixture is a hand-written ACP session/update notification sample. It is
// decoded by ACPMapper.MapNotification; the turn is then closed via
// MapPromptResponse (message.end + done) exactly as the worker does after a
// session/prompt resolve.
var acpFixture = &acp.JSONRPCNotification{
	JSONRPC: "2.0",
	Method:  "session/update",
	Params:  json.RawMessage(`{"sessionId":"session-contract","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"Hello from acp"}}}`),
}

// ─── WorkerProbe ─────────────────────────────────────────────────────────────

// WorkerProbe is a test double that embeds the noop worker surface and
// overrides the methods the contract tests exercise. EmitBasicTurn routes the
// hand-written fixture for the probe's worker type through the REAL
// parser/mapper, writing every mapper-produced envelope verbatim into the probe
// connection.
type WorkerProbe struct {
	*noop.Worker
	profile    e2econtract.WorkerProfile
	conn       *probeConn
	inputCalls atomic.Int32
	stopCalls  atomic.Int32 // RAW StopCurrentTurn invocations; the gateway's
	// turn fence (internal/gateway/stop_fence.go) is the authority that admits
	// exactly one effective stop per turn.
	stopped atomic.Bool // per-turn stopped marker: set by StopCurrentTurn,
	// cleared by Input (mirrors production BaseWorker.BeginTurn)
	turnN       atomic.Int64 // current turn ordinal, incremented by Input
	stoppedTurn atomic.Int64 // turn ordinal the stop landed on (per-turn fence)

	// Turn gate for the lifecycle contract tests (C04/C05). Zero value is
	// non-blocking: the platform matrix flows emit the full turn synchronously
	// with Input, exactly as before.
	blocking      atomic.Bool
	enteredTurn   chan struct{} // buffered(1): probe signals the pre-terminal content was emitted
	allowTerminal chan struct{} // buffered(1): test releases the held terminal
	holdTerminal  []*events.Envelope

	failNextStop atomic.Bool // arming the next StopCurrentTurn to fail (failed-abort contract)

	terminateCalls atomic.Int32
	killCalls      atomic.Int32
	failTerminate  atomic.Bool
	failKill       atomic.Bool
	blockStop      atomic.Bool
	blockWait      atomic.Bool
	lateOnDispose  atomic.Bool
	stopEntered    chan struct{}
	allowStop      chan struct{}
	waitEntered    chan struct{}
	allowWait      chan struct{}
}

// FailNextStop arms the NEXT StopCurrentTurn to return an error, simulating a
// real worker abort failure (e.g. OCS 500/timeout). The probe marks the turn
// stopped before failing, matching the production adapters (MarkStopped runs
// before the abort network call).
func (p *WorkerProbe) FailNextStop() { p.failNextStop.Store(true) }

// NewWorkerProbe creates a probe bound to the given worker profile. The probe's
// connection uses sessionID verbatim so contract assertions can match it.
func NewWorkerProbe(profile e2econtract.WorkerProfile, sessionID string) *WorkerProbe {
	return &WorkerProbe{
		Worker:        noop.NewWorker(),
		profile:       profile,
		conn:          newProbeConn(sessionID, "contract-user"),
		enteredTurn:   make(chan struct{}, 1),
		allowTerminal: make(chan struct{}, 1),
		stopEntered:   make(chan struct{}, 1),
		allowStop:     make(chan struct{}, 1),
		waitEntered:   make(chan struct{}, 1),
		allowWait:     make(chan struct{}, 1),
	}
}

// EnableBlocking arms the per-turn gate: EmitBasicTurn holds the terminal
// behind allowTerminal and signals enteredTurn once the pre-terminal content
// was emitted. Only the lifecycle contract tests enable it; the platform
// matrix flows keep the synchronous full-turn emission.
func (p *WorkerProbe) EnableBlocking() { p.blocking.Store(true) }

// MarkExitIntentional flags the probe's upcoming conn close as an intentional
// scenario teardown rather than a crash. The bridge's handleWorkerExit then
// treats the exit as a user stop (session → idle, no crash cleanup, no
// recovery-worker spawn), so the next scenario's init/input cannot race a
// stale recovery attach (webchat matrix flake root cause).
func (p *WorkerProbe) MarkExitIntentional() {
	p.stopped.Store(true)
	p.stoppedTurn.Store(p.turnN.Load())
}

// EnteredTurn returns the per-turn signal fired once the probe emitted the
// pre-terminal content of a turn (and is now holding the terminal).
func (p *WorkerProbe) EnteredTurn() <-chan struct{} { return p.enteredTurn }

// ReleaseTerminal lets the held terminal of the current turn through. When the
// turn was stopped in the meantime, the terminal is suppressed instead.
func (p *WorkerProbe) ReleaseTerminal() {
	select {
	case p.allowTerminal <- struct{}{}:
	default:
	}
}

func (p *WorkerProbe) Type() worker.WorkerType { return p.profile.Type }

func (p *WorkerProbe) SupportsResume() bool      { return p.profile.Resume == e2econtract.Native }
func (p *WorkerProbe) CanResumeTerminated() bool { return false }
func (p *WorkerProbe) SupportsStreaming() bool   { return true }
func (p *WorkerProbe) SupportsTools() bool       { return true }
func (p *WorkerProbe) EnvBlocklist() []string    { return nil }
func (p *WorkerProbe) SessionStoreDir() string   { return "" }
func (p *WorkerProbe) MaxTurns() int             { return 0 }
func (p *WorkerProbe) Modalities() []string      { return []string{"text"} }

func (p *WorkerProbe) Start(_ context.Context, _ worker.SessionInfo) error { return nil }
func (p *WorkerProbe) Resume(_ context.Context, _ worker.SessionInfo) error {
	return nil
}

// Input records the call, resets the per-turn stopped marker (production
// semantics: BaseWorker.BeginTurn clears the user-stop marker before each
// primary turn), then emits one fixture-driven turn through the real
// parser/mapper of the probe's worker type.
func (p *WorkerProbe) Input(_ context.Context, _ string, _ map[string]any) error {
	p.inputCalls.Add(1)
	p.turnN.Add(1)
	p.stopped.Store(false)
	return p.EmitBasicTurn(context.Background())
}

func (p *WorkerProbe) Conn() worker.SessionConn { return p.conn }

func (p *WorkerProbe) ResetContext(_ context.Context) (worker.ResetResult, error) {
	return worker.ResetResult{}, nil
}

// BlockStopCurrentTurn holds StopCurrentTurn after it marks the current turn
// stopped. ReleaseStopCurrentTurn resumes it. The buffered signals make the
// control deterministic regardless of which goroutine reaches the phase first.
func (p *WorkerProbe) BlockStopCurrentTurn() { p.blockStop.Store(true) }

func (p *WorkerProbe) StopEntered() <-chan struct{} { return p.stopEntered }

func (p *WorkerProbe) ReleaseStopCurrentTurn() {
	select {
	case p.allowStop <- struct{}{}:
	default:
	}
}

// BlockWait holds Worker.Wait so tests can prove that stopped_by_user is sent
// only after the old forwarder has completed handleWorkerExit.
func (p *WorkerProbe) BlockWait() { p.blockWait.Store(true) }

func (p *WorkerProbe) WaitEntered() <-chan struct{} { return p.waitEntered }

func (p *WorkerProbe) ReleaseWait() {
	select {
	case p.allowWait <- struct{}{}:
	default:
	}
}

// EmitLateEventsOnDispose makes Terminate enqueue representative events from
// the stopped run immediately before it closes the frozen connection.
func (p *WorkerProbe) EmitLateEventsOnDispose() { p.lateOnDispose.Store(true) }

// EmitLateEventsNow writes the same late-run fixture immediately. It lets a
// contract test reproduce the pre-fix window after the early synthetic done.
func (p *WorkerProbe) EmitLateEventsNow() {
	p.lateOnDispose.Store(true)
	p.emitLateEvents()
}

// FailNextTerminate and FailNextKill arm teardown fallback paths.
func (p *WorkerProbe) FailNextTerminate() { p.failTerminate.Store(true) }
func (p *WorkerProbe) FailNextKill()      { p.failKill.Store(true) }

// StopCurrentTurn records the invocation and marks the current turn stopped.
// The single-effective-stop guarantee is owned by the gateway's turn fence
// (internal/gateway/stop_fence.go), not by this probe: a duplicate stop never
// reaches the Worker call at all, so stopCalls stays 1 under the fence.
func (p *WorkerProbe) StopCurrentTurn(ctx context.Context) error {
	p.stopCalls.Add(1)
	p.stopped.Store(true)
	p.stoppedTurn.Store(p.turnN.Load())
	if p.blockStop.Load() {
		select {
		case p.stopEntered <- struct{}{}:
		default:
		}
		select {
		case <-p.allowStop:
		case <-ctx.Done():
			p.stopped.Store(false)
			return ctx.Err()
		}
	}
	if p.failNextStop.Swap(false) {
		p.stopped.Store(false)
		return errors.New("contracttest: injected stop failure")
	}
	return nil
}

func (p *WorkerProbe) IsStopped() bool { return p.stopped.Load() }

func (p *WorkerProbe) Terminate(_ context.Context) error {
	p.terminateCalls.Add(1)
	if p.failTerminate.Swap(false) {
		return errors.New("contracttest: injected terminate failure")
	}
	p.emitLateEvents()
	return p.conn.Close()
}

func (p *WorkerProbe) Kill() error {
	p.killCalls.Add(1)
	if p.failKill.Swap(false) {
		return errors.New("contracttest: injected kill failure")
	}
	p.emitLateEvents()
	return p.conn.Close()
}

func (p *WorkerProbe) Wait() (int, error) {
	if p.blockWait.Load() {
		select {
		case p.waitEntered <- struct{}{}:
		default:
		}
		<-p.allowWait
	}
	return 0, nil
}

func (p *WorkerProbe) emitLateEvents() {
	if !p.lateOnDispose.Swap(false) {
		return
	}
	late := []*events.Envelope{
		events.NewEnvelope(aep.NewID(), p.conn.sessionID, 0, events.MessageDelta,
			events.MessageDeltaData{MessageID: "late_run_message", Content: "late_run_delta"}),
		events.NewEnvelope(aep.NewID(), p.conn.sessionID, 0, events.Message,
			events.MessageData{ID: "late_run_message", Role: "assistant", Content: "late_run_message"}),
		events.NewEnvelope(aep.NewID(), p.conn.sessionID, 0, events.Reasoning,
			events.ReasoningData{ID: "late_run_reasoning", Content: "late_run_reasoning"}),
		events.NewEnvelope(aep.NewID(), p.conn.sessionID, 0, events.ToolCall,
			events.ToolCallData{ID: "late_run_tool", Name: "late_run_tool", Input: map[string]any{"marker": "late_run_tool"}}),
		events.NewEnvelope(aep.NewID(), p.conn.sessionID, 0, events.PermissionRequest,
			events.PermissionRequestData{ID: "late_run_permission", ToolName: "late_run_permission"}),
		events.NewEnvelope(aep.NewID(), p.conn.sessionID, 0, events.State,
			events.StateData{State: events.StateRunning, Message: "late_run_state"}),
		events.NewEnvelope(aep.NewID(), p.conn.sessionID, 0, events.Done,
			events.DoneData{Success: true, Reason: "late_run_done"}),
		events.NewEnvelope(aep.NewID(), p.conn.sessionID, 0, events.Error,
			events.ErrorData{Code: events.ErrCodeInternalError, Message: "late_run_error"}),
	}
	for _, env := range late {
		p.conn.write(env)
	}
}

// InputCalls returns how many times Input was invoked.
func (p *WorkerProbe) InputCalls() int { return int(p.inputCalls.Load()) }

// StopCalls returns how many times StopCurrentTurn was invoked.
func (p *WorkerProbe) StopCalls() int { return int(p.stopCalls.Load()) }

func (p *WorkerProbe) TerminateCalls() int { return int(p.terminateCalls.Load()) }
func (p *WorkerProbe) KillCalls() int      { return int(p.killCalls.Load()) }

// Events returns every envelope the probe attempted to emit, including writes
// rejected from the live Recv stream after the connection closed.
func (p *WorkerProbe) Events() []*events.Envelope { return p.conn.Events() }

// EmitBasicTurn drives the probe's worker-type fixture through the real
// parser/mapper and writes every mapper-produced envelope verbatim into the
// probe connection. When the turn gate is armed (EnableBlocking), the terminal
// envelopes are held off the wire and Input returns right after the
// pre-terminal content — exactly like a production worker whose Input is a
// delivery while the turn keeps running. The held terminal is released by
// ReleaseTerminal: a turn stopped in the meantime (StopCurrentTurn before the
// release) has its terminal suppressed, so the interrupted turn never
// completes on the wire. Platform queues (feishu chatQueue, slack event
// goroutines, webchat async dispatch) therefore stay free to process the stop
// while the fixture turn is still live.
func (p *WorkerProbe) EmitBasicTurn(_ context.Context) error {
	switch p.profile.Type {
	case worker.TypeClaudeCode:
		if err := p.emitClaude(); err != nil {
			return err
		}
	case worker.TypeOpenCodeSrv:
		if err := p.emitOpenCodeServer(); err != nil {
			return err
		}
	case worker.TypeCodexCLI:
		if err := p.emitCodex(); err != nil {
			return err
		}
	case worker.TypeACP:
		if err := p.emitACP(); err != nil {
			return err
		}
	default:
		return fmt.Errorf("contracttest: unsupported worker type %q", p.profile.Type)
	}
	if !p.blocking.Load() {
		return nil
	}
	// Gate the held terminal: signal the test that the turn reached the wire
	// (start/delta visible), then return — the terminal is finished by the
	// release goroutine below, which observes the stop if one lands in the
	// meantime. The held slice is handed off to the goroutine so a later turn's
	// emission never races it.
	select {
	case p.enteredTurn <- struct{}{}:
	default:
	}
	held := p.holdTerminal
	p.holdTerminal = nil
	go p.finishHeldTurn(held, p.turnN.Load())
	return nil
}

// finishHeldTurn waits for the release, then emits the held terminal or
// suppresses it when THIS turn was stopped in the meantime (per-turn scoped
// via the turn ordinal — a later turn's Input resetting the stopped marker
// must not release an earlier turn's held terminal). It runs in its own
// goroutine per gated turn, and only one release is consumed per turn
// (allowTerminal is buffered(1)).
func (p *WorkerProbe) finishHeldTurn(held []*events.Envelope, turn int64) {
	<-p.allowTerminal
	if p.stopped.Load() && p.stoppedTurn.Load() == turn {
		return
	}
	for _, env := range held {
		p.conn.write(env)
	}
}

// emitClaude pipes each fixture line through claudecode.Parser.ParseLine then
// claudecode.Mapper.Map — the same pipeline as the worker's readLoop.
func (p *WorkerProbe) emitClaude() error {
	parser := claudecode.NewParser(slog.Default())
	mapper := claudecode.NewMapper(slog.Default(), p.conn.sessionID, func() int64 { return 0 })
	for _, line := range claudeCodeFixture {
		evts, err := parser.ParseLine(line)
		if err != nil {
			return fmt.Errorf("contracttest: claudecode parse: %w", err)
		}
		for _, evt := range evts {
			envs, err := mapper.Map(evt)
			if err != nil {
				return fmt.Errorf("contracttest: claudecode map: %w", err)
			}
			p.writeAll(envs)
		}
	}
	return nil
}

// emitOpenCodeServer pipes each fixture event through opencodeserver.Converter
// with the probe session ID — the same call the singleton dispatch makes.
func (p *WorkerProbe) emitOpenCodeServer() error {
	conv := opencodeserver.NewConverter()
	for _, evt := range opencodeServerFixture {
		p.writeAll(conv.Convert(p.conn.sessionID, evt.EventType, evt.Props))
	}
	return nil
}

// emitCodex pipes each fixture line through codexcli.Parser.ParseNotification
// then codexcli.Mapper.MapNotification — the same pipeline as the manager's
// readNotification loop.
func (p *WorkerProbe) emitCodex() error {
	parser := codexcli.NewParser()
	mapper := codexcli.NewMapper(p.conn.sessionID)
	for _, line := range codexFixture {
		method, params, err := parser.ParseNotification(line)
		if err != nil {
			return fmt.Errorf("contracttest: codexcli parse: %w", err)
		}
		p.writeAll(mapper.MapNotification(method, params))
	}
	return nil
}

// emitACP decodes the fixture notification through acp.ACPMapper.MapNotification
// then closes the turn with MapPromptResponse — the same sequence as the
// worker's readNotification + prompt resolve path.
func (p *WorkerProbe) emitACP() error {
	mapper := acp.NewACPMapper(p.conn.sessionID, p.conn.userID, slog.Default())
	p.writeAll(mapper.MapNotification(context.Background(), acpFixture))
	p.writeAll(mapper.MapPromptResponse(&acp.PromptResult{StopReason: "end_turn"}))
	return nil
}

func (p *WorkerProbe) writeAll(envs []*events.Envelope) {
	if !p.blocking.Load() {
		for _, env := range envs {
			p.conn.write(env)
		}
		return
	}
	for _, env := range envs {
		if env.Event.Type == events.Done {
			p.holdTerminal = append(p.holdTerminal, env)
			continue
		}
		p.conn.write(env)
	}
}

// ─── probeConn ───────────────────────────────────────────────────────────────

// probeConn is the SessionConn backing a WorkerProbe. It accumulates every
// envelope written by the mappers (Events) and exposes a live Recv channel for
// anyone consuming the worker interface.
type probeConn struct {
	sessionID string
	userID    string

	mu     sync.Mutex
	events []*events.Envelope
	recvCh chan *events.Envelope
	closed atomic.Bool
}

func newProbeConn(sessionID, userID string) *probeConn {
	return &probeConn{
		sessionID: sessionID,
		userID:    userID,
		events:    make([]*events.Envelope, 0, 16),
		recvCh:    make(chan *events.Envelope, 64),
	}
}

// write appends env to the accumulated event log and mirrors it onto recvCh
// without blocking (a dropped mirror is fine — Events is the source of truth).
func (c *probeConn) write(env *events.Envelope) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, env)
	if c.closed.Load() {
		return
	}
	select {
	case c.recvCh <- env:
	default:
	}
}

// Events returns a snapshot of every envelope written by the mappers.
func (c *probeConn) Events() []*events.Envelope {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]*events.Envelope, len(c.events))
	copy(out, c.events)
	return out
}

func (c *probeConn) Send(_ context.Context, _ *events.Envelope) error { return nil }

func (c *probeConn) Recv() <-chan *events.Envelope { return c.recvCh }

func (c *probeConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed.CompareAndSwap(false, true) {
		close(c.recvCh)
	}
	return nil
}

func (c *probeConn) UserID() string    { return c.userID }
func (c *probeConn) SessionID() string { return c.sessionID }
