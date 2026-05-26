package dbutil

import (
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

type Dialect string

const (
	DialectSQLite   Dialect = "sqlite"
	DialectPostgres Dialect = "postgres"
)

func ParseDialect(s string) Dialect {
	switch strings.ToLower(s) {
	case "postgres", "pg", "postgresql":
		return DialectPostgres
	default:
		return DialectSQLite
	}
}

func (d Dialect) Rebind(query string) string {
	if d == DialectPostgres {
		return rebind(query)
	}
	return query
}

func (d Dialect) Placeholder(i int) string {
	if d == DialectPostgres {
		return fmt.Sprintf("$%d", i)
	}
	return "?"
}

func (d Dialect) QuoteIdent(name string) string {
	return `"` + name + `"`
}

func (d Dialect) BoolValue(b bool) any {
	if d == DialectPostgres {
		return b
	}
	if b {
		return 1
	}
	return 0
}

func (d Dialect) IsUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	switch d {
	case DialectSQLite:
		return strings.Contains(err.Error(), "UNIQUE constraint failed")
	case DialectPostgres:
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			return pgErr.Code == "23505"
		}
		return false
	default:
		return false
	}
}
