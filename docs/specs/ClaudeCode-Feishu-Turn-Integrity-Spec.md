---
type: spec
tags:
  - project/HotPlex
  - reliability
  - messaging
  - worker
  - gateway
  - webchat
date: 2026-07-15
status: draft
progress: 0
related_issues:
  - "#889"
related_specs:
  - Messaging-Turn-Idle-Context-Loss-Spec.md
  - CodexCLI-Delta-Integrity-Fix-Spec.md
  - Worker-Turn-Summary-Parity-Spec.md
  - Feishu-Streaming-Card-TTL-Rotation-Spec.md
---

# 跨平台 × 多 Worker 跨 Turn 回复完整性修复 Spec

## 0. 摘要

飞书平台使用 `claude_code` worker 时，已复现同一根用户链路上的四类异常：

1. 正常 assistant 回复后额外出现 `✅ 已完成 · 🔧 …` 卡片，与 Turn Summary 重复。
2. 后续用户输入已经被 Claude Code 处理并生成 assistant 文本，但飞书只留下占位卡片。
3. 后续 turn 的 Timer 包含两个 turn 之间的空闲时间。
4. eventstore/turns 中可见空 assistant turn 或合成 fallback，无法反映 Claude 原始会话事实。

本次取证确认它们不是飞书 API 故障，也不是 `claude --print` 在首个 turn 后停止处理输入，而是三类投递状态边界错误叠加，另有一个独立的计时错误：

- Gateway reset 后，旧 `forwardEvents` 可能延迟绑定到新 `SessionConn`，与新 forwarder 分流同一事件流。
- Claude Code parser 丢弃完整 assistant 消息的原生 ID，mapper 将跨 turn 文本压进固定 `assistant_msg` 去重键，较短的新回复被静默抑制。
- Feishu `Close()` 把 placeholder 误当作已经刷新的真实正文，空成功 turn 最终保留模板占位内容。

另有一个独立计时错误：下一 turn 的时钟在上一 turn Done 时启动，因此空闲时间被计入 Timer。

飞书空卡只是平台表现，不是完整故障边界。静态代码审计进一步确认：

- Gateway forwarder 所有权错误对平台无关，在 `ConnReplaced=true` 的 Claude Code、Codex CLI reset 路径上可影响 Feishu、Slack、WebChat。
- Claude Code mapper 去重错误对平台无关，三种通道复用该 worker 时均可能丢失或截断回复。
- tool-only Done fallback 明确只排除 WebChat，因此 Feishu、Slack 均可能出现 fallback 与 Turn Summary 重复。
- Timer 错误位于共享 Gateway，所有平台、所有持久多 turn worker 都受影响。
- placeholder 空卡是 Feishu 特有表现；Slack 更可能表现为状态结束但无正文，WebChat 更可能表现为空 assistant、仅工具/摘要或等待终态。

**优先级：P1。** 修复必须覆盖共享 Gateway、Claude Code worker、Feishu/Slack/WebChat 终态语义和跨 Worker 回归，不能只做飞书表层兜底。

---

## 1. 范围

### 1.1 目标

- 确保每个 `SessionConn` 在任一时刻只有一个 forwarder 消费者。
- 确保每个 Claude assistant 内容块在跨 turn 场景中拥有稳定、隔离的身份。
- 确保 Feishu、Slack、WebChat 对成功 Done 采用可解释且不重复的终态。
- 确保 Timer 表示当前 turn 的实际处理区间，不包含 turn 间空闲时间。
- 确保 eventstore、turns、各平台展示与 Worker 原始输出在 assistant 内容上保持一致。
- 确保 Claude Code、Codex CLI、OpenCode Server、ACP 的差异化 reset/message identity 契约都有回归覆盖。

### 1.2 非目标

- 不改变 Claude Code CLI 的进程复用策略或 `--print --input-format stream-json` 启动模式。
- 不重构全部 messaging adapter，也不重新设计 Slack/WebChat UI；仅统一终态语义和必要的错误呈现。
- 不合并或替代 durable-ingress execution ledger 工作。
- 不删除 Turn Summary；本 spec 只消除错误的 tool-only fallback 与空卡。
- 不引入新的 AEP breaking event type。
- 不因 ACP 当前使用固定 `msg_<sessionID>` 就预设必须修改；先通过 WebChat 合并契约测试确认是否构成独立缺陷。

### 1.3 影响范围

| 层 | 直接影响 | 适用边界 |
|---|---|---|
| Gateway | reset/restart 后 forwarder 所有权、turn timing、fallback 判定 | 所有平台；连接替换竞态当前集中在 Claude Code、Codex CLI |
| Claude Code worker | assistant ID、跨 turn 去重状态 | Feishu、Slack、WebChat 均复用同一 AEP 输出 |
| Feishu | placeholder 终态与空成功 turn | CardKit 与 IM Patch 两条更新路径 |
| Slack | stream/status 终态、standalone fallback 与 Turn Summary | 无 Feishu 式 placeholder，但可无正文或重复完成消息 |
| WebChat | empty assistant、Done settle、turns reconciliation、Turn Summary Timer | 明确跳过 Done fallback，但仍接收共享 delta/Done/stats |
| Codex CLI | `ConnReplaced=true` reset 生命周期 | 不命中 Claude 固定 `assistant_msg` 去重根因 |
| OpenCode Server | 原地 reset 与 message/part identity | 不命中 RC-1/RC-2；受共享 Timer 影响 |
| ACP | 原地 reset、真实 chunk、固定 session-scoped message ID | 不命中 RC-1/RC-2；固定 ID 作为 WebChat 独立审计点 |
| Eventstore | assistant turn 和合成 fallback 的真实性 | 历史回放、审计与各平台终态 reconciliation |

### 1.4 平台 × Worker 风险矩阵

| Worker | Feishu | Slack | WebChat |
|---|---|---|---|
| Claude Code | **已复现**：漏回复、重复 fallback、空占位卡、Timer 错误 | **代码路径高风险**：漏/截断回复、重复 fallback、Timer 错误 | **代码路径高风险**：空/不完整 assistant、错误 Timer；不会生成 Done fallback |
| Codex CLI | `/reset` 后存在 shared forwarder 竞态；Timer 错误 | 同左 | 同左；表现可能为空 assistant 或 pending settle 异常 |
| OpenCode Server | 不命中 RC-1/RC-2；共享 Timer 错误 | 同左 | 同左 |
| ACP | 不命中 RC-1/RC-2；共享 Timer 错误 | 同左 | 同左；另需验证固定 message ID 是否造成跨 turn 合并 |

证据等级必须保留：只有 Claude Code × Feishu 已由真实日志、JSONL、SQLite 和截图闭环复现；其他格子来自共享代码路径审计，关闭 issue 前必须通过对应通道测试，不得表述为已复现。

---

## 2. 复现与证据

### 2.1 环境

- HotPlex `v1.34.0`
- 分支：`feat/durable-ingress-reliability`
- 平台：飞书私聊
- Worker：`claude_code`
- 模式：本地 dev，Gateway `127.0.0.1:8888`
- Session：`b1b00a8c-a225-5ebb-9da5-6615c9cfe4c7`
- 日志：`logs/hotplex.log`
- Claude 会话：`~/.claude/projects/-Users-huangzhonghui-hotplex/<session>.jsonl`

### 2.2 时间线

| 时间 | 事件 | 证据含义 |
|---|---|---|
| `08:36:54.198` | 第一个 `forwardEvents` 启动，`resumed=true` | reset 前旧 forwarder 已创建 |
| `08:36:54.200` | `/reset` 开始终止旧进程 | 连接替换窗口开启 |
| `08:36:54.215` | 第二个 `forwardEvents` 启动，`resumed=false` | 新 forwarder 与旧 forwarder 并存 |
| `08:39:41.006` | 飞书收到正文 delta，`text_len=1688` | Worker→Gateway→Feishu 正文链路成功 |
| `08:39:45.902` | Done 处理后又发送独立小卡 | Done 消费者误判本轮无正文 |
| `08:39:46.621` | 正常 Turn Summary 发送 | 形成“完成卡 + Summary”重复 |
| `08:51:30.519` | “自我介绍”成功写入 worker | 输入没有丢失 |
| `08:51:40.772` | Claude JSONL 记录完整 assistant 回复，`output_tokens=250` | Claude 确实回复 |
| `08:51:46.099` | Gateway Done：`text_len=0` | 文本在 worker mapper 内被抑制 |
| `08:52:21.632` | Claude JSONL 记录“在的，回复了呀…”，`output_tokens=60` | 第三轮同样有回复 |
| `08:52:26.912` | Gateway Done：`text_len=0` | 再次被抑制 |

### 2.3 SQLite 交叉验证

同一 session 的 `events`/`turns` 显示：

- Turn 1 存在完整 `message` 事件，同时又持久化 `✅ 已完成 · 🔧 2 (Bash×2) · ⏱ 23s` fallback。
- Turn 2、Turn 3 只有 input 与 Done，没有 assistant message。
- Turn 2、Turn 3 的 assistant turn 内容长度为 0。
- Turn 2 的 `duration_ms=715203`，接近从上一 Done 开始计算的 11m55s，而不是本轮约 15s 的处理时长。

这组状态在单一、正确绑定的 forwarder 中不可能同时成立：能捕获并持久化 Turn 1 delta 的 `forwardContext` 应当具有非空 `turnText`，不应触发 tool-only fallback。唯一符合日志与代码的解释是 Done 与 delta 被两个 forwarder 分流。

### 2.4 排除项

- 相关时间窗内 `card_kit_ok=true`，没有 CardKit flush failure、IM Patch failure 或 backpressure drop。
- 输入均记录 `gateway: input delivered to worker`。
- Claude JSONL 有后续 user、assistant、Stop hook 和 queue dequeue 记录。
- 因此 #889 原有的“`claude --print` 首个 result 后不再处理输入”判断不成立，必须撤销。

---

## 3. 根因链

### RC-1：forwarder 延迟读取可变 `w.Conn()`

`forwardEvents` goroutine 启动后先恢复 accumulator/generation/turn number，最后才执行：

```go
recvCh := w.Conn().Recv()
```

`ResetContext` 会在同一个 Worker 对象上终止旧 Conn 并安装新 Conn。若旧 forwarder 在替换后才执行 `w.Conn()`，它会绑定新 Conn；`ResetSession` 随后又为新 Conn 启动第二个 forwarder。两个 goroutine 从同一 channel 竞争读取，事件被拆分而非复制。

现有 reset-generation guard 只在 recv channel 关闭、进入 `handleWorkerExit` 时检查。它可以阻止旧 goroutine 清理新 worker，却不能阻止旧 goroutine 提前绑定并消费新 channel。

**结果：**

- delta 与 Done 进入不同 `forwardContext`。
- Done 侧 `turnText.Len()==0`，触发 tool-only fallback。
- stats、turn capture、retry 状态和计时也可能由错误消费者处理。

**适用边界：** 该根因位于共享 Gateway，因此平台无关；但当前只有 Claude Code、Codex CLI 的 reset 返回 `ConnReplaced=true` 并启动新 forwarder。OpenCode Server、ACP 原地 reset 并复用原 Conn，不触发同一双消费者窗口。

### RC-2：完整 assistant 消息身份退化为跨 turn 常量

Claude 原始 assistant message 带唯一 `message.id`，但 `AssistantMessage` 类型只解析 `role/content`；`parseAssistant` 构造的 `StreamPayload` 没有 `MessageID`。Mapper 统一 fallback：

```go
if msgID == "" {
    msgID = "assistant_msg"
}
```

`sentTexts["assistant_msg_text"]` 在 Worker 生命周期内不按 turn 清理。`getDeltaText` 又只比较字符长度：

```go
if currLen <= lastLen {
    return ""
}
```

第一轮长回复建立较长基线后，第二、第三轮较短回复直接返回空字符串；如果新回复更长，则只会发送“超过旧长度的尾部”，造成内容截断。

**结果：** Claude 已生成回复，但 AEP `message.delta` 从未产生。

**适用边界：** 该根因属于 Claude Code mapper，但输出丢失发生在进入平台 adapter 之前，因此 Feishu、Slack、WebChat 均受影响。Codex CLI 使用原生 item ID 并已有 snapshot drift 防御；OpenCode Server 使用 message/part ID；ACP 发送真实增量 chunk，均不命中同一去重链。

### RC-3：placeholder 被误分类为已完成正文

`SendPlaceholder` 同时设置：

- `lastFlushed = placeholder`
- `placeholder = placeholder`
- `buf` 保持为空

Done 时 `Close()` 看到 `content=="" && lastFlushed!=""`，进入“已经刷新真实正文”的分支并跳过最终 flush。实际上 `lastFlushed` 仍是 placeholder。随后 streaming 被关闭，卡片保持模板占位状态。

**结果：** 上游空输出被伪装成成功完成，用户看不到错误或重试建议。

**适用边界：** placeholder 误分类只属于 Feishu。Slack 没有相同模板卡，但 Done 会清除 status 并关闭 stream，可能表现为“处理结束但没有正文”；WebChat 没有平台 placeholder，可能形成空 assistant 或只有工具/Turn Summary。Gateway 的 tool-only fallback 只排除 WebChat，所以错误的 `turnText==0` 在 Feishu、Slack 都可能额外生成与 Turn Summary 重复的完成消息。

### RC-4：下一 turn 时钟在上一 Done 时启动

Done 尾部执行：

```go
fc.turnStartTime = time.Now()
```

后续 `State(running)` 只重置 `doneReceived`，不重置时钟。因此用户在两个 turn 之间等待的时间被计入下一 turn。

**结果：** Turn Summary Timer 与真实处理耗时不符，并可能污染 fallback 文案及 turns 审计数据。

**适用边界：** 计时由共享 `forwardContext` 维护，所有平台和所有复用 forwarder 的 Worker 都受影响；WebChat 也会在 `TurnSummaryCard` 中展示 `turn_duration_ms`。

---

## 4. 设计原则与不变式

| ID | 不变式 |
|---|---|
| I-1 | 一个具体 `SessionConn` 在其生命周期内最多只有一个 forwarder 消费者。 |
| I-2 | forwarder 的 Conn、reset generation、worker run identity 必须在 goroutine 启动前同步冻结。 |
| I-3 | Claude 原生 assistant message ID 必须传播到去重层；缺 ID 时使用 turn-scoped synthetic ID。 |
| I-4 | 去重状态不得跨 Done 泄漏；不能仅因新文本更短就静默返回空。 |
| I-5 | 成功 Done 必须产生真实正文、合法 tool-only fallback，或明确的“无可展示回复”终态，三者必居其一。 |
| I-6 | placeholder 永远不是 completed content。 |
| I-7 | Turn Timer 从该轮 input 被接受/投递时开始，在 Done 时结束；turn 间 idle 不计入。 |
| I-8 | eventstore/turns 的 assistant 内容必须与实际投递内容一致。 |
| I-9 | 平台 adapter 可以采用不同 UI，但必须共享“真实正文 / 合法 tool-only / 明确 empty-success”三态语义。 |
| I-10 | 未经真实复现的跨平台/跨 Worker 风险必须通过契约测试验证，不能用 Feishu 单通道绿灯代替。 |

---

## 5. 方案设计

### Fix A（P1）：冻结 forwarder 绑定

新增单一 forwarder 启动入口，例如 `launchForwarder`，在调用 goroutine 中同步捕获：

- `worker.Worker`
- 当前 `worker.SessionConn`
- `sessionID`
- reset generation
- `forwardOpts` / `workerRunID`

`forwardEvents` 接收冻结后的 binding，并且只能从 `binding.Conn.Recv()` 读取。函数内部禁止再次调用 `w.Conn()` 选择事件源。

所有启动路径必须统一走该入口：

1. fresh Start
2. Resume
3. `/reset` 的 `ConnReplaced=true`
4. crash recovery/replacement

旧 forwarder 在旧 Conn 关闭后退出；generation guard 继续负责阻止 stale cleanup，但不再承担连接所有权保证。

Worker 契约测试必须同时证明：Claude Code、Codex CLI 的 Conn replacement 各启动一个新绑定；OpenCode Server、ACP 的原地 reset 不启动第二个 forwarder。

#### 为什么不采用其他方案

- **仅在每个事件前检查 generation：不成立。** stale forwarder 已从 channel 取走事件，再丢弃仍会造成新 forwarder 永远收不到该事件。
- **reset 后 sleep/wait：不成立。** 依赖时序且扩大控制路径延迟，不能证明旧 goroutine 已绑定旧 Conn。
- **扩大 channel buffer：不相关。** 问题是多消费者所有权，不是容量。

### Fix B（P1）：恢复 Claude assistant 身份并限定去重生命周期

#### B1. 解析原生身份

`AssistantMessage` 增加 `ID`，`parseAssistant` 将原生 ID 写入 `StreamPayload.MessageID`。多个内容块必须拥有稳定的 block identity，去重键使用：

```text
native_message_id + content_block_index + content_type
```

AEP `MessageID` 保留 native message ID；block identity 仅用于 mapper 内部去重，不改变外部协议。

#### B2. 缺 ID 兼容

若旧版或第三方 wrapper 不提供 message ID，生成：

```text
assistant:<turn_epoch>:<block_index>
```

`turn_epoch` 在每个 Result 后递增，禁止回退到 worker 生命周期常量 `assistant_msg`。

#### B3. 清理与防御

- Result 映射完成后清理该 turn 的 `sentTexts`，避免跨 turn 污染和无界增长。
- 全量 snapshot 只有在已发送文本是其严格前缀时才提取尾部。
- snapshot 相等时允许返回空，表示同一内容的合法重复。
- snapshot 更短或前缀不一致时不得静默吞掉；应使用新的 synthetic block identity 发出完整 snapshot，并记录 integrity warning。

本规则取代 `CodexCLI-Delta-Integrity-Fix-Spec.md` §1.1 中“Claude assistant 固定 ID 冲突概率极低、暂不修复”的旧判断；本次真实多 turn 证据已证明该判断不成立。

### Fix C（P2 防御层，与 P1 修复同批交付）：统一跨平台空成功 turn 语义

Gateway 将成功 Done 的用户可见结果分类为：

1. 已交付真实 assistant content；
2. 无 assistant content，但存在工具结果，属于合法 tool-only turn；
3. 无 assistant content、无工具结果，属于 empty-success integrity failure。

分类使用本 turn 的真实交付账本，不能把 placeholder、status 文案、上一 turn 内容或错误 forwarder 的局部状态当成 assistant content。

#### Feishu

Controller 必须区分：

1. placeholder
2. 已刷新的真实 partial content
3. 最终 content

Done 时：

- 若 `buf` 有真实 content：正常 final flush。
- 若 `buf` 为空但存在真实 partial content：保留 partial，并正常关闭。
- 若 `buf` 为空且当前只显示 placeholder：使用 `SetTerminalContent` 写入本地化终态，例如“⚠️ 本轮未收到可展示的 Agent 回复，请重试。”，再执行 final flush。
- 不允许用 placeholder 满足 `finalFlushOK`。

#### Slack

- 已有真实正文时只关闭当前 stream，再单独发送 Turn Summary；不得发送完成 fallback。
- 合法 tool-only turn 可以发送一次 standalone fallback，再发送 Turn Summary。
- empty-success 必须发送一次明确可重试消息，不能仅清除 status 后静默结束。

#### WebChat

- 保持 tool-only turn 不生成 Done fallback，因为 UI 已独立渲染工具列表。
- empty-success 必须形成明确的 assistant integrity/error part，不能只完成 pending promise 并留下空 assistant。
- Done reconciliation 必须以 turns/eventstore 的同 turn 内容为准；若上游持久化为空，不得声称 reconciliation 已恢复正文。

Bridge 的 tool-only fallback 仍保留，但只有同一正确绑定的 turn 确认 `assistant text == 0 && tool count > 0` 时才允许生成。empty-success 可以复用现有 `Message`/错误呈现能力，不新增 breaking AEP event type。所有新增用户文案必须同步进入对应平台的中英文资源。

### Fix D（P2）：显式 turn lifecycle clock

在 session accumulator 中增加并发安全的 per-turn start timestamp：

- Input 完成状态转换、即将投递 worker 时记录 start。
- Input 投递失败时清除 start，避免后续 Done 误用。
- Done 原子读取并清除 start，计算当前 turn duration。
- 若 crash recovery/replay 场景缺少 start，回退到该 turn 首个非终态 worker 事件时间，并记录 debug 日志。
- 删除“上一 Done 时启动下一 turn 时钟”的行为。

该时间语义覆盖模型等待、工具执行和最终输出，但排除用户两次输入之间的 idle。

### Fix E（P2）：诊断可观测性

新增或补强以下结构化证据：

- forwarder start/exit：`session_id`、`forwarder_id`、`reset_generation`、`worker_run_id`
- stale forwarder exit 原因
- successful Done with zero displayable content 警告
- mapper snapshot identity divergence 警告
- `worker_empty_success_total{worker_type,platform}` 指标
- `stale_forwarder_event_total{worker_type}`、`assistant_snapshot_drift_total{worker_type}`、`platform_terminal_fallback_total{platform,reason}` 指标

不得记录完整用户正文、Claude 原始回复或飞书凭证。

---

## 6. 事件流

### 6.1 正常多 turn

```text
Input accepted
  → mark turn start
  → worker.Input
  → Claude assistant(id=msg-A)
  → parser preserves msg-A
  → mapper emits delta under msg-A/block-0
  → bound forwarder appends turnText
  → Feishu replaces placeholder
  → Result/Done clears mapper turn state
  → bound forwarder records assistant turn
  → Feishu finalizes real content + Turn Summary
```

### 6.2 Reset 连接替换

```text
old Conn + old forwarder(binding=old Conn)
  → ResetContext closes old Conn
  → old forwarder exits and stale-cleanup guard applies

new Conn captured synchronously
  → one new forwarder(binding=new Conn)
  → all new worker events remain ordered in one forwardContext
```

### 6.3 Worker 成功但无正文

```text
Done
  ├─ assistant content > 0
  │    └─ finalize real content; no Done fallback
  ├─ assistant content = 0 && tool_count > 0
  │    ├─ Feishu/Slack → one legitimate tool-only fallback
  │    └─ WebChat → no fallback; retain rendered tool list
  └─ assistant content = 0 && tool_count = 0
       └─ all platforms → explicit retryable integrity terminal

Feishu must replace placeholder before Close.
```

### 6.4 Worker reset 差异

```text
Claude Code / Codex CLI
  → reset replaces Conn
  → synchronously capture new binding
  → start exactly one new forwarder

OpenCode Server / ACP
  → reset in place on current Conn
  → inject internal reset boundary
  → keep the existing single forwarder
```

---

## 7. 实施分批

### Batch 1：失败测试与可重复证据

- reset replacement 双消费者确定性测试
- Claude 三个连续 turn（长→短→短）mapper 测试
- Feishu placeholder-only Close、Slack empty-success、WebChat empty assistant 测试
- idle gap timing 测试
- Claude Code/Codex CLI replacement reset 与 OpenCode Server/ACP in-place reset 契约测试

先确认测试在当前代码上失败，再进入修复。

### Batch 2：Gateway forwarder 所有权与计时

- 引入冻结 binding/统一 launch helper
- 改造 Start/Resume/Reset/recovery 启动点
- 实现显式 turn clock
- 验证 `-race`

### Batch 3：Claude parser/mapper

- 传播 message/block identity
- turn-scoped fallback identity
- Result 清理去重状态
- snapshot divergence 防静默丢失

### Batch 4：跨平台终态与可观测性

- Feishu placeholder/real content 明确分类
- Slack standalone fallback 与 empty-success terminal
- WebChat tool-only/empty-success/Done reconciliation 终态
- 中英文资源同步
- structured logs/metric

### Batch 5：全量验收

- targeted race tests
- messaging/worker/gateway 回归
- Slack/WebChat adapter 与前端契约测试
- Claude Code、Codex CLI、OpenCode Server、ACP 跨平台矩阵
- `make check`
- 本地 dev 飞书、Slack、WebChat 连续三 turn 与 `/reset` 验收

---

## 8. 测试矩阵

| ID | 场景 | 断言 |
|---|---|---|
| T-A1 | old forwarder 启动后立即 reset，旧启动被 DB barrier 阻塞 | 旧 forwarder 只读旧 Conn；新 Conn 只有一个消费者 |
| T-A2 | reset 后新 Conn 连续注入 delta/tool/result/done | 所有事件保持顺序且只处理一次 |
| T-A3 | stale old Conn 退出 | 不 detach/cleanup 新 worker |
| T-A4 | Start/Resume/crash recovery | 均使用统一 frozen binding helper |
| T-A5 | Claude Code、Codex CLI `/reset` | Conn replacement 各自只启动一个新 forwarder |
| T-A6 | OpenCode Server、ACP `/reset` | 原地 reset，不启动第二个 forwarder |
| T-B1 | turn1 长回复，turn2/3 较短回复 | 三轮文本完整、互不截断 |
| T-B2 | assistant 原生 ID 不同 | mapper 使用不同去重命名空间 |
| T-B3 | assistant 缺 ID | synthetic ID 含不同 turn epoch |
| T-B4 | 同 turn delta + 完整 snapshot | 重复部分不二次发送 |
| T-B5 | snapshot 更短或前缀漂移 | 不静默返回空；记录 warning 并发送完整 snapshot |
| T-B6 | 1000 个 turn | `sentTexts` 不随 turn 无界增长 |
| T-C1 | placeholder 后收到真实正文 | 同一卡片显示正文，placeholder 消失 |
| T-C2 | placeholder + 成功 Done + 无正文/工具 | 同一卡片显示明确终态，不保留 placeholder |
| T-C3 | 只有工具、无正文 | 只显示一次 tool-only fallback；Turn Summary 保持独立 |
| T-C4 | 已有真实 partial，最终 buf 为空 | 保留真实 partial，不替换为空终态 |
| T-C5 | Slack 已有真实正文 + Done + tools | 关闭 stream 并发送 Summary；不生成完成 fallback |
| T-C6 | Slack tool-only turn | 一次 standalone fallback + 一次 Summary，无重复正文 |
| T-C7 | Slack empty-success | status 被清除后发送明确可重试终态，不静默结束 |
| T-C8 | WebChat tool-only turn | 渲染工具与 Summary，不生成 Done fallback |
| T-C9 | WebChat empty-success | pending 正常 settle，并显示明确 integrity/error part，不留下空 assistant |
| T-C10 | WebChat Done reconciliation 对应空 turns | 不伪造已恢复正文；保留可诊断终态 |
| T-D1 | 两 turn 间 idle 10 分钟，第二 turn 15 秒 | Timer≈15秒，不含 idle |
| T-D2 | Input 投递失败 | turn clock 被清除，不污染下轮 |
| T-D3 | Done 缺 start timestamp | 使用首个 worker 事件回退并有 debug 证据 |
| T-W1 | Claude Code × Feishu/Slack/WebChat 长→短→追问 | 三通道均完整交付，平台终态符合各自语义 |
| T-W2 | Codex CLI × 三平台 `/reset` 后三 turn | 无事件分流、空回复、重复 Done 或错误 Timer |
| T-W3 | OpenCode Server、ACP × 三平台连续三 turn | 多 turn 基线不回归，Timer 正确 |
| T-W4 | ACP × WebChat 复用固定 `msg_<sessionID>` | 不跨 turn 合并；若失败则单独登记 message identity 缺陷 |
| T-X1 | `go test -count=1 -race ./internal/gateway/...` | 通过；各 package ≤5s 目标不回归 |
| T-X2 | `go test -count=1 -race ./internal/worker/claudecode/...` | 通过 |
| T-X3 | `go test -count=1 -race` messaging 与四类 worker 相关 package | 通过 |
| T-X4 | `npx tsc --noEmit`、`npx vitest run`（webchat） | 通过 |
| T-X5 | `make check` | 全量质量门禁通过 |

---

## 9. 验收标准

### 已复现组合验收：Claude Code × Feishu

- `/reset` 后依次发送“简单分析当前目录状态”“自我介绍”“为啥不回复？”，三轮均得到完整 agent 回复。
- 每个 turn 最多一条 agent 正文卡；仅真正 tool-only 的 turn 允许出现一次 `✅ 已完成 · 🔧 …` fallback。
- Turn Summary 可以继续独立展示，但不得与错误 fallback 重复表达完成状态。
- 空成功 turn 不留下模板占位卡，必须显示可理解、可重试的终态。
- 第二 turn Timer 不包含第一、第二次用户输入之间的等待时间。
- Claude JSONL、events 表、turns 表和飞书卡片的 assistant 内容一致。

### 跨平台验收

- Claude Code × Slack 连续执行长回复、短回复、追问与 `/reset`：每轮完整正文；仅真正 tool-only 才有一次 fallback；Summary 不重复正文。
- Claude Code × WebChat 执行同一序列：无空 assistant、错误合并或 pending 卡死；tool-only 不生成 Done fallback。
- empty-success 在 Feishu、Slack、WebChat 均显示明确且可重试的终态，不允许静默成功。
- 三个平台显示的 Timer 都从当前 input 开始，不包含用户轮间 idle。

### 跨 Worker 验收

- Claude Code、Codex CLI 的 `/reset` replacement 路径各自保持新 Conn 单消费者。
- OpenCode Server、ACP 的 in-place reset 不重复启动 forwarder。
- Codex CLI × 三平台 `/reset` 后三 turn 无丢失、截断、重复或挂起。
- OpenCode Server、ACP × 三平台连续三 turn 不回归，且 Timer 语义一致。
- ACP × WebChat 固定 message ID 不造成跨 turn 合并；若测试失败，作为独立 message identity 缺陷拆分，但不得阻塞本 spec 的共享 Gateway 修复。

### 并发与可靠性验收

- 任一 `SessionConn` 同时只有一个 forwarder reader。
- reset、crash recovery、gateway shutdown 在 `-race` 下无数据竞争、无 goroutine 泄漏、无事件分流。
- mapper 去重状态按 turn 回收，不随长会话增长。
- 无新增 delta/backpressure 丢弃。

### 回归验收

- Claude 同 turn 的 delta + assistant snapshot 仍不重复展示。
- Slack/WebChat 的 Claude worker 多 turn 回复通过真实 adapter/前端契约测试。
- OpenCode Server、Codex CLI、ACP 的 forwarder 生命周期与现有 message identity 不回归。
- Feishu CardKit→IM Patch 降级路径仍能终态收敛。
- `make check`、webchat `tsc`、`vitest` 通过。

---

## 10. 风险与回滚

| 风险 | 缓解 |
|---|---|
| 冻结 Conn 后旧 forwarder 不退出 | Conn replacement 必须关闭旧 Conn；测试覆盖 stale exit 与 shutdown |
| assistant block identity 改变导致同 turn 重复 | 保留 native message ID，单独使用 block identity 做内部去重 |
| synthetic snapshot reconciliation 产生重复块 | 仅在协议违约/前缀漂移时触发，并记录 warning；优先不丢内容 |
| empty-success 文案掩盖真实错误 | Error 事件仍优先走 `SetTerminalContent(error)`；该文案仅用于无 Error 的成功 Done |
| turn clock 与并发 Input 竞争 | start timestamp 使用原子/单 owner 状态；Input 失败显式回滚 |
| 将静态风险误报为已复现 | issue/spec 持续标注证据等级；只有真实通道复现才能升级为 confirmed |
| ACP 固定 message ID 引出独立前端问题 | 先做契约测试；失败后单独跟踪，不把未经证实的改动混入 Claude mapper 修复 |

回滚按 Batch 独立进行。Fix C 的终态兜底可单独保留，即使 Fix A/B 需要回滚，它仍能避免用户看到不可解释的空卡。

---

## 11. Issue 跟踪约定

使用现有 #889 作为唯一 P1 umbrella issue，不新建重复 Issue。Issue 必须：

- 删除/纠正“`--print` 首轮后不处理输入”的错误根因。
- 链接本 spec。
- 使用标签：`bug`、`P1`、`reliability`、`race-condition`、`area/gateway`、`area/worker`、`area/messaging`、`area/webchat`。
- 以 Batch 1–5 checklist 跟踪实施。
- 完成条件以 §9 为准，不以“单元测试变绿”代替 Feishu/Slack/WebChat 和跨 Worker 矩阵验收。

---

## 12. 根因纠正记录

2026-07-15 的新证据纠正两项旧判断：

1. #889 初版把问题归因于 `claude --print` 单 turn 语义；Claude JSONL 已证明后续输入被处理并产生回复，该判断撤销。
2. `CodexCLI-Delta-Integrity-Fix-Spec.md` 曾认为 Claude 固定 `assistant_msg` 冲突概率极低；真实长→短多 turn 已稳定触发静默丢失，该结论由本 spec 取代。

实现与 review 必须以本 spec 的证据链为准。

2026-07-15 的影响面审计进一步补充：

3. RC-1 是共享 Gateway 缺陷，但当前 Conn replacement 风险集中在 Claude Code、Codex CLI；OpenCode Server、ACP 原地 reset 不命中同一竞态。
4. RC-2 是 Claude Code mapper 缺陷，但在平台 adapter 之前发生，因此 Feishu、Slack、WebChat 均暴露。
5. RC-3 的 placeholder 表现为 Feishu 特有；Slack/WebChat 需要各自的 empty-success 终态，不应照搬卡片修复。
6. RC-4 是全平台、全 Worker 的共享 Timer 错误。
