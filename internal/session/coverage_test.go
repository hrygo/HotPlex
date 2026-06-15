package session

import (
	"context"
	"log/slog"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/security"
	"github.com/hrygo/hotplex/internal/worker"
)

// --- key.go: DeriveCronSessionKey ---

func TestDeriveCronSessionKey_Deterministic(t *testing.T) {
	t.Parallel()
	k1 := DeriveCronSessionKey("job-1", 1700000000)
	k2 := DeriveCronSessionKey("job-1", 1700000000)
	require.Equal(t, k1, k2)
}

func TestDeriveCronSessionKey_DifferentArgs(t *testing.T) {
	t.Parallel()
	k1 := DeriveCronSessionKey("job-1", 100)
	k2 := DeriveCronSessionKey("job-1", 200)
	k3 := DeriveCronSessionKey("job-2", 100)
	require.NotEqual(t, k1, k2, "different epoch → different key")
	require.NotEqual(t, k1, k3, "different jobID → different key")
}

func TestDeriveCronSessionKey_UUIDv5Format(t *testing.T) {
	t.Parallel()
	re := regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-5[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}`)
	require.Regexp(t, re, DeriveCronSessionKey("job", 1))
}

// --- pool.go: UpdateLimits ---

func TestPool_UpdateLimits(t *testing.T) {
	t.Parallel()
	p := NewPoolManager(slog.Default(), 10, 3, 0)
	p.UpdateLimits(20, 5)
	total, max, users := p.Stats()
	require.Equal(t, 0, total)
	require.Equal(t, 20, max)
	require.Equal(t, 0, users)
}

// --- SQLiteStore: remaining multitenancy methods ---

func TestSQLiteStore_TouchUserLastLogin(t *testing.T) {
	t.Parallel()
	store, _ := helperDB(t)
	ctx := context.Background()
	require.NoError(t, store.CreateUser(ctx, &security.User{ID: "u-1", Username: "alice", Role: "user", Status: "active"}, 100))
	require.NoError(t, store.TouchUserLastLogin(ctx, "u-1", 200))
}

func TestSQLiteStore_GetWorkspaceByID(t *testing.T) {
	t.Parallel()
	store, _ := helperDB(t)
	ctx := context.Background()
	require.NoError(t, store.CreateUser(ctx, &security.User{ID: "u-1", Username: "alice", Role: "user", Status: "active"}, 100))
	require.NoError(t, store.CreateWorkspace(ctx, &Workspace{ID: "ws-1", OwnerUserID: "u-1", Name: "p", WorkDir: "/tmp/x"}, 100))

	got, err := store.GetWorkspaceByID(ctx, "ws-1")
	require.NoError(t, err)
	require.Equal(t, "p", got.Name)
}

func TestSQLiteStore_GetWorkspaceByID_NotFound(t *testing.T) {
	t.Parallel()
	store, _ := helperDB(t)
	_, err := store.GetWorkspaceByID(context.Background(), "nope")
	require.ErrorIs(t, err, ErrWorkspaceNotFound)
}

func TestSQLiteStore_UpdateWorkspace(t *testing.T) {
	t.Parallel()
	store, _ := helperDB(t)
	ctx := context.Background()
	require.NoError(t, store.CreateUser(ctx, &security.User{ID: "u-1", Username: "alice", Role: "user", Status: "active"}, 100))
	require.NoError(t, store.CreateWorkspace(ctx, &Workspace{ID: "ws-1", OwnerUserID: "u-1", Name: "p", WorkDir: "/tmp/x"}, 100))

	require.NoError(t, store.UpdateWorkspace(ctx, &Workspace{ID: "ws-1", Name: "renamed", AgentConfigOverrides: `{"foo":"bar"}`}, 200))
	got, _ := store.GetWorkspaceByID(ctx, "ws-1")
	require.Equal(t, "renamed", got.Name)
	require.Equal(t, `{"foo":"bar"}`, got.AgentConfigOverrides)
}

func TestSQLiteStore_DeleteWorkspace(t *testing.T) {
	t.Parallel()
	store, _ := helperDB(t)
	ctx := context.Background()
	require.NoError(t, store.CreateUser(ctx, &security.User{ID: "u-1", Username: "alice", Role: "user", Status: "active"}, 100))
	require.NoError(t, store.CreateWorkspace(ctx, &Workspace{ID: "ws-1", OwnerUserID: "u-1", Name: "p", WorkDir: "/tmp/x"}, 100))

	require.NoError(t, store.DeleteWorkspace(ctx, "ws-1"))
	_, err := store.GetWorkspaceByID(ctx, "ws-1")
	require.ErrorIs(t, err, ErrWorkspaceNotFound)
}

func TestSQLiteStore_DeleteInvitation(t *testing.T) {
	t.Parallel()
	store, _ := helperDB(t)
	ctx := context.Background()
	require.NoError(t, store.CreateUser(ctx, &security.User{ID: "admin-1", Username: "admin", Role: "admin", Status: "active"}, 100))
	require.NoError(t, store.CreateInvitation(ctx, &Invitation{ID: "inv-1", Code: "C1", CreatedBy: "admin-1", Role: "user", ExpiresAt: 200}, 100))

	require.NoError(t, store.DeleteInvitation(ctx, "inv-1"))
	_, err := store.GetInvitationByCode(ctx, "C1")
	require.ErrorIs(t, err, ErrInvitationNotFound)
}

// --- Manager: SetWorkspaceID, UpdateWorkDir, ResetExpiry, EnsureWorkerSessionID ---

func TestManager_SetWorkspaceID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := new(mockStore)
	store.Test(t)
	store.On("Upsert", ctx, mock.AnythingOfType("*session.SessionInfo")).Return(nil)
	store.On("Close").Return(nil)
	store.On("Get", ctx, "nope").Return(nil, ErrSessionNotFound)

	m, _ := NewManager(ctx, nil, config.Default(), nil, store)
	defer m.Close()

	_, err := m.Create(ctx, "sess-ws", "u1", worker.TypeClaudeCode, nil, "", "test")
	require.NoError(t, err)

	require.NoError(t, m.SetWorkspaceID(ctx, "sess-ws", "ws-123"))
	info, _ := m.Get(ctx, "sess-ws")
	require.Equal(t, "ws-123", info.WorkspaceID)

	// Empty workspaceID is a no-op
	require.NoError(t, m.SetWorkspaceID(ctx, "sess-ws", ""))

	// Not found
	require.ErrorIs(t, m.SetWorkspaceID(ctx, "nope", "ws"), ErrSessionNotFound)
}

func TestManager_UpdateWorkDir(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := new(mockStore)
	store.Test(t)
	store.On("Upsert", ctx, mock.AnythingOfType("*session.SessionInfo")).Return(nil)
	store.On("Close").Return(nil)
	store.On("Get", ctx, "nope").Return(nil, ErrSessionNotFound)

	m, _ := NewManager(ctx, nil, config.Default(), nil, store)
	defer m.Close()

	_, err := m.Create(ctx, "sess-wd", "u1", worker.TypeClaudeCode, nil, "", "test")
	require.NoError(t, err)

	require.NoError(t, m.UpdateWorkDir(ctx, "sess-wd", "/new/path"))
	info, _ := m.Get(ctx, "sess-wd")
	require.Equal(t, "/new/path", info.WorkDir)

	// Same workDir → no-op (no Upsert call beyond initial)
	require.NoError(t, m.UpdateWorkDir(ctx, "sess-wd", "/new/path"))

	// Not found
	require.ErrorIs(t, m.UpdateWorkDir(ctx, "nope", "/x"), ErrSessionNotFound)
}

func TestManager_ResetExpiry(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := new(mockStore)
	store.Test(t)
	store.On("Upsert", ctx, mock.AnythingOfType("*session.SessionInfo")).Return(nil)
	store.On("Close").Return(nil)
	store.On("Get", ctx, "nope").Return(nil, ErrSessionNotFound)

	m, _ := NewManager(ctx, nil, config.Default(), nil, store)
	defer m.Close()

	_, err := m.Create(ctx, "sess-exp", "u1", worker.TypeClaudeCode, nil, "", "test")
	require.NoError(t, err)

	old, _ := m.Get(ctx, "sess-exp")
	time.Sleep(10 * time.Millisecond)
	require.NoError(t, m.ResetExpiry(ctx, "sess-exp"))
	newInfo, _ := m.Get(ctx, "sess-exp")
	require.True(t, newInfo.ExpiresAt.After(*old.ExpiresAt), "ExpiresAt should be extended")

	// Not found
	require.ErrorIs(t, m.ResetExpiry(ctx, "nope"), ErrSessionNotFound)
}

func TestManager_EnsureWorkerSessionID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := new(mockStore)
	store.Test(t)
	store.On("Upsert", ctx, mock.AnythingOfType("*session.SessionInfo")).Return(nil)
	store.On("UpdateWorkerSessionIDSQL", ctx, "sess-wsid", "wsid-new").Return(nil)
	store.On("Close").Return(nil)
	store.On("Get", ctx, "nope").Return(nil, ErrSessionNotFound)

	m, _ := NewManager(ctx, nil, config.Default(), nil, store)
	defer m.Close()

	_, err := m.Create(ctx, "sess-wsid", "u1", worker.TypeClaudeCode, nil, "", "test")
	require.NoError(t, err)

	// force=true bypasses the fast-path skip even when the value matches
	require.NoError(t, m.EnsureWorkerSessionID(ctx, "sess-wsid", "wsid-new"))
	info, _ := m.Get(ctx, "sess-wsid")
	require.Equal(t, "wsid-new", info.WorkerSessionID)

	// Not found
	require.ErrorIs(t, m.EnsureWorkerSessionID(ctx, "nope", "x"), ErrSessionNotFound)
}
