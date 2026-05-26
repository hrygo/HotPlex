package session

import (
	"database/sql"
	"time"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/sqlutil"
)

// dbOpenOpts configures differences between store DB connections.
type dbOpenOpts struct {
	Label       string // label for InitSQLiteDB ("session", "conversation")
	MaxOpen     int    // connection pool size (0 = driver default)
	MaxIdle     int    // idle connection pool size
	MaxLifetime time.Duration
	MaxIdleTime time.Duration
}

// openSQLiteDB opens a SQLite database with PRAGMAs and pool settings.
func openSQLiteDB(cfg *config.Config, opts dbOpenOpts) (*sql.DB, error) {
	dbPath := cfg.DB.EffectiveSQLitePath()
	return sqlutil.OpenDB(dbPath, &cfg.DB, sqlutil.DialectSQLite, opts.Label, sqlutil.PoolOpts{
		MaxOpen:     opts.MaxOpen,
		MaxIdle:     opts.MaxIdle,
		MaxLifetime: opts.MaxLifetime,
		MaxIdleTime: opts.MaxIdleTime,
	})
}
