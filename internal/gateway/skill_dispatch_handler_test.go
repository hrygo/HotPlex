package gateway

import (
	"context"
	"errors"
	"testing"

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
	invocation worker.SkillInvocation
}

func (w *recordedSkillWorker) InvokeSkill(_ context.Context, invocation worker.SkillInvocation) error {
	w.invocation = invocation
	return nil
}

type advertisedSkillWorker struct {
	recordedSkillWorker
	descriptors []worker.SkillDescriptor
}

func (w *advertisedSkillWorker) ListInvokableSkills(context.Context, string) ([]worker.SkillDescriptor, error) {
	return w.descriptors, nil
}

func TestHandleInputKnownSkillUsesWorkerInvoker(t *testing.T) {
	t.Parallel()

	sm := new(mockInputSM)
	w := &recordedSkillWorker{}
	si := &session.SessionInfo{ID: "s1", State: events.StateRunning, WorkDir: "/workspace", Platform: "webchat"}
	sm.On("Get", "s1").Return(si, nil).Times(3)
	sm.On("GetWorker", "s1").Return(w).Once()

	h := newInputHandler(t, sm)
	h.skillsLocator = fixedSkillsLocator{items: []skills.Skill{{
		Name:     "oracle-dba",
		FilePath: "/workspace/.agents/skills/oracle-dba/SKILL.md",
	}}}

	err := h.handleInput(context.Background(), inputEnvelopeWithMetadata("s1", "/oracle-dba 10.102.78.1", nil))
	require.NoError(t, err)
	require.Equal(t, worker.SkillInvocation{
		Name: "oracle-dba",
		Args: "10.102.78.1",
		Path: "/workspace/.agents/skills/oracle-dba/SKILL.md",
		Mode: worker.SkillModeTextCommand,
	}, w.invocation)
	sm.AssertExpectations(t)
}

func TestHandleInputKnownSkillRequiresWorkerAdvertisement(t *testing.T) {
	t.Parallel()

	sm := new(mockInputSM)
	w := &advertisedSkillWorker{descriptors: []worker.SkillDescriptor{{Name: "other"}}}
	si := &session.SessionInfo{ID: "s1", State: events.StateRunning, WorkDir: "/workspace", Platform: "webchat"}
	sm.On("Get", "s1").Return(si, nil).Times(3)
	sm.On("GetWorker", "s1").Return(w).Once()

	h := newInputHandler(t, sm)
	h.skillsLocator = fixedSkillsLocator{items: []skills.Skill{{
		Name:     "oracle-dba",
		FilePath: "/workspace/.agents/skills/oracle-dba/SKILL.md",
	}}}

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
	w := &advertisedSkillWorker{descriptors: []worker.SkillDescriptor{{
		Name: "oracle-dba",
		Path: "/private/workspace/.agents/skills/oracle-dba/SKILL.md",
	}}}
	si := &session.SessionInfo{ID: "s1", State: events.StateRunning, WorkDir: "/workspace", Platform: "webchat"}
	sm.On("Get", "s1").Return(si, nil).Times(3)
	sm.On("GetWorker", "s1").Return(w).Once()

	h := newInputHandler(t, sm)
	h.skillsLocator = fixedSkillsLocator{items: []skills.Skill{{
		Name:     "oracle-dba",
		FilePath: "/workspace/.agents/skills/oracle-dba/SKILL.md",
	}}}

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
	inv := &worker.SkillInvocation{
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

// TestDeliverReplay_SkillStashSurvivesCatalogRemoval covers the replay path:
// when the Skill vanished from the filesystem catalog between buffering and
// replay, the stashed invocation keeps native semantics instead of degrading
// to raw slash text delivered as an ordinary prompt.
func TestDeliverReplay_SkillStashSurvivesCatalogRemoval(t *testing.T) {
	t.Parallel()

	sm := new(mockInputSM)
	w := &recordedSkillWorker{}
	sm.On("Get", "s").Return(&session.SessionInfo{ID: "s", State: events.StateRunning, Platform: "webchat"}, nil).Maybe()
	sm.On("GetWorker", "s").Return(w).Maybe()

	h := newInputHandler(t, sm) // skillsLocator nil → re-resolution never matches

	const content = "/oracle-dba 10.102.78.1"
	env := inputEnvelopeWithMetadata("s", content, nil)
	stashInvocation(env, worker.SkillInvocation{
		Name: "oracle-dba",
		Args: "10.102.78.1",
		Path: "/workspace/.agents/skills/oracle-dba/SKILL.md",
		Mode: worker.SkillModeTextCommand,
	}, content)

	require.NoError(t, h.DeliverReplay(t.Context(), env))
	require.Equal(t, "oracle-dba", w.invocation.Name, "stashed invocation must reach the native Skill path")
	require.Equal(t, "10.102.78.1", w.invocation.Args)
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
	stashInvocation(env, worker.SkillInvocation{Name: "oracle-dba"}, "/oracle-dba 10.102.78.1")

	require.NoError(t, h.DeliverReplay(t.Context(), env))
	w.AssertExpectations(t)
	w.AssertNotCalled(t, "InvokeSkill", mock.Anything, mock.Anything)
}

type failingCatalogWorker struct {
	mockWorkerForHandler
}

func (w *failingCatalogWorker) ListInvokableSkills(context.Context, string) ([]worker.SkillDescriptor, error) {
	return nil, errors.New("catalog unavailable")
}

func TestSkillsListStatusForWorker(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		worker     worker.Worker
		wantStatus events.SkillStatus
	}{
		{
			name:       "advertised catalog marks callable",
			worker:     &advertisedSkillWorker{descriptors: []worker.SkillDescriptor{{Name: "oracle-dba"}}},
			wantStatus: events.SkillStatusCallable,
		},
		{
			name:       "catalog without skill marks unavailable",
			worker:     &advertisedSkillWorker{descriptors: []worker.SkillDescriptor{{Name: "other"}}},
			wantStatus: events.SkillStatusUnavailable,
		},
		{
			name:       "worker without catalog capability stays discoverable",
			worker:     &recordedSkillWorker{},
			wantStatus: events.SkillStatusDiscoverable,
		},
		{
			name:       "catalog query failure degrades to discoverable",
			worker:     &failingCatalogWorker{},
			wantStatus: events.SkillStatusDiscoverable,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sm := new(mockInputSM)
			sm.On("GetWorker", "s1").Return(tt.worker)

			h := newInputHandler(t, sm)
			status := h.skillStatusForWorker(context.Background(), "s1", "/workspace")
			require.Equal(t, tt.wantStatus, status("oracle-dba"))
			sm.AssertExpectations(t)
		})
	}
}

func TestBuildSkillEntriesAppliesStatusLookup(t *testing.T) {
	t.Parallel()

	allSkills := []skills.Skill{
		{Name: "oracle-dba", Source: skills.SourceProject, Description: "DBA helper"},
		{Name: "build", Source: skills.SourceGlobal, Description: "Build helper"},
	}
	status := func(name string) events.SkillStatus {
		if name == "oracle-dba" {
			return events.SkillStatusCallable
		}
		return events.SkillStatusUnavailable
	}

	entries := buildSkillEntries(allSkills, status)
	require.Len(t, entries, 2)
	require.Equal(t, events.SkillStatusCallable, entries[0].Status)
	require.Equal(t, events.SkillStatusUnavailable, entries[1].Status)
	require.Equal(t, "oracle-dba", entries[0].Name)
	require.Equal(t, skills.SourceProject, entries[0].Source)
	require.Equal(t, "DBA helper", entries[0].Description)
}
