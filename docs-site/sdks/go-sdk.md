# Go SDK Mastery

## The High-Performance Control Plane

The **HotPlex Go SDK** is the definitive interface for orchestrating AI CLI agents with absolute precision. Designed for high-throughput, stateful environments, it provides the bridge between your Go services and the specialized cognitive capabilities of agents like Claude Code or OpenCode.

---

## Quick Start

To begin with the Go SDK, initialize a new `Engine` and establish a structured interaction loop.

### Installation

```bash
go get github.com/hrygo/hotplex
```

### Basic Usage

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"
    
    "github.com/hrygo/hotplex"
    "github.com/hrygo/hotplex/event"
)

func main() {
    // Initialize the Engine
    engine, err := hotplex.NewEngine(hotplex.EngineOptions{
        Timeout:     5 * time.Minute,
        IdleTimeout: 30 * time.Minute,
        Namespace:   "my-app",
    })
    if err != nil {
        log.Fatalf("Failed to create engine: %v", err)
    }
    defer engine.Close()

    // Configure the session
    cfg := &hotplex.Config{
        SessionID:        "my-session-001",
        WorkDir:          "/tmp/hotplex-sandbox",
        TaskInstructions: "You are a helpful coding assistant.",
    }

    // Execute with streaming callback
    ctx := context.Background()
    err = engine.Execute(ctx, cfg, "What is the current directory?", 
        func(ev *event.EventWithMeta) error {
            switch ev.Type {
            case "thinking":
                fmt.Printf("🤔 Reasoning: %s\n", ev.Data)
            case "answer":
                fmt.Printf("🤖 Answer: %s\n", ev.Data)
            case "tool_use":
                fmt.Printf("🔧 Tool: %s\n", ev.Meta.ToolName)
            }
            return nil
        })
    
    if err != nil {
        log.Printf("Execution error: %v", err)
    }
}
```

---

## Engine Options

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `Timeout` | `time.Duration` | Max time for single execution | 5 minutes |
| `IdleTimeout` | `time.Duration` | Auto-cleanup after inactivity | 30 minutes |
| `Namespace` | `string` | Session pool isolation | "default" |
| `Logger` | `*slog.Logger` | Custom logger | `slog.Default()` |
| `PermissionMode` | `string` | CLI permission level | strict |
| `AllowedTools` | `[]string` | Whitelist of allowed tools | all |
| `DisallowedTools` | `[]string` | Blacklist of forbidden tools | none |
| `AdminToken` | `string` | Token for security bypass | empty |
| `Provider` | `provider.Provider` | Custom CLI provider | Claude Code |

---

## Session Configuration

```go
cfg := &hotplex.Config{
    // Unique identifier for this session (use same ID for continuity)
    SessionID: "user-123-session",
    
    // Working directory (sandbox)
    WorkDir: "/project/sandbox",
    
    // Persistent instructions
    TaskInstructions: `You are a Go expert.
    - Always run tests after changes
    - Prefer stdlib over external packages`,
    
    // Optional: Base system prompt
    // BaseSystemPrompt: "Additional system context...",
}
```

---

## Security

### Tool Whitelist

```go
engine, _ := hotplex.NewEngine(hotplex.EngineOptions{
    AllowedTools: []string{"Bash", "Read", "Edit", "FileSearch", "Glob"},
})
```

### Custom Allowed Paths

```go
// Allow additional paths beyond WorkDir
engine.SetDangerAllowPaths([]string{
    "/project/sandbox/src",
    "/project/sandbox/tests",
})
```

### Bypass Mode (Development Only!)

> [!WARNING]
> Never use bypass mode in production!

```go
// Set admin token during initialization
engine, _ := hotplex.NewEngine(hotplex.EngineOptions{
    AdminToken: "dev-secret",
})

// Enable bypass (requires valid token)
err := engine.SetDangerBypassEnabled("dev-secret", true)
if err != nil {
    log.Printf("Bypass failed: %v", err)
}
```

---

## Real-time Telemetry

### Session Statistics

```go
stats, err := engine.GetSessionStats("my-session-001")
if err != nil {
    log.Printf("Session not found: %v", err)
    return
}

fmt.Printf("Token Usage: %d\n", stats.TokenUsage)
fmt.Printf("Turn Count: %d\n", stats.TurnCount)
fmt.Printf("Uptime: %s\n", stats.Uptime)
```

### Check Session Status

```go
// Check if session exists and is active
hasSession := engine.HasSession("my-session-001")
```

### Session Lifecycle Control

```go
// Terminate a session cleanly
err := engine.TerminateSession("my-session-001")
if err != nil {
    log.Printf("Termination failed: %v", err)
}
```

---

## Event Types

The callback receives these event types:

| Event | Description |
|-------|-------------|
| `thinking` | Agent reasoning process |
| `tool_use` | Tool invocation |
| `tool_result` | Tool execution result |
| `message` | Claude's response block |
| `error` | Error from engine or CLI |
| `done` | Execution complete |

---

## Complete Example

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"
    
    "github.com/hrygo/hotplex"
    "github.com/hrygo/hotplex/event"
)

func main() {
    // Create engine with custom config
    engine, err := hotplex.NewEngine(hotplex.EngineOptions{
        Timeout:     10 * time.Minute,
        IdleTimeout: 30 * time.Minute,
        Namespace:   "production",
        AllowedTools: []string{"Bash", "Read", "Edit", "FileSearch"},
    })
    if err != nil {
        log.Fatalf("Engine creation failed: %v", err)
    }
    defer engine.Close()

    // Session configuration
    cfg := &hotplex.Config{
        SessionID:        "production-session",
        WorkDir:          "/app/sandbox",
        TaskInstructions: "You are a senior Go engineer.",
    }

    // Multi-turn conversation
    ctx := context.Background()
    
    // Turn 1
    fmt.Println("=== Turn 1 ===")
    engine.Execute(ctx, cfg, "Create a simple HTTP server", callback)
    
    // Turn 2 (same session - context preserved)
    fmt.Println("=== Turn 2 ===")
    engine.Execute(ctx, cfg, "Add graceful shutdown", callback)
}

func callback(ev *event.EventWithMeta) error {
    switch ev.Type {
    case "thinking":
        fmt.Printf("🤔: %s\n", ev.Data)
    case "tool_use":
        fmt.Printf("🔧: Using %s\n", ev.Meta.ToolName)
    case "message":
        if content, ok := ev.Data.(string); ok {
            fmt.Printf("📝: %s\n", content)
        }
    case "error":
        fmt.Printf("❌: %v\n", ev.Data)
    }
    return nil
}
```

---

## Related Topics

- [API Reference](/reference/api) - Full API documentation
- [Protocol](/reference/protocol) - DMP protocol details
- [State Management](/guide/state) - Session persistence
- [Security](/guide/security) - WAF and isolation
