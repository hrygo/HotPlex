package session

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/security"
)

// TestWorkspacesStore_ListAllWithOwner verifies the admin console projection
// (spec §3.1, issue #807): every active workspace returns with its owner's
// readable identity (display_name + username) joined in, ordered by owner
// display_name then workspace name, and the permission_mode override passes
// through ("" = no explicit override).
func TestWorkspacesStore_ListAllWithOwner(t *testing.T) {
	t.Parallel()
	store, _ := helperDB(t)
	ctx := context.Background()

	require.NoError(t, store.CreateUser(ctx, &security.User{ID: "u-1", Username: "alice", DisplayName: "Alice", Role: "user", Status: "active"}, 1700000000))
	require.NoError(t, store.CreateUser(ctx, &security.User{ID: "u-2", Username: "bob", DisplayName: "Bob", Role: "user", Status: "active"}, 1700000000))
	require.NoError(t, store.CreateWorkspace(ctx, &Workspace{ID: "ws-1", OwnerUserID: "u-1", Name: "proj-a", WorkDir: "/tmp/a", PermissionMode: "workspace"}, 1700000000))
	require.NoError(t, store.CreateWorkspace(ctx, &Workspace{ID: "ws-2", OwnerUserID: "u-1", Name: "proj-b", WorkDir: "/tmp/b"}, 1700000000)) // no override
	require.NoError(t, store.CreateWorkspace(ctx, &Workspace{ID: "ws-3", OwnerUserID: "u-2", Name: "proj-c", WorkDir: "/tmp/c", PermissionMode: "read-only"}, 1700000000))

	views, err := store.ListAllWorkspacesWithOwner(ctx)
	require.NoError(t, err)
	require.Len(t, views, 3)

	// ORDER BY u.display_name, w.name → Alice/proj-a, Alice/proj-b, Bob/proj-c.
	require.Equal(t, "ws-1", views[0].ID)
	require.Equal(t, "Alice", views[0].OwnerDisplayName, "owner display_name must join in")
	require.Equal(t, "alice", views[0].OwnerUsername, "owner username must join in")
	require.Equal(t, "workspace", views[0].PermissionMode)

	require.Equal(t, "ws-2", views[1].ID)
	require.Equal(t, "Alice", views[1].OwnerDisplayName)
	require.Equal(t, "", views[1].PermissionMode, "no explicit override scans as empty")

	require.Equal(t, "ws-3", views[2].ID)
	require.Equal(t, "Bob", views[2].OwnerDisplayName)
	require.Equal(t, "read-only", views[2].PermissionMode)
}

func TestWorkspacesStore_ListAllWithOwner_Empty(t *testing.T) {
	t.Parallel()
	store, _ := helperDB(t)
	views, err := store.ListAllWorkspacesWithOwner(context.Background())
	require.NoError(t, err)
	require.Empty(t, views)
}
