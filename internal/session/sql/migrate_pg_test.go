//go:build pg

package session_test

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

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
