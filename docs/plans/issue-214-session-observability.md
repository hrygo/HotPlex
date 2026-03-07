## 背景

当前 HotPlex 的 Session 和可观测性存在以下问题：

1. **Session 语义缺失**：`Session` 结构体仅有 `ID`、`Status`，缺乏业务上下文，排障时无法快速定位问题
2. **日志关联困难**：各模块日志分散，缺乏统一的 TraceID 串联
3. **指标维度不足**：现有 Prometheus 指标缺乏任务类型、平台等维度
4. **与 Native Brain 断联**：Brain 模块无法获取 Session 语义信息进行智能决策

## 目标

1. 为 Session 注入完整语义信息，支持快速排障和监控
2. 增强可观测性，实现全链路追踪
3. 与 Native Brain 深度集成，赋能智能路由和上下文管理

## 技术方案

### 1. SessionContext 语义增强

```go
// internal/engine/session.go

type SessionContext struct {
    // 来源信息
    Platform      string    // slack/feishu/discord/telegram
    UserID        string    // 用户标识
    ChannelID     string    // 来源频道/群组
    TeamID        string    // 团队/工作空间标识

    // 任务信息
    TaskType      TaskType  // code/chat/analysis/debug/git
    PromptSummary string    // 首条 prompt 摘要 (前 50 字符)

    // 追踪信息
    TraceID       string    // OpenTelemetry TraceID
    ParentSpanID  string    // 父 SpanID (可选)

    // 时间信息
    CreatedAt     time.Time
    LastActiveAt  time.Time

    // 状态信息
    TurnCount     int       // 对话轮次
    TokenUsed     int64     // 已消耗 Token
    ErrorCount    int       // 错误次数
    LastError     string    // 最后一次错误摘要
}

type TaskType string

const (
    TaskTypeCode     TaskType = "code"      // 代码相关
    TaskTypeChat     TaskType = "chat"      // 普通对话
    TaskTypeAnalysis TaskType = "analysis"  // 分析任务
    TaskTypeDebug    TaskType = "debug"     // 调试任务
    TaskTypeGit      TaskType = "git"       // Git 操作
    TaskTypeUnknown  TaskType = "unknown"   // 未知类型
)
```

### 2. 日志增强

**2.1 结构化日志字段**

所有 Session 相关日志自动注入语义字段：

```json
{
  "level": "info",
  "msg": "session started",
  "session_id": "abc123",
  "platform": "slack",
  "user_id": "U12345",
  "channel_id": "C67890",
  "task_type": "code",
  "trace_id": "4bf92f3577b34da6",
  "prompt_summary": "帮我分析 pool.go 的并发安全问题..."
}
```

**2.2 日志级别规范**

| 级别 | 场景 |
|------|------|
| DEBUG | 详细调试 (I/O 内容、内部状态) |
| INFO | 正常运行 (Session 创建、销毁、任务完成) |
| WARN | 可恢复异常 (超时重试、降级处理) |
| ERROR | 执行失败 (进程崩溃、WAF 拦截) |

### 3. Prometheus 指标增强

**3.1 新增指标**

| 指标名 | 类型 | 标签 | 描述 |
|--------|------|------|------|
| `hotplex_session_duration_seconds` | histogram | platform, task_type | Session 生命周期时长 |
| `hotplex_session_turns_total` | counter | platform, task_type | 对话轮次统计 |
| `hotplex_session_errors_by_type` | counter | platform, error_type | 按错误类型统计 |
| `hotplex_tokens_by_task` | counter | platform, task_type, direction | 按任务类型统计 Token 消耗 |
| `hotplex_brain_requests_total` | counter | feature, model | Native Brain 调用统计 |

**3.2 指标维度**

```go
// 按平台维度
metrics.SessionDuration.WithLabelValues("slack", "code").Observe(duration)
metrics.SessionDuration.WithLabelValues("feishu", "chat").Observe(duration)

// 按任务类型维度
metrics.TokensByTask.WithLabelValues("slack", "code", "input").Add(tokens)
```

### 4. 与 Native Brain 集成

**4.1 智能路由决策**

```go
// brain/router.go

func (r *Router) SelectEngine(ctx context.Context, sessCtx *SessionContext) string {
    // 基于 TaskType 选择最优 Provider
    switch sessCtx.TaskType {
    case TaskTypeCode:
        return r.config.CodeEngineID
    case TaskTypeChat:
        return r.config.ChatEngineID
    default:
        return r.config.DefaultEngineID
    }
}
```

**4.2 上下文压缩触发**

```go
// brain/memory.go

func (m *Memory) CheckCompressionNeeded(sessCtx *SessionContext) bool {
    // 当 Token 接近阈值时触发压缩
    return sessCtx.TokenUsed > m.config.CompressionThreshold
}

func (m *Memory) Compress(ctx context.Context, sessionID string) error {
    // 调用 Native Brain 进行摘要压缩
    summary, err := m.brain.Summarize(ctx, sessionID)
    if err != nil {
        return err
    }
    // 持久化摘要并清理历史
    return m.store.SaveSummary(sessionID, summary)
}
```

**4.3 视觉推理透视**

```go
// brain/visual.go

func (v *Visualizer) TranslateEvent(evt *ProviderEvent, sessCtx *SessionContext) string {
    switch evt.Type {
    case "tool_use":
        return fmt.Sprintf("正在执行 %s...", evt.ToolName)
    case "thinking":
        return "正在思考..."
    default:
        return ""
    }
}
```

### 5. 文件结构

```
internal/engine/
├── session.go          # 添加 SessionContext
├── context.go          # [NEW] SessionContext 定义
└── pool.go             # 传递上下文

engine/
├── runner.go           # 填充 SessionContext
└── telemetry.go        # [NEW] 指标采集增强

brain/
├── router.go           # 集成 SessionContext
├── memory.go           # 压缩触发逻辑
└── visual.go           # [NEW] 视觉推理翻译

chatapps/*/
└── adapter.go          # 填充平台信息
```

## 价值

### 排障效率提升
- **Before**: "session-xxx 死了，不知道什么问题"
- **After**: "slack 用户 U12345 的代码任务超时，已执行 12 轮，消耗 15K tokens"

### 监控 SLA 支持
- 按平台统计成功率、延迟
- 按任务类型统计 Token 消耗
- 支持 SLI/SLO 告警规则

### Native Brain 赋能
- 智能路由：基于 TaskType 选择最优 Provider
- 成本优化：识别高频闲聊任务，降级到轻量模型
- 上下文管理：自动触发压缩释放 Token 空间

## 范围

- `internal/engine/session.go` - 添加 SessionContext
- `internal/engine/context.go` - 新增 SessionContext 定义
- `engine/runner.go` - 传递上下文到各层
- `internal/telemetry/` - 指标增强
- `brain/` - 集成 SessionContext 进行智能决策
- `chatapps/*/` - 填充平台相关语义信息

## 成功标准

1. Session 日志包含完整语义字段 (platform, user_id, task_type, trace_id)
2. Prometheus 指标支持 platform 和 task_type 维度
3. 支持 `--session-id` 和 `--trace-id` 日志过滤
4. Native Brain 能基于 SessionContext 进行智能路由
5. 当 Token 超过阈值时自动触发上下文压缩
