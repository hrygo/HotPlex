*Read this in other languages: [English](hooks-architecture.md), [简体中文](hooks-architecture_zh.md).*

# Event Hooks Architecture

## Overview

The Event Hooks system provides a plugin-based architecture for reacting to events in HotPlex. It enables:

- **Audit Logging**: Record all session events for compliance
- **Notifications**: Send alerts to Slack and Feishu
- **Webhooks**: Forward events to external services
- **Custom Logic**: Implement custom reactions to events

## Architecture

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
└────────────────────────┼─────────────────────────────────────┘
                         │
          ┌──────────────┼──────────────┐
          │              │              │
          ▼              ▼              ▼
    ┌──────────┐  ┌──────────┐  ┌──────────┐
    │ Webhook  │  │ Logging  │  │ Slack    │
    │  Hook    │  │  Hook    │  │  Hook    │
    └──────────┘  └──────────┘  └──────────┘
```

## Event Types

| Event            | Description        | When Fired                    |
| ---------------- | ------------------ | ----------------------------- |
| `session.start`  | Session created    | New CLI process started       |
| `session.end`    | Session terminated | Process cleanup               |
| `session.error`  | Session error      | Unrecoverable error           |
| `tool.use`       | Tool invoked       | Agent uses Bash, Edit, etc.   |
| `tool.result`    | Tool completed     | Tool execution finished       |
| `danger.blocked` | Security block     | WAF blocked dangerous command |
| `stream.start`   | Stream begun       | First token received          |
| `stream.end`     | Stream finished    | Turn completed                |
| `turn.start`     | Turn begun         | User prompt received          |
| `turn.end`       | Turn completed     | AI response finished          |

## Hook Interface

```go
type Hook interface {
    Name() string
    Handle(ctx context.Context, event *Event) error
    Events() []EventType
}
```

## Usage

### Basic Hook Registration

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

### Webhook Hook

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

### Custom Hook

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

## Event Flow

1. **Event Source** (Engine, Session, WAF) creates an `Event`
2. **Hook Manager** receives event via `Emit()` or `EmitSync()`
3. **Event Loop** processes events from buffered channel
4. **Registered Hooks** are invoked for matching event types
5. **Async Hooks** run in goroutines, don't block main flow
6. **Sync Hooks** block until completion (for critical events)

## Configuration

### HookConfig

| Field     | Type     | Default | Description               |
| --------- | -------- | ------- | ------------------------- |
| `Enabled` | bool     | true    | Whether hook is active    |
| `Async`   | bool     | false   | Run in goroutine          |
| `Timeout` | Duration | 5s      | Per-hook timeout          |
| `Retry`   | int      | 0       | Retry attempts on failure |

### Manager Options

| Option       | Default        | Description               |
| ------------ | -------------- | ------------------------- |
| `bufferSize` | 1000           | Event channel buffer size |
| `logger`     | slog.Default() | Logger for hook events    |

## Thread Safety

- Hook Manager is thread-safe via `sync.RWMutex`
- Hooks can be registered/unregistered at runtime
- Event emission is non-blocking (drops if buffer full)
- Each hook execution is isolated

## Error Handling

- Failed hooks are logged with retry attempts
- Errors don't propagate to caller (fire-and-forget)
- Retry uses exponential backoff: `100ms * attempt`

## Performance

- Event emission: O(1) channel send
- Hook lookup: O(n) where n = hooks for event type
- Memory: ~200 bytes per event
- No blocking on main execution path (async mode)

## Future Extensions

- **Persistent Hooks**: Store hook configs in database
- **Hook Chaining**: Chain multiple hooks with ordering
- **Rate Limiting**: Per-hook rate limits
- **Circuit Breaker**: Disable failing hooks automatically
