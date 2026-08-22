package gateway

import (
	"context"
	"errors"
	"log/slog"
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

type fixedSkillsLocator struct {
	items []skills.Skill
}

func (l fixedSkillsLocator) List(context.Context, string, string) ([]skills.Skill, error) {
	return l.items, nil
}

func (fixedSkillsLocator) Close() {}

type recordedSkillWorker struct {
	mockWorkerForHandler
	invocation worker.NativeCommandInvocation
}

func (w *recordedSkillWorker) InvokeNativeCommand(_ context.Context, invocation worker.NativeCommandInvocation) error {
	w.invocation = invocation
	return nil
}

type advertisedSkillWorker struct {
	recordedSkillWorker
	descriptors []worker.NativeCommandDescriptor
}

func (w *advertisedSkillWorker) ListNativeCommands(context.Context, string) ([]worker.NativeCommandDescriptor, error) {
	return w.descriptors, nil
}

func TestHandleInputFilesystemOnlySkill(t *testing.T) {
	t.Parallel()

	sm := new(mockInputSM)
	w := &recordedSkillWorker{}
	si := &session.SessionInfo{ID: "s1", State: events.StateRunning, WorkDir: "/workspace", Platform: "webchat"}
	sm.On("Get", "s1").Return(si, nil).Maybe()
	sm.On("GetWorker", "s1").Return(w).Maybe()

	locator := fixedSkillsLocator{items: []skills.Skill{{
		Name:     "oracle-dba",
		FilePath: "/workspace/.agents/skills/oracle-dba/SKILL.md",
	}}}
	hub := newTestHub(t)
	go hub.Run()
	capture := &recordingSessionWriter{}
	hub.mu.Lock()
	hub.sessions["s1"] = map[SessionWriter]bool{capture: true}
	hub.everHadConn["s1"] = true
	hub.mu.Unlock()
	h := &Handler{
		log:           slog.Default(),
		hub:           hub,
		sm:            sm,
		skillsLocator: locator,
		catalogStore:  newSessionCatalogStore(slog.Default(), locator),
	}

	// Filesystem discovery is still visible through the real /skills listing,
	// but a native invoker without an authoritative catalog cannot claim that
	// the Worker can execute the discovered Skill.
	require.NoError(t, h.handleSkillsList(t.Context(), &events.Envelope{SessionID: "s1"}, w, ""))
	listing := capture.waitFor(t, "s1", events.SkillsList)
	data, ok := listing.Event.Data.(events.SkillsListData)
	require.True(t, ok)
	var found *events.SkillEntry
	for i := range data.Skills {
		if data.Skills[i].Name == "oracle-dba" {
			found = &data.Skills[i]
			break
		}
	}
	require.NotNil(t, found)
	require.Equal(t, events.SkillStatusDiscoverable, found.Status)

	for _, content := range []string{
		"/oracle-dba 10.102.78.1",
		"/worker oracle-dba 10.102.78.1",
	} {
		err := h.handleInput(context.Background(), inputEnvelopeWithMetadata("s1", content, nil))
		require.Error(t, err, "%s must not be callable from filesystem discovery alone", content)
		require.ErrorContains(t, err, string(events.ErrCodeNotSupported))
	}
	require.Empty(t, w.invocation)
	require.Empty(t, w.Calls)
	sm.AssertExpectations(t)
}

func TestHandleInputWorkerAdvertisedSkillBothEntryForms(t *testing.T) {
	t.Parallel()

	sm := new(mockInputSM)
	w := &advertisedSkillWorker{descriptors: []worker.NativeCommandDescriptor{{
		Name:        "oracle-dba",
		Kind:        worker.NativeCommandKindSkill,
		Mode:        worker.SkillModeTextCommand,
		StartsTurn:  true,
		AcceptsArgs: true,
		Path:        "/private/workspace/oracle-dba/SKILL.md",
	}}}
	si := &session.SessionInfo{ID: "s1", State: events.StateRunning, WorkDir: "/workspace", Platform: "webchat"}
	sm.On("Get", "s1").Return(si, nil).Maybe()
	sm.On("GetWorker", "s1").Return(w).Maybe()

	locator := fixedSkillsLocator{items: []skills.Skill{{
		Name:     "oracle-dba",
		FilePath: "/workspace/.agents/skills/oracle-dba/SKILL.md",
	}}}
	h := newInputHandler(t, sm)
	h.skillsLocator = locator
	h.catalogStore = newSessionCatalogStore(slog.Default(), locator)

	for _, content := range []string{
		"/worker oracle-dba 10.102.78.1",
		"/oracle-dba 10.102.78.1",
	} {
		require.NoError(t, h.handleInput(t.Context(), inputEnvelopeWithMetadata("s1", content, nil)), content)
		require.Equal(t, "oracle-dba", w.invocation.Name)
		require.Equal(t, "10.102.78.1", w.invocation.Args)
		require.Equal(t, "/private/workspace/oracle-dba/SKILL.md", w.invocation.Path)
		require.Equal(t, worker.SkillModeTextCommand, w.invocation.Mode)
	}
	sm.AssertExpectations(t)
}

func TestExplicitWorkerFilesystemOnlySkill(t *testing.T) {
	t.Parallel()

	sm := new(mockInputSM)
	w := &recordedSkillWorker{}
	si := sessionWithWorkDir("s1")
	sm.On("Get", "s1").Return(si, nil).Maybe()
	sm.On("GetWorker", "s1").Return(w).Maybe()
	locator := fixedSkillsLocator{items: []skills.Skill{{
		Name:     "oracle-dba",
		FilePath: "/workspace/.agents/skills/oracle-dba/SKILL.md",
	}}}
	h := newInputHandler(t, sm)
	h.skillsLocator = locator
	h.catalogStore = newSessionCatalogStore(slog.Default(), locator)

	err := h.handleInput(t.Context(), inputEnvelopeWithMetadata("s1", "/worker oracle-dba 10.102.78.1", nil))
	require.Error(t, err)
	require.ErrorContains(t, err, string(events.ErrCodeNotSupported))
	require.Empty(t, w.invocation)
	require.Empty(t, w.Calls)
}

func TestHandleInputUnknownSkillDoesNotReachWorker(t *testing.T) {
	t.Parallel()

	sm := new(mockInputSM)
	w := &recordedSkillWorker{}
	sm.On("Get", "s1").Return(sessionWithWorkDir("s1"), nil).Maybe()
	sm.On("GetWorker", "s1").Return(w).Maybe()
	locator := fixedSkillsLocator{items: []skills.Skill{{
		Name:     "oracle-dba",
		FilePath: "/workspace/.agents/skills/oracle-dba/SKILL.md",
	}}}
	h := newInputHandler(t, sm)
	h.skillsLocator = locator
	h.catalogStore = newSessionCatalogStore(slog.Default(), locator)

	err := h.handleInput(t.Context(), inputEnvelopeWithMetadata("s1", "/missing-skill", nil))
	require.Error(t, err)
	require.ErrorContains(t, err, string(events.ErrCodeNotSupported))
	require.Empty(t, w.invocation)
	require.Empty(t, w.Calls)
}

func TestHandleInputKnownSkillRequiresWorkerAdvertisement(t *testing.T) {
	t.Parallel()

	sm := new(mockInputSM)
	w := &advertisedSkillWorker{descriptors: []worker.NativeCommandDescriptor{{Name: "other", Kind: worker.NativeCommandKindSkill}}}
	si := &session.SessionInfo{ID: "s1", State: events.StateRunning, WorkDir: "/workspace", Platform: "webchat"}
	sm.On("Get", "s1").Return(si, nil).Maybe()
	sm.On("GetWorker", "s1").Return(w).Maybe()

	h := newInputHandler(t, sm)
	h.skillsLocator = fixedSkillsLocator{items: []skills.Skill{{
		Name:     "oracle-dba",
		FilePath: "/workspace/.agents/skills/oracle-dba/SKILL.md",
	}}}
	h.catalogStore = newSessionCatalogStore(slog.Default(), h.skillsLocator)

	err := h.handleInput(context.Background(), inputEnvelopeWithMetadata("s1", "/oracle-dba 10.102.78.1", nil))
	require.Error(t, err)
	require.ErrorContains(t, err, string(events.ErrCodeNotSupported))
	require.Empty(t, w.invocation)
	require.Empty(t, w.Calls)
	sm.AssertExpectations(t)
}

// TestHandleInputOrdinaryTextSkipsSkillResolution guards the hot path:
// non-slash input must not pay the session lookup + filesystem catalog scan
// that Skill resolution requires.
func TestHandleInputOrdinaryTextSkipsSkillResolution(t *testing.T) {
	t.Parallel()

	sm := new(mockInputSM)
	w := new(mockWorkerForHandler)
	w.On("Input", mock.Anything, "hello world", mock.Anything).Return(nil).Once()
	si := &session.SessionInfo{ID: "s1", State: events.StateRunning, WorkDir: "/workspace", Platform: "webchat"}
	// Delivery performs two Gets (session check + post-accept re-read);
	// resolution must add none for non-slash input.
	sm.On("Get", "s1").Return(si, nil).Times(2)
	sm.On("GetWorker", "s1").Return(w).Once()

	h := newInputHandler(t, sm)
	// "hello" is a known Skill name, but without the slash prefix the input
	// is ordinary text and must never reach the catalog.
	h.skillsLocator = fixedSkillsLocator{items: []skills.Skill{{Name: "hello", FilePath: "/x/SKILL.md"}}}

	require.NoError(t, h.handleInput(t.Context(), inputEnvelopeWithMetadata("s1", "hello world", nil)))
	w.AssertExpectations(t)
	sm.AssertExpectations(t)
}

// TestHandleInputSlashOnMissingSessionKeepsNotFoundSemantics covers the
// concurrent-delete race: Skill resolution must fall through so the client
// sees the canonical session-not-found error instead of a generic internal
// "skill resolution failed" error.
func TestHandleInputSlashOnMissingSessionKeepsNotFoundSemantics(t *testing.T) {
	t.Parallel()

	sm := new(mockInputSM)
	sm.On("Get", "s1").Return(nil, errors.New("session gone"))

	h := newInputHandler(t, sm)
	h.skillsLocator = fixedSkillsLocator{items: []skills.Skill{{Name: "oracle-dba", FilePath: "/x/SKILL.md"}}}

	err := h.handleInput(t.Context(), inputEnvelopeWithMetadata("s1", "/oracle-dba 10.102.78.1", nil))
	require.Error(t, err)
	require.ErrorContains(t, err, string(events.ErrCodeSessionNotFound))
}

func TestHandleInputKnownSkillPrefersWorkerAuthoritativePath(t *testing.T) {
	t.Parallel()

	// Regression: the dispatched invocation used the HotPlex filesystem path
	// (skill.FilePath) even though the Worker's authoritative catalog was
	// queried; a divergent canonical path (symlinks, /private/var aliases)
	// would leave the native Skill item unresolvable by the Worker.
	sm := new(mockInputSM)
	w := &advertisedSkillWorker{descriptors: []worker.NativeCommandDescriptor{{
		Name: "oracle-dba",
		Path: "/private/workspace/.agents/skills/oracle-dba/SKILL.md",
		Kind: worker.NativeCommandKindSkill,
	}}}
	si := &session.SessionInfo{ID: "s1", State: events.StateRunning, WorkDir: "/workspace", Platform: "webchat"}
	sm.On("Get", "s1").Return(si, nil).Maybe()
	sm.On("GetWorker", "s1").Return(w).Maybe()

	h := newInputHandler(t, sm)
	h.skillsLocator = fixedSkillsLocator{items: []skills.Skill{{
		Name:     "oracle-dba",
		FilePath: "/workspace/.agents/skills/oracle-dba/SKILL.md",
	}}}
	h.catalogStore = newSessionCatalogStore(slog.Default(), h.skillsLocator)

	err := h.handleInput(context.Background(), inputEnvelopeWithMetadata("s1", "/oracle-dba 10.102.78.1", nil))
	require.NoError(t, err)
	require.Equal(t, "/private/workspace/.agents/skills/oracle-dba/SKILL.md", w.invocation.Path)
	sm.AssertExpectations(t)
}

// TestHandleSupplementOnBusy_SkillNeverInjectedAsText covers the busy path:
// a resolved Skill arriving mid-turn must not be degraded to raw slash text
// via InjectMidTurn. With the gate still held it is buffered, carrying the
// resolved invocation stash so the post-Done replay keeps Skill semantics.
func TestHandleSupplementOnBusy_SkillNeverInjectedAsText(t *testing.T) {
	t.Parallel()

	mw := &mockMidTurnWorker{}
	h, sm, _, bridge := newBusyTestHandler(t, mw)
	// Gate held → deliverAsNewTurn observes SESSION_BUSY and falls through to
	// the pending buffer.
	h.executionStore = &fakeExecutionStore{
		acceptErr:    execution.ErrSessionBusy,
		activeRecord: testExecutionRecord(execution.StatusDelivered),
	}
	sm.On("Get", "s").Return(&session.SessionInfo{ID: "s", State: events.StateRunning}, nil).Maybe()

	const content = "/oracle-dba 10.102.78.1"
	env := newInputEnvelope(t, "s", content)
	env.Seq = 5
	inv := &worker.NativeCommandInvocation{
		Name: "oracle-dba",
		Args: "10.102.78.1",
		Path: "/workspace/.agents/skills/oracle-dba/SKILL.md",
		Mode: worker.SkillModeStructuredSkill,
	}

	require.NoError(t, h.handleSupplementOnBusy(t.Context(), env, content, inv))
	require.Equal(t, 0, mw.injectCount, "a busy Skill must not be mid-turn injected as raw text")

	merged, repr, ok := bridge.pending.DrainAndMerge("s")
	require.True(t, ok, "busy Skill must be buffered for post-Done replay")
	require.Equal(t, content, merged)
	got, ok := invocationFromMetadata(repr.Metadata, merged)
	require.True(t, ok, "buffered envelope must carry the resolved invocation stash")
	require.Equal(t, "oracle-dba", got.Name)
	require.Equal(t, "10.102.78.1", got.Args)
}

// TestDeliverReplay_FilesystemOnlyStashRequiresCurrentAdvertisement covers the
// replay path: a stashed filesystem invocation is correlation metadata only;
// when the current Worker has no authoritative advertisement it is rejected
// instead of reaching the native invoker.
func TestDeliverReplay_FilesystemOnlyStashRequiresCurrentAdvertisement(t *testing.T) {
	t.Parallel()

	sm := new(mockInputSM)
	w := &recordedSkillWorker{}
	sm.On("Get", "s").Return(&session.SessionInfo{ID: "s", State: events.StateRunning, Platform: "webchat"}, nil).Maybe()
	sm.On("GetWorker", "s").Return(w).Maybe()

	locator := fixedSkillsLocator{items: []skills.Skill{{
		Name:     "oracle-dba",
		FilePath: "/workspace/.agents/skills/oracle-dba/SKILL.md",
	}}}
	h := newInputHandler(t, sm)
	h.skillsLocator = locator
	h.catalogStore = newSessionCatalogStore(slog.Default(), locator)

	const content = "/oracle-dba 10.102.78.1"
	env := inputEnvelopeWithMetadata("s", content, nil)
	stashInvocation(env, worker.NativeCommandInvocation{
		Name: "oracle-dba",
		Args: "10.102.78.1",
		Path: "/workspace/.agents/skills/oracle-dba/SKILL.md",
		Mode: worker.SkillModeTextCommand,
	}, content)

	err := h.DeliverReplay(t.Context(), env)
	require.Error(t, err)
	require.ErrorContains(t, err, string(events.ErrCodeNotSupported))
	require.Empty(t, w.invocation, "stashed filesystem path must not bypass current Worker evidence")
}

// TestDeliverReplay_StashIgnoredWhenContentDiverged guards the merged-replay
// case: the stash only applies while the replayed content is unchanged, so a
// merged multi-entry replay never replays only its Skill half.
func TestDeliverReplay_StashIgnoredWhenContentDiverged(t *testing.T) {
	t.Parallel()

	sm := new(mockInputSM)
	w := new(mockWorkerForHandler)
	w.On("Input", mock.Anything, "merged list", mock.Anything).Return(nil).Once()
	sm.On("Get", "s").Return(&session.SessionInfo{ID: "s", State: events.StateRunning, Platform: "webchat"}, nil).Maybe()
	sm.On("GetWorker", "s").Return(w).Maybe()

	h := newInputHandler(t, sm)

	env := inputEnvelopeWithMetadata("s", "merged list", nil)
	stashInvocation(env, worker.NativeCommandInvocation{Name: "oracle-dba"}, "/oracle-dba 10.102.78.1")

	require.NoError(t, h.DeliverReplay(t.Context(), env))
	w.AssertExpectations(t)
	w.AssertNotCalled(t, "InvokeSkill", mock.Anything, mock.Anything)
}

// TestDeliverReplay_PrefersStashOverFilesystemHijack guards the replay path
// against the /worker namespace: a filesystem Skill literally named "worker"
// must never hijack a buffered explicit "/worker oracle-dba ..." entry at
// replay time. The content-keyed stash records the buffer-time resolution, but
// current Worker evidence still supplies the callable descriptor and path.
func TestDeliverReplay_PrefersStashOverFilesystemHijack(t *testing.T) {
	t.Parallel()

	sm := new(mockInputSM)
	w := &advertisedSkillWorker{descriptors: []worker.NativeCommandDescriptor{{
		Name:        "oracle-dba",
		Kind:        worker.NativeCommandKindSkill,
		Mode:        worker.SkillModeTextCommand,
		StartsTurn:  true,
		AcceptsArgs: true,
		Path:        "/worker/oracle-dba",
	}}}
	sm.On("Get", "s").Return(&session.SessionInfo{ID: "s", State: events.StateRunning, Platform: "webchat"}, nil).Maybe()
	sm.On("GetWorker", "s").Return(w).Maybe()

	h := newInputHandler(t, sm)
	locator := fixedSkillsLocator{items: []skills.Skill{{
		Name:     "worker",
		FilePath: "/workspace/.agents/skills/worker/SKILL.md",
	}}}
	h.skillsLocator = locator
	h.catalogStore = newSessionCatalogStore(slog.Default(), locator)

	const content = "/worker oracle-dba 10.102.78.1"
	env := inputEnvelopeWithMetadata("s", content, nil)
	stashInvocation(env, worker.NativeCommandInvocation{
		Name: "oracle-dba",
		Args: "10.102.78.1",
		Path: "/stale/path/oracle-dba/SKILL.md",
	}, content)

	require.NoError(t, h.DeliverReplay(t.Context(), env))
	require.Equal(t, "oracle-dba", w.invocation.Name, "stashed oracle-dba must win over the filesystem \"worker\" skill")
	require.Equal(t, "10.102.78.1", w.invocation.Args)
	require.Equal(t, "/worker/oracle-dba", w.invocation.Path, "current Worker evidence must replace the stashed path")
}

func TestDeliverReplayStartsTurnControlUsesCurrentDescriptor(t *testing.T) {
	t.Parallel()

	sm := new(mockInputSM)
	w := &advertisedSkillWorker{descriptors: []worker.NativeCommandDescriptor{{
		Name:       "queue",
		Kind:       worker.NativeCommandKindControl,
		Mode:       worker.SkillModeAdvertisedCommand,
		StartsTurn: true,
		Path:       "/worker/queue",
	}}}
	sm.On("Get", "s").Return(sessionWithWorkDir("s"), nil).Maybe()
	sm.On("GetWorker", "s").Return(w).Maybe()
	h := newInputHandler(t, sm)
	h.catalogStore = newSessionCatalogStore(slog.Default(), nil)

	const content = "/worker queue refresh"
	env := inputEnvelopeWithMetadata("s", content, nil)
	stashInvocation(env, worker.NativeCommandInvocation{
		Name: "queue",
		Args: "refresh",
		Path: "/stale/path/queue",
		Mode: worker.SkillModeTextCommand,
	}, content)

	require.NoError(t, h.DeliverReplay(t.Context(), env))
	require.Equal(t, "queue", w.invocation.Name)
	require.Equal(t, "refresh", w.invocation.Args)
	require.Equal(t, "/worker/queue", w.invocation.Path)
	require.Equal(t, worker.SkillModeAdvertisedCommand, w.invocation.Mode)
}

type failingCatalogWorker struct {
	mockWorkerForHandler
}

func (w *failingCatalogWorker) ListNativeCommands(context.Context, string) ([]worker.NativeCommandDescriptor, error) {
	return nil, errors.New("catalog unavailable")
}

// TestDeliverToWorkerAuthoritativeQueryIsBounded verifies the delivery-path
// authoritative catalog re-validation honors the same "cannot confirm" bound
// as catalog assembly (spec §8.2, §8.5): a hung provider must not stall an
// already-accepted turn beyond the injected timeout.
func TestDeliverToWorkerAuthoritativeQueryIsBounded(t *testing.T) {
	t.Parallel()

	sm := new(mockInputSM)
	w := &nativeCatalogTestWorker{fakeWorker: &fakeWorker{}, blockCtx: true}
	si := &session.SessionInfo{ID: "s1", State: events.StateRunning, WorkDir: "/workspace", Platform: "webchat"}
	sm.On("Get", "s1").Return(si, nil).Maybe()
	sm.On("GetWorker", "s1").Return(w).Maybe()

	hub := newTestHub(t)
	conn := &mockPlatformConn{}
	hub.JoinPlatformSession("s1", conn)
	h := &Handler{
		log:            slog.Default(),
		hub:            hub,
		sm:             sm,
		catalogStore:   newTestCatalogStore(t, nil), // 50ms "cannot confirm" bound
		executionStore: &fakeExecutionStore{record: testExecutionRecord(execution.StatusAccepted)},
	}
	h.skillsLocator = fixedSkillsLocator{items: []skills.Skill{{
		Name:     "oracle-dba",
		FilePath: "/workspace/.agents/skills/oracle-dba/SKILL.md",
	}}}

	start := time.Now()
	err := h.handleInput(t.Context(), inputEnvelopeWithMetadata("s1", "/oracle-dba 10.102.78.1", nil))
	require.Error(t, err)
	require.ErrorContains(t, err, string(events.ErrCodeNotSupported))
	require.Less(t, time.Since(start), 2*time.Second, "delivery-path catalog query must honor the injected bound")
	sm.AssertExpectations(t)
}

func TestSkillsListEntriesClassifyMergedCatalog(t *testing.T) {
	t.Parallel()

	// The merged catalog resolves fixed Gateway commands > Worker authoritative
	// catalog > filesystem skills by case-sensitive name (spec §5.2). This test
	// feeds a representative merged snapshot through the entry builder and
	// asserts each tier's Source/Status semantics.
	w := &advertisedSkillWorker{descriptors: []worker.NativeCommandDescriptor{{
		Name: "oracle-dba", Description: "DBA helper", Kind: worker.NativeCommandKindSkill,
		Mode: worker.SkillModeTextCommand, StartsTurn: true, AcceptsArgs: true, Path: "/worker/oracle-dba",
	}}}
	fsSkills := []skills.Skill{
		{Name: "oracle-dba", Description: "DBA helper", Source: skills.SourceProject, Managed: true, FilePath: "/workspace/.agents/skills/oracle-dba/SKILL.md"},
		{Name: "build", Description: "Build helper", Source: skills.SourceGlobal, Managed: false, FilePath: "/home/.claude/skills/build/SKILL.md"},
	}
	merged := []worker.NativeCommandDescriptor{
		// Tier 1 — fixed Gateway control command (always-on).
		{Name: "reset", Description: "重置上下文（全新开始）", Kind: worker.NativeCommandKindControl},
		// Tier 2 — Worker authoritative skill claiming a filesystem name.
		{Name: "oracle-dba", Description: "DBA helper", Kind: worker.NativeCommandKindSkill, Mode: worker.SkillModeTextCommand, StartsTurn: true, AcceptsArgs: true, Path: "/worker/oracle-dba"},
		// Tier 2 — Worker-only command with no filesystem counterpart.
		{Name: "queue", Description: "queue status", Kind: worker.NativeCommandKindSkill, Mode: worker.SkillModeAdvertisedCommand, StartsTurn: true},
		// Tier 2 — Worker-advertised control command (e.g. ACP available_commands_update).
		{Name: "tools", Description: "tool usage", Kind: worker.NativeCommandKindControl, Mode: worker.SkillModeAdvertisedCommand},
		// Tier 3 — filesystem-only skill (filesystem-tier descriptor shape).
		{Name: "build", Description: "Build helper", Kind: worker.NativeCommandKindSkill, Mode: worker.NativeModeForType(worker.TypeClaudeCode), StartsTurn: true, AcceptsArgs: true, Path: "/home/.claude/skills/build/SKILL.md"},
	}

	entries := buildSkillEntriesFromCatalog(merged, fsSkills, w, true)
	byName := make(map[string]events.SkillEntry, len(entries))
	for _, e := range entries {
		byName[e.Name] = e
	}

	t.Run("fixed gateway command is gateway callable", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, "gateway", byName["reset"].Source)
		require.Equal(t, events.SkillStatusCallable, byName["reset"].Status)
	})

	t.Run("filesystem skill confirmed by authoritative is callable", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, skills.SourceProject, byName["oracle-dba"].Source)
		require.True(t, byName["oracle-dba"].Managed)
		require.Equal(t, events.SkillStatusCallable, byName["oracle-dba"].Status)
	})

	t.Run("worker-only command is worker callable", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, "worker", byName["queue"].Source)
		require.Equal(t, events.SkillStatusCallable, byName["queue"].Status)
	})

	t.Run("worker-advertised control is worker callable", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, "worker", byName["tools"].Source)
		require.Equal(t, events.SkillStatusCallable, byName["tools"].Status)
	})

	t.Run("filesystem-only skill is discoverable", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, skills.SourceGlobal, byName["build"].Source)
		require.False(t, byName["build"].Managed)
		require.Equal(t, events.SkillStatusDiscoverable, byName["build"].Status)
	})
}

func TestSkillsListEntriesCapabilityGatedFixedCommand(t *testing.T) {
	t.Parallel()

	// /context is a ControlRequester-gated fixed command. A plain worker that
	// lacks the surface must not surface it as a Gateway command even if a
	// worker descriptor shares the name.
	w := &advertisedSkillWorker{descriptors: []worker.NativeCommandDescriptor{{
		Name: "context", Description: "worker context", Kind: worker.NativeCommandKindControl,
	}}}
	merged := []worker.NativeCommandDescriptor{
		{Name: "context", Description: "worker context", Kind: worker.NativeCommandKindControl},
	}

	entries := buildSkillEntriesFromCatalog(merged, nil, w, true)
	require.Len(t, entries, 1)
	require.Equal(t, "worker", entries[0].Source, "plain worker cannot expose the ControlRequester-gated /context as a gateway command")
	require.Equal(t, events.SkillStatusCallable, entries[0].Status)

	// A ControlRequester worker enables the same name as a fixed Gateway command.
	ctrl := &advertisedControlSkillWorker{advertisedSkillWorker: advertisedSkillWorker{descriptors: w.descriptors}}
	entries = buildSkillEntriesFromCatalog(merged, nil, ctrl, true)
	require.Len(t, entries, 1)
	require.Equal(t, "gateway", entries[0].Source)
	require.Equal(t, events.SkillStatusCallable, entries[0].Status)
}

func TestSkillsListEntriesFetchFailureNoCallable(t *testing.T) {
	t.Parallel()

	// authoritativeOK=false simulates sessionCatalogStore.Lookup returning an
	// error (spec §8.5: catalog fetch failure means "cannot confirm"). No entry
	// may be presented as callable except the fixed Gateway tier.
	w := &advertisedSkillWorker{}
	fsSkills := []skills.Skill{
		{Name: "build", Source: skills.SourceGlobal, FilePath: "/home/.claude/skills/build/SKILL.md"},
	}
	merged := []worker.NativeCommandDescriptor{
		{Name: "reset", Kind: worker.NativeCommandKindControl},
		{Name: "build", Kind: worker.NativeCommandKindSkill, Mode: worker.NativeModeForType(worker.TypeClaudeCode), StartsTurn: true, AcceptsArgs: true, Path: "/home/.claude/skills/build/SKILL.md"},
		// Defensive: even a worker-only descriptor must not be callable when the
		// authoritative tier could not be fetched.
		{Name: "queue", Kind: worker.NativeCommandKindSkill, Mode: worker.SkillModeAdvertisedCommand, StartsTurn: true},
	}

	entries := buildSkillEntriesFromCatalog(merged, fsSkills, w, false)
	byName := make(map[string]events.SkillEntry, len(entries))
	for _, e := range entries {
		byName[e.Name] = e
	}

	require.Equal(t, "gateway", byName["reset"].Source)
	require.Equal(t, events.SkillStatusCallable, byName["reset"].Status, "fixed gateway commands stay callable on fetch failure")
	require.Equal(t, events.SkillStatusDiscoverable, byName["build"].Status, "filesystem skill cannot be confirmed callable")
	require.Equal(t, events.SkillStatusDiscoverable, byName["queue"].Status, "worker-only command cannot be confirmed callable")
}

func TestFilterCatalogDescriptors(t *testing.T) {
	t.Parallel()

	descriptors := []worker.NativeCommandDescriptor{
		{Name: "oracle-dba", Description: "DBA helper"},
		{Name: "reset", Description: "重置上下文（全新开始）"},
		{Name: "build", Description: "Build helper"},
	}
	tests := []struct {
		name   string
		filter string
		want   []string
	}{
		{"empty filter keeps all", "", []string{"oracle-dba", "reset", "build"}},
		{"name substring", "oracle", []string{"oracle-dba"}},
		{"description substring", "helper", []string{"oracle-dba", "build"}},
		{"case-insensitive", "ORACLE", []string{"oracle-dba"}},
		{"no match", "zzz", []string{}},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := filterCatalogDescriptors(descriptors, tt.filter)
			names := make([]string, 0, len(got))
			for _, d := range got {
				names = append(names, d.Name)
			}
			require.Equal(t, tt.want, names)
		})
	}
}

func TestHandleSkillsListUsesMergedCatalog(t *testing.T) {
	t.Parallel()

	sm := new(mockInputSM)
	w := &advertisedSkillWorker{descriptors: []worker.NativeCommandDescriptor{
		{Name: "oracle-dba", Description: "DBA helper", Kind: worker.NativeCommandKindSkill, Mode: worker.SkillModeTextCommand, StartsTurn: true, AcceptsArgs: true, Path: "/worker/oracle-dba"},
		{Name: "queue", Description: "queue status", Kind: worker.NativeCommandKindSkill, Mode: worker.SkillModeAdvertisedCommand, StartsTurn: true},
	}}
	si := &session.SessionInfo{ID: "s1", State: events.StateRunning, WorkDir: "/workspace", Platform: "webchat"}
	sm.On("Get", "s1").Return(si, nil).Maybe()
	sm.On("GetWorker", "s1").Return(w).Maybe()

	hub := newTestHub(t)
	go hub.Run()
	capture := &recordingSessionWriter{}
	hub.mu.Lock()
	hub.sessions["s1"] = map[SessionWriter]bool{capture: true}
	hub.everHadConn["s1"] = true
	hub.mu.Unlock()

	locator := fixedSkillsLocator{items: []skills.Skill{
		{Name: "oracle-dba", Source: skills.SourceProject, Managed: true, FilePath: "/workspace/.agents/skills/oracle-dba/SKILL.md"},
		{Name: "build", Description: "Build helper", Source: skills.SourceGlobal, FilePath: "/home/.claude/skills/build/SKILL.md"},
	}}
	h := NewHandler(HandlerDeps{Log: slog.Default(), Hub: hub, SM: sm, SkillsLocator: locator})

	require.NoError(t, h.handleSkillsList(t.Context(), &events.Envelope{SessionID: "s1"}, w, ""))

	skillsEnv := capture.waitFor(t, "s1", events.SkillsList)
	data, ok := skillsEnv.Event.Data.(events.SkillsListData)
	require.True(t, ok, "SkillsList envelope must carry typed SkillsListData")
	byName := make(map[string]events.SkillEntry, len(data.Skills))
	for _, e := range data.Skills {
		byName[e.Name] = e
	}

	require.Equal(t, "gateway", byName["reset"].Source)
	require.Equal(t, events.SkillStatusCallable, byName["reset"].Status)
	require.Equal(t, "worker", byName["queue"].Source)
	require.Equal(t, events.SkillStatusCallable, byName["queue"].Status)
	require.Equal(t, skills.SourceProject, byName["oracle-dba"].Source)
	require.Equal(t, events.SkillStatusCallable, byName["oracle-dba"].Status)
	require.Equal(t, skills.SourceGlobal, byName["build"].Source)
	require.Equal(t, events.SkillStatusDiscoverable, byName["build"].Status)
}

func TestHandleSkillsListLookupErrorNoCallable(t *testing.T) {
	t.Parallel()

	sm := new(mockInputSM)
	w := &failingCatalogWorker{}
	si := &session.SessionInfo{ID: "s1", State: events.StateRunning, WorkDir: "/workspace", Platform: "webchat"}
	sm.On("Get", "s1").Return(si, nil).Maybe()
	sm.On("GetWorker", "s1").Return(w).Maybe()

	hub := newTestHub(t)
	go hub.Run()
	capture := &recordingSessionWriter{}
	hub.mu.Lock()
	hub.sessions["s1"] = map[SessionWriter]bool{capture: true}
	hub.everHadConn["s1"] = true
	hub.mu.Unlock()

	locator := fixedSkillsLocator{items: []skills.Skill{
		{Name: "build", Source: skills.SourceGlobal, FilePath: "/home/.claude/skills/build/SKILL.md"},
	}}
	h := NewHandler(HandlerDeps{Log: slog.Default(), Hub: hub, SM: sm, SkillsLocator: locator})

	require.NoError(t, h.handleSkillsList(t.Context(), &events.Envelope{SessionID: "s1"}, w, ""))

	skillsEnv := capture.waitFor(t, "s1", events.SkillsList)
	data, ok := skillsEnv.Event.Data.(events.SkillsListData)
	require.True(t, ok)
	byName := make(map[string]events.SkillEntry, len(data.Skills))
	for _, e := range data.Skills {
		byName[e.Name] = e
	}

	require.Equal(t, "gateway", byName["reset"].Source)
	require.Equal(t, events.SkillStatusCallable, byName["reset"].Status, "fixed gateway commands stay callable on fetch failure")
	require.Equal(t, events.SkillStatusDiscoverable, byName["build"].Status, "filesystem skill cannot be confirmed callable on fetch failure")
	for name, e := range byName {
		if e.Source == "gateway" {
			require.Equal(t, events.SkillStatusCallable, e.Status, "gateway entry %q", name)
			continue
		}
		require.NotEqual(t, events.SkillStatusCallable, e.Status, "entry %q must not be callable on fetch failure", name)
	}
}

func TestHandleSkillsListFilterStillApplies(t *testing.T) {
	t.Parallel()

	sm := new(mockInputSM)
	w := &advertisedSkillWorker{descriptors: []worker.NativeCommandDescriptor{
		{Name: "queue", Description: "queue status", Kind: worker.NativeCommandKindSkill, Mode: worker.SkillModeAdvertisedCommand, StartsTurn: true},
	}}
	si := &session.SessionInfo{ID: "s1", State: events.StateRunning, WorkDir: "/workspace", Platform: "webchat"}
	sm.On("Get", "s1").Return(si, nil).Maybe()
	sm.On("GetWorker", "s1").Return(w).Maybe()

	hub := newTestHub(t)
	go hub.Run()
	capture := &recordingSessionWriter{}
	hub.mu.Lock()
	hub.sessions["s1"] = map[SessionWriter]bool{capture: true}
	hub.everHadConn["s1"] = true
	hub.mu.Unlock()

	locator := fixedSkillsLocator{items: []skills.Skill{
		{Name: "build", Description: "Build helper", Source: skills.SourceGlobal, FilePath: "/home/.claude/skills/build/SKILL.md"},
	}}
	h := NewHandler(HandlerDeps{Log: slog.Default(), Hub: hub, SM: sm, SkillsLocator: locator})

	// "reset" and "queue" match the filter; "build" and the other fixed
	// commands do not.
	require.NoError(t, h.handleSkillsList(t.Context(), &events.Envelope{SessionID: "s1"}, w, "queue"))

	skillsEnv := capture.waitFor(t, "s1", events.SkillsList)
	data, ok := skillsEnv.Event.Data.(events.SkillsListData)
	require.True(t, ok)
	require.Equal(t, "queue", data.Filter, "SkillsListData must echo the applied filter")
	names := make([]string, 0, len(data.Skills))
	for _, e := range data.Skills {
		names = append(names, e.Name)
	}
	require.ElementsMatch(t, []string{"queue"}, names, "filter must narrow the merged catalog")
	require.Equal(t, len(data.Skills), data.Total)
}

// recordingSessionWriter captures envelopes routed to a session for hub-level
// assertions without a real WebSocket connection.
type recordingSessionWriter struct {
	mu   sync.Mutex
	envs []*events.Envelope
}

func (w *recordingSessionWriter) WriteCtx(_ context.Context, env *events.Envelope) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.envs = append(w.envs, env)
	return nil
}

func (w *recordingSessionWriter) RouteWrite(_ context.Context, env *events.Envelope) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.envs = append(w.envs, env)
	return nil
}

func (w *recordingSessionWriter) RouteWriteData([]byte, events.Kind) error { return nil }
func (w *recordingSessionWriter) Close() error                             { return nil }
func (w *recordingSessionWriter) PreferEnvelope() bool                     { return true }

func (w *recordingSessionWriter) waitFor(t *testing.T, sessionID string, kind events.Kind) *events.Envelope {
	t.Helper()
	var env *events.Envelope
	require.Eventually(t, func() bool {
		w.mu.Lock()
		defer w.mu.Unlock()
		for _, e := range w.envs {
			if e.SessionID == sessionID && e.Event.Type == kind {
				env = e
				return true
			}
		}
		return false
	}, time.Second, 5*time.Millisecond)
	require.NotNil(t, env, "expected a %s envelope for session %s", kind, sessionID)
	return env
}

// advertisedControlSkillWorker adds the ControlRequester capability surface to
// the scripted-catalog worker so capability-gated fixed commands surface.
type advertisedControlSkillWorker struct {
	advertisedSkillWorker
}

func (w *advertisedControlSkillWorker) SendControlRequest(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, nil
}

var _ worker.ControlRequester = (*advertisedControlSkillWorker)(nil)
