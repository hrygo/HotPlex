---
type: spec
tags:
  - project/HotPlex
  - eventstore
  - perf
  - refactor
date: 2026-05-19
status: proposed
priority: P1
estimated_hours: 8
---

# Turns 物化表规格书

> 版本: v2.2
> 日期: 2026-05-19
> 状态: Proposed
> 前置: #451 cache token 修复（已合入本方案）
> 审计: v2.0 → v2.1（§6.4-6.6, §12）| v2.1 → v2.2（§6.2 generation 覆盖、§5.2 游标翻页、§7.4 send/flush、§6.1 cache delta）

---

## 设计约束

| # | 约束 | 影响 |
|---|------|------|
| C1 | 接受删库重建 | 可删除 003/008 旧迁移，schema 自由设计 |
| C2 | 接受极端数据丢失 | 不为服务重启做 WAL/恢复设计，collector 通道丢数据可接受 |
| C3 | turns/events 独立于 sessions 生命周期 | 无级联删除，独立 TTL 清理 |
| C4 | session 可手工重置（保留历史数据） | 引入 `generation` 列，重置后 generation+1，turn 编号从 1 重新开始 |
| C5 | 前端严格时序回放 | `id`（自增主键）作为唯一排序键，保证 user→assistant 严格有序 |
| C6 | PostgreSQL 可移植 | 纯标准 SQL，无 json_extract/group_concat/窗口函数 |

---

## 1. 问题

当前 `v_turns_assistant` 视图每次查询对每行执行 13+ 次 `json_extract` + O(n log n) 窗口函数 + `group_concat`。此外：

- session 重置后 AEP seq 重置，视图无法区分新旧 generation
- `tokens_in` 混合了常规输入和缓存 token，无法做成本归因和缓存效率分析
- turns 数据绑定 sessions 表的 JOIN，无法独立清理

## 2. 核心洞察

`bridge_forward.go:forwardEvents` 在处理 `done` 事件时，内存中已拥有全部所需数据：

| 数据 | 位置 | 就绪时机 |
|------|------|---------|
| turn 文本 | `turnText` (L116 累积) | done 时 |
| 工具调用 | `acc.ToolNames / ToolCallCount` (L128-134) | done 时 |
| token/cost/duration | `acc.snapshot()` (L154 注入) | `injectSessionStats` 后 |
| session 元数据 | `sessPlatform / sessOwner` (L35-41 缓存) | goroutine 启动时 |
| generation | bridge resetGeneration 机制 | goroutine 启动时 |

在 L154（injectSessionStats）与 L155（resetPerTurn）之间，数据完全就绪。

---

## 3. Schema 设计

### 3.1 turns 表

```sql
CREATE TABLE turns (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id          TEXT    NOT NULL,
    generation          INTEGER NOT NULL DEFAULT 1,
    turn_num            INTEGER NOT NULL,           -- generation 内递增 (1,2,3...)
    seq                 INTEGER NOT NULL DEFAULT 0, -- AEP 事件 seq（信息性，不用于排序）
    role                TEXT    NOT NULL,            -- 'user' | 'assistant'
    content             TEXT    NOT NULL DEFAULT '',
    platform            TEXT    NOT NULL DEFAULT '',
    user_id             TEXT    NOT NULL DEFAULT '',
    model               TEXT    NOT NULL DEFAULT '',
    success             INTEGER,                    -- NULL for user turns
    source              TEXT    NOT NULL DEFAULT 'normal',
    tools_json          TEXT,                       -- {"Read":2,"Bash":1}
    tool_count          INTEGER NOT NULL DEFAULT 0,
    tokens_input        INTEGER NOT NULL DEFAULT 0,
    tokens_cache_write  INTEGER NOT NULL DEFAULT 0,
    tokens_cache_read   INTEGER NOT NULL DEFAULT 0,
    tokens_out          INTEGER NOT NULL DEFAULT 0,
    duration_ms         INTEGER NOT NULL DEFAULT 0,
    cost_usd            REAL    NOT NULL DEFAULT 0.0,
    created_at          INTEGER NOT NULL            -- Unix ms
);

CREATE INDEX idx_turns_session_gen_id
    ON turns(session_id, generation, id);

CREATE INDEX idx_turns_created
    ON turns(created_at);
```

### 3.2 排序与唯一性

| 场景 | 排序键 | 说明 |
|------|--------|------|
| **前端回放** | `id ASC` | 自增主键，保证 user→assistant 严格时序 |
| **会话内翻页** | `id` 游标 | `WHERE id < cursor` 向前翻页 |
| **generation 内 turn 编号** | `turn_num` | 显示用「第 N 轮」，重置后从 1 开始 |
| **TTL 清理** | `created_at` | 按时间批量删除过期数据 |

**generation 语义**：

```
Session X, generation=1:
  id=1  turn_num=1  role=user      ← 用户输入
  id=2  turn_num=1  role=assistant  ← 助手回复
  id=3  turn_num=2  role=user
  id=4  turn_num=2  role=assistant

Session X 重置 → generation=2:
  id=5  turn_num=1  role=user      ← 重置后重新从 1 开始
  id=6  turn_num=1  role=assistant
```

**seq vs turn_num vs id**：

| 字段 | 含义 | 重置后行为 | 用于 |
|------|------|-----------|------|
| `id` | 全局自增主键 | 单调递增，永不重置 | **排序、翻页游标** |
| `seq` | AEP 事件 seq | 重置为 0（信息性） | 调试、与 events 表关联 |
| `turn_num` | generation 内 turn 编号 | 重置为 1 | **前端显示「第 N 轮」** |
| `generation` | session 重置次数 | 重置时 +1 | 区分新旧 generation |

### 3.3 字段映射

| 字段 | User Turn | Assistant Turn |
|------|-----------|----------------|
| `session_id` | `sessionID` | `sessionID` |
| `generation` | `acc.Generation` | `acc.Generation` |
| `turn_num` | `acc.TurnCount + 1` | `acc.TurnCount`（done 时已 TurnCount++） |
| `seq` | input 事件 seq | done 事件 seq |
| `role` | `"user"` | `"assistant"` |
| `content` | `InputData.Content` | `turnText.String()` |
| `platform` | `sessPlatform` | `sessPlatform` |
| `user_id` | `sessOwner` | `sessOwner` |
| `model` | `""` | `acc.ModelName` |
| `success` | `NULL` | `dd.Success ? 1 : 0` |
| `source` | `"normal"` | `env.Source` 或 `"normal"` |
| `tools_json` | `NULL` | `json.Marshal(acc.ToolNames)` |
| `tool_count` | `0` | `acc.ToolCallCount` |
| `tokens_input` | `0` | `max(0, acc.PerTurnInput - PerTurnCacheWrite - PerTurnCacheRead)` |
| `tokens_cache_write` | `0` | `acc.PerTurnCacheWrite` |
| `tokens_cache_read` | `0` | `acc.PerTurnCacheRead` |
| `tokens_out` | `0` | `acc.PerTurnOutput` |
| `duration_ms` | `0` | `acc.TurnDurationMs` |
| `cost_usd` | `0.0` | `acc.PerTurnCost` |
| `created_at` | `env.Timestamp` | `env.Timestamp` |

### 3.4 tokens_in 拆分

| 分析维度 | 未拆分 | 拆分后 |
|----------|--------|--------|
| 总输入 | `SUM(tokens_in)` | `SUM(input + cache_write + cache_read)` |
| 缓存命中率 | ❌ | `SUM(cache_read) / SUM(input + cache_write + cache_read)` |
| 成本归因 | ❌ 混合单价 | Anthropic 分层：input $3, cache_write $3.75, cache_read $0.30 |

查询兼容：SQL 层提供计算列 `(tokens_input + tokens_cache_write + tokens_cache_read) AS tokens_in`。

---

## 4. 数据生命周期

### 4.1 独立清理（无 session 级联）

turns 和 events 表的清理**完全独立**于 sessions 表：

| 操作 | sessions | events | turns |
|------|----------|--------|-------|
| session 删除 | DELETE | 不动 | 不动 |
| session 重置 | 更新状态 | 不动 | 不动（新 generation） |
| TTL 过期清理 | GC 独立清理 | `DeleteExpired(cutoff)` | `DeleteExpiredTurns(cutoff)` |

### 4.2 `DeleteExpiredTurns`

```go
func (s *SQLiteStore) DeleteExpiredTurns(ctx context.Context, cutoff time.Time) (int64, error) {
    ctx, cancel := withDefaultTimeout(ctx)
    defer cancel()
    res, err := s.db.ExecContext(ctx,
        "DELETE FROM turns WHERE created_at < ?", cutoff.UnixMilli())
    if err != nil {
        return 0, fmt.Errorf("eventstore: delete expired turns: %w", err)
    }
    return res.RowsAffected()
}
```

### 4.3 TTL 清理集成

**现状**：`EventStore.DeleteExpired` 和 `DeleteBySession` 在生产代码中**均未被调用**。Session GC (`manager.go:gc()`) 只处理 session 状态和内存驱逐，不涉及 events/turns 清理。

**方案**：在 `cmd/hotplex/gateway_run.go` 新增独立 GC goroutine，与 session GC 并行运行：

```go
// gateway_run.go — gateway 启动时
go runEventsGC(ctx, deps.EventStore, log, cfg.EventsRetention)
```

```go
func runEventsGC(ctx context.Context, es eventstore.EventStore, log *slog.Logger, retention time.Duration) {
    ticker := time.NewTicker(1 * time.Hour)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            cutoff := time.Now().Add(-retention)
            if n, err := es.DeleteExpired(ctx, cutoff); err == nil && n > 0 {
                log.Info("events gc: deleted expired events", "count", n)
            }
            if n, err := es.DeleteExpiredTurns(ctx, cutoff); err == nil && n > 0 {
                log.Info("events gc: deleted expired turns", "count", n)
            }
        }
    }
}
```

events 和 turns 使用**同一 cutoff**，保证一致性。

### 4.4 `DeleteBySession` 不变

`DeleteBySession` 只删除 events，不动 turns。turns 保留历史数据用于审计/回放。

---

## 5. 查询设计

### 5.1 `turns.query.sql`（分页查询）

```sql
SELECT id, session_id, generation, turn_num, seq, role, content,
       platform, user_id, model, success, source, tools_json, tool_count,
       tokens_input, tokens_cache_write, tokens_cache_read,
       (tokens_input + tokens_cache_write + tokens_cache_read) AS tokens_in,
       tokens_out, duration_ms, cost_usd, created_at
FROM turns
WHERE session_id = ? AND generation = ?
ORDER BY id ASC
LIMIT ? OFFSET ?
```

默认查最新 generation。前端可传 `generation` 参数查看历史。

### 5.2 `turns.query_before.sql`（游标翻页）

```sql
SELECT id, session_id, generation, turn_num, seq, role, content,
       platform, user_id, model, success, source, tools_json, tool_count,
       tokens_input, tokens_cache_write, tokens_cache_read,
       (tokens_input + tokens_cache_write + tokens_cache_read) AS tokens_in,
       tokens_out, duration_ms, cost_usd, created_at
FROM turns
WHERE session_id = ? AND id < ?
ORDER BY id DESC
LIMIT ?
```

**注意**：游标翻页**不过滤 generation**。初始加载（§5.1）只显示最新 generation，但游标翻页需跨 generation 显示历史，保证前端滚动到 generation 边界时无缝衔接。

### 5.3 `turns.stats.sql`（聚合统计）

```sql
SELECT session_id, generation, turn_num, seq, success, source,
       tools_json, tool_count,
       tokens_input, tokens_cache_write, tokens_cache_read,
       (tokens_input + tokens_cache_write + tokens_cache_read) AS tokens_in,
       tokens_out, duration_ms, cost_usd, model, created_at
FROM turns
WHERE session_id = ? AND generation = ? AND role = 'assistant'
ORDER BY id ASC
```

### 5.4 `turns.latest_generation.sql`（新增）

```sql
SELECT COALESCE(MAX(generation), 0) FROM turns WHERE session_id = ?
```

用于 `forwardEvents` 启动时确定当前 generation。

### 5.5 前端时序回放保证

`id` 是自增主键，Collector 单 goroutine 写入，同一 session 内严格保序：

```
id=1  role=user       turn_num=1   generation=1
id=2  role=assistant  turn_num=1   generation=1
id=3  role=user       turn_num=2   generation=1
id=4  role=assistant  turn_num=2   generation=1
...（session 重置）...
id=5  role=user       turn_num=1   generation=2
id=6  role=assistant  turn_num=1   generation=2
```

`ORDER BY id ASC` 严格时序。user 总在 assistant 之前（因为 Collector FIFO + 同一 goroutine 处理）。

---

## 6. Generation 跟踪机制

### 6.1 Accumulator 扩展

```go
type sessionAccumulator struct {
    // ... 现有字段 ...

    Generation     int64  // session 的当前 generation
    TurnCount      int    // generation 内的 turn 计数

    // Cache token tracking (cumulative across turns).
    TotalCacheWrite int64
    TotalCacheRead  int64
    PrevCacheWrite  int64
    PrevCacheRead   int64
    PerTurnCacheWrite int64
    PerTurnCacheRead  int64
}
```

**`mergePerTurnStats` 补充 cache 累加**：

```go
// Claude Code format
if usage, ok := data.Stats["usage"].(map[string]any); ok {
    a.TotalCacheWrite += events.ToInt64(usage["cache_creation_input_tokens"])
    a.TotalCacheRead += events.ToInt64(usage["cache_read_input_tokens"])
    // ... 原有 TotalInput 累加不变 ...
}
// OpenCode format
if tokens, ok := data.Stats["tokens"].(map[string]any); ok {
    a.TotalCacheWrite += events.ToInt64(tokens["cache_write"])
    a.TotalCacheRead += events.ToInt64(tokens["cache_read"])
    // ... 原有 TotalInput 累加不变 ...
}
```

**`computePerTurnDeltas` 补充 cache delta**：

```go
func (a *sessionAccumulator) computePerTurnDeltas() {
    a.PerTurnInput = a.TotalInput - a.PrevTotalIn
    a.PerTurnOutput = a.TotalOutput - a.PrevTotalOut
    a.PerTurnCost = a.TotalCostUSD - a.PrevTotalCost
    a.PerTurnCacheWrite = a.TotalCacheWrite - a.PrevCacheWrite
    a.PerTurnCacheRead = a.TotalCacheRead - a.PrevCacheRead
    // 负值保护
    if a.PerTurnCacheWrite < 0 { a.PerTurnCacheWrite = 0 }
    if a.PerTurnCacheRead < 0 { a.PerTurnCacheRead = 0 }
    // ... 原有负值保护不变 ...
}
```

**`resetPerTurn` 补充 cache baseline 保存**：

```go
func (a *sessionAccumulator) resetPerTurn() {
    a.PrevTotalIn = a.TotalInput
    a.PrevTotalOut = a.TotalOutput
    a.PrevTotalCost = a.TotalCostUSD
    a.PrevCacheWrite = a.TotalCacheWrite
    a.PrevCacheRead = a.TotalCacheRead
    // ... 原有字段重置不变 ...
}
```

> **注意**：`tokens_input` 字段（§3.3）计算为 `PerTurnInput - PerTurnCacheWrite - PerTurnCacheRead`，表示排除缓存后的纯输入 token。当缓存机制未启用时，三者均为 0，`tokens_input == PerTurnInput`。为防负值，写入时加 `max(0, ...)` 保护。

### 6.2 Generation 初始化

`forwardEvents` goroutine 启动时，仅当 accumulator 首次创建时从 turns 表读取 generation：

```go
// bridge_forward.go forwardEvents() 启动阶段
// b.turnsQuerier 在 Bridge 构造时注入（与 b.collector 同源）
acc := b.getOrInitAccum(sessionID, opts.workDir, startTime)
if acc.Generation == 0 {
    // 首次创建：从 turns 表恢复最新 generation（冷启动 / 服务重启场景）
    gen := int64(1)
    if b.turnsQuerier != nil {
        if latest, _ := b.turnsQuerier.LatestGeneration(context.Background(), sessionID); latest > 0 {
            gen = latest
        }
    }
    acc.Generation = gen
}
// acc.Generation > 0 时：已被 ResetSession 递增（§6.4），或上一轮 forwardEvents 设置
// 不覆盖，避免 clobber ResetSession 的 generation++
```

> **为什么不能无条件设置**：ResetSession（§6.4）在启动新 forwardEvents 前已执行 `acc.Generation++`。如果 forwardEvents 再从 turns 表读 max generation 覆盖，会导致 generation 回退到旧值，新旧 turns 混入同一 generation。
>
> **场景覆盖**：
> | 场景 | acc.Generation 状态 | 行为 |
> |------|-------------------|------|
> | 全新 session | 0 → 1 | 从 turns 表读（无记录 → 默认 1） |
> | 服务重启恢复 | 0 → N | 从 turns 表读 max generation |
> | ResetSession 后 | N → N+1（已递增） | 保留，不从 turns 表读 |

> **依赖链**：Bridge 需要 `TurnQuerier` 接口（非 Collector）。Bridge 构造时从 `GatewayDeps` 注入 `EventStore`（同时实现 `TurnQuerier`）。

### 6.3 Session 重置时 Generation 递增

**CC Worker（非 in-place 重置）**：

1. `ResetSession` → `IncResetGeneration()` → `ResetContext()`（杀进程 + 起新进程）
2. 旧 `forwardEvents` goroutine 检测到 generation 不匹配，退出
3. `ResetSession` 启动新 `forwardEvents` goroutine
4. 新 goroutine 调用 `LatestGeneration(sessionID)` → 返回旧 max gen
5. `generation = maxGen + 1`，`TurnCount` 重置为 0
6. 新 turns 从 `generation=N+1, turn_num=1` 开始

**OCS Worker（in-place 重置）**：

OCS 实现了 `InPlaceReseter`，同一 `forwardEvents` goroutine 继续运行。需要在 goroutine 内部检测重置：

```go
// bridge_forward.go forwardEvents() 循环内
// 现有代码已有 myGen 检测（L50-52）
if rg, ok := w.(resetGenerationer); ok {
    currentGen := rg.LoadResetGeneration()
    if currentGen != myGen {
        // OCS in-place reset detected
        acc.Generation++
        acc.TurnCount = 0
        turnText.Reset()
        myGen = currentGen
    }
}
```

### 6.4 Accumulator 重置（关键遗漏修复）

**现状问题**：当前 `getOrInitAccum` 在 session 重置后返回**同一个 accumulator 对象**，`TurnCount` 持续递增不归零。

**解决方案**：在 `ResetSession` 中显式重置 accumulator 的 generation 计数器：

```go
// bridge.go ResetSession()
if rg, ok := w.(resetGenerationer); ok {
    rg.IncResetGeneration()
}
// ... ResetContext() ...
// 重置 accumulator 的 generation 内计数器
b.accumMu.Lock()
if acc, ok := b.accum[sessionID]; ok {
    acc.TurnCount = 0
    acc.Generation++  // 不清除累计总量（TotalInput 等），只重置 generation 内计数
}
b.accumMu.Unlock()
```

对于 in-place 重置（OCS），在 `forwardEvents` 循环内检测 generation 变化后同步重置。

> **TurnCount 数据竞争说明**：`ResetSession`（handler goroutine）在 `accumMu` 锁内写 `acc.TurnCount = 0`，但 `handleWorkerExit`（旧 forwardEvents goroutine）在锁外读 `acc.TurnCount`（bridge_forward.go:373）。这是预存问题（非本方案引入），被 Go race detector 检测到。缓解措施：CC Worker reset 后旧 forwardEvents 退出时只读 TurnCount 用于日志，不用于逻辑决策。长期修复应将 TurnCount 改为 `atomic.Int32`。

### 6.5 `LatestGeneration` 依赖链

```
Bridge.turnsQuerier (注入 TurnQuerier 接口)
  └── SQLiteStore (同时实现 EventStore + TurnQuerier)
        └── SELECT COALESCE(MAX(generation), 0) FROM turns WHERE session_id = ?
```

Bridge 需要新增 `turnsQuerier eventstore.TurnQuerier` 字段，在 `NewBridge` 时从 `BridgeDeps` 注入。

### 6.6 用户 Turn 写入的 Session 元数据

**现状**：`CaptureInbound(sessionID, seq, eventType, data)` 无 platform/owner 参数。调用点在 `handler.go:126,236`。

**方案**：扩展 `CaptureInbound` 签名：

```go
// bridge.go
func (b *Bridge) CaptureInbound(sessionID string, seq int64, eventType events.Kind,
    data any, platform, owner string) {
    b.captureDirected(sessionID, seq, eventType, data, "inbound", platform, owner)
}
```

调用点 `handler.go` 已有 `si` (SessionInfo) 可用：
```go
h.bridge.CaptureInbound(env.SessionID, env.Seq, events.Input, env.Event.Data,
    si.Platform, si.OwnerID)
```

---

## 7. 修改清单

### 7.1 文件变更

| 文件 | 操作 | 说明 |
|------|------|------|
| `internal/session/sql/migrations/009_create_turns_table.sql` | 新增 | turns 表 + 索引 |
| `internal/eventstore/turn_write.go` | 新增 | `TurnWriteRequest` + INSERT SQL |
| `internal/eventstore/collector.go` | 修改 | `captureRequest` 扩展 + `CaptureTurn` + `AppendTurn` |
| `internal/eventstore/store.go` | 修改 | 结构体 generation/cache 字段；`DeleteExpiredTurns`；`LatestGeneration`；接口签名变更；`scanTurns` 重写 |
| `internal/eventstore/sql/queries/turns.*.sql` | 修改 | 从 turns 表读取，id 排序 + generation 过滤 |
| `internal/gateway/session_stats.go` | 修改 | accumulator generation + cache write/read |
| `internal/gateway/bridge.go` | 修改 | 新增 `turnsQuerier` 字段；`CaptureInbound` 签名扩展；`ResetSession` accumulator 重置 |
| `internal/gateway/bridge_forward.go` | 修改 | generation 初始化 + OCS in-place 检测 + 用户/助手 turn 写入 |
| `internal/gateway/handler.go` | 修改 | `CaptureInbound` 调用点传递 platform/owner |
| `internal/gateway/api.go` | 修改 | `GetHistory` cursor 从 `before_seq` 改 `before_id`，增加 generation |
| `internal/gateway/api_test.go` | 修改 | mockTurnsStore 签名更新 |
| `internal/admin/admin.go` | 修改 | `TurnStatsProvider` 接口增加 generation 参数 |
| `internal/admin/sessions.go` | 修改 | `HandleSessionStats` 传递 generation |
| `cmd/hotplex/admin_adapters.go` | 修改 | `turnsStoreAdapter.TurnStats` 适配 |
| `cmd/hotplex/cron_admin_adapter.go` | 修改 | `RunHistory` 传递 generation |
| `cmd/hotplex/gateway_run.go` | 修改 | Bridge 构造注入 `turnsQuerier`；cron delivery 适配；新增 events/turns GC goroutine |
| `internal/cli/cron/client.go` | 修改 | `QueryHistory` 传递 generation |
| `internal/eventstore/turns_view_test.go` | 重写 | → `turns_table_test.go` |
| `internal/gateway/session_stats_test.go` | 修改 | generation + cache 测试 |
| `pkg/events/events.go` | 不变 | `InputData` 结构已有 `Content` 字段 |

可删除：

| 文件 | 说明 |
|------|------|
| `internal/session/sql/migrations/003_create_turns_view.sql` | 视图迁移 |
| `internal/session/sql/migrations/008_fix_cache_token_accounting.sql` | cache token 视图修复 |

### 7.2 接口变更

**`TurnQuerier` 接口**（破坏性变更，接受删库）：

```go
type TurnQuerier interface {
    QueryTurns(ctx context.Context, sessionID string, limit, offset int) ([]*TurnRecord, error)
    QueryTurnsBefore(ctx context.Context, sessionID string, beforeID int64, limit int) ([]*TurnRecord, error) // 不过滤 generation
    QueryTurnStats(ctx context.Context, sessionID string) (*TurnStats, error)
    LatestGeneration(ctx context.Context, sessionID string) (int64, error)
    DeleteExpiredTurns(ctx context.Context, cutoff time.Time) (int64, error)
}
```

**内部实现**：`QueryTurns`/`QueryTurnStats` 内部调用 `LatestGeneration` 自动获取最新 generation，外部调用者无需感知 generation。

> **决策**：generation 是内部实现细节，不暴露到 `TurnQuerier` 方法签名。所有方法签名保持与当前相同（除 `beforeSeq` → `beforeID`），generation 过滤在 `SQLiteStore` 内部完成。

**`TurnStatsProvider` 接口**（`internal/admin/admin.go`）：

签名不变（`TurnStats(ctx, sessionID) (*TurnStats, error)`），内部实现自动获取最新 generation。

**`EventTx` 接口**扩展：

```go
type EventTx interface {
    Append(ctx context.Context, event *StoredEvent) error
    AppendTurn(ctx context.Context, turn *TurnWriteRequest) error  // 新增
    Commit() error
}
```

**`Bridge` 结构扩展**：

```go
type Bridge struct {
    // ... 现有字段 ...
    turnsQuerier eventstore.TurnQuerier  // 新增：用于 LatestGeneration
}
```

### 7.3 结构体变更

**`TurnRecord`**：

```go
type TurnRecord struct {
    ID               int64          `json:"id"`               // 数据库自增 id
    SessionID        string         `json:"session_id"`
    Generation       int64          `json:"generation"`
    TurnNum          int            `json:"turn_num"`         // generation 内编号
    Seq              int64          `json:"seq"`              // AEP seq（信息性）
    Role             string         `json:"role"`
    Content          string         `json:"content"`
    Platform         string         `json:"platform"`
    UserID           string         `json:"user_id"`
    Model            string         `json:"model"`
    Success          *bool          `json:"success"`
    Source           string         `json:"source"`
    Tools            map[string]int `json:"tools"`
    ToolCount        int            `json:"tool_call_count"`
    TokensIn         int            `json:"tokens_in"`               // 兼容：三字段之和
    TokensInput      int            `json:"tokens_input"`
    TokensCacheWrite int            `json:"tokens_cache_write"`
    TokensCacheRead  int            `json:"tokens_cache_read"`
    TokensOut        int            `json:"tokens_out"`
    DurationMs       int64          `json:"duration_ms"`
    CostUSD          float64        `json:"cost_usd"`
    CreatedAt        int64          `json:"created_at"`
}
```

**`TurnStats`**：

```go
type TurnStats struct {
    SessionID         string         `json:"session_id"`
    Generation        int64          `json:"generation"`
    TotalTurns        int            `json:"total_turns"`
    SuccessTurns      int            `json:"success_turns"`
    FailedTurns       int            `json:"failed_turns"`
    TotalDurMs        int64          `json:"total_duration_ms"`
    TotalCostUSD      float64        `json:"total_cost_usd"`
    TotalTokIn        int64          `json:"total_tokens_in"`               // 兼容
    TotalTokInput     int64          `json:"total_tokens_input"`
    TotalTokCacheWrite int64         `json:"total_tokens_cache_write"`
    TotalTokCacheRead  int64         `json:"total_tokens_cache_read"`
    TotalTokOut       int64          `json:"total_tokens_out"`
    Turns             []TurnStatItem `json:"turns"`
}
```

### 7.4 Collector 扩展

**`captureRequest`**：

```go
type captureRequest struct {
    event *StoredEvent      // event-only request
    turn  *TurnWriteRequest // turn-only request（互斥）
}

func (r *captureRequest) sessionID() string {
    if r.event != nil {
        return r.event.SessionID
    }
    return r.turn.SessionID
}
```

**`TurnWriteRequest`**：

```go
type TurnWriteRequest struct {
    SessionID        string
    Generation       int64
    TurnNum          int
    Seq              int64
    Role             string
    Content          string
    Platform         string
    UserID           string
    Model            string
    Success          *bool
    Source           string
    ToolsJSON        string
    ToolCount        int
    TokensInput      int64
    TokensCacheWrite int64
    TokensCacheRead  int64
    TokensOut        int64
    DurationMs       int64
    CostUSD          float64
    CreatedAt        int64
}
```

**`CaptureTurn` 方法**：

```go
func (c *Collector) CaptureTurn(turn *TurnWriteRequest) {
    if turn == nil {
        return
    }
    req := &captureRequest{turn: turn}
    c.send(req)
}
```

**`send` 方法更新**（当前硬编码 `req.event.SessionID`，turn-only 会 panic）：

```go
func (c *Collector) send(req *captureRequest) {
    select {
    case c.captureC <- req:
    default:
        c.dropped.Add(1)
        c.log.Warn("eventstore: capture channel full, dropping",
            "session_id", req.sessionID(),
            "kind", kindOf(req), // "event" or "turn"
        )
    }
}

func kindOf(req *captureRequest) string {
    if req.turn != nil {
        return "turn"
    }
    return "event"
}
```

**`EventTx` 接口扩展**：

```go
type EventTx interface {
    Append(ctx context.Context, event *StoredEvent) error
    AppendTurn(ctx context.Context, turn *TurnWriteRequest) error
    Commit() error
}
```

**`flushBatch`**：遍历 batch，按类型分派：

```go
func (c *Collector) flushBatch(batch []*captureRequest) {
    // ... tx begin ...
    for _, req := range batch {
        var err error
        switch {
        case req.turn != nil:
            err = tx.AppendTurn(ctx, req.turn)
        case req.event != nil:
            err = tx.Append(ctx, req.event)
        }
        if err != nil {
            c.log.Warn("eventstore: batch append failed",
                "session_id", req.sessionID(), "err", err)
        }
    }
    // ... tx commit ...
}
```

### 7.5 Bridge 写入

**助手 turn**（done 事件处理路径，L154-155 之间）：

```go
b.injectSessionStats(env, acc)
// 写入 turns 表
b.captureAssistantTurn(sessionID, env.Seq, acc, turnText.String(),
    sessOwner, sessPlatform, env.Timestamp)
acc.resetPerTurn()
```

**用户 turn**（`CaptureInbound` 路径）：

`captureDirected` 扩展后需要 accumulator 获取 turn_num。从 `getOrInitAccum` 获取：

```go
func (b *Bridge) CaptureInbound(sessionID string, seq int64, eventType events.Kind,
    data any, platform, owner string) {

    b.captureDirected(sessionID, seq, eventType, data, "inbound", platform, owner)

    // 同时写入 user turn 记录
    if eventType == events.Input && b.collector != nil {
        acc := b.getOrInitAccum(sessionID, "", time.Now())
        content := extractInputContent(data) // 从 InputData 提取 content
        turn := &eventstore.TurnWriteRequest{
            SessionID:  sessionID,
            Generation: acc.Generation,
            TurnNum:    acc.TurnCount + 1, // 当前 turn 编号（TurnCount 尚未递增）
            Seq:        seq,
            Role:       "user",
            Content:    content,
            Platform:   platform,
            UserID:     owner,
            Source:     eventstore.SourceNormal,
            CreatedAt:  time.Now().UnixMilli(),
        }
        b.collector.CaptureTurn(turn)
    }
}

// extractInputContent 从 InputData 提取用户输入文本
func extractInputContent(data any) string {
    switch d := data.(type) {
    case events.InputData:
        return d.Content
    case map[string]any:
        if c, ok := d["content"].(string); ok {
            return c
        }
    }
    return ""
}
```

**synthetic 事件**（crash/timeout）：

`captureSyntheticEvent` 同时写入 assistant turn（`success=false`）。需获取 accumulator 以填写 generation/turn_num。

---

## 8. 数据流

### 8.1 写入流

```
Handler (Input)
  ├── CaptureInbound → Collector.Capture(event)     → INSERT events
  └── CaptureTurn(user)  → Collector.CaptureTurn    → INSERT turns

Worker (MessageDelta/Reasoning) → turnText 累积 + Collector delta 合并

Worker (Done)
  ├── acc.mergePerTurnStats → acc.computePerTurnDeltas → injectSessionStats
  ├── CaptureTurn(assistant) → Collector.CaptureTurn   → INSERT turns
  └── captureEvent(done)   → Collector.Capture(event)  → INSERT events

Collector (单 goroutine, FIFO)
  ├── captureC channel (cap=2048)
  ├── flushBatch: 同一事务内 Append + AppendTurn
  └── 保证 user turn.id < 对应 assistant turn.id（FIFO 保序）
```

### 8.2 读取流

```
GatewayAPI.GetHistory(sessionID, cursorID, limit)
  ├── LatestGeneration(sessionID) → gen=N
  ├── QueryTurns(sessionID, gen, limit, offset)       → ORDER BY id ASC（generation 过滤）
  └── QueryTurnsBefore(sessionID, cursorID, limit)    → WHERE id < cursor（不过滤 generation）

AdminAPI.SessionStats(sessionID)
  ├── LatestGeneration(sessionID) → gen=N
  └── QueryTurnStats(sessionID, gen) → SUM/MAX 聚合

Cron Delivery(sessionID)
  ├── LatestGeneration(sessionID) → gen=N
  └── QueryTurns(sessionID, gen, 1, 0) → 最新 turn content
```

---

## 9. 性能对比

| 指标 | 当前（视图） | 优化后（turns 表） |
|------|------------|-------------------|
| json_extract / 行 | 13+ | **0** |
| 窗口函数 | O(n log n) | **无** |
| group_concat | 有 | **无** |
| 排序 | created_at（非唯一） | **id（自增，严格有序）** |
| 索引 | session_id 过滤 | session_id + generation + id 覆盖 |
| 100 turn 查询 | ~2-5ms | **~0.1ms** |
| 写入开销 | 0 | +1 INSERT/done（同事务） |
| 存储冗余 | 0 | ~280 bytes/turn |

---

## 10. 验证

```bash
go test ./internal/eventstore/ -run TestTurns -v -count=1
go test ./internal/gateway/ -run TestSessionAccumulator -v -count=1
go test ./internal/gateway/ -run TestBridge -v -count=1
make check
```

### 关键测试用例

| 测试 | 验证点 |
|------|--------|
| 基础写入 | user/assistant turn 完整写入，id 严格递增 |
| Cache 拆分 | tokens_input/cache_write/cache_read 正确拆分 |
| Session 重置 | generation 递增，turn_num 从 1 重新开始 |
| 重置后查询 | 默认返回最新 generation，历史 generation 可查 |
| 重置后翻页 | id 游标跨 generation 正确 |
| TTL 清理 | `DeleteExpiredTurns` 按 created_at 清理 |
| Session 删除不级联 | `DeleteBySession` 不影响 turns |
| Synthetic 事件 | crash/timeout source + generation 正确 |
| 前端时序回放 | `ORDER BY id ASC` 保证 user→assistant 严格有序 |

---

## 11. 破坏性变更清单

### 11.1 外部 API

| 端点 | 变更 | 影响 |
|------|------|------|
| `GET /api/sessions/{id}/history` | `id` 字段类型 `string` → `int64`；查询参数 `before_seq` → `before_id`；新增 `generation`/`turn_num`/`tokens_input`/`tokens_cache_write`/`tokens_cache_read` 字段 | WebChat 前端需适配 |
| `GET /admin/sessions/{id}/stats` | `TurnStats` 新增 `generation`/`total_tokens_input`/`total_tokens_cache_write`/`total_tokens_cache_read` | Admin WebUI 需适配 |
| `GET /admin/cron/jobs/{id}/runs` | 同 `TurnStats` 变更 | CLI `--json` 输出新增字段（向后兼容） |

### 11.2 内部 Go 接口

| 接口/方法 | 变更 | 所有实现方 |
|-----------|------|-----------|
| `TurnQuerier.QueryTurnsBefore` | `beforeSeq` → `beforeID`；不过滤 generation（跨 generation 翻页） | `SQLiteStore` + `mockTurnsStore` (api_test.go:148) |
| `TurnQuerier.LatestGeneration` | 新增方法 | `SQLiteStore` + `mockTurnsStore` |
| `TurnQuerier.DeleteExpiredTurns` | 新增方法 | `SQLiteStore` + `mockTurnsStore` |
| `EventTx.AppendTurn` | 新增方法 | `sqliteTx` |
| `Bridge.CaptureInbound` | 新增 `platform, owner string` 参数 | `handler.go:126,236` 两处调用 |
| `TurnRecord.ID` | `string` → `int64` | `scanTurns` (store.go:456) 删除合成 ID 生成 |

### 11.3 Mock/测试更新

| 文件 | 变更项 |
|------|--------|
| `internal/gateway/api_test.go` | `mockTurnsStore` 三个方法签名 + 新增 `LatestGeneration`/`DeleteExpiredTurns` mock；所有 `ts.On(...)` expectation 更新；`before_seq=5` → `before_id=5` |
| `internal/eventstore/turns_view_test.go` | 整体重写：视图 SQL → turns 表 INSERT + SELECT |
| `internal/gateway/ctrl_test.go` | `DeleteExpiredEvents` mock（已过时）检查 |
| `internal/gateway/conn_test.go` | 同上 |

### 11.4 配置新增

| 配置项 | Viper 路径 | 类型 | 默认值 | 说明 |
|--------|-----------|------|--------|------|
| `events.retention` | `config.yaml` → `events.retention` | duration | `168h`（7 天） | events + turns TTL |

注入点：`gateway_run.go` 中读取 `cfg.GetDuration("events.recention")`，传递给 `runEventsGC`。默认值在 `config.go` 的 `Default()` 函数中设置。

---

## 12. 实施步骤（修订）

### Phase 1：Schema + 写入管道

1. `009_create_turns_table.sql` 迁移
2. `turn_write.go`：`TurnWriteRequest` + INSERT SQL
3. `collector.go`：`captureRequest` 扩展 + `CaptureTurn` + `AppendTurn`
4. 删除 003/008 旧迁移

### Phase 2：Accumulator

5. `session_stats.go`：generation + cache write/read 字段 + resetGeneration 方法
6. `session_stats_test.go`：generation + cache 测试

### Phase 3：Bridge 写入

7. `bridge.go`：新增 `turnsQuerier` 字段 + `CaptureInbound` 签名扩展 + `ResetSession` accumulator 重置
8. `bridge_forward.go`：generation 初始化 + OCS in-place 检测 + assistant turn 写入 + user turn 写入 + synthetic turn 写入
9. `handler.go`：`CaptureInbound` 调用点传递 platform/owner

### Phase 4：查询改写

10. 更新 SQL 查询文件（id 排序 + 内部 generation 过滤）
11. 更新 `TurnRecord` / `TurnStats` 结构体
12. 重写 `scanTurns` + `QueryTurnStats`（新增列扫描）
13. 新增 `LatestGeneration` + `DeleteExpiredTurns`

### Phase 5：API + 下游适配

14. `api.go`：`GetHistory` cursor `before_seq` → `before_id`
15. `admin/admin.go` + `admin/sessions.go`：适配
16. `admin_adapters.go` + `cron_admin_adapter.go`：适配
17. `gateway_run.go`：Bridge 注入 turnsQuerier + cron delivery 适配 + events/turns GC goroutine
18. `cli/cron/client.go`：适配

### Phase 6：Mock + 测试

19. `api_test.go`：mockTurnsStore 全部签名更新
20. `turns_view_test.go` → `turns_table_test.go` 重写
21. `make check` 全量验证

---

## 13. 验收标准 (AC)

> 前缀 `TURN` 对应本规格书。优先级：P0 发布前必须通过，P1 MVP 后实现，P2 增强功能。

---

### TURN-001 — turns 表创建与索引
**描述**: `009_create_turns_table.sql` 迁移正确创建 turns 表及索引，字段类型、约束满足 §3.1。

**验收标准**:
- Given 全新数据库, When goose up 执行 009 迁移, Then turns 表存在且包含所有 21 列（id/session_id/generation/turn_num/seq/role/content/platform/user_id/model/success/source/tools_json/tool_count/tokens_input/tokens_cache_write/tokens_cache_read/tokens_out/duration_ms/cost_usd/created_at）
- Given turns 表已创建, Then `idx_turns_session_gen_id(session_id, generation, id)` 和 `idx_turns_created(created_at)` 索引存在
- Given 003/008 旧迁移已删除, When goose up 全量执行, Then v_turns / v_turns_assistant / v_turns_user 视图不存在
- Given goose down 执行 009, Then turns 表被 DROP 且无残留索引

### TURN-002 — turns 表 INSERT 正确性
**描述**: `TurnWriteRequest` 通过 Collector 管道写入 turns 表，所有字段正确映射。

**验收标准**:
- Given 有效 `TurnWriteRequest`, When `EventTx.AppendTurn` 执行, Then INSERT 成功且 id 自增、created_at 正确
- Given 连续 3 个 TurnWriteRequest（同 session）, When flushBatch 提交, Then id 严格递增（id₁ < id₂ < id₃）
- Given `tools_json` 为 `{"Read":2,"Bash":1}`, When 写入后读取, Then JSON 字符串原样保留
- Given `success` 为 NULL（user turn）, When 写入后读取, Then success 列为 NULL 而非 0
- Given `content` 超过 100KB, When AppendTurn, Then 写入成功无截断

### TURN-003 — Collector 双通道写入
**描述**: Collector 的 `captureRequest` 支持 event 和 turn 互斥分发，`send` / `flushBatch` 不 panic。

**验收标准**:
- Given `captureRequest{turn: req}`, When `send()` 执行, Then 不访问 `req.event`（不 panic），日志输出 `kind=turn`
- Given `captureRequest{event: req}`, When `send()` 执行, Then 日志输出 `kind=event`
- Given batch 包含 2 个 event + 1 个 turn, When `flushBatch` 执行, Then 同一事务内 events 表 INSERT 2 行 + turns 表 INSERT 1 行
- Given channel 已满（cap=2048）, When `CaptureTurn` 调用, Then `dropped` 计数器 +1 且不阻塞

---

### TURN-004 — Assistant Turn 写入（done 路径）
**描述**: forwardEvents 处理 done 事件时，在 injectSessionStats 和 resetPerTurn 之间写入 assistant turn。

**验收标准**:
- Given forwardEvents 收到 done 事件, When `injectSessionStats` 完成, Then `captureAssistantTurn` 被调用且 `acc.snapshot()` 已注入 `_session`
- Given done 事件的 stats.usage 含 input_tokens=100/cache_creation=50/cache_read=30/output_tokens=200, When assistant turn 写入, Then `tokens_input=20, tokens_cache_write=50, tokens_cache_read=30, tokens_out=200`
- Given turnText 累积 "Hello\nWorld", When assistant turn 写入, Then content = "Hello\nWorld"
- Given acc.ToolNames = {"Read":2}, When assistant turn 写入, Then tools_json = `{"Read":2}`，tool_count = 2
- Given done 事件 success=false, When assistant turn 写入, Then success = 0（INTEGER）

### TURN-005 — User Turn 写入（Input 路径）
**描述**: CaptureInbound 在 input 事件同时写入 user turn 记录。

**验收标准**:
- Given handler.go 调用 `CaptureInbound(sid, seq, Input, data, platform, owner)`, When eventType == Input, Then `CaptureTurn` 被调用且 role="user"
- Given InputData.Content = "fix the bug", When user turn 写入, Then content = "fix the bug"
- Given acc.TurnCount = 3, When user turn 写入, Then turn_num = 4（TurnCount + 1）
- Given eventType == ToolResult, When CaptureInbound, Then 不写入 turn（仅 Input 事件触发）
- Given platform="feishu" owner="user_123", When user turn 写入, Then platform="feishu" user_id="user_123"

### TURN-006 — Synthetic Turn 写入（crash/timeout）
**描述**: captureSyntheticEvent 在 crash/timeout 场景同时写入 assistant turn。

**验收标准**:
- Given worker crash (exit_code != 0), When `captureSyntheticEvent` 调用, Then 同时写入 events 表（done type）和 turns 表（role="assistant", success=false, source="crash"）
- Given turn timeout, When synthetic turn 写入, Then source="timeout"
- Given crash 时 turnText 非空, When synthetic turn 写入, Then content 包含已累积的 turnText

### TURN-007 — User/Assistant Turn 严格时序
**描述**: Collector 单 goroutine FIFO 保证 user turn.id < assistant turn.id。

**验收标准**:
- Given session 首个 turn, When user input → worker done 完整流程, Then user turn.id < assistant turn.id
- Given 连续 3 轮对话, When 所有 turns 写入, Then id 序列为: user₁ < asst₁ < user₂ < asst₂ < user₃ < asst₃
- Given `ORDER BY id ASC` 查询, When 遍历结果, Then role 序列为 user/assistant/user/assistant/...

---

### TURN-008 — Generation 首次初始化
**描述**: 全新 session 的 accumulator generation 从 turns 表读取，无记录时默认 1。

**验收标准**:
- Given 全新 session（turns 表无记录）, When forwardEvents 启动, Then acc.Generation = 1
- Given 服务重启后 session 恢复（turns 表有 generation=3 记录）, When forwardEvents 启动, Then acc.Generation = 3
- Given forwardEvents 启动, When turnsQuerier == nil, Then acc.Generation = 1（降级默认值）

### TURN-009 — Generation 不覆盖 ResetSession 递增值
**描述**: forwardEvents 的 generation 初始化仅在 acc.Generation == 0 时执行，不覆盖 ResetSession 已递增的值。

**验收标准**:
- Given ResetSession 已执行 `acc.Generation++`（1→2）, When 新 forwardEvents 启动, Then acc.Generation 保持为 2，不被 turns 表 max generation 覆盖
- Given acc.Generation == 0（首次创建）, When forwardEvents 启动, Then 从 turns 表读取 max generation
- Given acc.Generation > 0, When forwardEvents 启动, Then 跳过 turns 表查询

### TURN-010 — CC Worker Session Reset Generation 递增
**描述**: ResetSession 对 CC Worker 递增 generation 并重置 TurnCount。

**验收标准**:
- Given CC Worker session generation=1 TurnCount=5, When ResetSession 调用, Then acc.Generation = 2, acc.TurnCount = 0
- Given ResetSession 完成, When 旧 forwardEvents 检测 generation 不匹配, Then 旧 goroutine 退出不触发 crash 处理
- Given ResetSession 完成, When 新 forwardEvents 启动, Then 新 turns 的 generation=2, turn_num 从 1 开始
- Given ResetSession 执行, When acc 中 TotalInput=10000, Then TotalInput 保持不变（仅重置 generation 内计数器）

### TURN-011 — OCS In-Place Reset Generation 递增
**描述**: OCS Worker in-place reset 时，同一 forwardEvents goroutine 检测 generation 变化并重置 accumulator。

**验收标准**:
- Given OCS Worker myGen=3, When `LoadResetGeneration()` 返回 4, Then acc.Generation 从 3 变为 4，acc.TurnCount 重置为 0
- Given OCS in-place reset 检测后, When 下一轮 input 到达, Then turn_num = 1, generation = 4
- Given OCS Worker myGen 与 `LoadResetGeneration()` 一致, When 正常事件处理, Then 不触发 reset 逻辑

### TURN-012 — Reset 后 TurnNum 正确递增
**描述**: Session reset 后 turn_num 从 1 重新开始，与 generation 配合正确。

**验收标准**:
- Given generation=1 已有 turn_num=1,2,3, When reset 后 generation=2, Then 新 turn_num = 1, 2, 3（独立递增）
- Given 同一 session turns 表包含 generation=1 的 3 轮和 generation=2 的 2 轮, When 查询, Then 可通过 generation 区分两组 turn_num

---

### TURN-013 — QueryTurns 默认查最新 Generation
**描述**: QueryTurns 内部调用 LatestGeneration 自动获取最新 generation，外部调用者无需感知。

**验收标准**:
- Given session 有 generation=1（3 轮）和 generation=2（2 轮）, When `QueryTurns(sessionID, 10, 0)`, Then 返回 generation=2 的 2 条记录
- Given session 无任何 turns, When `QueryTurns(sessionID, 10, 0)`, Then 返回 ErrNotFound
- Given `LatestGeneration` 返回 0（新 session）, When `QueryTurns`, Then 返回 ErrNotFound（无 generation=0 的记录）

### TURN-014 — QueryTurnsBefore 跨 Generation 翻页
**描述**: 游标翻页不过滤 generation，支持跨 generation 无缝滚动。

**验收标准**:
- Given generation=2 最新 turn.id=100, When `QueryTurnsBefore(sessionID, 100, 10)`, Then 返回 id < 100 的记录（含 generation=1 的 turns）
- Given generation=1 turn.id=50 是最后一条, When `QueryTurnsBefore(sessionID, 51, 10)`, Then 返回空 + ErrNotFound
- Given 跨 generation 边界, When id=52（gen=2 首条）的上一页, Then 返回 id < 52 的 generation=1 记录

### TURN-015 — QueryTurnStats 聚合正确性
**描述**: TurnStats 聚合仅统计最新 generation 的 assistant turns。

**验收标准**:
- Given generation=2 有 3 个 assistant turn（success: 2, failed: 1）, When `QueryTurnStats(sessionID)`, Then TotalTurns=3, SuccessTurns=2, FailedTurns=1
- Given generation=2 有 tokens_input=100+200, tokens_cache_write=50+30, tokens_out=300+400, When `QueryTurnStats`, Then TotalTokInput=300, TotalTokCacheWrite=80, TotalTokOut=700
- Given 仅 generation=1 数据（无 generation=2）, When `QueryTurnStats`, Then 统计 generation=1 的数据
- Given user turns 存在, When `QueryTurnStats`, Then 不计入 TotalTurns（仅 role='assistant'）

### TURN-016 — LatestGeneration 查询
**描述**: `COALESCE(MAX(generation), 0)` 正确返回最新 generation。

**验收标准**:
- Given session 有 generation=1,2,3 的 turns, When `LatestGeneration(sessionID)`, Then 返回 3
- Given session 无任何 turns, When `LatestGeneration(sessionID)`, Then 返回 0
- Given session 有 generation=1 的 turns, When `LatestGeneration(sessionID)`, Then 返回 1

### TURN-017 — ID 游标翻页（before_id）
**描述**: GetHistory API 使用 id 游标替代 seq 游标。

**验收标准**:
- Given `GET /api/sessions/{id}/history?before_id=50&limit=10`, When 响应, Then 返回 id < 50 的最新 10 条 turns（DESC 排序后逆序为 ASC）
- Given `before_id` 未传, When `GetHistory`, Then 返回最新 generation 的最新 N 条 turns
- Given `before_id=0`, When `GetHistory`, Then 等同于未传（返回最新）
- Given 返回记录 > limit, When `has_more=true`, Then 截断至 limit 条

---

### TURN-018 — DeleteExpiredTurns 独立 TTL 清理
**描述**: turns 表通过独立 GC goroutine 按 created_at 清理，与 sessions/events 表解耦。

**验收标准**:
- Given turns 有 created_at < cutoff 的 100 条记录, When `DeleteExpiredTurns(cutoff)`, Then 删除 100 条并返回 affected=100
- Given turns 有 created_at >= cutoff 的记录, When `DeleteExpiredTurns(cutoff)`, Then 不删除任何记录
- Given session 被删除（DeletePhysical）, When events/turns GC 运行, Then 该 session 的 turns 不受影响（仅 TTL 清理）
- Given `DeleteBySession(sessionID)` 调用, Then 仅删除 events 表记录，turns 表不动

### TURN-019 — Events/Turns GC Goroutine 生命周期
**描述**: `runEventsGC` 在 gateway 启动时创建，ctx 取消时退出。

**验收标准**:
- Given gateway 启动, When `runEventsGC` 运行, Then 每小时执行一次 `DeleteExpired` + `DeleteExpiredTurns`
- Given ctx 被取消（gateway shutdown）, When `runEventsGC` 检测 `<-ctx.Done()`, Then 退出 goroutine
- Given `events.retention=168h`（默认 7 天）, When GC 运行, Then cutoff = now - 168h
- Given `events.retention` 未配置, When GC 运行, Then 使用默认值 168h

---

### TURN-020 — TurnRecord ID 类型变更
**描述**: `TurnRecord.ID` 从 `string`（"sessionID:seq" 合成）变为 `int64`（数据库自增）。

**验收标准**:
- Given turns 表 id=42, When `scanTurns` 读取, Then `TurnRecord.ID = 42`（int64 类型）
- Given 旧代码 `r.ID = fmt.Sprintf("%s:%d", r.SessionID, r.Seq)`, When 改写后, Then 该行删除（scanTurns 直接扫描 id 列）

### TURN-021 — GetHistory API 查询参数变更
**描述**: `before_seq` 参数替换为 `before_id`，响应新增 generation/turn_num/cache token 字段。

**验收标准**:
- Given `GET /api/sessions/{id}/history?before_id=50`, When 响应, Then 200 + JSON 含 records 数组
- Given `GET /api/sessions/{id}/history?before_seq=50`（旧参数）, When 响应, Then before_seq 被忽略（不再识别），返回最新 N 条
- Given turn 记录含 generation=2 turn_num=3, When 响应 JSON, Then 包含 `"generation":2, "turn_num":3, "tokens_input":100, "tokens_cache_write":50, "tokens_cache_read":30, "tokens_in":180`

### TURN-022 — TurnQuerier 接口变更兼容
**描述**: 所有 TurnQuerier 实现方（SQLiteStore + mockTurnsStore）更新新签名。

**验收标准**:
- Given `api_test.go` 的 `mockTurnsStore`, When 编译, Then 实现 `LatestGeneration` 和 `DeleteExpiredTurns` 方法
- Given `api_test.go` 的 `QueryTurnsBefore` mock, When 验证, Then 参数为 `(ctx, sessionID, beforeID, limit)` 无 generation 参数
- Given 所有 mock 更新完成, When `make check`, Then 编译通过 + 测试通过

---

### TURN-023 — Cache Token 累加正确性
**描述**: mergePerTurnStats 分别累加 TotalCacheWrite / TotalCacheRead。

**验收标准**:
- Given Claude Code usage: input_tokens=100, cache_creation_input_tokens=50, cache_read_input_tokens=30, When `mergePerTurnStats`, Then TotalInput += 180, TotalCacheWrite += 50, TotalCacheRead += 30
- Given OpenCode tokens: input=80, cache_write=40, cache_read=20, When `mergePerTurnStats`, Then TotalInput += 140, TotalCacheWrite += 40, TotalCacheRead += 20
- Given 连续 2 轮（Turn 1: cache_write=50, Turn 2: cache_write=30）, When 累加后, Then TotalCacheWrite=80, PrevCacheWrite=50（Turn 1 resetPerTurn 后）

### TURN-024 — Cache Token Delta 计算正确性
**描述**: computePerTurnDeltas 正确计算 PerTurnCacheWrite / PerTurnCacheRead delta。

**验收标准**:
- Given TotalCacheWrite=80, PrevCacheWrite=50, When `computePerTurnDeltas`, Then PerTurnCacheWrite = 30
- Given TotalCacheRead 增量为 0（无 cache read）, When `computePerTurnDeltas`, Then PerTurnCacheRead = 0
- Given delta 计算结果为负（异常情况）, When `computePerTurnDeltas`, Then 负值被钳位为 0

### TURN-025 — Tokens Input 拆分写入正确性
**描述**: assistant turn 的 tokens_input = max(0, PerTurnInput - PerTurnCacheWrite - PerTurnCacheRead)。

**验收标准**:
- Given PerTurnInput=180, PerTurnCacheWrite=50, PerTurnCacheRead=30, When 写入, Then tokens_input=100, tokens_cache_write=50, tokens_cache_read=30, tokens_in（计算列）=180
- Given PerTurnInput=0（无 usage 数据）, When 写入, Then tokens_input=0, tokens_cache_write=0, tokens_cache_read=0
- Given PerTurnInput=50, PerTurnCacheWrite=30, PerTurnCacheRead=30, When tokens_input = max(0, 50-30-30), Then tokens_input=0（负值保护生效）

---

### TURN-026 — Accumulator 重置不丢失累计总量
**描述**: ResetSession 重置 TurnCount/Generation 但保留 TotalInput/TotalOutput/TotalCostUSD。

**验收标准**:
- Given acc.TotalInput=10000 TotalOutput=5000 TotalCostUSD=2.5, When ResetSession accumulator 重置, Then TotalInput/TotalOutput/TotalCostUSD 不变
- Given acc.Generation=1 TurnCount=5, When ResetSession, Then Generation=2 TurnCount=0

### TURN-027 — Service Restart 后 Generation 恢复
**描述**: 服务重启后，新 forwardEvents 从 turns 表恢复 generation。

**验收标准**:
- Given turns 表有 session_id="s1" generation=3 的记录, When 服务重启后 s1 的 forwardEvents 启动, Then acc.Generation=3
- Given turns 表无该 session 的记录（全新 session）, When forwardEvents 启动, Then acc.Generation=1

### TURN-028 — make check 全量通过
**描述**: 所有变更后 `make check`（含 -race 测试 + lint + build）三平台通过。

**验收标准**:
- Given 所有代码变更提交, When `make check`, Then `make test -race` 通过（Linux/macOS/Windows）
- Given 所有代码变更提交, When `make lint`, Then golangci-lint 无新增 error
- Given 所有代码变更提交, When `make build`, Then 构建成功
