# WebChat 多租户 spec ⑤：配额增强 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 PoolManager 的 4 个限额全部支持运行时热重载（降额不驱逐），并新增 4 个低基数聚合 Prometheus gauge。

**Architecture:** 引入 `pool.Limits` struct 镜像 `config.PoolConfig`，`UpdateLimits(Limits)` 替换旧二参签名；`snapshotMetricsLocked()` 在持锁临界区统一写 5 个 atomic 快照变量（4 新 gauge + 1 现有 utilization），gauge 回调无锁读。config watcher 解锁 2 个 pool 键、config 校验拒绝负数、gateway_run 热重载回调传完整 Limits。不改计账模型、不加 per-workspace 内存层（与并发层等价，冗余）。

**Tech Stack:** Go 1.24 · `sync.Mutex` + `sync/atomic` · OpenTelemetry metric（`go.opentelemetry.io/otel/metric`）· Viper config · testify/require · table-driven + `-race`

**关联设计:** [`docs/specs/WebChat-Multitenancy-Quota-Enhancement-Design-Spec.md`](../../specs/WebChat-Multitenancy-Quota-Enhancement-Design-Spec.md)
**Issue:** #754 · **PR:** #755 · **分支:** `feat/webchat-multitenancy-spec5-quota-enhancement`

---

## File Structure

| 文件 | 职责 | 改动 |
|---|---|---|
| `internal/session/pool.go` | PoolManager 核心：限额计数 + 指标快照 | 新增 `Limits` struct、`UpdateLimits(Limits)`、`snapshotMetricsLocked()`、4 atomic + 4 gauge；移除旧二参 `UpdateLimits` 与散落 `setPoolUtilization` 调用 |
| `internal/session/pool_test.go` | pool 单元测试 | 新增热重载 + 不驱逐测试 |
| `internal/session/pool_metrics_test.go` | 指标快照测试（新文件） | 新增 4 gauge 快照断言测试 |
| `internal/config/watcher.go` | 热重载字段白名单 | 白名单加 `pool.max_memory_per_user` / `pool.max_per_workspace` |
| `internal/config/watcher_test.go` | watcher 测试 | 新增 pool 键热重载白名单断言 |
| `internal/config/config.go` | 配置校验 | `pool.max_*` 4 字段拒绝负数 |
| `internal/config/config_test.go` | 校验测试 | 扩展 `TestConfig_Validate` 表格加负数用例 |
| `cmd/hotplex/gateway_run.go` | 热重载回调接线 | 4 字段变更触发 `UpdateLimits(Limits)` |

**不动**：`acquireLocked` / `ReleaseForWorkspace` / `AcquireForWorkspace` 的校验与计账逻辑（spec ⑤ 不改计账模型）；`NewPoolManager` / `NewPoolManagerWithWorkspace` 构造函数签名（保持向后兼容）；`PoolAcquire{result}` counter（已覆盖 4 种拒绝原因）。

---

## Task 1: `pool.Limits` struct + `UpdateLimits(Limits)`

把热重载入口从二参签名升级为 struct，覆盖全部 4 个限额字段。先写测试锁定新签名行为，再实现。

**Files:**
- Modify: `internal/session/pool.go:41-54`（struct 字段）、`pool.go:233-246`（UpdateLimits）
- Test: `internal/session/pool_test.go`（新增）

- [ ] **Step 1: 写失败测试 — 4 字段热重载**

追加到 `internal/session/pool_test.go` 末尾：

```go
// TestUpdateLimits_AllFourHotReload: UpdateLimits applies all four quota fields;
// new Acquire calls observe the new limits immediately (spec ⑤).
func TestUpdateLimits_AllFourHotReload(t *testing.T) {
	t.Parallel()
	// 初始：global 100, per-user 5, per-ws 3, per-user-mem 10GB
	p := NewPoolManagerWithWorkspace(slog.Default(), 100, 5, 10*1024*1024*1024, 3)
	ctx := context.Background()

	// 占用一个 ws-1 slot
	require.NoError(t, p.AcquireForWorkspace(ctx, "u1", "ws-1"))

	// 热重载收紧全部 4 维：global 1, per-user 1, per-ws 1, per-user-mem 1B
	p.UpdateLimits(Limits{
		MaxSize:          1,
		MaxIdlePerUser:   1,
		MaxPerWorkspace:  1,
		MaxMemoryPerUser: 1,
	})

	// global 已 1（u1 占着），另一用户被拒
	var pe *PoolError
	err := p.Acquire(ctx, "u2")
	require.ErrorAs(t, err, &pe)
	require.Equal(t, poolErrKindExhausted, pe.Kind)

	// per-ws 已 1，ws-2 的新 slot 因 per-user=1 被 u1 占用而拒（per-user 先于 per-ws 校验）
	err = p.AcquireForWorkspace(ctx, "u1", "ws-2")
	require.ErrorAs(t, err, &pe)
	require.Equal(t, poolErrKindUserQuotaExceeded, pe.Kind)

	// 释放 u1 后，u1 在 ws-2 可拿 1 个 slot（per-ws=1 对新 ws 未满）
	p.ReleaseForWorkspace(ctx, "u1", "ws-1")
	require.NoError(t, p.AcquireForWorkspace(ctx, "u1", "ws-2"))
}
```

注意：`pool_test.go` 现有 import 已含 `context`/`require`/`config`，需追加 `"log/slog"`（`pool_workspace_test.go` 已用 `slog.Default()`，确认 pool_test.go 是否已 import；若无则加）。

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/session/ -run TestUpdateLimits_AllFourHotReload -count=1`
Expected: 编译失败 —— `undefined: Limits`（struct 尚未定义）。

- [ ] **Step 3: 实现 `Limits` struct**

在 `internal/session/pool.go` 的 `PoolManager` struct 定义之后（约 `pool.go:54` 后）、`workerMemoryEstimate` 常量之前，插入：

```go
// Limits 是 PoolManager 的运行时限额集合，镜像 config.PoolConfig。
// 所有字段 0 = unlimited。UpdateLimits 在持 p.mu 下原子覆盖全部字段。
type Limits struct {
	MaxSize          int   // 全局最大活跃 Worker
	MaxIdlePerUser   int   // per-user 最大并发 session
	MaxPerWorkspace  int   // WebChat per-workspace 并发（spec ①）
	MaxMemoryPerUser int64 // bytes；per-user 内存
}
```

- [ ] **Step 4: 实现 `UpdateLimits(Limits)`，替换旧二参版**

把 `pool.go:233-246` 的旧 `UpdateLimits(maxSize, maxIdlePerUser int)` 整体替换为：

```go
// UpdateLimits 动态调整全部 4 个限额（spec ⑤）。
// 若某维限额被降到当前占用之下，已运行 session 不被驱逐 —— 新 Acquire 将被拒，
// 直到 session 自然 Release。所有 gauge 快照在此重算（utilization 的分母 maxSize 可能变了）。
func (p *PoolManager) UpdateLimits(l Limits) {
	p.mu.Lock()
	defer p.mu.Unlock()
	old := Limits{
		MaxSize:          p.maxSize,
		MaxIdlePerUser:   p.maxIdlePerUser,
		MaxPerWorkspace:  p.maxPerWorkspace,
		MaxMemoryPerUser: p.maxMemoryPerUser,
	}
	p.maxSize = l.MaxSize
	p.maxIdlePerUser = l.MaxIdlePerUser
	p.maxPerWorkspace = l.MaxPerWorkspace
	p.maxMemoryPerUser = l.MaxMemoryPerUser
	p.snapshotMetricsLocked()
	p.log.Info("pool: limits updated",
		"old_max", old.MaxSize, "new_max", l.MaxSize,
		"old_per_user", old.MaxIdlePerUser, "new_per_user", l.MaxIdlePerUser,
		"old_per_ws", old.MaxPerWorkspace, "new_per_ws", l.MaxPerWorkspace,
		"old_mem_mb", old.MaxMemoryPerUser/(1024*1024), "new_mem_mb", l.MaxMemoryPerUser/(1024*1024))
}
```

> 说明：此步引用了 `snapshotMetricsLocked()`，它在 Task 2 才实现。为让本步编译通过，**先在 Task 2 Step 1 之前**临时在 `pool.go` 加一个空壳 `func (p *PoolManager) snapshotMetricsLocked() {}`，Task 2 再填实现。或者把 Task 1 与 Task 2 的实现步合并执行——但为 TDD 隔离，推荐先加空壳，Task 2 替换。本计划采用「Task 2 Step 1 先加空壳」约定，故 Task 1 Step 4 后编译会因 `snapshotMetricsLocked` 未定义而失败 —— **此时立即跳到 Task 2 Step 1 补空壳**，再回 Task 1 Step 5 跑测试。

- [ ] **Step 5: 跑测试确认通过**

Run: `go test ./internal/session/ -run TestUpdateLimits_AllFourHotReload -count=1 -race`
Expected: PASS

- [ ] **Step 6: 修复 `gateway_run.go` 唯一调用点（否则包编译失败）**

`cmd/hotplex/gateway_run.go:197-201`，把：

```go
	cfgStore.RegisterFunc(func(prev, next *config.Config) {
		if prev.Pool.MaxSize != next.Pool.MaxSize || prev.Pool.MaxIdlePerUser != next.Pool.MaxIdlePerUser {
			sm.Pool().UpdateLimits(next.Pool.MaxSize, next.Pool.MaxIdlePerUser)
		}
	})
```

替换为：

```go
	cfgStore.RegisterFunc(func(prev, next *config.Config) {
		if prev.Pool.MaxSize != next.Pool.MaxSize ||
			prev.Pool.MaxIdlePerUser != next.Pool.MaxIdlePerUser ||
			prev.Pool.MaxPerWorkspace != next.Pool.MaxPerWorkspace ||
			prev.Pool.MaxMemoryPerUser != next.Pool.MaxMemoryPerUser {
			sm.Pool().UpdateLimits(session.Limits{
				MaxSize:          next.Pool.MaxSize,
				MaxIdlePerUser:   next.Pool.MaxIdlePerUser,
				MaxPerWorkspace:  next.Pool.MaxPerWorkspace,
				MaxMemoryPerUser: next.Pool.MaxMemoryPerUser,
			})
		}
	})
```

确认 `gateway_run.go` 是否已 import `internal/session`（应已 import，因 `sm` 即 `*session.Manager`）。若未直接引用 `session` 包名，加 `"github.com/hrygo/hotplex/internal/session"`。

- [ ] **Step 7: 全包构建确认无残留旧签名**

Run: `go build ./...`
Expected: 成功（无 `UpdateLimits has 2 arguments` 之类错误）。

- [ ] **Step 8: Commit**

```bash
git add internal/session/pool.go internal/session/pool_test.go cmd/hotplex/gateway_run.go
git commit -m "feat(pool): Limits struct + UpdateLimits(Limits) covers all 4 quotas (spec ⑤)"
```

---

## Task 2: `snapshotMetricsLocked()` 快照函数（含空壳先行）

统一 5 个 atomic 快照的写入点。先加空壳让 Task 1 编译，再填实现 + 写快照调用点。

**Files:**
- Modify: `internal/session/pool.go`（新增 4 atomic 变量 + 快照函数 + 调用点）

- [ ] **Step 1: 先加空壳（让 Task 1 Step 4 编译通过）**

若 Task 1 Step 4 后编译报 `snapshotMetricsLocked undefined`，在 `pool.go` 的 `setPoolUtilization` 函数（`pool.go:36-38`）之后插入空壳：

```go
// snapshotMetricsLocked 将当前 pool 状态写入 atomic 快照变量供 gauge 回调无锁读取。
// 调用方必须持有 p.mu。Task 2 Step 3 填实现。
func (p *PoolManager) snapshotMetricsLocked() {}
```

确认 `go build ./internal/session/` 通过后，回到 Task 1 Step 5。

- [ ] **Step 2: 写失败测试 — 快照反映状态（新文件）**

创建 `internal/session/pool_metrics_test.go`：

```go
package session

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestMetrics_SnapshotReflectsState: 4 个 atomic 快照随 Acquire 正确变化（spec ⑤）。
func TestMetrics_SnapshotReflectsState(t *testing.T) {
	t.Parallel()
	p := NewPoolManagerWithWorkspace(slog.Default(), 100, 5, 10*1024*1024*1024, 3)
	ctx := context.Background()

	require.Equal(t, int64(0), metricActiveSessions.Load())
	require.Equal(t, int64(0), metricDistinctUsers.Load())
	require.Equal(t, int64(0), metricDistinctWorkspaces.Load())
	require.Equal(t, int64(0), metricMemoryReserved.Load())

	// u1 在 ws-1 占 2 个 slot
	require.NoError(t, p.AcquireForWorkspace(ctx, "u1", "ws-1"))
	require.NoError(t, p.AcquireForWorkspace(ctx, "u1", "ws-1"))
	// u2 在 ws-2 占 1 个 slot（platform session ws=""）
	require.NoError(t, p.Acquire(ctx, "u2"))

	require.Equal(t, int64(3), metricActiveSessions.Load())
	require.Equal(t, int64(2), metricDistinctUsers.Load())        // u1, u2
	require.Equal(t, int64(2), metricDistinctWorkspaces.Load())   // ws-1, ws-2 — 注意 u2 是 platform(ws=""),不计数
	require.Equal(t, int64(3*workerMemoryEstimate), metricMemoryReserved.Load())
}
```

> ⚠️ 修正：上面 u2 用 `Acquire`（非 workspace 路径），ws=""，所以 `distinct_workspaces` 只应计 **1**（ws-1）。把断言改为：
> `require.Equal(t, int64(1), metricDistinctWorkspaces.Load())`
> （u2 的 platform session 不进 workspace 维度）。在写测试时用这个修正值。

- [ ] **Step 3: 运行测试确认失败**

Run: `go test ./internal/session/ -run TestMetrics_SnapshotReflectsState -count=1`
Expected: 编译失败 —— `undefined: metricActiveSessions`（atomic 变量未定义）+ `snapshotMetricsLocked` 是空壳（值恒 0，但编译先挂）。

- [ ] **Step 4: 新增 4 个 atomic 快照变量**

在 `pool.go` 的 `var poolUtilization atomic.Uint64`（`pool.go:17`）之后插入：

```go
// 指标快照（atomic，gauge 回调无锁读取；snapshotMetricsLocked 持 mu 写）。
var (
	metricActiveSessions    atomic.Int64
	metricDistinctUsers     atomic.Int64
	metricDistinctWorkspaces atomic.Int64
	metricMemoryReserved    atomic.Int64
)
```

- [ ] **Step 5: 实现 `snapshotMetricsLocked`（替换空壳）**

把 Task 2 Step 1 的空壳替换为：

```go
// snapshotMetricsLocked 将当前 pool 状态写入 atomic 快照变量供 gauge 回调无锁读取。
// 调用方必须持有 p.mu。
func (p *PoolManager) snapshotMetricsLocked() {
	metricActiveSessions.Store(int64(p.totalCount))
	metricDistinctUsers.Store(int64(len(p.userCount)))
	metricDistinctWorkspaces.Store(int64(len(p.workspaceCount)))
	metricMemoryReserved.Store(p.totalMemoryReserved())
	if p.maxSize > 0 {
		setPoolUtilization(float64(p.totalCount) / float64(p.maxSize))
	} else {
		setPoolUtilization(0)
	}
}
```

并新增辅助方法（放在 `snapshotMetricsLocked` 之后）：

```go
// totalMemoryReserved 返回全局已预留内存（所有用户之和）。调用方持 p.mu。
func (p *PoolManager) totalMemoryReserved() int64 {
	var sum int64
	for _, m := range p.userMemory {
		sum += m
	}
	return sum
}
```

> 说明：`memory_reserved_bytes` 是全局聚合（含 platform session 的内存），与设计 §4.4 口径一致。

- [ ] **Step 6: 在 acquire/release 路径调用快照**

`internal/session/pool.go`，`acquireLocked` 末尾（现 `pool.go:151` `p.log.Debug(...)` 之前）把：

```go
	if p.maxSize > 0 {
		setPoolUtilization(float64(p.totalCount) / float64(p.maxSize))
	}
```

替换为：

```go
	p.snapshotMetricsLocked()
```

`releaseCoreLocked` 末尾（现 `pool.go:219-221`）把：

```go
	if p.maxSize > 0 {
		setPoolUtilization(float64(p.totalCount) / float64(p.maxSize))
	}
```

替换为：

```go
	p.snapshotMetricsLocked()
```

`UpdateLimits` 中的 `p.snapshotMetricsLocked()` 调用已在 Task 1 Step 4 写入，保留。

> 注意：`AcquireMemory`（deprecated，`pool.go:255-271`）也改 `userMemory`，但它走 TOCTOU 不安全路径、不计数 totalCount。为保持计数一致，在 `AcquireMemory` 成功分支末尾（`return nil` 前）也加 `p.snapshotMetricsLocked()`。同理 `ReleaseMemory`（`pool.go:277-281`）调 `releaseMemoryLocked` 后加。这 2 处是 best-effort 一致性，避免 deprecated 路径漏更新 memory gauge。

- [ ] **Step 7: 跑测试确认通过**

Run: `go test ./internal/session/ -run TestMetrics -count=1 -race`
Expected: PASS

- [ ] **Step 8: 回归全部 pool 测试**

Run: `go test ./internal/session/ -count=1 -race`
Expected: PASS（现有测试不受影响——快照是附加行为）。

- [ ] **Step 9: Commit**

```bash
git add internal/session/pool.go internal/session/pool_metrics_test.go
git commit -m "feat(pool): snapshotMetricsLocked + 4 atomic gauges (active/users/workspaces/memory) (spec ⑤)"
```

---

## Task 3: 注册 4 个 Prometheus gauge

把 atomic 快照接到 OTel observable gauge，`/metrics` 端点可见。

**Files:**
- Modify: `internal/session/pool.go:19-34`（init 注册块）

- [ ] **Step 1: 扩展 init 注册回调**

把 `pool.go:19-34` 的 `init()` 整体替换为：

```go
func init() {
	observability.RegisterGaugeCallbacks(func(m metric.Meter) {
		poolGauge, err := m.Float64ObservableGauge(
			"hotplex.pool.utilization",
			metric.WithDescription("Pool utilization ratio (0-1)"),
		)
		if err != nil {
			slog.Warn("pool: failed to create pool.utilization gauge", "err", err)
			return
		}
		activeGauge, err := m.Int64ObservableGauge(
			"hotplex.pool.active_sessions",
			metric.WithDescription("Active worker sessions (global, includes platform/cron)"),
		)
		if err != nil {
			slog.Warn("pool: failed to create pool.active_sessions gauge", "err", err)
			return
		}
		usersGauge, err := m.Int64ObservableGauge(
			"hotplex.pool.distinct_users",
			metric.WithDescription("Distinct users with at least one active session"),
		)
		if err != nil {
			slog.Warn("pool: failed to create pool.distinct_users gauge", "err", err)
			return
		}
		wsGauge, err := m.Int64ObservableGauge(
			"hotplex.pool.distinct_workspaces",
			metric.WithDescription("Distinct WebChat workspaces with at least one active session (platform sessions excluded)"),
		)
		if err != nil {
			slog.Warn("pool: failed to create pool.distinct_workspaces gauge", "err", err)
			return
		}
		memGauge, err := m.Int64ObservableGauge(
			"hotplex.pool.memory_reserved_bytes",
			metric.WithDescription("Estimated reserved memory in bytes (active sessions × 512MB, global aggregate)"),
		)
		if err != nil {
			slog.Warn("pool: failed to create pool.memory_reserved_bytes gauge", "err", err)
			return
		}
		_, _ = m.RegisterCallback(func(_ context.Context, o metric.Observer) error {
			o.ObserveFloat64(poolGauge, math.Float64frombits(poolUtilization.Load()))
			o.ObserveInt64(activeGauge, metricActiveSessions.Load())
			o.ObserveInt64(usersGauge, metricDistinctUsers.Load())
			o.ObserveInt64(wsGauge, metricDistinctWorkspaces.Load())
			o.ObserveInt64(memGauge, metricMemoryReserved.Load())
			return nil
		}, poolGauge, activeGauge, usersGauge, wsGauge, memGauge)
	})
}
```

> 注意 `RegisterCallback` 把所有 gauge 传入同一个回调（一次注册观察 5 个），与设计 §3.2 一致。`math`/`context`/`slog`/`metric` 已在 pool.go import。

- [ ] **Step 2: 写测试 — gauge 回调读快照（追加到 pool_metrics_test.go）**

```go
// TestMetrics_GaugeCallbackObservesSnapshot: OTel callback reads the atomic snapshot.
// 不启真实 Prometheus —— 直接验证 atomic 值在 Acquire 后被快照写入（gauge 回调读同一 atomic）。
func TestMetrics_GaugeCallbackObservesSnapshot(t *testing.T) {
	t.Parallel()
	p := NewPoolManagerWithWorkspace(slog.Default(), 100, 5, 0, 3)
	ctx := context.Background()

	require.NoError(t, p.AcquireForWorkspace(ctx, "u1", "ws-1"))
	require.NoError(t, p.AcquireForWorkspace(ctx, "u1", "ws-1"))

	// 快照已写（gauge 回调读这些 atomic）
	require.Equal(t, int64(2), metricActiveSessions.Load())
	require.Equal(t, int64(1), metricDistinctUsers.Load())
	require.Equal(t, int64(1), metricDistinctWorkspaces.Load())

	// 全部释放后回归基线（无泄漏）
	p.ReleaseForWorkspace(ctx, "u1", "ws-1")
	p.ReleaseForWorkspace(ctx, "u1", "ws-1")
	require.Equal(t, int64(0), metricActiveSessions.Load())
	require.Equal(t, int64(0), metricDistinctUsers.Load())
	require.Equal(t, int64(0), metricDistinctWorkspaces.Load())
	require.Equal(t, int64(0), metricMemoryReserved.Load())
}
```

- [ ] **Step 3: 跑测试**

Run: `go test ./internal/session/ -run TestMetrics -count=1 -race`
Expected: PASS

- [ ] **Step 4: 构建确认 OTel 注册无 panic**

Run: `go build ./... && go vet ./internal/session/`
Expected: 成功。

- [ ] **Step 5: Commit**

```bash
git add internal/session/pool.go internal/session/pool_metrics_test.go
git commit -m "feat(pool): register 4 Prometheus gauges (active/users/workspaces/memory) (spec ⑤)"
```

---

## Task 4: watcher 解锁 2 个 pool 热重载键

让 `pool.max_memory_per_user` / `pool.max_per_workspace` 的变更触发 onChange 回调（进而触发 Task 1 接线的 UpdateLimits）。

**Files:**
- Modify: `internal/config/watcher.go:18-34`（hotReloadableFields）
- Test: `internal/config/watcher_test.go`

- [ ] **Step 1: 写失败测试 — 白名单含 4 个 pool 键**

先看 `watcher_test.go` 是否已有遍历 `hotReloadableFields` 的测试。若无，追加：

```go
// TestHotReloadableFields_PoolQuotas: spec ⑤ — 全部 4 个 pool 限额键可热重载。
func TestHotReloadableFields_PoolQuotas(t *testing.T) {
	t.Parallel()
	required := []string{
		"pool.max_size",
		"pool.max_idle_per_user",
		"pool.max_memory_per_user",
		"pool.max_per_workspace",
	}
	for _, f := range required {
		require.True(t, hotReloadableFields[f], "missing hot-reloadable field: %s", f)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/config/ -run TestHotReloadableFields_PoolQuotas -count=1`
Expected: FAIL —— `missing hot-reloadable field: pool.max_memory_per_user`（与 max_per_workspace）。

- [ ] **Step 3: 加白名单**

`internal/config/watcher.go:21-22`，把：

```go
	"pool.max_size":             true,
	"pool.max_idle_per_user":    true,
```

替换为：

```go
	"pool.max_size":             true,
	"pool.max_idle_per_user":    true,
	"pool.max_memory_per_user":  true, // spec ⑤
	"pool.max_per_workspace":    true, // spec ⑤
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/config/ -run TestHotReloadableFields -count=1 -race`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/watcher.go internal/config/watcher_test.go
git commit -m "feat(config): hot-reload pool.max_memory_per_user + pool.max_per_workspace (spec ⑤)"
```

---

## Task 5: config 校验拒绝负数

把 `pool.max_*` 的校验从「max_size 必须 >0」扩展到「4 字段均 >= 0」（0 = unlimited，负数非法）。trust config 层为唯一校验点，pool 内部不重复校验。

**Files:**
- Modify: `internal/config/config.go:84-86`
- Test: `internal/config/config_test.go:90-98`（扩展表格）

- [ ] **Step 1: 写失败测试 — 负数被拒**

在 `internal/config/config_test.go` 的 `TestConfig_Validate` 表格里，`"non-positive pool max size"` 用例（约 `config_test.go:90-98`）之后插入新用例：

```go
		{
			name: "negative pool quota fields (spec ⑤)",
			cfg: func() Config {
				c := *Default()
				c.Pool.MaxIdlePerUser = -1
				return c
			}(),
			errCnt: 1, // negative per-user quota
		},
		{
			name: "negative pool max_per_workspace (spec ⑤)",
			cfg: func() Config {
				c := *Default()
				c.Pool.MaxPerWorkspace = -1
				return c
			}(),
			errCnt: 1,
		},
		{
			name: "negative pool max_memory_per_user (spec ⑤)",
			cfg: func() Config {
				c := *Default()
				c.Pool.MaxMemoryPerUser = -1
				return c
			}(),
			errCnt: 1,
		},
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/config/ -run "TestConfig_Validate" -count=1`
Expected: FAIL —— 3 个新用例 `errCnt` 为 0（负数当前不被拒）。

- [ ] **Step 3: 实现校验**

`internal/config/config.go:84-86`，把：

```go
	if c.Pool.MaxSize <= 0 {
		errs = append(errs, "pool.max_size must be positive")
	}
```

替换为：

```go
	if c.Pool.MaxSize <= 0 {
		errs = append(errs, "pool.max_size must be positive")
	}
	if c.Pool.MaxIdlePerUser < 0 {
		errs = append(errs, "pool.max_idle_per_user must be non-negative")
	}
	if c.Pool.MaxPerWorkspace < 0 {
		errs = append(errs, "pool.max_per_workspace must be non-negative")
	}
	if c.Pool.MaxMemoryPerUser < 0 {
		errs = append(errs, "pool.max_memory_per_user must be non-negative")
	}
```

> 注意：`MaxSize` 保持 `<= 0` 报错（pool 必须有全局上限，0 不表示 unlimited 而表示非法）。其余 3 字段 `< 0` 报错、`0` 合法（= unlimited），与 PoolConfig 注释一致。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/config/ -count=1 -race`
Expected: PASS（含现有 `TestDefault` 等不受影响——默认值均 >= 0）。

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): reject negative pool quota fields (spec ⑤)"
```

---

## Task 6: 降额不驱逐 + race 压测

锁定"热重载降额后已运行 session 不被强制释放"的不变量，并做并发压测。

**Files:**
- Test: `internal/session/pool_test.go`

- [ ] **Step 1: 写测试 — 降额不驱逐**

追加到 `pool_test.go`：

```go
// TestUpdateLimits_DoesNotEvict: 降低内存限额后，已占用超额的 user 不被强制释放；
// 仅新 Acquire 被拒，直到自然 Release（spec ⑤ §4.3 不变量）。
func TestUpdateLimits_DoesNotEvict(t *testing.T) {
	t.Parallel()
	// per-user-mem 10GB → 1 个 slot 占 512MB，可拿多个
	p := NewPoolManagerWithWorkspace(slog.Default(), 100, 5, 10*1024*1024*1024, 3)
	ctx := context.Background()

	// u1 占 2 slot（≈1GB）
	require.NoError(t, p.AcquireForWorkspace(ctx, "u1", "ws-1"))
	require.NoError(t, p.AcquireForWorkspace(ctx, "u1", "ws-1"))

	// 降到 512MB —— u1 现已超额（用了 1GB）
	p.UpdateLimits(Limits{
		MaxSize:          100,
		MaxIdlePerUser:   5,
		MaxPerWorkspace:  3,
		MaxMemoryPerUser: workerMemoryEstimate, // 512MB
	})

	// 已占用的 u1 计数仍在（未被驱逐）
	total, _, _ := p.Stats()
	require.Equal(t, 2, total)
	require.Equal(t, int64(2*workerMemoryEstimate), p.UserMemory("u1"))

	// u1 新 Acquire 被内存维度拒（超额）
	var pe *PoolError
	err := p.AcquireForWorkspace(ctx, "u1", "ws-1")
	require.ErrorAs(t, err, &pe)
	require.Equal(t, poolErrKindMemoryExceeded, pe.Kind)

	// 释放 1 个后，内存回到 512MB，可再拿 1 个（恰好等于上限）
	p.ReleaseForWorkspace(ctx, "u1", "ws-1")
	require.NoError(t, p.AcquireForWorkspace(ctx, "u1", "ws-1"))
}
```

- [ ] **Step 2: 跑测试**

Run: `go test ./internal/session/ -run TestUpdateLimits_DoesNotEvict -count=1 -race`
Expected: PASS

- [ ] **Step 3: 写 race 压测 — 并发 Acquire/Release/UpdateLimits**

追加到 `pool_test.go`：

```go
// TestPool_ConcurrentMixedOperations: 并发 Acquire/Release/UpdateLimits 不产生
// 数据竞争或计数漂移（spec ⑤ §5.6 race 场景）。
func TestPool_ConcurrentMixedOperations(t *testing.T) {
	t.Parallel()
	p := NewPoolManagerWithWorkspace(slog.Default(), 50, 10, 5*1024*1024*1024, 4)
	ctx := context.Background()

	done := make(chan struct{})
	// Acquire/Release worker
	go func() {
		defer close(done)
		for i := 0; i < 200; i++ {
			uid := "u" + strconv.Itoa(i%5)
			ws := "ws-" + strconv.Itoa(i%3)
			if p.AcquireForWorkspace(ctx, uid, ws) == nil {
				p.ReleaseForWorkspace(ctx, uid, ws)
			}
		}
	}()
	// UpdateLimits worker — 在 Acquire/Release 跑的同时热重载
	for i := 0; i < 20; i++ {
		p.UpdateLimits(Limits{
			MaxSize:          50,
			MaxIdlePerUser:   10,
			MaxPerWorkspace:  4,
			MaxMemoryPerUser: 5 * 1024 * 1024 * 1024,
		})
	}
	<-done

	// 收敛后：所有 slot 应已释放，快照归零
	require.Eventually(t, func() bool {
		return metricActiveSessions.Load() == 0
	}, time.Second, 10*time.Millisecond)
}
```

需追加 import：`"strconv"`、`"time"`（确认 pool_test.go 现有 import，缺则补）。

- [ ] **Step 4: 跑 race 压测**

Run: `go test ./internal/session/ -run TestPool_ConcurrentMixedOperations -count=1 -race`
Expected: PASS（无 `DATA RACE` 报告，计数收敛归零）。

- [ ] **Step 5: Commit**

```bash
git add internal/session/pool_test.go
git commit -m "test(pool): no-evict on downgrade + concurrent Acquire/Release/UpdateLimits race (spec ⑤)"
```

---

## Task 7: 全量验证 + 路线图收尾

跑完整质量门禁，确认无回归；更新路线图标 spec ⑤ 已交付。

**Files:**
- Modify: `docs/specs/WebChat-Multitenancy-Roadmap-Spec.md`

- [ ] **Step 1: 全量测试（含 race）**

Run: `go test ./internal/session/ ./internal/config/ ./cmd/hotplex/ -count=1 -race`
Expected: PASS

- [ ] **Step 2: 完整 CI 门禁**

Run: `make check`
Expected: 通过（fmt + lint + vet + mod verify + build + test）。

- [ ] **Step 3: 更新路线图**

`docs/specs/WebChat-Multitenancy-Roadmap-Spec.md`：

顶部状态行，把 `spec ③ 已合入（PR #753）` 之后补 spec ⑤ 状态。找到：

```
**状态**: spec ① 已合入（[PR #746]...）；spec ② 已合入（[PR #748]...）；spec ③ 已合入（[PR #753]...，`207d47e3`）；④-⑥ 待逐个 brainstorm
```

替换为：

```
**状态**: spec ① 已合入（[PR #746]...）；spec ② 已合入（[PR #748]...）；spec ③ 已合入（[PR #753]...，`207d47e3`）；spec ⑤ 已合入（[PR #755]...）；④/⑥ 待逐个 brainstorm
```

（PR #755 合并后再填 commit hash；未合并前用 PR 号占位即可）

§3 阶段 C 表格，spec ⑤ 行：

```markdown
| ⑤ | 多租户配额增强 | ①② | ✅ 已合入（[PR #755](https://github.com/hrygo/hotplex/pull/755)）：`Limits` struct 4 限额热重载 + 4 聚合 gauge，[设计](./WebChat-Multitenancy-Quota-Enhancement-Design-Spec.md) |
```

§4 spec ⑤ 段落标题改为 `### spec ⑤ — 多租户配额增强（✅ 已合入 PR #755）` 并在段首加交付摘要一句。

§7 "下一步"段落更新为指向 spec ④/⑥。

- [ ] **Step 4: Commit 路线图**

```bash
git add docs/specs/WebChat-Multitenancy-Roadmap-Spec.md
git commit -m "docs(roadmap): mark spec ⑤ merged (PR #755)"
```

- [ ] **Step 5: 推送分支**

```bash
git push origin feat/webchat-multitenancy-spec5-quota-enhancement
```

PR #755 将自动更新，CI 重跑。等待 review。

---

## Self-Review(计划自审)

**Spec 覆盖**：
- §1.2 验收「4 限额热重载」→ Task 1（UpdateLimits）+ Task 4（watcher 解锁）+ Task 1 Step 6（gateway_run 接线）。✅
- §1.2 验收「4 gauge /metrics 可见」→ Task 2（快照）+ Task 3（注册）。✅
- §1.2 验收「降额不驱逐」→ Task 6 Step 1。✅
- §1.2 验收「make check」→ Task 7 Step 2。✅
- §4.1 负数校验 → Task 5。✅
- §4.3 不变量 → Task 6。✅
- §4.4 双轨口径（distinct_workspaces 排除 platform）→ Task 2 Step 5（totalMemoryReserved 全局聚合）+ Task 3 gauge 描述 + Task 2 测试修正注释。✅
- §5 全部测试 → Task 1/2/3/4/5/6。✅

**占位符扫描**：无 TBD/TODO；每个代码步含完整代码；Task 1 Step 4 的 `snapshotMetricsLocked` 前向引用已在同 Step 显式说明「Task 2 Step 1 补空壳」并给出空壳代码，非占位符。✅

**类型一致性**：
- `Limits` struct 字段（MaxSize int / MaxIdlePerUser int / MaxPerWorkspace int / MaxMemoryPerUser int64）与 `config.PoolConfig`（config_types.go:614-620）完全对齐。✅
- `UpdateLimits(Limits)` 签名在 Task 1 定义、Task 6 调用一致。✅
- 4 个 atomic 变量名（metricActiveSessions / metricDistinctUsers / metricDistinctWorkspaces / metricMemoryReserved）在 Task 2 Step 4 定义、Step 5/Task 3/Task 6 引用一致。✅
- `snapshotMetricsLocked()` 命名（Locked 后缀）与现有 `releaseMemoryLocked`/`releaseCoreLocked` 风格一致。✅
- `totalMemoryReserved()` 辅助方法在 Task 2 Step 5 定义并被 snapshotMetricsLocked 调用。✅

**已知权衡**：
- Task 2 Step 2 测试注释里 `distinct_workspaces` 断言值需用修正后的 `1`（u2 platform session 不计），已在 Step 2 内联标注。实现者注意以修正值为准。
- Task 2 Step 6 给 deprecated `AcquireMemory`/`ReleaseMemory` 补快照调用是 best-effort 一致性；这俩方法标注 Deprecated 且主路径用 `AcquireWithMemory`/`AcquireForWorkspace`，补丁避免 memory gauge 在 deprecated 路径漂移。
- 未加真实 Prometheus e2e（设计 §5.4 明示靠单元测试 + 键白名单组合保证）。
