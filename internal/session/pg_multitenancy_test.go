package session

import (
	"context"
	"database/sql"
	"log/slog"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/dbutil"
	"github.com/hrygo/hotplex/internal/security"
)

// newPGMultitenancyMock creates a pgStore with all multitenancy queries pre-bound to PG dialect.
func newPGMultitenancyMock(t *testing.T) (*pgStore, sqlmock.Sqlmock, func()) {
	t.Helper()
	mockDB, mock, err := sqlmock.New()
	require.NoError(t, err)

	db := &dbutil.DB{DB: mockDB}
	d := dbutil.DialectPostgres

	q := map[string]string{
		"users.create":                        d.Rebind("INSERT INTO users (id, username, password_hash, role, display_name, status, created_at, updated_at, last_login_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULL)"),
		"users.get_by_id":                     d.Rebind("SELECT id, username, password_hash, role, display_name, status, created_at, updated_at, last_login_at FROM users WHERE id = ?"),
		"users.get_by_username":               d.Rebind("SELECT id, username, password_hash, role, display_name, status, created_at, updated_at, last_login_at FROM users WHERE username = ?"),
		"users.list":                          d.Rebind("SELECT id, username, password_hash, role, display_name, status, created_at, updated_at, last_login_at FROM users ORDER BY created_at ASC LIMIT ? OFFSET ?"),
		"users.update_status":                 d.Rebind("UPDATE users SET status = ?, updated_at = ? WHERE id = ?"),
		"users.touch_last_login":              d.Rebind("UPDATE users SET last_login_at = ?, updated_at = ? WHERE id = ?"),
		"workspaces.create":                   d.Rebind("INSERT INTO workspaces (id, owner_user_id, name, work_dir, agent_config_overrides, worker_preference, status, created_at, updated_at, permission_mode) VALUES (?, ?, ?, ?, NULL, NULL, 'active', ?, ?, ?)"),
		"workspaces.get_by_id":                d.Rebind("SELECT id, owner_user_id, name, work_dir, agent_config_overrides, worker_preference, status, created_at, updated_at, permission_mode FROM workspaces WHERE id = ?"),
		"workspaces.list_by_owner":            d.Rebind("SELECT id, owner_user_id, name, work_dir, agent_config_overrides, worker_preference, status, created_at, updated_at, permission_mode FROM workspaces WHERE owner_user_id = ? AND status = 'active' ORDER BY created_at ASC LIMIT ? OFFSET ?"),
		"workspaces.get_by_owner_and_workdir": d.Rebind("SELECT id, owner_user_id, name, work_dir, agent_config_overrides, worker_preference, status, created_at, updated_at, permission_mode FROM workspaces WHERE owner_user_id = ? AND work_dir = ? AND status = 'active'"),
		"workspaces.update":                   d.Rebind("UPDATE workspaces SET name = ?, agent_config_overrides = ?, worker_preference = ?, work_dir = ?, permission_mode = ?, updated_at = ? WHERE id = ? AND updated_at = ? AND (? = work_dir OR NOT EXISTS (SELECT 1 FROM sessions WHERE workspace_id = ? AND state IN ('created','running','idle')))"),
		"workspaces.delete":                   d.Rebind("DELETE FROM workspaces WHERE id = ?"),
		"workspaces.delete_if_empty":          d.Rebind("DELETE FROM workspaces WHERE id = ? AND NOT EXISTS (SELECT 1 FROM sessions WHERE workspace_id = ? AND state IN ('created','running','idle'))"),
		"workspaces.count_active_sessions":    d.Rebind("SELECT COUNT(*) FROM sessions WHERE workspace_id = ? AND state IN ('created','running','idle')"),
		"invitations.create":                  d.Rebind("INSERT INTO invitations (id, code, created_by, role, used_by, expires_at, created_at, used_at) VALUES (?, ?, ?, ?, NULL, ?, ?, NULL)"),
		"invitations.get_by_code":             d.Rebind("SELECT id, code, created_by, role, used_by, expires_at, created_at, used_at FROM invitations WHERE code = ?"),
		"invitations.mark_used":               d.Rebind("UPDATE invitations SET used_by = ?, used_at = ? WHERE id = ? AND used_by IS NULL"),
		"invitations.list":                    d.Rebind("SELECT id, code, created_by, role, used_by, expires_at, created_at, used_at FROM invitations ORDER BY created_at DESC"),
		"invitations.delete":                  d.Rebind("DELETE FROM invitations WHERE id = ?"),
	}

	store := &pgStore{db: db, dialect: d, queries: q, log: slog.Default()}
	return store, mock, func() {
		require.NoError(t, mock.ExpectationsWereMet())
		mockDB.Close()
	}
}

func userColumns() []string {
	return []string{"id", "username", "password_hash", "role", "display_name", "status", "created_at", "updated_at", "last_login_at"}
}

func workspaceColumns() []string {
	return []string{"id", "owner_user_id", "name", "work_dir", "agent_config_overrides", "worker_preference", "status", "created_at", "updated_at", "permission_mode"}
}

func invitationColumns() []string {
	return []string{"id", "code", "created_by", "role", "used_by", "expires_at", "created_at", "used_at"}
}

// --- PG users ---

func TestPGMultitenancy_CreateUser(t *testing.T) {
	t.Parallel()
	store, mock, cleanup := newPGMultitenancyMock(t)
	defer cleanup()
	q := store.queries["users.create"]
	mock.ExpectExec(regexp.QuoteMeta(q)).
		WithArgs("u-1", "alice", "hash", "admin", "Alice", "active", int64(100), int64(100)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	err := store.CreateUser(context.Background(), &security.User{ID: "u-1", Username: "alice", PasswordHash: "hash", Role: "admin", DisplayName: "Alice", Status: "active"}, 100)
	require.NoError(t, err)
}

func TestPGMultitenancy_GetUserByID(t *testing.T) {
	t.Parallel()
	store, mock, cleanup := newPGMultitenancyMock(t)
	defer cleanup()
	q := store.queries["users.get_by_id"]
	rows := sqlmock.NewRows(userColumns()).AddRow("u-1", "alice", "hash", "admin", "Alice", "active", int64(100), int64(100), nil)
	mock.ExpectQuery(regexp.QuoteMeta(q)).WithArgs("u-1").WillReturnRows(rows)
	u, err := store.GetUserByID(context.Background(), "u-1")
	require.NoError(t, err)
	require.Equal(t, "alice", u.Username)
}

func TestPGMultitenancy_GetUserByID_NotFound(t *testing.T) {
	t.Parallel()
	store, mock, cleanup := newPGMultitenancyMock(t)
	defer cleanup()
	q := store.queries["users.get_by_id"]
	mock.ExpectQuery(regexp.QuoteMeta(q)).WithArgs("nope").WillReturnError(sql.ErrNoRows)
	_, err := store.GetUserByID(context.Background(), "nope")
	require.ErrorIs(t, err, security.ErrUserNotFound)
}

func TestPGMultitenancy_GetUserByUsername(t *testing.T) {
	t.Parallel()
	store, mock, cleanup := newPGMultitenancyMock(t)
	defer cleanup()
	q := store.queries["users.get_by_username"]
	rows := sqlmock.NewRows(userColumns()).AddRow("u-1", "alice", "hash", "admin", "", "active", int64(100), int64(100), nil)
	mock.ExpectQuery(regexp.QuoteMeta(q)).WithArgs("alice").WillReturnRows(rows)
	u, err := store.GetUserByUsername(context.Background(), "alice")
	require.NoError(t, err)
	require.Equal(t, "u-1", u.ID)
}

func TestPGMultitenancy_GetUserByUsername_NotFound(t *testing.T) {
	t.Parallel()
	store, mock, cleanup := newPGMultitenancyMock(t)
	defer cleanup()
	q := store.queries["users.get_by_username"]
	mock.ExpectQuery(regexp.QuoteMeta(q)).WithArgs("nobody").WillReturnError(sql.ErrNoRows)
	_, err := store.GetUserByUsername(context.Background(), "nobody")
	require.ErrorIs(t, err, security.ErrUserNotFound)
}

func TestPGMultitenancy_ListUsers(t *testing.T) {
	t.Parallel()
	store, mock, cleanup := newPGMultitenancyMock(t)
	defer cleanup()
	q := store.queries["users.list"]
	rows := sqlmock.NewRows(userColumns()).
		AddRow("u-1", "alice", "hash", "admin", "", "active", int64(100), int64(100), nil).
		AddRow("u-2", "bob", "hash", "user", "", "active", int64(200), int64(200), nil)
	mock.ExpectQuery(regexp.QuoteMeta(q)).WithArgs(100, 0).WillReturnRows(rows)
	users, err := store.ListUsers(context.Background(), 100, 0)
	require.NoError(t, err)
	require.Len(t, users, 2)
}

func TestPGMultitenancy_ListUsers_Empty(t *testing.T) {
	t.Parallel()
	store, mock, cleanup := newPGMultitenancyMock(t)
	defer cleanup()
	q := store.queries["users.list"]
	mock.ExpectQuery(regexp.QuoteMeta(q)).WithArgs(1000, 0).WillReturnRows(sqlmock.NewRows(userColumns()))
	users, err := store.ListUsers(context.Background(), 0, 0) // limit<=0 → default 1000
	require.NoError(t, err)
	require.Empty(t, users)
}

func TestPGMultitenancy_UpdateUserStatus(t *testing.T) {
	t.Parallel()
	store, mock, cleanup := newPGMultitenancyMock(t)
	defer cleanup()
	q := store.queries["users.update_status"]
	mock.ExpectExec(regexp.QuoteMeta(q)).WithArgs("disabled", int64(200), "u-1").WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, store.UpdateUserStatus(context.Background(), "u-1", "disabled", 200))
}

func TestPGMultitenancy_TouchUserLastLogin(t *testing.T) {
	t.Parallel()
	store, mock, cleanup := newPGMultitenancyMock(t)
	defer cleanup()
	q := store.queries["users.touch_last_login"]
	mock.ExpectExec(regexp.QuoteMeta(q)).WithArgs(int64(300), int64(300), "u-1").WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, store.TouchUserLastLogin(context.Background(), "u-1", 300))
}

// --- PG workspaces ---

func TestPGMultitenancy_CreateWorkspace(t *testing.T) {
	t.Parallel()
	store, mock, cleanup := newPGMultitenancyMock(t)
	defer cleanup()
	q := store.queries["workspaces.create"]
	mock.ExpectExec(regexp.QuoteMeta(q)).
		WithArgs("ws-1", "u-1", "proj", "/tmp/p", int64(100), int64(100), nil).
		WillReturnResult(sqlmock.NewResult(0, 1))
	err := store.CreateWorkspace(context.Background(), &Workspace{ID: "ws-1", OwnerUserID: "u-1", Name: "proj", WorkDir: "/tmp/p"}, 100)
	require.NoError(t, err)
}

func TestPGMultitenancy_GetWorkspaceByID(t *testing.T) {
	t.Parallel()
	store, mock, cleanup := newPGMultitenancyMock(t)
	defer cleanup()
	q := store.queries["workspaces.get_by_id"]
	rows := sqlmock.NewRows(workspaceColumns()).AddRow("ws-1", "u-1", "proj", "/tmp/p", nil, nil, "active", int64(100), int64(100), nil)
	mock.ExpectQuery(regexp.QuoteMeta(q)).WithArgs("ws-1").WillReturnRows(rows)
	w, err := store.GetWorkspaceByID(context.Background(), "ws-1")
	require.NoError(t, err)
	require.Equal(t, "proj", w.Name)
}

func TestPGMultitenancy_GetWorkspaceByID_NotFound(t *testing.T) {
	t.Parallel()
	store, mock, cleanup := newPGMultitenancyMock(t)
	defer cleanup()
	q := store.queries["workspaces.get_by_id"]
	mock.ExpectQuery(regexp.QuoteMeta(q)).WithArgs("nope").WillReturnError(sql.ErrNoRows)
	_, err := store.GetWorkspaceByID(context.Background(), "nope")
	require.ErrorIs(t, err, ErrWorkspaceNotFound)
}

func TestPGMultitenancy_ListWorkspacesByOwner(t *testing.T) {
	t.Parallel()
	store, mock, cleanup := newPGMultitenancyMock(t)
	defer cleanup()
	q := store.queries["workspaces.list_by_owner"]
	rows := sqlmock.NewRows(workspaceColumns()).
		AddRow("ws-1", "u-1", "a", "/tmp/a", nil, nil, "active", int64(100), int64(100), nil)
	mock.ExpectQuery(regexp.QuoteMeta(q)).WithArgs("u-1", 100, 0).WillReturnRows(rows)
	list, err := store.ListWorkspacesByOwner(context.Background(), "u-1", 100, 0)
	require.NoError(t, err)
	require.Len(t, list, 1)
}

func TestPGMultitenancy_GetWorkspaceByOwnerAndWorkDir(t *testing.T) {
	t.Parallel()
	store, mock, cleanup := newPGMultitenancyMock(t)
	defer cleanup()
	q := store.queries["workspaces.get_by_owner_and_workdir"]
	rows := sqlmock.NewRows(workspaceColumns()).AddRow("ws-1", "u-1", "p", "/tmp/x", nil, nil, "active", int64(100), int64(100), nil)
	mock.ExpectQuery(regexp.QuoteMeta(q)).WithArgs("u-1", "/tmp/x").WillReturnRows(rows)
	w, err := store.GetWorkspaceByOwnerAndWorkDir(context.Background(), "u-1", "/tmp/x")
	require.NoError(t, err)
	require.Equal(t, "ws-1", w.ID)
}

func TestPGMultitenancy_GetWorkspaceByOwnerAndWorkDir_NotFound(t *testing.T) {
	t.Parallel()
	store, mock, cleanup := newPGMultitenancyMock(t)
	defer cleanup()
	q := store.queries["workspaces.get_by_owner_and_workdir"]
	mock.ExpectQuery(regexp.QuoteMeta(q)).WithArgs("u-1", "/nope").WillReturnError(sql.ErrNoRows)
	_, err := store.GetWorkspaceByOwnerAndWorkDir(context.Background(), "u-1", "/nope")
	require.ErrorIs(t, err, ErrWorkspaceNotFound)
}

func TestPGMultitenancy_UpdateWorkspace(t *testing.T) {
	t.Parallel()
	store, mock, cleanup := newPGMultitenancyMock(t)
	defer cleanup()
	q := store.queries["workspaces.update"]
	// ws.UpdatedAt=100 is the cached version from a prior Get (handler pattern);
	// it binds the CAS WHERE clause. The trailing "" and "ws-1" bind the TOCTOU
	// guard (? = work_dir compare, workspace_id for NOT EXISTS) — work_dir is "",
	// so the compare matches and the row updates regardless of sessions.
	mock.ExpectExec(regexp.QuoteMeta(q)).
		WithArgs("newname", nil, nil, "", nil, int64(200), "ws-1", int64(100), "", "ws-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	ws := &Workspace{ID: "ws-1", Name: "newname", UpdatedAt: 100}
	err := store.UpdateWorkspace(context.Background(), ws, 200)
	require.NoError(t, err)
	require.Equal(t, int64(200), ws.UpdatedAt, "CAS success bumps in-memory updated_at")
}

// TestPGMultitenancy_UpdateWorkspace_VersionConflict: RowsAffected==0 with zero
// active sessions surfaces as ErrWorkspaceConflict (review P3-1, mirrors SQLite).
func TestPGMultitenancy_UpdateWorkspace_VersionConflict(t *testing.T) {
	t.Parallel()
	store, mock, cleanup := newPGMultitenancyMock(t)
	defer cleanup()
	q := store.queries["workspaces.update"]
	mock.ExpectExec(regexp.QuoteMeta(q)).
		WithArgs("by-b", nil, nil, "", nil, int64(300), "ws-1", int64(100), "", "ws-1"). // stale cached updated_at
		WillReturnResult(sqlmock.NewResult(0, 0))                                        // 0 rows → disambiguate
	mock.ExpectQuery(regexp.QuoteMeta(store.queries["workspaces.count_active_sessions"])).
		WithArgs("ws-1").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0)) // no active session → conflict, not NotEmpty
	ws := &Workspace{ID: "ws-1", Name: "by-b", UpdatedAt: 100}
	require.ErrorIs(t, store.UpdateWorkspace(context.Background(), ws, 300), ErrWorkspaceConflict)
}

// TestPGMultitenancy_UpdateWorkspace_WorkDirChangeBlockedByActiveSession: the SQL
// TOCTOU guard rejects a work_dir change when an active session exists, surfacing
// as ErrWorkspaceNotEmpty (review P1-1, mirrors SQLite).
func TestPGMultitenancy_UpdateWorkspace_WorkDirChangeBlockedByActiveSession(t *testing.T) {
	t.Parallel()
	store, mock, cleanup := newPGMultitenancyMock(t)
	defer cleanup()
	q := store.queries["workspaces.update"]
	mock.ExpectExec(regexp.QuoteMeta(q)).
		WithArgs("n", nil, nil, "/new/dir", nil, int64(300), "ws-1", int64(100), "/new/dir", "ws-1").
		WillReturnResult(sqlmock.NewResult(0, 0)) // guard blocked: 0 rows
	mock.ExpectQuery(regexp.QuoteMeta(store.queries["workspaces.count_active_sessions"])).
		WithArgs("ws-1").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1)) // 1 active session → NotEmpty
	ws := &Workspace{ID: "ws-1", Name: "n", WorkDir: "/new/dir", UpdatedAt: 100}
	require.ErrorIs(t, store.UpdateWorkspace(context.Background(), ws, 300), ErrWorkspaceNotEmpty)
}

func TestPGMultitenancy_DeleteWorkspace(t *testing.T) {
	t.Parallel()
	store, mock, cleanup := newPGMultitenancyMock(t)
	defer cleanup()
	q := store.queries["workspaces.delete"]
	mock.ExpectExec(regexp.QuoteMeta(q)).WithArgs("ws-1").WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, store.DeleteWorkspace(context.Background(), "ws-1"))
}

func TestPGMultitenancy_CountActiveSessionsInWorkspace(t *testing.T) {
	t.Parallel()
	store, mock, cleanup := newPGMultitenancyMock(t)
	defer cleanup()
	q := store.queries["workspaces.count_active_sessions"]
	rows := sqlmock.NewRows([]string{"count"}).AddRow(3)
	mock.ExpectQuery(regexp.QuoteMeta(q)).WithArgs("ws-1").WillReturnRows(rows)
	n, err := store.CountActiveSessionsInWorkspace(context.Background(), "ws-1")
	require.NoError(t, err)
	require.Equal(t, 3, n)
}

// DeleteWorkspaceIfEmpty 三态：成功删除 / 有活跃会话（409）/ 已被并发删除（404）。
// PG 版必须与 SQLite 版行为一致（PR #773 reviewer P1）。

func TestPGMultitenancy_DeleteWorkspaceIfEmpty(t *testing.T) {
	t.Parallel()
	store, mock, cleanup := newPGMultitenancyMock(t)
	defer cleanup()
	q := store.queries["workspaces.delete_if_empty"]
	mock.ExpectExec(regexp.QuoteMeta(q)).WithArgs("ws-1", "ws-1").WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, store.DeleteWorkspaceIfEmpty(context.Background(), "ws-1"))
}

func TestPGMultitenancy_DeleteWorkspaceIfEmpty_NotEmpty(t *testing.T) {
	t.Parallel()
	store, mock, cleanup := newPGMultitenancyMock(t)
	defer cleanup()
	q := store.queries["workspaces.delete_if_empty"]
	mock.ExpectExec(regexp.QuoteMeta(q)).WithArgs("ws-1", "ws-1").WillReturnResult(sqlmock.NewResult(0, 0))
	// RowsAffected==0 → 重新检查存在性：workspace 仍存在 → 有活跃会话阻塞 → ErrWorkspaceNotEmpty。
	getQ := store.queries["workspaces.get_by_id"]
	rows := sqlmock.NewRows(workspaceColumns()).AddRow("ws-1", "u-1", "p", "/tmp/x", nil, nil, "active", int64(100), int64(100), nil)
	mock.ExpectQuery(regexp.QuoteMeta(getQ)).WithArgs("ws-1").WillReturnRows(rows)
	require.ErrorIs(t, store.DeleteWorkspaceIfEmpty(context.Background(), "ws-1"), ErrWorkspaceNotEmpty)
}

func TestPGMultitenancy_DeleteWorkspaceIfEmpty_AlreadyDeletedReturnsNotFound(t *testing.T) {
	t.Parallel()
	store, mock, cleanup := newPGMultitenancyMock(t)
	defer cleanup()
	q := store.queries["workspaces.delete_if_empty"]
	mock.ExpectExec(regexp.QuoteMeta(q)).WithArgs("ws-1", "ws-1").WillReturnResult(sqlmock.NewResult(0, 0))
	// RowsAffected==0 → 重新检查存在性：workspace 已被并发 actor 删除 → ErrWorkspaceNotFound。
	// 修复前 PG 版一律返回 ErrWorkspaceNotEmpty（误导 409），handler 的 404 分支不可达。
	getQ := store.queries["workspaces.get_by_id"]
	mock.ExpectQuery(regexp.QuoteMeta(getQ)).WithArgs("ws-1").WillReturnError(sql.ErrNoRows)
	require.ErrorIs(t, store.DeleteWorkspaceIfEmpty(context.Background(), "ws-1"), ErrWorkspaceNotFound)
}

// --- PG invitations ---

func TestPGMultitenancy_CreateInvitation(t *testing.T) {
	t.Parallel()
	store, mock, cleanup := newPGMultitenancyMock(t)
	defer cleanup()
	q := store.queries["invitations.create"]
	mock.ExpectExec(regexp.QuoteMeta(q)).
		WithArgs("inv-1", "CODE", "admin-1", "user", int64(200), int64(100)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	err := store.CreateInvitation(context.Background(), &Invitation{ID: "inv-1", Code: "CODE", CreatedBy: "admin-1", Role: "user", ExpiresAt: 200}, 100)
	require.NoError(t, err)
}

func TestPGMultitenancy_GetInvitationByCode(t *testing.T) {
	t.Parallel()
	store, mock, cleanup := newPGMultitenancyMock(t)
	defer cleanup()
	q := store.queries["invitations.get_by_code"]
	rows := sqlmock.NewRows(invitationColumns()).AddRow("inv-1", "CODE", "admin-1", "user", nil, int64(200), int64(100), nil)
	mock.ExpectQuery(regexp.QuoteMeta(q)).WithArgs("CODE").WillReturnRows(rows)
	inv, err := store.GetInvitationByCode(context.Background(), "CODE")
	require.NoError(t, err)
	require.Equal(t, "inv-1", inv.ID)
}

func TestPGMultitenancy_GetInvitationByCode_NotFound(t *testing.T) {
	t.Parallel()
	store, mock, cleanup := newPGMultitenancyMock(t)
	defer cleanup()
	q := store.queries["invitations.get_by_code"]
	mock.ExpectQuery(regexp.QuoteMeta(q)).WithArgs("NOPE").WillReturnError(sql.ErrNoRows)
	_, err := store.GetInvitationByCode(context.Background(), "NOPE")
	require.ErrorIs(t, err, ErrInvitationNotFound)
}

func TestPGMultitenancy_MarkInvitationUsed(t *testing.T) {
	t.Parallel()
	store, mock, cleanup := newPGMultitenancyMock(t)
	defer cleanup()
	q := store.queries["invitations.mark_used"]
	mock.ExpectExec(regexp.QuoteMeta(q)).WithArgs("u-1", int64(200), "inv-1").WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, store.MarkInvitationUsed(context.Background(), "inv-1", "u-1", 200))
}

func TestPGMultitenancy_MarkInvitationUsed_AlreadyUsed(t *testing.T) {
	t.Parallel()
	store, mock, cleanup := newPGMultitenancyMock(t)
	defer cleanup()
	q := store.queries["invitations.mark_used"]
	mock.ExpectExec(regexp.QuoteMeta(q)).WithArgs("u-2", int64(300), "inv-1").WillReturnResult(sqlmock.NewResult(0, 0))
	err := store.MarkInvitationUsed(context.Background(), "inv-1", "u-2", 300)
	require.ErrorIs(t, err, ErrInvitationAlreadyUsed)
}

func TestPGMultitenancy_ListInvitations(t *testing.T) {
	t.Parallel()
	store, mock, cleanup := newPGMultitenancyMock(t)
	defer cleanup()
	q := store.queries["invitations.list"]
	rows := sqlmock.NewRows(invitationColumns()).
		AddRow("inv-1", "C1", "admin", "user", nil, int64(200), int64(100), nil).
		AddRow("inv-2", "C2", "admin", "user", nil, int64(300), int64(150), nil)
	mock.ExpectQuery(regexp.QuoteMeta(q)).WillReturnRows(rows)
	list, err := store.ListInvitations(context.Background(), 100, 0)
	require.NoError(t, err)
	require.Len(t, list, 2)
}

func TestPGMultitenancy_DeleteInvitation(t *testing.T) {
	t.Parallel()
	store, mock, cleanup := newPGMultitenancyMock(t)
	defer cleanup()
	q := store.queries["invitations.delete"]
	mock.ExpectExec(regexp.QuoteMeta(q)).WithArgs("inv-1").WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, store.DeleteInvitation(context.Background(), "inv-1"))
}
