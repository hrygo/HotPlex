package sqlutil

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/hrygo/hotplex/internal/config"
)

// PoolOpts configures connection pool settings for OpenDB.
type PoolOpts struct {
	MaxOpen     int
	MaxIdle     int
	MaxLifetime time.Duration
	MaxIdleTime time.Duration
}

// OpenDB opens a SQLite database connection with PRAGMAs and pool settings.
// For PostgreSQL, use dbutil.Open instead.
func OpenDB(dbPath string, dbCfg *config.DBConfig, dialect, label string, pool PoolOpts) (*sql.DB, error) {
	if dialect != DialectSQLite {
		return nil, fmt.Errorf("%s: unsupported dialect: %s (use dbutil.Open for PostgreSQL)", label, dialect)
	}
	return openSQLiteDB(dbPath, dbCfg, label, pool)
}

// openSQLiteDB opens a SQLite database with PRAGMAs and pool settings.
func openSQLiteDB(dbPath string, dbCfg *config.DBConfig, label string, pool PoolOpts) (*sql.DB, error) {
	dir := filepath.Dir(dbPath)
	if dir != "." && dir != "/" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("%s: create db dir: %w", label, err)
		}
	}

	db, err := sql.Open(DriverName, dbPath)
	if err != nil {
		return nil, fmt.Errorf("%s: open db: %w", label, err)
	}

	if err := InitSQLiteDB(db, dbCfg, DialectSQLite, label); err != nil {
		_ = db.Close()
		return nil, err
	}

	if pool.MaxOpen > 0 {
		db.SetMaxOpenConns(pool.MaxOpen)
	}
	if pool.MaxIdle > 0 {
		db.SetMaxIdleConns(pool.MaxIdle)
	}
	db.SetConnMaxLifetime(pool.MaxLifetime)
	db.SetConnMaxIdleTime(pool.MaxIdleTime)

	return db, nil
}
