---
type: spec
tags:
  - project/HotPlex
date: 2026-06-23
status: implemented
progress: 100
related_issues:
  - Turn-Summary-Spec.md (数据流基准)
  - Worker-GAP-Analysis-2026-05-15.md (OCS vs CC 能力差距)
---
# Worker Turn Summary Parity Spec — codex / acp / opencode 对齐 claudecode

## 概述

Turn summary 卡片（Slack/Feishu/WebChat）的数据质量取决于各 Worker 是否产出了完整的 stats 事件。基准实现 **claudecode worker** 覆盖全部字段；**opencodeserver worker** 作为次基准覆盖 9/11 字段；而 **codexcli** 与 **acp** worker 存在系统性数据缺失。

本 spec 定位三 worker 的 turn summary 数据产出 gap，给出逐字段弥补方案。

> **范围**：仅 turn summary 数据字段（context / token / model / tool / cost）。Worker 能力全景见 `Worker-GAP-Analysis-2026-05-15.md`；turn summary 数据流与渲染见 `Turn-Summary-Spec.md`。

---

## 1. 数据流回顾

```
Worker 产出事件 ──► SessionAccumulator 聚合 ──► snapshot() 注入 Done.Data.Stats["_session"]
                                                                    │
                          messaging.ExtractTurnSummary ◄────────────┘
                                                                    │
                         FormatTurnSummary / FormatTurnSummaryRich ◄┘
                                                                    │
                         Slack / Feishu / WebChat 渲染 ◄────────────┘
```

`SessionAccumulator`（`internal/gateway/session_stats.go`）有三个数据入口：

| 入口 | 方法 | 数据来源 |
|------|------|---------|
| **Done stats** | `mergePerTurnStats(DoneData)` | Done 事件 `Data.Stats`，当前**仅识别两种格式**：CC 的 `usage`+`model_usage`，OCS 的 `tokens`+`cost` |
| **ContextUsage 事件** | `mergeContextUsage(ContextUsageData)` | `context_usage` 控制事件，**context_fill 的唯一来源** |
| **ToolCall 事件** | bridge forwardEvents 累加 | `tool_call` 事件的 `ToolCallData.Name` |

`SessionAccumulator` 假设 **"Workers report cumulative totals"**（`session_stats.go:99`），per-turn delta 由 `computePerTurnDeltas` 从累计值相减得出。

---

## 2. 字段产出矩阵（核心 gap）

| 字段 | claudecode (基准) | opencodeserver (次基准) | codexcli | acp |
|------|:---:|:---:|:---:|:---:|
| `context_fill` | ✅ context_usage | ✅ 真实计算 | ❌ **管道断裂** | ⚠️ 依赖 usage_update |
| `context_window` | ✅ model_usage | ❌ maxTokens=0 | ❌ | ⚠️ 依赖 usage_update |
| `context_pct` | ✅ | ❌ | ❌ | ⚠️ |
| `model_name` | ✅ | ✅ 双路径 | ⚠️ **仅 rerouted** | ❌ **完全缺失** |
| `total_input_tok` | ✅ | ✅ | ⚠️ **字段不全+非累计** | ✅ CC 格式 |
| `total_output_tok` | ✅ | ✅ | ⚠️ 同上 | ✅ |
| `turn_input_tok` | ✅ delta | ✅ delta | ⚠️ | ✅ |
| `turn_output_tok` | ✅ delta | ✅ delta | ⚠️ | ✅ |
| `tool_call_count` | ✅ | ✅ | ✅ | ✅ |
| `tool_names` | ✅ | ✅ | ✅ | ⚠️ **仅 CC agent 可靠** |
| `cost` | ✅ | ✅ | ❌ **缺失** | ✅ |

**严重度统计**：codexcli 6 项缺口（最严重），acp 3 项核心缺口，opencode 2 项（均受限于上游 API）。

---

## 3. codexcli Worker Gap（P1，最严重）

### 3.1 核心问题

**Gap-C1：context_usage 管道完全断裂**
- `commands.go:22-31` `get_context_usage` 调用 `thread/read` 后返回 `{"raw": string(resp)}`（未解析的原始 JSON 字符串）。
- `pkg/events/helpers.go:60-98` `MapContextUsageResponse` 期望顶层 camelCase 键（`totalTokens`/`maxTokens`/`model`），`"raw"` 不匹配任何键 → `TotalTokens=0`。
- `bridge_forward.go:913-916` 的守卫是 `cu.MaxTokens > 0 || cu.TotalTokens > 0 || cu.Model != ""` —— 三者皆空才跳过 `mergeContextUsage`（PR #779 review P3-1 勘误：原描述只提 TotalTokens，漏了 MaxTokens/Model 两个短路条件，可能误导修复者只补 TotalTokens）。
- **后果**：`context_fill` / `context_window` / `context_pct` 三字段恒为 0。

**Gap-C2：token 字段不全且非累计**
- `mapper.go:667-688` `trackedUsageStats` 产出 CC 兼容的 `usage` map，但：
  - 缺 `cache_creation_input_tokens`（`CodexTokenUsage` 有 `CachedInputTokens` 映射到 `cache_read_input_tokens`，但无 cache_creation 对应字段）。
  - `m.lastUsage` 来自 `thread/tokenUsage/updated` 的 `last`（per-turn 增量），每次 `turn/started` 清空（`mapper.go:104`），与 SessionAccumulator "cumulative totals" 假设相悖。
- **后果**：`total_input_tok` 偏低（缺 cache 维度），delta 计算虽然因每轮重置而"碰巧正确"，但累计值失真。

**Gap-C3：model_name 仅在 rerouted 时产出**
- `mapper.go:657-665` `trackModelRerouted` 仅监听 `model/rerouted` 事件（模型被重路由才触发）。
- 正常对话路径不产出 model → `model_name` 大多数情况为空。

**Gap-C4：cost 完全缺失**
- `trackedUsageStats` 不产出任何 cost 字段，`mergePerTurnStats` 读取的 `total_cost_usd`/`cost` 均不存在。

### 3.2 修复方案

| ID | 改点 | 位置 | 方案 |
|----|------|------|------|
| C1 | context_usage 解析 | `codexcli/commands.go:22-31` | 解析 `thread/read` 响应 JSON，从最近 assistant message 或 token usage 提取 `totalTokens`/`maxTokens`/`model`，返回 camelCase map（参考 OCS `commands.go:157-168` `queryContextUsage`） |
| C2a | token 累计修正 | `codexcli/mapper.go:trackedUsageStats` | 优先采用 `thread/tokenUsage/updated` 的 `total`（累计）字段，而非 `last`（增量）；若无 total 则保留 last 但在 stats 标注 `cumulative:false` |
| C2b | cache 字段补全 | 同上 | 补 `cache_creation_input_tokens`（若 codex 协议无对应字段，标注为 0 并在 spec 注明协议限制） |
| C3 | model 常态化 | `codexcli/mapper.go` | 从 `thread/tokenUsage/updated` 或 session 元数据提取当前 model（不只靠 rerouted） |
| C4 | cost 产出 | 同上 | 若 codex `tokenUsage` 含 cost 字段则映射到 `total_cost_usd`；若无则标注协议限制 |

### 3.3 待实现时确认

- codex `thread/read` 响应结构：是否暴露 context 用量与 maxTokens？需对照 codex app-server 协议（`Codex-AppServer-Worker-Spec.md`）。
- codex `thread/tokenUsage/updated` 的 `total` vs `last` 语义，及是否含 cost。

---

## 4. acp Worker Gap（P2）

### 4.1 核心问题

**Gap-A1：model_name 完全缺失**
- 全链路无 model 产出：`mapper.go:466-515` `buildStats` 不写 `model_usage`/`model`；`worker.go:798-805` `handleContextUsage` 返回值无 `model` 字段；`client.go:416-419` `PromptResult` 结构体无 model。
- `mergePerTurnStats` 找不到 `model_usage`，`mergeContextUsage` 收到 `cu.Model==""` → `ModelName` 恒空。

**Gap-A2：tool_names 非 ClaudeCode agent 不可靠**
- `mapper.go:590-606` `extractToolName` 优先取 `_meta.claudeCode.toolName`，fallback 用 `u.Kind`（`mapper.go:283-288`）。
- 非 ClaudeCode agent 若 `kind` 为空（仅有 `title`），toolName 聚合到空字符串 key。

**Gap-A3：context 强依赖 agent 主动发 usage_update**
- `mapper.go:549-553` `usageSnapshot` 仅由 `usage_update` notification 填充，无 fallback。
- ACP 协议的 `usage_update` 是可选的；若 agent 不发，context_fill/window/pct 全部归零。

### 4.2 修复方案

| ID | 改点 | 位置 | 方案 |
|----|------|------|------|
| A1 | model 补全 | `acp/mapper.go:buildStats` | 写 `stats["model_usage"]`（CC 格式）；model 来源优先级：usage_update → session/info → agent card 元数据 |
| A2 | tool_name 通用化 | `acp/mapper.go:extractToolName` | 扩展 fallback 链：`_meta.claudeCode.toolName` → `kind` → 从 tool call content 解析 tool 名 → `title` |
| A3 | context fallback | `acp/worker.go:handleContextUsage` | 若 `usageSnapshot` 为空，调用 ACP `session/info`（或等价方法）获取 context 用量；仍不可得则返回空（标注协议限制） |

### 4.3 待实现时确认

- ACP 协议是否提供 session/info 或类似方法暴露 model 与 context 用量？需对照 `ACP-Worker-Spec.md` 与上游 ACP 规范。

---

## 5. opencodeserver Worker Gap（P3，受限于上游）

### 5.1 核心问题

**Gap-O1：context_window 恒为 0**
- `commands.go:167` `queryContextUsage` 硬编码 `"maxTokens": 0`。
- OCS HTTP REST API 不暴露模型上下文窗口大小（`session_stats.go:134` 注释 "OCS scenario" 即指此行）。
- **后果**：`context_pct` 无法计算（`computeContextPct` 在 `ContextWindow<=0` 时返回 0）。

**注**：OCS 的 `context_fill`（`commands.go:161-166`，`lastInput+lastCacheRead+lastCacheWrite`）、token、model、tool、cost 全部正常产出，是三非-CC worker 中的基准。

### 5.2 修复方案

| ID | 改点 | 位置 | 方案 |
|----|------|------|------|
| O1 | context_window 补全 | `opencodeserver/commands.go:queryContextUsage` | 优先级：① 查询 OCS model/provider 元数据 API → ② 维护 `model→contextWindow` 静态映射表（参考 `internal/security/model.go` 已有的模型注册） → ③ 标注协议限制 |

### 5.3 待实现时确认

- OCS 是否有 `/model` 或 `/provider` 端点暴露 context window？需对照 `~/opencode` 源码。

---

## 6. SessionAccumulator 适配层（可选增强）

当前 `mergePerTurnStats` 硬编码识别 CC/OCS 两种格式（`session_stats.go:59-96`）。两种修复路径：

| 路径 | 方案 | 优劣 |
|------|------|------|
| **A. Worker 侧兼容**（推荐） | 让 codex/acp 产出 CC 或 OCS 兼容格式（acp 已做，codex 部分做了） | 改动局部，无需动核心聚合器 |
| **B. 聚合器侧适配** | 在 `mergePerTurnStats` 增加 codex/acp 格式分支 | 核心代码膨胀，违反现有"两种格式"简洁假设 |

**推荐路径 A**：Worker 负责产出兼容格式，聚合器保持稳定。本 spec 所有修复方案均遵循此路径。

---

## 7. 实施计划（Batch）

### Batch 1: codexcli P0/P1（最严重，优先）

| ID | 改点 | 优先级 |
|----|------|:---:|
| C1 | context_usage 解析修复 | P0 |
| C2a | token 累计修正（total vs last） | P1 |
| C3 | model 常态化 | P1 |
| C2b | cache 字段补全（受协议限制） | P2 |
| C4 | cost 产出（受协议限制） | P2 |

### Batch 2: acp P0/P1

| ID | 改点 | 优先级 |
|----|------|:---:|
| A1 | model_name 补全 | P0 |
| A2 | tool_name 通用化 | P1 |
| A3 | context fallback（受协议限制） | P2 |

### Batch 3: opencode P2（受限于上游）

| ID | 改点 | 优先级 |
|----|------|:---:|
| O1 | context_window 静态映射表 | P2 |

---

## 8. 验收标准

每个修复项需满足：

1. **单元测试**：对应 worker 的 mapper/converter/commands 测试覆盖新字段产出。
2. **集成验证**：在真实 codex/acp/ocs agent 对话后，turn summary 卡片显示对应字段非空。
3. **回归**：claudecode worker 的 turn summary 不受影响（现有 `turn_summary_test.go` 全绿）。
4. **协议限制透明化**：受上游协议限制无法产出的字段（如 codex cache_creation、ocs context_window），在 spec 与代码注释中明确标注，不得静默归零。

---

## 9. 相关文档

- `Turn-Summary-Spec.md` — turn summary 数据流与渲染设计（§1.3 Worker 兼容性表需补充 codex/acp 列）
- `Worker-GAP-Analysis-2026-05-15.md` — OCS vs CC 能力全景差距
- `Codex-AppServer-Worker-Spec.md` — codex app-server 协议参考
- `ACP-Worker-Spec.md` / `ACP-Worker-Enhancement-Spec.md` — ACP 协议参考

---

## 10. 实施记录（2026-06-29）

本节记录实现期对上游协议的确认结果，以及与原方案的偏差。协议事实来自 codex 源码（`codex-rs/app-server-protocol/src/protocol/v2/thread.rs`）、opencode 源码（`~/opencode`）与 ACP client 实现。

### 10.1 协议确认（澄清原 spec 的"待实现时确认"）

| 项 | 原 spec 假设 | 实测结论 |
|----|------|---------|
| codex `thread/read` | 暴露 context 用量 | **否**。`Thread.turns[].items` 无 token 计数；usage 仅由 `thread/tokenUsage/updated` 通知承载 |
| codex `tokenUsage` 结构 | 含 `last`/`total` | 确认。**额外含 `modelContextWindow`**（maxTokens 的唯一可靠来源） |
| codex cache_creation | 待确认 | **协议不暴露**。`TokenUsageBreakdown` 只有 `cachedInputTokens`(=cache_read)，无 cache-creation 维度 |
| codex cost | 待确认 | **协议不暴露**。`TokenUsageBreakdown` 无 cost 字段 |
| ACP `session/info` | 可查 model/context | **不存在**。ACP 无该方法；model 仅来自 `usage_update`(best-effort) 或 `session/set_model` |
| OCS context window API | 待确认 `/model` 端点 | **不存在**。`context_length` 仅在 LLM provider 层，未通过 HTTP 暴露 |

### 10.2 实施决策与偏差

**codexcli (#776)**
- **C1** 改为从 `thread/tokenUsage/updated` 通知提取（`last` input+cached 作为 fill，`modelContextWindow` 作为 window），**不走 `thread/read`**（其 Turn 负载无计数）。`ServerCommander.get_context_usage` 经 `manager.LastContextUsage()` 读 mapper 状态。
- **C2a 保持 `last`（per-turn delta），不改用 `total`**。`SessionAccumulator.mergePerTurnStats` 对 Done stats 做 `TotalInput += usage` 累加，期望 per-turn delta；`total` 已是跨 turn 累计，传入会重复累加导致 `total_input_tok` 虚高。原 spec 建议"优先用 total"在此为**误判**，已纠正。
- **C2b/C4 协议限制**：codex 不暴露 cache_creation 与 cost，无法产出。已在代码注释标注，不静默归零（字段缺省而非伪造 0）。
- **C3**：除 `model/rerouted` 外，`worker.startNewThread` 通过 `manager.SetCurrentModel(cfg.Model)` 注入 thread/start 配置的 model，覆盖正常（非 rerouted）对话。

**acp (#777)**
- **A1**：`buildStats` 写 `model_usage`（CC 格式）。model 来源 `usage_update.model`（best-effort 解析）+ `session/set_model`（worker 记录到 mapper）。未知时不产 model_usage（不猜测）。
- **A2**：fallback 链 `_meta.claudeCode.toolName → kind → title` 已在 `mapToolCall` 实现；ACP content 不含 tool 名（仅位置/参数），故无 content 解析步骤。
- **A3**：协议限制——无 `session/info`，context 强依赖 `usage_update`；agent 不发则 context_fill/window 为 0（已注释）。

**opencodeserver (#778)**
- **O1**：静态映射表 `contextWindowForModel`（Claude 200K / GPT-4o 128K / GPT-4.1·Gemini 1M / o 系列 200K / DeepSeek 64K）。未知模型返回 0（context_pct 保持未设，不伪造）。

### 10.3 改动文件

| 文件 | 改动 |
|------|------|
| `internal/worker/codexcli/mapper.go` | C1/C2a/C3：trackTokenUsage 解析 modelContextWindow；LastContextUsage/SetModel；trackedUsageStats 携带 contextWindow；mu 保护跨 goroutine |
| `internal/worker/codexcli/manager.go` | LastContextUsage/SetCurrentModel 委托 converter |
| `internal/worker/codexcli/commands.go` | get_context_usage 改用 mapper 状态（弃 thread/read） |
| `internal/worker/codexcli/worker.go` | startNewThread 注入 configured model |
| `internal/worker/acp/mapper.go` | A1：usageSnapshot.Model；updateUsage 解析 model；buildStats 写 model_usage；SetModel |
| `internal/worker/acp/worker.go` | handleSetModel 记录 model；handleContextUsage 返回 model + A3 注释 |
| `internal/worker/opencodeserver/commands.go` | O1：contextWindowForModel 静态映射 |
| 新增 `*_test.go` ×3 | codexcli/mapper_test、acp/mapper_turnsummary_test、ocs/commands_contextwindow_test |
