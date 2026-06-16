package session

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/security"
)

// --- users ---

func TestUsersStore_CreateAndGet(t *testing.T) {
	t.Parallel()
	store, _ := helperDB(t)
	ctx := context.Background()
	u := &security.User{ID: "u-1", Username: "alice", PasswordHash: "$2a$12$fake", Role: "admin", Status: "active"}
	require.NoError(t, store.CreateUser(ctx, u, 1700000000))

	got, err := store.GetUserByUsername(ctx, "alice")
	require.NoError(t, err)
	require.Equal(t, "u-1", got.ID)
	require.Equal(t, "admin", got.Role)

	got2, err := store.GetUserByID(ctx, "u-1")
	require.NoError(t, err)
	require.Equal(t, "alice", got2.Username)
}

func TestUsersStore_GetByUsername_NotFound(t *testing.T) {
	t.Parallel()
	store, _ := helperDB(t)
	_, err := store.GetUserByUsername(context.Background(), "nobody")
	require.ErrorIs(t, err, security.ErrUserNotFound)
}

func TestUsersStore_ListAndUpdateStatus(t *testing.T) {
	t.Parallel()
	store, _ := helperDB(t)
	ctx := context.Background()
	require.NoError(t, store.CreateUser(ctx, &security.User{ID: "u-1", Username: "alice", Role: "user", Status: "active"}, 1700000000))
	require.NoError(t, store.CreateUser(ctx, &security.User{ID: "u-2", Username: "bob", Role: "user", Status: "active"}, 1700000000))

	list, err := store.ListUsers(ctx, 100, 0)
	require.NoError(t, err)
	require.Len(t, list, 2)

	require.NoError(t, store.UpdateUserStatus(ctx, "u-2", "disabled", 1800000000))
	disabled, _ := store.GetUserByID(ctx, "u-2")
	require.Equal(t, "disabled", disabled.Status)
}

// --- workspaces ---

func TestWorkspacesStore_CreateUniqueConflict(t *testing.T) {
	t.Parallel()
	store, _ := helperDB(t)
	ctx := context.Background()
	require.NoError(t, store.CreateUser(ctx, &security.User{ID: "u-1", Username: "alice", Role: "user", Status: "active"}, 1700000000))

	require.NoError(t, store.CreateWorkspace(ctx, &Workspace{ID: "ws-1", OwnerUserID: "u-1", Name: "proj", WorkDir: "/tmp/proj"}, 1700000000))
	// 同 owner 同 work_dir 冲突（UNIQUE(owner_user_id, work_dir)，spec §6.2 per-user 1:1）
	err := store.CreateWorkspace(ctx, &Workspace{ID: "ws-2", OwnerUserID: "u-1", Name: "proj2", WorkDir: "/tmp/proj"}, 1700000000)
	require.Error(t, err, "同 owner+work_dir 必须 1:1 唯一")
}

func TestWorkspacesStore_DifferentOwnersSameWorkDir(t *testing.T) {
	t.Parallel()
	store, _ := helperDB(t)
	ctx := context.Background()
	require.NoError(t, store.CreateUser(ctx, &security.User{ID: "u-1", Username: "alice", Role: "user", Status: "active"}, 1700000000))
	require.NoError(t, store.CreateUser(ctx, &security.User{ID: "u-2", Username: "bob", Role: "user", Status: "active"}, 1700000000))
	// 不同 owner 同 work_dir 允许（协作场景，spec §2.2）
	require.NoError(t, store.CreateWorkspace(ctx, &Workspace{ID: "ws-1", OwnerUserID: "u-1", Name: "p", WorkDir: "/tmp/shared"}, 1700000000))
	require.NoError(t, store.CreateWorkspace(ctx, &Workspace{ID: "ws-2", OwnerUserID: "u-2", Name: "p", WorkDir: "/tmp/shared"}, 1700000000))
}

func TestWorkspacesStore_GetByOwnerAndWorkDir(t *testing.T) {
	t.Parallel()
	store, _ := helperDB(t)
	ctx := context.Background()
	require.NoError(t, store.CreateUser(ctx, &security.User{ID: "u-1", Username: "alice", Role: "user", Status: "active"}, 1700000000))
	require.NoError(t, store.CreateWorkspace(ctx, &Workspace{ID: "ws-1", OwnerUserID: "u-1", Name: "p", WorkDir: "/tmp/x"}, 1700000000))

	got, err := store.GetWorkspaceByOwnerAndWorkDir(ctx, "u-1", "/tmp/x")
	require.NoError(t, err)
	require.Equal(t, "ws-1", got.ID)

	_, err = store.GetWorkspaceByOwnerAndWorkDir(ctx, "u-1", "/tmp/other")
	require.ErrorIs(t, err, ErrWorkspaceNotFound)
}

func TestWorkspacesStore_ListByOwnerIsolated(t *testing.T) {
	t.Parallel()
	store, _ := helperDB(t)
	ctx := context.Background()
	require.NoError(t, store.CreateUser(ctx, &security.User{ID: "u-1", Username: "alice", Role: "user", Status: "active"}, 1700000000))
	require.NoError(t, store.CreateUser(ctx, &security.User{ID: "u-2", Username: "bob", Role: "user", Status: "active"}, 1700000000))
	require.NoError(t, store.CreateWorkspace(ctx, &Workspace{ID: "ws-1", OwnerUserID: "u-1", Name: "a", WorkDir: "/tmp/a"}, 1700000000))
	require.NoError(t, store.CreateWorkspace(ctx, &Workspace{ID: "ws-2", OwnerUserID: "u-1", Name: "b", WorkDir: "/tmp/b"}, 1700000000))
	require.NoError(t, store.CreateWorkspace(ctx, &Workspace{ID: "ws-3", OwnerUserID: "u-2", Name: "c", WorkDir: "/tmp/c"}, 1700000000))

	list, err := store.ListWorkspacesByOwner(ctx, "u-1")
	require.NoError(t, err)
	require.Len(t, list, 2, "只返回 owner=u-1 的 workspace")
	for _, w := range list {
		require.Equal(t, "u-1", w.OwnerUserID)
	}
}

func TestWorkspacesStore_CountActiveSessions(t *testing.T) {
	t.Parallel()
	store, _ := helperDB(t)
	ctx := context.Background()
	require.NoError(t, store.CreateUser(ctx, &security.User{ID: "u-1", Username: "alice", Role: "user", Status: "active"}, 1700000000))
	require.NoError(t, store.CreateWorkspace(ctx, &Workspace{ID: "ws-1", OwnerUserID: "u-1", Name: "p", WorkDir: "/tmp/x"}, 1700000000))

	n, err := store.CountActiveSessionsInWorkspace(ctx, "ws-1")
	require.NoError(t, err)
	require.Equal(t, 0, n, "新 workspace 无活跃会话")
}

// --- invitations ---

func TestInvitationsStore_CreateAndMarkUsedCAS(t *testing.T) {
	t.Parallel()
	store, _ := helperDB(t)
	ctx := context.Background()
	require.NoError(t, store.CreateUser(ctx, &security.User{ID: "admin-1", Username: "admin", Role: "admin", Status: "active"}, 1700000000))

	inv := &Invitation{ID: "inv-1", Code: "CODE123", CreatedBy: "admin-1", Role: "user", ExpiresAt: 1800000000}
	require.NoError(t, store.CreateInvitation(ctx, inv, 1700000000))

	got, err := store.GetInvitationByCode(ctx, "CODE123")
	require.NoError(t, err)
	require.Nil(t, got.UsedBy, "新建邀请 used_by 应为 NULL")

	// accept-invite 流程（spec §8.6）：先创建 user，再 mark invitation used。
	// used_by 的 FK 要求 user 先存在。
	require.NoError(t, store.CreateUser(ctx, &security.User{ID: "new-user-1", Username: "newbie", Role: "user", Status: "active"}, 1700000000))
	require.NoError(t, store.MarkInvitationUsed(ctx, "inv-1", "new-user-1", 1750000000))
	got2, _ := store.GetInvitationByCode(ctx, "CODE123")
	require.NotNil(t, got2.UsedBy)
	require.Equal(t, "new-user-1", *got2.UsedBy)

	// CAS：重复 mark 失败（防重放，spec §8.6）
	err = store.MarkInvitationUsed(ctx, "inv-1", "new-user-2", 1750000001)
	require.ErrorIs(t, err, ErrInvitationAlreadyUsed)
}

func TestInvitationsStore_GetByCode_NotFound(t *testing.T) {
	t.Parallel()
	store, _ := helperDB(t)
	_, err := store.GetInvitationByCode(context.Background(), "NOPE")
	require.ErrorIs(t, err, ErrInvitationNotFound)
}

func TestInvitationsStore_List(t *testing.T) {
	t.Parallel()
	store, _ := helperDB(t)
	ctx := context.Background()
	require.NoError(t, store.CreateUser(ctx, &security.User{ID: "admin-1", Username: "admin", Role: "admin", Status: "active"}, 1700000000))
	require.NoError(t, store.CreateInvitation(ctx, &Invitation{ID: "inv-1", Code: "C1", CreatedBy: "admin-1", Role: "user", ExpiresAt: 1800000000}, 1700000000))
	require.NoError(t, store.CreateInvitation(ctx, &Invitation{ID: "inv-2", Code: "C2", CreatedBy: "admin-1", Role: "user", ExpiresAt: 1800000000}, 1700000001))

	list, err := store.ListInvitations(ctx, 100, 0)
	require.NoError(t, err)
	require.Len(t, list, 2)
}
