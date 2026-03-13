# State Management

## The Memory of Your Agents

Every intelligent agent needs memory. HotPlex provides a sophisticated state management system that ensures your agents remember context across interactions, survive restarts, and maintain consistency even in distributed environments.

---

## Session Lifecycle

A HotPlex session goes through well-defined lifecycle states:

![Session Lifecycle](/images/session-lifecycle.svg)

| Status | Description | Duration |
|--------|-------------|----------|
| `starting` | Process spawning, CLI initialization | ~1-2s |
| `ready` | Awaiting commands, persistent connection | Indefinite |
| `busy` | Actively processing requests | Variable |
| `dead` | Process terminated, cleanup in progress | Transient |

A HotPlex session goes through well-defined lifecycle states:

<div class="session-lifecycle">

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│  STARTING   │ ──► │    READY    │ ──► │    BUSY     │ ──► │    DEAD     │
└─────────────┘     └─────────────┘     └─────────────┘     └─────────────┘
       │                   │                   │                   │
       │                   │                   │                   │
  1-2 seconds        Idle waiting        Processing          Terminal/
                   for commands         requests           cleaned up
```

</div>

| Status | Description | Duration |
|--------|-------------|----------|
| `starting` | Process spawning, CLI initialization | ~1-2s |
| `ready` | Awaiting commands, persistent connection | Indefinite |
| `busy` | Actively processing requests | Variable |
| `dead` | Process terminated, cleanup in progress | Transient |

---

## Session Persistence

### How It Works

HotPlex uses a **marker-based persistence system** to track resumable sessions:

1. **Marker Creation**: When a session starts, a marker file is created in `~/.config/hotplex/sessions/`
2. **Session Resume**: On next request, HotPlex checks for existing markers and resumes if found
3. **Marker Cleanup**: When a session terminates, its marker is deleted

```go
// Default marker location
```
~/.config/hotplex/sessions/
├── session-abc123.lock    # Marker for "session-abc123"
├── session-def456.lock    # Marker for "session-def456"
└── ...
```

### Configuration

```go
opts := hotplex.EngineOptions{
    // Namespace ensures isolated session pools for different applications
    Namespace: "my-app-prod",  // Optional: prevents cross-app session leaks
    
    // Idle timeout: auto-cleanup after 30 minutes of inactivity
    IdleTimeout: 30 * time.Minute,
}
```

### Session ID Generation

Session IDs are generated deterministically using UUID v5:

```go
cfg := &hotplex.Config{
    // Same SessionID = same persistent session
    SessionID: "user-123-session",  // Auto-generated if empty
    
    // Persistent instructions across all turns
    TaskInstructions: "You are a Go code reviewer.",
}
```

> [!TIP]
> Using consistent `SessionID` values enables:
> - Context preservation across multiple requests
> - Session recovery after application restarts
> - Load balancing across multiple HotPlex instances

---

## Context Preservation

### The Continuity Problem

Traditional CLI tools start fresh every time:

```
User: "Refactor auth.go"
Claude: [Processes request, exits]

User: "Now add tests"  
Claude: [No memory of auth.go changes! 😞]
```

### HotPlex Solution

HotPlex maintains a **stateful session pool**:

```
User: "Refactor auth.go"
Claude: [Processes, maintains process]

User: "Now add tests"
Claude: [Has full context of auth.go changes! 😊]
```

### Implementation

```go
// Turn 1: Initialize session
cfg := &hotplex.Config{
    SessionID: "coding-assistant",
    WorkDir:   "/project",
}
engine.Execute(ctx, cfg, "Refactor auth.go to use JWT", callback)
// Session persists in background

// Turn 2: Reuse same session (context preserved)
engine.Execute(ctx, cfg, "Now add unit tests", callback)
// Agent remembers auth.go changes!
```

---

## Session Statistics

HotPlex tracks runtime metrics for each session:

```go
stats := engine.GetSessionStats("session-123")

// Access statistics
fmt.Printf("Token Usage: %d\n", stats.TokenUsage)
fmt.Printf("Turn Count: %d\n", stats.TurnCount)
fmt.Printf("Uptime: %s\n", stats.Uptime)
fmt.Printf("Last Activity: %s\n", stats.LastActivity)
```

### Available Metrics

| Metric | Description |
|--------|-------------|
| `TokenUsage` | Total tokens consumed by the session |
| `TurnCount` | Number of interaction cycles |
| `Uptime` | Time since session creation |
| `LastActivity` | Timestamp of last request |
| `ToolInvocations` | Count of tool calls made |

---

## Best Practices

### 1. Use Descriptive Session IDs

```go
// ✅ Good: Clear purpose
SessionID: "user-123-code-review"

// ❌ Bad: Random IDs lose meaning
SessionID: "sess-a1b2c3d4"
```

### 2. Set Appropriate Idle Timeouts

```go
// Development: Quick cleanup
IdleTimeout: 5 * time.Minute

// Production: Longer persistence
IdleTimeout: 30 * time.Minute
```

### 3. Leverage Persistent Instructions

```go
cfg := &hotplex.Config{
    TaskInstructions: `You are a senior Go engineer.
    - Always run tests after changes
    - Prefer stdlib over external deps
    - Add doc comments to exported functions`,
}
```

---

## Troubleshooting

### Session Won't Resume

**Symptom**: New session created instead of resuming existing one

**Causes**:
1. Marker file was deleted
2. Session process crashed (zombie)
3. WorkDir was modified externally

**Solutions**:
```go
// Check if session exists
exists := engine.HasSession("session-123")

// Force new session if needed
engine.TerminateSession("session-123")
```

### Context Lost

**Symptom**: Agent doesn't remember previous turns

**Solutions**:
1. Verify `SessionID` is consistent
2. Check session is still active (`GetSessionStats`)
3. Ensure `TaskInstructions` is set correctly

---

## Related Topics

- [Architecture Overview](/guide/architecture) - System design
- [API Reference](/reference/api) - Session management endpoints
- [Hooks System](/guide/hooks) - Event-driven state updates
