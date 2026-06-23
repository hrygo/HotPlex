package session

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

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

func (s *pgStore) HasAdmin(ctx context.Context) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx, s.queries["users.has_admin"]).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("has admin: %w", err)
	}
	return true, nil
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

func (s *pgStore) ListWorkspacesByOwner(ctx context.Context, ownerUserID string, limit, offset int) ([]*Workspace, error) {
	rows, err := s.db.QueryContext(ctx, s.queries["workspaces.list_by_owner"], ownerUserID, limit, offset)
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

// ListAllWorkspaces returns all active workspaces regardless of owner (PG backend).
func (s *pgStore) ListAllWorkspaces(ctx context.Context) ([]*Workspace, error) {
	rows, err := s.db.QueryContext(ctx, s.queries["workspaces.list_all"])
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

// UpdateWorkspace applies the in-memory workspace fields, guarded by an
// optimistic-concurrency CAS on updated_at (review P3-1, mirrors SQLiteStore).
// RowsAffected==0 means a concurrent update bumped updated_at (or the row was
// deleted between the handler's Get and this write) → ErrWorkspaceConflict.
func (s *pgStore) UpdateWorkspace(ctx context.Context, w *Workspace, now int64) error {
	res, err := s.db.ExecContext(ctx, s.queries["workspaces.update"],
		w.Name, nullableString(w.AgentConfigOverrides), nullableString(w.WorkerPreference), now, w.ID, w.UpdatedAt)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrWorkspaceConflict
	}
	w.UpdatedAt = now
	return nil
}

func (s *pgStore) DeleteWorkspace(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, s.queries["workspaces.delete"], id)
	return err
}

// DeleteWorkspaceIfEmpty 原子删除：仅当无活跃会话时成功（防 TOCTOU，spec §9.1）。
//
// RowsAffected==0 有两种成因，必须区分（与 SQLite 版一致）：
//   - workspace 存在但有活跃会话 → ErrWorkspaceNotEmpty（HTTP 409）
//   - workspace 已被并发 actor 删除 → ErrWorkspaceNotFound（HTTP 404）
//
// PG 无 writeMu（数据库自身 MVCC 已保证查询隔离），重新检查 GetWorkspaceByID
// 即可区分两种成因，使 handler 的 ErrWorkspaceNotFound 分支在 PG 下可达。
func (s *pgStore) DeleteWorkspaceIfEmpty(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, s.queries["workspaces.delete_if_empty"], id, id)
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

// SetInvitationUsedBy 将 CAS 消费时的占位 used_by（inv.CreatedBy）更新为真实接受者。
func (s *pgStore) SetInvitationUsedBy(ctx context.Context, id, oldUsedBy, newUsedBy string) error {
	_, err := s.db.ExecContext(ctx, s.queries["invitations.set_used_by"], newUsedBy, id, oldUsedBy)
	return err
}

func (s *pgStore) ListInvitations(ctx context.Context, limit, offset int) ([]*Invitation, error) {
	if limit <= 0 {
		limit = 1000
	}
	rows, err := s.db.QueryContext(ctx, s.queries["invitations.list"], limit, offset)
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

// --- pgStore: user identities (spec ④) ---

func (s *pgStore) GetUserIdentityByProviderSubject(ctx context.Context, provider, subject string) (*UserIdentity, error) {
	id, err := scanIdentity(s.db.QueryRowContext(ctx, s.queries["identities.get_by_provider_subject"], provider, subject))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrIdentityNotFound
	}
	return id, err
}

func (s *pgStore) CreateUserIdentity(ctx context.Context, id *UserIdentity, now int64) error {
	_, err := s.db.ExecContext(ctx, s.queries["identities.create"],
		id.ID, id.UserID, id.Provider, id.Subject, id.DisplayName, id.Email, now, now)
	return err
}

func (s *pgStore) UpdateUserIdentityProfile(ctx context.Context, id, displayName, email string, now int64) error {
	_, err := s.db.ExecContext(ctx, s.queries["identities.update_profile"], displayName, email, now, id)
	return err
}
