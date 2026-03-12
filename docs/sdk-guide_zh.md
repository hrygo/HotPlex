# HotPlex SDK 开发者手册 (Go)

*查看其他语言: [English](sdk-guide.md), [简体中文](sdk-guide_zh.md).*

欢迎使用 HotPlex SDK！本手册旨在指导开发者如何将 HotPlex 强大的 AI 智能体运行时 (Agent Runtime) 集成到自己的 Go 应用程序中。

---

## 1. 核心理念

HotPlex 的核心哲学是 **"利用胜于构建 (Leverage vs Build)"**。我们不重新发明 AI 智能体，而是通过 SDK 将顶级的终端 AI 工具（如 Claude Code, OpenCode）转化为生产就绪的后端服务：

- **热复用 (Hot-Multiplexing)**：消除进程冷启动延迟，实现毫秒级响应。
- **安全隔离**：提供进程组 (PGID) 隔离和指令级 WAF 审计，包含 danger block 闭环确认。
- **协议标准化**：将多样化的 CLI 输出归一化为统一的流式事件。
- **多平台支持**：原生适配器支持 Slack、飞书。

---

## 2. 快速开始

### 2.1 安装

```bash
go get github.com/hrygo/hotplex
```

### 2.2 基础使用示例

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/hrygo/hotplex"
)

func main() {
    // 1. 初始化引擎
    engine, err := hotplex.NewEngine(hotplex.EngineOptions{
        Namespace: "my_app",
        Timeout:   5 * time.Minute,
    })
    if err != nil {
        panic(err)
    }
    defer engine.Close()

    // 2. 配置执行参数
    cfg := &hotplex.Config{
        WorkDir:   "/tmp/project",
        SessionID: "user_session_123",
    }

    // 3. 定义流式回调
    callback := func(eventType string, data any) error {
        switch eventType {
        case "answer":
            if evt, ok := data.(*hotplex.EventWithMeta); ok {
                fmt.Print(evt.EventData)
            }
        case "session_stats":
            if stats, ok := data.(*hotplex.SessionStatsData); ok {
                fmt.Printf("\n消耗 Token: %d 输入, %d 输出\n",
                    stats.InputTokens, stats.OutputTokens)
            }
        case "danger_block":
            if evt, ok := data.(*hotplex.EventWithMeta); ok {
                fmt.Printf("[已拦截] %s\n", evt.EventData)
            }
        }
        return nil
    }

    // 4. 执行指令
    ctx := context.Background()
    engine.Execute(ctx, cfg, "帮我写一个快速排序算法", callback)
}
```

---

## 3. 核心 API 详解

### 3.1 `EngineOptions` (引擎初始化配置)

用于 `hotplex.NewEngine(opts)`，定义引擎的全局行为边界。

| 字段               | 类型            | 说明                                                                           |
| :----------------- | :-------------- | :----------------------------------------------------------------------------- |
| `Namespace`        | `string`        | **命名空间**。用于生成确定性的 UUID v5 SessionID，确保多租户间的会话物理隔离。 |
| `Timeout`          | `time.Duration` | **执行超时**。单次 `Execute` 调用的最大允许时间（默认 5 分钟）。               |
| `IdleTimeout`      | `time.Duration` | **空闲回收时间**。后台进程超过此时间无活动将被自动清理（默认 30 分钟）。       |
| `BaseSystemPrompt` | `string`        | **引擎级系统提示词**。在进程启动时注入，作为所有会话的底层行为准则。           |
| `PermissionMode`   | `string`        | **权限模式**。如 `"bypass-permissions"` (自动授权) 或 `"default"`。            |
| `AllowedTools`     | `[]string`      | **工具白名单**。显式允许智能体使用的工具列表（如 `["Bash", "Edit"]`）。        |
| `DisallowedTools`  | `[]string`      | **工具黑名单**。显式禁止智能体使用的工具列表。                                 |
| `AdminToken`       | `string`        | **管理令牌**。用于在运行时通过安全审计接口越权或调整策略的凭证。               |
| `Logger`           | `*slog.Logger`  | **结构化日志**。注入该实例以维持应用整体的观测一致性。                         |
| `Provider`         | `Provider`      | **驱动程序**。可选，用于手动指定底层智能体实现（默认使用 Claude Code）。       |

### 3.2 `Config` (单次任务配置)

用于 `engine.Execute(ctx, cfg, prompt, cb)`，定义当前任务的具体环境。

| 字段               | 类型     | 说明                                                         |
| :----------------- | :------- | :----------------------------------------------------------- |
| `WorkDir`          | `string` | **工作目录**。智能体执行文件操作、搜索和脚本运行的根目录。   |
| `SessionID`        | `string` | **会话 ID**。业务层 ID，HotPlex 将其映射为唯一的后台热进程。 |
| `TaskInstructions` | `string` | **任务指令**。定义会话目标的持久化指令。                     |

### 3.3 事件回调与数据模型 (`Callback`)

`Callback` 的定义为 `func(eventType string, data any) error`。

#### 事件类型 (`eventType`)

| 事件类型 | 说明 |
|----------|------|
| `thinking` | 智能体逻辑推理中 |
| `tool_use` | 开始调用本地工具（如 `bash`, `editor_write`） |
| `tool_result` | 工具执行完毕返回结果 |
| `answer` | 智能体生成的回复文本片段 |
| `session_stats` | 会话最终统计（仅在成功结束时触发一次） |
| `danger_block` | 安全火墙拦截告警 - 需要用户批准 |
| `runner_exit` | 底层进程意外退出 |
| `plan_mode` | AI 处于计划模式，正在生成计划 |
| `exit_plan_mode` | AI 计划完成，请求用户批准 |
| `ask_user_question` | AI 正在询问澄清问题 |
| `permission_request` | 来自 Claude Code 的权限请求 |
| `command_progress` | 斜杠命令执行中，带进度更新 |
| `command_complete` | 斜杠命令执行完成 |
| `session_start` | 新会话开始（冷启动） |
| `engine_starting` | 引擎正在启动 |
| `user_message_received` | 用户消息已接收 |

#### 详细元数据 (`EventMeta`)

除 `session_stats` 外，大部分事件的 `data` 为 `*hotplex.EventWithMeta`。其 `Meta` 包含：

| 字段 | 说明 |
|------|------|
| `DurationMs` | 当前步骤耗时 |
| `TotalDurationMs` | 累计总耗时 |
| `ToolName` / `ToolID` | 调用的工具名称及唯一 ID |
| `Status` | 执行状态（`running`, `success`, `error`） |
| `InputSummary` / `OutputSummary` | 工具输入参数的摘要及输出结果的截断预览 |
| `FilePath` / `LineCount` | 涉及的文件路径及影响的行数 |
| `Progress` | 进度百分比（针对长耗时任务） |
| `InputTokens` / `OutputTokens` | 当前步骤的 Token 消耗 |

#### 最终统计 (`SessionStatsData`)

`session_stats` 事件的 `data` 为 `*hotplex.SessionStatsData`：

| 字段 | 说明 |
|------|------|
| `InputTokens` / `OutputTokens` | 会话全过程总 Token |
| `CacheReadTokens` / `CacheWriteTokens` | 提示词缓存的命中与写入 |
| `TotalDurationMs` | 从请求开始到结束的总毫秒数 |
| `ToolCallCount` | 总工具调用次数 |
| `ToolsUsed` | 调用的工具名称列表（去重） |
| `FilesModified` | 实际产生修改的文件数 |
| `TotalCostUSD` | 该轮通话的预估美金成本 |
| `IsError` | 执行是否以失败告终 |

### 3.4 管理与安全控制 (`HotPlexClient`)

`HotPlexClient` 通过多个功能专一的子接口提供控制。由于 `hotplex.NewEngine` 返回的 `*Engine` 结构体已完整实现这些接口，您可以直接调用，或者在持有通用 Client 接口时通过类型断言来使用。

#### 使用示例

```go
// 1. 基础执行 (Executor 接口)
client.Execute(ctx, cfg, prompt, cb)

// 2. 进阶控制 (SessionController 接口)
// 如果您持有的是 hotplex.HotPlexClient 接口，可以通过断言获取子能力
if controller, ok := client.(hotplex.SessionController); ok {
    stats := controller.GetSessionStats("user_session_123")
    fmt.Printf("已消耗 Input Tokens: %d\n", stats.InputTokens)

    // 强制终止一个超时的会话
    controller.StopSession("session_123", "用户手动取消")
}

// 3. 安全策略管理 (SafetyManager 接口)
if safety, ok := client.(hotplex.SafetyManager); ok {
    // 动态调整安全沙箱允许的路径
    safety.SetDangerAllowPaths([]string{"/home/user/project"})
    // 使用 AdminToken 开启 Bypass 模式（慎用）
    safety.SetDangerBypassEnabled("your-admin-token", true)
}
```

#### `SessionController` (生命周期与观测)

| 方法                                | 说明                                                       |
| :---------------------------------- | :--------------------------------------------------------- |
| `GetSessionStats(id) *SessionStats` | 返回特定会话的最新的遥测数据和 Token 使用情况。            |
| `StopSession(id, reason) error`     | 强制终止特定会话及其进程组（适用于 Web UI 的"停止"按钮）。 |
| `GetCLIVersion() (string, error)`   | 返回底层 AI CLI 工具的版本号。                             |

#### `SafetyManager` (安全策略)

| 方法                                        | 说明                                                    |
| :------------------------------------------ | :------------------------------------------------------ |
| `SetDangerAllowPaths([]string)`             | 动态配置管理文件操作的安全路径白名单。                  |
| `SetDangerBypassEnabled(token, bool) error` | 在运行时越权关闭/开启 WAF 火墙（需验证 `AdminToken`）。 |

#### `Executor` (配置校验)

| 方法                            | 说明                               |
| :------------------------------ | :--------------------------------- |
| `ValidateConfig(*Config) error` | 执行前置安全审计和参数完整性校验。 |

---

## 4. 错误处理

HotPlex 导出了以下核心错误变量，用于业务逻辑判断：

- `hotplex.ErrDangerBlocked`: 用户的 Prompt 或智能体的操作触发了安全正则 WAF，被强制拦截。
- `hotplex.ErrInvalidConfig`: 传入的 `Config` 或 `EngineOptions` 参数校验不通过（如路径不存在）。

---

## 5. 安全与隔离

HotPlex 默认提供以下安全特性：

1. **指令级 WAF**：自动过滤高危指令（如 `rm -rf /`）。
2. **Danger Block 闭环**：交互式安全确认，包含用户批准工作流。
3. **进程组隔离**：确保智能体及其产生的任何子进程都能被强制清理。
4. **能力约束**：通过 `AllowedTools` 在语义层面限制智能体的能力。
5. **签名验证**：内置 Slack、飞书 Webhook 的 HMAC 验证。

---

## 6. 多平台 ChatApps

HotPlex 通过 `chatapps` 包提供原生平台适配器：

```go
import "github.com/hrygo/hotplex/chatapps"

// 创建 Slack 适配器
adapter := slack.NewAdapter(slack.Config{
    BotToken:   "xoxb-...",
    AppToken:   "xapp-...",
    SigningSecret: "...",
}, logger)

// 设置消息处理器
adapter.SetHandler(func(ctx context.Context, msg *base.ChatMessage) error {
    // 处理消息并生成响应
    return nil
})

// 启动适配器
ctx := context.Background()
adapter.Start(ctx)
```

### 平台特性对比

| 平台 | 关键特性 |
|------|----------|
| **Slack** | Block Kit UI、原生流式输出、Assistant Status、斜杠命令、Socket Mode |
| **飞书** | 卡片消息、交互回调 |

---

## 7. 进阶特性

### 7.1 多 Provider 支持

HotPlex 支持多种底座智能体：

- **Claude Code**: 极致的代码编辑和执行能力 (默认)
- **OpenCode**: 灵活的开源智能体支持

### 7.2 统计与观测

每次会话结束时，`session_stats` 事件会返回详细的 `SessionStatsData`：

- `TotalDurationMs`: 总耗时
- `InputTokens` / `OutputTokens`: Token 消耗
- `TotalCostUSD`: 实时估算的财务成本
- `FilesModified`: 修改的文件数量

### 7.3 配置热重载

使用 `ConfigLoader` 实现动态配置更新：

```go
loader, err := chatapps.NewConfigLoader("./configs", logger)
if err != nil {
    // 处理错误
}

// 热重载支持
loader.StartHotReload(ctx, "./configs", func(platform string, cfg *chatapps.PlatformConfig) {
    log.Printf("平台 %s 配置已重新加载", platform)
})
```

---

## 8. 最佳实践

1. **会话生命周期管理**：程序退出时务必调用 `engine.Close()` 以清理所有僵尸进程。
2. **Context 控制**：始终为 `Execute` 传递带有超时或取消信号的 `context.Context`，以防止无限等待。
3. **并发安全**：回调函数由流式协程触发，若在回调中操作外部资源，请注意并发安全。
4. **Namespace 隔离**：在多租户环境下，利用不同的 `Namespace` 确保环境隔离。
5. **错误处理**：妥善处理 `danger_block` 事件，为敏感操作实现用户批准流程。
6. **平台安全**：为所有基于 Webhook 的集成启用签名验证。

---

*更多详情请参考 `_examples/` 目录下的完整示例代码。另请参阅 [架构文档](architecture_zh.md) 和 [生产指南](production-guide_zh.md)。*
