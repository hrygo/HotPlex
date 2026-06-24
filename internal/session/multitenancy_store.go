package session

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/hrygo/hotplex/internal/security"
)

// Workspace is a per-user named project directory (spec §6.2).
// work_dir is immutable after creation (enters session key derivation, spec §7).
type Workspace struct {
	ID                   string `json:"id"`
	OwnerUserID          string `json:"owner_user_id"`
	Name                 string `json:"name"`
	WorkDir              string `json:"work_dir"`
	AgentConfigOverrides string `json:"agent_config_overrides"` // JSON value; spec ② fills, spec ① stays empty
	WorkerPreference     string `json:"worker_preference"`      // spec ③ fills
	Status               string `json:"status"`
	CreatedAt            int64  `json:"created_at"`
	UpdatedAt            int64  `json:"updated_at"`
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
	TouchUserLastLogin(ctx context.Context, userID string, now int64) error
	// workspaces
	CreateWorkspace(ctx context.Context, w *Workspace, now int64) error
	GetWorkspaceByID(ctx context.Context, id string) (*Workspace, error)
	ListWorkspacesByOwner(ctx context.Context, ownerUserID string, limit, offset int) ([]*Workspace, error)
	ListAllWorkspaces(ctx context.Context) ([]*Workspace, error)
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
	var overrides, pref sql.NullString
	var createdAt, updatedAt sql.NullInt64
	err := sc.Scan(&w.ID, &w.OwnerUserID, &w.Name, &w.WorkDir, &overrides, &pref, &w.Status,
		&createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	w.AgentConfigOverrides = overrides.String
	w.WorkerPreference = pref.String
	w.CreatedAt = createdAt.Int64
	w.UpdatedAt = updatedAt.Int64
	return &w, nil
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

func (s *SQLiteStore) UpdateUserStatus(ctx context.Context, id, status string, now int64) error {
	return s.writeMu.WithLock(func() error {
		_, err := s.db.ExecContext(ctx, queries["users.update_status"], status, now, id)
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
			w.ID, w.OwnerUserID, w.Name, w.WorkDir, now, now)
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
func (s *SQLiteStore) UpdateWorkspace(ctx context.Context, w *Workspace, now int64) error {
	return s.writeMu.WithLock(func() error {
		res, err := s.db.ExecContext(ctx, queries["workspaces.update"],
			w.Name, nullableString(w.AgentConfigOverrides), nullableString(w.WorkerPreference), w.WorkDir, now, w.ID, w.UpdatedAt)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
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
