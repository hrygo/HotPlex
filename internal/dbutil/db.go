package dbutil

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/sqlutil"
)

type DB struct {
	*sql.DB
	dialect Dialect
}

func (db *DB) Dialect() Dialect {
	return db.dialect
}

func Open(dialect Dialect, cfg *config.DBConfig) (*DB, error) {
	switch dialect {
	case DialectSQLite:
		return openSQLite(cfg)
	case DialectPostgres:
		return openPostgres(cfg)
	default:
		return nil, fmt.Errorf("dbutil: unsupported dialect: %s", dialect)
	}
}

func openSQLite(cfg *config.DBConfig) (*DB, error) {
	dbPath := cfg.EffectiveSQLitePath()
	if dbPath == "" {
		dbPath = ":memory:"
	}

	if dbPath != ":memory:" {
		dir := filepath.Dir(dbPath)
		if dir != "." && dir != "/" {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, fmt.Errorf("dbutil: create db dir: %w", err)
			}
		}
	}

	sqldb, err := sql.Open(sqlutil.DriverName, dbPath)
	if err != nil {
		return nil, fmt.Errorf("dbutil: open sqlite %s: %w", dbPath, err)
	}

	if err := sqlutil.InitSQLiteDB(sqldb, cfg, sqlutil.DialectSQLite, "dbutil"); err != nil {
		_ = sqldb.Close()
		return nil, fmt.Errorf("dbutil: init sqlite: %w", err)
	}

	if maxOpen := cfg.EffectiveMaxOpenConns(); maxOpen > 0 {
		sqldb.SetMaxOpenConns(maxOpen)
	}
	sqldb.SetMaxIdleConns(2)

	return &DB{DB: sqldb, dialect: DialectSQLite}, nil
}

func openPostgres(cfg *config.DBConfig) (*DB, error) {
	if cfg.Postgres.ConnStr == "" {
		return nil, fmt.Errorf("dbutil: postgres DSN is required — set db.postgres.dsn or HOTPLEX_DB_POSTGRES_DSN")
	}

	dsn := cfg.DSN()
	sqldb, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("dbutil: open postgres: %w", err)
	}

	if err := sqldb.Ping(); err != nil {
		_ = sqldb.Close()
		return nil, fmt.Errorf("dbutil: ping postgres: %w", err)
	}

	maxOpen := cfg.Postgres.MaxOpenConns
	if maxOpen <= 0 {
		maxOpen = 25
	}
	sqldb.SetMaxOpenConns(maxOpen)
	sqldb.SetMaxIdleConns(5)
	sqldb.SetConnMaxLifetime(5 * time.Minute)
	sqldb.SetConnMaxIdleTime(3 * time.Minute)

	return &DB{DB: sqldb, dialect: DialectPostgres}, nil
}
