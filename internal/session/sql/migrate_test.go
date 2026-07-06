package session_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/dbutil"
	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/internal/sqlutil"
)

// openMigrationTestDB opens a fresh on-disk SQLite file (avoids modernc.org/sqlite
// quirks with shared :memory:) and returns the *sql.DB after PRAGMA init. The caller
// owns Close.
func openMigrationTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "migrate_test.db")
	db, err := sql.Open(sqlutil.DriverName, dbPath)
	require.NoError(t, err, "open sqlite")
	t.Cleanup(func() { _ = db.Close() })
	// Reuse the project PRAGMA stack so tests run against the same tuning as production.
	cfg := config.Default()
	require.NoError(t, sqlutil.InitSQLiteDB(db, &cfg.DB, sqlutil.DialectSQLite, "migrate_test"))
	return db
}

// TestMigrations_023UserActivity_AppliesAndIsImmutable covers the user-facing contract
// of issue #833 Phase 1 migration 023 (Spec §5.1 / §5.5 / §11.2):
//   - user_activity table exists with the expected columns
//   - audit_chain_checkpoints table exists
//   - the BEFORE UPDATE trigger blocks mutations with the spec-mandated message
//   - all 3 expected indexes are present
//   - the 3 dead v_turns* views (spec §11.2) are dropped
func TestMigrations_023UserActivity_AppliesAndIsImmutable(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openMigrationTestDB(t)

	// Boot goose against the full migration set. This exercises the real
	// go:embed glob (internal/session/migrate.go:17) end-to-end.
	require.NoError(t, session.RunMigrations(ctx, db, dbutil.DialectSQLite))

	// 1) Both Phase 1 tables exist.
	for _, table := range []string{"user_activity", "audit_chain_checkpoints"} {
		var n int
		err := db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table,
		).Scan(&n)
		require.NoError(t, err, "lookup table %s", table)
		require.Equal(t, 1, n, "expected table %s to exist after migration 023", table)
	}

	// 2) user_activity has every spec column.
	wantCols := []string{
		"id", "ts", "user_id", "user_id_type", "platform", "session_id",
		"action", "resource_type", "resource_id", "outcome", "detail_json",
		"event_ref", "ip", "user_agent", "prev_hash", "self_hash",
	}
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(user_activity)`)
	require.NoError(t, err)
	defer rows.Close()
	gotCols := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		require.NoError(t, rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk))
		gotCols[name] = true
	}
	require.NoError(t, rows.Err())
	for _, c := range wantCols {
		require.True(t, gotCols[c], "user_activity missing column %s", c)
	}

	// 3) INSERT works (smoke) and UPDATE is rejected by the trigger with the
	//    spec-exact message. SQLite returns SQLITE_CONSTRAINT (19) on ABORT.
	_, err = db.ExecContext(ctx, `INSERT INTO user_activity
		(ts, user_id, user_id_type, platform, action, outcome, detail_json, prev_hash, self_hash)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		1700000000000, "u_test", "registered", "webchat",
		"auth.login", "success", `{}`, "", "h0")
	require.NoError(t, err, "INSERT into user_activity")

	_, err = db.ExecContext(ctx, `UPDATE user_activity SET user_id='x' WHERE 1=1`)
	require.Error(t, err, "UPDATE must be blocked by trg_ua_no_update")
	require.Contains(t, err.Error(), "audit: rows are immutable",
		"trigger must surface the spec-exact message")

	// 4) All 3 indexes present (idx_ua_user_ts, idx_ua_ts, idx_ua_action_ts).
	for _, idx := range []string{"idx_ua_user_ts", "idx_ua_ts", "idx_ua_action_ts"} {
		var n int
		err := db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=? AND tbl_name='user_activity'`, idx,
		).Scan(&n)
		require.NoError(t, err)
		require.Equal(t, 1, n, "expected index %s on user_activity", idx)
	}

	// 5) Spec §11.2: dead v_turns* views must be gone after migration 023.
	//    Even though migration 009 already replaced them with the materialized turns
	//    table, fresh DBs and pre-009 leftovers must all be cleaned.
	for _, view := range []string{"v_turns", "v_turns_user", "v_turns_assistant"} {
		var n int
		err := db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM sqlite_master WHERE type='view' AND name=?`, view,
		).Scan(&n)
		require.NoError(t, err)
		require.Equal(t, 0, n, "dead view %s should be dropped by migration 023", view)
	}

	// 6) The audit_chain_checkpoints schema is the contract for the rebase logic
	//    in spec §5.5 — guard against accidental column drift.
	cpRows, err := db.QueryContext(ctx, `PRAGMA table_info(audit_chain_checkpoints)`)
	require.NoError(t, err)
	defer cpRows.Close()
	wantCP := map[string]bool{"id": false, "pruned_at": false, "last_self_hash": false, "next_id": false}
	for cpRows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		require.NoError(t, cpRows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk))
		if _, expected := wantCP[name]; expected {
			wantCP[name] = true
		}
	}
	require.NoError(t, cpRows.Err())
	for col, found := range wantCP {
		require.True(t, found, "audit_chain_checkpoints missing column %s", col)
	}
}

// TestMigrations_023UserActivity_TriggerBlocksEveryColumnShapeOfUpdate guards against
// the trigger being accidentally weakened to allow some UPDATE shapes. We try a
// no-op-equivalent UPDATE (SET ts=ts) and confirm it still aborts.
func TestMigrations_023UserActivity_TriggerBlocksEveryColumnShapeOfUpdate(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openMigrationTestDB(t)
	require.NoError(t, session.RunMigrations(ctx, db, dbutil.DialectSQLite))

	_, err := db.ExecContext(ctx, `INSERT INTO user_activity
		(ts, user_id, user_id_type, platform, action, outcome, detail_json, prev_hash, self_hash)
		VALUES (1, 'u', 'registered', 'webchat', 'x', 'success', '{}', '', 'h')`)
	require.NoError(t, err)

	for _, stmt := range []string{
		`UPDATE user_activity SET user_id='y'`,
		`UPDATE user_activity SET ts=ts`, // "self-assign" — should still abort
		`UPDATE user_activity SET detail_json='{"k":1}'`,
	} {
		_, err := db.ExecContext(ctx, stmt)
		require.Error(t, err, "expected trigger to abort: %s", stmt)
		require.True(t,
			strings.Contains(err.Error(), "audit: rows are immutable"),
			"unexpected error for %s: %v", stmt, err)
	}
}
