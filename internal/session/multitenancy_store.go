package session

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/hrygo/hotplex/internal/security"
)

// Workspace is a per-user named project directory (spec §6.2).
// work_dir is workspace-mutable via Update (workspaces.update.sql guards the
// change against active sessions — see UpdateWorkspace), but immutable for any
// bound session's lifetime since it enters DeriveSessionKey (spec §7).
type Workspace struct {
	ID                   string `json:"id"`
	OwnerUserID          string `json:"owner_user_id"`
	Name                 string `json:"name"`
	WorkDir              string `json:"work_dir"`
	AgentConfigOverrides string `json:"agent_config_overrides"` // JSON value; spec ② fills, spec ① stays empty
	WorkerPreference     string `json:"worker_preference"`      // spec ③ fills
	PermissionMode       string `json:"permission_mode"`        // issue #789: read-only|workspace|auto-edit|bypass; "" = "worker default" (NULL scans to ""; bridge injects only explicit overrides)
	Status               string `json:"status"`
	CreatedAt            int64  `json:"created_at"`
	UpdatedAt            int64  `json:"updated_at"`
}

// AdminWorkspaceView is the admin console projection of a workspace with the
// owner's readable identity joined in (spec §3.1, issue #807). It embeds Workspace
// so the full row (id/owner/name/work_dir/permission_mode/...) serializes as-is,
// plus OwnerDisplayName/OwnerUsername so an admin scanning the global list can
// identify ownership instead of staring at a raw owner_user_id UUID.
type AdminWorkspaceView struct {
	Workspace
	OwnerDisplayName string `json:"owner_display_name"`
	OwnerUsername    string `json:"owner_username"`
}

// Invitation is a one-time invite code (spec §6.3).
// json tags are required: AdminListInvitations responds with the struct
// directly, so without them the field names serialize as PascalCase and the
// snake_case frontend (auth.ts Invitation) reads undefined for every field —
// including id, which made revoke hit DELETE /invitations/undefined (PR #762
// review P0).
type Invitation struct {
	ID        string  `json:"id"`
	Code      string  `json:"code"`
	CreatedBy string  `json:"created_by"`
	Role      string  `json:"role"`
	UsedBy    *string `json:"used_by,omitempty"` // nil = unused
	ExpiresAt int64   `json:"expires_at"`
	CreatedAt int64   `json:"created_at,omitempty"`
	UsedAt    *int64  `json:"used_at,omitempty"` // nil = unused
}

// UserIdentity binds an OAuth/OIDC identity to a local user (spec ④).
// One user may have multiple identities (different IdPs); each (provider, subject)
// pair uniquely maps to a single user_id via UNIQUE constraint.
type UserIdentity struct {
	ID          string // UUID
	UserID      string // FK → users.id
	Provider    string // provider name (config key)
	Subject     string // IdP subject (OIDC "sub" claim)
	DisplayName string // synced from IdP
	Email       string // synced from IdP (not used for auto-merge)
	CreatedAt   int64
	UpdatedAt   int64
}

// IdentityUserResult is the atomic result of resolving an OAuth/OIDC identity
// to a local user during SSO login.
type IdentityUserResult struct {
	User     *security.User
	Identity *UserIdentity
	Created  bool // true when this call created the identity binding
}

// Multitenancy store sentinels.
var (
	ErrWorkspaceNotFound     = errors.New("session: workspace not found")
	ErrWorkspaceNotEmpty     = errors.New("session: workspace has active sessions")
	ErrWorkspaceConflict     = errors.New("session: workspace version conflict (concurrent update)")
	ErrInvitationNotFound    = errors.New("session: invitation not found")
	ErrInvitationAlreadyUsed = errors.New("session: invitation already used")
	ErrIdentityNotFound      = errors.New("session: user identity not found")
)

// UserWorkspaceStore is the store capability surface used by gateway auth/workspace
// handlers. SQLiteStore and pgStore both satisfy it. Embeds security.UserStore so
// LocalAccountProvider can be wired with the same store.
type UserWorkspaceStore interface {
	security.UserStore
	// users
	HasAdmin(ctx context.Context) (bool, error)
	ListUsers(ctx context.Context, limit, offset int) ([]*security.User, error)
	UpdateUserStatus(ctx context.Context, id, status string, now int64) error
	UpdateUserPassword(ctx context.Context, id, passwordHash string, now int64) error
	TouchUserLastLogin(ctx context.Context, userID string, now int64) error
	// workspaces
	CreateWorkspace(ctx context.Context, w *Workspace, now int64) error
	GetWorkspaceByID(ctx context.Context, id string) (*Workspace, error)
	ListWorkspacesByOwner(ctx context.Context, ownerUserID string, limit, offset int) ([]*Workspace, error)
	ListAllWorkspaces(ctx context.Context) ([]*Workspace, error)
	// ListAllWorkspacesWithOwner returns all active workspaces joined with owner
	// identity (display_name + username) for the admin console global view (issue
	// #807). Distinct from ListAllWorkspaces (bare rows for the startup scan).
	ListAllWorkspacesWithOwner(ctx context.Context) ([]*AdminWorkspaceView, error)
	GetWorkspaceByOwnerAndWorkDir(ctx context.Context, ownerUserID, workDir string) (*Workspace, error)
	UpdateWorkspace(ctx context.Context, w *Workspace, now int64) error
	DeleteWorkspace(ctx context.Context, id string) error
	// DeleteWorkspaceIfEmpty 原子删除：仅当无活跃会话时成功，防 Count↔Delete TOCTOU（spec §9.1）。
	// 返回 ErrWorkspaceNotEmpty 若期间有新活跃会话。
	DeleteWorkspaceIfEmpty(ctx context.Context, id string) error
	CountActiveSessionsInWorkspace(ctx context.Context, workspaceID string) (int, error)
	// invitations
	CreateInvitation(ctx context.Context, inv *Invitation, now int64) error
	GetInvitationByCode(ctx context.Context, code string) (*Invitation, error)
	MarkInvitationUsed(ctx context.Context, id, usedBy string, now int64) error
	SetInvitationUsedBy(ctx context.Context, id, oldUsedBy, newUsedBy string) error
	ListInvitations(ctx context.Context, limit, offset int) ([]*Invitation, error)
	DeleteInvitation(ctx context.Context, id string) error
	// user identities (spec ④ SSO)
	GetUserIdentityByProviderSubject(ctx context.Context, provider, subject string) (*UserIdentity, error)
	GetOrCreateUserByIdentity(ctx context.Context, provider, subject, username, displayName, email, userID, identityID string, now int64) (*IdentityUserResult, error)
	CreateUserIdentity(ctx context.Context, id *UserIdentity, now int64) error
	UpdateUserIdentityProfile(ctx context.Context, id, displayName, email string, now int64) error
}

// Compile-time assertions that both stores satisfy UserWorkspaceStore.
var (
	_ UserWorkspaceStore = (*SQLiteStore)(nil)
	_ UserWorkspaceStore = (*pgStore)(nil)
)

// nullableString returns "" as SQL NULL (for optional JSON/preference columns).
func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// --- scanners ---

// distinctNonEmptyIDs returns de-duplicated, non-empty ids, preserving order.
// Empty strings are skipped so an IN (...) clause never matches the empty row.
func distinctNonEmptyIDs(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// usersByIDSQL builds `SELECT ... FROM users WHERE id IN (...)` with `?`
// placeholders plus its args. SQLite uses it verbatim; PG rebinds via
// dialect.Rebind. Column order mirrors users.list.sql so scanUser applies.
func usersByIDSQL(ids []string) (string, []any) {
	placeholders := strings.TrimRight(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return "SELECT id, username, password_hash, role, display_name, status, created_at, updated_at, last_login_at FROM users WHERE id IN (" + placeholders + ")", args
}

func scanUser(sc rowScanner) (*security.User, error) {
	var u security.User
	var lastLogin sql.NullInt64 // last_login_at 可空（用户从未登录时为 NULL）
	err := sc.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.DisplayName, &u.Status,
		&u.CreatedAt, &u.UpdatedAt, &lastLogin)
	if err != nil {
		return nil, err
	}
	u.LastLoginAt = lastLogin.Int64 // NULL → 0
	return &u, nil
}

func scanIdentity(sc rowScanner) (*UserIdentity, error) {
	var id UserIdentity
	err := sc.Scan(&id.ID, &id.UserID, &id.Provider, &id.Subject, &id.DisplayName,
		&id.Email, &id.CreatedAt, &id.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &id, nil
}

func scanWorkspace(sc rowScanner) (*Workspace, error) {
	var w Workspace
	var overrides, pref, perm sql.NullString
	var createdAt, updatedAt sql.NullInt64
	err := sc.Scan(&w.ID, &w.OwnerUserID, &w.Name, &w.WorkDir, &overrides, &pref, &w.Status,
		&createdAt, &updatedAt, &perm)
	if err != nil {
		return nil, err
	}
	w.AgentConfigOverrides = overrides.String
	w.WorkerPreference = pref.String
	w.PermissionMode = perm.String
	w.CreatedAt = createdAt.Int64
	w.UpdatedAt = updatedAt.Int64
	return &w, nil
}

// scanAdminWorkspace scans the workspaces.list_with_owner projection: the 10
// Workspace columns followed by the two LEFT-JOIN'd owner columns (COALESCE'd to
// ” in SQL, so plain string — no NullString). Mirrors scanWorkspace's column
// order for the embedded fields (see workspaces.list_with_owner.sql).
func scanAdminWorkspace(sc rowScanner) (*AdminWorkspaceView, error) {
	var v AdminWorkspaceView
	var overrides, pref, perm sql.NullString
	var createdAt, updatedAt sql.NullInt64
	err := sc.Scan(&v.ID, &v.OwnerUserID, &v.Name, &v.WorkDir, &overrides, &pref, &v.Status,
		&createdAt, &updatedAt, &perm, &v.OwnerDisplayName, &v.OwnerUsername)
	if err != nil {
		return nil, err
	}
	v.AgentConfigOverrides = overrides.String
	v.WorkerPreference = pref.String
	v.PermissionMode = perm.String
	v.CreatedAt = createdAt.Int64
	v.UpdatedAt = updatedAt.Int64
	return &v, nil
}

func scanInvitation(sc rowScanner) (*Invitation, error) {
	var inv Invitation
	var usedBy sql.NullString
	var createdAt, usedAt sql.NullInt64
	err := sc.Scan(&inv.ID, &inv.Code, &inv.CreatedBy, &inv.Role, &usedBy, &inv.ExpiresAt, &createdAt, &usedAt)
	if err != nil {
		return nil, err
	}
	if usedBy.Valid {
		inv.UsedBy = &usedBy.String
	}
	if usedAt.Valid {
		v := usedAt.Int64
		inv.UsedAt = &v
	}
	inv.CreatedAt = createdAt.Int64
	return &inv, nil
}

// --- SQLiteStore: users ---

func (s *SQLiteStore) CreateUser(ctx context.Context, u *security.User, now int64) error {
	return s.writeMu.WithLock(func() error {
		_, err := s.db.ExecContext(ctx, queries["users.create"],
			u.ID, u.Username, u.PasswordHash, u.Role, u.DisplayName, u.Status, now, now)
		return err
	})
}

func (s *SQLiteStore) GetUserByID(ctx context.Context, id string) (*security.User, error) {
	u, err := scanUser(s.db.QueryRowContext(ctx, queries["users.get_by_id"], id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, security.ErrUserNotFound
	}
	return u, err
}

func (s *SQLiteStore) GetUserByUsername(ctx context.Context, username string) (*security.User, error) {
	u, err := scanUser(s.db.QueryRowContext(ctx, queries["users.get_by_username"], username))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, security.ErrUserNotFound
	}
	return u, err
}

func (s *SQLiteStore) ListUsers(ctx context.Context, limit, offset int) ([]*security.User, error) {
	if limit <= 0 {
		limit = 1000
	}
	rows, err := s.db.QueryContext(ctx, queries["users.list"], limit, offset)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []*security.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) ListByIDs(ctx context.Context, ids []string) (map[string]*security.User, error) {
	out := make(map[string]*security.User)
	distinct := distinctNonEmptyIDs(ids)
	if len(distinct) == 0 {
		return out, nil
	}
	query, args := usersByIDSQL(distinct)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out[u.ID] = u
	}
	return out, rows.Err()
}

func (s *SQLiteStore) UpdateUserStatus(ctx context.Context, id, status string, now int64) error {
	return s.writeMu.WithLock(func() error {
		_, err := s.db.ExecContext(ctx, queries["users.update_status"], status, now, id)
		return err
	})
}

func (s *SQLiteStore) UpdateUserPassword(ctx context.Context, id, passwordHash string, now int64) error {
	return s.writeMu.WithLock(func() error {
		_, err := s.db.ExecContext(ctx, queries["users.update_password"], passwordHash, now, id)
		return err
	})
}

func (s *SQLiteStore) TouchUserLastLogin(ctx context.Context, userID string, now int64) error {
	return s.writeMu.WithLock(func() error {
		_, err := s.db.ExecContext(ctx, queries["users.touch_last_login"], now, now, userID)
		return err
	})
}

func (s *SQLiteStore) HasAdmin(ctx context.Context) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx, queries["users.has_admin"]).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("has admin: %w", err)
	}
	return true, nil
}

// --- SQLiteStore: workspaces ---

func (s *SQLiteStore) CreateWorkspace(ctx context.Context, w *Workspace, now int64) error {
	return s.writeMu.WithLock(func() error {
		_, err := s.db.ExecContext(ctx, queries["workspaces.create"],
			w.ID, w.OwnerUserID, w.Name, w.WorkDir, now, now, nullableString(w.PermissionMode))
		return err
	})
}

func (s *SQLiteStore) GetWorkspaceByID(ctx context.Context, id string) (*Workspace, error) {
	w, err := scanWorkspace(s.db.QueryRowContext(ctx, queries["workspaces.get_by_id"], id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrWorkspaceNotFound
	}
	return w, err
}

func (s *SQLiteStore) ListWorkspacesByOwner(ctx context.Context, ownerUserID string, limit, offset int) ([]*Workspace, error) {
	rows, err := s.db.QueryContext(ctx, queries["workspaces.list_by_owner"], ownerUserID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []*Workspace
	for rows.Next() {
		w, err := scanWorkspace(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// ListAllWorkspaces returns all active workspaces regardless of owner. Used by the
// gateway startup scan to detect stale/invalid agent_config_overrides (spec ② #749).
func (s *SQLiteStore) ListAllWorkspaces(ctx context.Context) ([]*Workspace, error) {
	rows, err := s.db.QueryContext(ctx, queries["workspaces.list_all"])
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []*Workspace
	for rows.Next() {
		w, err := scanWorkspace(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// ListAllWorkspacesWithOwner returns all active workspaces joined with the owner's
// readable identity (display_name + username) for the admin console global view
// (spec §3.1, issue #807). Distinct from ListAllWorkspaces: that returns bare rows
// for the gateway startup stale-override scan; this carries owner identity so an
// admin doesn't face raw UUIDs.
func (s *SQLiteStore) ListAllWorkspacesWithOwner(ctx context.Context) ([]*AdminWorkspaceView, error) {
	rows, err := s.db.QueryContext(ctx, queries["workspaces.list_with_owner"])
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []*AdminWorkspaceView
	for rows.Next() {
		v, err := scanAdminWorkspace(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) GetWorkspaceByOwnerAndWorkDir(ctx context.Context, ownerUserID, workDir string) (*Workspace, error) {
	w, err := scanWorkspace(s.db.QueryRowContext(ctx, queries["workspaces.get_by_owner_and_workdir"], ownerUserID, workDir))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrWorkspaceNotFound
	}
	return w, err
}

// UpdateWorkspace applies the in-memory workspace fields to the row, guarded by
// an optimistic-concurrency CAS on updated_at (review P3-1): the WHERE clause
// binds the caller's cached updated_at, so a concurrent update that bumped it
// makes this affect 0 rows → ErrWorkspaceConflict. The writeMu scope keeps the
// read-modify-write atomic against other SQLiteStore writers; PG relies on MVCC.
//
// The SQL also atomically rejects work_dir changes while active sessions exist
// (review P1-1: closes the Count→Update TOCTOU the handler's pre-check alone
// can't, since a concurrent session insert doesn't bump updated_at). RowsAffected==0
// is disambiguated into ErrWorkspaceNotEmpty (work_dir change blocked by an
// active session) vs ErrWorkspaceConflict (CAS loss / row gone), mirroring
// DeleteWorkspaceIfEmpty.
func (s *SQLiteStore) UpdateWorkspace(ctx context.Context, w *Workspace, now int64) error {
	return s.writeMu.WithLock(func() error {
		res, err := s.db.ExecContext(ctx, queries["workspaces.update"],
			w.Name, nullableString(w.AgentConfigOverrides), nullableString(w.WorkerPreference), w.WorkDir, nullableString(w.PermissionMode), now,
			w.ID, w.UpdatedAt, w.WorkDir, w.ID)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			cnt, qerr := s.CountActiveSessionsInWorkspace(ctx, w.ID)
			if qerr != nil {
				return qerr
			}
			if cnt > 0 {
				return ErrWorkspaceNotEmpty
			}
			return ErrWorkspaceConflict
		}
		w.UpdatedAt = now
		return nil
	})
}

func (s *SQLiteStore) DeleteWorkspace(ctx context.Context, id string) error {
	return s.writeMu.WithLock(func() error {
		_, err := s.db.ExecContext(ctx, queries["workspaces.delete"], id)
		return err
	})
}

// DeleteWorkspaceIfEmpty 原子删除：仅当无活跃会话时成功（防 TOCTOU，spec §9.1）。
//
// RowsAffected==0 有两种成因，必须区分（review P3）：
//   - workspace 存在但有活跃会话 → ErrWorkspaceNotEmpty（HTTP 409）
//   - workspace 已被并发 actor 删除 → ErrWorkspaceNotFound（HTTP 404）
//
// 重新检查在 writeMu 内进行，关闭 Get↔Delete 之间的 TOCTOU 窗口。
func (s *SQLiteStore) DeleteWorkspaceIfEmpty(ctx context.Context, id string) error {
	return s.writeMu.WithLock(func() error {
		res, err := s.db.ExecContext(ctx, queries["workspaces.delete_if_empty"], id, id)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			if _, err := s.GetWorkspaceByID(ctx, id); err != nil {
				if errors.Is(err, ErrWorkspaceNotFound) {
					return ErrWorkspaceNotFound
				}
				return err
			}
			return ErrWorkspaceNotEmpty
		}
		return nil
	})
}

func (s *SQLiteStore) CountActiveSessionsInWorkspace(ctx context.Context, workspaceID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, queries["workspaces.count_active_sessions"], workspaceID).Scan(&n)
	return n, err
}

// --- SQLiteStore: invitations ---

func (s *SQLiteStore) CreateInvitation(ctx context.Context, inv *Invitation, now int64) error {
	return s.writeMu.WithLock(func() error {
		_, err := s.db.ExecContext(ctx, queries["invitations.create"],
			inv.ID, inv.Code, inv.CreatedBy, inv.Role, inv.ExpiresAt, now)
		return err
	})
}

func (s *SQLiteStore) GetInvitationByCode(ctx context.Context, code string) (*Invitation, error) {
	inv, err := scanInvitation(s.db.QueryRowContext(ctx, queries["invitations.get_by_code"], code))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrInvitationNotFound
	}
	return inv, err
}

func (s *SQLiteStore) MarkInvitationUsed(ctx context.Context, id, usedBy string, now int64) error {
	return s.writeMu.WithLock(func() error {
		res, err := s.db.ExecContext(ctx, queries["invitations.mark_used"], usedBy, now, id)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return ErrInvitationAlreadyUsed
		}
		return nil
	})
}

// SetInvitationUsedBy 将 CAS 消费时的占位 used_by（inv.CreatedBy）更新为真实接受者。
// AcceptInvite 在用户创建成功后调用（uid 已存在，满足 FK）。
func (s *SQLiteStore) SetInvitationUsedBy(ctx context.Context, id, oldUsedBy, newUsedBy string) error {
	return s.writeMu.WithLock(func() error {
		_, err := s.db.ExecContext(ctx, queries["invitations.set_used_by"], newUsedBy, id, oldUsedBy)
		return err
	})
}

func (s *SQLiteStore) ListInvitations(ctx context.Context, limit, offset int) ([]*Invitation, error) {
	if limit <= 0 {
		limit = 1000
	}
	rows, err := s.db.QueryContext(ctx, queries["invitations.list"], limit, offset)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []*Invitation
	for rows.Next() {
		inv, err := scanInvitation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) DeleteInvitation(ctx context.Context, id string) error {
	return s.writeMu.WithLock(func() error {
		_, err := s.db.ExecContext(ctx, queries["invitations.delete"], id)
		return err
	})
}

// --- SQLiteStore: user identities (spec ④) ---

func (s *SQLiteStore) GetUserIdentityByProviderSubject(ctx context.Context, provider, subject string) (*UserIdentity, error) {
	id, err := scanIdentity(s.db.QueryRowContext(ctx, queries["identities.get_by_provider_subject"], provider, subject))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrIdentityNotFound
	}
	return id, err
}

func (s *SQLiteStore) GetOrCreateUserByIdentity(ctx context.Context, provider, subject, username, displayName, email, userID, identityID string, now int64) (*IdentityUserResult, error) {
	var result *IdentityUserResult
	err := s.writeMu.WithLock(func() error {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()

		result, err = getOrCreateUserByIdentityTx(ctx, tx, provider, subject, username, displayName, email, userID, identityID, now, sqliteIdentitySQL{})
		if err != nil {
			return err
		}
		return tx.Commit()
	})
	return result, err
}

func (s *SQLiteStore) CreateUserIdentity(ctx context.Context, id *UserIdentity, now int64) error {
	return s.writeMu.WithLock(func() error {
		_, err := s.db.ExecContext(ctx, queries["identities.create"],
			id.ID, id.UserID, id.Provider, id.Subject, id.DisplayName, id.Email, now, now)
		return err
	})
}

func (s *SQLiteStore) UpdateUserIdentityProfile(ctx context.Context, id, displayName, email string, now int64) error {
	return s.writeMu.WithLock(func() error {
		_, err := s.db.ExecContext(ctx, queries["identities.update_profile"], displayName, email, now, id)
		return err
	})
}

type identityTx interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type identitySQL interface {
	selectIdentity() string
	insertUserIgnoreConflict() string
	selectUserByID() string
	selectUserByUsername() string
	insertIdentityIgnoreConflict() string
	updateIdentityProfile() string
}

type sqliteIdentitySQL struct{}

func (sqliteIdentitySQL) selectIdentity() string {
	return "SELECT id, user_id, provider, subject, display_name, email, created_at, updated_at FROM user_identities WHERE provider = ? AND subject = ?"
}
func (sqliteIdentitySQL) insertUserIgnoreConflict() string {
	return "INSERT INTO users (id, username, password_hash, role, display_name, status, created_at, updated_at, last_login_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULL) ON CONFLICT(username) DO NOTHING"
}
func (sqliteIdentitySQL) selectUserByID() string {
	return "SELECT id, username, password_hash, role, display_name, status, created_at, updated_at, last_login_at FROM users WHERE id = ?"
}
func (sqliteIdentitySQL) selectUserByUsername() string {
	return "SELECT id, username, password_hash, role, display_name, status, created_at, updated_at, last_login_at FROM users WHERE username = ?"
}
func (sqliteIdentitySQL) insertIdentityIgnoreConflict() string {
	return "INSERT INTO user_identities (id, user_id, provider, subject, display_name, email, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(provider, subject) DO NOTHING"
}
func (sqliteIdentitySQL) updateIdentityProfile() string {
	return "UPDATE user_identities SET display_name = ?, email = ?, updated_at = ? WHERE id = ?"
}

func getOrCreateUserByIdentityTx(ctx context.Context, tx identityTx, provider, subject, username, displayName, email, userID, identityID string, now int64, sqls identitySQL) (*IdentityUserResult, error) {
	identity, err := scanIdentity(tx.QueryRowContext(ctx, sqls.selectIdentity(), provider, subject))
	if err == nil {
		if identity.DisplayName != displayName || identity.Email != email {
			if _, err := tx.ExecContext(ctx, sqls.updateIdentityProfile(), displayName, email, now, identity.ID); err != nil {
				return nil, err
			}
			identity.DisplayName = displayName
			identity.Email = email
			identity.UpdatedAt = now
		}
		user, err := scanUser(tx.QueryRowContext(ctx, sqls.selectUserByID(), identity.UserID))
		if errors.Is(err, sql.ErrNoRows) {
			return nil, security.ErrUserNotFound
		}
		if err != nil {
			return nil, err
		}
		return &IdentityUserResult{User: user, Identity: identity}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	if _, err := tx.ExecContext(ctx, sqls.insertUserIgnoreConflict(),
		userID, username, "", "user", displayName, "active", now, now); err != nil {
		return nil, err
	}
	user, err := scanUser(tx.QueryRowContext(ctx, sqls.selectUserByUsername(), username))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, security.ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}

	insertResult, err := tx.ExecContext(ctx, sqls.insertIdentityIgnoreConflict(),
		identityID, user.ID, provider, subject, displayName, email, now, now)
	if err != nil {
		return nil, err
	}
	created := true
	if rows, err := insertResult.RowsAffected(); err == nil && rows == 0 {
		created = false
		conflicted, err := scanIdentity(tx.QueryRowContext(ctx, sqls.selectIdentity(), provider, subject))
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrIdentityNotFound
		}
		if err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, sqls.updateIdentityProfile(), displayName, email, now, conflicted.ID); err != nil {
			return nil, err
		}
	}
	identity, err = scanIdentity(tx.QueryRowContext(ctx, sqls.selectIdentity(), provider, subject))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrIdentityNotFound
	}
	if err != nil {
		return nil, err
	}
	return &IdentityUserResult{User: user, Identity: identity, Created: created}, nil
}
