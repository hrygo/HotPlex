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
	"github.com/hrygo/hotplex/internal/worker"
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
		"SELECT id, user_id, COALESCE(owner_id, user_id), worker_session_id, worker_type, state, bot_id, COALESCE(bot_name, ''), platform, platform_key_json, COALESCE(work_dir, ''), COALESCE(title, ''), created_at, updated_at, expires_at, idle_expires_at, context_json, source, COALESCE(client_key, ''), COALESCE(workspace_id, ''), COALESCE(permission_ceiling, '') FROM sessions WHERE id = ?")
	q["store.select_terminated_ids"] = dbutil.DialectPostgres.Rebind(
		"SELECT id FROM sessions WHERE state = ? AND ((source = 'cron' AND updated_at <= ?) OR (source != 'cron' AND updated_at <= ?))")
	q["store.delete_terminated_by_id"] = dbutil.DialectPostgres.Rebind(
		"DELETE FROM sessions WHERE id = ? AND state = ? AND ((source = 'cron' AND updated_at <= ?) OR (source != 'cron' AND updated_at <= ?)) RETURNING id, user_id, COALESCE(owner_id, user_id), worker_session_id, worker_type, state, bot_id, COALESCE(bot_name, ''), platform, platform_key_json, COALESCE(work_dir, ''), COALESCE(title, ''), created_at, updated_at, expires_at, idle_expires_at, context_json, source, COALESCE(client_key, ''), COALESCE(workspace_id, ''), COALESCE(permission_ceiling, '')")
	q["store.get_sessions_by_state"] = dbutil.DialectPostgres.Rebind(
		"SELECT id FROM sessions WHERE state = ?")
	q["store.delete_physical"] = dbutil.DialectPostgres.Rebind(
		"DELETE FROM sessions WHERE id = ?")
	q["sessions.set_permission_ceiling_if_empty"] = dbutil.DialectPostgres.Rebind(
		"UPDATE sessions SET permission_ceiling = ? WHERE id = ? AND permission_ceiling = ''")
	q["store.get_permission_ceiling"] = dbutil.DialectPostgres.Rebind(
		"SELECT COALESCE(permission_ceiling, '') FROM sessions WHERE id = ?")

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
		"id", "user_id", "owner_id", "worker_session_id", "worker_type", "state", "bot_id", "bot_name",
		"platform", "platform_key_json", "work_dir", "title",
		"created_at", "updated_at", "expires_at", "idle_expires_at", "context_json", "source", "client_key", "workspace_id", "permission_ceiling",
	}
}

func TestPGStore_Get_Found(t *testing.T) {
	t.Parallel()
	store, mock, cleanup := newPGMock(t)
	defer cleanup()

	now := time.Now()
	rows := sqlmock.NewRows(sessionColumns()).
		AddRow("sess-1", "user-1", "owner-1", "", "claude_code", string(events.StateRunning), "bot-1", "",
			"slack", `{"channel_id":"C123"}`, "/work", "My Session",
			now, now, nil, nil, `{"key":"value"}`, "", "", "", "")

	q := dbutil.DialectPostgres.Rebind(
		"SELECT id, user_id, COALESCE(owner_id, user_id), worker_session_id, worker_type, state, bot_id, COALESCE(bot_name, ''), platform, platform_key_json, COALESCE(work_dir, ''), COALESCE(title, ''), created_at, updated_at, expires_at, idle_expires_at, context_json, source, COALESCE(client_key, ''), COALESCE(workspace_id, ''), COALESCE(permission_ceiling, '') FROM sessions WHERE id = ?")

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
		"SELECT id, user_id, COALESCE(owner_id, user_id), worker_session_id, worker_type, state, bot_id, COALESCE(bot_name, ''), platform, platform_key_json, COALESCE(work_dir, ''), COALESCE(title, ''), created_at, updated_at, expires_at, idle_expires_at, context_json, source, COALESCE(client_key, ''), COALESCE(workspace_id, ''), COALESCE(permission_ceiling, '') FROM sessions WHERE id = ?")

	mock.ExpectQuery(regexp.QuoteMeta(q)).WithArgs("nonexistent").WillReturnError(sql.ErrNoRows)
	pendingQuery := dbutil.DialectPostgres.Rebind(`SELECT EXISTS(SELECT 1 FROM session_cleanup_tasks WHERE session_id = ?)`)
	mock.ExpectQuery(regexp.QuoteMeta(pendingQuery)).WithArgs("nonexistent").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	info, err := store.Get(context.Background(), "nonexistent")
	require.ErrorIs(t, err, ErrSessionNotFound)
	require.Nil(t, info)
}

func TestPGStore_SetPermissionCeilingIfEmpty(t *testing.T) {
	t.Parallel()
	store, mock, cleanup := newPGMock(t)
	defer cleanup()

	mock.ExpectExec(regexp.QuoteMeta(store.queries["sessions.set_permission_ceiling_if_empty"])).
		WithArgs("workspace", "sess-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta(store.queries["store.get_permission_ceiling"])).
		WithArgs("sess-1").
		WillReturnRows(sqlmock.NewRows([]string{"permission_ceiling"}).AddRow("workspace"))

	stored, err := store.SetPermissionCeilingIfEmpty(t.Context(), "sess-1", "workspace")
	require.NoError(t, err)
	require.Equal(t, "workspace", stored)
}

func TestPGStore_DeleteTerminated(t *testing.T) {
	t.Parallel()
	store, mock, cleanup := newPGMock(t)
	defer cleanup()

	cronCutoff := time.UnixMilli(1000)
	defaultCutoff := time.UnixMilli(2000)

	selectQuery := store.queries["store.select_terminated_ids"]
	deleteQuery := store.queries["store.delete_terminated_by_id"]
	rows := sqlmock.NewRows([]string{
		"id", "user_id", "owner_id", "worker_session_id", "worker_type", "state", "bot_id", "bot_name",
		"platform", "platform_key_json", "work_dir", "title", "created_at", "updated_at",
		"expires_at", "idle_expires_at", "context_json", "source", "client_key", "workspace_id", "permission_ceiling",
	}).AddRow("sess-old", "u1", "u1", "ocs-old", "opencode_server", string(events.StateTerminated), "", "",
		"webchat", "", "", "", time.Now(), time.Now(), nil, nil, nil, "", "", "", "")
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(selectQuery)).
		WithArgs(string(events.StateTerminated), cronCutoff, defaultCutoff).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("sess-old"))
	mock.ExpectExec(regexp.QuoteMeta(`SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`)).
		WithArgs("sess-old").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta(deleteQuery)).
		WithArgs("sess-old", string(events.StateTerminated), cronCutoff, defaultCutoff).WillReturnRows(rows)
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO session_cleanup_tasks (id, session_id, worker_type, worker_session_id, attempts, next_attempt_at, lease_until, lease_token, last_error, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, NULL, NULL, $7, $8, $9) ON CONFLICT(session_id) DO NOTHING`)).
		WithArgs(sqlmock.AnyArg(), "sess-old", worker.TypeOpenCodeSrv, "ocs-old", 0, sqlmock.AnyArg(), "", sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	deleted, err := store.DeleteTerminated(context.Background(), cronCutoff, defaultCutoff)
	require.NoError(t, err)
	require.Len(t, deleted, 1)
	require.Equal(t, "ocs-old", deleted[0].WorkerSessionID)
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
