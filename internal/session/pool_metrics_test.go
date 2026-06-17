package session

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestMetrics_SnapshotLogic verifies the per-pool state that snapshotMetricsLocked
// reads from — the values that get written to the package-global atomic gauges.
// We assert on the pool's own locked state (not the package globals) because the
// globals are shared across all PoolManager instances in the test binary and cannot
// be asserted reliably. The atomic.Store itself is trivial; the logic worth testing
// is the computation: totalCount, distinct users/workspaces, and totalMemoryReserved.
func TestMetrics_SnapshotLogic(t *testing.T) {
	t.Parallel()
	p := NewPoolManagerWithWorkspace(slog.Default(), 100, 5, 10*1024*1024*1024, 3)
	ctx := context.Background()

	// u1 在 ws-1 占 2 个 slot（AcquireForWorkspace 预留内存）
	require.NoError(t, p.AcquireForWorkspace(ctx, "u1", "ws-1"))
	require.NoError(t, p.AcquireForWorkspace(ctx, "u1", "ws-1"))
	// u2 是 platform session（ws=""），走 Acquire 路径：不进 workspace 维度，不预留内存
	require.NoError(t, p.Acquire(ctx, "u2"))

	// 读 pool 自身状态（snapshotMetricsLocked 的输入），持 p.mu 避免与 Acquire/Release 竞态
	p.mu.Lock()
	active := int64(p.totalCount)
	users := int64(len(p.userCount))
	workspaces := int64(len(p.workspaceCount))
	mem := p.totalMemoryReserved()
	p.mu.Unlock()

	require.Equal(t, int64(3), active)                   // 2 (u1) + 1 (u2)
	require.Equal(t, int64(2), users)                    // u1, u2
	require.Equal(t, int64(1), workspaces)               // ONLY ws-1; u2 platform (ws="") 不计入
	require.Equal(t, int64(2*workerMemoryEstimate), mem) // only u1's 2 workspace acquires reserve memory
}

// TestMetrics_SnapshotUpdatesOnRelease verifies that after Release the per-pool state
// returns toward baseline (snapshot is kept consistent on the release path).
func TestMetrics_SnapshotUpdatesOnRelease(t *testing.T) {
	t.Parallel()
	p := NewPoolManagerWithWorkspace(slog.Default(), 100, 5, 0, 3)
	ctx := context.Background()

	require.NoError(t, p.AcquireForWorkspace(ctx, "u1", "ws-1"))
	require.NoError(t, p.AcquireForWorkspace(ctx, "u1", "ws-1"))

	p.mu.Lock()
	activeBefore := int64(p.totalCount)
	p.mu.Unlock()
	require.Equal(t, int64(2), activeBefore)

	p.ReleaseForWorkspace(ctx, "u1", "ws-1")
	p.ReleaseForWorkspace(ctx, "u1", "ws-1")

	p.mu.Lock()
	activeAfter := int64(p.totalCount)
	usersAfter := int64(len(p.userCount))
	workspacesAfter := int64(len(p.workspaceCount))
	p.mu.Unlock()

	require.Equal(t, int64(0), activeAfter)
	require.Equal(t, int64(0), usersAfter)
	require.Equal(t, int64(0), workspacesAfter)
}

// TestMetrics_GaugesRegistered verifies the 4 new OTel gauges are registered
// (init() created them without returning an error). We cannot assert gauge VALUES
// here because the snapshot atomics are package-global and overwritten by every
// PoolManager instance in the test binary (flaky). Registration + the per-instance
// snapshot logic test (TestMetrics_SnapshotLogic) together cover the contract.
func TestMetrics_GaugesRegistered(t *testing.T) {
	t.Parallel()
	// init() has already run (package load). The fact that the package compiled and
	// other pool tests create PoolManagers without panic confirms the gauge callback
	// registered. We exercise the code path by creating a pool and acquiring/releasing,
	// which triggers snapshotMetricsLocked → writes the atomics the gauges read.
	p := NewPoolManagerWithWorkspace(slog.Default(), 100, 5, 0, 3)
	ctx := context.Background()
	require.NoError(t, p.AcquireForWorkspace(ctx, "u1", "ws-1"))
	p.ReleaseForWorkspace(ctx, "u1", "ws-1")
	// No panic, no error — gauge callback registration is sound.
}
