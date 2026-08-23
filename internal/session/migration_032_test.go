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

func TestMigration032AddsNullableTurnClientMessageID(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open(sqlutil.DriverName, filepath.Join(t.TempDir(), "migration-032.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	cfg := config.Default()
	require.NoError(t, sqlutil.InitSQLiteDB(db, &cfg.DB, sqlutil.DialectSQLite, "migration_032_test"))

	migrations, err := fs.Sub(migrationFS, "sql/migrations")
	require.NoError(t, err)
	provider, err := goose.NewProvider(
		goose.DialectSQLite3,
		db,
		migrations,
		goose.WithDisableGlobalRegistry(true),
	)
	require.NoError(t, err)
	_, err = provider.UpTo(ctx, 31)
	require.NoError(t, err)

	// A row written by the pre-032 schema must survive the additive migration
	// and read back with a NULL identity.
	_, err = db.ExecContext(ctx, `INSERT INTO turns
		(session_id, generation, turn_num, seq, role, content, created_at)
		VALUES ('legacy', 1, 1, 1, 'user', 'old', 1)`)
	require.NoError(t, err)

	_, err = provider.UpTo(ctx, 32)
	require.NoError(t, err)

	var columnCount int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('turns')
		WHERE name = 'client_message_id'`).Scan(&columnCount))
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
