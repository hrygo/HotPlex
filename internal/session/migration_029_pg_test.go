//go:build pg

package session

import (
	"context"
	"database/sql"
	"io/fs"
	"os"
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/sqlutil"
)

func TestMigration029PGAddsEmptySessionPermissionCeiling(t *testing.T) {
	dsn := os.Getenv("HOTPLEX_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("HOTPLEX_TEST_PG_DSN not set; skipping PG migration test")
	}
	ctx := context.Background()
	db, err := sql.Open(sqlutil.DriverNamePG, dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.ExecContext(ctx, `DROP SCHEMA IF EXISTS public CASCADE`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `CREATE SCHEMA public`)
	require.NoError(t, err)

	migrations, err := fs.Sub(migrationsPGFs, "sql/migrations-postgres")
	require.NoError(t, err)
	provider, err := goose.NewProvider(
		goose.DialectPostgres,
		db,
		migrations,
		goose.WithDisableGlobalRegistry(true),
	)
	require.NoError(t, err)
	_, err = provider.UpTo(ctx, 28)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO sessions
		(id, user_id, worker_type, state, platform, platform_key_json, created_at, updated_at, title)
		VALUES ('legacy', 'u1', 'claude_code', 'created', '', '', NOW(), NOW(), '')`)
	require.NoError(t, err)

	_, err = provider.UpTo(ctx, 29)
	require.NoError(t, err)
	var ceiling string
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT permission_ceiling FROM sessions WHERE id = 'legacy'`).Scan(&ceiling))
	require.Empty(t, ceiling)
}
