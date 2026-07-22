package runtimecontext

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/agentspec"
	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/eventstore"
	"github.com/hrygo/hotplex/internal/security"
	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/internal/sqlutil"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/events"
)

// dualStore builds a session store (whose goose migrations create the sessions,
// workspaces, events, and turns tables) and an eventstore sharing the same
// SQLite file, so a single helper exercises all four runtime-context sources
// against real stores. Mirrors session.helperDB for the config shape.
func dualStore(t *testing.T) (*session.SQLiteStore, *eventstore.SQLiteStore) {
	t.Helper()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "rc.db")
	cfg := config.Default()
	cfg.DB.Path = dbPath
	cfg.DB.SQLite.Path = dbPath
	cfg.DB.WALMode = true

	sessStore, err := session.NewSQLiteStore(ctx, cfg, sqlutil.NewWriteMu(sqlutil.DialectSQLite))
	require.NoError(t, err)
	t.Cleanup(func() { _ = sessStore.Close() })

	// Open the eventstore on the SAME file the session store just migrated —
	// the events (002) and turns (009) tables already exist, so the eventstore
	// needs no schema setup of its own (its nil writeMu is nil-safe).
	evtStore, err := eventstore.NewIndependentStore(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = evtStore.Close() })

	return sessStore, evtStore
}

// seedWorkspace inserts a workspace row (and its owner user, required by the
// workspaces→users FK) and returns the workspace fully populated.
//
// The workspaces.create INSERT (sql/queries/workspaces.create.sql) hard-codes
// agent_config_overrides/worker_preference to NULL — only the base columns plus
// permission_mode are set on insert. Spec ②③ fields are filled later via
// UpdateWorkspace, exactly as production does (create a workspace, then
// configure its agent config). CreateWorkspace does not write back UpdatedAt,
// so we re-read the row to obtain the CAS value the update's
// optimistic-concurrency guard requires (workspaces.update.sql).
func seedWorkspace(t *testing.T, store *session.SQLiteStore, id, owner string) *session.Workspace {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, store.CreateUser(ctx, &security.User{
		ID: owner, Username: owner, Role: "user", Status: "active",
	}, time.Now().Unix()))

	created := &session.Workspace{
		ID:             id,
		OwnerUserID:    owner,
		Name:           "proj-" + id,
		WorkDir:        "/repo/" + id,
		PermissionMode: "workspace",
		Status:         "active",
	}
	require.NoError(t, store.CreateWorkspace(ctx, created, time.Now().Unix()))

	fetched, err := store.GetWorkspaceByID(ctx, id)
	require.NoError(t, err)
	fetched.WorkerPreference = "codex_cli"
	fetched.AgentConfigOverrides = `{"model":"gpt-5"}`
	require.NoError(t, store.UpdateWorkspace(ctx, fetched, time.Now().Unix()))
	return fetched
}

// seedSession inserts a session bound to a workspace, carrying an effective spec
// snapshot and identity-deriving fields.
func seedSession(t *testing.T, store *session.SQLiteStore, id, wsID string) {
	t.Helper()
	now := time.Now()
	snap := agentspec.SnapshotFromSpec(agentspec.AgentSpec{
		Worker: agentspec.WorkerSpec{Type: "claude_code", Model: "claude-sonnet-5"},
		Policy: agentspec.PolicySpec{PermissionMode: "workspace", AllowedTools: []string{"git", "grep"}},
	})
	require.NoError(t, store.Upsert(context.Background(), &session.SessionInfo{
		ID:              id,
		UserID:          "u1",
		WorkerType:      worker.TypeClaudeCode,
		State:           events.StateIdle,
		Platform:        "webchat",
		BotName:         "coding-agent",
		WorkspaceID:     wsID,
		WorkDir:         "/repo/" + wsID,
		WorkerSessionID: "wsess-" + id,
		CreatedAt:       now,
		UpdatedAt:       now,
		SpecSnapshot:    &snap,
	}))
}

func seedEvents(t *testing.T, evtStore *eventstore.SQLiteStore, sessionID string, seqs ...int64) {
	t.Helper()
	ctx := context.Background()
	for i, seq := range seqs {
		require.NoError(t, evtStore.Append(ctx, &eventstore.StoredEvent{
			SessionID: sessionID,
			Seq:       seq,
			Type:      "message",
			Data:      json.RawMessage(`{}`),
			Direction: "outbound",
			Source:    eventstore.SourceNormal,
			CreatedAt: time.Now().Add(time.Duration(i) * time.Second).UnixMilli(),
		}))
	}
}

func seedTurns(t *testing.T, evtStore *eventstore.SQLiteStore, sessionID string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tx, err := evtStore.BeginTx(ctx)
	require.NoError(t, err)
	success := true
	base := time.Now().UnixMilli()
	turns := []*eventstore.TurnWriteRequest{
		{SessionID: sessionID, Generation: 1, TurnNum: 1, Seq: 1, Role: eventstore.RoleUser, Content: "hi", CreatedAt: base},
		{SessionID: sessionID, Generation: 1, TurnNum: 2, Seq: 2, Role: eventstore.RoleAssistant, Content: "hello", Model: "claude-sonnet-5", Success: &success, Source: eventstore.SourceNormal, TokensInput: 120, TokensOut: 40, DurationMs: 1500, CostUSD: 0.01, CreatedAt: base + 1000},
	}
	for _, tr := range turns {
		require.NoError(t, tx.AppendTurn(ctx, tr))
	}
	require.NoError(t, tx.Commit())
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// --- per-source projection tests ---

func TestAdapters_SessionProjection(t *testing.T) {
	t.Parallel()
	sessStore, _ := dualStore(t)
	seedSession(t, sessStore, "s1", "ws1")

	sr := NewSessionReader(sessStore)
	sess, err := sr.Session(context.Background(), "s1")
	require.NoError(t, err)
	require.NotNil(t, sess)
	require.Equal(t, "wsess-s1", sess.WorkerSessionID)
	require.Equal(t, "ws1", sess.WorkspaceID)
	require.Equal(t, "claude_code", sess.WorkerType)
	require.Equal(t, "idle", sess.State)
	require.NotEmpty(t, sess.Identity.AgentID, "identity derived from fields")
	require.Equal(t, "u1", sess.Identity.UserID)
	require.NotNil(t, sess.SpecSnapshot, "spec snapshot round-tripped")
	require.Equal(t, []string{"git", "grep"}, sess.SpecSnapshot.AllowedTools)

	// Unknown session → the underlying store returns ErrSessionNotFound; the
	// adapter forwards it.
	_, err = sr.Session(context.Background(), "nope")
	require.Error(t, err)
}

func TestAdapters_WorkspaceProjection(t *testing.T) {
	t.Parallel()
	sessStore, _ := dualStore(t)
	seedWorkspace(t, sessStore, "ws2", "u1")

	wr := NewWorkspaceReader(sessStore)
	ws, err := wr.Workspace(context.Background(), "ws2")
	require.NoError(t, err)
	require.NotNil(t, ws)
	require.Equal(t, "ws2", ws.ID)
	require.Equal(t, "/repo/ws2", ws.WorkDir)
	require.Equal(t, "workspace", ws.PermissionMode)
	require.Equal(t, "codex_cli", ws.WorkerPreference)
	require.Equal(t, `{"model":"gpt-5"}`, ws.AgentConfigOverrides)

	wsMissing, err := wr.Workspace(context.Background(), "ghost")
	require.Error(t, err, "missing workspace errors (best-effort at facade layer)")
	require.Nil(t, wsMissing)
}

func TestAdapters_EventsProjection_ASCOrder(t *testing.T) {
	t.Parallel()
	_, evtStore := dualStore(t)
	seedEvents(t, evtStore, "s1", 10, 20, 30)

	er := NewEventReader(evtStore)
	got, err := er.RecentEvents(context.Background(), "s1", 50)
	require.NoError(t, err)
	require.Len(t, got, 3)
	// CursorLatest is reversed to ASC in Go → oldest (seq 10) first.
	require.Equal(t, int64(10), got[0].Seq)
	require.Equal(t, int64(30), got[2].Seq)
	require.Equal(t, "message", got[0].Type)
	require.Equal(t, "outbound", got[0].Direction)

	// Empty session → nil, no error.
	empty, err := er.RecentEvents(context.Background(), "none", 50)
	require.NoError(t, err)
	require.Empty(t, empty)

	// Non-positive limit → nil without querying.
	skip, err := er.RecentEvents(context.Background(), "s1", 0)
	require.NoError(t, err)
	require.Nil(t, skip)
}

func TestAdapters_TurnsProjection(t *testing.T) {
	t.Parallel()
	_, evtStore := dualStore(t)
	seedTurns(t, evtStore, "s1")

	tr := NewTurnReader(evtStore)
	turns, err := tr.RecentTurns(context.Background(), "s1", 20)
	require.NoError(t, err)
	require.Len(t, turns, 2)
	// ASC (reversed in Go) → user turn first, assistant second.
	require.Equal(t, 1, turns[0].TurnNum)
	require.Equal(t, "user", turns[0].Role)
	require.Equal(t, 2, turns[1].TurnNum)
	require.Equal(t, "assistant", turns[1].Role)
	require.NotNil(t, turns[1].Success)
	require.True(t, *turns[1].Success)
	require.Equal(t, int64(120), turns[1].TokensIn)

	stats, err := tr.TurnStats(context.Background(), "s1")
	require.NoError(t, err)
	require.NotNil(t, stats)
	require.Equal(t, int64(1), stats.Generation)
	require.Greater(t, stats.TotalTurns, 0)
}

// --- end-to-end: Facade.Load over real stores, all four sources ---

func TestFacade_LoadOverRealStores(t *testing.T) {
	t.Parallel()
	sessStore, evtStore := dualStore(t)
	seedWorkspace(t, sessStore, "ws9", "u1")
	seedSession(t, sessStore, "s9", "ws9")
	seedEvents(t, evtStore, "s9", 1, 2, 3)
	seedTurns(t, evtStore, "s9")

	facade := NewFacade(
		NewSessionReader(sessStore),
		NewEventReader(evtStore),
		NewTurnReader(evtStore),
		NewWorkspaceReader(sessStore),
		discardLogger(),
	)

	snap, err := facade.Load(context.Background(), "s9", ContextLoadOptions{})
	require.NoError(t, err)
	require.NotNil(t, snap)

	// All four sources populated from real stores.
	require.Equal(t, "wsess-s9", snap.Session.WorkerSessionID)
	require.NotEmpty(t, snap.Session.Identity.AgentID)
	require.NotNil(t, snap.Session.SpecSnapshot)
	require.NotNil(t, snap.Workspace)
	require.Equal(t, "ws9", snap.Workspace.ID)
	require.Len(t, snap.Events, 3)
	require.Equal(t, int64(1), snap.Events[0].Seq)
	require.Len(t, snap.Turns, 2)
	require.NotNil(t, snap.TurnStats)
	require.NotZero(t, snap.LoadedAt)
}

// TestFacade_LoadPlatformSessionNoWorkspaceOverRealStore: a platform session
// (no WorkspaceID) loads session/events/turns but skips the workspace lookup
// without error — proving resume/fork/history behavior is unchanged for
// non-workchat sessions.
func TestFacade_LoadPlatformSessionNoWorkspaceOverRealStore(t *testing.T) {
	t.Parallel()
	sessStore, evtStore := dualStore(t)
	// Platform session: empty WorkspaceID.
	now := time.Now()
	require.NoError(t, sessStore.Upsert(context.Background(), &session.SessionInfo{
		ID: "plat1", UserID: "u1", WorkerType: worker.TypeClaudeCode,
		State: events.StateRunning, Platform: "slack", CreatedAt: now, UpdatedAt: now,
	}))
	seedEvents(t, evtStore, "plat1", 5)

	facade := NewFacade(
		NewSessionReader(sessStore),
		NewEventReader(evtStore),
		NewTurnReader(evtStore),
		NewWorkspaceReader(sessStore),
		discardLogger(),
	)
	snap, err := facade.Load(context.Background(), "plat1", ContextLoadOptions{})
	require.NoError(t, err)
	require.Nil(t, snap.Workspace, "platform session has no workspace")
	require.Equal(t, "slack", snap.Session.Platform)
	require.Len(t, snap.Events, 1)
}
