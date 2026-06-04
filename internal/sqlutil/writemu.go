package sqlutil

import "sync"

// Dialect constants for write serializer configuration.
// These mirror dbutil.Dialect values but are defined here to
// avoid an import cycle (dbutil imports sqlutil).
const (
	DialectSQLite   = "sqlite"
	DialectPostgres = "postgres"
)

// WriteMu serializes write operations across all stores sharing
// the same database file. For SQLite (WAL mode), writes are serialized
// to prevent SQLITE_BUSY errors. For PostgreSQL, all operations are
// no-ops — PG handles concurrency natively.
type WriteMu struct {
	mu      sync.Mutex
	dialect string
}

// NewWriteMu creates a new write serializer for the given dialect.
// Pass the dialect string (e.g., "sqlite" or "postgres").
// Empty dialect defaults to SQLite (safe-write behavior).
func NewWriteMu(dialect string) *WriteMu {
	if dialect == "" {
		dialect = DialectSQLite
	}
	return &WriteMu{dialect: dialect}
}

// Lock acquires the write mutex. No-op for PostgreSQL.
func (m *WriteMu) Lock() {
	if m == nil || m.dialect == DialectPostgres {
		return
	}
	m.mu.Lock()
}

// Unlock releases the write mutex. No-op for PostgreSQL.
func (m *WriteMu) Unlock() {
	if m == nil || m.dialect == DialectPostgres {
		return
	}
	m.mu.Unlock()
}

// WithLock acquires the write mutex, calls fn, and releases it.
// If m is nil or dialect is PostgreSQL, fn is called without locking.
func (m *WriteMu) WithLock(fn func() error) error {
	if m == nil || m.dialect == DialectPostgres {
		return fn()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return fn()
}
