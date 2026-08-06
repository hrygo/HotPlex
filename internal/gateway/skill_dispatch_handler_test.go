package gateway

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

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
