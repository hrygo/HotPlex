package session

import (
	"context"
	"database/sql"
	"errors"

	"github.com/hrygo/hotplex/internal/security"
)

// Workspace is a per-user named project directory (spec §6.2).
// work_dir is immutable after creation (enters session key derivation, spec §7).
type Workspace struct {
	ID                   string
	OwnerUserID          string
	Name                 string
	WorkDir              string
	AgentConfigOverrides string // JSON; spec ② fills, spec ① stays empty
	WorkerPreference     string // spec ③ fills
	Status               string
}

// Invitation is a one-time invite code (spec §6.3).
type Invitation struct {
	ID        string
	Code      string
	CreatedBy string
	Role      string
	UsedBy    *string // nil = unused
	ExpiresAt int64
	CreatedAt int64
	UsedAt    *int64 // nil = unused
}

// Multitenancy store sentinels.
var (
	ErrWorkspaceNotFound     = errors.New("session: workspace not found")
	ErrInvitationNotFound    = errors.New("session: invitation not found")
	ErrInvitationAlreadyUsed = errors.New("session: invitation already used")
)

// UserWorkspaceStore is the store capability surface used by gateway auth/workspace
// handlers. SQLiteStore and pgStore both satisfy it. Embeds security.UserStore so
// LocalAccountProvider can be wired with the same store.
type UserWorkspaceStore interface {
	security.UserStore
	// users
	ListUsers(ctx context.Context, limit, offset int) ([]*security.User, error)
	UpdateUserStatus(ctx context.Context, id, status string, now int64) error
	DeleteUser(ctx context.Context, id string) error
	TouchUserLastLogin(ctx context.Context, userID string, now int64) error
	// workspaces
	CreateWorkspace(ctx context.Context, w *Workspace, now int64) error
	GetWorkspaceByID(ctx context.Context, id string) (*Workspace, error)
	ListWorkspacesByOwner(ctx context.Context, ownerUserID string) ([]*Workspace, error)
	GetWorkspaceByOwnerAndWorkDir(ctx context.Context, ownerUserID, workDir string) (*Workspace, error)
	UpdateWorkspace(ctx context.Context, w *Workspace, now int64) error
	DeleteWorkspace(ctx context.Context, id string) error
	CountActiveSessionsInWorkspace(ctx context.Context, workspaceID string) (int, error)
	// invitations
	CreateInvitation(ctx context.Context, inv *Invitation, now int64) error
	GetInvitationByCode(ctx context.Context, code string) (*Invitation, error)
	MarkInvitationUsed(ctx context.Context, id, usedBy string, now int64) error
	ListInvitations(ctx context.Context) ([]*Invitation, error)
	DeleteInvitation(ctx context.Context, id string) error
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
	var createdAt, updatedAt, lastLogin sql.NullInt64
	err := sc.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.DisplayName, &u.Status,
		&createdAt, &updatedAt, &lastLogin)
	if err != nil {
		return nil, err
	}
	return &u, nil
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

func (s *SQLiteStore) DeleteUser(ctx context.Context, id string) error {
	return s.writeMu.WithLock(func() error {
		_, err := s.db.ExecContext(ctx, queries["users.delete"], id)
		return err
	})
}

func (s *SQLiteStore) TouchUserLastLogin(ctx context.Context, userID string, now int64) error {
	return s.writeMu.WithLock(func() error {
		_, err := s.db.ExecContext(ctx, queries["users.touch_last_login"], now, now, userID)
		return err
	})
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

func (s *SQLiteStore) ListWorkspacesByOwner(ctx context.Context, ownerUserID string) ([]*Workspace, error) {
	rows, err := s.db.QueryContext(ctx, queries["workspaces.list_by_owner"], ownerUserID)
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

func (s *SQLiteStore) UpdateWorkspace(ctx context.Context, w *Workspace, now int64) error {
	return s.writeMu.WithLock(func() error {
		_, err := s.db.ExecContext(ctx, queries["workspaces.update"],
			w.Name, nullableString(w.AgentConfigOverrides), nullableString(w.WorkerPreference), now, w.ID)
		return err
	})
}

func (s *SQLiteStore) DeleteWorkspace(ctx context.Context, id string) error {
	return s.writeMu.WithLock(func() error {
		_, err := s.db.ExecContext(ctx, queries["workspaces.delete"], id)
		return err
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

func (s *SQLiteStore) ListInvitations(ctx context.Context) ([]*Invitation, error) {
	rows, err := s.db.QueryContext(ctx, queries["invitations.list"])
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
