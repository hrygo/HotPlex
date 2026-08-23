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

func TestMigration032PGAddsNullableTurnClientMessageID(t *testing.T) {
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
	_, err = provider.UpTo(ctx, 31)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `INSERT INTO turns
		(session_id, generation, turn_num, seq, role, content, created_at)
		VALUES ('legacy', 1, 1, 1, 'user', 'old', 1)`)
	require.NoError(t, err)

	_, err = provider.UpTo(ctx, 32)
	require.NoError(t, err)

	var columnCount int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'turns' AND column_name = 'client_message_id'`).Scan(&columnCount))
	require.Equal(t, 1, columnCount)

	var legacyID sql.NullString
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT client_message_id FROM turns WHERE session_id = 'legacy'`).Scan(&legacyID))
	require.False(t, legacyID.Valid, "legacy turn identity must remain NULL")

	_, err = db.ExecContext(ctx, `INSERT INTO turns
		(session_id, client_message_id, generation, turn_num, seq, role, content, created_at)
		VALUES ('new', 'cm-new', 1, 1, 2, 'user', 'new', 2)`)
	require.NoError(t, err)
	var newID string
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT client_message_id FROM turns WHERE session_id = 'new'`).Scan(&newID))
	require.Equal(t, "cm-new", newID)
}
