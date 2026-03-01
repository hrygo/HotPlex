# Go SDK Mastery

## The High-Performance Control Plane

The **HotPlex Go SDK** is the definitive interface for orchestrating AI CLI agents with absolute precision. Designed for high-throughput, stateful environments, it provides the bridge between your Go services and the specialized cognitive capabilities of agents like Claude Code or OpenCode.

---

### 🎨 The Architecture of Command

Unlike simple wrappers, the Go SDK is structured around three core pillars of agency:

1.  **Executor**: Handles the core execution logic and real-time event normalization.
2.  **SessionController**: Provides sovereign control over long-lived process groups and telemetry.
3.  **SafetyManager**: Enforces deterministic security boundaries and WAF policies.

---

### 🚀 Rapid Integration

To begin your journey with the Go SDK, initialize a new `Engine` and established a structured interaction loop.

```go
package main

import (
	"context"
	"fmt"
	"github.com/hrygo/hotplex"
	"github.com/hrygo/hotplex/event"
)

func main() {
	// Initialize the high-performance Engine
	ctx := context.Background()
	client := hotplex.NewEngine(hotplex.EngineOptions{
		Port: 8080,
		LogLevel: "info",
	})
	defer client.Close()

	// Configure the execution context
	cfg := &hotplex.Config{
		SessionID: "artisanal-session-001",
		WorkDir:   "/go/src/my-project",
	}

	// Execute with structured streaming
	err := client.Execute(ctx, cfg, "Refactor the authentication middleware.", func(ev *event.EventWithMeta) {
		switch ev.Type {
		case "thinking":
			fmt.Printf("🧠 Agent Reasoning: %s\n", ev.Data)
		case "answer":
			fmt.Print(ev.Data)
		case "tool_use":
			fmt.Printf("\n🛠 Tool Invoked: %s\n", ev.Meta.ToolName)
		}
	})

	if err != nil {
		panic(err)
	}
}
```

---

### 🛡️ Sovereign Safety

Security is not an afterthought; it is a core constraint. Use the `SafetyManager` to define the "Dangerous zone" for your agents.

```go
// Define deterministic I/O boundaries
client.SetDangerAllowPaths([]string{
    "/go/src/my-project/pkg",
    "/go/src/my-project/internal",
})

// Toggle WAF bypass for administrative pulses
err := client.SetDangerBypassEnabled("ADMIN_SECRET_TOKEN", true)
```

---

### 📊 Real-time Telemetry

Extract the pulse of your sessions via the `SessionController`.

```go
stats := client.GetSessionStats("artisanal-session-001")
fmt.Printf("Token Consumption: %d | Process Uptime: %s\n", 
    stats.TokenUsage, stats.Uptime)
```

[Explore the API Reference](/reference/api) or [Master the Protocol](/reference/protocol)
