# PostgreSQL Migrations

Migration numbering aligns with the SQLite migrations in `../migrations/`.
Missing numbers indicate migrations that were SQLite-only (no PG equivalent needed):

- **003** — SQLite-only PRAGMA tuning (WAL mode, busy_timeout, etc.)
- **008** — SQLite-only event store optimize (writable_schema rebuild)

New PostgreSQL-specific migrations start at 009+.
