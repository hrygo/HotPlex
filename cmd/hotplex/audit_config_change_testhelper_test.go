package main

import (
	"database/sql"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/audit"
	"github.com/hrygo/hotplex/internal/dbutil"
	"github.com/hrygo/hotplex/internal/sqlutil"
)

// auditTestStore wraps an audit.Store with row-inspection helpers for tests in
// package main (cmd/hotplex). Mirrors the pattern in internal/audit/store_test.go
// but lives here because newTestSQLiteStore is package-private to audit.
const auditTestSchemaSQL = `
CREATE TABLE IF NOT EXISTS user_activity (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    ts            INTEGER NOT NULL,
    user_id       TEXT    NOT NULL,
    user_id_type  TEXT    NOT NULL,
    platform      TEXT    NOT NULL,
    session_id    TEXT,
    action        TEXT    NOT NULL,
    resource_type TEXT,
    resource_id   TEXT,
    outcome       TEXT    NOT NULL,
    detail_json   TEXT    NOT NULL,
    event_ref     TEXT,
    ip            TEXT,
    user_agent    TEXT,
    prev_hash     TEXT    NOT NULL,
    self_hash     TEXT    NOT NULL
);
CREATE TABLE IF NOT EXISTS audit_chain_checkpoints (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    pruned_at       INTEGER NOT NULL,
    last_self_hash  TEXT    NOT NULL,
    next_id         INTEGER NOT NULL
);
`

type auditTestStore struct {
	audit.Store
	db *sql.DB
}

func newAuditTestStore(t *testing.T) *auditTestStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "audit_cmd_test.db")
	db, err := sql.Open(sqlutil.DriverName, dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	_, err = db.Exec("PRAGMA journal_mode=WAL")
	require.NoError(t, err)
	_, err = db.Exec(auditTestSchemaSQL)
	require.NoError(t, err)
	writeMu := sqlutil.NewWriteMu(sqlutil.DialectSQLite)
	store, err := audit.NewStore(db, dbutil.DialectSQLite, writeMu, slog.Default())
	require.NoError(t, err)
	return &auditTestStore{Store: store, db: db}
}

// RowCount returns the number of rows in user_activity.
func (s *auditTestStore) RowCount(t *testing.T) int {
	t.Helper()
	var n int
	err := s.db.QueryRow("SELECT COUNT(*) FROM user_activity").Scan(&n)
	require.NoError(t, err)
	return n
}

// AllRows returns every user_activity row ordered by id.
func (s *auditTestStore) AllRows(t *testing.T) []audit.UserActivity {
	t.Helper()
	rows, err := s.db.Query(`SELECT id, ts, user_id, user_id_type, platform,
		COALESCE(session_id,''), action, COALESCE(resource_type,''),
		COALESCE(resource_id,''), outcome, detail_json, COALESCE(event_ref,''),
		COALESCE(ip,''), COALESCE(user_agent,''), prev_hash, self_hash
		FROM user_activity ORDER BY id`)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var out []audit.UserActivity
	for rows.Next() {
		var r audit.UserActivity
		err := rows.Scan(&r.ID, &r.Ts, &r.UserID, &r.UserIDType, &r.Platform,
			&r.SessionID, &r.Action, &r.ResourceType, &r.ResourceID, &r.Outcome,
			&r.DetailJSON, &r.EventRef, &r.IP, &r.UserAgent, &r.PrevHash, &r.SelfHash)
		require.NoError(t, err)
		out = append(out, r)
	}
	require.NoError(t, rows.Err())
	return out
}
