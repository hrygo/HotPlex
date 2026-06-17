# WebChat 多租户 spec ⑤：配额增强（热重载完整化 + 聚合指标）

**日期**: 2026-06-17
**状态**: 设计稿（待 plan）
**分支**: main · **基线**: v1.29.1（`207d47e3`，含 spec ③ PR #753）
**路线图**: [`WebChat-Multitenancy-Roadmap-Spec.md`](./WebChat-Multitenancy-Roadmap-Spec.md) §4 spec ⑤
**前置**: spec ①（PR #746，PoolManager 三层并发 + per-user 内存）、spec ②（PR #748，per-workspace agent-configs）

---

## 1. 目标与范围

**目标**：补齐 PoolManager 配额体系的两个真实缺口，使多租户配额可动态调整、可观测，且不引入与现有机制冗余的能力。

**范围（仅 2 项）**：

1. **热重载完整化** — 4/4 限额（`max_size` / `max_idle_per_user` / `max_memory_per_user` / `max_per_workspace`）全部支持运行时热重载；降额不驱逐已运行 session。
2. **低基数聚合指标** — 新增 4 个 gauge 反映 pool 当前状态；复用现有拒绝原因 counter。

### 1.1 明确不做（YAGNI，已 brainstorm 拍板）

- **per-workspace 内存配额层** — 固定估算（512MB/worker）下与 `max_per_workspace` 并发层数学等价（内存 = 会话数 × 512MB），两者同时设则紧者恒生效、松者永不触发，属功能冗余。仅在改用 worker-type 区分估算后才有独立价值，而本 spec 沿用固定估算。
- **计费 / 用量统计 / 历史落盘** — 路线图 §6.3 拍板为纯配额增强；用量台账/计费基础若将来需要，另立 spec。
- **per-workspace 单独配额** — 全局统一值已满足"每人相同上限"的公平语义；per-workspace 可配需动 `workspaces` 表 + admin API，YAGNI。
- **worker-type 区分内存估算** — 沿用固定 `workerMemoryEstimate = 512MB`（历史常量，沿用 spec ①；本项目已停止调用 RLIMIT_AS，见 `proc/memlimit_linux.go`，故该值仅作粗略记账单位，不对应进程级内存硬限）。
- **降额主动驱逐** — 自然释放，运维主动降额的代价由"期间新 Acquire 被拒"承担，spec 文档明示。

### 1.2 验收标准

- 运行中实例修改 `config.yaml` 的任一 `pool.max_*` → 不重启即生效（`UpdateLimits` 被调用，日志 `"pool: limits updated"` 含 4 个新旧值）。
- 4 个新 gauge 在 `/metrics` 端点可见，值随 session 增减正确变化。
- 降额后已超额 session 不被中断，仅新 `Acquire` 被拒。
- `make check` 通过（含 `-race`）。

---

## 2. 架构与组件

改动集中在 3 个文件，无新包、无跨模块依赖。

```
internal/session/pool.go        ← 核心:UpdateLimits(config.PoolConfig) + snapshotMetricsLocked + 5 atomic gauge 快照
internal/config/watcher.go      ← 解锁 2 个热重载键
cmd/hotplex/gateway_run.go      ← 热重载回调传 next.Pool
```

### 2.1 组件 1：`UpdateLimits` 收 `config.PoolConfig`（pool.go）

直接复用 `config.PoolConfig` 作为运行时限额载体，不再维护独立的 `session.Limits` 镜像 struct——逐字段复制 `config.PoolConfig` 会在新增字段时静默漂移（漏传 / 漏热重载检测）。

```go
// UpdateLimits 动态调整全部 4 个限额。替换旧的 (maxSize, maxIdlePerUser) 二参签名。
// 仅消费 PoolConfig 的 MaxSize / MaxIdlePerUser / MaxPerWorkspace / MaxMemoryPerUser
// 四字段（MinSize 忽略）。若某维被降到当前占用之下，已运行 session 不被驱逐 ——
// 新 Acquire 将被拒，直到 session 自然 Release。
func (p *PoolManager) UpdateLimits(l config.PoolConfig)
```

- `0 = unlimited` 约定与 `config.PoolConfig` 一致。
- 旧二参 `UpdateLimits(maxSize, maxIdlePerUser int)` 唯一调用点是 `gateway_run.go:199`，直接迁移为新签名。调用点单一，且 pool 包无外部 SDK 消费者，**不留废弃别名**（避免 backwards-compat shim，符合项目反模式规范）。

### 2.2 组件 2：热重载解锁（watcher.go）

`config/watcher.go:21-22` 当前仅白名单 2 键：

```go
"pool.max_size":          true,
"pool.max_idle_per_user": true,
```

加入：

```go
"pool.max_memory_per_user": true,
"pool.max_per_workspace":   true,
```

watcher 检测到任一 `pool.*` 变更 → 触发现有回调 → `gateway_run.go:199` 热重载回调用 `prev.Pool != next.Pool` 判变更，直接传 `next.Pool`（`config.PoolConfig`）调 `UpdateLimits`。

### 2.3 组件 3：聚合指标（pool.go）

复用现有 `init()` + `observability.RegisterGaugeCallbacks` 模式（pool.go:19-34），新增 4 个 observable gauge：

| 指标 | 类型 | 口径 |
|---|---|---|
| `hotplex.pool.active_sessions` | int64 | 全局活跃 session 总数（含 platform/cron） |
| `hotplex.pool.distinct_users` | int64 | 去重 user 数 |
| `hotplex.pool.distinct_workspaces` | int64 | 去重 WebChat workspace 数（`workspaceID != ""`） |
| `hotplex.pool.memory_reserved_bytes` | int64 | per-user 内存配额下已预留字节（仅 `max_memory_per_user` 设置时累计；512MB/worker 估算，非真实 RSS） |

保留现有 `hotplex.pool.utilization`（float64，基于 `maxSize`）与 `PoolAcquire{result=...}` counter（已覆盖 4 种拒绝原因：`pool_exhausted` / `user_quota_exceeded` / `workspace_quota_exceeded` / `memory_exceeded`），零改动。

**快照加锁设计（关键）**：

当前 `poolUtilization` 用单个 `atomic.Uint64`（存 `math.Float64bits`）规避锁，因为只存一个标量。4 个新 gauge 若各自在回调里 `mu.Lock` 会重复加锁且与 Acquire/Release 争用。改为：

- 包级新增 4 个 `atomic.Int64`（active / distinct_users / distinct_workspaces / memory_reserved），保留现有 1 个 `atomic.Uint64`（utilization，float64 bits 技巧）。共 5 个独立 atomic 变量，沿用现有 `poolUtilization` 的单标量模式 —— **不引入聚合 struct**，保持与现有代码风格一致。
- **写快照**：新增 `snapshotMetricsLocked()`，在 `acquireLocked` / `releaseCoreLocked` / `UpdateLimits` 末尾各调一次（**调用方已持 `mu`**，函数名带 `Locked` 后缀表此约定，与现有 `releaseMemoryLocked` / `releaseCoreLocked` 同风格）。一次 `mu` 临界区内计算全部 5 值并 `atomic.Store`。
- **读快照**：gauge 回调 `atomic.Load`，无锁。Prometheus 高频 scrape 不与配额操作争 `mu`。

`snapshotMetricsLocked()` 取代现有散落的 `setPoolUtilization(...)` 调用点（pool.go:149 / :220 / :243），统一为单点维护。`setPoolUtilization` 本身保留为 `snapshotMetricsLocked` 内部使用的辅助，或内联——实现时定。

### 2.4 不动的部分

`AcquireForWorkspace` / `ReleaseForWorkspace` / `acquireLocked` 的配额**校验逻辑零改动**。spec ⑤ 不改计账模型（仍是 `userCount` / `workspaceCount` / `userMemory` 三个 map + totalCount），只改：

- "限额从哪来"：`maxPerWorkspace` / `maxMemoryPerUser` 现已可热更新（字段早就在 struct 里，只是 `UpdateLimits` 没覆盖它们）。
- "状态怎么导出"：新增聚合快照。

---

## 3. 数据流

### 3.1 流 A：热重载（运行中改配置）

```
config.yaml 改 pool.max_memory_per_user: 3GB → 1GB
        │
        ▼
config watcher 检测变更（4 个 pool.* 键均在白名单）
        │
        ▼
gateway_run.go:199 热重载回调（prev.Pool != next.Pool 判变更）
        │  直接传 next.Pool（config.PoolConfig）
        ▼
pool.UpdateLimits(next.Pool)
        │  mu.Lock → 覆盖 4 字段 →（maxMemoryPerUser >0→0 时清空 userMemory/total）→ snapshotMetricsLocked() → mu.Unlock
        ▼
日志: "pool: limits updated"（含 4 个新旧值）
        │
        ▼
✅ 新 Acquire 立即按新阈值校验
   ❌ 不驱逐：已运行的 2GB session 继续跑，Release 时自然回归
```

### 3.2 流 B：指标采集（Prometheus scrape）

```
Prometheus /metrics scrape
        │
        ▼
OTel callback 触发（5 gauge 共用一次注册回调）
        │
        ▼
读包级 atomic 快照（无锁）
        │  active_sessions / distinct_users / distinct_workspaces / memory_reserved / utilization
        ▼
o.ObserveInt64(...) × 4 + o.ObserveFloat64(utilization)
```

### 3.3 快照写入点

每次 pool 状态变更，持 `p.mu` 内调用 `snapshotMetricsLocked()`：

| 触发位置 | 动作 |
|---|---|
| `acquireLocked` 末尾 | 计数自增后 |
| `releaseCoreLocked` 末尾 | 计数自减后（`Release` / `ReleaseForWorkspace` 共用） |
| `ReleaseForWorkspace` 末尾 | `workspaceCount` 递减后**再快照一次**——`releaseCoreLocked` 的快照早于 workspace 递减，否则 `distinct_workspaces` 慢一拍（见 §4.7） |
| `UpdateLimits` 内 | 覆盖阈值后（utilization 分母 `maxSize` 变化需重算） |
| `AcquireMemory` / `ReleaseMemory` 末尾 | deprecated 路径同样维护快照，保证 memory-only 操作不丢指标 |

### 3.4 流 C：拒绝路径（无变化，仅复用）

现有 `acquireLocked` 4 段校验 + `PoolAcquire{result=...}` counter 已覆盖全部拒绝原因。spec ⑤ 零改动。

---

## 4. 错误处理与边界

### 4.1 配置校验（config 包，扩展现有）

- 现状 `config.go:85` 已有 `"pool.max_size must be positive"` 校验。
- 扩展：`max_size` 保持 `> 0`（pool 必须有全局上限，0 非法）；其余 3 字段（`max_idle_per_user` / `max_per_workspace` / `max_memory_per_user`）允许 `0`（= unlimited），仅拒绝负数。加载时报错汇总，拒绝启动。
- 内存字段单位 = bytes。

### 4.2 热重载边界

- watcher 解析失败 → 现有策略：日志 + 保留旧配置继续运行（`config.Watch` 回调内）。spec ⑤ 沿用，不新增。
- `UpdateLimits` 收到非法值（负数）→ **trust config 层**（单一校验点，避免重复）。config loader 是唯一边界，pool 是内部消费者，不重复校验。若直接调用方（测试）传负数，pool 不 panic，行为按字段原值处理（不 clamp，避免掩盖调用 bug）。

### 4.3 降额不驱逐的不变量

- `UpdateLimits` 只写字段，不遍历 session、不调 Terminate。已超额状态是暂态 —— Release 自然收敛。
- **memory 层禁用（`>0→0`）即时清零**：`UpdateLimits` 检测到 `max_memory_per_user` 从正数降为 0 时，立即清空 `userMemory` 与 `totalMemoryReservedBytes`，让 `memory_reserved_bytes` 即时归零。`releaseMemoryLocked` 已去掉 `maxMemoryPerUser>0` 守卫、本可随 drain 清理，但即时清零避免 gauge 滞后到所有 session Release。反向（`0→正数`）不重建——无法在不新增计数器的前提下得知哪些活跃 session 占内存，该方向仅 under-count（宽松），旧 session Release 后自洽。
- **风险明示**：若 `max_memory_per_user` 从 1GB 降到 512MB 而用户正占 1GB，期间该用户新 Acquire 永远被拒，直到释放。这是运维主动降额的预期代价。

### 4.4 双轨隔离（spec ① 既有约束，spec ⑤ 保持）

- `workspaceID == ""` 的 platform/cron session 跳过 per-workspace 校验层（现有逻辑，不动）。
- 指标口径：`distinct_workspaces` 只计 WebChat workspace（`workspaceID != ""`）；`active_sessions` / `memory_reserved_bytes` 是全局聚合（含 platform session）。gauge 描述里注明口径。

### 4.5 指标边界

- gauge 回调读快照失败 → 不可能（`atomic.Load` 永不出错），无 fallback 需要。
- Prometheus 不在线 → OTel noop 降级（`RegisterGaugeCallbacks` 已有 `sync.Once` + noop，现有机制）。

### 4.6 并发安全

- 所有限额字段读写均在 `p.mu` 内；gauge 读走 atomic 快照，不碰 `mu`。
- `UpdateLimits` 与 Acquire/Release 互斥 —— 无新竞态。
- 快照写（持 `mu` 时 `atomic.Store`）与快照读（gauge 回调 `atomic.Load`）—— 标准 atomic 模式，无撕裂。
- **单实例假设**：5 个 atomic 快照变量是包级全局，由唯一 `PoolManager` 实例写入。多实例会互相覆盖；若未来引入 per-tenant pool，需移到 struct 并改为捕获实例的回调（代码内已留 TODO）。

### 4.7 distinct_workspaces 快照时机

`releaseCoreLocked`（被 `Release` / `ReleaseForWorkspace` 共用）在末尾调 `snapshotMetricsLocked()`，但 `ReleaseForWorkspace` 在其返回**后**才递减 `workspaceCount`。若不补快照，`distinct_workspaces` 会慢一个 release，且 pool 排空后永久卡在非零值。故 `ReleaseForWorkspace` 在 workspace 递减后**再调一次** `snapshotMetricsLocked()`。`acquireLocked` 的 `workspaceCount++` 在快照之前，故 acquire 侧无此问题。

---

## 5. 测试策略

遵循项目规范：`testify/require` + table-driven + `t.Parallel()` + 单模块 ≤5s（`-count=1 -race`）、禁 `time.Sleep` 等待异步（改 `require.Eventually` / channel）。

### 5.1 pool_test.go 扩展（纯单元，无 DB/无 worker）

| 测试 | 验证点 |
|---|---|
| `TestUpdateLimits_AllFourHotReload` | 构造 pool → Acquire → `UpdateLimits(config.PoolConfig{...})` 改 4 字段 → 新 Acquire 按新阈值被拒/通过 |
| `TestUpdateLimits_DoesNotEvict` | 占满配额 → `UpdateLimits` 降额 → 已 Acquire 的 session 仍计数（无强制释放）；仅新 Acquire 被拒 |
| `TestUpdateLimits_MemoryQuotaToggleOff` | `max_memory_per_user` 正数→0 禁用 → `totalMemoryReservedBytes`/`userMemory` 即时清零；release 后无泄漏/负值 |
| `TestPool_ConcurrentMixedOperations` | 并发 Acquire/Release/UpdateLimits（`-race`）→ 计数不漂移、无数据竞争 |
| `TestMetrics_SnapshotLogic` | Acquire N 个（user/workspace 混合）→ 断言 pool 内部状态（active / distinct_users / distinct_workspaces / memory_reserved） |
| `TestMetrics_SnapshotUpdatesOnRelease` | Acquire → Release → 内部状态回归基线（无泄漏） |

### 5.2 config 包测试

| 测试 | 验证点 |
|---|---|
| `TestPoolConfig_NegativeRejected` | YAML 4 个 `max_*` 任一为负 → 加载报错 `"pool.max_* must be non-negative"` |

### 5.3 watcher 测试

| 测试 | 验证点 |
|---|---|
| `TestWatcher_PoolKeysHotReloaded` | 断言 4 个 `pool.*` 键均在热重载白名单（若 watcher 有现成测试矩阵则扩展，否则轻量断言） |

### 5.4 热重载集成（gateway_run.go，倾向不新增）

真实 watcher + 回调的 e2e 较重，且现有热重载路径已被 spec ① / config spec 覆盖。靠 pool 单元的 `UpdateLimits` 测试 + config 的键白名单测试组合保证。若 reviewer 要求再加。

### 5.5 指标验证

不启真实 Prometheus。OTel callback 在测试里手动触发（调注册的回调函数读观测值），或直接读快照原子变量（包内可见）。避免网络/端口依赖。

### 5.6 race 关键场景

并发 Acquire / Release / UpdateLimits 三线程交叉跑 `-race`，验证快照 atomic 与 `mu` 无数据竞争、计数不漂移。

---

## 6. 改动清单

| 文件 | 改动 |
|---|---|
| `internal/session/pool.go` | `UpdateLimits(config.PoolConfig)` 替换二参版（直接收 config 类型，无镜像 struct）；`snapshotMetricsLocked()` + 5 atomic gauge；`releaseMemoryLocked` 去掉 `>0` 守卫；`ReleaseForWorkspace` 末尾补快照；memory 层禁用即时清零；gauge 描述澄清 |
| `internal/config/watcher.go` | 白名单加 `pool.max_memory_per_user` / `pool.max_per_workspace` |
| `internal/config/config.go` | `pool.max_*` 校验改 `>= 0`（扩展负数拒绝） |
| `internal/config/config_loader.go` | `BindEnv("pool.max_per_workspace")` |
| `cmd/hotplex/gateway_run.go` | 热重载回调用 `prev.Pool != next.Pool` 判变更，传 `next.Pool` 调 `UpdateLimits` |
| `internal/session/pool_test.go` | `TestUpdateLimits_AllFourHotReload` / `TestUpdateLimits_DoesNotEvict` / `TestUpdateLimits_MemoryQuotaToggleOff` / `TestPool_ConcurrentMixedOperations`（race） |
| `internal/session/pool_metrics_test.go` | `TestMetrics_SnapshotLogic` / `TestMetrics_SnapshotUpdatesOnRelease`（断言 pool 内部状态，非包全局 atomic） |
| `internal/config/*_test.go` | 负数校验测试 |

无 DB 迁移、无新 API 端点、无前端改动。

---

## 7. 风险

| 风险 | 缓解 |
|---|---|
| `UpdateLimits` 旧二参签名被遗忘的调用点引用 | grep 确认仅 `gateway_run.go:199` 一处；pool 包无外部消费者 |
| gauge 快照 atomic 与 mu 临界区顺序错位致瞬态不一致 | snapshot 在持锁末尾调；race 测试覆盖 |
| 降额后用户长时间无法新 Acquire（运维忘记恢复） | 日志 `"pool: limits updated"` 含新旧值便于审计；文档明示为预期行为 |
| 4 个 pool.* 全热重载后，高频改配置抖动 | 沿用 watcher 既有去抖；不新增 |

---

## 8. 路线图对齐

实现并合入后，更新 [`WebChat-Multitenancy-Roadmap-Spec.md`](./WebChat-Multitenancy-Roadmap-Spec.md)：

- §3 阶段 C 表格 spec ⑤ 标 ✅ 已合入（附 PR 号）
- §4 spec ⑤ 段落补"交付摘要"
- §7 推进节奏"下一步"推进到 spec ⑥（待 ④ 就绪）或并行 ④
