# IM 渠道 SESSION_BUSY 暂存与合并重投设计

**日期：** 2026-07-25
**状态：** 已批准，待实施
**范围：** `internal/messaging`（共享基座 + 飞书/Slack/yuanxin 三渠道接入）、`pkg/events`（typed error）、`internal/gateway/errors.go`（小改）
**关联：** [Durable Ingress 可靠性闭环设计](./2026-07-14-durable-ingress-reliability-closure-design.md)（PR #890，引入 active gate）；issue #868（ExecutionQueue，本轮非目标）

## 决策摘要

PR #890 引入 active gate 后，每个 session 同时只允许一个 `pending/running` execution。
当用户在长任务执行中追问，新消息撞 `SESSION_BUSY` 被直接丢弃，且各 IM 渠道用通用兜底文案
通知用户，造成**黑洞效应**与**误判故障**。该问题**跨渠道**：飞书（`handler.go:242`
“抱歉，处理您的请求时遇到问题”）、Slack（`conn_events.go:86` “An internal error occurred.
Please try again or use /reset.”）、yuanxin（消息处理只 `Log.Error` 后返回）均有。

本设计**不修改 gateway 的 active gate 单活跃约束**（durable ingress 设计刻意保留的底线），
在 messaging 共享基座做“暂存 + 合并重投”，三渠道统一复用：

1. `SESSION_BUSY` 时，把追问（content + 原 envelope）append 到 `PlatformAdapter` 的
   per-session pending 列表，回复渠道各自的静默提示。
2. 当前 turn `done` 后，合并列表所有条目为一条 input，一次性投递给 Worker。
3. 不主动超时，pending 随 session 销毁 / reset / close 清理。

**合并而非逐条**：用户连发多条追问时合并成一个 turn，避免逐条串行慢队列，仍单活跃。
**共享基座**：`PendingBuffer` 与重投逻辑放 `internal/messaging`，三渠道仅各接两个点
（busy 时 append + 提示文案、done 时触发重投），重投逻辑零重复——得益于通用的
`Bridge.MakeEnvelope`。

## 背景与已证实问题

### 行为变更溯源

- v1.34.x 及更早：`deliverToWorker` 无 active gate，新消息直接投 `Worker.Input`。
- v1.35.0（PR #890）：`idx_execution_one_active_per_session` partial unique index 强制
  每 session 单活跃，`ErrSessionBusy` 硬拒绝；队列被有意列为非目标。

### 黑洞效应三重成因

1. **消息丢弃**：`SESSION_BUSY` 时不排队、不暂存，直接拒绝。
2. **文案误导**：各渠道无 `SESSION_BUSY` 专门处理，走通用兜底，用户误判故障。
3. **时机矛盾**：typing indicator / placeholder card 在报错前已发出。

### 运行时佐证（2026-07-25）

session `9b1aae6d` 的“执行”任务 `running` 30+ 分钟，用户 11:56/11:57/12:03 连续追问 3 次
全部 `SESSION_BUSY`（`feishu/chat_queue.go:113` 记录 `err="SESSION_BUSY: ..."`）。

## 错误识别（唯一的 gateway 接触点）

`sendErrorf`（`internal/gateway/errors.go:14`）当前返回 `fmt.Errorf("%s: %s", code, msg)`，
错误码仅在字符串前缀。改为返回 `pkg/events.CodedError`，各渠道 `errors.As` 识别：

```go
// pkg/events
type CodedError struct {
    Code ErrorCode
    Err  error
}
func (e *CodedError) Error() string { return string(e.Code) + ": " + e.Err.Error() }
func (e *CodedError) Unwrap() error { return e.Err }
```

放 `pkg/events` 避免循环依赖。仅类型化，不动 active gate 控制流，不改 error event 发送
（event 已携带 `Code`）。

## 共享设计（internal/messaging）

### PendingBuffer（新增 `pending_buffer.go`）

挂在 `PlatformAdapter`，按 `sessionID` 索引。

```go
type pendingEntry struct {
    content    string             // 追问原文
    envelope   *events.Envelope   // 原消息 envelope 副本（含 sessionID/owner/metadata）
    receivedAt int64              // unix milli
}

type PendingBuffer struct {
    mu    sync.Mutex
    items map[string][]pendingEntry
    log   *slog.Logger
}
```

**存原 envelope 而非 pctx**：envelope 已含重投所需的全部信息（sessionID、OwnerID、
metadata），各渠道 append 时直接传手里已构造好的 envelope，零额外上下文组装。

方法：

| 方法 | 行为 |
|---|---|
| `Append(sessionID, content, env)` | 追加；**相邻完全相同 content 去重**；超 `maxPendingPerSession`（默认 20）则丢最旧保最新 |
| `DrainAndMerge(sessionID) (mergedContent string, repr *events.Envelope, ok bool)` | 原子取出并清空；单条原样、多条加合并头+编号；`repr` 取最后一条 entry 的 envelope 作为重投模板 |
| `Clear(sessionID)` / `ClearAll()` | 清理 |

### PlatformAdapter 接入方法

```go
// busy 时调用：存追问 + 由渠道发自己的提示文案
func (a *PlatformAdapter) AppendPendingInput(sessionID, content string, env *events.Envelope)

// done 时调用：drain + 合并 + 用通用 MakeEnvelope 重投；返回是否发生重投
func (a *PlatformAdapter) ReplayPendingIfNeeded(ctx context.Context, sessionID string, pc PlatformConn) bool
```

`ReplayPendingIfNeeded` 实现（**完全共享，渠道无关**）：

```go
merged, repr, ok := a.pendingBuffer.DrainAndMerge(sessionID)
if !ok { return false }
// 基于 repr 构造新 envelope：新 id + merged content，复用 sessionID/owner/metadata
replayEnv := events.CloneForReplay(repr, merged)   // pkg/events helper
if err := a.bridge.Handle(ctx, replayEnv, pc); err != nil {
    // 重投又 busy（done 后瞬间被占，极端）：递归 append 回去，等下一个 done
    var coded *events.CodedError
    if errors.As(err, &coded) && coded.Code == events.ErrCodeSessionBusy {
        a.pendingBuffer.Append(sessionID, merged, replayEnv)
    }
}
return true
```

- 重投用**新 `client_message_id` / id**（`events.CloneForReplay` 生成），彻底避免
  `UNIQUE(session_id, client_message_id)` 去重误判。
- `seq` 由 `Bridge.Handle`（`bridge.go:130`）重新分配，无需关心。
- `pc`（PlatformConn）由各渠道 handleDone 传入自己的 conn，三渠道 conn 均实现该接口。

### 清理（随 session）

| 时机 | 动作 |
|---|---|
| `ReplayPendingIfNeeded` 后 | `DrainAndMerge` 已清空 |
| 各渠道 `conn.Close` | `Clear(sessionID)` |
| `PlatformAdapter.CloseSharedState` | `ClearAll()` |
| reset | sessionID 变更，旧 buffer 由旧 conn.Close 清理；新 session 天然隔离 |

**不主动超时**：长任务不应丢消息；生命周期由 session 兜底。

## 各渠道接入（每渠道两个点）

### 飞书 `internal/messaging/feishu`

- **消息处理**（`handler.go` `handleTextMessage`，`Bridge().Handle` 返回 err 后）：
  ```go
  var coded *events.CodedError
  if errors.As(err, &coded) && coded.Code == events.ErrCodeSessionBusy {
      a.AppendPendingInput(env.SessionID, text, envelope)
      _ = a.sendTextMessage(context.Background(), channelID,
          "⏳ 已收到，当前任务完成后会自动处理")
      return nil
  }
  // 非 busy：保持原通用报错
  ```
- **done**（`conn.go` `handleDone`）：`go a.ReplayPendingIfNeeded(ctx, env.SessionID, c)`。
- 提示文案：飞书中文（见上）。

### Slack `internal/messaging/slack`

- **消息处理**（`Bridge().Handle` 返回 err 处）：同模式，`AppendPendingInput` + 英文提示
  `"⏳ Got it — will process automatically once the current task finishes."`。
- **done**（`conn_events.go:46` `handleDone`）：`go a.ReplayPendingIfNeeded(...)`。
- 提示文案：Slack 英文。

### yuanxin `internal/messaging/yuanxin`

- **消息处理**（`adapter.go:403` `Bridge().Handle` 返回 err 处）：同模式，
  `AppendPendingInput(env.SessionID, text, envelope)` + 提示。
- **done**（`adapter.go:596` `case events.Done`）：`go a.ReplayPendingIfNeeded(...)`。
- yuanxin 当前 busy 只 `Log.Error` 返回（无用户提示），本改动补上提示，顺带消除其黑洞。

## 合并格式

- **单条**：原样投递，不注入框架文本。
- **多条**（`DrainAndMerge` 输出）：

```
（以下是上一轮执行期间追加的 3 条消息，请一并处理）
1. 继续
2. 补充：重点关注金融领域
3. 换个角度
```

条目按收到时序编号；content 原文忠实保留，仅做 `SanitizeText` 清洗。

## 关键决策与理由

| 决策 | 选择 | 理由 |
|---|---|---|
| 渠道范围 | 飞书 + Slack + yuanxin（共享基座） | 黑洞跨渠道；共享基座避免重复，未来加渠道零成本 |
| 重投逻辑位置 | 共享于 `PlatformAdapter` | `Bridge.MakeEnvelope` 通用，渠道无需各自实现 |
| 暂存内容 | content + 原 envelope | envelope 已含重投全部信息，渠道 append 零额外组装 |
| 暂存容量 | 多条列表 | 用户边想边发，需保留全部追加意图 |
| 投递方式 | 合并为一条 | 避免逐条串行慢队列，一个 turn 处理，仍单活跃 |
| 超时 | 不主动超时 | 随 session 清理；长任务不应丢消息 |
| 用户反馈 | 各渠道静默文本 | 轻量；交互按钮留作未来方案 C |
| 单 session 上限 | 20 条 | 防滥用与内存；超出保留最新 |
| 错误识别 | typed error（`pkg/events.CodedError`） | 健壮，符合 `errors.As` 惯例 |

## 非目标

- 不引入 gateway 层 `ExecutionQueue`（issue #868，durable ingress 设计已列为非目标）。
- 不改 active gate 单活跃约束与 partial unique index。
- 不做交互式卡片（方案 C，未来可选增强）。
- 不自动重投 delivery `unknown` 的输入。
- 不持久化 pending（仅内存；gateway 重启时 in-flight 暂存丢失，可接受——用户会重发）。
- WebChat 不涉及：前端已对 `SESSION_BUSY` 做友好终端态提示（CHANGELOG）。

## 边界与异常

- **重投用新 id**：`events.CloneForReplay` 生成新 `client_message_id`，避免去重误判。
- **多 session 隔离**：buffer 按 `sessionID`，天然隔离。
- **Worker 卡死永不 done**：lease 过期 → fence → fresh worker；buffer 残留由
  `conn.Close` / `CloseSharedState` 兜底清理。
- **重投又 busy**：递归 `Append` 回列表，等下一个 done。
- **busy 时新建的 placeholder card**（飞书）：常见情况前一个任务 streaming 已 active，
  `handler.go:223` 自动跳过本次新建；少数情况（busy 但 streaming 未 active）本次新建
  `streamCtrl` + placeholder，busy 分支需 `SetTerminalContent("已收到待处理")` 后 `Close`，
  避免空占位卡残留。
- **reset 期间**：旧 buffer 随旧 conn.Close 清理；新 session 无 pending。
- **合并后 Worker 处理失败**：属正常 turn 错误，走各渠道现有 error 展示，不影响 pending 机制。

## 测试要点

- 共享：busy → append → done → `DrainAndMerge` → 合并重投完整链路（基座单测，mock Bridge）。
- 合并顺序与编号格式；单条原样（无合并头）。
- 上限 20 条截断（保留最新）；相邻完全相同 content 去重；不同 content 不去重。
- 重投用新 id；`seq` 由 Handle 重分配。
- typed error 识别：非 busy 错误（`INTERNAL_ERROR`）不进 pending。
- 重投又 busy 的递归暂存。
- 并发：`Append` 与 `DrainAndMerge` 并发安全（`-race`）。
- 各渠道集成：飞书/Slack/yuanxin 各自的 busy 分支与 handleDone 触发重投；提示文案正确。
- reset / `conn.Close` / `CloseSharedState` 清理。

## 改动文件清单

| 文件 | 改动 |
|---|---|
| `pkg/events/errors.go`（或 events.go） | 新增 `CodedError`；`CloneForReplay(env, content)` helper |
| `internal/gateway/errors.go` | `sendErrorf` 返回 `*events.CodedError` |
| `internal/messaging/pending_buffer.go` | 新增 `PendingBuffer` + `pendingEntry` + 合并/上限/去重 |
| `internal/messaging/platform_adapter.go` | `pendingBuffer` 字段；`InitSharedState` 初始化 / `CloseSharedState` 清理；`AppendPendingInput` / `ReplayPendingIfNeeded` |
| `internal/messaging/feishu/{handler,conn}.go` | busy 分支 + 提示文案；`handleDone` 重投 |
| `internal/messaging/slack/{adapter,conn_events}.go` | busy 分支 + 提示文案；`handleDone` 重投 |
| `internal/messaging/yuanxin/adapter.go` | `:403` busy 分支 + 提示；`:596` done 重投 |
| 对应 `*_test.go` | 基座单测 + 三渠道集成测试 + `-race` |
