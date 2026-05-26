package dbutil

import (
	"testing"
)

func TestRebind(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "simple",
			input: "SELECT ? FROM t",
			want:  "SELECT $1 FROM t",
		},
		{
			name:  "multiple",
			input: "SELECT ?, ?, ?",
			want:  "SELECT $1, $2, $3",
		},
		{
			name:  "string literal preserves ?",
			input: "SELECT * FROM t WHERE name = 'hello?world'",
			want:  "SELECT * FROM t WHERE name = 'hello?world'",
		},
		{
			name:  "sqlite escaped quote preserves ?",
			input: "SELECT * FROM t WHERE name = 'it''s ?'",
			want:  "SELECT * FROM t WHERE name = 'it''s ?'",
		},
		{
			name:  "double quoted ident preserves ?",
			input: `SELECT * FROM "my?table"`,
			want:  `SELECT * FROM "my?table"`,
		},
		{
			name:  "line comment preserves ?",
			input: "SELECT ? FROM t -- comment ?\nWHERE id = ?",
			want:  "SELECT $1 FROM t -- comment ?\nWHERE id = $2",
		},
		{
			name:  "block comment preserves ?",
			input: "SELECT ? /* ? */ FROM t WHERE id = ?",
			want:  "SELECT $1 /* ? */ FROM t WHERE id = $2",
		},
		{
			name:  "dollar quote preserves ?",
			input: "SELECT $$text?text$$ AS x WHERE id = ?",
			want:  "SELECT $$text?text$$ AS x WHERE id = $1",
		},
		{
			name:  "dollar quote with tag preserves ?",
			input: "SELECT $tag$text?text$tag$ AS x WHERE id = ?",
			want:  "SELECT $tag$text?text$tag$ AS x WHERE id = $1",
		},
		{
			name:  "mixed",
			input: "SELECT ? FROM t WHERE x = 'test?x' AND y = ?",
			want:  "SELECT $1 FROM t WHERE x = 'test?x' AND y = $2",
		},
		{
			name:  "no question mark",
			input: "SELECT * FROM t WHERE id = 1",
			want:  "SELECT * FROM t WHERE id = 1",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "multi-line",
			input: "SELECT ?\nFROM t\nWHERE id = ?\n  AND name = ?",
			want:  "SELECT $1\nFROM t\nWHERE id = $2\n  AND name = $3",
		},
		{
			name:  "escaped backslash question mark",
			input: `SELECT \? FROM t WHERE id = ?`,
			want:  `SELECT \? FROM t WHERE id = $1`,
		},
		{
			name:  "dollar not quote (single dollar)",
			input: "SELECT $1 FROM t WHERE id = ?",
			want:  "SELECT $1 FROM t WHERE id = $1",
		},
		{
			name:  "double escaped quote in string",
			input: "SELECT '?''?' FROM t WHERE id = ?",
			want:  "SELECT '?''?' FROM t WHERE id = $1",
		},
		{
			name:  "block comment spanning multiple lines",
			input: "SELECT ? /*\n?\n*/ FROM t",
			want:  "SELECT $1 /*\n?\n*/ FROM t",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rebind(tt.input)
			if got != tt.want {
				t.Errorf("rebind() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRebindDialectSQLite(t *testing.T) {
	input := "SELECT ? FROM t WHERE name = 'hello?world' AND id = ?"
	got := DialectSQLite.Rebind(input)
	if got != input {
		t.Errorf("DialectSQLite.Rebind() = %q, want %q", got, input)
	}
}

func TestDialectPlaceholder(t *testing.T) {
	tests := []struct {
		name     string
		dialect  Dialect
		n        int
		expected string
	}{
		{"sqlite 1", DialectSQLite, 1, "?"},
		{"sqlite 3", DialectSQLite, 3, "?"},
		{"postgres 1", DialectPostgres, 1, "$1"},
		{"postgres 3", DialectPostgres, 3, "$3"},
		{"postgres 10", DialectPostgres, 10, "$10"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.dialect.Placeholder(tt.n)
			if got != tt.expected {
				t.Errorf("Placeholder(%d) = %q, want %q", tt.n, got, tt.expected)
			}
		})
	}
}

func TestDialectQuoteIdent(t *testing.T) {
	tests := []struct {
		name     string
		dialect  Dialect
		input    string
		expected string
	}{
		{"sqlite", DialectSQLite, "my_table", `"my_table"`},
		{"postgres", DialectPostgres, "my_table", `"my_table"`},
		{"sqlite with special chars", DialectSQLite, "my table", `"my table"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.dialect.QuoteIdent(tt.input)
			if got != tt.expected {
				t.Errorf("QuoteIdent(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestDialectBoolValue(t *testing.T) {
	tests := []struct {
		name     string
		dialect  Dialect
		input    bool
		expected any
	}{
		{"sqlite true", DialectSQLite, true, 1},
		{"sqlite false", DialectSQLite, false, 0},
		{"postgres true", DialectPostgres, true, true},
		{"postgres false", DialectPostgres, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.dialect.BoolValue(tt.input)
			if got != tt.expected {
				t.Errorf("BoolValue(%v) = %v (type %T), want %v (type %T)",
					tt.input, got, got, tt.expected, tt.expected)
			}
		})
	}
}

func TestDialectBoolValueSQLiteType(t *testing.T) {
	// Verify SQLite BoolValue returns int, not bool
	v := DialectSQLite.BoolValue(true)
	if _, ok := v.(int); !ok {
		t.Errorf("SQLite BoolValue(true) type = %T, want int", v)
	}

	v = DialectSQLite.BoolValue(false)
	if _, ok := v.(int); !ok {
		t.Errorf("SQLite BoolValue(false) type = %T, want int", v)
	}
}

func TestDialectBoolValuePostgresType(t *testing.T) {
	v := DialectPostgres.BoolValue(true)
	if _, ok := v.(bool); !ok {
		t.Errorf("Postgres BoolValue(true) type = %T, want bool", v)
	}
}

func TestDialectIsUniqueViolation(t *testing.T) {
	errSQLite := errStr("UNIQUE constraint failed: users.email")
	errPG23505 := errStr("ERROR: duplicate key value violates unique constraint \"users_pkey\" (SQLSTATE 23505)")
	errPGDupKey := errStr("duplicate key value violates unique constraint")
	errOther := errStr("some other error")

	tests := []struct {
		name     string
		dialect  Dialect
		err      error
		expected bool
	}{
		{"sqlite match", DialectSQLite, errSQLite, true},
		{"sqlite no match", DialectSQLite, errOther, false},
		{"sqlite nil", DialectSQLite, nil, false},
		{"postgres 23505", DialectPostgres, errPG23505, true},
		{"postgres duplicate key", DialectPostgres, errPGDupKey, true},
		{"postgres no match", DialectPostgres, errOther, false},
		{"postgres nil", DialectPostgres, nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.dialect.IsUniqueViolation(tt.err)
			if got != tt.expected {
				t.Errorf("IsUniqueViolation() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestParseDialect(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected Dialect
	}{
		{"postgres", "postgres", DialectPostgres},
		{"pg", "pg", DialectPostgres},
		{"postgresql", "postgresql", DialectPostgres},
		{"Postgres", "Postgres", DialectPostgres},
		{"PG", "PG", DialectPostgres},
		{"PostgreSQL", "PostgreSQL", DialectPostgres},
		{"sqlite", "sqlite", DialectSQLite},
		{"SQLite", "SQLite", DialectSQLite},
		{"empty", "", DialectSQLite},
		{"unknown", "mysql", DialectSQLite},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseDialect(tt.input)
			if got != tt.expected {
				t.Errorf("ParseDialect(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

// errStr is a simple error that implements the error interface.
type errStr string

func (e errStr) Error() string { return string(e) }
