package session

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/pkg/events"
)

// ─── Pool Manager tests ────────────────────────────────────────────────────────

func TestPoolAcquire_Release(t *testing.T) {
	t.Parallel()

	_ = config.Default()
	pool := NewPoolManager(nil, 10, 3, 0)

	// First acquire succeeds
	err := pool.Acquire(context.Background(), "user1")
	require.Nil(t, err)

	total, max, users := pool.Stats()
	require.Equal(t, 1, total)
	require.Equal(t, 10, max)
	require.Equal(t, 1, users)

	pool.Release(context.Background(), "user1")

	total, _, users = pool.Stats()
	require.Equal(t, 0, total)
	require.Equal(t, 0, users)
}

func TestPoolAcquire_GlobalLimit(t *testing.T) {
	t.Parallel()

	pool := NewPoolManager(nil, 2, 10, 0)

	require.Nil(t, pool.Acquire(context.Background(), "user1"))
	require.Nil(t, pool.Acquire(context.Background(), "user2"))

	// Third should fail due to global limit
	err := pool.Acquire(context.Background(), "user3")
	require.NotNil(t, err)
	pe := new(PoolError)
	require.ErrorAs(t, err, &pe)
	require.Equal(t, poolErrKindExhausted, pe.Kind)
	require.Equal(t, 2, pe.Current)
	require.Equal(t, 2, pe.Max)
}

func TestPoolAcquire_UserQuotaLimit(t *testing.T) {
	t.Parallel()

	pool := NewPoolManager(nil, 10, 2, 0)

	require.Nil(t, pool.Acquire(context.Background(), "user1"))
	require.Nil(t, pool.Acquire(context.Background(), "user1"))

	// Third for same user fails
	err := pool.Acquire(context.Background(), "user1")
	require.NotNil(t, err)
	pe := new(PoolError)
	require.ErrorAs(t, err, &pe)
	require.Equal(t, poolErrKindUserQuotaExceeded, pe.Kind)
	require.Equal(t, "user1", pe.UserID)
	require.Equal(t, 2, pe.Current)
	require.Equal(t, 2, pe.Max)

	// Different user succeeds
	require.Nil(t, pool.Acquire(context.Background(), "user2"))
}

func TestPoolAcquire_Unlimited(t *testing.T) {
	t.Parallel()

	// maxSize=0 means unlimited
	pool := NewPoolManager(nil, 0, 0, 0)

	for i := 0; i < 100; i++ {
		err := pool.Acquire(context.Background(), "user1")
		require.Nil(t, err, "acquire %d should succeed", i)
	}

	total, max, _ := pool.Stats()
	require.Equal(t, 100, total)
	require.Equal(t, 0, max) // max=0 means unlimited
}

func TestPoolRelease_UserCountGoesToZero(t *testing.T) {
	t.Parallel()

	pool := NewPoolManager(nil, 10, 3, 0)

	require.NoError(t, pool.Acquire(context.Background(), "user1"))
	require.NoError(t, pool.Acquire(context.Background(), "user1"))
	pool.Release(context.Background(), "user1")
	pool.Release(context.Background(), "user1")

	_, _, users := pool.Stats()
	require.Equal(t, 0, users)
}

func TestPoolRelease_Underflow(t *testing.T) {
	t.Parallel()

	pool := NewPoolManager(nil, 10, 3, 0)

	// Release without acquire is guarded — no underflow.
	pool.Release(context.Background(), "user1")
	pool.Release(context.Background(), "user1")

	total, _, users := pool.Stats()
	require.Equal(t, 0, total) // guard prevents negative total
	require.Equal(t, 0, users)
}

func TestPoolError_Error(t *testing.T) {
	t.Parallel()

	err := &PoolError{Kind: poolErrKindExhausted, Current: 10, Max: 10}
	require.Contains(t, err.Error(), "pool:")
	require.Contains(t, err.Error(), "exhausted")
}

func TestPoolStats_MultiUser(t *testing.T) {
	t.Parallel()

	pool := NewPoolManager(nil, 100, 5, 0)

	require.NoError(t, pool.Acquire(context.Background(), "user1"))
	require.NoError(t, pool.Acquire(context.Background(), "user1"))
	require.NoError(t, pool.Acquire(context.Background(), "user2"))
	require.NoError(t, pool.Acquire(context.Background(), "user3"))

	total, _, users := pool.Stats()
	require.Equal(t, 4, total)
	require.Equal(t, 3, users)
}

// ─── Pool GC integration ──────────────────────────────────────────────────────

func TestPoolRelease_AfterGCTransitions(t *testing.T) {
	t.Parallel()

	pool := NewPoolManager(nil, 100, 3, 0)

	// Simulate multiple sessions per user
	require.Nil(t, pool.Acquire(context.Background(), "user1"))
	require.Nil(t, pool.Acquire(context.Background(), "user1"))

	// GC transitions one session to terminated → release quota
	pool.Release(context.Background(), "user1")

	// Now one slot is available for user1
	require.Nil(t, pool.Acquire(context.Background(), "user1"))
}

// ─── ValidTransitions table ──────────────────────────────────────────────────

func TestValidTransitions_Completeness(t *testing.T) {
	t.Parallel()

	allStates := []events.SessionState{
		events.StateCreated,
		events.StateRunning,
		events.StateIdle,
		events.StateTerminated,
		events.StateDeleted,
	}

	for _, state := range allStates {
		hasTarget := false
		for _, target := range allStates {
			if events.IsValidTransition(state, target) {
				hasTarget = true
				break
			}
		}
		if state == events.StateDeleted {
			require.False(t, hasTarget, "DELETED should have no outgoing transitions")
		} else {
			require.True(t, hasTarget, "state %s should have at least one valid transition", state)
		}
	}
}

func TestIsValidTransition_UnknownState(t *testing.T) {
	t.Parallel()

	// Unknown state should return false
	ok := events.IsValidTransition(events.SessionState("unknown"), events.StateRunning)
	require.False(t, ok)
}

// ─── Memory tracking tests ─────────────────────────────────────────────────────

func TestPoolAcquireMemory_Limit(t *testing.T) {
	t.Parallel()

	// 1 GB limit, 512 MB per worker → max 2 workers
	pool := NewPoolManager(nil, 100, 10, 1<<30)

	require.Nil(t, pool.AcquireMemory("user1"))
	require.Nil(t, pool.AcquireMemory("user1"))

	// Third worker would exceed 1 GB → rejected
	err := pool.AcquireMemory("user1")
	require.NotNil(t, err)
	pe := new(PoolError)
	require.ErrorAs(t, err, &pe)
	require.Equal(t, poolErrKindMemoryExceeded, pe.Kind)
}

func TestPoolAcquireMemory_Unlimited(t *testing.T) {
	t.Parallel()

	// maxMemoryPerUser=0 → unlimited
	pool := NewPoolManager(nil, 100, 10, 0)

	for i := 0; i < 10; i++ {
		require.Nil(t, pool.AcquireMemory("user1"))
	}
}

func TestPoolReleaseMemory(t *testing.T) {
	t.Parallel()

	pool := NewPoolManager(nil, 100, 10, 1<<30)

	require.Nil(t, pool.AcquireMemory("user1"))
	require.Nil(t, pool.AcquireMemory("user1"))

	// Release one → should allow a third acquire
	pool.ReleaseMemory("user1")
	require.Nil(t, pool.AcquireMemory("user1"))

	pool.ReleaseMemory("user1")
	pool.ReleaseMemory("user1")
}

func TestPoolMemory_AcrossUsers(t *testing.T) {
	t.Parallel()

	// 1 GB limit per user
	pool := NewPoolManager(nil, 100, 10, 1<<30)

	// user1 uses 1 GB
	require.Nil(t, pool.AcquireMemory("user1"))
	require.Nil(t, pool.AcquireMemory("user1"))
	err := pool.AcquireMemory("user1")
	require.NotNil(t, err)

	// user2 is independent
	require.Nil(t, pool.AcquireMemory("user2"))
	require.Nil(t, pool.AcquireMemory("user2"))
}

func TestPoolAttachMemory_Integrated(t *testing.T) {
	t.Parallel()

	// Test that memory is tracked alongside session count.
	// Simulates: Acquire + AcquireMemory → Release + ReleaseMemory.
	pool := NewPoolManager(nil, 10, 5, 1<<30)

	require.Nil(t, pool.Acquire(context.Background(), "user1"))
	require.Nil(t, pool.AcquireMemory("user1"))

	require.Equal(t, int64(workerMemoryEstimate), pool.UserMemory("user1"))

	pool.Release(context.Background(), "user1")
	pool.ReleaseMemory("user1")

	require.Equal(t, int64(0), pool.UserMemory("user1"))
}

func TestPoolAcquireWithMemory_Success(t *testing.T) {
	t.Parallel()

	pool := NewPoolManager(nil, 10, 5, 1<<30)
	require.Nil(t, pool.AcquireWithMemory(context.Background(), "user1"))
	require.Equal(t, int64(workerMemoryEstimate), pool.UserMemory("user1"))
	pool.Release(context.Background(), "user1")
	require.Equal(t, int64(0), pool.UserMemory("user1"))
}

func TestPoolAcquireWithMemory_MemoryExceeded(t *testing.T) {
	t.Parallel()

	// Limit to 1 worker's worth of memory.
	pool := NewPoolManager(nil, 10, 5, int64(workerMemoryEstimate))

	require.Nil(t, pool.AcquireWithMemory(context.Background(), "user1"))

	err := pool.AcquireWithMemory(context.Background(), "user1")
	require.Error(t, err)
	var pe *PoolError
	require.True(t, errors.As(err, &pe))
	require.Equal(t, poolErrKindMemoryExceeded, pe.Kind)

	pool.Release(context.Background(), "user1")
}

func TestPoolAcquireWithMemory_PoolExhausted(t *testing.T) {
	t.Parallel()

	pool := NewPoolManager(nil, 1, 5, 0)
	require.Nil(t, pool.AcquireWithMemory(context.Background(), "user1"))

	err := pool.AcquireWithMemory(context.Background(), "user2")
	require.Error(t, err)
	var pe *PoolError
	require.True(t, errors.As(err, &pe))
	require.Equal(t, poolErrKindExhausted, pe.Kind)

	pool.Release(context.Background(), "user1")
}

func TestPoolAcquireWithMemory_UserQuotaExceeded(t *testing.T) {
	t.Parallel()

	pool := NewPoolManager(nil, 10, 1, 0)
	require.Nil(t, pool.AcquireWithMemory(context.Background(), "user1"))

	err := pool.AcquireWithMemory(context.Background(), "user1")
	require.Error(t, err)
	var pe *PoolError
	require.True(t, errors.As(err, &pe))
	require.Equal(t, poolErrKindUserQuotaExceeded, pe.Kind)

	pool.Release(context.Background(), "user1")
}

// TestUpdateLimits_AllFourHotReload: UpdateLimits applies all four quota fields
// atomically under p.mu; new Acquire calls observe the new limits immediately (spec ⑤).
// 检查顺序在 acquireLocked 中是 global → per-user → per-workspace → memory,
// 所以测试针对每条分支构造"该分支是首个失败点"的场景。
func TestUpdateLimits_AllFourHotReload(t *testing.T) {
	t.Parallel()
	// 初始:global 100, per-user 5, per-ws 3, per-user-mem 10GB
	p := NewPoolManagerWithWorkspace(slog.Default(), 100, 5, 10*1024*1024*1024, 3)
	ctx := context.Background()

	// u1 占用 ws-1(global=1, u1 count=1, ws-1 count=1, u1 mem=512MB)
	require.NoError(t, p.AcquireForWorkspace(ctx, "u1", "ws-1"))

	var pe *PoolError

	// ── 场景 A:global 收紧 → exhausted(u2 受 global 限制先于 per-user 拦截) ──
	p.UpdateLimits(config.PoolConfig{
		MaxSize:          1,
		MaxIdlePerUser:   5, // 放宽,使 per-user 不先触发
		MaxPerWorkspace:  3,
		MaxMemoryPerUser: 10 * 1024 * 1024 * 1024,
	})
	err := p.Acquire(ctx, "u2")
	require.ErrorAs(t, err, &pe)
	require.Equal(t, poolErrKindExhausted, pe.Kind)

	// ── 场景 B:per-user 收紧 → user_quota_exceeded(global 放宽,使 per-user 成首个失败点) ──
	p.UpdateLimits(config.PoolConfig{
		MaxSize:          100,
		MaxIdlePerUser:   1,
		MaxPerWorkspace:  3,
		MaxMemoryPerUser: 10 * 1024 * 1024 * 1024,
	})
	err = p.AcquireForWorkspace(ctx, "u1", "ws-2")
	require.ErrorAs(t, err, &pe)
	require.Equal(t, poolErrKindUserQuotaExceeded, pe.Kind)

	// ── 场景 C:per-workspace 收紧 → workspace_quota_exceeded
	// (用新用户 u3 避开 u1 的 per-user 限制;ws-1 已有 1 个,MaxPerWorkspace=1) ──
	p.UpdateLimits(config.PoolConfig{
		MaxSize:          100,
		MaxIdlePerUser:   5,
		MaxPerWorkspace:  1,
		MaxMemoryPerUser: 10 * 1024 * 1024 * 1024,
	})
	err = p.AcquireForWorkspace(ctx, "u3", "ws-1")
	require.ErrorAs(t, err, &pe)
	require.Equal(t, poolErrKindWorkspaceQuotaExceeded, pe.Kind)

	// ── 场景 D:per-user-memory 收紧 → memory_exceeded
	// (用新用户 u4 + 新 ws-3 避开其他限制;mem=1B 时 512MB 估算必超) ──
	p.UpdateLimits(config.PoolConfig{
		MaxSize:          100,
		MaxIdlePerUser:   5,
		MaxPerWorkspace:  3,
		MaxMemoryPerUser: 1, // 1 字节
	})
	err = p.AcquireForWorkspace(ctx, "u4", "ws-3")
	require.ErrorAs(t, err, &pe)
	require.Equal(t, poolErrKindMemoryExceeded, pe.Kind)

	// ── 场景 E:放宽全部 4 维 → 释放并重新 Acquire 成功,证明 UpdateLimits 是可逆的 ──
	p.UpdateLimits(config.PoolConfig{
		MaxSize:          100,
		MaxIdlePerUser:   5,
		MaxPerWorkspace:  3,
		MaxMemoryPerUser: 10 * 1024 * 1024 * 1024,
	})
	p.ReleaseForWorkspace(ctx, "u1", "ws-1")
	require.NoError(t, p.AcquireForWorkspace(ctx, "u1", "ws-2"))
}

// TestUpdateLimits_DoesNotEvict: lowering the memory quota does NOT force-release
// already-running sessions; only new Acquire calls are rejected until natural Release.
// (spec ⑤ §4.3 no-evict invariant.)
func TestUpdateLimits_DoesNotEvict(t *testing.T) {
	t.Parallel()
	// per-user-mem 10GB → 1 slot = 512MB, can hold several
	p := NewPoolManagerWithWorkspace(slog.Default(), 100, 5, 10*1024*1024*1024, 3)
	ctx := context.Background()

	// u1 占 2 slot (≈1GB)
	require.NoError(t, p.AcquireForWorkspace(ctx, "u1", "ws-1"))
	require.NoError(t, p.AcquireForWorkspace(ctx, "u1", "ws-1"))

	// 降到 1GB (2*estimate) —— u1 现已恰好等于上限 (used 1GB),新 Acquire 仍被拒
	// (acquireLocked 用严格 > 比较: used+estimate > limit → 1GB+512MB > 1GB)。
	// 选 2*estimate 而非 1*estimate 是为了让释放 1 个 slot 后能恰好再拿 1 个
	// (used 512MB + estimate 512MB == 1024MB,不 > limit)。
	p.UpdateLimits(config.PoolConfig{
		MaxSize:          100,
		MaxIdlePerUser:   5,
		MaxPerWorkspace:  3,
		MaxMemoryPerUser: 2 * workerMemoryEstimate, // 1GB
	})

	// 已占用的 u1 计数仍在 (未被驱逐)
	total, _, _ := p.Stats()
	require.Equal(t, 2, total)
	require.Equal(t, int64(2*workerMemoryEstimate), p.UserMemory("u1"))

	// u1 新 Acquire 被内存维度拒 (超额)
	var pe *PoolError
	err := p.AcquireForWorkspace(ctx, "u1", "ws-1")
	require.ErrorAs(t, err, &pe)
	require.Equal(t, poolErrKindMemoryExceeded, pe.Kind)

	// 释放 1 个后,内存回到 512MB,可再拿 1 个 (恰好等于上限)
	p.ReleaseForWorkspace(ctx, "u1", "ws-1")
	require.NoError(t, p.AcquireForWorkspace(ctx, "u1", "ws-1"))
}

// TestPool_ConcurrentMixedOperations: concurrent Acquire/Release/UpdateLimits
// produces no data race and no counter drift (spec ⑤ §5.6).
// Asserts on per-instance p.Stats() (NOT package-global atomics, which are
// shared across all PoolManager instances in the test binary and thus flaky).
func TestPool_ConcurrentMixedOperations(t *testing.T) {
	t.Parallel()
	p := NewPoolManagerWithWorkspace(slog.Default(), 50, 10, 5*1024*1024*1024, 4)
	ctx := context.Background()

	var wg sync.WaitGroup
	wg.Add(1)
	// Acquire/Release worker
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			uid := "u" + strconv.Itoa(i%5)
			ws := "ws-" + strconv.Itoa(i%3)
			if p.AcquireForWorkspace(ctx, uid, ws) == nil {
				p.ReleaseForWorkspace(ctx, uid, ws)
			}
		}
	}()
	// UpdateLimits concurrently — 20 hot reloads while Acquire/Release runs
	for i := 0; i < 20; i++ {
		p.UpdateLimits(config.PoolConfig{
			MaxSize:          50,
			MaxIdlePerUser:   10,
			MaxPerWorkspace:  4,
			MaxMemoryPerUser: 5 * 1024 * 1024 * 1024,
		})
	}
	wg.Wait()

	// 收敛后:本 pool 的所有 slot 应已释放。用 per-instance Stats (race-free),
	// NOT package-global metricActiveSessions (shared → flaky).
	require.Eventually(t, func() bool {
		total, _, _ := p.Stats()
		return total == 0
	}, time.Second, 10*time.Millisecond)
}

// TestUpdateLimits_MemoryQuotaToggleOff: hot-reloading max_memory_per_user from
// positive to 0 (disable) must not strand userMemory entries or inflate the
// memory_reserved snapshot. Before the fix, releaseMemoryLocked skipped cleanup
// when maxMemoryPerUser==0, so the running total leaked forever (spec ⑤ review #2).
func TestUpdateLimits_MemoryQuotaToggleOff(t *testing.T) {
	t.Parallel()
	p := NewPoolManagerWithWorkspace(slog.Default(), 100, 5, 10*1024*1024*1024, 3)
	ctx := context.Background()

	// 2 个 workspace session,每个预留 512MB → total 1GB
	require.NoError(t, p.AcquireForWorkspace(ctx, "u1", "ws-1"))
	require.NoError(t, p.AcquireForWorkspace(ctx, "u1", "ws-1"))

	p.mu.Lock()
	require.Equal(t, int64(2*workerMemoryEstimate), p.totalMemoryReservedBytes)
	require.NotEmpty(t, p.userMemory)
	p.mu.Unlock()

	// 热重载禁用内存层
	p.UpdateLimits(config.PoolConfig{
		MaxSize: 100, MaxIdlePerUser: 5, MaxPerWorkspace: 3, MaxMemoryPerUser: 0,
	})

	// 禁用瞬间:gauge 源(totalMemoryReservedBytes)立即归零,userMemory 清空
	p.mu.Lock()
	totalAfterToggle := p.totalMemoryReservedBytes
	memMapAfterToggle := len(p.userMemory)
	p.mu.Unlock()
	require.Equal(t, int64(0), totalAfterToggle, "totalMemoryReservedBytes must reset to 0 on disable")
	require.Equal(t, 0, memMapAfterToggle, "userMemory must be cleared on disable")

	// release 不产生负值/泄漏
	p.ReleaseForWorkspace(ctx, "u1", "ws-1")
	p.ReleaseForWorkspace(ctx, "u1", "ws-1")
	p.mu.Lock()
	finalTotal := p.totalMemoryReservedBytes
	p.mu.Unlock()
	require.Equal(t, int64(0), finalTotal, "no leak/negative after release post-disable")
}
