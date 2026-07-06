# hotplex 2.0 API Design

## Design Goal

Provide a stable runtime API for autonomous AI agents.

The API should abstract agent providers while exposing lifecycle, execution and orchestration capabilities.

---

# Agent Runtime API

Conceptual Go interface:

```go
type AgentRuntime interface {
    CreateAgent(spec AgentSpec) AgentID
    Execute(agent AgentID, task Task) Stream
    Stop(agent AgentID) error
    Status(agent AgentID) AgentStatus
}
```

---

# Agent Specification

```go
type AgentSpec struct {
    Name string
    Provider string
    Sandbox SandboxSpec
    Policy PolicySpec
    Budget BudgetSpec
}
```

Example:

```yaml
agent:
  name: coder

runtime:
  provider: claude-code

sandbox:
  mode: container

budget:
  max_tokens: 100000
```

---

# Scheduler API

```go
type Scheduler interface {
    Submit(task Task) TaskID
    Cancel(task TaskID) error
    Assign(task TaskID, agent AgentID) error
}
```

Responsibilities:

- queue management
- priority scheduling
- retry handling
- resource allocation

---

# Memory API

```go
type Memory interface {
    Store(item MemoryItem) error
    Recall(query string) []MemoryItem
    Delete(id string) error
}
```

Memory layers:

- session memory
- project memory
- skill memory
- knowledge memory

---

# Event API

All runtime behavior emits events.

```go
type AgentEvent struct {
    Type string
    AgentID string
    Timestamp int64
    Data any
}
```

Events:

```
agent.created
agent.started
agent.tool.called
agent.file.changed
agent.failed
agent.completed
```

---

# Provider Interface

```go
type Provider interface {
    Start(ctx Context) Runtime
    Execute(task Task) Stream
    Stop() error
}
```

Providers:

- Claude
- OpenCode
- Codex
- Aider
- Local models

---

# Security API

```go
type PolicyEngine interface {
    Check(action Action) Decision
}
```

Controls:

- tool access
- filesystem access
- network access
- execution budget

---

# Future API Direction

hotplex should expose an Agent Runtime Protocol similar to:

```
HTTP API
WebSocket Streaming
SDK
CLI
Kubernetes Operator
```

The final goal is a universal execution interface for AI agents.
