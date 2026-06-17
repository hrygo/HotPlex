# SQL Driver Plumbing Package

## OVERVIEW
Low-level database driver registration and SQLite-specific helpers shared by every store in HotPlex. Registers pure-Go SQLite (`modernc.org/sqlite`) and PostgreSQL (`pgx/v5/stdlib`) drivers, applies the SQLite PRAGMA stack on open, and exposes `WriteMu` to serialize writes at the application level. A companion combined doc lives at `internal/dbutil/AGENTS.md`.

## STRUCTURE
```
sqlutil/
  driver.go     # Blank-imports both drivers; exposes DriverName / DriverNamePG constants
  open.go       # OpenDB + PoolOpts: SQLite-only open with dir creation, PRAGMA, pool config
  pragma.go     # InitSQLiteDB: 8 PRAGMAs gated by config, no-op when dialect == postgres
  writemu.go    # WriteMu + DialectSQLite / DialectPostgres constants (mirrored from dbutil)
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Driver constants | `driver.go:10` | `DriverName="sqlite"`, `DriverNamePG="pgx"` |
| Open SQLite | `open.go:23` OpenDB | Rejects non-SQLite dialect; Postgres path lives in `dbutil.Open` |
| Pool config | `open.go:14` PoolOpts | MaxOpen/MaxIdle/MaxLifetime/MaxIdleTime applied only when > 0 |
| Parent dir mkdir | `open.go:32` | Creates db dir (mode 0755) unless `.` or `/` |
| PRAGMA stack | `pragma.go:14` InitSQLiteDB | Returns nil immediately for Postgres |
| Write serializer | `writemu.go:17` WriteMu | `mu sync.Mutex` (explicit, not embedded) + dialect string |
| NewWriteMu | `writemu.go:25` | Empty dialect defaults to SQLite (safe-write behavior) |
| WithLock helper | `writemu.go:50` | nil-safe; calls `fn` without locking for Postgres |

## KEY PATTERNS

**WriteMu behavior split**
- SQLite: every `Lock`/`Unlock`/`WithLock` acquires the real `sync.Mutex`. One `*WriteMu` instance is shared across all SQLite stores (session, event, cron, message) to eliminate `SQLITE_BUSY`.
- PostgreSQL: all three methods short-circuit on `m.dialect == DialectPostgres`. PG's MVCC handles concurrency natively, so the mutex is pure overhead.
- The mutex field is named `mu` and never embedded, matching the project convention.

**Dialect constant duplication (import-cycle avoidance)**
- `DialectSQLite` / `DialectPostgres` are declared in `writemu.go:9` even though `dbutil.Dialect` already models the same concept.
- Reason: `dbutil` imports `sqlutil` (it calls `OpenDB`), so `sqlutil` cannot import `dbutil` back without a cycle. The two definitions must stay string-equal; treat both as the source of truth for the literal `"sqlite"` / `"postgres"` values.

**PRAGMA gating**
- `InitSQLiteDB` reads `config.DBConfig` effective values (WAL, busy_timeout, cache_size, mmap_size, wal_autocheckpoint) so tuning is config-driven, not hard-coded.
- WAL-only PRAGMAs (`journal_mode=WAL`, `wal_autocheckpoint`) are skipped when `EffectiveWALMode()` is false.

**Open-time failure handling**
- `openSQLiteDB` closes the `*sql.DB` if `InitSQLiteDB` returns an error, preventing leaked connections on misconfigured startup.

## ANTI-PATTERNS
- ❌ Import `dbutil` from `sqlutil` — creates a cycle; use the mirrored constants here
- ❌ Open SQLite without `InitSQLiteDB` — missing PRAGMAs risk corruption and busy errors
- ❌ Skip `WriteMu.WithLock` for SQLite writes — concurrent stores will hit `SQLITE_BUSY`
- ❌ Rely on `Lock`/`Unlock` to do anything on Postgres — they are no-ops by design
- ❌ Let `DialectSQLite`/`DialectPostgres` literals drift from `dbutil.Dialect` values — stores compare strings directly
