package checkers

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/cli"
	"github.com/hrygo/hotplex/internal/sqlutil"
)

// newFenceTestDB creates a SQLite file with the minimal execution_inputs
// columns the checker reads, optionally seeding fenced rows.
func newFenceTestDB(t *testing.T, fencedRows int) string {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "hotplex.db")
	db, err := sql.Open(sqlutil.DriverName, dbPath)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	_, err = db.Exec(`CREATE TABLE execution_inputs (
		execution_id TEXT PRIMARY KEY,
		fence_reason TEXT NOT NULL DEFAULT '',
		fence_version INTEGER NOT NULL DEFAULT 0,
		fence_created_at INTEGER
	)`)
	require.NoError(t, err)
	for i := range fencedRows {
		_, err = db.Exec(
			`INSERT INTO execution_inputs (execution_id, fence_reason, fence_version, fence_created_at) VALUES (?, 'GATEWAY_LEASE_EXPIRED', 1, ?)`,
			"exec-"+string(rune('a'+i)), int64(1722800000000+i*1000))
		require.NoError(t, err)
	}
	return dbPath
}

func TestFencedExecutionsChecker_NoFences(t *testing.T) {
	// Check() -> loadConfig() reads package-level configPath; cannot
	// t.Parallel (see config_test.go:13).
	withConfigPath(t, "")
	dbPath := newFenceTestDB(t, 0)
	c := fencedExecutionsChecker{defaultDBPath: dbPath}

	d := c.Check(context.Background())

	require.Equal(t, cli.StatusPass, d.Status)
	require.Contains(t, d.Message, "No fenced executions")
	require.Nil(t, d.FixFunc, "fence checker must never auto-fix")
}

func TestFencedExecutionsChecker_FencesWarn(t *testing.T) {
	withConfigPath(t, "")
	dbPath := newFenceTestDB(t, 2)
	c := fencedExecutionsChecker{defaultDBPath: dbPath}

	d := c.Check(context.Background())

	require.Equal(t, cli.StatusWarn, d.Status)
	require.Contains(t, d.Message, "2 fenced execution(s)")
	require.Contains(t, d.Detail, "earliest fence at")
	require.Contains(t, d.FixHint, "hotplex runtime fences")
	require.Nil(t, d.FixFunc, "fence decisions are operator actions, never auto-fixed")
}

func TestFencedExecutionsChecker_MissingDBPass(t *testing.T) {
	withConfigPath(t, "")
	c := fencedExecutionsChecker{defaultDBPath: filepath.Join(t.TempDir(), "absent.db")}

	d := c.Check(context.Background())

	require.Equal(t, cli.StatusPass, d.Status)
	require.Contains(t, d.Message, "No execution database yet")
}

func TestFencedExecutionsChecker_NoTablesPass(t *testing.T) {
	withConfigPath(t, "")
	dbPath := filepath.Join(t.TempDir(), "empty.db")
	db, err := sql.Open(sqlutil.DriverName, dbPath)
	require.NoError(t, err)
	_, _ = db.Exec("CREATE TABLE unrelated (id INTEGER)")
	require.NoError(t, db.Close())

	c := fencedExecutionsChecker{defaultDBPath: dbPath}
	d := c.Check(context.Background())

	require.Equal(t, cli.StatusPass, d.Status)
	require.Contains(t, d.Message, "not initialized")
}

func TestCountFencedExecutions_ReadOnly(t *testing.T) {
	t.Parallel()
	dbPath := newFenceTestDB(t, 1)

	count, earliest, err := countFencedExecutions(context.Background(), dbPath)
	require.NoError(t, err)
	require.Equal(t, int64(1), count)
	require.Equal(t, int64(1722800000000), earliest)

	// query_only must have prevented any accidental write path: a second
	// independent read sees the same state.
	count2, _, err := countFencedExecutions(context.Background(), dbPath)
	require.NoError(t, err)
	require.Equal(t, count, count2)
}
