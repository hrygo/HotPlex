package sqlutil

import (
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

const (
	// DriverName is the database/sql driver name for modernc.org/sqlite (pure Go).
	DriverName = "sqlite"
	// DriverNamePG is the database/sql driver name for pgx (PostgreSQL).
	DriverNamePG = "pgx"
)
