package dbutil

import (
	"context"
	"testing"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/stretchr/testify/require"
)

func TestOpenSQLiteMemory(t *testing.T) {
	cfg := &config.DBConfig{Path: ":memory:"}
	db, err := Open(DialectSQLite, cfg)
	require.NoError(t, err)
	require.NotNil(t, db)
	t.Cleanup(func() { db.Close() })

	require.Equal(t, DialectSQLite, db.Dialect())

	_, err = db.ExecContext(context.Background(),
		"CREATE TABLE test (id INTEGER PRIMARY KEY, name TEXT)")
	require.NoError(t, err)

	row := db.QueryRowContext(context.Background(), "SELECT 1")
	var val int
	err = row.Scan(&val)
	require.NoError(t, err)
	require.Equal(t, 1, val)

	_, err = db.ExecContext(context.Background(),
		"INSERT INTO test (id, name) VALUES (?, ?)", 1, "hello")
	require.NoError(t, err)

	row = db.QueryRowContext(context.Background(),
		"SELECT name FROM test WHERE id = ?", 1)
	var name string
	err = row.Scan(&name)
	require.NoError(t, err)
	require.Equal(t, "hello", name)
}

func TestOpenSQLiteMemoryDefaultPath(t *testing.T) {
	cfg := &config.DBConfig{Path: ""}
	db, err := Open(DialectSQLite, cfg)
	require.NoError(t, err)
	require.NotNil(t, db)
	t.Cleanup(func() { db.Close() })
	require.Equal(t, DialectSQLite, db.Dialect())
}

func TestOpenUnsupportedDialect(t *testing.T) {
	cfg := &config.DBConfig{Path: ":memory:"}
	_, err := Open("mysql", cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported dialect")
}

func TestDBDialectRoundTrip(t *testing.T) {
	cfg := &config.DBConfig{Path: ":memory:"}
	db, err := Open(DialectSQLite, cfg)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	require.Equal(t, DialectSQLite, db.Dialect())
	require.Equal(t, "?", db.Dialect().Placeholder(1))
	require.Equal(t, `"test"`, db.Dialect().QuoteIdent("test"))
	require.Equal(t, 1, db.Dialect().BoolValue(true))
	require.Equal(t, 0, db.Dialect().BoolValue(false))
}
