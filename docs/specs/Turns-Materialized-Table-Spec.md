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

> 版本: v2.1
> 日期: 2026-05-19
> 状态: Proposed
> 前置: #451 cache token 修复（已合入本方案）
> 审计: 上下游影响全覆盖（v2.0 → v2.1 补充 §6.4-6.6, §12）

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
| `tokens_input` | `0` | `acc.PerTurnInput - PerTurnCacheWrite - PerTurnCacheRead` |
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
WHERE session_id = ? AND generation = ? AND id < ?
ORDER BY id DESC
LIMIT ?
```

游标用 `id`（不是 seq），保证重置后翻页正确。

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

### 6.2 Generation 初始化

`forwardEvents` goroutine 启动时，通过 TurnQuerier 查询当前 generation：

```go
// bridge_forward.go forwardEvents() 启动阶段
// b.turnsQuerier 在 Bridge 构造时注入（与 b.collector 同源）
gen := int64(1)
if b.turnsQuerier != nil {
    if latest, _ := b.turnsQuerier.LatestGeneration(context.Background(), sessionID); latest > 0 {
        gen = latest
    }
}
acc := b.getOrInitAccum(sessionID, opts.workDir, startTime)
acc.Generation = gen
```

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
    QueryTurnsBefore(ctx context.Context, sessionID string, beforeID int64, limit int) ([]*TurnRecord, error)
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

**`EventTx` 接口扩展**：

```go
type EventTx interface {
    Append(ctx context.Context, event *StoredEvent) error
    AppendTurn(ctx context.Context, turn *TurnWriteRequest) error
    Commit() error
}
```

**`flushBatch`**：遍历 batch，按类型分派：
- `req.turn != nil` → `tx.AppendTurn(ctx, req.turn)`
- `req.event != nil` → `tx.Append(ctx, req.event)`

### 7.5 Bridge 写入

**助手 turn**（done 事件处理路径，L154-155 之间）：

```go
b.injectSessionStats(env, acc)
// 写入 turns 表
b.captureAssistantTurn(sessionID, env.Seq, acc, turnText.String(),
    sessOwner, sessPlatform, env.Timestamp)
acc.resetPerTurn()
```

**用户 turn**（`captureDirected` 扩展）：

```go
// 在 CaptureInbound 路径中，Input 事件同时写 turn 记录
if eventType == events.Input && b.collector != nil {
    b.collector.CaptureTurn(buildUserTurn(sessionID, seq, data, sessPlatform, sessOwner, acc))
}
```

**synthetic 事件**（crash/timeout）：

`captureSyntheticEvent` 同时写入 assistant turn（`success=false`）。

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
  ├── QueryTurns(sessionID, gen, limit, offset)       → ORDER BY id ASC
  └── QueryTurnsBefore(sessionID, gen, cursorID, limit) → WHERE id < cursor

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

## 11. 验证

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

## 12. 破坏性变更清单

### 12.1 外部 API

| 端点 | 变更 | 影响 |
|------|------|------|
| `GET /api/sessions/{id}/history` | `id` 字段类型 `string` → `int64`；查询参数 `before_seq` → `before_id`；新增 `generation`/`turn_num`/`tokens_input`/`tokens_cache_write`/`tokens_cache_read` 字段 | WebChat 前端需适配 |
| `GET /admin/sessions/{id}/stats` | `TurnStats` 新增 `generation`/`total_tokens_input`/`total_tokens_cache_write`/`total_tokens_cache_read` | Admin WebUI 需适配 |
| `GET /api/cron/jobs/{id}/runs` | 同 `TurnStats` 变更 | CLI `--json` 输出新增字段（向后兼容） |

### 12.2 内部 Go 接口

| 接口/方法 | 变更 | 所有实现方 |
|-----------|------|-----------|
| `TurnQuerier.QueryTurnsBefore` | `beforeSeq` → `beforeID` | `SQLiteStore` + `mockTurnsStore` (api_test.go:148) |
| `TurnQuerier.LatestGeneration` | 新增方法 | `SQLiteStore` + `mockTurnsStore` |
| `TurnQuerier.DeleteExpiredTurns` | 新增方法 | `SQLiteStore` + `mockTurnsStore` |
| `EventTx.AppendTurn` | 新增方法 | `sqliteTx` |
| `Bridge.CaptureInbound` | 新增 `platform, owner string` 参数 | `handler.go:126,236` 两处调用 |
| `TurnRecord.ID` | `string` → `int64` | `scanTurns` (store.go:456) 删除合成 ID 生成 |

### 12.3 Mock/测试更新

| 文件 | 变更项 |
|------|--------|
| `internal/gateway/api_test.go` | `mockTurnsStore` 三个方法签名 + 新增 `LatestGeneration`/`DeleteExpiredTurns` mock；所有 `ts.On(...)` expectation 更新；`before_seq=5` → `before_id=5` |
| `internal/eventstore/turns_view_test.go` | 整体重写：视图 SQL → turns 表 INSERT + SELECT |
| `internal/gateway/ctrl_test.go` | `DeleteExpiredEvents` mock（已过时）检查 |
| `internal/gateway/conn_test.go` | 同上 |

### 12.4 配置新增

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `events.retention` | duration | `168h`（7 天） | events + turns TTL |

---

## 13. 实施步骤（修订）

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
