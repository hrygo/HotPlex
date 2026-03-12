*Read this in other languages: [English](hooks-architecture.md), [简体中文](hooks-architecture_zh.md).*

# 事件钩子架构 (Event Hooks)

## 概述

事件钩子系统提供了一个基于插件的架构，用于对 HotPlex 中的事件做出反应。它支持：

-   **审计日志**：记录所有会话事件以进行合规性检查
-   **通知**：向 Slack、飞书发送告警
-   **Webhooks**：将事件转发到外部服务
-   **自定义逻辑**：实现对事件的自定义反应

## 架构

```
┌─────────────────────────────────────────────────────────────┐
│                      HotPlex Engine                          │
│  ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌─────────┐        │
│  │ Session │  │  Tool   │  │ Danger  │  │ Stream  │        │
│  │  Pool   │  │  Use    │  │   WAF   │  │  I/O    │        │
│  └────┬────┘  └────┬────┘  └────┬────┘  └────┬────┘        │
│       │            │            │            │              │
│       └────────────┴────────────┴────────────┘              │
│                          │                                   │
│                          ▼                                   │
│               ┌──────────────────┐                          │
│               │   Hook Manager    │                          │
│               │                  │                          │
│               │  ┌────────────┐  │                          │
│               │  │ Event Chan │  │                          │
│               │  │ (buffered) │  │                          │
│               │  └─────┬──────┘  │                          │
│               │        │         │                          │
│               │        ▼         │                          │
│               │  ┌────────────┐  │                          │
│               │  │ Event Loop │  │                          │
│               │  └─────┬──────┘  │                          │
│               └────────┼─────────┘                          │
│                        │                                     │
│ └──────────────────────┼─────────────────────────────────────┘
│                        │
│         ┌──────────────┼──────────────┐
│         │              │              │
│         ▼              ▼              ▼
│   ┌──────────┐  ┌──────────┐  ┌──────────┐
│   │ Webhook  │  │ Logging  │  │ Slack    │
│   │  Hook    │  │  Hook    │  │  Hook    │
│   └──────────┘  └──────────┘  └──────────┘
```

## 事件类型

| 事件             | 描述     | 触发时机                 |
| ---------------- | -------- | ------------------------ |
| `session.start`  | 会话创建 | 新的 CLI 进程启动        |
| `session.end`    | 会话终止 | 进程清理                 |
| `session.error`  | 会话错误 | 不可恢复的错误           |
| `tool.use`       | 工具调用 | 智能体使用 Bash, Edit 等 |
| `tool.result`    | 工具结果 | 工具执行完成             |
| `danger.blocked` | 安全拦截 | WAF 拦截了危险命令       |
| `stream.start`   | 流开始   | 接收到第一个 Token       |
| `stream.end`     | 流结束   | 轮次完成                 |
| `turn.start`     | 轮次开始 | 接收到用户提示词         |
| `turn.end`       | 轮次结束 | AI 响应完成              |

## 钩子接口 (Hook Interface)

```go
type Hook interface {
    Name() string
    Handle(ctx context.Context, event *Event) error
    Events() []EventType
}
```

## 使用方法

### 基础钩子注册

```go
import "github.com/hrygo/hotplex/hooks"

mgr := hooks.NewManager(logger, 1000)
defer mgr.Close()

loggingHook := hooks.NewLoggingHook("audit-log", logger, nil)
mgr.Register(loggingHook, hooks.HookConfig{
    Enabled: true,
    Async:   true,
})
```

### Webhook 钩子

```go
webhook := hooks.NewWebhookHook("slack-webhook", hooks.WebhookConfig{
    URL:     "https://hooks.slack.com/services/xxx",
    Secret:  "your-signing-secret",
    Timeout: 5 * time.Second,
    FilterEvents: []hooks.EventType{
        hooks.EventDangerBlocked,
        hooks.EventSessionError,
    },
}, logger)

mgr.Register(webhook, hooks.HookConfig{
    Enabled: true,
    Async:   true,
    Retry:   3,
})
```

### 自定义钩子

```go
type MetricsHook struct{}

func (h *MetricsHook) Name() string { return "metrics" }
func (h *MetricsHook) Events() []hooks.EventType {
    return []hooks.EventType{hooks.EventTurnEnd}
}
func (h *MetricsHook) Handle(ctx context.Context, event *hooks.Event) error {
    metrics.RecordTurn(event.SessionID)
    return nil
}
```

## 事件流

1.  **事件源** (Engine, Session, WAF) 创建 `Event`
2.  **Hook Manager** 通过 `Emit()` 或 `EmitSync()` 接收事件
3.  **Event Loop** 从缓冲通道处理事件
4.  **已注册的钩子** 根据匹配的事件类型被调用
5.  **异步钩子** 在 goroutine 中运行，不阻塞主流程
6.  **同步钩子** 阻塞直至完成 (用于关键事件)

## 配置参数

### HookConfig

| 字段      | 类型     | 默认值 | 描述               |
| --------- | -------- | ------ | ------------------ |
| `Enabled` | bool     | true   | 钩子是否激活       |
| `Async`   | bool     | false  | 是否异步运行       |
| `Timeout` | Duration | 5s     | 每个钩子的超时时间 |
| `Retry`   | int      | 0      | 失败后的重试次数   |

### Manager Options

| 选项         | 默认值         | 描述                 |
| ------------ | -------------- | -------------------- |
| `bufferSize` | 1000           | 事件通道缓冲大小     |
| `logger`     | slog.Default() | 钩子事件的日志记录器 |

## 线程安全

-   Hook Manager 通过 `sync.RWMutex` 保证线程安全
-   钩子可以在运行时动态注册/注销
-   事件发射是非阻塞的 (如果缓冲区满则丢弃)
-   每个钩子的执行是相互隔离的

## 错误处理

-   失败的钩子会被记录并带有重试尝试
-   错误不会传播给调用者 (Fire-and-forget)
-   重试使用指数退避策略：`100ms * attempt`

## 性能表现

-   事件发射：O(1) 通道发送
-   钩子查找：O(n)，其中 n 为对应事件类型的钩子数量
-   内存占用：每个事件约 200 字节
-   主执行路径无阻塞 (异步模式)

## 未来扩展

-   **持久化钩子**：在数据库中存储钩子配置
-   **钩子链**：支持有序的多个钩子链式调用
-   **频率限制**：针对每个钩子的速率限制
-   **熔断器**：自动禁用持续失败的钩子
