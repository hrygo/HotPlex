//go:build pg

package session_test

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/dbutil"
	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/internal/sqlutil"
)

// openTestPGDB opens a PostgreSQL connection using HOTPLEX_TEST_PG_DSN, runs
// the full migration set, and returns the *sql.DB. The caller owns Close.
// Tests call t.Skip when HOTPLEX_TEST_PG_DSN is unset so the default suite
// (which has no PG instance) is unaffected; a developer with PG available
// runs: HOTPLEX_TEST_PG_DSN=... go test -tags pg ./internal/session/sql/...
func openTestPGDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("HOTPLEX_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("HOTPLEX_TEST_PG_DSN not set; skipping PG migration test")
	}
	db, err := sql.Open(sqlutil.DriverNamePG, dsn)
	require.NoError(t, err, "open pg")

	// Start from a clean slate so the test is deterministic regardless of
	// what previous runs (or other tests) left behind. DROP SCHEMA cascade
	// removes every table/sequence/function/trigger goose created.
	_, err = db.ExecContext(context.Background(), "DROP SCHEMA IF EXISTS public CASCADE")
	require.NoError(t, err, "drop schema")
	_, err = db.ExecContext(context.Background(), "CREATE SCHEMA public")
	require.NoError(t, err, "recreate schema")

	require.NoError(t, session.RunMigrations(context.Background(), db, dbutil.DialectPostgres),
		"PG migrations must apply cleanly")
	return db
}

// TestMigrations_PG_023UserActivity_TriggerBlocksUpdate is the PostgreSQL
// counterpart to TestMigrations_023UserActivity_AppliesAndIsImmutable. It
// guards the audit system's core tamper-evidence invariant on PG: migration
// 023 creates a BEFORE UPDATE trigger that must reject every mutation of
// user_activity (review I4 — the PR shipped with zero PG migration tests).
//
// A semantic bug here (e.g. the trigger firing AFTER instead of BEFORE, or
// the function existing but never bound to the table) would let UPDATEs
// succeed silently and defeat the entire audit chain's tamper-evidence.
func TestMigrations_PG_023UserActivity_TriggerBlocksUpdate(t *testing.T) {
	ctx := context.Background()
	db := openTestPGDB(t)
	defer func() { _ = db.Close() }()

	// Seed a row. The append path goes through audit.Store, but for a focused
	// migration test we insert directly — the trigger must fire regardless of
	// how the row got there.
	_, err := db.ExecContext(ctx,
		`INSERT INTO user_activity (ts, user_id, user_id_type, platform, action, outcome, detail_json, prev_hash, self_hash)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		1700000000000, "u1", "platform", "api", "auth.login", "success", "{}", "", "hash1",
	)
	require.NoError(t, err, "seed insert")

	// An UPDATE of ANY column must be rejected by the trigger with the
	// spec-mandated message. We use the spec phrase so a future copy-edit of
	// the message is caught here too.
	_, err = db.ExecContext(ctx, `UPDATE user_activity SET outcome = 'failure' WHERE user_id = 'u1'`)
	require.Error(t, err, "UPDATE must be blocked by the immutability trigger")
	require.True(t,
		strings.Contains(err.Error(), "audit: rows are immutable") || strings.Contains(err.Error(), "immutable"),
		"trigger error should mention immutability, got: %v", err)

	// A no-op self-assign (SET ts = ts) must ALSO be blocked — the trigger is
	// BEFORE UPDATE with no WHEN clause, so it fires unconditionally.
	_, err = db.ExecContext(ctx, `UPDATE user_activity SET ts = ts`)
	require.Error(t, err, "self-assign UPDATE must also be blocked")
}

// TestMigrations_PG_030AuditNoDelete_BlocksUnauthorizedRowDeletes is the
// PostgreSQL counterpart to TestMigrations_030AuditNoDelete_BlocksUnauthorizedRowDeletes:
// migration 030's fn_ua_no_delete trigger must reject every DELETE that is
// not covered by a checkpoint anchor, while the GC prune path (checkpoint
// written in the same transaction before the delete) must still pass.
func TestMigrations_PG_030AuditNoDelete_BlocksUnauthorizedRowDeletes(t *testing.T) {
	ctx := context.Background()
	db := openTestPGDB(t)
	defer func() { _ = db.Close() }()

	insertRow := func(id int, selfHash string) {
		t.Helper()
		_, err := db.ExecContext(ctx,
			`INSERT INTO user_activity (ts, user_id, user_id_type, platform, action, outcome, detail_json, prev_hash, self_hash)
			 VALUES ($1, 'u', 'registered', 'webchat', 'x', 'success', '{}', 'prev', $2)`,
			int64(id), selfHash)
		require.NoError(t, err, "INSERT row %d", id)
	}
	insertRow(1, "h1")
	insertRow(2, "h2")

	// Unauthorized DELETE (no checkpoint anchor) must abort with the spec message.
	_, err := db.ExecContext(ctx, `DELETE FROM user_activity WHERE id = 1`)
	require.Error(t, err, "unauthorized DELETE must be rejected by trg_ua_no_delete")
	require.True(t,
		strings.Contains(err.Error(), "audit: rows are immutable") || strings.Contains(err.Error(), "immutable"),
		"trigger error should mention immutability, got: %v", err)

	// The GC prune path (checkpoint written in the SAME transaction before
	// the DELETE) must still pass.
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx,
		`INSERT INTO audit_chain_checkpoints (pruned_at, last_self_hash, next_id) VALUES ($1, $2, $3)`,
		time.Now().UnixMilli(), "h1", int64(2))
	require.NoError(t, err, "checkpoint insert inside GC tx")
	_, err = tx.ExecContext(ctx, `DELETE FROM user_activity WHERE id <= 1`)
	require.NoError(t, err, "checkpoint-anchored DELETE (GC prune) must pass")
	require.NoError(t, tx.Commit())

	// A row beyond the checkpoint's next_id (id=2 survives, next_id=2)
	// must NOT be deletable.
	_, err = db.ExecContext(ctx, `DELETE FROM user_activity WHERE id = 2`)
	require.Error(t, err, "row past the checkpoint anchor must stay immutable")
}
