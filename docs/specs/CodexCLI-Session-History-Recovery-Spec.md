# Feature: CodexCLI 会话历史恢复 — 从 Turn 表重建上下文

**Issue**: CodexCLI worker 无法会话保持
**Severity**: P2 (用户体验核心问题：上下文丢失导致指令遗忘)
**Scope**: `internal/worker/worker.go`, `internal/gateway/bridge.go`, `internal/worker/codexcli/worker.go`
**Prerequisite**: 熟悉 CodexCLI `AppServerWorker` singleton 模式、bridge `prepareWorkerInfo` 流程、`TurnQuerier` 接口

---

## 1. Problem Statement

CodexCLI worker 使用 `codex app-server` 单例进程管理 thread。所有 thread 历史存储在进程内存中，无磁盘持久化。

当 session 空闲超过 30 分钟，Zombie GC 终止 session → app-server 进程被 KillIfIdle 杀死 → 所有对话历史随进程销毁。用户下次发消息时，创建全新 thread，无法记住之前的指令。

**复现场景**（2026-06-09 22:05–22:41 实际日志）:

| 时间 | 事件 |
|------|------|
| 22:05:04 | 用户发送 "现在开始我发 ping，你回复 汪"，session `092bc1ab` 处理 |
| 22:05:23 | Session 最后 IO |
| 22:36:21 | Zombie GC 终止 session（30 分钟无 IO），KillIfIdle 立即杀死 app-server 进程 |
| 22:41:48 | 用户在同一 Slack thread 发 "ping"，Resume 失败 → 创建全新 session/thread |
| 22:41:49 | Agent 回复 "pong" 而非 "汪" — 历史完全丢失 |

**根因链**:

1. **Zombie GC**（30 min execution timeout）终止空闲 session
2. **KillIfIdle** 立即杀死 app-server 进程（绕过 30 分钟 drain timer）
3. **Thread 历史全丢** — 仅存于进程内存，无持久化
4. **Resume 创建新 thread** — `startNewThread()` 调用 `thread/start` 创建全新 thread
5. **thread/start 协议不支持恢复** — `ThreadStartParams` 无 threadId 字段

**对比 Claude Code worker**（无此问题）: 使用 `--session-id` + `--resume` 参数，session 文件持久化在磁盘。进程重启后从磁盘加载历史。

---

## 2. Solution: 从 Turn 表重建上下文

### 2.1 核心思路

HotPlex 的 `turns` 表已经持久化了完整的 user/assistant 对话文本。当 CodexCLI 创建新 thread 时，查询 turn 历史，格式化为上下文前缀，注入首条用户消息。

**数据流**:

```
用户发消息
  ↓
StartPlatformSession → session TERMINATED
  ↓
prepareWorkerInfo()
  ├→ buildWorkerInfo()       (现有逻辑)
  ├→ turnsQuerier.QueryTurns(sessionID, 50, 0)  ← 新增
  │    ↓
  │  []TurnRecord → []ConversationTurn
  │    ↓
  │  info.ConversationHistory = history
  ├→ injectSlackEnv()        (现有逻辑)
  └→ injectGatewayContext()  (现有逻辑)
  ↓
worker.Start(info) → startNewThread()
  └→ 存储 info.ConversationHistory 到 w.pendingHistory
  ↓
worker.Input(content)  ← 用户首条消息
  └→ injectHistoryPrefix(content)
       ↓
     "<conversation_history>
      [User]: 现在开始我发 ping，你回复 汪
      [Assistant]: 收到，以后你发 ping 我就回 汪
      [User]: ping
      [Assistant]: 汪
      </conversation_history>

      ping"                              ← 实际用户消息
  ↓
turn/start → agent 看到完整上下文，正确回复 "汪"
```

### 2.2 为什么选择首条 Input 注入

| 方案 | 优势 | 劣势 |
|------|------|------|
| **A. SystemPrompt 注入** | 语义清晰 | CodexCLI worker 不将 SystemPrompt 传递给 app-server |
| B. thread/start params | 协议层支持 | upstream 不支持 input 参数 |
| C. 单独 turn/start | 隔离性好 | 触发 agent 不必要的响应，浪费 token |
| **D. 首条 Input 前缀** ✓ | 最简洁，无协议改动，同 turn 内可见 | 历史作为 user message 的一部分 |

选择方案 D：在首条 `Input()` 调用时，将历史格式化为结构化文本块，拼接到用户消息前面。Agent 在同一 turn 内同时看到历史上下文和当前消息。

---

## 3. Detailed Design

### 3.1 新增类型 — `internal/worker/worker.go`

在 `SessionInfo` 结构体后添加 `ConversationTurn` 类型：

```go
// ConversationTurn represents a single turn in a conversation history,
// used to seed context when a worker cannot natively resume its session.
type ConversationTurn struct {
    Role    string // "user" | "assistant"
    Content string
}
```

在 `SessionInfo` 末尾添加字段：

```go
// ConversationHistory carries prior turns for pre-seeding a new worker thread.
// Populated by the bridge when resuming/recreating a session with existing history.
// Workers that support native resume (Claude Code --resume) ignore this;
// workers that always create fresh threads (CodexCLI) use it to inject context.
ConversationHistory []ConversationTurn
```

### 3.2 填充历史 — `internal/gateway/bridge.go`

在 `prepareWorkerInfo()` 中，`buildWorkerInfo()` 之后、`injectSlackEnv()` 之前，查询 turns 表：

```go
func (b *Bridge) prepareWorkerInfo(sessionID, userID, workDir string, si *session.SessionInfo) worker.SessionInfo {
    info := b.buildWorkerInfo(sessionID, userID, workDir, si)

    // Populate conversation history from turns table for context recovery.
    if b.turnsQuerier != nil {
        if turns, err := b.turnsQuerier.QueryTurns(context.Background(), sessionID, 50, 0); err == nil && len(turns) > 0 {
            history := make([]worker.ConversationTurn, 0, len(turns))
            for _, t := range turns {
                if t.Content == "" {
                    continue
                }
                history = append(history, worker.ConversationTurn{
                    Role:    t.Role,
                    Content: t.Content,
                })
            }
            info.ConversationHistory = history
        }
    }

    injectSlackEnv(&info, si.PlatformKey)
    info.Env = injectGatewayContext(info.Env, si.Platform, si.BotID, si.BotName, si.UserID, si.PlatformKey, sessionID, workDir)
    return info
}
```

**设计决策**:
- **最近 50 条 turn**（约 25 轮对话），足够覆盖短期间断场景
- 空内容 turn 跳过
- 查询失败静默忽略（不阻塞 session 创建）
- `b.turnsQuerier` 已是 Bridge 字段（bridge.go:45），无需新依赖注入

### 3.3 CodexCLI 消费历史 — `internal/worker/codexcli/worker.go`

#### 3.3.1 新增字段

在 `AppServerWorker` 结构体中添加：

```go
pendingHistory  []worker.ConversationTurn // stored from session info
historyInjected bool                      // protected by w.mu
```

#### 3.3.2 startNewThread 存储历史

在 `startNewThread()` 中，thread 创建成功后存储历史：

```go
func (w *AppServerWorker) startNewThread(session worker.SessionInfo, errPrefix string) error {
    // ... existing code ...

    // Store conversation history for first-input injection.
    w.mu.Lock()
    w.pendingHistory = session.ConversationHistory
    w.historyInjected = false
    w.mu.Unlock()

    return nil
}
```

#### 3.3.3 Input 注入历史前缀

在 `Input()` 中，metadata dispatch 之后、构建 `TurnStartParams` 之前，注入历史：

```go
func (w *AppServerWorker) Input(ctx context.Context, content string, metadata map[string]any) error {
    // ... existing metadata dispatch ...

    // Inject conversation history prefix on first input of a new thread.
    content = w.injectHistoryPrefix(content)

    // ... existing turn/start logic (unchanged) ...
}
```

#### 3.3.4 新增 injectHistoryPrefix 方法

```go
// injectHistoryPrefix prepends conversation history to the first user input
// of a new thread. After injection, pendingHistory is cleared.
func (w *AppServerWorker) injectHistoryPrefix(content string) string {
    w.mu.Lock()
    if w.historyInjected || len(w.pendingHistory) == 0 {
        w.mu.Unlock()
        return content
    }
    history := w.pendingHistory
    w.pendingHistory = nil
    w.historyInjected = true
    w.mu.Unlock()

    var sb strings.Builder
    sb.WriteString("<conversation_history>\n")
    sb.WriteString("Below is the conversation history from a previous session. ")
    sb.WriteString("Use it as context to maintain continuity.\n\n")
    for _, turn := range history {
        switch turn.Role {
        case "user":
            sb.WriteString("[User]: ")
        case "assistant":
            sb.WriteString("[Assistant]: ")
        default:
            continue
        }
        sb.WriteString(turn.Content)
        sb.WriteString("\n\n")
    }
    sb.WriteString("</conversation_history>\n\n")
    sb.WriteString(content)
    return sb.String()
}
```

#### 3.3.5 cleanupOldThread 重置状态

在 `cleanupOldThread()` 中清理历史注入状态：

```go
func (w *AppServerWorker) cleanupOldThread() {
    // ... existing unsubscribe logic ...
    w.mu.Lock()
    // ... existing cleanup ...
    w.pendingHistory = nil
    w.historyInjected = false
    w.mu.Unlock()
}
```

---

## 4. Impact Analysis

### 4.1 受影响的 Worker 类型

| Worker | 影响 | 说明 |
|--------|------|------|
| **CodexCLI** | ✅ 消费历史 | 首条 Input 注入历史前缀 |
| Claude Code | ❌ 不受影响 | 已有 `--resume` 原生恢复，`ConversationHistory` 为空切片 |
| OpenCode Server | ❌ 不受影响 | 使用 session ID 恢复 |
| ACP | ❌ 不受影响 | 无状态适配器 |

### 4.2 性能影响

- `prepareWorkerInfo()` 增加一次 `QueryTurns()` DB 查询，带 session_id 索引，~1ms
- 首条 Input 消息体增大（历史文本），增加 token 消耗
- 后续 Input 不受影响（`historyInjected` 标志跳过）

### 4.3 边界情况

| 场景 | 行为 |
|------|------|
| 全新 session（无历史） | `QueryTurns` 返回空，`ConversationHistory` 为 nil，`injectHistoryPrefix` 返回原 content |
| 查询失败 | 静默忽略，session 正常创建（无历史恢复） |
| 用户执行 /reset | generation 递增，`QueryTurns` 按新 generation 查询，只看到新历史 |
| 长对话（>50 turns） | 只取最近 50 条，早期上下文丢弃 |
| 助手回复含长文本/代码 | 完整注入，可能消耗较多 token |

---

## 5. Testing Plan

### 5.1 单元测试

在 `internal/worker/codexcli/worker_test.go` 添加：

| 测试用例 | 验证 |
|----------|------|
| `TestInjectHistoryPrefix` | 验证格式化输出包含 `[User]`/`[Assistant]` 标记和 `<conversation_history>` 标签 |
| `TestInjectHistoryPrefixIdempotent` | 验证第二次调用 `injectHistoryPrefix` 不修改 content（`historyInjected` 标志） |
| `TestInjectHistoryPrefixEmpty` | `pendingHistory` 为空/nil 时返回原 content |
| `TestInjectHistoryPrefixSkipsEmpty` | 空 Content 的 turn 被跳过 |
| `TestInjectHistoryPrefixClearedOnCleanup` | `cleanupOldThread()` 后状态重置 |

### 5.2 集成测试

1. 启动 dev gateway（`make dev`）
2. 通过 Slack 发送 "记住我的名字是小明"
3. 等待 turn 完成，确认回复
4. 等待 zombie GC 或通过 API 手动 terminate session
5. 在同一 Slack thread 发送 "我叫什么"
6. 验证回复包含 "小明"（从历史恢复）

### 5.3 回归验证

```bash
make test-short   # 快速测试
make lint         # golangci-lint
make check        # 完整 CI
```

---

## 6. Future Improvements (Out of Scope)

- **~Token 预算控制~**: ~~基于 `TurnRecord.TokensIn` 累计~~ → 已实现：字符级预算 `maxHistoryChars=50000`，按 `len(turn.Content)` 累计
- **历史摘要**: 对长对话生成摘要替代全文注入，减少 token 消耗
- **配置化**: 将历史条数上限（当前硬编码 50）暴露为 YAML 配置项
- **Tool 事件注入**: 当前仅注入 text turn，未来可选择性注入 tool_call/tool_result
- **上游 thread resume**: 推动 codex app-server 支持 `thread/resume` API，从根本上解决
