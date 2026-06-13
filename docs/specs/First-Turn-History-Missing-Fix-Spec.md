---
type: spec
tags:
  - project/HotPlex
  - area/gateway
  - area/session
date: 2026-06-05
status: implemented
progress: 100
---
# 首轮 Turn 历史记录丢失修复 (First Turn History Missing Fix)

> **Issue**: #658
> **Status**: Implemented
> **Date**: 2026-06-05

## 问题描述

用户在 webchat 中创建新会话后，发送第一条消息（如"你是谁"），Worker 正常回复，随后再正常聊几轮。刷新页面后，第一条用户消息从历史记录中消失。后端 `GET /api/sessions/{id}/history` 接口返回的 turns 列表中缺少该条 user turn。

**表现**：仅首轮 user turn 丢失，后续轮次正常。对应的 assistant turn 通常仍可见。

## 根因分析

### 核心竞态：`acc.Generation` 初始化不同步

`bridge_forward.go` 中存在两条并发路径访问同一个 `sessionAccumulator`：

| 路径 | Goroutine | 操作 |
|------|-----------|------|
| `forwardEvents` L86-98 | forwardEvents goroutine | `getOrInitAccum` → 创建 acc (Generation=0) → 查询 `LatestGeneration` → 设置 `acc.Generation = 1` |
| `CaptureInbound` L564-568 | Handler/ReadPump goroutine | `getOrInitAccum` → 获取同一 acc → 读取 `acc.Generation` → 写入 user turn |

**竞态窗口**：如果 `CaptureInbound` 在 `forwardEvents` 完成初始化之前读取 `acc.Generation`，user turn 以 `Generation=0` 写入。后续 `forwardEvents` 将 `acc.Generation` 设为 `1`，assistant turn 以 `Generation=1` 写入。

**后果**：`QueryTurns` 查询 `MAX(generation)` = 1，然后 `WHERE generation = 1` 过滤，导致 `Generation=0` 的 user turn 不可见。

> **注**：PR #727 将 `resolveGeneration` 两步查询合并为 CTE（`WITH latest_gen AS (...)` 单条 SQL），消除了竞态窗口中 generation 查询与过滤之间的两次 DB 往返。竞态根因（`acc.Generation` 初始化不同步）仍存在，但 CTE 方案确保了 `QueryTurns` 的原子性和正确性。

### 竞态时序

```
forwardEvents goroutine          Handler goroutine
─────────────────────           ──────────────────
getOrInitAccum → acc (Gen=0)
LatestGeneration(DB)...         
                                 CaptureInbound
                                   getOrInitAccum → same acc
                                   Generation = acc.Generation = 0  ← 竞态!
                                   CaptureTurn({Generation: 0})
...DB query returns 0
acc.Generation = 1              

Worker Done 事件
  captureAssistantTurn
    Generation = acc.Generation = 1
    CaptureTurn({Generation: 1})
```

### 量化数据

| 指标 | 值 |
|------|-----|
| forwardEvents 初始化总耗时 | ~200μs（新会话，空 turns 表） |
| 竞态窗口大小 | ~100-200μs（`LatestGeneration` DB 查询耗时） |
| 正常路径 session 创建总耗时 | 500ms-3s（含 w.Start） |
| 竞态发生概率 | 低（正常路径有 >100ms 缓冲），但在高负载/GC pause/WS init 创建场景下可复现 |

### 次要可能原因

`w.Input()` 返回 `ErrKindTimeout` 时 `CaptureInbound` 不被调用（`handler.go:268-270`）。Worker 可能仍在处理输入并发送响应，但 user turn 从未持久化。

## 修复方案

### 方案 A（推荐）：CaptureInbound 中同步初始化 Generation

在 `CaptureInbound` 读取 `acc.Generation` 之前，增加同步初始化逻辑，确保 Generation 被正确设置。

**改动范围**：`internal/gateway/bridge_forward.go` `CaptureInbound` 方法，~15 行。

**延迟影响**：
- 首次调用：+100-200μs（1 次 `LatestGeneration` DB 查询，索引覆盖）
- 后续调用：0（`acc.Generation != 0`，跳过查询）
- 占 w.Start() 基准延迟：≤0.04%（相对于 500ms-3s）

**优势**：
- 精准修复，不涉及核心 goroutine 启动机制
- 零死锁风险
- 不影响 `createAndLaunchWorker` 的 7+ 调用路径

**代码修改**：

```go
// bridge_forward.go CaptureInbound — 在 L564 之后、L568 之前插入
func (b *Bridge) CaptureInbound(sessionID string, seq int64, eventType events.Kind, data any, platform, owner string) {
    b.captureDirected(sessionID, seq, eventType, data, "inbound")

    if eventType == events.Input && b.collector != nil {
        acc := b.getOrInitAccum(sessionID, "", time.Now())

        // Synchronize Generation initialization to prevent race with forwardEvents.
        // forwardEvents sets acc.Generation asynchronously after goroutine launch;
        // if CaptureInbound reads Generation before that initialization completes,
        // the user turn would be written with Generation=0 and become invisible
        // to QueryTurns which filters by MAX(generation).
        if acc.Generation == 0 {
            gen := int64(1)
            if b.turnsQuerier != nil {
                genCtx, genCancel := context.WithTimeout(context.Background(), 3*time.Second)
                latest, _ := b.turnsQuerier.LatestGeneration(genCtx, sessionID)
                genCancel()
                if latest > 0 {
                    gen = latest
                }
            }
            acc.Generation = gen
        }

        content := extractInputContent(data)
        turn := &eventstore.TurnWriteRequest{
            SessionID:  sessionID,
            Generation: acc.Generation,
            // ... rest unchanged
        }
        b.collector.CaptureTurn(turn)
    }
}
```

### 方案 B（可选防御层）：createAndLaunchWorker 同步屏障

在 `createAndLaunchWorker` 中 goroutine 启动后等待一个 ready 信号，确保 `forwardEvents` 已被调度执行。

**延迟影响**：+50-100μs per session creation（goroutine 调度开销），占 w.Start() 的 ≤0.02%。

**风险**：`attemptResumeFallback`（`bridge_worker.go:176`）从旧 forwardEvents goroutine 内部调用 `createAndLaunchWorker`。Go M:N 调度器保证新旧 goroutine 可并发运行，不会死锁。但需在 `GOMAXPROCS=1` 下测试验证。

**建议**：作为方案 A 的防御补充，非必需。

## 验收标准

### AC-1：首轮 user turn Generation 一致性

**Given** 一个新创建的 session
**When** 用户发送第一条消息并获得回复
**Then** turns 表中首轮 user turn 和 assistant turn 的 `generation` 值一致

验证 SQL：
```sql
SELECT id, generation, role FROM turns WHERE session_id = ? ORDER BY id ASC LIMIT 2;
-- 期望：两条记录的 generation 值相同
```

### AC-2：页面刷新后首轮消息可见

**Given** 用户在新会话中发送了至少 3 条消息并获得回复
**When** 刷新 webchat 页面
**Then** 所有 user turn 和 assistant turn 均在历史记录中可见，包括首条消息

### AC-3：无性能回退

**Given** 方案 A 实施后
**When** 使用 `make test-short` 运行测试
**Then** 所有现有测试通过，无新增超时

### AC-4：单元测试覆盖

**新增测试用例**：
1. `TestCaptureInbound_GenerationSync`：模拟 `forwardEvents` 未初始化时 `CaptureInbound` 正确设置 Generation
2. `TestCaptureInbound_GenerationAlreadySet`：Generation 已被 forwardEvents 设置时跳过 DB 查询
3. `TestCaptureInbound_ConcurrentGenerationInit`：两个 goroutine 并发调用时的 Generation 一致性

## 影响范围

| 文件 | 改动类型 | 行数 |
|------|---------|------|
| `internal/gateway/bridge_forward.go` | 修改 `CaptureInbound` | +15 |
| `internal/gateway/bridge_forward_test.go` | 新增测试 | +80 |

## 关联

- **相关 Spec**：`docs/specs/Session-History-Persistence-Spec.md`
- **相关 Spec**：`docs/specs/Turns-Materialized-Table-Spec.md`
- **相关代码**：`internal/gateway/bridge_forward.go:559-580`（CaptureInbound）
- **相关代码**：`internal/gateway/bridge_forward.go:86-98`（forwardEvents Generation 初始化）
- **相关代码**：`internal/eventstore/store.go`（QueryTurns CTE 合并 generation 解析，PR #727）
- **相关代码**：`internal/eventstore/sql/queries/turns.query_with_gen.sql`（CTE SQL 模板）
