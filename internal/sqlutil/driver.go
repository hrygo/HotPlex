package sqlutil

import _ "modernc.org/sqlite"

// DriverName is the database/sql driver name for modernc.org/sqlite (pure Go).
const DriverName = "sqlite"

// DriverNamePG is the database/sql driver name for pgx (PostgreSQL).
// The pgx driver must be imported by the main binary (import _ "github.com/jackc/pgx/v5/stdlib").
const DriverNamePG = "pgx"
