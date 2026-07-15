package session

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/sqlutil"
)

func TestMigration028PreservesLegacyDuplicateSequences(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, err := sql.Open(sqlutil.DriverName, filepath.Join(t.TempDir(), "migration-028.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	cfg := config.Default()
	require.NoError(t, sqlutil.InitSQLiteDB(db, &cfg.DB, sqlutil.DialectSQLite, "migration_028_test"))

	migrations, err := fs.Sub(migrationFS, "sql/migrations")
	require.NoError(t, err)
	provider, err := goose.NewProvider(
		goose.DialectSQLite3,
		db,
		migrations,
		goose.WithDisableGlobalRegistry(true),
	)
	require.NoError(t, err)
	_, err = provider.UpTo(ctx, 27)
	require.NoError(t, err)

	for _, content := range []string{`{"content":"old-1"}`, `{"content":"old-2"}`} {
		_, err = db.ExecContext(ctx, `INSERT INTO events
			(session_id, seq, type, data, direction, source, created_at)
			VALUES ('legacy', 1, 'message', ?, 'outbound', 'normal', 1)`, content)
		require.NoError(t, err)
	}

	_, err = provider.UpTo(ctx, 28)
	require.NoError(t, err)

	rows, err := db.QueryContext(ctx, `SELECT data, seq_guard_id FROM events
		WHERE session_id = 'legacy' ORDER BY id`)
	require.NoError(t, err)
	defer rows.Close()
	var contents []string
	var guards []int64
	for rows.Next() {
		var content string
		var guard int64
		require.NoError(t, rows.Scan(&content, &guard))
		contents = append(contents, content)
		guards = append(guards, guard)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []string{`{"content":"old-1"}`, `{"content":"old-2"}`}, contents)
	require.Equal(t, []int64{0, 2}, guards)

	_, err = db.ExecContext(ctx, `INSERT INTO events
		(session_id, seq, type, data, direction, source, created_at)
		VALUES ('legacy', 1, 'message', '{}', 'outbound', 'normal', 2)`)
	require.Error(t, err, "new event must conflict with a legacy sequence")

	_, err = db.ExecContext(ctx, `INSERT INTO events
		(session_id, seq, type, data, direction, source, created_at)
		VALUES ('legacy', 2, 'message', '{}', 'outbound', 'normal', 2)`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO events
		(session_id, seq, type, data, direction, source, created_at)
		VALUES ('legacy', 2, 'message', '{}', 'outbound', 'normal', 3)`)
	require.Error(t, err, "new duplicate sequence must be rejected")

	_, err = provider.DownTo(ctx, 27)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO events
		(session_id, seq, type, data, direction, source, created_at)
		VALUES ('legacy', 2, 'message', '{}', 'outbound', 'normal', 4)`)
	require.NoError(t, err, "down migration must remove the uniqueness guard without deleting rows")
	var count int
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM events WHERE session_id = 'legacy'`).Scan(&count))
	require.Equal(t, 4, count)
}
