package gateway

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/execution"
	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/internal/skills"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/events"
)

// catalogOnlyWorker advertises a native command catalog but does NOT implement
// worker.NativeCommandInvoker — the "discoverable but not callable" case that
// must resolve to NOT_SUPPORTED instead of degrading to ordinary text.
type catalogOnlyWorker struct {
	mockWorkerForHandler
	descriptors []worker.NativeCommandDescriptor
}

func (w *catalogOnlyWorker) ListNativeCommands(context.Context, string) ([]worker.NativeCommandDescriptor, error) {
	return w.descriptors, nil
}

// captureLogHandler records every slog record as "message key=value ..." lines
// so tests can assert log-key hygiene (spec §8.4) without a real logger.
type captureLogHandler struct {
	mu  sync.Mutex
	buf *bytes.Buffer
}

func (h *captureLogHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *captureLogHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.buf.WriteString(r.Message)
	r.Attrs(func(a slog.Attr) bool {
		h.buf.WriteString(" ")
		h.buf.WriteString(a.Key)
		h.buf.WriteString("=")
		fmt.Fprint(h.buf, a.Value.Any())
		return true
	})
	h.buf.WriteString("\n")
	return nil
}

func (h *captureLogHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *captureLogHandler) WithGroup(string) slog.Handler      { return h }

// sessionWithWorkDir is the shared active session stub for /worker tests.
func sessionWithWorkDir(id string) *session.SessionInfo {
	return &session.SessionInfo{ID: id, State: events.StateRunning, WorkDir: "/workspace", Platform: "webchat"}
}

// oracleDBASkill is the canonical worker-advertised skill used across the
// dispatch tests. StartsTurn=true exercises the durable path.
func oracleDBASkill() []worker.NativeCommandDescriptor {
	return []worker.NativeCommandDescriptor{{
		Name:        "oracle-dba",
		Description: "DBA helper",
		Kind:        worker.NativeCommandKindSkill,
		Mode:        worker.SkillModeTextCommand,
		StartsTurn:  true,
		AcceptsArgs: true,
		Path:        "/private/workspace/.agents/skills/oracle-dba/SKILL.md",
	}}
}

func TestWorkerCommandParseDispatchWithArgsAndNameOnly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		want    worker.NativeCommandInvocation
	}{
		{
			name:    "name with args",
			content: "/worker oracle-dba 10.0.0.1",
			want: worker.NativeCommandInvocation{
				Name: "oracle-dba",
				Args: "10.0.0.1",
				Path: "/private/workspace/.agents/skills/oracle-dba/SKILL.md",
			},
		},
		{
			name:    "name only",
			content: "/worker oracle-dba",
			want: worker.NativeCommandInvocation{
				Name: "oracle-dba",
				Path: "/private/workspace/.agents/skills/oracle-dba/SKILL.md",
			},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sm := new(mockInputSM)
			w := &advertisedSkillWorker{descriptors: oracleDBASkill()}
			sm.On("Get", "s1").Return(sessionWithWorkDir("s1"), nil).Times(3)
			sm.On("GetWorker", "s1").Return(w).Times(2)

			h := newInputHandler(t, sm)
			h.catalogStore = newSessionCatalogStore(slog.Default(), nil)

			err := h.handleInput(context.Background(), inputEnvelopeWithMetadata("s1", tt.content, nil))
			require.NoError(t, err)
			require.Equal(t, tt.want.Name, w.invocation.Name, "parsed command name must reach the native invocation")
			require.Equal(t, tt.want.Args, w.invocation.Args, "parsed args must reach the native invocation")
			require.Equal(t, tt.want.Path, w.invocation.Path, "worker authoritative path must be used")
			require.Equal(t, worker.SkillModeTextCommand, w.invocation.Mode, "mode must be derived from the worker type")
			sm.AssertExpectations(t)
		})
	}
}

func TestWorkerCommandBareFormIsNotSupported(t *testing.T) {
	t.Parallel()

	sm := new(mockInputSM)
	sm.On("GetWorker", "s1").Return(&advertisedSkillWorker{descriptors: oracleDBASkill()}).Maybe()
	sm.On("Get", "s1").Return(sessionWithWorkDir("s1"), nil).Maybe()

	h := newInputHandler(t, sm)
	h.catalogStore = newSessionCatalogStore(slog.Default(), nil)

	for _, content := range []string{"/worker", "/worker ", "/worker   "} {
		err := h.handleInput(context.Background(), inputEnvelopeWithMetadata("s1", content, nil))
		require.Error(t, err, "bare %q must be rejected", content)
		require.ErrorContains(t, err, string(events.ErrCodeNotSupported), "bare %q must map to NOT_SUPPORTED", content)
	}
	sm.AssertExpectations(t)
}

func TestWorkerCommandUnknownNameNotSupportedAndNoFallthrough(t *testing.T) {
	t.Parallel()

	sm := new(mockInputSM)
	w := &advertisedSkillWorker{descriptors: oracleDBASkill()}
	sm.On("Get", "s1").Return(sessionWithWorkDir("s1"), nil).Once()
	sm.On("GetWorker", "s1").Return(w).Once()

	h := newInputHandler(t, sm)
	h.catalogStore = newSessionCatalogStore(slog.Default(), nil)

	err := h.handleInput(context.Background(), inputEnvelopeWithMetadata("s1", "/worker unknown-thing", nil))
	require.Error(t, err)
	require.ErrorContains(t, err, string(events.ErrCodeNotSupported))
	require.Empty(t, w.invocation, "unknown /worker name must never reach the native invoker")
	require.Empty(t, w.Calls, "unknown /worker name must never fall through to the ordinary text path")
	sm.AssertExpectations(t)
}

func TestWorkerCommandUnavailableWithoutInvokerIsNotSupported(t *testing.T) {
	t.Parallel()

	sm := new(mockInputSM)
	w := &catalogOnlyWorker{descriptors: oracleDBASkill()}
	sm.On("Get", "s1").Return(sessionWithWorkDir("s1"), nil).Once()
	sm.On("GetWorker", "s1").Return(w).Once()

	h := newInputHandler(t, sm)
	h.catalogStore = newSessionCatalogStore(slog.Default(), nil)

	err := h.handleInput(context.Background(), inputEnvelopeWithMetadata("s1", "/worker oracle-dba 10.0.0.1", nil))
	require.Error(t, err)
	require.ErrorContains(t, err, string(events.ErrCodeNotSupported))
	require.Empty(t, w.Calls, "a worker without a native invoker must not receive the invocation as text or command")
	sm.AssertExpectations(t)
}

func TestWorkerCommandAmbiguousCaseVariantIsNotSupported(t *testing.T) {
	t.Parallel()

	sm := new(mockInputSM)
	w := &advertisedSkillWorker{descriptors: []worker.NativeCommandDescriptor{
		{Name: "Oracle-DBA", Kind: worker.NativeCommandKindSkill, StartsTurn: true},
		{Name: "ORACLE-DBA", Kind: worker.NativeCommandKindSkill, StartsTurn: true},
	}}
	sm.On("Get", "s1").Return(sessionWithWorkDir("s1"), nil).Once()
	sm.On("GetWorker", "s1").Return(w).Once()

	h := newInputHandler(t, sm)
	h.catalogStore = newSessionCatalogStore(slog.Default(), nil)

	// Exact case-sensitive match fails ("oracle-dba" is not present); two
	// case variants remain → ambiguous, must not guess.
	err := h.handleInput(context.Background(), inputEnvelopeWithMetadata("s1", "/worker oracle-dba", nil))
	require.Error(t, err)
	require.ErrorContains(t, err, string(events.ErrCodeNotSupported))
	require.Empty(t, w.invocation, "ambiguous case variants must never be invoked")
	sm.AssertExpectations(t)
}

func TestWorkerCommandStaleCatalogIsNotSupported(t *testing.T) {
	t.Parallel()

	sm := new(mockInputSM)
	w := &failingCatalogWorker{} // ListNativeCommands returns an error
	sm.On("Get", "s1").Return(sessionWithWorkDir("s1"), nil).Once()
	sm.On("GetWorker", "s1").Return(w).Once()

	h := newInputHandler(t, sm)
	h.catalogStore = newSessionCatalogStore(slog.Default(), nil)

	err := h.handleInput(context.Background(), inputEnvelopeWithMetadata("s1", "/worker oracle-dba", nil))
	require.Error(t, err)
	require.ErrorContains(t, err, string(events.ErrCodeNotSupported))
	require.ErrorContains(t, err, "catalog unavailable", "a degraded catalog must surface an explicit stale-catalog error")
	require.Empty(t, w.Calls, "a degraded catalog must never dispatch anything")
	sm.AssertExpectations(t)
}

// TestWorkerCommandResetStaysFixedDispatch guards the fixed-vs-worker
// namespace isolation (spec §5.3): "/reset" must be consumed by
// tryCommandDispatch (the control path) and never reach the native command
// path.
func TestWorkerCommandResetStaysFixedDispatch(t *testing.T) {
	t.Parallel()

	sm := new(mockInputSM)
	w := &catalogOnlyWorker{descriptors: oracleDBASkill()}
	w.On("ResetContext", mock.Anything).Return(worker.ResetResult{}, nil).Maybe()
	sm.On("Get", "s1").Return(sessionWithWorkDir("s1"), nil).Maybe()
	sm.On("GetWorker", "s1").Return(w).Maybe()

	hub := newTestHub(t)
	h := &Handler{log: slog.Default(), hub: hub, sm: sm, catalogStore: newSessionCatalogStore(slog.Default(), nil), catalogGen: make(map[string]uint64)}

	require.NoError(t, h.handleInput(t.Context(), inputEnvelopeWithMetadata("s1", "/reset", nil)))
	w.AssertCalled(t, "ResetContext", mock.Anything)
	w.AssertNotCalled(t, "InvokeNativeCommand", mock.Anything, mock.Anything)
	sm.AssertExpectations(t)
}

func TestWorkerCommandResetViaWorkerNamespaceNotSupported(t *testing.T) {
	t.Parallel()

	sm := new(mockInputSM)
	w := &catalogOnlyWorker{descriptors: oracleDBASkill()} // worker catalog has no "reset"
	sm.On("Get", "s1").Return(sessionWithWorkDir("s1"), nil).Once()
	sm.On("GetWorker", "s1").Return(w).Once()

	h := newInputHandler(t, sm)
	h.catalogStore = newSessionCatalogStore(slog.Default(), nil)

	// "/worker reset" resolves against the worker namespace: the merged
	// catalog's "reset" is the Gateway fixed entry, which this worker cannot
	// invoke natively → NOT_SUPPORTED, never a /reset control.
	err := h.handleInput(context.Background(), inputEnvelopeWithMetadata("s1", "/worker reset", nil))
	require.Error(t, err)
	require.ErrorContains(t, err, string(events.ErrCodeNotSupported))
	require.Empty(t, w.Calls, "/worker reset must not trigger any worker invocation")
	sm.AssertExpectations(t)
}

// TestWorkerCommandFixedCommandRejectedEvenWithInvoker guards the reserved
// /worker entry against Gateway fixed commands: even a worker WITH a native
// invoker must never receive a fixed command (reset/stop/compact/clear/...)
// through it — dispatching would inject slash text into a running turn
// (claudecode), surface an internal error from the empty fixed Path
// (codexcli), or bypass the busy gate/execution record entirely (spec §5.2).
func TestWorkerCommandFixedCommandRejectedEvenWithInvoker(t *testing.T) {
	t.Parallel()

	sm := new(mockInputSM)
	w := &advertisedSkillWorker{descriptors: oracleDBASkill()} // implements the native invoker
	sm.On("Get", "s1").Return(sessionWithWorkDir("s1"), nil).Once()
	sm.On("GetWorker", "s1").Return(w).Once()

	h := newInputHandler(t, sm)
	h.catalogStore = newSessionCatalogStore(slog.Default(), nil)

	err := h.handleInput(context.Background(), inputEnvelopeWithMetadata("s1", "/worker reset", nil))
	require.Error(t, err)
	require.ErrorContains(t, err, string(events.ErrCodeNotSupported))
	require.Empty(t, w.invocation, "/worker reset must not reach the native invoker")
	require.Empty(t, w.Calls, "/worker reset must not trigger any worker invocation")
	sm.AssertExpectations(t)
}

// TestWorkerCommandStartsTurnDurableDispatch covers the full durable path for
// a StartsTurn=true entry: accept → ACK → active gate → InvokeNativeCommand
// with the resolved invocation, all through the shared delivery pipeline.
func TestWorkerCommandStartsTurnDurableDispatch(t *testing.T) {
	t.Parallel()

	sm := new(mockInputSM)
	w := &advertisedSkillWorker{descriptors: oracleDBASkill()}
	sm.On("Get", "s1").Return(sessionWithWorkDir("s1"), nil).Times(3)
	sm.On("GetWorker", "s1").Return(w).Times(2)

	hub := newTestHub(t)
	conn := &mockPlatformConn{}
	hub.JoinPlatformSession("s1", conn)
	h := &Handler{
		log:            slog.Default(),
		hub:            hub,
		sm:             sm,
		catalogStore:   newSessionCatalogStore(slog.Default(), nil),
		executionStore: &fakeExecutionStore{record: testExecutionRecord(execution.StatusAccepted)},
	}

	env := inputEnvelopeWithMetadata("s1", "/worker oracle-dba 10.0.0.1", nil)
	require.NoError(t, h.handleInput(t.Context(), env))
	require.Equal(t, worker.NativeCommandInvocation{
		Name: "oracle-dba",
		Args: "10.0.0.1",
		Path: "/private/workspace/.agents/skills/oracle-dba/SKILL.md",
		Mode: worker.SkillModeTextCommand,
	}, w.invocation)

	// The durable path must have emitted at least the terminal delivered ACK.
	require.Eventually(t, func() bool {
		for _, got := range conn.envelopes() {
			if got.Event.Type != events.InputAck {
				continue
			}
			data, ok := got.Event.Data.(events.InputAckData)
			if ok && data.ClientMessageID == "evt-client-1" && data.Status == events.ExecutionStatusDelivered {
				return true
			}
		}
		return false
	}, time.Second, 10*time.Millisecond)
	sm.AssertExpectations(t)
}

// TestWorkerCommandControlKindBoundedInvoke covers StartsTurn=false: a bounded
// invoke through the native invoker with a synthetic ACK and no execution
// record.
func TestWorkerCommandControlKindBoundedInvoke(t *testing.T) {
	t.Parallel()

	sm := new(mockInputSM)
	w := &advertisedSkillWorker{descriptors: []worker.NativeCommandDescriptor{{
		Name: "compact", Kind: worker.NativeCommandKindControl, StartsTurn: false,
	}}}
	sm.On("Get", "s1").Return(sessionWithWorkDir("s1"), nil).Once()
	sm.On("GetWorker", "s1").Return(w).Once()

	store := &fakeExecutionStore{record: testExecutionRecord(execution.StatusAccepted)}
	hub := newTestHub(t)
	conn := &mockPlatformConn{}
	hub.JoinPlatformSession("s1", conn)
	h := &Handler{log: slog.Default(), hub: hub, sm: sm, catalogStore: newSessionCatalogStore(slog.Default(), nil), executionStore: store}

	env := inputEnvelopeWithMetadata("s1", "/worker compact", nil)
	require.NoError(t, h.handleInput(t.Context(), env))
	require.Equal(t, worker.NativeCommandInvocation{Name: "compact"}, w.invocation, "control-kind command must be invoked through the native invoker")

	require.Eventually(t, func() bool {
		for _, got := range conn.envelopes() {
			if got.Event.Type != events.InputAck {
				continue
			}
			data, ok := got.Event.Data.(events.InputAckData)
			return ok && data.ClientMessageID == clientMessageID(env) && data.Status == events.ExecutionStatusDelivered
		}
		return false
	}, time.Second, 10*time.Millisecond)
	require.Equal(t, 0, store.statusCalls, "control-kind dispatch must not create an execution record")
	sm.AssertExpectations(t)
}

// TestWorkerCommandBusyBuffersNativeInvocation extends the Skill busy pattern:
// a /worker input arriving while the gate is held must be buffered with the
// resolved NativeCommandInvocation stashed, never injected as mid-turn text.
func TestWorkerCommandBusyBuffersNativeInvocation(t *testing.T) {
	t.Parallel()

	w := &advertisedSkillWorker{descriptors: oracleDBASkill()}
	h, sm, _, bridge := newBusyTestHandler(t, w)
	h.catalogStore = newSessionCatalogStore(slog.Default(), nil)
	h.executionStore = &fakeExecutionStore{
		acceptErr:    execution.ErrSessionBusy,
		activeRecord: testExecutionRecord(execution.StatusDelivered),
	}
	sm.On("Get", "s").Return(sessionWithWorkDir("s"), nil).Maybe()

	const content = "/worker oracle-dba 10.0.0.1"
	env := newInputEnvelope(t, "s", content)
	env.Seq = 5

	require.NoError(t, h.handleInput(t.Context(), env))
	require.Empty(t, w.invocation, "busy native command must not be invoked immediately")

	merged, repr, ok := bridge.pending.DrainAndMerge("s")
	require.True(t, ok, "busy /worker input must be buffered for post-Done replay")
	require.Equal(t, content, merged)
	got, ok := invocationFromMetadata(repr.Metadata, merged)
	require.True(t, ok, "buffered envelope must carry the resolved native invocation stash")
	require.Equal(t, "oracle-dba", got.Name)
	require.Equal(t, "10.0.0.1", got.Args)
	require.Equal(t, "/private/workspace/.agents/skills/oracle-dba/SKILL.md", got.Path)
	require.Equal(t, worker.SkillModeTextCommand, got.Mode, "stashed invocation must preserve the descriptor mode")
}

// TestWorkerCommandCrashReplayCarriesNativeInvocation verifies that the
// crash-replay path replays the structured NativeCommandInvocation (Name, Args,
// Path, Mode) rather than the raw "/worker name args" text.
func TestWorkerCommandCrashReplayCarriesNativeInvocation(t *testing.T) {
	t.Parallel()

	b := &Bridge{log: testLogger(t)}
	w := &recordedSkillWorker{}
	replay := worker.InputReplay{
		Content: "/worker oracle-dba 10.0.0.1",
		Skill: &worker.NativeCommandInvocation{
			Name: "oracle-dba",
			Args: "10.0.0.1",
			Path: "/private/workspace/.agents/skills/oracle-dba/SKILL.md",
			Mode: worker.SkillModeStructuredSkill,
		},
	}

	require.NoError(t, b.deliverInputReplay(t.Context(), w, replay))
	require.Equal(t, *replay.Skill, w.invocation, "replay must use the structured native invocation")
	require.NotEqual(t, "oracle-dba", strings.TrimSpace("/worker oracle-dba 10.0.0.1"), "guard: slash text must never be mistaken for the invocation name")
	w.AssertNotCalled(t, "Input", mock.Anything, mock.Anything, mock.Anything)
}

// TestWorkerCommandDuplicateClientMessageIDIsIdempotent verifies that a
// duplicate client message id is suppressed by the durable accept gate and the
// native command is not invoked twice.
func TestWorkerCommandDuplicateClientMessageIDIsIdempotent(t *testing.T) {
	t.Parallel()

	sm := new(mockInputSM)
	w := &advertisedSkillWorker{descriptors: oracleDBASkill()}
	sm.On("Get", "s1").Return(sessionWithWorkDir("s1"), nil).Times(2)
	sm.On("GetWorker", "s1").Return(w).Once()

	h := newInputHandler(t, sm)
	h.catalogStore = newSessionCatalogStore(slog.Default(), nil)
	h.executionStore = &fakeExecutionStore{record: testExecutionRecord(execution.StatusAccepted), duplicate: true}

	env := inputEnvelopeWithMetadata("s1", "/worker oracle-dba", nil)
	env.ID = "evt-client-1"

	require.NoError(t, h.handleInput(t.Context(), env))
	require.Empty(t, w.invocation, "duplicate client message id must not re-invoke the native command")
	sm.AssertExpectations(t)
}

// TestWorkerCommandDispatchLogHygiene captures the dispatch log and asserts it
// only carries worker_type/command/status/duration_ms/error_class — never
// args, absolute paths, or skill bodies (spec §8.4).
func TestWorkerCommandDispatchLogHygiene(t *testing.T) {
	t.Parallel()

	var logBuf bytes.Buffer
	sm := new(mockInputSM)
	w := &advertisedSkillWorker{descriptors: oracleDBASkill()}
	sm.On("Get", "s1").Return(sessionWithWorkDir("s1"), nil).Times(3)
	sm.On("GetWorker", "s1").Return(w).Times(2)

	h := &Handler{log: slog.New(&captureLogHandler{buf: &logBuf}), sm: sm, catalogStore: newSessionCatalogStore(slog.Default(), nil)}

	require.NoError(t, h.handleInput(t.Context(), inputEnvelopeWithMetadata("s1", "/worker oracle-dba 10.0.0.1", nil)))

	var dispatchLine string
	for _, line := range strings.Split(logBuf.String(), "\n") {
		if strings.HasPrefix(line, "gateway: native command dispatched") {
			dispatchLine = line
			break
		}
	}
	require.NotEmpty(t, dispatchLine, "dispatch must emit a structured log line")
	require.Contains(t, dispatchLine, "worker_type=claude_code")
	require.Contains(t, dispatchLine, "command=oracle-dba")
	require.Contains(t, dispatchLine, "status=")
	require.Contains(t, dispatchLine, "duration_ms=")
	require.Contains(t, dispatchLine, "error_class=")
	require.NotContains(t, dispatchLine, "10.0.0.1", "command args must never appear in dispatch logs")
	require.NotContains(t, dispatchLine, "SKILL.md", "skill body paths must never appear in dispatch logs")
	require.NotContains(t, dispatchLine, "/private/", "absolute paths must never appear in dispatch logs")
}

// TestWorkerCommandNotSlashPrefixFallsThrough guards the fall-through gate:
// a non-"/worker" input must not be consumed by the explicit native command
// branch (it reaches the ordinary delivery path unchanged).
func TestWorkerCommandNotSlashPrefixFallsThrough(t *testing.T) {
	t.Parallel()

	sm := new(mockInputSM)
	w := new(mockWorkerForHandler)
	w.On("Input", mock.Anything, "hello world", mock.Anything).Return(nil).Once()
	si := &session.SessionInfo{ID: "s1", State: events.StateRunning, WorkDir: "/workspace", Platform: "webchat"}
	sm.On("Get", "s1").Return(si, nil).Times(2)
	sm.On("GetWorker", "s1").Return(w).Once()

	h := newInputHandler(t, sm)
	h.catalogStore = newSessionCatalogStore(slog.Default(), nil)

	require.NoError(t, h.handleInput(t.Context(), inputEnvelopeWithMetadata("s1", "hello world", nil)))
	w.AssertExpectations(t)
	sm.AssertExpectations(t)
}

// TestWorkerCommandWorkernameNoSpaceFallsThroughToSkill guards the reserved
// prefix boundary: "/workername" (no space after /worker) is NOT a /worker
// input and must reach the Skill/ordinary path, not be rejected.
func TestWorkerCommandWorkernameNoSpaceFallsThroughToSkill(t *testing.T) {
	t.Parallel()

	sm := new(mockInputSM)
	w := &advertisedSkillWorker{descriptors: []worker.NativeCommandDescriptor{{
		Name:       "workername",
		Kind:       worker.NativeCommandKindSkill,
		StartsTurn: true,
		Path:       "/workspace/.agents/skills/workername/SKILL.md",
	}}}
	// The filesystem tier lists a Skill literally named "workername".
	h := newInputHandler(t, sm)
	h.skillsLocator = fixedSkillsLocator{items: []skills.Skill{{
		Name:     "workername",
		FilePath: "/workspace/.agents/skills/workername/SKILL.md",
	}}}
	h.catalogStore = newSessionCatalogStore(slog.Default(), h.skillsLocator)

	si := &session.SessionInfo{ID: "s1", State: events.StateRunning, WorkDir: "/workspace", Platform: "webchat"}
	sm.On("Get", "s1").Return(si, nil).Times(3)
	sm.On("GetWorker", "s1").Return(w).Once()

	// "/workername" is not a /worker input: it resolves as a filesystem Skill
	// and dispatches through the durable Skill path.
	require.NoError(t, h.handleInput(t.Context(), inputEnvelopeWithMetadata("s1", "/workername", nil)))
	require.Equal(t, "workername", w.invocation.Name, "/workername must resolve as a Skill, not be shadowed by /worker")
	require.Equal(t, worker.SkillModeTextCommand, w.invocation.Mode)
	sm.AssertExpectations(t)
}

// TestWorkerCommandPlainWorkerWithoutCatalogIsNotSupported covers a worker
// with neither a catalog provider nor an invoker: the merged catalog cannot
// confirm anything, so the explicit name resolves to NOT_SUPPORTED.
func TestWorkerCommandPlainWorkerWithoutCatalogIsNotSupported(t *testing.T) {
	t.Parallel()

	sm := new(mockInputSM)
	w := new(mockWorkerForHandler) // no catalog provider, no invoker
	sm.On("Get", "s1").Return(sessionWithWorkDir("s1"), nil).Once()
	sm.On("GetWorker", "s1").Return(w).Once()

	h := newInputHandler(t, sm)
	h.catalogStore = newSessionCatalogStore(slog.Default(), nil)

	err := h.handleInput(context.Background(), inputEnvelopeWithMetadata("s1", "/worker oracle-dba", nil))
	require.Error(t, err)
	require.ErrorContains(t, err, string(events.ErrCodeNotSupported))
	require.Empty(t, w.Calls)
	sm.AssertExpectations(t)
}
