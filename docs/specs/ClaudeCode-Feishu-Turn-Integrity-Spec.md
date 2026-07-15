---
type: spec
tags:
  - project/HotPlex
  - reliability
  - messaging
  - worker
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

# Claude Code × Feishu 跨 Turn 回复完整性修复 Spec

## 0. 摘要

飞书平台使用 `claude_code` worker 时，出现了同一根用户链路上的四类异常：

1. 正常 assistant 回复后额外出现 `✅ 已完成 · 🔧 …` 卡片，与 Turn Summary 重复。
2. 后续用户输入已经被 Claude Code 处理并生成 assistant 文本，但飞书只留下占位卡片。
3. 后续 turn 的 Timer 包含两个 turn 之间的空闲时间。
4. eventstore/turns 中可见空 assistant turn 或合成 fallback，无法反映 Claude 原始会话事实。

本次取证确认它们不是飞书 API 故障，也不是 `claude --print` 在首个 turn 后停止处理输入，而是三类投递状态边界错误叠加，另有一个独立的计时错误：

- Gateway reset 后，旧 `forwardEvents` 可能延迟绑定到新 `SessionConn`，与新 forwarder 分流同一事件流。
- Claude Code parser 丢弃完整 assistant 消息的原生 ID，mapper 将跨 turn 文本压进固定 `assistant_msg` 去重键，较短的新回复被静默抑制。
- Feishu `Close()` 把 placeholder 误当作已经刷新的真实正文，空成功 turn 最终保留模板占位内容。

另有一个独立计时错误：下一 turn 的时钟在上一 turn Done 时启动，因此空闲时间被计入 Timer。

**优先级：P1。** 修复必须覆盖 Gateway、Claude Code worker 和 Feishu terminal rendering，不能只做飞书表层兜底。

---

## 1. 范围

### 1.1 目标

- 确保每个 `SessionConn` 在任一时刻只有一个 forwarder 消费者。
- 确保每个 Claude assistant 内容块在跨 turn 场景中拥有稳定、隔离的身份。
- 确保成功 Done 不会把未替换的 placeholder 当作最终答复。
- 确保 Timer 表示当前 turn 的实际处理区间，不包含 turn 间空闲时间。
- 确保 eventstore、turns、飞书卡片与 Claude 原始 JSONL 在 assistant 内容上保持一致。

### 1.2 非目标

- 不改变 Claude Code CLI 的进程复用策略或 `--print --input-format stream-json` 启动模式。
- 不重构全部 messaging adapter，也不改变 Slack/WebChat 的 UI 设计。
- 不合并或替代 durable-ingress execution ledger 工作。
- 不删除 Turn Summary；本 spec 只消除错误的 tool-only fallback 与空卡。
- 不引入新的 AEP breaking event type。

### 1.3 影响范围

| 层 | 直接影响 | 间接影响 |
|---|---|---|
| Gateway | reset/restart 后 forwarder 所有权、turn timing、fallback 判定 | 所有 worker 的连接替换路径 |
| Claude Code worker | assistant ID、跨 turn 去重状态 | Slack/WebChat 等复用该 worker 的平台 |
| Feishu | placeholder 终态与空成功 turn | CardKit 与 IM Patch 两条更新路径 |
| Eventstore | assistant turn 和合成 fallback 的真实性 | 历史回放、审计与诊断 |

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

### RC-3：placeholder 被误分类为已完成正文

`SendPlaceholder` 同时设置：

- `lastFlushed = placeholder`
- `placeholder = placeholder`
- `buf` 保持为空

Done 时 `Close()` 看到 `content=="" && lastFlushed!=""`，进入“已经刷新真实正文”的分支并跳过最终 flush。实际上 `lastFlushed` 仍是 placeholder。随后 streaming 被关闭，卡片保持模板占位状态。

**结果：** 上游空输出被伪装成成功完成，用户看不到错误或重试建议。

### RC-4：下一 turn 时钟在上一 Done 时启动

Done 尾部执行：

```go
fc.turnStartTime = time.Now()
```

后续 `State(running)` 只重置 `doneReceived`，不重置时钟。因此用户在两个 turn 之间等待的时间被计入下一 turn。

**结果：** Turn Summary Timer 与真实处理耗时不符，并可能污染 fallback 文案及 turns 审计数据。

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

### Fix C（P2 防御层，与 P1 修复同批交付）：显式处理空成功 turn

Feishu controller 必须区分：

1. placeholder
2. 已刷新的真实 partial content
3. 最终 content

Done 时：

- 若 `buf` 有真实 content：正常 final flush。
- 若 `buf` 为空但存在真实 partial content：保留 partial，并正常关闭。
- 若 `buf` 为空且当前只显示 placeholder：使用 `SetTerminalContent` 写入本地化终态，例如“⚠️ 本轮未收到可展示的 Agent 回复，请重试。”，再执行 final flush。
- 不允许用 placeholder 满足 `finalFlushOK`。

Bridge 的 tool-only fallback 仍保留，但只有同一正确绑定的 turn 确认 `assistant text == 0 && tool count > 0` 时才允许生成。

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
  ├─ tool_count > 0 → legitimate tool-only fallback
  └─ tool_count = 0 → explicit no-displayable-reply terminal content

Both paths replace placeholder before Close.
```

---

## 7. 实施分批

### Batch 1：失败测试与可重复证据

- reset replacement 双消费者确定性测试
- Claude 三个连续 turn（长→短→短）mapper 测试
- placeholder-only Close 测试
- idle gap timing 测试

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

### Batch 4：Feishu 终态与可观测性

- placeholder/real content 明确分类
- empty-success terminal content
- structured logs/metric

### Batch 5：全量验收

- targeted race tests
- messaging/worker/gateway 回归
- `make check`
- 本地 dev 飞书连续三 turn 手工验收

---

## 8. 测试矩阵

| ID | 场景 | 断言 |
|---|---|---|
| T-A1 | old forwarder 启动后立即 reset，旧启动被 DB barrier 阻塞 | 旧 forwarder 只读旧 Conn；新 Conn 只有一个消费者 |
| T-A2 | reset 后新 Conn 连续注入 delta/tool/result/done | 所有事件保持顺序且只处理一次 |
| T-A3 | stale old Conn 退出 | 不 detach/cleanup 新 worker |
| T-A4 | Start/Resume/crash recovery | 均使用统一 frozen binding helper |
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
| T-D1 | 两 turn 间 idle 10 分钟，第二 turn 15 秒 | Timer≈15秒，不含 idle |
| T-D2 | Input 投递失败 | turn clock 被清除，不污染下轮 |
| T-D3 | Done 缺 start timestamp | 使用首个 worker 事件回退并有 debug 证据 |
| T-X1 | `go test -count=1 -race ./internal/gateway/...` | 通过；各 package ≤5s 目标不回归 |
| T-X2 | `go test -count=1 -race ./internal/worker/claudecode/...` | 通过 |
| T-X3 | `go test -count=1 -race ./internal/messaging/feishu/...` | 通过 |
| T-X4 | `make check` | 全量质量门禁通过 |

---

## 9. 验收标准

### 功能验收

- `/reset` 后依次发送“简单分析当前目录状态”“自我介绍”“为啥不回复？”，三轮均得到完整 agent 回复。
- 每个 turn 最多一条 agent 正文卡；仅真正 tool-only 的 turn 允许出现一次 `✅ 已完成 · 🔧 …` fallback。
- Turn Summary 可以继续独立展示，但不得与错误 fallback 重复表达完成状态。
- 空成功 turn 不留下模板占位卡，必须显示可理解、可重试的终态。
- 第二 turn Timer 不包含第一、第二次用户输入之间的等待时间。
- Claude JSONL、events 表、turns 表和飞书卡片的 assistant 内容一致。

### 并发与可靠性验收

- 任一 `SessionConn` 同时只有一个 forwarder reader。
- reset、crash recovery、gateway shutdown 在 `-race` 下无数据竞争、无 goroutine 泄漏、无事件分流。
- mapper 去重状态按 turn 回收，不随长会话增长。
- 无新增 delta/backpressure 丢弃。

### 回归验收

- Claude 同 turn 的 delta + assistant snapshot 仍不重复展示。
- Slack/WebChat 的 Claude worker 多 turn 回复不回归。
- OCS、Codex CLI、ACP 的 forwarder 生命周期不回归。
- Feishu CardKit→IM Patch 降级路径仍能终态收敛。
- `make check` 通过。

---

## 10. 风险与回滚

| 风险 | 缓解 |
|---|---|
| 冻结 Conn 后旧 forwarder 不退出 | Conn replacement 必须关闭旧 Conn；测试覆盖 stale exit 与 shutdown |
| assistant block identity 改变导致同 turn 重复 | 保留 native message ID，单独使用 block identity 做内部去重 |
| synthetic snapshot reconciliation 产生重复块 | 仅在协议违约/前缀漂移时触发，并记录 warning；优先不丢内容 |
| empty-success 文案掩盖真实错误 | Error 事件仍优先走 `SetTerminalContent(error)`；该文案仅用于无 Error 的成功 Done |
| turn clock 与并发 Input 竞争 | start timestamp 使用原子/单 owner 状态；Input 失败显式回滚 |

回滚按 Batch 独立进行。Fix C 的终态兜底可单独保留，即使 Fix A/B 需要回滚，它仍能避免用户看到不可解释的空卡。

---

## 11. Issue 跟踪约定

使用现有 #889 作为唯一 P1 umbrella issue，不新建重复 Issue。Issue 必须：

- 删除/纠正“`--print` 首轮后不处理输入”的错误根因。
- 链接本 spec。
- 使用标签：`bug`、`P1`、`reliability`、`race-condition`、`area/gateway`、`area/worker`、`area/messaging`。
- 以 Batch 1–5 checklist 跟踪实施。
- 完成条件以 §9 为准，不以“单元测试变绿”代替飞书真实多 turn 验收。

---

## 12. 根因纠正记录

2026-07-15 的新证据纠正两项旧判断：

1. #889 初版把问题归因于 `claude --print` 单 turn 语义；Claude JSONL 已证明后续输入被处理并产生回复，该判断撤销。
2. `CodexCLI-Delta-Integrity-Fix-Spec.md` 曾认为 Claude 固定 `assistant_msg` 冲突概率极低；真实长→短多 turn 已稳定触发静默丢失，该结论由本 spec 取代。

实现与 review 必须以本 spec 的证据链为准。
