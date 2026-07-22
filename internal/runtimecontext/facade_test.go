package runtimecontext

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/agentspec"
)

// quietLogger returns a slog.Discard logger so the facade's best-effort debug
// logs never reach test output.
func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// --- fakes ---

type fakeSessionReader struct {
	sess   *SessionContext
	err    error
	calls  int
	lastID string
}

func (f *fakeSessionReader) Session(_ context.Context, sessionID string) (*SessionContext, error) {
	f.calls++
	f.lastID = sessionID
	return f.sess, f.err
}

type fakeEventReader struct {
	events    []EventSummary
	err       error
	lastLimit int
}

func (f *fakeEventReader) RecentEvents(_ context.Context, _ string, limit int) ([]EventSummary, error) {
	f.lastLimit = limit
	if f.err != nil {
		return nil, f.err
	}
	return f.events, nil
}

type fakeTurnReader struct {
	turns          []TurnSummary
	stats          *TurnStatSummary
	turnsErr       error
	statsErr       error
	lastTurnsLimit int
}

func (f *fakeTurnReader) RecentTurns(_ context.Context, _ string, limit int) ([]TurnSummary, error) {
	f.lastTurnsLimit = limit
	if f.turnsErr != nil {
		return nil, f.turnsErr
	}
	return f.turns, nil
}

func (f *fakeTurnReader) TurnStats(_ context.Context, _ string) (*TurnStatSummary, error) {
	if f.statsErr != nil {
		return nil, f.statsErr
	}
	return f.stats, nil
}

type fakeWorkspaceReader struct {
	ws     *WorkspaceContext
	err    error
	lastID string
}

func (f *fakeWorkspaceReader) Workspace(_ context.Context, workspaceID string) (*WorkspaceContext, error) {
	f.lastID = workspaceID
	if f.err != nil {
		return nil, f.err
	}
	return f.ws, nil
}

func sampleSessionCtx() *SessionContext {
	return &SessionContext{
		WorkerSessionID: "wsess-1",
		WorkDir:         "/repo",
		WorkspaceID:     "ws-1",
		WorkerType:      "claude_code",
		Platform:        "webchat",
		State:           "idle",
		Identity: agentspec.AgentIdentity{
			AgentID:     "aid-1",
			AgentName:   "coding-agent",
			UserID:      "u1",
			WorkspaceID: "ws-1",
			WorkerType:  "claude_code",
		},
	}
}

// --- tests ---

func TestLoad_EmptySessionID(t *testing.T) {
	t.Parallel()
	f := NewFacade(&fakeSessionReader{sess: sampleSessionCtx()}, nil, nil, nil, quietLogger())
	_, err := f.Load(context.Background(), "", ContextLoadOptions{})
	require.ErrorIs(t, err, ErrEmptySessionID)
}

func TestLoad_NoSessionReader(t *testing.T) {
	t.Parallel()
	f := NewFacade(nil, nil, nil, nil, quietLogger())
	_, err := f.Load(context.Background(), "s1", ContextLoadOptions{})
	require.ErrorIs(t, err, errNoSessionReader)
}

func TestLoad_SessionErrorPropagates(t *testing.T) {
	t.Parallel()
	boom := errors.New("store down")
	f := NewFacade(&fakeSessionReader{err: boom}, nil, nil, nil, quietLogger())
	_, err := f.Load(context.Background(), "s1", ContextLoadOptions{})
	require.ErrorIs(t, err, boom)
}

func TestLoad_NilSessionIsNotFound(t *testing.T) {
	t.Parallel()
	f := NewFacade(&fakeSessionReader{sess: nil}, nil, nil, nil, quietLogger())
	_, err := f.Load(context.Background(), "s1", ContextLoadOptions{})
	require.ErrorIs(t, err, errSessionNotFound)
}

func TestLoad_AggregatesAllSources(t *testing.T) {
	t.Parallel()
	sr := &fakeSessionReader{sess: sampleSessionCtx()}
	er := &fakeEventReader{events: []EventSummary{{Seq: 1, Type: "message"}, {Seq: 2, Type: "done"}}}
	tr := &fakeTurnReader{
		turns: []TurnSummary{{TurnNum: 1, Role: "user"}, {TurnNum: 2, Role: "assistant"}},
		stats: &TurnStatSummary{TotalTurns: 2, SuccessTurns: 2},
	}
	wr := &fakeWorkspaceReader{ws: &WorkspaceContext{ID: "ws-1", Name: "proj", WorkDir: "/repo"}}
	f := NewFacade(sr, er, tr, wr, quietLogger())

	snap, err := f.Load(context.Background(), "s1", ContextLoadOptions{})
	require.NoError(t, err)
	require.NotNil(t, snap)

	require.Equal(t, "s1", snap.SessionID)
	require.Equal(t, "wsess-1", snap.Session.WorkerSessionID)
	require.Equal(t, "aid-1", snap.Session.Identity.AgentID)
	require.Len(t, snap.Events, 2)
	require.Equal(t, int64(2), snap.Events[1].Seq)
	require.Len(t, snap.Turns, 2)
	require.NotNil(t, snap.TurnStats)
	require.Equal(t, 2, snap.TurnStats.TotalTurns)
	require.NotNil(t, snap.Workspace)
	require.Equal(t, "proj", snap.Workspace.Name)
	require.NotZero(t, snap.LoadedAt)
	require.Equal(t, "ws-1", wr.lastID, "workspace reader called with session's workspace id")
}

func TestLoad_DefaultLimitsApplied(t *testing.T) {
	t.Parallel()
	er := &fakeEventReader{events: []EventSummary{{Seq: 1}}}
	tr := &fakeTurnReader{turns: []TurnSummary{{TurnNum: 1}}}
	f := NewFacade(&fakeSessionReader{sess: sampleSessionCtx()}, er, tr, &fakeWorkspaceReader{ws: &WorkspaceContext{ID: "ws-1"}}, quietLogger())

	_, err := f.Load(context.Background(), "s1", ContextLoadOptions{}) // zero opts → defaults
	require.NoError(t, err)
	require.Equal(t, DefaultMaxTurns, tr.lastTurnsLimit)
	require.Equal(t, DefaultMaxEvents, er.lastLimit)
}

func TestLoad_ExplicitLimitsRespected(t *testing.T) {
	t.Parallel()
	er := &fakeEventReader{events: []EventSummary{}}
	tr := &fakeTurnReader{turns: []TurnSummary{}}
	f := NewFacade(&fakeSessionReader{sess: sampleSessionCtx()}, er, tr, &fakeWorkspaceReader{ws: &WorkspaceContext{ID: "ws-1"}}, quietLogger())

	_, err := f.Load(context.Background(), "s1", ContextLoadOptions{MaxTurns: 3, MaxEvents: 7})
	require.NoError(t, err)
	require.Equal(t, 3, tr.lastTurnsLimit)
	require.Equal(t, 7, er.lastLimit)
}

func TestLoad_BestEffort_EventsErrorIgnored(t *testing.T) {
	t.Parallel()
	er := &fakeEventReader{err: errors.New("eventstore transient")}
	tr := &fakeTurnReader{turns: []TurnSummary{{TurnNum: 1}}, stats: &TurnStatSummary{TotalTurns: 1}}
	f := NewFacade(&fakeSessionReader{sess: sampleSessionCtx()}, er, tr, &fakeWorkspaceReader{ws: &WorkspaceContext{ID: "ws-1"}}, quietLogger())

	snap, err := f.Load(context.Background(), "s1", ContextLoadOptions{})
	require.NoError(t, err, "event error must not abort Load")
	require.Empty(t, snap.Events, "failed event source leaves events empty")
	require.Len(t, snap.Turns, 1, "other sources still loaded")
}

func TestLoad_BestEffort_TurnsErrorIgnored(t *testing.T) {
	t.Parallel()
	tr := &fakeTurnReader{turnsErr: errors.New("turns transient"), statsErr: errors.New("stats transient")}
	f := NewFacade(&fakeSessionReader{sess: sampleSessionCtx()}, &fakeEventReader{events: []EventSummary{{Seq: 1}}}, tr, nil, quietLogger())

	snap, err := f.Load(context.Background(), "s1", ContextLoadOptions{})
	require.NoError(t, err)
	require.Empty(t, snap.Turns)
	require.Nil(t, snap.TurnStats)
	require.Len(t, snap.Events, 1, "events still loaded")
}

func TestLoad_BestEffort_WorkspaceErrorIgnored(t *testing.T) {
	t.Parallel()
	wr := &fakeWorkspaceReader{err: errors.New("workspace transient")}
	f := NewFacade(&fakeSessionReader{sess: sampleSessionCtx()}, nil, nil, wr, quietLogger())

	snap, err := f.Load(context.Background(), "s1", ContextLoadOptions{})
	require.NoError(t, err)
	require.Nil(t, snap.Workspace)
}

func TestLoad_PlatformSessionNoWorkspaceLookup(t *testing.T) {
	t.Parallel()
	sess := sampleSessionCtx()
	sess.WorkspaceID = "" // platform/cron session
	wr := &fakeWorkspaceReader{ws: &WorkspaceContext{ID: "ws-1"}}
	f := NewFacade(&fakeSessionReader{sess: sess}, nil, nil, wr, quietLogger())

	snap, err := f.Load(context.Background(), "s1", ContextLoadOptions{})
	require.NoError(t, err)
	require.Nil(t, snap.Workspace)
	require.Empty(t, wr.lastID, "workspace reader must not be called when WorkspaceID empty")
}

func TestLoad_SkipOptions(t *testing.T) {
	t.Parallel()
	er := &fakeEventReader{events: []EventSummary{{Seq: 1}}}
	tr := &fakeTurnReader{turns: []TurnSummary{{TurnNum: 1}}, stats: &TurnStatSummary{TotalTurns: 1}}
	wr := &fakeWorkspaceReader{ws: &WorkspaceContext{ID: "ws-1"}}
	f := NewFacade(&fakeSessionReader{sess: sampleSessionCtx()}, er, tr, wr, quietLogger())

	snap, err := f.Load(context.Background(), "s1", ContextLoadOptions{
		MaxTurns:      -1, // skip turns
		MaxEvents:     -1, // skip events
		SkipStats:     true,
		SkipWorkspace: true,
	})
	require.NoError(t, err)
	require.Empty(t, snap.Turns)
	require.Nil(t, snap.TurnStats)
	require.Empty(t, snap.Events)
	require.Nil(t, snap.Workspace)
	require.Empty(t, wr.lastID, "workspace skipped entirely")
}

func TestLoad_NilAdaptersSkipped(t *testing.T) {
	t.Parallel()
	// Only a session reader; events/turns/workspace are nil.
	f := NewFacade(&fakeSessionReader{sess: sampleSessionCtx()}, nil, nil, nil, quietLogger())

	snap, err := f.Load(context.Background(), "s1", ContextLoadOptions{})
	require.NoError(t, err)
	require.Equal(t, "wsess-1", snap.Session.WorkerSessionID)
	require.Empty(t, snap.Events)
	require.Empty(t, snap.Turns)
	require.Nil(t, snap.TurnStats)
	require.Nil(t, snap.Workspace)
}

func TestLoad_SessionCtxByValueIsSnapshot(t *testing.T) {
	t.Parallel()
	// Mutating the returned snapshot must not leak back into the facade.
	sr := &fakeSessionReader{sess: sampleSessionCtx()}
	f := NewFacade(sr, nil, nil, nil, quietLogger())
	snap, err := f.Load(context.Background(), "s1", ContextLoadOptions{})
	require.NoError(t, err)
	snap.Session.WorkerSessionID = "tampered"
	again, err := f.Load(context.Background(), "s1", ContextLoadOptions{})
	require.NoError(t, err)
	require.Equal(t, "wsess-1", again.Session.WorkerSessionID, "facade state isolated from caller mutation")
}
