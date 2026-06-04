package sqlutil

import (
	"database/sql"
	"fmt"

	"github.com/hrygo/hotplex/internal/config"
)

// InitSQLiteDB configures a SQLite connection with standard PRAGMAs.
// All hotplex SQLite stores (session, conversation, event) should call this
// to ensure consistent tuning driven by the shared DBConfig.
// When dialect is PostgreSQL, it returns immediately (PRAGMAs are SQLite-only).
func InitSQLiteDB(db *sql.DB, cfg *config.DBConfig, dialect, label string) error {
	if dialect == DialectPostgres {
		return nil
	}
	if cfg.EffectiveWALMode() {
		if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
			return fmt.Errorf("%s WAL: %w", label, err)
		}
	}
	if _, err := db.Exec(fmt.Sprintf("PRAGMA busy_timeout=%d", int(cfg.EffectiveBusyTimeout().Milliseconds()))); err != nil {
		return fmt.Errorf("%s busy_timeout: %w", label, err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		return fmt.Errorf("%s foreign_keys: %w", label, err)
	}
	if _, err := db.Exec("PRAGMA synchronous=NORMAL"); err != nil {
		return fmt.Errorf("%s synchronous: %w", label, err)
	}
	if _, err := db.Exec(fmt.Sprintf("PRAGMA cache_size=-%d", cfg.EffectiveCacheSizeKiB())); err != nil {
		return fmt.Errorf("%s cache_size: %w", label, err)
	}
	if _, err := db.Exec("PRAGMA temp_store=MEMORY"); err != nil {
		return fmt.Errorf("%s temp_store: %w", label, err)
	}
	if _, err := db.Exec(fmt.Sprintf("PRAGMA mmap_size=%d", cfg.EffectiveMmapSizeMiB()*1024*1024)); err != nil {
		return fmt.Errorf("%s mmap_size: %w", label, err)
	}
	if cfg.EffectiveWALMode() {
		if _, err := db.Exec(fmt.Sprintf("PRAGMA wal_autocheckpoint=%d", cfg.EffectiveWalAutoCheckpoint())); err != nil {
			return fmt.Errorf("%s wal_autocheckpoint: %w", label, err)
		}
	}
	return nil
}
