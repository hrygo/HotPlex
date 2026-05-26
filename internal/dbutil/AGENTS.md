# Database Utility Packages (sqlutil + dbutil)

## OVERVIEW
Cross-dialect database infrastructure: pure-Go SQLite driver (modernc.org), global write serialization (WriteMu) to eliminate SQLITE_BUSY, PRAGMA tuning, connection pool management, and dialect-aware query helpers (? → $N rebind).

## STRUCTURE
```
sqlutil/
  driver.go     # DriverName/sqlite constant, DriverNamePG/pgx constant
  open.go       # OpenDB: dialect-conditional open + PRAGMA + pool config
  pragma.go     # InitSQLiteDB: 8 PRAGMAs (WAL, busy_timeout, FK, sync, cache, temp, mmap, wal_checkpoint)
  writemu.go    # WriteMu: global write serializer, no-op for PostgreSQL

dbutil/
  db.go         # DB struct (wraps *sql.DB), Open helper for SQLite/Postgres
  dialect.go    # Dialect type, ParseDialect, Rebind/Placeholder/QuoteIdent/BoolValue/IsUniqueViolation
  rebind.go     # ? → $N positional rebind for PostgreSQL queries
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Open database connection | `sqlutil/open.go:25` OpenDB | Dialect-conditional: SQLite (PRAGMA + pool) or Postgres (pool) |
| SQLite PRAGMA tuning | `sqlutil/pragma.go:14` InitSQLiteDB | 8 PRAGMAs: WAL, busy_timeout, FK, sync, cache, temp, mmap, wal_checkpoint |
| Write serialization | `sqlutil/writemu.go:17` WriteMu | Global mutex, no-op for PG |
| WithLock helper | `sqlutil/writemu.go:46` WithLock | Acquire → fn → release, nil-safe |
| DB wrapper | `dbutil/db.go:13` DB | Embeds *sql.DB, carries Dialect |
| Dialect helpers | `dbutil/dialect.go` | Rebind, Placeholder, QuoteIdent, BoolValue, IsUniqueViolation |
| Query rebind | `dbutil/rebind.go` | ? → $1, $2, ... for PostgreSQL compatibility |

## KEY PATTERNS

**WriteMu global write serialization**
- Single `*WriteMu` shared across ALL SQLite stores (session, event, cron, message)
- Eliminates SQLITE_BUSY by serializing writes at application level
- PostgreSQL: all operations are no-ops (native MVCC concurrency)
- Usage: `writeMu.WithLock(func() error { return writeOp(db) })`

**SQLite PRAGMA stack (InitSQLiteDB)**
- journal_mode=WAL: concurrent reads during writes
- busy_timeout: configurable (default 5000ms)
- foreign_keys=ON: enforce referential integrity
- synchronous=NORMAL: balance durability/performance
- cache_size: configurable KiB
- temp_store=MEMORY: avoid temp file I/O
- mmap_size: configurable MiB for memory-mapped I/O
- wal_autocheckpoint: configurable threshold

**Dialect abstraction**
- SQLite: `?` placeholders, `1/0` booleans, `UNIQUE constraint failed`
- PostgreSQL: `$N` placeholders, native booleans, `23505` unique violation
- `Rebind()`: transforms `INSERT INTO t VALUES (?, ?, ?)` → `VALUES ($1, $2, $3)`

**Connection pool defaults**
- SQLite: MaxOpenConns configurable (session store uses 1 for single-writer), MaxIdleConns=2
- PostgreSQL: MaxOpenConns=25, MaxIdleConns=5

## ANTI-PATTERNS
- ❌ Open SQLite without InitSQLiteDB — missing PRAGMAs cause data corruption
- ❌ Skip WriteMu for SQLite writes — SQLITE_BUSY under concurrent access
- ❌ Use MaxOpenConns > 1 for SQLite single-writer stores — breaks write serialization
- ❌ Use `?` placeholders in PostgreSQL queries — use `$N` or Rebind()
- ❌ Hard-code dialect strings — use `sqlutil.DialectSQLite`/`sqlutil.DialectPostgres` constants
