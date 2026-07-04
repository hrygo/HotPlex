# CodexCLI Delta Integrity Fix Spec

**状态**: Draft
**分支**: fix/codexcli-delta-integrity
**日期**: 2026-07-04
**关联**: 流式 delta 丢失复核（#838 讨论衍生）

---

## 1. 背景与核实结论

### 1.1 复核中发现的复核分析本身的事实错误

复核分析（"三层对照后的修正判定"）在其第一节 **"Claude Code mapper：原始判断有误，须修正"** 中有两处事实错误。本 spec 不针对 Claude Code，但为避免遗留误判先在此纠正：

1. **"stream_event 路径不调用 recordSentDelta"** — 错误。`mapper.go:134` 明确调用 `m.recordSentDelta(msgID+"_"+p.Type, p.Content)`。
2. **"assistant 消息用全新独立 ID `cc-<sessionID>-<blockIndex>`"** — 代码中不存在该 ID 生成。Claude Code `parseAssistant` 生成的 `StreamPayload` **没有填 `MessageID`**（`parser.go:213-216`），Mapper 在 `mapStream` 里 fallback 到 `"assistant_msg"`（`mapper.go:128-130`）。两条路径共用同一 `msgID+"_"+p.Type` 命名空间，因此 `assistant` 全量快照确实会取回 delta（因 `IsDelta=false` 走 `getDeltaText`，`sentTexts` 非空 → 只发追加部分）。复核分析的"重复发送"结论方向上接近，但其 ID 命名空间论证路径是错的。

**本 spec 不修复 Claude Code mapper**：其 `assistant` 全量快照复用 delta ID 的设计是有意的兜底（codex app-server 之前的 Claude CLI 版本在 assistant 块里补发完整文本，diff 提取追加部分作为修复）。其 `getDeltaText` 同样缺少前缀校验，但 Claude Code 的 `assistant` 全量快照与 `stream_event` delta 在实际协议里保证同源单调追加，冲突概率极低。

### 1.2 本 spec 聚焦：CodexCLI mapper 的异构流融合缺陷

CodexCLI mapper 的 `sentTexts` + `getDeltaText` 机制存在设计缺陷，是流式 delta 处理链路中唯一的"过度设计"点。

**核实确认的代码事实**（`internal/worker/codexcli/mapper.go`，行号随提交变动，下同）：

两条异构来源共用同一 `item.ID` 命名空间：

| 来源 | 通知方法 | 文本语义 | 调用路径 |
|---|---|---|---|
| **纯增量** | `item/agentMessage/delta` | 仅本次新增片段 | `mapNotifDelta` → `recordSentDelta(id, delta)` 累加到 `sentTexts[id]`，直接发 `delta` 本身 |
| **全量快照** | `item/updated` | 截至当前的全量文本 | `mapItemUpdated` → `getDeltaText(id, fullText)` 做尾部 diff |

> **注**：`item/completed` 对 `ItemAgentMessage` 在 `MapNotification` 派发处被短路（只发 `MessageEnd` + `endMessage`），**不会**走到 `mapItemCompleted` 的 `getDeltaText` 路径；`mapItemCompleted` 里保留的 `case ItemAgentMessage` 分支是防御性兜底（已加注释）。因此本 spec 的修复在生产中**仅经 `item/updated` 路径生效**。

`getDeltaText` 算法（旧实现）：

```go
sentRunes := []rune(m.sentTexts[itemID])
currRunes := []rune(currentText)
lastLen := len(sentRunes)
currLen := len(currRunes)
if currLen <= lastLen {
    return ""  // ← 静默丢弃
}
deltaRunes := currRunes[lastLen:]  // ← 无前缀一致性校验
delta := string(deltaRunes)
m.sentTexts[itemID] += delta
return delta
```

**问题**：算法假设 `currentText` 的前缀严格等于 `sentTexts[id]`，但只比较长度，不比较内容。当 codex app-server 后端在 delta 流与快照之间存在任何不一致（重采样、修正、截断、重排），即触发以下两类失败：

- **类型 A（静默丢失）**：`currLen <= lastLen` → 返回空字符串，快照与已发文本长度不符时整段差异被吞没。
- **类型 B（错位拼接）**：`currLen > lastLen` 但前缀不一致 → 返回 `currRunes[lastLen:]`，这段"delta"既不是真实增量也不是真实后缀，下游拼接出错乱文本。

Reference 3 给出的具体漂移场景：`sentTexts="Hel"+"lo"` (len=5) vs 快照 `"Hola"` (len=4) → 触发类型 A，4 字符的差异静默丢失。

### 1.3 影响评估

- **当前触发概率**：中低。codex app-server 在正常流程下保证文本单调追加，`item/updated` 快照是 delta 流的累积复本。
- **触发条件**：codex app-server 后端版本迭代引入文本重采样/修正/截断，或并发/重试导致同一 `item.ID` 的事件乱序到达。
- **后果**：静默丢失（类型 A）下游无感知；错位拼接（类型 B）产生乱码且持久化到 event store、飞书卡片。
- **严重度**：设计缺陷，当前可控，代码脆性高。一旦 codex 后端行为变化，丢失从"静默"变成"系统性且不可检测"。

### 1.4 不在本 spec 修复范围的项

| 丢失源 | 状态 | 理由 |
|---|---|---|
| Hub 背压丢弃 `MessageDelta` | **不修**（`hub.go:40-41`） | 人为接受的设计取舍，已有 `dropped=true` 标记 + bridge `reconcileDroppedDeltas` 在 Done 事件上打标。是实际最大丢失源但非 bug。 |
| 飞书卡片 flush 限流 | **不修**（`streaming.go:686`） | 平台限流降级，已有 90% 完整性校验 + warning append。是第二丢失源但属平台层。 |
| Claude Code mapper 同类 diff | **不修** | 实际触发概率极低（同源单调追加），单独评估。 |

---

## 2. 修复目标

1. 让 CodexCLI mapper 的 `getDeltaText` 在全量快照与已发 delta 不一致时**不静默丢失、不错位拼接**。
2. 检测到前缀不一致时，发出明确的"纠正"delta（从差异点开始的剩余文本），并发出诊断信号。
3. 保持正常流程（单调追加）下的行为不变：只发追加部分，不重发已发内容。
4. 添加单元测试覆盖：正常追加、前缀一致追加、前缀不一致（类型 A/B）、空快照、快照短于已发。
5. 不改动 `item/agentMessage/delta` 路径（纯增量，已正确）。

---

## 3. 设计

### 3.1 `getDeltaText` 增加前缀一致性校验

修改 `internal/worker/codexcli/mapper.go:getDeltaText`：

```go
func (m *Mapper) getDeltaText(itemID, currentText string) string {
    m.mu.Lock()
    defer m.mu.Unlock()

    if m.sentTexts == nil {
        m.sentTexts = make(map[string]string)
    }

    sentRunes := []rune(m.sentTexts[itemID])
    currRunes := []rune(currentText)
    lastLen := len(sentRunes)
    currLen := len(currRunes)

    // Case 1: 快照短于等于已发 → 检查是否完全一致。
    // 一致：无新内容，返回空（正常幂等）。
    // 不一致：前缀漂移（类型 A），用公共前缀后剩余的快照作为纠正 delta。
    if currLen <= lastLen {
        if string(currRunes) == string(sentRunes) {
            return "" // 幂等：快照与已发完全一致
        }
        // 前缀漂移：找到最长公共前缀，发送快照从该点之后的部分作为纠正。
        common := commonPrefixLen(sentRunes, currRunes)
        // ...见 3.2
    }

    // Case 2: 快照长于已发 → 先验证前缀一致。
    // 一致：正常追加，返回 currRunes[lastLen:]。
    // 不一致：类型 B，从公共前缀后发送剩余作为纠正。
    // ...
}
```

### 3.2 纠正 delta 策略

当检测到前缀不一致时：

1. 计算 `sentRunes` 与 `currRunes` 的最长公共前缀长度 `common`。
2. 纠正 delta = `string(currRunes[common:])`（快照在公共前缀之后的全部内容）。
3. **重置** `sentTexts[itemID] = currentText`（以快照为准重新建立基线）。
4. 发送 `MessageDelta` 带纠正 delta，并在 mapper 上记录一次诊断计数器/日志（`slog.Warn`，含 `item_id`、`sent_len`、`snapshot_len`、`common_prefix`）。

**为什么不"只标记 dropped 不发 delta"**：那样下游永远收不到纠正后的文本，最终展示停留在错误版本。发送纠正 delta 让下游能通过追加拼接得到正确文本（前提：下游是追加式累加，不会因纠正 delta 而重置）。

**为什么不"重发全文"**：下游累加器会重复拼接。必须只发"从公共前缀之后的差异"。

### 3.3 下游累加器兼容性核实

`bridge_forward.go:extractTurnContent` 对 `MessageDelta` 做的是 `fc.turnText.WriteString(content)`（`bridge_forward.go:318-322`），纯追加。event store 的 `CaptureDeltaString` 同样追加。飞书卡片 `StreamingCardController` 追加到 buffer。因此"发纠正 delta + 重置基线"对下游安全。

**风险**：下游累加器已拼接的错误前缀无法撤回。这是 codex 后端漂移的固有后果，不在 mapper 可修复范围内。mapper 能做的是：① 不让错误继续扩散；② 让最终文本至少包含正确后缀。真实严重场景应标记 `dropped` 让 bridge 触发完整性提示。

### 3.4 诊断信号

在 mapper 上新增字段 `driftCount atomic.Int64`，每次前缀漂移递增。`Reset()` 不重置该计数器（跨 turn 累计，用于可观测性）。在漂移时 `slog.Warn` 一条日志：

```
codexcli: delta prefix drift detected, sending corrective delta
  item_id=<id> sent_len=<N> snapshot_len=<M> common_prefix=<K>
```

可选：在 `turn/completed` 的 `DoneData.Stats` 注入 `delta_drifts` 计数（类似 bridge 注入 `dropped`）。**本 spec 范围内只做日志 + mapper 字段，不改 DoneData struct**，避免跨包改动。后续若需可观测性再扩展。

### 3.5 `commonPrefixLen` 辅助函数

```go
// commonPrefixLen returns the length of the longest common prefix of two rune slices.
func commonPrefixLen(a, b []rune) int {
    n := len(a)
    if len(b) < n {
        n = len(b)
    }
    for i := 0; i < n; i++ {
        if a[i] != b[i] {
            return i
        }
    }
    return n
}
```

放 `mapper.go` 内部私有函数。

---

## 4. 变更清单

| 文件 | 变更 |
|---|---|
| `internal/worker/codexcli/mapper.go` | 重写 `getDeltaText` 加前缀校验 + 纠正 delta；新增 `commonPrefixLen`；新增 `driftCount` 字段；漂移时 `slog.Warn` |
| `internal/worker/codexcli/worker_test.go` | 新增 table-driven 测试：正常追加 / 幂等 / 类型 A 漂移 / 类型 B 漂移 / 空快照 |

**不改**：`mapNotifDelta`（delta 路径已正确）、`recordSentDelta`、Bridge、Hub、飞书卡片、Claude Code mapper。

---

## 5. 测试矩阵

`TestGetDeltaText_DriftScenarios`，table-driven，`t.Parallel()`：

| Case | sentTexts 初值 | currentText | 期望 delta | 期望 sentTexts 终值 | 漂移计数 |
|---|---|---|---|---|---|
| 首次空 | "" | "Hello" | "Hello" | "Hello" | 0 |
| 正常追加 | "Hello" | "Hello world" | " world" | "Hello world" | 0 |
| 幂等 | "Hello" | "Hello" | "" | "Hello" | 0 |
| 类型 A 漂移（短于） | "Hello" | "Hola" | "la"（公共前缀 "H"，剩余 "ola"？见下）| "Hola" | 1 |
| 类型 A 漂移（等长不同） | "Hello" | "Hallo" | "llo"（公共前缀 "H"，剩余 "allo"）| "Hallo" | 1 |
| 类型 B 漂移（长于但前缀不一致） | "Hel" | "Hallo!" | "lo!"（公共前缀 ""，剩余 "Hallo!"？见下）| "Hallo!" | 1 |
| 空快照 | "Hello" | "" | "" | "Hello" | 0 |

**类型 A "Hello"→"Hola" 的期望需要核对**：公共前缀是 "H"（`sentRunes[0]='H'` vs `currRunes[0]='H'` 一致，`[1]='e'` vs `[1]='o'` 不一致），`common=1`，纠正 delta = `currRunes[1:]` = `"ola"`，重置 `sentTexts="Hola"`。**但下游已收到 "Hello"**，再追加 "ola" 得到 "Helloola" 而非 "Hola"。这是固有缺陷——下游累加器无法撤回已发错误前缀。

**这是本 spec 必须承认的根本限制**：mapper 无法让下游"撤回"已发的错误文本。mapper 能做的只是：
- 不让错误继续（重置基线到正确快照）
- 让后续 delta 基于正确基线

因此纠正 delta 的语义是"从现在起以快照为准"，而非"修复下游已拼接的错误"。下游最终文本会是 "已发错误前缀 + 纠正后缀"。

### 5.1 测试策略调整

鉴于上述限制，测试矩阵的"期望 delta"列应反映"纠正 delta = 快照从公共前缀之后"，而**不**断言下游最终拼接结果（那超出 mapper 职责）。测试只验证 mapper 单次 `getDeltaText` 返回值 + `sentTexts` 终值 + 漂移计数。

测试用例修正：

| Case | sentTexts | currentText | 期望返回 | 期望 sentTexts | drifts |
|---|---|---|---|---|---|
| 首次空 | "" | "Hello" | "Hello" | "Hello" | 0 |
| 正常追加 | "Hello" | "Hello world" | " world" | "Hello world" | 0 |
| 幂等 | "Hello" | "Hello" | "" | "Hello" | 0 |
| 漂移-短于 | "Hello" | "Hola" | "ola"（common=1）| "Hola" | 1 |
| 漂移-等长不同 | "Hello" | "Hallo" | "allo"（common=1）| "Hallo" | 1 |
| 漂移-长于前缀不一致 | "Hel" | "Hallo!" | "allo!"（common=1，sentRunes[1]='e' vs currRunes[1]='a'）| "Hallo!" | 1 |
| 空快照不变基线 | "Hello" | "" | "" | "Hello" | 0 |

核对"漂移-长于"：`common=1`（`'H'` 一致，`'e'` vs `'a'` 不一致），返回 `currRunes[1:]` = `"allo!"`。✓

### 5.2 现有测试不回归

`TestMapNotificationAgentMessageStateMachine`（`worker_test.go`）验证 delta×3 + completed 全量="Hello world!" 场景。在新逻辑下：
- 3×`recordSentDelta`: `sentTexts["msg_1"]` = "Hello"+" world"+"!"
- `item/completed` 在 `MapNotification` 派发处短路 `ItemAgentMessage`（见 §1.2 注），直接发 `MessageEnd` + `endMessage`，**不调用 `getDeltaText`**。`mapItemCompleted` 中的 `case ItemAgentMessage` 是防御性死分支（已加注释）。

**回归安全**。现有测试的 step 3 期望 `item/completed` 只返回 MessageEnd（len=1），不期望 MessageDelta。新逻辑保持此行为（短路未改）。新增 `driftCount==0` 断言锁定 happy-path 不漂移。✓

---

## 6. 验证

```bash
# 单元测试
go test ./internal/worker/codexcli/... -run TestGetDeltaText -count=1 -race -v

# 全包回归
go test ./internal/worker/codexcli/... -count=1 -race

# CI 质量
make quality
```

---

## 7. 限制与后续

- **不修复下游已拼接错误**：mapper 无法撤回已发 delta。漂移后下游文本 = 错误前缀 + 纠正后缀。真实修复需要 codex 后端保证单调追加，或引入"重置下游累加器"的协议事件（超出本 spec）。
- **不改 DoneData.Stats**：`delta_drifts` 计数暂只在日志。后续可在 bridge 注入 DoneData（类似 `reconcileDroppedDeltas` 注入 `dropped=true`），但需改 `events.DoneData` struct + bridge 逻辑，本 spec 不含。
- **Claude Code mapper 不在范围**：同类 `getDeltaText` 但触发概率极低，单独评估。
- **Hub 背压、飞书卡片限流**：不在范围，见 1.4。

---

## 8. 状态

- [x] 实现：`getDeltaText` 重写 + `commonPrefixLen` + `applyDrift` + `driftCount` + 空快照 short-circuit
- [x] 测试：table-driven 7 case + 累计/Reset + 3 个序列场景（幂等/追加/漂移）
- [x] 回归：`TestMapNotificationAgentMessageStateMachine` 通过
- [x] 质量：`make quality` 通过