package main

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/audit"
	"github.com/hrygo/hotplex/internal/dbutil"
	"github.com/hrygo/hotplex/internal/sqlutil"
)

// openAuditStoreAt opens (or creates) the audit schema at the given db path,
// mirroring newAuditTestStore but pinning the file so the CLI under test
// opens the SAME database the test seeded.
func openAuditStoreAt(t *testing.T, dbPath string) *auditTestStore {
	t.Helper()
	db, err := sql.Open(sqlutil.DriverName, dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	_, err = db.Exec("PRAGMA journal_mode=WAL")
	require.NoError(t, err)
	_, err = db.Exec(auditTestSchemaSQL)
	require.NoError(t, err)
	writeMu := sqlutil.NewWriteMu(sqlutil.DialectSQLite)
	store, err := audit.NewStore(db, dbutil.DialectSQLite, writeMu, slog.Default())
	require.NoError(t, err)
	return &auditTestStore{Store: store, db: db}
}

// appendAuditRow appends one linked row (real hash chain) to the store.
func appendAuditRow(t *testing.T, store audit.Store, prev string, ts int64) string {
	t.Helper()
	ctx := context.Background()
	ua := &audit.UserActivity{
		Ts:         ts,
		UserID:     "u1",
		UserIDType: audit.UserIDTypePlatform,
		Platform:   audit.PlatformWebChat,
		Action:     audit.ActionAuthLogin,
		Outcome:    audit.OutcomeSuccess,
		DetailJSON: `{}`,
		PrevHash:   prev,
	}
	h, err := audit.ComputeSelfHash(prev, ua)
	require.NoError(t, err)
	ua.SelfHash = h
	tx, err := store.BeginTx(ctx)
	require.NoError(t, err)
	require.NoError(t, tx.Append(ctx, ua))
	require.NoError(t, tx.Commit())
	return h
}

// writeBrokenAuditDB creates a sqlite DB with a 3-row chain whose middle row
// is deleted (orphaning the third row), plus a minimal config pointing at it.
func writeBrokenAuditDB(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "audit.db")
	cfgPath := filepath.Join(dir, "cfg.yaml")

	store := openAuditStoreAt(t, dbPath)
	prev := ""
	for i := range 3 {
		prev = appendAuditRow(t, store, prev, 1700000000000+int64(i))
	}
	_, err := store.db.ExecContext(context.Background(), `DELETE FROM user_activity WHERE id = 2`)
	require.NoError(t, err)
	require.NoError(t, store.Close())

	cfg := "db:\n  driver: sqlite\n  sqlite:\n    path: " + dbPath + "\n"
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfg), 0o644))
	return cfgPath
}

// TestAuditVerify_BrokenChainReturnsError pins the exit-code contract: a
// verify command that detects a broken chain must fail (non-zero exit for
// CI/cron callers), not silently succeed like the OK path.
func TestAuditVerify_BrokenChainReturnsError(t *testing.T) {
	cfgPath := writeBrokenAuditDB(t)

	cmd := newAuditVerifyCmd()
	cmd.SetArgs([]string{"--config", cfgPath})

	err := cmd.Execute()
	require.Error(t, err, "broken chain must surface as an error (non-zero exit)")
	require.Contains(t, err.Error(), "audit chain broken")
}

// TestAuditVerify_IntactChainSucceeds pins the happy path: a healthy chain
// exits cleanly with no error.
func TestAuditVerify_IntactChainSucceeds(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "audit.db")
	cfgPath := filepath.Join(dir, "cfg.yaml")

	store := openAuditStoreAt(t, dbPath)
	prev := ""
	for i := range 3 {
		prev = appendAuditRow(t, store, prev, 1700000000000+int64(i))
	}
	require.NoError(t, store.Close())

	cfg := "db:\n  driver: sqlite\n  sqlite:\n    path: " + dbPath + "\n"
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfg), 0o644))

	cmd := newAuditVerifyCmd()
	cmd.SetArgs([]string{"--config", cfgPath})

	err := cmd.Execute()
	require.NoError(t, err)
}

// TestAuditRebase_FixesBrokenChain pins the repair contract: a chain broken
// by a historical manual DELETE is repaired by re-anchoring the checkpoint
// at the first surviving row, after which verify reports a healthy chain.
func TestAuditRebase_FixesBrokenChain(t *testing.T) {
	cfgPath := writeBrokenAuditDB(t) // 3 rows, middle row deleted

	cmd := newAuditRebaseCmd()
	cmd.SetArgs([]string{"--config", cfgPath, "--next-id", "3", "--confirm"})

	err := cmd.Execute()
	require.NoError(t, err, "rebase with --confirm must succeed")

	// The rebase command runs verify internally; a separate verify pass must
	// also report the chain healthy from the new anchor.
	verifyCmd := newAuditVerifyCmd()
	verifyCmd.SetArgs([]string{"--config", cfgPath})
	err = verifyCmd.Execute()
	require.NoError(t, err, "chain must verify clean after rebase")
}

// TestAuditRebase_RequiresConfirm pins the confirmation gate: without
// --confirm the command must fail and must NOT write a checkpoint (the chain
// stays broken).
func TestAuditRebase_RequiresConfirm(t *testing.T) {
	cfgPath := writeBrokenAuditDB(t)

	cmd := newAuditRebaseCmd()
	cmd.SetArgs([]string{"--config", cfgPath, "--next-id", "3"})

	err := cmd.Execute()
	require.Error(t, err, "rebase without --confirm must fail")
	require.Contains(t, err.Error(), "--confirm")

	verifyCmd := newAuditVerifyCmd()
	verifyCmd.SetArgs([]string{"--config", cfgPath})
	err = verifyCmd.Execute()
	require.Error(t, err, "no checkpoint may be written without confirmation")
}

// TestAuditRebase_TargetMissing pins the target validation contract: an id
// with no surviving row must fail loudly instead of anchoring elsewhere.
func TestAuditRebase_TargetMissing(t *testing.T) {
	cfgPath := writeBrokenAuditDB(t)

	cmd := newAuditRebaseCmd()
	cmd.SetArgs([]string{"--config", cfgPath, "--next-id", "99", "--confirm"})

	err := cmd.Execute()
	require.Error(t, err, "missing target row must fail")
	require.Contains(t, err.Error(), "rebase target row not found")
}

// TestAuditRebase_RequiresNextID pins the required-flag contract.
func TestAuditRebase_RequiresNextID(t *testing.T) {
	cfgPath := writeBrokenAuditDB(t)

	cmd := newAuditRebaseCmd()
	cmd.SetArgs([]string{"--config", cfgPath, "--confirm"})

	err := cmd.Execute()
	require.Error(t, err, "missing --next-id must fail")
}
