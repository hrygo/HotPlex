package session

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"

	"github.com/hrygo/hotplex/internal/dbutil"
	"github.com/pressly/goose/v3"
)

//go:embed sql/migrations/*.sql
var migrationFS embed.FS

//go:embed sql/migrations-postgres/*.sql
var migrationsPGFs embed.FS

// runMigrations applies all pending goose migrations to the database.
func runMigrations(ctx context.Context, db *sql.DB, dialect dbutil.Dialect) error {
	var gooseDialect goose.Dialect
	var embedFS embed.FS
	switch dialect {
	case dbutil.DialectSQLite:
		gooseDialect = goose.DialectSQLite3
		embedFS = migrationFS
	case dbutil.DialectPostgres:
		gooseDialect = goose.DialectPostgres
		embedFS = migrationsPGFs
	default:
		return fmt.Errorf("unsupported dialect: %s", dialect)
	}

	var subDir string
	switch dialect {
	case dbutil.DialectSQLite:
		subDir = "sql/migrations"
	case dbutil.DialectPostgres:
		subDir = "sql/migrations-postgres"
	}
	migrations, err := fs.Sub(embedFS, subDir)
	if err != nil {
		return fmt.Errorf("session store: migration fs: %w", err)
	}

	provider, err := goose.NewProvider(
		gooseDialect,
		db,
		migrations,
		goose.WithDisableGlobalRegistry(true),
	)
	if err != nil {
		return fmt.Errorf("session store: goose provider: %w", err)
	}

	results, err := provider.Up(ctx)
	if err != nil {
		return fmt.Errorf("session store: goose up: %w", err)
	}
	for _, r := range results {
		slog.Default().Debug("session store: migration applied", "component", "session_store", "source", r.Source.Path, "duration", r.Duration)
	}
	return nil
}
