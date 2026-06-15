package session

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/events"
)

// TestUpsertAndGet_PreservesWorkspaceID 验证 workspace_id 能持久化并读回（spec §6.4）。
func TestUpsertAndGet_PreservesWorkspaceID(t *testing.T) {
	store, _ := helperDB(t)
	ctx := context.Background()
	now := time.Now()
	info := &SessionInfo{
		ID:          "sess-ws-1",
		UserID:      "u1",
		OwnerID:     "u1",
		WorkerType:  worker.TypeClaudeCode,
		State:       events.StateCreated,
		WorkDir:     "/tmp/proj",
		WorkspaceID: "ws-123",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	require.NoError(t, store.Upsert(ctx, info))

	got, err := store.Get(ctx, "sess-ws-1")
	require.NoError(t, err)
	require.Equal(t, "ws-123", got.WorkspaceID, "workspace_id 必须持久化并读回")
}

// TestUpsert_WorkspaceIDImmutableOnConflict 验证 ON CONFLICT 时 workspace_id 不可变（spec §6.4 注释 + key 派生稳定性）。
func TestUpsert_WorkspaceIDImmutableOnConflict(t *testing.T) {
	store, _ := helperDB(t)
	ctx := context.Background()
	now := time.Now()
	first := &SessionInfo{
		ID: "sess-imm", UserID: "u1", OwnerID: "u1", WorkerType: worker.TypeClaudeCode,
		State: events.StateCreated, WorkDir: "/tmp/p", WorkspaceID: "ws-orig",
		CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, store.Upsert(ctx, first))

	// 第二次 upsert 试图改 workspace_id → 应保留原值（CASE WHEN NULL 守卫）
	second := *first
	second.WorkspaceID = "ws-hijack"
	require.NoError(t, store.Upsert(ctx, &second))

	got, err := store.Get(ctx, "sess-imm")
	require.NoError(t, err)
	require.Equal(t, "ws-orig", got.WorkspaceID, "workspace_id ON CONFLICT 不可变")
}

// TestUpsert_PlatformSessionEmptyWorkspaceID 验证平台/cron 会话 workspace_id 为空（双轨隔离，spec §2）。
func TestUpsert_PlatformSessionEmptyWorkspaceID(t *testing.T) {
	store, _ := helperDB(t)
	ctx := context.Background()
	now := time.Now()
	info := &SessionInfo{
		ID: "sess-plat", UserID: "u1", OwnerID: "u1", WorkerType: worker.TypeClaudeCode,
		State: events.StateCreated, WorkDir: "/tmp/p", WorkspaceID: "",
		CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, store.Upsert(ctx, info))
	got, err := store.Get(ctx, "sess-plat")
	require.NoError(t, err)
	require.Equal(t, "", got.WorkspaceID, "平台会话 workspace_id 为空")
}

// TestListSessions_IncludesWorkspaceID 验证 List 读回也带 workspace_id（scanSession 覆盖）。
func TestListSessions_IncludesWorkspaceID(t *testing.T) {
	store, _ := helperDB(t)
	ctx := context.Background()
	now := time.Now()
	require.NoError(t, store.Upsert(ctx, &SessionInfo{
		ID: "sess-list", UserID: "u2", OwnerID: "u2", WorkerType: worker.TypeClaudeCode,
		State: events.StateCreated, WorkDir: "/tmp/q", WorkspaceID: "ws-list",
		CreatedAt: now, UpdatedAt: now,
	}))
	list, err := store.List(ctx, "u2", "", 10, 0)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, "ws-list", list[0].WorkspaceID)
}
