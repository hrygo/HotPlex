# Hooks System

## Customizing the Agent Lifecycle

The HotPlex architecture is built on the principle of **Inversion of Control**. Instead of a monolithic engine with fixed behaviors, we provide a powerful **Hooks System** that allows you to inject custom logic at every critical stage of the agent's execution through **Go interfaces**.

---

### Event Types

HotPlex hooks are triggered by the following event types:

| Event | Description |
| :---- | :---------- |
| `session.start` | Fired when a new session is created |
| `session.end` | Fired when a session terminates normally |
| `session.error` | Fired when a session encounters an error |
| `tool.use` | Fired when the agent is about to use a tool |
| `tool.result` | Fired after a tool returns a result |
| `danger.blocked` | Fired when WAF blocks dangerous input |
| `stream.start` | Fired when streaming response begins |
| `stream.end` | Fired when streaming response completes |
| `turn.start` | Fired when a new conversation turn begins |
| `turn.end` | Fired when a conversation turn ends |

---

### Implementing a Hook

Hooks are implemented as **Go interfaces**. Create a struct that implements the `Hook` interface:

#### 1. Define Your Hook

```go
package main

import (
    "context"
    "log"
    "github.com/hrygo/hotplex/hooks"
)

type LoggingHook struct{}

func (h *LoggingHook) Name() string {
    return "logging-hook"
}

func (h *LoggingHook) Events() []hooks.EventType {
    return []hooks.EventType{
        hooks.EventSessionStart,
        hooks.EventSessionEnd,
        hooks.EventToolUse,
        hooks.EventToolResult,
    }
}

func (h *LoggingHook) Handle(ctx context.Context, event *hooks.Event) error {
    log.Printf("[%s] Session: %s, Type: %s",
        event.Timestamp.Format("2006-01-02 15:04:05"),
        event.SessionID,
        event.Type)
    return nil
}
```

#### 2. Register with Engine

```go
import "github.com/hrygo/hotplex"

engine := hotplex.NewEngine(hotplex.EngineOptions{
    // ... other options
})

// Register your hook
engine.RegisterHook(&LoggingHook{})
```

---

### Hook Interface Reference

```go
type Hook interface {
    // Name returns the unique identifier for this hook
    Name() string

    // Handle processes the event
    // Return nil to continue normal execution
    // Return error to signal failure (may interrupt flow)
    Handle(ctx context.Context, event *Event) error

    // Events returns the list of event types this hook subscribes to
    Events() []EventType
}
```

#### Event Structure

```go
type Event struct {
    Type      EventType   // Event identifier (e.g., "session.start")
    Timestamp time.Time   // When the event occurred
    Namespace string      // Optional namespace for the event
    SessionID string      // The session this event belongs to
    Data      interface{} // Event-specific data payload
    Error     string      // Error message if eventType is "session.error"
}
```

---

### Webhook Integration

If you need to integrate with external HTTP services, use the built-in webhook adapter:

```go
import "github.com/hrygo/hotplex/hooks"

webhookHook := hooks.NewWebhookHook(hooks.WebhookConfig{
    URL:    "https://api.example.com/hook",
    Events: []hooks.EventType{hooks.EventSessionEnd},
    Async:  true,
    Timeout: 10 * time.Second,
})
```

---

### Example: Security Hook

```go
type SecurityHook struct{}

func (h *SecurityHook) Name() string { return "security" }
func (h *SecurityHook) Events() []hooks.EventType {
    return []hooks.EventType{hooks.EventToolUse}
}

func (h *SecurityHook) Handle(ctx context.Context, event *hooks.Event) error {
    // Inspect tool use, block if needed
    toolCall, ok := event.Data.(string)
    if !ok {
        return nil
    }

    blocked := []string{"rm -rf", "delete database", "drop table"}
    for _, cmd := range blocked {
        if strings.Contains(toolCall, cmd) {
            log.Printf("Blocked dangerous tool: %s", toolCall)
            return errors.New("tool call blocked by security policy")
        }
    }
    return nil
}
```

---

> [!TIP]
> For more examples, check out the [SDKs](/sdks/go-sdk) documentation for high-level hook abstractions.
