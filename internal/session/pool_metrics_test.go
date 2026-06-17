package session

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestMetrics_SnapshotReflectsState: 4 个 atomic 快照随 Acquire 正确变化(spec ⑤)。
//
// snapshotMetricsLocked 写入的是包级全局 atomic 变量(Prometheus gauge 约定),
// 其他并行测试可能同时通过 Acquire/Release 改写这些计数器。因此本测试不并行,
// 且用 **delta 比较**(after - before = expected Δ)替代绝对值断言,
// 消除任何残留全局状态的干扰。
func TestMetrics_SnapshotReflectsState(t *testing.T) {
	p := NewPoolManagerWithWorkspace(slog.Default(), 100, 5, 10*1024*1024*1024, 3)
	ctx := context.Background()

	// 读基线(允许其他测试残留状态;只关心本测试引入的 Δ)。
	baseActive := metricActiveSessions.Load()
	baseUsers := metricDistinctUsers.Load()
	baseWS := metricDistinctWorkspaces.Load()
	baseMem := metricMemoryReserved.Load()

	// u1 在 ws-1 占 2 个 slot(每个 reserve 1×workerMemoryEstimate)。
	require.NoError(t, p.AcquireForWorkspace(ctx, "u1", "ws-1"))
	require.NoError(t, p.AcquireForWorkspace(ctx, "u1", "ws-1"))
	// u2 是 platform session(ws=""),走 Acquire 路径,不进 workspace 维度,也不 reserve memory。
	require.NoError(t, p.Acquire(ctx, "u2"))

	// 比较增量,不受其他并行测试影响。
	require.Equal(t, int64(3), metricActiveSessions.Load()-baseActive, "active sessions delta")
	require.Equal(t, int64(2), metricDistinctUsers.Load()-baseUsers, "distinct users delta (u1, u2)")
	require.Equal(t, int64(1), metricDistinctWorkspaces.Load()-baseWS, "distinct workspaces delta (ONLY ws-1; u2 platform ws=\"\" not counted)")
	require.Equal(t, int64(2*workerMemoryEstimate), metricMemoryReserved.Load()-baseMem, "memory delta (u1×2 slots, u2 plain Acquire does not reserve)")
}
