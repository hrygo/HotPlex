# HotPlex Worker Go Client

> Go client SDK for HotPlex Worker Gateway

[![Go Reference](https://pkg.go.dev/badge/github.com/hotplex/client.svg)](https://pkg.go.dev/github.com/hotplex/client)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

---

## Status

🚧 **Under Development** - API may change

---

## Installation

```bash
go get github.com/hotplex/client
```

---

## Quick Start

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    "github.com/hotplex/client"
)

func main() {
    ctx := context.Background()

    // Create client
    cfg := &client.Config{
        URL:        "ws://localhost:8888",
        WorkerType: client.WorkerTypeClaudeCode,
        AuthToken:  "your-api-key",
    }

    c := client.New(cfg)
    defer c.Close()

    // Register event handlers
    c.OnMessageDelta(func(data *client.MessageDeltaData) {
        fmt.Print(data.Content)
    })

    c.OnDone(func(data *client.DoneData) {
        fmt.Printf("\n✅ Done! Success: %v\n", data.Success)
        if data.Stats != nil {
            fmt.Printf("   Duration: %dms\n", data.Stats.DurationMs)
            fmt.Printf("   Tokens: %d\n", data.Stats.TotalTokens)
        }
    })

    c.OnError(func(data *client.ErrorData) {
        log.Printf("Error [%s]: %s", data.Code, data.Message)
    })

    // Connect
    if err := c.Connect(ctx); err != nil {
        log.Fatal("Connect failed:", err)
    }

    log.Printf("Connected! Session: %s", c.SessionID())

    // Send input
    if err := c.SendInput(ctx, &client.InputData{
        Content: "Write a hello world in Go",
    }); err != nil {
        log.Fatal("Send failed:", err)
    }

    // Wait for completion
    time.Sleep(30 * time.Second)
}
```

---

## API Reference

### Config

```go
type Config struct {
    URL                 string        // Gateway WebSocket URL (required)
    WorkerType          WorkerType    // Worker type (required)
    AuthToken           string        // API key or JWT (optional)
    SessionID           string        // Resume session (optional)
    Reconnect           bool          // Auto-reconnect (default: true)
    ReconnectMaxAttempts int          // Max reconnect attempts (default: 5)
    Timeout             time.Duration // Connection timeout (default: 30s)
}
```

### Client Methods

#### Constructor

```go
c := client.New(cfg)
```

#### Connection

```go
// Connect establishes WebSocket connection and initializes session
err := c.Connect(ctx)

// Close closes connection and cleanup resources
c.Close()

// SessionID returns current session ID
sessionID := c.SessionID()
```

#### Sending Messages

```go
// SendInput sends user input to worker
err := c.SendInput(ctx, &client.InputData{
    Content:  "Your input here",
    Metadata: map[string]any{"key": "value"}, // optional
})

// SendToolResult sends tool execution result
err := c.SendToolResult(ctx, &client.ToolResultData{
    ToolCallID: "call_123",
    Output:     "result",
    Error:      "", // optional
})

// SendPermissionResponse sends permission approval/denial
err := c.SendPermissionResponse(ctx, &client.PermissionResponseData{
    PermissionID: "perm_456",
    Allowed:      true,
    Reason:       "User approved", // optional
})
```

### Event Handlers

Register handlers using `On<Event>` methods:

```go
// Message streaming
c.OnMessageStart(func(data *client.MessageStartData) {
    fmt.Println("Message started:", data.ID)
})

c.OnMessageDelta(func(data *client.MessageDeltaData) {
    fmt.Print(data.Content)
})

c.OnMessageEnd(func(data *client.MessageEndData) {
    fmt.Println("Message ended:", data.ID)
})

// Tool calls
c.OnToolCall(func(data *client.ToolCallData) {
    result := executeTool(data.Name, data.Input)
    c.SendToolResult(ctx, &client.ToolResultData{
        ToolCallID: data.ID,
        Output:     result,
    })
})

// Permission requests
c.OnPermissionRequest(func(data *client.PermissionRequestData) {
    allowed := askUser(data.ToolName)
    c.SendPermissionResponse(ctx, &client.PermissionResponseData{
        PermissionID: data.ID,
        Allowed:      allowed,
    })
})

// State changes
c.OnState(func(data *client.StateData) {
    fmt.Println("State:", data.State)
})

// Completion
c.OnDone(func(data *client.DoneData) {
    fmt.Println("Done! Success:", data.Success)
})

// Errors
c.OnError(func(data *client.ErrorData) {
    log.Printf("Error [%s]: %s", data.Code, data.Message)
})

// Connection lifecycle
c.OnConnected(func() {
    fmt.Println("Connected")
})

c.OnDisconnected(func() {
    fmt.Println("Disconnected")
})

c.OnReconnecting(func(attempt, maxAttempts int) {
    fmt.Printf("Reconnecting %d/%d\n", attempt, maxAttempts)
})
```

---

## Data Types

### InputData

```go
type InputData struct {
    Content  string         `json:"content"`
    Metadata map[string]any `json:"metadata,omitempty"`
}
```

### MessageDeltaData

```go
type MessageDeltaData struct {
    Content string `json:"content"`
}
```

### ToolCallData

```go
type ToolCallData struct {
    ID    string         `json:"id"`
    Name  string         `json:"name"`
    Input map[string]any `json:"input"`
}
```

### DoneData

```go
type DoneData struct {
    Success bool      `json:"success"`
    Stats   *DoneStats `json:"stats,omitempty"`
}

type DoneStats struct {
    DurationMs  int     `json:"duration_ms"`
    TotalTokens int     `json:"total_tokens"`
    CostUsd     float64 `json:"cost_usd"`
}
```

### ErrorData

```go
type ErrorData struct {
    Code    string         `json:"code"`
    Message string         `json:"message"`
    Details map[string]any `json:"details,omitempty"`
}
```

---

## Examples

See [`examples/`](examples/) directory:

- [`basic/main.go`](examples/basic/main.go): Minimal example
- [`advanced/main.go`](examples/advanced/main.go): Full-featured demo

---

## Error Handling

### Error Types

```go
import (
    "errors"
    "github.com/hotplex/client"
)

err := c.Connect(ctx)

var connErr *client.ConnectionError
if errors.As(err, &connErr) {
    log.Printf("Connection failed: %v", connErr)
}

var sessionErr *client.SessionError
if errors.As(err, &sessionErr) {
    log.Printf("Session error: %v", sessionErr)
}
```

### Common Error Codes

| Code | Meaning | Action |
|------|---------|--------|
| `SESSION_NOT_FOUND` | Session doesn't exist | Create new session |
| `SESSION_TERMINATED` | Session terminated | Create new session |
| `UNAUTHORIZED` | Invalid auth token | Check token |
| `INVALID_INPUT` | Malformed input | Check message format |

---

## Testing

### Run Tests

```bash
go test ./... -v
```

### Integration Tests

```bash
# Start gateway
./hotplex-worker &

# Run integration tests
go test ./... -tags=integration -v
```

---

## Architecture

```
┌─────────────────────────────────────────┐
│         Client                          │
│  - Event handlers (On*)                 │
│  - Message builders (Send*)             │
│  - State management                     │
├─────────────────────────────────────────┤
│         Transport (WebSocket)           │
│  - Connection lifecycle                 │
│  - Auto-reconnect with backoff          │
│  - Message queue                        │
├─────────────────────────────────────────┤
│         Protocol (AEP v1)               │
│  - NDJSON codec                         │
│  - Envelope builder                     │
│  - Event type definitions               │
└─────────────────────────────────────────┘
```

**Source Structure**:
```
client/
├── client.go       # High-level client API
├── config.go       # Configuration types
├── transport.go    # WebSocket transport
├── protocol.go     # AEP codec
├── types.go        # Data types
└── errors.go       # Error types
```

---

## Development

### Prerequisites

- Go 1.21+
- HotPlex Worker Gateway

### Build

```bash
go build ./...
```

### Lint

```bash
golangci-lint run
```

---

## License

Apache-2.0

---

## Related

- **Protocol Spec**: `docs/architecture/AEP-v1-Protocol.md`
- **Python Client**: `examples/python-client/`
- **TypeScript Client**: `examples/typescript-client/`
- **Java Client**: `examples/java-client/`
