package session

import (
	"context"
	"database/sql"
	"errors"

	"github.com/hrygo/hotplex/internal/security"
)

// pgStore multitenancy methods — mirror SQLiteStore but use rebound queries (s.queries)
// and no writeMu (PG handles concurrency natively, see pg_store.go).

// --- pgStore: users ---

func (s *pgStore) CreateUser(ctx context.Context, u *security.User, now int64) error {
	_, err := s.db.ExecContext(ctx, s.queries["users.create"],
		u.ID, u.Username, u.PasswordHash, u.Role, u.DisplayName, u.Status, now, now)
	return err
}

func (s *pgStore) GetUserByID(ctx context.Context, id string) (*security.User, error) {
	u, err := scanUser(s.db.QueryRowContext(ctx, s.queries["users.get_by_id"], id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, security.ErrUserNotFound
	}
	return u, err
}

func (s *pgStore) GetUserByUsername(ctx context.Context, username string) (*security.User, error) {
	u, err := scanUser(s.db.QueryRowContext(ctx, s.queries["users.get_by_username"], username))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, security.ErrUserNotFound
	}
	return u, err
}

func (s *pgStore) ListUsers(ctx context.Context, limit, offset int) ([]*security.User, error) {
	if limit <= 0 {
		limit = 1000
	}
	rows, err := s.db.QueryContext(ctx, s.queries["users.list"], limit, offset)
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

func (s *pgStore) UpdateUserStatus(ctx context.Context, id, status string, now int64) error {
	_, err := s.db.ExecContext(ctx, s.queries["users.update_status"], status, now, id)
	return err
}

func (s *pgStore) TouchUserLastLogin(ctx context.Context, userID string, now int64) error {
	_, err := s.db.ExecContext(ctx, s.queries["users.touch_last_login"], now, now, userID)
	return err
}

// --- pgStore: workspaces ---

func (s *pgStore) CreateWorkspace(ctx context.Context, w *Workspace, now int64) error {
	_, err := s.db.ExecContext(ctx, s.queries["workspaces.create"],
		w.ID, w.OwnerUserID, w.Name, w.WorkDir, now, now)
	return err
}

func (s *pgStore) GetWorkspaceByID(ctx context.Context, id string) (*Workspace, error) {
	w, err := scanWorkspace(s.db.QueryRowContext(ctx, s.queries["workspaces.get_by_id"], id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrWorkspaceNotFound
	}
	return w, err
}

func (s *pgStore) ListWorkspacesByOwner(ctx context.Context, ownerUserID string) ([]*Workspace, error) {
	rows, err := s.db.QueryContext(ctx, s.queries["workspaces.list_by_owner"], ownerUserID)
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

func (s *pgStore) GetWorkspaceByOwnerAndWorkDir(ctx context.Context, ownerUserID, workDir string) (*Workspace, error) {
	w, err := scanWorkspace(s.db.QueryRowContext(ctx, s.queries["workspaces.get_by_owner_and_workdir"], ownerUserID, workDir))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrWorkspaceNotFound
	}
	return w, err
}

func (s *pgStore) UpdateWorkspace(ctx context.Context, w *Workspace, now int64) error {
	_, err := s.db.ExecContext(ctx, s.queries["workspaces.update"],
		w.Name, nullableString(w.AgentConfigOverrides), nullableString(w.WorkerPreference), now, w.ID)
	return err
}

func (s *pgStore) DeleteWorkspace(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, s.queries["workspaces.delete"], id)
	return err
}

func (s *pgStore) CountActiveSessionsInWorkspace(ctx context.Context, workspaceID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, s.queries["workspaces.count_active_sessions"], workspaceID).Scan(&n)
	return n, err
}

// --- pgStore: invitations ---

func (s *pgStore) CreateInvitation(ctx context.Context, inv *Invitation, now int64) error {
	_, err := s.db.ExecContext(ctx, s.queries["invitations.create"],
		inv.ID, inv.Code, inv.CreatedBy, inv.Role, inv.ExpiresAt, now)
	return err
}

func (s *pgStore) GetInvitationByCode(ctx context.Context, code string) (*Invitation, error) {
	inv, err := scanInvitation(s.db.QueryRowContext(ctx, s.queries["invitations.get_by_code"], code))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrInvitationNotFound
	}
	return inv, err
}

func (s *pgStore) MarkInvitationUsed(ctx context.Context, id, usedBy string, now int64) error {
	res, err := s.db.ExecContext(ctx, s.queries["invitations.mark_used"], usedBy, now, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrInvitationAlreadyUsed
	}
	return nil
}

func (s *pgStore) ListInvitations(ctx context.Context) ([]*Invitation, error) {
	rows, err := s.db.QueryContext(ctx, s.queries["invitations.list"])
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

func (s *pgStore) DeleteInvitation(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, s.queries["invitations.delete"], id)
	return err
}
