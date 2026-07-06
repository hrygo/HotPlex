package audit

import (
	"context"
	"database/sql"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/dbutil"
	"github.com/hrygo/hotplex/internal/sqlutil"
)

// testSchemaSQL creates the audit tables for testing (mirrors migration 023).
const testSchemaSQL = `
CREATE TABLE IF NOT EXISTS user_activity (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    ts            INTEGER NOT NULL,
    user_id       TEXT    NOT NULL,
    user_id_type  TEXT    NOT NULL,
    platform      TEXT    NOT NULL,
    session_id    TEXT,
    action        TEXT    NOT NULL,
    resource_type TEXT,
    resource_id   TEXT,
    outcome       TEXT    NOT NULL,
    detail_json   TEXT    NOT NULL,
    event_ref     TEXT,
    ip            TEXT,
    user_agent    TEXT,
    prev_hash     TEXT    NOT NULL,
    self_hash     TEXT    NOT NULL
);
CREATE TABLE IF NOT EXISTS audit_chain_checkpoints (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    pruned_at       INTEGER NOT NULL,
    last_self_hash  TEXT    NOT NULL,
    next_id         INTEGER NOT NULL
);
`

// newTestSQLiteStore creates a fresh SQLite-backed Store in t.TempDir().
func newTestSQLiteStore(t *testing.T) Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "audit_test.db")
	db, err := sql.Open(sqlutil.DriverName, dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(1)
	_, err = db.Exec("PRAGMA journal_mode=WAL")
	require.NoError(t, err)

	_, err = db.Exec(testSchemaSQL)
	require.NoError(t, err)

	writeMu := sqlutil.NewWriteMu(sqlutil.DialectSQLite)
	log := slog.Default()
	store, err := NewStore(db, dbutil.DialectSQLite, writeMu, log)
	require.NoError(t, err)
	return store
}

func TestNewStore_SQLite(t *testing.T) {
	t.Parallel()
	store := newTestSQLiteStore(t)
	require.Equal(t, dbutil.DialectSQLite, store.Dialect())
}

func TestNewStore_UnknownDialect(t *testing.T) {
	t.Parallel()
	db, err := sql.Open(sqlutil.DriverName, ":memory:")
	require.NoError(t, err)
	defer db.Close()
	_, err = NewStore(db, dbutil.Dialect("unknown"), nil, slog.Default())
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown dialect")
}

func TestSQLiteStore_BeginTx_Append_Commit(t *testing.T) {
	t.Parallel()
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	ua := &UserActivity{
		Ts: 1700000000000, UserID: "u1", UserIDType: UserIDTypePlatform,
		Platform: PlatformFeishu, Action: ActionAuthLogin, Outcome: OutcomeSuccess,
		DetailJSON: `{"k":"v"}`, PrevHash: "", SelfHash: "abc123",
	}

	tx, err := store.BeginTx(ctx)
	require.NoError(t, err)

	err = tx.Append(ctx, ua)
	require.NoError(t, err)

	err = tx.Commit()
	require.NoError(t, err)

	// Verify row persisted
	rows, err := store.Query(ctx, Query{UserID: "u1"})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "u1", rows[0].UserID)
	require.Equal(t, ActionAuthLogin, rows[0].Action)
	require.Equal(t, "abc123", rows[0].SelfHash)
}

func TestSQLiteStore_AppendBatch(t *testing.T) {
	t.Parallel()
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	uas := make([]*UserActivity, 3)
	for i := 0; i < 3; i++ {
		uas[i] = &UserActivity{
			Ts: int64(1700000000000 + i*1000), UserID: "u1", UserIDType: UserIDTypePlatform,
			Platform: PlatformWebChat, Action: ActionMessageInbound, Outcome: OutcomeSuccess,
			DetailJSON: `{}`, PrevHash: "", SelfHash: "hash" + string(rune('a'+i)),
		}
	}

	tx, err := store.BeginTx(ctx)
	require.NoError(t, err)
	err = tx.AppendBatch(ctx, uas)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())

	rows, err := store.Query(ctx, Query{UserID: "u1"})
	require.NoError(t, err)
	require.Len(t, rows, 3)
}

func TestSQLiteStore_Rollback(t *testing.T) {
	t.Parallel()
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	tx, err := store.BeginTx(ctx)
	require.NoError(t, err)

	ua := &UserActivity{
		Ts: 1700000000000, UserID: "u1", UserIDType: UserIDTypePlatform,
		Platform: PlatformFeishu, Action: ActionAuthLogin, Outcome: OutcomeSuccess,
		DetailJSON: `{}`, PrevHash: "", SelfHash: "rollback_hash",
	}
	require.NoError(t, tx.Append(ctx, ua))
	require.NoError(t, tx.Rollback())

	// Row should NOT exist
	rows, err := store.Query(ctx, Query{UserID: "u1"})
	require.NoError(t, err)
	require.Empty(t, rows)
}

func TestSQLiteStore_Query_Filters(t *testing.T) {
	t.Parallel()
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	// Insert 5 rows with varying fields
	rows := []*UserActivity{
		{Ts: 1000, UserID: "u1", UserIDType: UserIDTypePlatform, Platform: PlatformFeishu, Action: ActionAuthLogin, Outcome: OutcomeSuccess, DetailJSON: `{}`, PrevHash: "", SelfHash: "h1"},
		{Ts: 2000, UserID: "u1", UserIDType: UserIDTypePlatform, Platform: PlatformFeishu, Action: ActionSessionCreate, Outcome: OutcomeSuccess, DetailJSON: `{}`, PrevHash: "h1", SelfHash: "h2"},
		{Ts: 3000, UserID: "u2", UserIDType: UserIDTypeRegistered, Platform: PlatformWebChat, Action: ActionAuthLogin, Outcome: OutcomeFailure, DetailJSON: `{}`, PrevHash: "h2", SelfHash: "h3"},
		{Ts: 4000, UserID: "u1", UserIDType: UserIDTypePlatform, Platform: PlatformFeishu, Action: ActionAuthLogin, Outcome: OutcomeDenied, DetailJSON: `{}`, PrevHash: "h3", SelfHash: "h4"},
		{Ts: 5000, UserID: "u3", UserIDType: UserIDTypeAnonymous, Platform: PlatformAPI, Action: ActionAuthDenied, Outcome: OutcomeDenied, DetailJSON: `{}`, IP: "1.2.3.4", PrevHash: "h4", SelfHash: "h5"},
	}

	tx, err := store.BeginTx(ctx)
	require.NoError(t, err)
	require.NoError(t, tx.AppendBatch(ctx, rows))
	require.NoError(t, tx.Commit())

	// Filter by UserID
	res, err := store.Query(ctx, Query{UserID: "u1"})
	require.NoError(t, err)
	require.Len(t, res, 3)

	// Filter by Action
	res, err = store.Query(ctx, Query{Action: ActionAuthLogin})
	require.NoError(t, err)
	require.Len(t, res, 3)

	// Filter by Outcome
	res, err = store.Query(ctx, Query{Outcome: OutcomeDenied})
	require.NoError(t, err)
	require.Len(t, res, 2)

	// Filter by time range
	res, err = store.Query(ctx, Query{From: time.UnixMilli(2000), To: time.UnixMilli(4000)})
	require.NoError(t, err)
	require.Len(t, res, 3)

	// Limit + Offset
	res, err = store.Query(ctx, Query{Limit: 2, Offset: 1})
	require.NoError(t, err)
	require.Len(t, res, 2)

	// Default limit (should return all 5 since < 100)
	res, err = store.Query(ctx, Query{})
	require.NoError(t, err)
	require.Len(t, res, 5)
}

func TestSQLiteStore_DeleteBefore(t *testing.T) {
	t.Parallel()
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	rows := []*UserActivity{
		{Ts: 1000, UserID: "u1", UserIDType: UserIDTypePlatform, Platform: PlatformFeishu, Action: ActionAuthLogin, Outcome: OutcomeSuccess, DetailJSON: `{}`, PrevHash: "", SelfHash: "h1"},
		{Ts: 2000, UserID: "u1", UserIDType: UserIDTypePlatform, Platform: PlatformFeishu, Action: ActionAuthLogin, Outcome: OutcomeSuccess, DetailJSON: `{}`, PrevHash: "h1", SelfHash: "h2"},
		{Ts: 3000, UserID: "u1", UserIDType: UserIDTypePlatform, Platform: PlatformFeishu, Action: ActionAuthLogin, Outcome: OutcomeSuccess, DetailJSON: `{}`, PrevHash: "h2", SelfHash: "h3"},
	}
	tx, err := store.BeginTx(ctx)
	require.NoError(t, err)
	require.NoError(t, tx.AppendBatch(ctx, rows))
	require.NoError(t, tx.Commit())

	deleted, err := store.DeleteBefore(ctx, time.UnixMilli(2500))
	require.NoError(t, err)
	require.Equal(t, int64(2), deleted)

	remaining, err := store.Query(ctx, Query{})
	require.NoError(t, err)
	require.Len(t, remaining, 1)
	require.Equal(t, int64(3000), remaining[0].Ts)
}

func TestSQLiteStore_Checkpoint(t *testing.T) {
	t.Parallel()
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	// No checkpoint initially
	cp, err := store.LatestCheckpoint(ctx)
	require.NoError(t, err)
	require.Nil(t, cp)

	// Save checkpoint
	c := Checkpoint{
		PrunedAt:     time.UnixMilli(1700000000000),
		LastSelfHash: "deadbeef",
		NextID:       42,
	}
	err = store.SaveCheckpoint(ctx, c)
	require.NoError(t, err)

	// Retrieve it
	cp, err = store.LatestCheckpoint(ctx)
	require.NoError(t, err)
	require.NotNil(t, cp)
	require.Equal(t, "deadbeef", cp.LastSelfHash)
	require.Equal(t, int64(42), cp.NextID)
	require.Equal(t, time.UnixMilli(1700000000000), cp.PrunedAt)

	// Save another — LatestCheckpoint returns the newest
	c2 := Checkpoint{
		PrunedAt:     time.UnixMilli(1700000001000),
		LastSelfHash: "cafebabe",
		NextID:       100,
	}
	require.NoError(t, store.SaveCheckpoint(ctx, c2))
	cp, err = store.LatestCheckpoint(ctx)
	require.NoError(t, err)
	require.Equal(t, "cafebabe", cp.LastSelfHash)
	require.Equal(t, int64(100), cp.NextID)
}

func TestSQLiteStore_TxInSaveCheckpoint(t *testing.T) {
	t.Parallel()
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	// SaveCheckpoint via transaction
	tx, err := store.BeginTx(ctx)
	require.NoError(t, err)
	c := Checkpoint{PrunedAt: time.UnixMilli(5000), LastSelfHash: "txhash", NextID: 10}
	require.NoError(t, tx.SaveCheckpoint(ctx, c))
	require.NoError(t, tx.Commit())

	cp, err := store.LatestCheckpoint(ctx)
	require.NoError(t, err)
	require.NotNil(t, cp)
	require.Equal(t, "txhash", cp.LastSelfHash)
}

func TestSQLiteStore_Query_NullableFields(t *testing.T) {
	t.Parallel()
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	// Row with all optional fields empty
	ua := &UserActivity{
		Ts: 1000, UserID: "u1", UserIDType: UserIDTypePlatform,
		Platform: PlatformFeishu, Action: ActionAuthLogin, Outcome: OutcomeSuccess,
		DetailJSON: `{}`, PrevHash: "", SelfHash: "h1",
	}
	tx, err := store.BeginTx(ctx)
	require.NoError(t, err)
	require.NoError(t, tx.Append(ctx, ua))
	require.NoError(t, tx.Commit())

	rows, err := store.Query(ctx, Query{})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Empty(t, rows[0].SessionID)
	require.Empty(t, rows[0].ResourceType)
	require.Empty(t, rows[0].ResourceID)
	require.Empty(t, rows[0].EventRef)
	require.Empty(t, rows[0].IP)
	require.Empty(t, rows[0].UserAgent)
}

func TestSQLiteStore_DoubleCommitIsNoop(t *testing.T) {
	t.Parallel()
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	tx, err := store.BeginTx(ctx)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
	// Second commit should be a no-op, not error
	require.NoError(t, tx.Commit())
}

func TestSQLiteStore_DoubleRollbackIsNoop(t *testing.T) {
	t.Parallel()
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	tx, err := store.BeginTx(ctx)
	require.NoError(t, err)
	require.NoError(t, tx.Rollback())
	require.NoError(t, tx.Rollback())
}

func TestSQLiteStore_AppendAfterCommitErrors(t *testing.T) {
	t.Parallel()
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	tx, err := store.BeginTx(ctx)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())

	ua := &UserActivity{
		Ts: 1000, UserID: "u1", UserIDType: UserIDTypePlatform,
		Platform: PlatformFeishu, Action: ActionAuthLogin, Outcome: OutcomeSuccess,
		DetailJSON: `{}`, PrevHash: "", SelfHash: "h1",
	}
	err = tx.Append(ctx, ua)
	require.Error(t, err)
}
