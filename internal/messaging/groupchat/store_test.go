package groupchat

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/sqlutil"
)

func init() {
	_ = sqlutil.DriverName
}

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open(sqlutil.DriverName, dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	// Create tables from migration schema.
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS group_sessions (
			id TEXT PRIMARY KEY,
			topic TEXT NOT NULL,
			platform TEXT NOT NULL,
			channel_id TEXT NOT NULL DEFAULT '',
			thread_ts TEXT NOT NULL DEFAULT '',
			owner_id TEXT NOT NULL,
			initiator TEXT NOT NULL DEFAULT '',
			bot_ids TEXT NOT NULL DEFAULT '[]',
			state TEXT NOT NULL DEFAULT 'active',
			max_turns INTEGER NOT NULL DEFAULT 15,
			turn_count INTEGER NOT NULL DEFAULT 0,
			cost_limit_usd REAL NOT NULL DEFAULT 0,
			cost_accumulated REAL NOT NULL DEFAULT 0,
			turn_timeout_sec INTEGER NOT NULL DEFAULT 120,
			cooldown_ms INTEGER NOT NULL DEFAULT 5000,
			end_reason TEXT NOT NULL DEFAULT '',
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			ended_at DATETIME
		);
		CREATE TABLE IF NOT EXISTS group_turns (
			id TEXT PRIMARY KEY,
			group_session_id TEXT NOT NULL REFERENCES group_sessions(id),
			bot_id TEXT NOT NULL,
			bot_name TEXT NOT NULL,
			turn_num INTEGER NOT NULL,
			content TEXT NOT NULL DEFAULT '',
			skipped INTEGER NOT NULL DEFAULT 0,
			sanitized INTEGER NOT NULL DEFAULT 1,
			sanitize_reason TEXT NOT NULL DEFAULT '',
			timeout_count INTEGER NOT NULL DEFAULT 0,
			cost_usd REAL NOT NULL DEFAULT 0,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS group_chat_audit (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			event_type TEXT NOT NULL,
			session_id TEXT NOT NULL,
			bot_id TEXT NOT NULL DEFAULT '',
			initiator TEXT NOT NULL DEFAULT '',
			turn_num INTEGER NOT NULL DEFAULT 0,
			detail TEXT NOT NULL DEFAULT '',
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
	`)
	require.NoError(t, err)

	return NewSQLiteStore(db, slog.New(slog.NewTextHandler(io.Discard, nil)), sqlutil.NewWriteMu(sqlutil.DialectSQLite))
}

func makeTestGroupSession(id string) *GroupSession {
	return &GroupSession{
		ID:              id,
		Topic:           "test topic",
		Platform:        "feishu",
		ChannelID:       "ch_123",
		ThreadTS:        "ts_456",
		OwnerID:         "owner_1",
		Initiator:       "owner_1",
		BotIDs:          []string{"bot_a", "bot_b"},
		BotNames:        map[string]string{"bot_a": "Alice", "bot_b": "Bob"},
		MaxTurns:        10,
		TurnCount:       0,
		CostLimitUSD:    1.0,
		CostAccumulated: 0,
		TurnTimeoutSec:  120,
		CooldownMS:      5000,
		State:           GroupStateActive,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
}

func TestSQLiteStore_CreateAndGetGroup(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	gs := makeTestGroupSession("gs_1")
	require.NoError(t, s.CreateGroup(ctx, gs))

	got, err := s.GetGroup(ctx, "gs_1")
	require.NoError(t, err)
	require.Equal(t, gs.ID, got.ID)
	require.Equal(t, gs.Topic, got.Topic)
	require.Equal(t, gs.Platform, got.Platform)
	require.Equal(t, gs.ChannelID, got.ChannelID)
	require.Equal(t, gs.OwnerID, got.OwnerID)
	require.Equal(t, []string{"bot_a", "bot_b"}, got.BotIDs)
	require.Equal(t, GroupStateActive, got.State)
	require.Equal(t, 10, got.MaxTurns)
	require.Equal(t, 1.0, got.CostLimitUSD)
}

func TestSQLiteStore_GetGroup_NotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.GetGroup(ctx, "nonexistent")
	require.ErrorIs(t, err, ErrGroupNotFound)
}

func TestSQLiteStore_UpdateGroupState(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	gs := makeTestGroupSession("gs_2")
	require.NoError(t, s.CreateGroup(ctx, gs))

	require.NoError(t, s.UpdateGroupState(ctx, "gs_2", GroupStateCompleted, EndMaxTurns))

	got, err := s.GetGroup(ctx, "gs_2")
	require.NoError(t, err)
	require.Equal(t, GroupStateCompleted, got.State)
	require.Equal(t, EndMaxTurns, got.EndReason)
	require.NotNil(t, got.EndedAt)
}

func TestSQLiteStore_UpdateGroupState_ActiveNoEndedAt(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	gs := makeTestGroupSession("gs_2a")
	require.NoError(t, s.CreateGroup(ctx, gs))

	// Updating to active should not set ended_at.
	require.NoError(t, s.UpdateGroupState(ctx, "gs_2a", GroupStateActive, ""))
	got, err := s.GetGroup(ctx, "gs_2a")
	require.NoError(t, err)
	require.Nil(t, got.EndedAt)
}

func TestSQLiteStore_UpdateGroupCost(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	gs := makeTestGroupSession("gs_3")
	require.NoError(t, s.CreateGroup(ctx, gs))

	require.NoError(t, s.UpdateGroupCost(ctx, "gs_3", 5, 0.75))

	got, err := s.GetGroup(ctx, "gs_3")
	require.NoError(t, err)
	require.Equal(t, 5, got.TurnCount)
	require.InDelta(t, 0.75, got.CostAccumulated, 0.001)
}

func TestSQLiteStore_CountActive(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	count, err := s.CountActive(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, count)

	for i := range 3 {
		gs := makeTestGroupSession(fmt.Sprintf("gs_count_%d", i))
		require.NoError(t, s.CreateGroup(ctx, gs))
	}

	count, err = s.CountActive(ctx)
	require.NoError(t, err)
	require.Equal(t, 3, count)

	// End one session.
	require.NoError(t, s.UpdateGroupState(ctx, "gs_count_1", GroupStateCompleted, EndMaxTurns))

	count, err = s.CountActive(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, count)
}

func TestSQLiteStore_CountActiveByOwner(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	gs1 := makeTestGroupSession("gs_o1")
	gs1.OwnerID = "user_a"
	require.NoError(t, s.CreateGroup(ctx, gs1))

	gs2 := makeTestGroupSession("gs_o2")
	gs2.OwnerID = "user_b"
	require.NoError(t, s.CreateGroup(ctx, gs2))

	count, err := s.CountActiveByOwner(ctx, "user_a")
	require.NoError(t, err)
	require.Equal(t, 1, count)

	count, err = s.CountActiveByOwner(ctx, "user_b")
	require.NoError(t, err)
	require.Equal(t, 1, count)

	count, err = s.CountActiveByOwner(ctx, "user_c")
	require.NoError(t, err)
	require.Equal(t, 0, count)
}

func TestSQLiteStore_ListActive(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	active, err := s.ListActive(ctx)
	require.NoError(t, err)
	require.Empty(t, active)

	gs1 := makeTestGroupSession("gs_la1")
	require.NoError(t, s.CreateGroup(ctx, gs1))

	gs2 := makeTestGroupSession("gs_la2")
	require.NoError(t, s.CreateGroup(ctx, gs2))

	require.NoError(t, s.UpdateGroupState(ctx, "gs_la2", GroupStateCompleted, EndMaxTurns))

	active, err = s.ListActive(ctx)
	require.NoError(t, err)
	require.Len(t, active, 1)
	require.Equal(t, "gs_la1", active[0].ID)
}

func TestSQLiteStore_ListActiveByOwner(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	gs1 := makeTestGroupSession("gs_lao1")
	gs1.OwnerID = "owner_x"
	require.NoError(t, s.CreateGroup(ctx, gs1))

	gs2 := makeTestGroupSession("gs_lao2")
	gs2.OwnerID = "owner_y"
	require.NoError(t, s.CreateGroup(ctx, gs2))

	groups, err := s.ListActiveByOwner(ctx, "owner_x")
	require.NoError(t, err)
	require.Len(t, groups, 1)
	require.Equal(t, "gs_lao1", groups[0].ID)
}

func TestSQLiteStore_AppendAndListTurns(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	gs := makeTestGroupSession("gs_turns")
	require.NoError(t, s.CreateGroup(ctx, gs))

	turn1 := &TurnRecord{
		ID: "t1", GroupID: "gs_turns", BotID: "bot_a", BotName: "Alice",
		TurnNum: 1, Content: "Hello", Skipped: false, CostUSD: 0.01, CreatedAt: time.Now(),
	}
	require.NoError(t, s.AppendTurn(ctx, turn1))

	turn2 := &TurnRecord{
		ID: "t2", GroupID: "gs_turns", BotID: "bot_b", BotName: "Bob",
		TurnNum: 2, Content: "SKIP", Skipped: true, CostUSD: 0, CreatedAt: time.Now(),
	}
	require.NoError(t, s.AppendTurn(ctx, turn2))

	turns, err := s.ListTurns(ctx, "gs_turns")
	require.NoError(t, err)
	require.Len(t, turns, 2)

	require.Equal(t, "t1", turns[0].ID)
	require.Equal(t, "Hello", turns[0].Content)
	require.False(t, turns[0].Skipped)

	require.Equal(t, "t2", turns[1].ID)
	require.True(t, turns[1].Skipped)
}

func TestSQLiteStore_RecordAudit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	evt := &AuditEvent{
		EventType: "discussion_start",
		SessionID: "gs_audit",
		Initiator: "owner_1",
		Detail:    "topic=test bots=[a,b]",
		CreatedAt: time.Now(),
	}
	require.NoError(t, s.RecordAudit(ctx, evt))
	// No read method for audit, so just verify no error.
}

func TestSQLiteStore_Close(t *testing.T) {
	s := newTestStore(t)
	require.NoError(t, s.Close())
}
