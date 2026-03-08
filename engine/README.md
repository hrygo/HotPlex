# HotPlex Engine: The Control Plane

The `engine` package is the core orchestration layer of HotPlex. It transforms local AI CLI agents (like Claude Code) into high-availability, production-ready services by managing process lifecycles, security boundaries, and real-time event streaming.

## 🏛 Architecture Overview

The Engine operates as a **Stateful Multi-Session Controller**. It manages a pool of persistent CLI processes to eliminate the overhead of repeated cold starts.

![Agent Architecture](../docs/images/agent-architecture.svg)

### Key Architectural Concepts

-   **Hot-Multiplexing**: Instead of spawning a new process for every user message, the Engine keeps CLI processes alive in a "Busy/Ready" state. Subsequent turns are piped directly via `stdin`, reducing latency by 90%+.
-   **Session Isolation**: Each user session is bound to a unique working directory and a dedicated OS process group (PGID), ensuring that file operations and recursive tool executions are strictly sandboxed.
-   **Regex Firewall (WAF)**: A pre-execution security layer that scans user prompts for dangerous patterns (e.g., recursive `rm -rf /`, credential exfiltration) before they reach the AI agent.
-   **Event Bridge**: Translates raw CLI stdout (often inconsistent JSON lines) into a strictly typed, normalized event stream.

---

## 🛠 Developer Guide

### 1. Initializing the Engine

The Engine is typically initialized as a singleton.

```go
opts := engine.EngineOptions{
    Provider:         claudeProvider, // e.g. provider.NewClaudeCodeProvider
    Namespace:        "prod",
    Timeout:          10 * time.Minute,
    IdleTimeout:      30 * time.Minute,
    AllowedTools:     []string{"ls", "cat", "grep"},
    PermissionMode:  "auto", // auto-approve non-destructive tools
}

eng, err := engine.NewEngine(opts)
```

### 2. Executing a Task

To run a task, you provide a `context`, a session `Config`, the `prompt`, and a `callback` to receive real-time updates.

```go
cfg := &types.Config{
    SessionID: "user-123-abc",
    WorkDir:   "/tmp/workspace/user-123",
}

err := eng.Execute(ctx, cfg, "Analyze this project structure", func(eventType string, data any) error {
    switch eventType {
    case "thinking":
        fmt.Printf("AI is thinking: %v\n", data)
    case "answer":
        fmt.Printf("Token: %v\n", data)
    case "session_stats":
        stats := data.(*event.SessionStatsData)
        fmt.Printf("Task completed. Tokens used: %d\n", stats.TotalTokens)
    }
    return nil
})
```

### 3. Session Lifecycle Management

The Engine provides several methods to control active sessions:

-   **`StopSession(sessionID, reason)`**: Gracefully terminates a session and kills the underlying process group.
-   **`ResetSessionProvider(sessionID)`**: Signals the engine to start a fresh AI context (clears conversation history) on the next execution.
-   **`GetSessionStats(sessionID)`**: Retrieves real-time metrics (tokens used, tools called, duration breakdown).

---

## 🛡 Security Boundaries

The Engine enforces three layers of security:

1.  **Input Filtering**: Regex-based detection of malicious prompts.
2.  **Runtime Constraints**: `AllowedTools` and `DisallowedTools` whitelist/blacklist.
3.  **OS Isolation**: Each session runs in its own directory. Access outside `WorkDir` can be restricted via provider-specific flags.

---

## 📊 Observability

The `SessionStats` object provides high-fidelity tracking of AI performance:

-   **Duration Breakdown**: Thinking time vs. Tool execution time vs. Text generation time.
-   **Token Accounting**: Input, output, and cache (Read/Write) tokens.
-   **Tool Audit**: A complete list of all local tools successfully invoked during the session.

---

**Package Path**: `github.com/hrygo/hotplex/engine`  
**Core Components**: `Engine`, `SessionManager`, `Provider`
