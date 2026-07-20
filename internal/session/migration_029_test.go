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

func TestMigration029AddsEmptySessionPermissionCeiling(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, err := sql.Open(sqlutil.DriverName, filepath.Join(t.TempDir(), "migration-029.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	cfg := config.Default()
	require.NoError(t, sqlutil.InitSQLiteDB(db, &cfg.DB, sqlutil.DialectSQLite, "migration_029_test"))

	migrations, err := fs.Sub(migrationFS, "sql/migrations")
	require.NoError(t, err)
	provider, err := goose.NewProvider(
		goose.DialectSQLite3,
		db,
		migrations,
		goose.WithDisableGlobalRegistry(true),
	)
	require.NoError(t, err)
	_, err = provider.UpTo(ctx, 28)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO sessions
		(id, user_id, worker_type, state, platform, platform_key_json, created_at, updated_at, title)
		VALUES ('legacy', 'u1', 'claude_code', 'created', '', '', 1, 1, '')`)
	require.NoError(t, err)

	_, err = provider.UpTo(ctx, 29)
	require.NoError(t, err)
	var ceiling string
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT permission_ceiling FROM sessions WHERE id = 'legacy'`).Scan(&ceiling))
	require.Empty(t, ceiling)
}
