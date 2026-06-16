package session

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestPool_PerWorkspaceConcurrency: the per-workspace limit is enforced independently
// per workspace (WebChat multi-tenant, spec ① §10).
func TestPool_PerWorkspaceConcurrency(t *testing.T) {
	t.Parallel()
	// global 100, per-user 5, per-workspace 2
	p := NewPoolManagerWithWorkspace(slog.Default(), 100, 5, 0, 2)
	ctx := context.Background()

	// ws-1 fills its 2 slots
	require.NoError(t, p.AcquireForWorkspace(ctx, "u1", "ws-1"))
	require.NoError(t, p.AcquireForWorkspace(ctx, "u1", "ws-1"))
	// ws-1's 3rd slot is rejected by the workspace quota
	err := p.AcquireForWorkspace(ctx, "u1", "ws-1")
	require.Error(t, err)
	var pe *PoolError
	require.ErrorAs(t, err, &pe)
	require.Equal(t, poolErrKindWorkspaceQuotaExceeded, pe.Kind)

	// ws-2 can still acquire (workspace dimension is independent)
	require.NoError(t, p.AcquireForWorkspace(ctx, "u1", "ws-2"))

	// release one ws-1 slot → ws-1 recovers
	p.ReleaseForWorkspace(ctx, "u1", "ws-1")
	require.NoError(t, p.AcquireForWorkspace(ctx, "u1", "ws-1"))
}

// TestPool_PlatformSessionSkipsWorkspaceLayer: workspaceID == "" (platform/cron
// sessions) bypasses the per-workspace limit — backward compatible.
func TestPool_PlatformSessionSkipsWorkspaceLayer(t *testing.T) {
	t.Parallel()
	p := NewPoolManagerWithWorkspace(slog.Default(), 100, 5, 0, 2)
	ctx := context.Background()
	// platform sessions with workspaceID="" are not subject to per-workspace limit
	for i := 0; i < 5; i++ {
		require.NoError(t, p.AcquireForWorkspace(ctx, "u1", ""))
	}
}

// TestPool_WorkspaceDisabledByDefault: NewPoolManager (without workspace limit) leaves
// AcquireForWorkspace behaving like AcquireWithMemory (no workspace enforcement).
func TestPool_WorkspaceDisabledByDefault(t *testing.T) {
	t.Parallel()
	p := NewPoolManager(slog.Default(), 100, 5, 0) // maxPerWorkspace == 0 → disabled
	ctx := context.Background()
	// no workspace limit → many acquires on the same workspace succeed (up to per-user)
	for i := 0; i < 5; i++ {
		require.NoError(t, p.AcquireForWorkspace(ctx, "u1", "ws-1"))
	}
}
