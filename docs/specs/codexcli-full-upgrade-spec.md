# Codex CLI 全功能升级规范

**版本**: 1.1（经上游源码交叉复核）  
**日期**: 2026-05-31  
**目标**: 将 HotPlex Codex CLI 适配器 (`internal/worker/codexcli/`) 提升至与上游 Codex CLI (`~/tmp/codex/codex-rs/`) 100% 协议覆盖，并以 Claude Code 适配器 (`internal/worker/claudecode/`) 为基准实现功能对等。
**复核依据**: HotPlex 源码 + Codex CLI 上游 `codex-rs/app-server-protocol/src/protocol/common.rs:445-900`（ClientRequest wire format）

---

## 目录

1. [现状概述](#现状概述)
2. [Phase 0 —— 紧急协议修复（P0）](#phase-0--紧急协议修复p0)
3. [Phase 1 —— 功能对等（P1）](#phase-1--功能对等p1)
4. [Phase 2 —— 协议完整性（P2）](#phase-2--协议完整性p2)
5. [Phase 3 —— 标志与配置（P3）](#phase-3--标志与配置p3)
6. [Phase 4 —— 跨模块修复](#phase-4--跨模块修复)
7. [验证计划](#验证计划)
8. [文件影响矩阵](#文件影响矩阵)

---

## 现状概述

| 维度 | 当前覆盖率 | 目标 |
|------|-----------|------|
| Exec 事件类型 | 7/8 (87%) | 8/8 (100%) |
| Item 类型映射 | 6/9 (67%) | 9/9 (100%) |
| AppServer JSON-RPC 方法 | 5/25+ (≤20%) | 25/25+ (100%) |
| AppServer 通知 | 14/20 (70%) | 20/20 (100%) |
| Exec CLI 标志传递 | 6/14 (43%) | 14/14 (100%) |
| 交互式响应 | 1/3 (33%) | 3/3 (100%) |
| WorkerCommander 方法 | 0/3 (0%) | 3/3 (100%) |
| 模态 | 2/3 (67%) | 3/3 (100%) |

---

## Phase 0 —— 紧急协议修复（P0）

> **目标**: 修复直接影响流式输出和协议完整性的阻塞性问题。
> **预计工时**: ~4h
> **风险**: 低（所有修改均基于上游协议定义）

### Task 0.1: 添加 `item.updated` 事件处理

**文件**: `internal/worker/codexcli/types.go`, `parser.go`, `mapper.go`, `worker.go`

**背景**: Codex CLI 上游定义了 8 种事件类型（`exec_events.rs:8-37`），HotPlex 仅处理 7 种。缺失的 `item.updated` 是流式增量更新的核心事件，其缺失会导致实时文件变更、命令输出流等增量内容完全丢失。

**修改**:

1. **`types.go`** —— 添加事件常量：
   ```go
   const EventItemUpdated = "item.updated"  // 追加至 EventItemCompleted 之后
   ```

2. **`parser.go`** —— `ParseLine` 无变化（事件类型字段即 type）。

3. **`mapper.go`** —— 在 `Map()` 的 switch 中追加：
   ```go
   case EventItemUpdated:
       return m.mapItemUpdated(event.Item)
   ```
   新增私有方法 `mapItemUpdated(item *CodexItem) []*events.Envelope`：
   - `AgentMessage` → `MessageDelta`（增量文本内容）
   - `CommandExecution` → `ToolResult`（流式 stdout 增量）
   - `FileChange` → `ToolResult`（补丁进度更新）
   - `Reasoning` → `Reasoning`（推理增量）
   - `TodoList` → `State`（计划进度更新）
   - 其他类型 → 记录 debug 日志后跳过

4. **`worker.go`** —— `readOutput()` 中不必退出循环于 `EventItemUpdated`（它不是终止事件）。

**QA**: 
- 执行 `codex exec --json "create a file"` → 确认 item.updated 事件被解析并映射为 AEP 信封
- 确认 MessageDelta 流不被重复计数

---

### Task 0.2: 更新模态声明——添加 `image`

**文件**: `internal/worker/codexcli/worker.go`（两处：`ExecWorker.Modalities()` 和 `AppServerWorker.Modalities()`）

**背景**: Codex CLI 上游通过 `--image`/`-i` 标志（`shared_options.rs:10-18`）原生支持图像。HotPlex 声明 `Modalities() = ["text", "code"]` 但应声明 `["text", "code", "image"]`。

**修改**:
```go
// 之前
func (w *ExecWorker) Modalities() []string    { return []string{"text", "code"} }
func (w *AppServerWorker) Modalities() []string { return []string{"text", "code"} }

// 之后
func (w *ExecWorker) Modalities() []string    { return []string{"text", "code", "image"} }
func (w *AppServerWorker) Modalities() []string { return []string{"text", "code", "image"} }
```

**QA**: 网关应广播 `capabilities.modalities` 包含 `"image"`。

---

### Task 0.3: 移除幻影 `ItemImageGeneration` 常量

**文件**: `internal/worker/codexcli/types.go`, `mapper.go`

**背景**: `ItemImageGeneration = "image_generation"` 在 Codex CLI 上游（`exec_events.rs`）中不存在。图像是通过 `--image` CLI 标志（而非事件类型）处理的。保留此常量会产生误导。

**修改**:

1. **`types.go`** —— 删除 `ItemImageGeneration = "image_generation"`。
2. **`mapper.go`** —— 删除 `mapItemCompleted` 中 `case ItemImageGeneration:` 分支。

**QA**: 编译通过，无未引用常量。

---

## Phase 1 —— 功能对等（P1）

> **目标**: 实现 WorkerCommander、交互式响应，并补齐所有缺失的 Item 类型映射。
> **预计工时**: ~12h
> **风险**: 中（WorkerCommander 依赖 AppServer 协议；需谨慎测试）

### Task 1.1: 实现 WorkerCommander 接口

**文件**: `internal/worker/codexcli/worker.go`, `commands.go`

**背景**: HotPlex gateway 的直通命令（`/compact`、`/clear`、`/rewind`）通过 `WorkerCommander` 接口分发（`gateway/worker_cmds.go:132`）。Codex CLI 未实现该接口，导致命令静默降级为 `w.Input()` 原始文本。

Claude Code 将 Compact/Clear/Rewind 实现为结构化操作；Codex CLI 对应的协议方法为：`thread/compact/start`（显式的压缩方法，`common.rs:541`）、`thread/rollback`（回滚，`common.rs:562`）、`thread/name/set` + 新 thread/start（清除）、`turn/interrupt`（中断，`common.rs:762`）。

**修改**:

1. **`worker.go`** —— 追加编译时断言：
   ```go
   var _ worker.WorkerCommander = (*AppServerWorker)(nil)
   ```

2. **`commands.go`** —— 扩展 `ServerCommander`：
   ```go
   // Compact 发送 thread/compact/start 请求，触发上下文压缩。
   // 上游 wire format: "thread/compact/start" (common.rs:541)
   func (sc *ServerCommander) Compact(ctx context.Context, _ map[string]any) error

   // Clear 通过杀死当前线程并以相同配置创建新线程来清空上下文。
   // 实现：调用 turn/interrupt → thread/unsubscribe → thread/start（新线程）
   func (sc *ServerCommander) Clear(ctx context.Context) error

   // Rewind 发送 thread/rollback 请求，回滚至前一个助理消息。
   // 上游 wire format: "thread/rollback" (common.rs:562)
   func (sc *ServerCommander) Rewind(ctx context.Context, targetID string) error
   ```

3. **`worker.go`** —— 将 `ServerCommander` 作为接口暴露给 `AppServerWorker`：
   ```go
   func (w *AppServerWorker) Compact(ctx context.Context, args map[string]any) error {
       return w.commands.Compact(ctx, args)
   }
   func (w *AppServerWorker) Clear(ctx context.Context) error {
       return w.commands.Clear(ctx)
   }
   func (w *AppServerWorker) Rewind(ctx context.Context, targetID string) error {
       return w.commands.Rewind(ctx, targetID)
   }
   ```

4. **`worker.go`** —— `ExecWorker` 同样添加（必要时可返回 `ErrNotImplemented` 并附带解释性日志）：
   ```go
   func (w *ExecWorker) Compact(ctx context.Context, args map[string]any) error {
       return fmt.Errorf("codexcli: compact not supported in exec mode; use app-server mode")
   }
   // Clear, Rewind 同理
   ```

**QA**: 
- 在 WebChat/Slack/飞书中发送 `/compact` → 确认通过 `ServerCommander.Compact()` 分发（非 `w.Input()`）
- 验证 `gateway/worker_cmds.go:132` 的类型断言成功
- 验证命令反馈消息被正确发送（`sendCommandFeedback`）

---

### Task 1.2: 实现 HandleQuestionResponse

**文件**: `internal/worker/codexcli/worker.go`, `manager.go`

**背景**: 当 Codex CLI 调用 `AskUserQuestion` 工具时，HotPlex 会发出 `QuestionRequest` AEP 事件。用户回答后，gateway 调用 `HandleQuestionResponse()`——当前该方法返回 `"not supported in app-server mode yet"`。

**修改**:

1. **`worker.go`** —— `AppServerWorker.HandleQuestionResponse`：
   ```go
   func (w *AppServerWorker) HandleQuestionResponse(ctx context.Context, reqID string, answers map[string]string) error {
       result := map[string]any{
           "behavior": "allow",
           "updatedInput": map[string]any{
               "answers": answers,
           },
       }
       return w.manager.RespondServerRequest(reqID, result)
   }
   ```

2. **`worker.go`** —— `ExecWorker.HandleQuestionResponse`：记录该单次模式不支持此功能，并返回错误。

**QA**: 向 Codex CLI 发送需要 AskUserQuestion 的提示词 → 确认问题已传递至用户 → 用户回答后确认响应已发送。

---

### Task 1.3: 实现 HandleElicitationResponse

**文件**: `internal/worker/codexcli/worker.go`, `manager.go`

**背景**: 与 QuestionResponse 相同模式，用于 MCP 引导请求。

**修改**:

1. **`worker.go`** —— `AppServerWorker.HandleElicitationResponse`：
   ```go
   func (w *AppServerWorker) HandleElicitationResponse(ctx context.Context, reqID, action string, content map[string]any) error {
       result := map[string]any{
           "action":  action,
           "content": content,
       }
       return w.manager.RespondServerRequest(reqID, result)
   }
   ```

**QA**: 向支持引导的 Codex CLI 发送 MCP 提示词 → 确认引导已传递至用户 → 用户操作后确认响应已发送。

---

### Task 1.4: 添加缺失的 Item 类型映射

**文件**: `internal/worker/codexcli/mapper.go`, `types.go`

**背景**: Codex CLI 上游定义了 9 种 Item 类型（`exec_events.rs:104-130`）。HotPlex 映射了其中 6 种。缺失 3 种：`CollabToolCall`、`WebSearch`、`Error`（Item 级别）。`TodoList` 被映射为 `ItemPlan`，但上游结构更丰富。

**修改**:

1. **`types.go`** —— 添加 Item 类型常量：
   ```go
   const (
       ItemCollabToolCall = "collab_tool_call"
       ItemWebSearch      = "web_search"
       ItemError          = "error"          // Item 级别（不同于 event 级别 EventError）
       ItemTodoList       = "todo_list"      // 替换 ItemPlan
   )
   ```
   扩展 `CodexItem` 结构体以支持这些类型（`CollabTool`、`AgentsStates`、`Query`、`Action`、`TodoItems` 等）。

2. **`mapper.go`** —— `mapItemStarted`：
   ```go
   case ItemCollabToolCall:
       // 映射至 ToolCall(collab:X) —— 类似于 MCP 工具调用模式
   case ItemWebSearch:
       // 映射至 ToolCall(web_search) 
   ```

3. **`mapper.go`** —— `mapItemCompleted`：
   ```go
   case ItemCollabToolCall:
       // 映射至 ToolResult
   case ItemWebSearch:
       // 映射至 ToolResult with query + results summary
   case ItemError:
       // 映射至 Error 事件（非致命项目错误）
   case ItemTodoList:
       // 映射至 State(planning) 并附带结构化 todo items
   ```

**QA**: 
- 对每种 Item 类型，确认从 `codex exec --json` 输出中解析
- 确认 AEP 信封格式与现有模式一致
- 对不熟悉的上游代码，需从 `~/tmp/codex/codex-rs/exec/src/exec_events.rs` 获得精确的字段定义

---

## Phase 2 —— 协议完整性（P2）

> **目标**: 暴露 Codex CLI 的完整 AppServer JSON-RPC 2.0 协议。
> **预计工时**: ~16h
> **风险**: 高（上游协议接口可能变更；需防御性实现）

### Task 2.1: Thread 生命周期方法

**文件**: `internal/worker/codexcli/manager.go`, `worker.go`

**上游源文件**: `~/tmp/codex/codex-rs/app-server-protocol/src/protocol/common.rs:445-600`（ClientRequest wire format 定义）

| Wire 方法名 | 用途 | 使用位置 |
|--------|-------|---------|
| `thread/resume` | 通过 `thread_id` 恢复已有线程（`common.rs:451`） | `Resume()` 应使用此方法（而非重启） |
| `thread/fork` | 基于现有线程创建新线程（`common.rs:457`） | `session.ForkSession` 支持 |
| `thread/archive` | 归档线程（`common.rs:463`） | 会话清理 |
| `thread/unarchive` | 取消归档线程（`common.rs:536`） | 会话恢复 |
| `thread/name/set` | 重命名线程（`common.rs:492`） | 用户面向的命名 |
| `thread/goal/set` | 设置线程目标（`common.rs:497`） | 上下文感知 |
| `thread/goal/get` | 获取线程目标（`common.rs:502`） | 状态查询 |
| `thread/goal/clear` | 清除线程目标（`common.rs:507`） | 上下文重置 |
| `thread/settings/update` | 更新运行时配置（`common.rs:518`） | 热重载（模型、沙箱等） |
| `thread/compact/start` | 显式触发上下文压缩（`common.rs:541`） | **WorkerCommander.Compact() 直接映射** |
| `thread/rollback` | 回滚至先前状态（`common.rs:562`） | **WorkerCommander.Rewind() 直接映射** |

> ⚠️ **注意**: `thread/settings`（只读）在上游协议中**不存在**——仅有 `thread/settings/update`（写入）。如需读取设置，请通过 `thread/read` 获取。
> ⚠️ **注意**: `thread/goal` 是**三个独立方法**（set/get/clear），而非单一读写方法。

**修改**:

1. **`manager.go`** —— 添加以下方法：
   - `ResumeThread(id, params)` → 调用 `"thread/resume"`
   - `ForkThread(id, params)` → 调用 `"thread/fork"`
   - `ArchiveThread(id)` / `UnarchiveThread(id)` → 调用 `"thread/archive"` / `"thread/unarchive"`
   - `SetThreadName(id, name)` → 调用 `"thread/name/set"`
   - `SetThreadGoal(id, goal)` / `GetThreadGoal(id)` / `ClearThreadGoal(id)` → 调用 `"thread/goal/set"` / `"thread/goal/get"` / `"thread/goal/clear"`
   - `UpdateThreadSettings(id, settings)` → 调用 `"thread/settings/update"`
   - `CompactThread(id)` → 调用 `"thread/compact/start"`
   - `RollbackThread(id, targetID)` → 调用 `"thread/rollback"`

2. **`worker.go`** —— 更新 `Resume()` 使用 `thread/resume` 协议方法（而非 `thread/start`）。为新会话添加 `ForkSession` 支持。

**QA**: 每个方法均需独立验证其请求/响应是否符合上游协议定义。

---

### Task 2.2: Turn 控制方法 + 环境变量

**文件**: `internal/worker/codexcli/manager.go`, `worker.go`

**上游源文件**: `~/tmp/codex/codex-rs/app-server-protocol/src/protocol/common.rs:750-768`

| Wire 方法名 | 用途 | 对应 WorkerCommander |
|--------|-------|---------------------|
| `turn/steer` | 转向运行中的轮次（`common.rs:756`） | 用于 `Compact()` 的备选方案（主方案为 `thread/compact/start`） |
| `turn/interrupt` | 中断运行中的轮次（`common.rs:762`） | `Clear()` 流程的第一步 |
| `environment/add` | 添加环境变量（`common.rs:862`） | `session.ConfigEnv` 支持 |

> ⚠️ **注意**: `turn/environment` 在上游协议中**不存在**。环境变量通过全局的 `environment/add` 方法设置，而非 per-turn 方法。

**修改**:

1. **`manager.go`** —— 添加 `SteerTurn(threadID, params)`、`InterruptTurn(threadID)`、`AddEnvironment(env)`。

2. **`worker.go`** —— 在 `Compact()` 中连接 `thread/compact/start`（主方案）或 `turn/steer`（备选）；在 `Clear()` 中连接 `turn/interrupt` 后执行 `thread/unsubscribe` + `thread/start`。

**QA**: 转向正在执行命令的轮次（例如 `"write a large file"`）→ 确认轮次被转向至压缩上下文。

---

### Task 2.3: MCP 服务器方法

**文件**: `internal/worker/codexcli/manager.go`, `commands.go`

**上游源文件**: `~/tmp/codex/codex-rs/app-server-protocol/src/protocol/common.rs:868-898`

| Wire 方法名 | 用途 |
|--------|-------|
| `mcpServerStatus/list` | 列出所有 MCP 服务器及其状态（`common.rs:880`） |
| `mcpServer/resource/read` | 从 MCP 服务器读取资源（`common.rs:886`） |
| `mcpServer/tool/call` | 直接调用 MCP 工具（`common.rs:892`） |
| `config/mcpServer/reload` | 刷新 MCP 服务器配置（`common.rs:874`） |
| `mcpServer/oauth/login` | 启动 MCP OAuth 登录流程（`common.rs:868`） |

> ⚠️ **注意**: MCP 方法使用 **camelCase 分段 + 斜杠** 混合格式，与 thread/turn 方法的全小写格式不同。例如 `mcpServerStatus/list`（而非 `mcp/server/listStatus`）。

**修改**:

1. **`manager.go`** —— 添加 `ListMCPServerStatus()`、`ReadMCPResource(uri)`、`CallMCPTool(server, tool, args)`、`RefreshMCPServer(name)`、`MCPServerOAuthLogin(name)`。

2. **`commands.go`** —— 在 `SendControlRequest` 中暴露 `mcp_status`、`mcp_refresh`、`mcp_oauth` 子类型。

**QA**: 配置带 Codex CLI 的 MCP 服务器 → 通过网关控制命令验证状态/刷新。

---

### Task 2.4: Review 支持

**文件**: `internal/worker/codexcli/manager.go`, `worker.go`

**上游源文件**: `~/tmp/codex/codex-rs/app-server-protocol/src/protocol/v2/review.rs`

| 方法 | 用途 |
|--------|-------|
| `review/start` | 以审查模式启动会话（未提交的/基于分支的分支/基于提交的分支） |

**修改**:

1. **`manager.go`** —— 添加 `StartReview(params)`，支持 `ReviewTarget::Uncommitted`、`ReviewTarget::Branch(name)`、`ReviewTarget::Commit(sha)`。

2. **`worker.go`** —— 当 `session.Review` 标志被设置时（如果网关传递了该标志），于 `Start()` 中连接。

**QA**: 在 Git 仓库中启动审查会话 → 确认审查事件已正确传递至 AEP。

---

### Task 2.5: 缺失的通知处理

**文件**: `internal/worker/codexcli/mapper.go`, `manager.go`

**背景**: HotPlex 在 `MapNotification()` 中当前处理 20 种通知中的 14 种。

| 缺失的通知 | 映射 |
|---------|-------|
| `item/updated` | → 根据 Item 类型映射为 `MessageDelta` / `ToolResult` / `Reasoning` / `State` |
| `turn/diff/updated` | → 映射为 `ToolResult`（diff 输出）或 `Raw` |
| `turn/plan/updated` | → 映射为 `State(planning)` 并附带结构化步骤 |
| `thread/settings/updated` | → 映射为 `State`（新配置） |
| `serverRequest/resolved` | → 映射为日志条目（通常无需用户交互） |
| `deprecationNotice` | → 映射为 `Step(deprecation)` |
| `guardianWarning` | → 映射为 `Step(guardian)` 或 `Error`（取决于严重程度） |

**修改**:

1. **`mapper.go`** —— 在 `MapNotification()` 的 switch 中追加 7 个 case，分别包含适当的映射逻辑。

2. **`types.go`** —— 根据需要为通知负载添加类型。

**QA**: 验证来自 Codex CLI 上游的每种通知类型均能被解析，不会导致 "unknown notification" 日志。

---

## Phase 3 —— 标志与配置（P3）

> **目标**: 将 Codex CLI 的所有 CLI 标志传递至 `buildArgs()` 和 `buildThreadStartParams()`。
> **预计工时**: ~6h
> **风险**: 低（纯追加性修改）

### Task 3.1: Exec CLI 标志传递

**文件**: `internal/worker/codexcli/worker.go` (`buildArgs`), `internal/config/config.go`

**上游源文件**: `~/tmp/codex/codex-rs/exec/src/cli.rs`, `~/tmp/codex/codex-rs/utils/cli/src/shared_options.rs`

| Codex CLI 标志 | 来自 SessionInfo 的数据源 | 状态 |
|-------------|---------------------|--------|
| `--image` / `-i` | `session.Images`（需新增字段） | 缺失 |
| `--output-schema` | `session.JSONSchema`（已存在） | 缺失 |
| `--add-dir` | `session.AllowedDirs`（已存在） | 缺失 |
| `--color` | 新配置或 `session.Color` | 缺失 |
| `--output-last-message` / `-o` | `session.OutputFile` | 缺失 |
| `--strict-config` | `session.StrictConfig` | 缺失 |
| `--skip-git-repo-check` | `session.SkipGitRepoCheck` | 缺失 |
| `--ignore-user-config` | `session.IgnoreUserConfig` | 缺失 |
| `--ignore-rules` | `session.IgnoreRules` | 缺失 |
| `--oss` | 新配置 | 缺失 |
| `--local-provider` | `session.LocalProvider` | 缺失 |
| `--profile` / `-p` | `session.ConfigProfile` | 缺失 |
| `--dangerously-bypass-hook-trust` | `session.BypassHookTrust` | 缺失 |
| `resume --last` | `session.ResumeLast` | 缺失 |

**修改**:

1. **`worker.go`** —— 在 `buildArgs()` 中追加 14+ 个条件标志。
2. **`internal/config/config.go`** —— 在 `CodexCLIConfig` 中添加默认值（`Color`、`IgnoredRules` 等）。
3. **`internal/worker/worker.go`** —— 根据需要扩展 `SessionInfo` 结构体。

**QA**: 通过 `SessionInfo` 设置每个标志 → 确认标志已追加至进程参数列表。

---

### Task 3.2: AppServer Thread Start 参数

**文件**: `internal/worker/codexcli/worker.go` (`buildThreadStartParams`)

同样在 `thread/start` 参数中暴露缺失的标志：
- `images`（作为图片附件传递）
- `outputSchema`（JSON Schema 文件路径）
- `additionalDirectories`（写访问权限）
- `color`、`strictConfig`、`ignoreRules` 等

---

## Phase 4 —— 跨模块修复

> **目标**: 修复 Cron、Gateway API 和 Brain 中的硬编码缺点。
> **预计工时**: ~4h
> **风险**: 低

### Task 4.1: Cron SessionKey 硬编码

**文件**: `internal/cron/types.go`

**修改**（第 69 行）:
```go
// 之前
func (j *CronJob) SessionKey() string {
    return session.DerivePlatformSessionKey(j.Payload.OwnerID, worker.TypeClaudeCode, pctx)
}

// 之后
func (j *CronJob) SessionKey() string {
    wt := j.Payload.WorkerType
    if wt == "" {
        wt = worker.TypeClaudeCode  // 保留下兼容默认值
    }
    return session.DerivePlatformSessionKey(j.Payload.OwnerID, wt, pctx)
}
```

**QA**: 创建 `worker_type: codex_cli` 的 cron 任务 → 确认 session key 推导使用正确的 worker type。

---

### Task 4.2: EnvBlocklist 补充

**文件**: `internal/worker/codexcli/types.go`

**修改**:
```go
// 之前
var EnvBlocklist = []string{"HOTPLEX_", "CODEX_"}

// 之后（添加嵌套代理检测，与 claudecode 的做法一致）
var EnvBlocklist = []string{"HOTPLEX_", "CODEX_", "CODEXCLI"}
```

**QA**: 确认 `CODEXCLI=` 不会泄露至 worker 子进程。

---

## 验证计划

### 每项 Task 的通用 QA 步骤

1. **编译**: `go build ./internal/worker/codexcli/...`
2. **Lint**: `golangci-lint run ./internal/worker/codexcli/...`
3. **现有测试**: `go test -race -count=1 ./internal/worker/codexcli/...` —— 必须全部通过
4. **新增测试**: 每个公共方法至少一个 table-driven 测试

### Phase 0 QA

| 测试用例 | 输入 | 预期输出 | 验证方法 |
|-----------|-------|--------|-----------|
| item.updated 事件被解析 | `{"type":"item.updated","item":{...}}` | AEP 信封（MessageDelta/ToolResult/Reasoning） | 单元测试 |
| 模态包含 image | `worker.Modalities()` | `["text","code","image"]` | 单元测试 |
| ImageGeneration 已移除 | 代码搜索 | 无引用 | grep |

### Phase 1 QA

| 测试用例 | 输入 | 预期输出 | 验证方法 |
|-----------|-------|--------|-----------|
| /compact（AppServerWorker） | `Compact(ctx, nil)` | `thread/compact/start` 请求已发送至 Manager | 集成测试 |
| /compact（ExecWorker） | `Compact(ctx, nil)` | `ErrNotImplemented` 并附带解释性错误消息 | 单元测试 |
| HandleQuestionResponse | `HandleQuestionResponse(ctx, "req1", {"q1":"a"})` | `RespondServerRequest` 已调用 | 集成测试 |
| HandleElicitationResponse | `HandleElicitationResponse(ctx, "req1", "accept", {...})` | `RespondServerRequest` 已调用 | 集成测试 |
| CollabToolCall 已映射 | `{"type":"collab_tool_call",...}` | `ToolCall(collab:X)` AEP 信封 | 单元测试 |
| WebSearch 已映射 | `{"type":"web_search",...}` | `ToolCall(web_search)` AEP 信封 | 单元测试 |

### Phase 2 QA

| 测试用例 | 输入 | 预期输出 | 验证方法 |
|-----------|-------|--------|-----------|
| thread/resume | `ResumeThread("thread-123")` | 成功响应 | 集成测试 |
| thread/fork | `ForkThread("thread-123")` | 新 thread ID | 集成测试 |
| thread/name/set | `SetThreadName("thread-123", "My Thread")` | 成功响应 | 集成测试 |
| thread/goal/set | `SetThreadGoal("thread-123", goal)` | 成功响应 | 集成测试 |
| thread/goal/get | `GetThreadGoal("thread-123")` | goal 对象 | 集成测试 |
| thread/goal/clear | `ClearThreadGoal("thread-123")` | 成功响应 | 集成测试 |
| thread/compact/start | `CompactThread("thread-123")` | 成功响应 | 集成测试 |
| thread/rollback | `RollbackThread("thread-123", "")` | 成功响应 | 集成测试 |
| turn/interrupt | `InterruptTurn("thread-123")` | 成功响应 | 集成测试 |
| mcpServerStatus/list | `ListMCPServerStatus()` | 服务器列表 | 集成测试 |
| mcpServer/resource/read | `ReadMCPResource("uri")` | 资源内容 | 集成测试 |
| mcpServer/tool/call | `CallMCPTool("server", "tool", args)` | 工具结果 | 集成测试 |
| config/mcpServer/reload | `RefreshMCPServer("name")` | 成功响应 | 集成测试 |
| mcpServer/oauth/login | `MCPServerOAuthLogin("name")` | OAuth URL | 集成测试 |
| review/start | `StartReview({target:Uncommitted})` | 新线程已创建，审查事件 | 集成测试 |

### Phase 3 QA

| 测试用例 | 输入 | 预期输出 | 验证方法 |
|-----------|-------|--------|-----------|
| buildArgs --image | `session.Images = ["a.png"]` | `["exec","--json","--image","a.png",...]` | 单元测试 |
| buildArgs --output-schema | `session.JSONSchema = "{}"` | `["exec","--json","--output-schema","/tmp/...",...]` | 单元测试 |
| buildArgs --add-dir | `session.AllowedDirs = ["/tmp"]` | `["exec","--json","--add-dir","/tmp",...]` | 单元测试 |

### Phase 4 QA

| 测试用例 | 输入 | 预期输出 | 验证方法 |
|-----------|-------|--------|-----------|
| Cron SessionKey with codex_cli | `Payload{WorkerType:"codex_cli", OwnerID:"u1"}` | key 推导使用 TypeCodexCLI | 单元测试 |
| CODEXCLI env blocked | `os.Setenv("CODEXCLI", "v")` → worker process | CODEXCLI 在子进程中不存在 | 集成测试 |

---

## 文件影响矩阵

| 文件 | Phase 0 | Phase 1 | Phase 2 | Phase 3 | Phase 4 | 受影响行数 |
|------|---------|---------|---------|---------|---------|------|
| `types.go` | +1 常量, -1 常量 | +4 常量, +struct 字段 | — | — | +1 env | **~30** |
| `parser.go` | 无变更 | — | — | — | — | **0** |
| `mapper.go` | +1 事件 case, +1 方法 | +4 item case, +4 方法 | +7 通知 case | — | — | **~200** |
| `worker.go` | +1 模态字段 | +6 方法, +2 接口断言 | +5 方法 | +14 标志 | — | **~150** |
| `commands.go` | — | +3 方法 | +2 方法 | — | — | **~80** |
| `manager.go` | — | — | +22 方法 | — | — | **~480** |
| `config.go` | — | — | — | +字段 | — | **~20** |
| `internal/config/config.go` | — | — | — | +字段 | — | **~15** |
| `internal/worker/worker.go` | — | — | — | +SessionInfo 字段 | — | **~10** |
| `internal/cron/types.go` | — | — | — | — | +修复 | **~5** |
| **总计** | | | | | | **~990 行** |

---

## 依赖关系

```
Phase 0 (无依赖)
  ├── Task 0.1 (item.updated) ── 独立
  ├── Task 0.2 (modalities) ── 独立
  └── Task 0.3 (remove phantom) ── 独立

Phase 1 (依赖 Phase 0 中已定义的 item 类型)
  ├── Task 1.1 (WorkerCommander) ── 依赖 Task 2.1 (thread/compact/start, thread/rollback)
  ├── Task 1.2 (QuestionResponse) ── 独立
  ├── Task 1.3 (ElicitationResponse) ── 独立
  └── Task 1.4 (item types) ── 独立

Phase 2 (依赖 Phase 1 中已定义的 manager 方法)
  ├── Task 2.1 (thread 方法) ── 独立；被 Task 1.1 依赖
  ├── Task 2.2 (turn 方法 + 环境变量) ── 独立
  ├── Task 2.3 (MCP 方法) ── 独立
  ├── Task 2.4 (review) ── 独立
  └── Task 2.5 (notifications) ── 独立

Phase 3 (无依赖)
  └── Task 3.1+3.2 (标志) ── 独立

Phase 4 (无依赖)
  ├── Task 4.1 (cron) ── 独立
  └── Task 4.2 (env blocklist) ── 独立
```

---

## 风险与缓解措施

| 风险 | 影响 | 缓解措施 |
|------|--------|----------|
| 上游 Codex CLI 协议可能在无通知的情况下变更 | 对新字段的反序列化失败 | 对所有 JSON 结构体使用 `json:"..."` + `omitempty`；对未知字段优雅降级 |
| AppServer 单例管理器死锁 | 网关挂起 | 遵循现有的 mutex 排序（`m.mu` → `writeMu`）；所有新方法复用 `Call()` 的超时机制 |
| `thread/compact/start` 或 `turn/steer` 可能无响应地挂起 | Worker 挂起 | Manager 的 `Call()` 已设置 30s 超时（`defaultCallTimeout`） |
| Wire format 错误（如 spec 中之前错误的 `mcp/server/listStatus`） | JSON-RPC 调用失败 | 所有方法名已通过上游 `common.rs` 的 `#[serde(rename)]` 定义验证 |
| 破坏现有的 ExecWorker 行为 | 功能回归 | 首先运行现有测试；使用 `t.Parallel()` 隔离新测试 |

---

## 术语表

| 术语 | 定义 |
|------|------------|
| **AEP** | Agent 交换协议 —— HotPlex 的 WebSocket 事件协议 |
| **Exec 模式** | 单次 `codex exec --json <prompt>` —— 每次输入对应一个进程 |
| **AppServer 模式** | 持久的 `codex app-server` —— 所有会话共享一个 JSON-RPC 进程 |
| **WorkerCommander** | 支持 Compact/Clear/Rewind 直通命令的 Worker 接口 |
| **Thread** | Codex CLI 会话 —— 包含多个轮次（turns） |
| **Turn** | 单次提示词-响应循环 |
| **Item** | 轮次内的内容单元（代理消息、命令、文件变更等） |
| **Steer** | 在运行中的轮次中改变代理行为的中途指令 |
