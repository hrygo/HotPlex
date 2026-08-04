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
CREATE TABLE IF NOT EXISTS audit_identity_links (
    id                TEXT PRIMARY KEY,
    principal_user_id TEXT NOT NULL,
    provider          TEXT NOT NULL,
    subject           TEXT NOT NULL,
    subject_type      TEXT NOT NULL DEFAULT 'platform',
    display_name      TEXT NOT NULL DEFAULT '',
    email             TEXT NOT NULL DEFAULT '',
    created_at        INTEGER NOT NULL,
    updated_at        INTEGER NOT NULL,
    UNIQUE(provider, subject)
);
CREATE INDEX idx_audit_identity_links_principal ON audit_identity_links(principal_user_id);
CREATE INDEX idx_audit_identity_links_lookup ON audit_identity_links(provider, subject);
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

// newTestSQLiteStoreWithPool is like newTestSQLiteStore but configures the
// connection pool to maxOpen (mirrors production's MaxOpenConns=3). A pool
// larger than 1 is required to reproduce SQLITE_BUSY races between two
// concurrently-open write transactions; with MaxOpenConns=1 the single
// connection serializes all access and masks the missing writeMu on the
// transaction path.
func newTestSQLiteStoreWithPool(t *testing.T, maxOpen int) Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "audit_test.db")
	db, err := sql.Open(sqlutil.DriverName, dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(maxOpen)
	_, err = db.Exec("PRAGMA journal_mode=WAL")
	require.NoError(t, err)
	// Lower busy_timeout so a lock contention surfaces as SQLITE_BUSY
	// quickly (default 5s would make the test slow and flaky). 100ms is
	// long enough to ride out normal scheduling but short enough to fail
	// fast when writeMu is absent on the Tx path.
	_, err = db.Exec("PRAGMA busy_timeout=100")
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

func TestSQLiteStore_Query_ExtendedFilters(t *testing.T) {
	t.Parallel()
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	// Seed rows that exercise the new filter dimensions. tool.call carries a
	// session_id + resource_type so it is matchable by both; auth.login has
	// no session/resource and serves as the negative-match control.
	rows := []*UserActivity{
		{Ts: 1000, UserID: "u1", UserIDType: UserIDTypePlatform, Platform: PlatformFeishu, Action: ActionToolCall, Outcome: OutcomeSuccess, SessionID: "sess-a", ResourceType: "tool", ResourceID: "call-1", DetailJSON: `{}`, PrevHash: "", SelfHash: "h1"},
		{Ts: 2000, UserID: "u1", UserIDType: UserIDTypePlatform, Platform: PlatformFeishu, Action: ActionToolCall, Outcome: OutcomeSuccess, SessionID: "sess-a", ResourceType: "tool", ResourceID: "call-2", DetailJSON: `{}`, PrevHash: "h1", SelfHash: "h2"},
		{Ts: 3000, UserID: "u2", UserIDType: UserIDTypeRegistered, Platform: PlatformWebChat, Action: ActionToolCall, Outcome: OutcomeFailure, SessionID: "sess-b", ResourceType: "tool", ResourceID: "call-3", DetailJSON: `{}`, PrevHash: "h2", SelfHash: "h3"},
		{Ts: 4000, UserID: "u2", UserIDType: UserIDTypeRegistered, Platform: PlatformWebChat, Action: ActionSessionCreate, Outcome: OutcomeSuccess, SessionID: "sess-b", ResourceType: "session", ResourceID: "sess-b", DetailJSON: `{}`, PrevHash: "h3", SelfHash: "h4"},
		{Ts: 5000, UserID: "u3", UserIDType: UserIDTypeAnonymous, Platform: PlatformAPI, Action: ActionAuthLogin, Outcome: OutcomeSuccess, DetailJSON: `{}`, PrevHash: "h4", SelfHash: "h5"},
		{Ts: 6000, UserID: "u3", UserIDType: UserIDTypeAnonymous, Platform: PlatformAPI, Action: ActionAuthTokenValidated, Outcome: OutcomeSuccess, DetailJSON: `{}`, PrevHash: "h5", SelfHash: "h6"},
	}
	tx, err := store.BeginTx(ctx)
	require.NoError(t, err)
	require.NoError(t, tx.AppendBatch(ctx, rows))
	require.NoError(t, tx.Commit())

	cases := []struct {
		name    string
		q       Query
		wantIDs []int64 // by SelfHash suffix for readability
		wantCnt int
	}{
		{name: "platform feishu", q: Query{Platform: PlatformFeishu}, wantCnt: 2},
		{name: "platform webchat", q: Query{Platform: PlatformWebChat}, wantCnt: 2},
		{name: "session_id exact", q: Query{SessionID: "sess-a"}, wantCnt: 2},
		{name: "resource_type tool", q: Query{ResourceType: "tool"}, wantCnt: 3},
		{name: "resource_type session", q: Query{ResourceType: "session"}, wantCnt: 1},
		{name: "action_prefix tool.", q: Query{ActionPrefix: "tool."}, wantCnt: 3},
		{name: "action_prefix auth.", q: Query{ActionPrefix: "auth."}, wantCnt: 2},
		// Prefix literal safety: "auth.login" must not match via wildcard "_" .
		{name: "action_prefix literal underscore", q: Query{ActionPrefix: "auth_login"}, wantCnt: 0},
		// Composed filters.
		{name: "platform+action_prefix", q: Query{Platform: PlatformWebChat, ActionPrefix: "tool."}, wantCnt: 1},
		{name: "session+resource_type", q: Query{SessionID: "sess-b", ResourceType: "tool"}, wantCnt: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := store.Query(ctx, tc.q)
			require.NoError(t, err)
			require.Len(t, res, tc.wantCnt, "query=%+v", tc.q)
		})
	}
}

func TestSQLiteStore_Stats(t *testing.T) {
	t.Parallel()
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	rows := []*UserActivity{
		{Ts: 1000, UserID: "u1", UserIDType: UserIDTypePlatform, Platform: PlatformFeishu, Action: ActionAuthLogin, Outcome: OutcomeSuccess, DetailJSON: `{}`, PrevHash: "", SelfHash: "h1"},
		{Ts: 2000, UserID: "u1", UserIDType: UserIDTypePlatform, Platform: PlatformFeishu, Action: ActionToolCall, Outcome: OutcomeSuccess, DetailJSON: `{}`, PrevHash: "h1", SelfHash: "h2"},
		{Ts: 3000, UserID: "u2", UserIDType: UserIDTypeRegistered, Platform: PlatformWebChat, Action: ActionToolCall, Outcome: OutcomeFailure, DetailJSON: `{}`, PrevHash: "h2", SelfHash: "h3"},
		{Ts: 4000, UserID: "u2", UserIDType: UserIDTypeRegistered, Platform: PlatformWebChat, Action: ActionAuthLogin, Outcome: OutcomeDenied, DetailJSON: `{}`, PrevHash: "h3", SelfHash: "h4"},
	}
	tx, err := store.BeginTx(ctx)
	require.NoError(t, err)
	require.NoError(t, tx.AppendBatch(ctx, rows))
	require.NoError(t, tx.Commit())

	// Unscoped stats: total=4, outcome {success:2, failure:1, denied:1}, platform {feishu:2, webchat:2}.
	stats, err := store.Stats(ctx, Query{})
	require.NoError(t, err)
	require.Equal(t, int64(4), stats.Total)
	require.Equal(t, int64(2), stats.ByOutcome[OutcomeSuccess])
	require.Equal(t, int64(1), stats.ByOutcome[OutcomeFailure])
	require.Equal(t, int64(1), stats.ByOutcome[OutcomeDenied])
	require.Equal(t, int64(2), stats.ByPlatform[PlatformFeishu])
	require.Equal(t, int64(2), stats.ByPlatform[PlatformWebChat])

	// Scoped stats: only tool.call → total=2, failure=1.
	stats, err = store.Stats(ctx, Query{ActionPrefix: "tool."})
	require.NoError(t, err)
	require.Equal(t, int64(2), stats.Total)
	require.Equal(t, int64(1), stats.ByOutcome[OutcomeSuccess])
	require.Equal(t, int64(1), stats.ByOutcome[OutcomeFailure])
	require.Equal(t, int64(2), stats.ByPlatform[PlatformFeishu]+stats.ByPlatform[PlatformWebChat])

	// Empty table scoped to impossible filter returns zero counts, not nil maps.
	stats, err = store.Stats(ctx, Query{Platform: "nope"})
	require.NoError(t, err)
	require.Equal(t, int64(0), stats.Total)
	require.NotNil(t, stats.ByOutcome)
	require.NotNil(t, stats.ByPlatform)
}

// TestSQLiteStore_DeleteBefore_CheckpointAnchored covers the root-cause
// fix: the legacy DeleteBefore path deleted rows with no checkpoint
// anchor, orphaning the hash chain (the broken_id=1253 era). After the
// fix it must behave like the GC prune — delete the prefix, write a
// checkpoint, and leave the chain verifiable.
func TestSQLiteStore_DeleteBefore_CheckpointAnchored(t *testing.T) {
	t.Parallel()
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	rows := []*UserActivity{
		{Ts: 1000, UserID: "u1", UserIDType: UserIDTypePlatform, Platform: PlatformFeishu, Action: ActionAuthLogin, Outcome: OutcomeSuccess, DetailJSON: `{}`, PrevHash: "", SelfHash: "h1"},
		{Ts: 2000, UserID: "u1", UserIDType: UserIDTypePlatform, Platform: PlatformFeishu, Action: ActionAuthLogin, Outcome: OutcomeSuccess, DetailJSON: `{}`, PrevHash: "h1", SelfHash: "h2"},
		{Ts: 3000, UserID: "u1", UserIDType: UserIDTypePlatform, Platform: PlatformFeishu, Action: ActionAuthLogin, Outcome: OutcomeSuccess, DetailJSON: `{}`, PrevHash: "h2"},
	}
	// The surviving row's self_hash must be the real chain hash so the
	// post-prune verification pass stays clean.
	h3, err := ComputeSelfHash("h2", rows[2])
	require.NoError(t, err)
	rows[2].SelfHash = h3
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

	// The delete must have written a checkpoint anchoring the pruned prefix
	// (next_id = first surviving id), and the chain must verify clean.
	cp, err := store.LatestCheckpoint(ctx)
	require.NoError(t, err)
	require.NotNil(t, cp, "DeleteBefore must leave a checkpoint anchor")
	require.Equal(t, int64(3), cp.NextID, "checkpoint must anchor at the first surviving row")
	require.Equal(t, "h2", cp.LastSelfHash, "checkpoint must carry the last pruned row's self_hash")

	v := NewVerifier(store, VerifierConfig{}, nil)
	result, err := v.VerifyOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(0), result.BrokenID, "chain must verify clean after checkpoint-anchored delete")
	require.Empty(t, result.Reason)
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

func TestSQLiteStore_Query_UserIDs(t *testing.T) {
	t.Parallel()
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	tx, err := store.BeginTx(ctx)
	require.NoError(t, err)
	for i, userID := range []string{"u-local", "ou_feishu", "U_slack", "other"} {
		require.NoError(t, tx.Append(ctx, &UserActivity{
			Ts:         int64(1000 + i),
			UserID:     userID,
			UserIDType: UserIDTypePlatform,
			Platform:   PlatformFeishu,
			Action:     ActionMessageInbound,
			Outcome:    OutcomeSuccess,
			DetailJSON: `{}`,
			PrevHash:   "",
			SelfHash:   "h",
		}))
	}
	require.NoError(t, tx.Commit())

	rows, err := store.Query(ctx, Query{UserIDs: []string{"u-local", "ou_feishu", "U_slack"}})
	require.NoError(t, err)
	require.Len(t, rows, 3)
	got := map[string]bool{}
	for _, row := range rows {
		got[row.UserID] = true
	}
	require.True(t, got["u-local"])
	require.True(t, got["ou_feishu"])
	require.True(t, got["U_slack"])
	require.False(t, got["other"])
}

func TestSQLiteStore_IdentityLinks_CRUD(t *testing.T) {
	t.Parallel()
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	link := IdentityLink{
		ID:              "link-1",
		PrincipalUserID: "u-local",
		Provider:        "feishu",
		Subject:         "ou_feishu",
		SubjectType:     UserIDTypePlatform,
		DisplayName:     "Feishu User",
		Email:           "user@example.com",
		CreatedAt:       1000,
		UpdatedAt:       1000,
	}
	require.NoError(t, store.UpsertIdentityLink(ctx, link))

	links, err := store.ListIdentityLinks(ctx, "u-local")
	require.NoError(t, err)
	require.Len(t, links, 1)
	require.Equal(t, "ou_feishu", links[0].Subject)

	link.ID = "link-2"
	link.PrincipalUserID = "u-other"
	link.DisplayName = "Moved"
	link.CreatedAt = 2000
	link.UpdatedAt = 2000
	require.NoError(t, store.UpsertIdentityLink(ctx, link))

	links, err = store.ListIdentityLinks(ctx, "u-local")
	require.NoError(t, err)
	require.Empty(t, links)
	links, err = store.ListIdentityLinks(ctx, "u-other")
	require.NoError(t, err)
	require.Len(t, links, 1)
	require.Equal(t, "link-2", links[0].ID)
	require.Equal(t, "Moved", links[0].DisplayName)
	require.Equal(t, int64(2000), links[0].CreatedAt)

	require.NoError(t, store.DeleteIdentityLink(ctx, links[0].ID))
	links, err = store.ListIdentityLinks(ctx, "u-other")
	require.NoError(t, err)
	require.Empty(t, links)
}
