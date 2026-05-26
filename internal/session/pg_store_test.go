package session

import (
	"context"
	"database/sql"
	"log/slog"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/dbutil"
	"github.com/hrygo/hotplex/pkg/events"
)

func newPGMock(t *testing.T) (*pgStore, sqlmock.Sqlmock, func()) {
	t.Helper()
	mockDB, mock, err := sqlmock.New()
	require.NoError(t, err)

	db := &dbutil.DB{DB: mockDB}

	// Rebind all queries to PG $N placeholders manually (same as NewPGStore does).
	q := make(map[string]string)
	q["store.get_session"] = dbutil.DialectPostgres.Rebind(
		"SELECT id, user_id, COALESCE(owner_id, user_id), worker_session_id, worker_type, state, bot_id, platform, platform_key_json, COALESCE(work_dir, ''), COALESCE(title, ''), created_at, updated_at, expires_at, idle_expires_at, context_json, source FROM sessions WHERE id = ?")
	q["store.delete_terminated"] = dbutil.DialectPostgres.Rebind(
		"DELETE FROM sessions WHERE state = ? AND ((source = 'cron' AND updated_at <= ?) OR (source != 'cron' AND updated_at <= ?))")
	q["store.get_sessions_by_state"] = dbutil.DialectPostgres.Rebind(
		"SELECT id FROM sessions WHERE state = ?")
	q["store.delete_physical"] = dbutil.DialectPostgres.Rebind(
		"DELETE FROM sessions WHERE id = ?")

	store := &pgStore{
		db:      db,
		dialect: dbutil.DialectPostgres,
		queries: q,
		log:     slog.Default(),
	}

	return store, mock, func() {
		require.NoError(t, mock.ExpectationsWereMet())
		mockDB.Close()
	}
}

func sessionColumns() []string {
	return []string{
		"id", "user_id", "owner_id", "worker_session_id", "worker_type", "state", "bot_id",
		"platform", "platform_key_json", "work_dir", "title",
		"created_at", "updated_at", "expires_at", "idle_expires_at", "context_json", "source",
	}
}

func TestPGStore_Get_Found(t *testing.T) {
	t.Parallel()
	store, mock, cleanup := newPGMock(t)
	defer cleanup()

	now := time.Now()
	rows := sqlmock.NewRows(sessionColumns()).
		AddRow("sess-1", "user-1", "owner-1", "", "claude_code", string(events.StateRunning), "bot-1",
			"slack", `{"channel_id":"C123"}`, "/work", "My Session",
			now, now, nil, nil, `{"key":"value"}`, "")

	q := dbutil.DialectPostgres.Rebind(
		"SELECT id, user_id, COALESCE(owner_id, user_id), worker_session_id, worker_type, state, bot_id, platform, platform_key_json, COALESCE(work_dir, ''), COALESCE(title, ''), created_at, updated_at, expires_at, idle_expires_at, context_json, source FROM sessions WHERE id = ?")

	mock.ExpectQuery(regexp.QuoteMeta(q)).WithArgs("sess-1").WillReturnRows(rows)

	info, err := store.Get(context.Background(), "sess-1")
	require.NoError(t, err)
	require.NotNil(t, info)
	require.Equal(t, "sess-1", info.ID)
	require.Equal(t, "user-1", info.UserID)
	require.Equal(t, "owner-1", info.OwnerID)
	require.Equal(t, events.StateRunning, info.State)
}

func TestPGStore_Get_NotFound(t *testing.T) {
	t.Parallel()
	store, mock, cleanup := newPGMock(t)
	defer cleanup()

	q := dbutil.DialectPostgres.Rebind(
		"SELECT id, user_id, COALESCE(owner_id, user_id), worker_session_id, worker_type, state, bot_id, platform, platform_key_json, COALESCE(work_dir, ''), COALESCE(title, ''), created_at, updated_at, expires_at, idle_expires_at, context_json, source FROM sessions WHERE id = ?")

	mock.ExpectQuery(regexp.QuoteMeta(q)).WithArgs("nonexistent").WillReturnError(sql.ErrNoRows)

	info, err := store.Get(context.Background(), "nonexistent")
	require.ErrorIs(t, err, ErrSessionNotFound)
	require.Nil(t, info)
}

func TestPGStore_DeleteTerminated(t *testing.T) {
	t.Parallel()
	store, mock, cleanup := newPGMock(t)
	defer cleanup()

	cronCutoff := time.UnixMilli(1000)
	defaultCutoff := time.UnixMilli(2000)

	q := dbutil.DialectPostgres.Rebind(
		"DELETE FROM sessions WHERE state = ? AND ((source = 'cron' AND updated_at <= ?) OR (source != 'cron' AND updated_at <= ?))")

	mock.ExpectExec(regexp.QuoteMeta(q)).
		WithArgs(string(events.StateTerminated), cronCutoff, defaultCutoff).
		WillReturnResult(sqlmock.NewResult(0, 3))

	err := store.DeleteTerminated(context.Background(), cronCutoff, defaultCutoff)
	require.NoError(t, err)
}

func TestPGStore_GetSessionsByState(t *testing.T) {
	t.Parallel()
	store, mock, cleanup := newPGMock(t)
	defer cleanup()

	q := dbutil.DialectPostgres.Rebind("SELECT id FROM sessions WHERE state = ?")

	rows := sqlmock.NewRows([]string{"id"}).
		AddRow("sess-1").
		AddRow("sess-2")

	mock.ExpectQuery(regexp.QuoteMeta(q)).WithArgs(string(events.StateRunning)).WillReturnRows(rows)

	ids, err := store.GetSessionsByState(context.Background(), events.StateRunning)
	require.NoError(t, err)
	require.Equal(t, []string{"sess-1", "sess-2"}, ids)
}

func TestPGStore_DeletePhysical(t *testing.T) {
	t.Parallel()
	store, mock, cleanup := newPGMock(t)
	defer cleanup()

	q := dbutil.DialectPostgres.Rebind("DELETE FROM sessions WHERE id = ?")

	mock.ExpectExec(regexp.QuoteMeta(q)).WithArgs("sess-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := store.DeletePhysical(context.Background(), "sess-1")
	require.NoError(t, err)
}

func TestPGStore_Close(t *testing.T) {
	t.Parallel()
	store, _, cleanup := newPGMock(t)
	defer cleanup()

	// Close is a no-op for PGStore.
	err := store.Close()
	require.NoError(t, err)
}
