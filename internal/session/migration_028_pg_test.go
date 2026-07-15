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

func TestMigration028PGPreservesLegacyDuplicateSequences(t *testing.T) {
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
	_, err = provider.UpTo(ctx, 27)
	require.NoError(t, err)

	for _, content := range []string{`{"content":"old-1"}`, `{"content":"old-2"}`} {
		_, err = db.ExecContext(ctx, `INSERT INTO events
			(session_id, seq, type, data, direction, source, created_at)
			VALUES ('legacy', 1, 'message', $1, 'outbound', 'normal', 1)`, content)
		require.NoError(t, err)
	}
	_, err = provider.UpTo(ctx, 28)
	require.NoError(t, err)

	var count int
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM events WHERE session_id = 'legacy'`).Scan(&count))
	require.Equal(t, 2, count)
	_, err = db.ExecContext(ctx, `INSERT INTO events
		(session_id, seq, type, data, direction, source, created_at)
		VALUES ('legacy', 1, 'message', '{}', 'outbound', 'normal', 2)`)
	require.Error(t, err, "new event must conflict with a legacy sequence")
}
