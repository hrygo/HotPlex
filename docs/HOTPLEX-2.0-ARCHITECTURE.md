# hotplex 2.0 Architecture

## Target Architecture

```
Applications
     |
Agent Gateway
     |
+-----------------------------+
| hotplex Control Plane        |
|                             |
| Identity                    |
| Scheduler                   |
| Policy Engine               |
| Memory Service              |
| Event Bus                   |
| Observability               |
+-----------------------------+
     |
Agent Execution Runtime
     |
+-----------------------------+
| Claude | OpenCode | Codex     |
| Local Models | Custom Agents |
+-----------------------------+
     |
Sandbox Layer
     |
Process / Container / MicroVM / WASM
```

## Core Components

### Agent Registry

Stores:

- agent metadata
- versions
- capabilities
- ownership

### Identity Service

Provides:

- authentication
- authorization
- execution identity

### Scheduler

Responsible for:

- dispatch
- queue management
- resource allocation

### Policy Engine

Controls:

- tool permissions
- filesystem access
- network rules
- execution budgets

### Memory Service

Provides persistent context independent from a single runtime.

### Event Bus

All runtime actions become observable events.

Examples:

- lifecycle events
- tool calls
- file changes
- security decisions

### Observability

Trace model:

```
User Intent
    |
Agent Reasoning
    |
Tool Execution
    |
Environment Change
    |
Final Result
```

## Design Principles

1. Runtime first
2. Provider independent
3. Secure by default
4. Observable execution
5. Cloud native lifecycle

## Long Term Goal

hotplex becomes the execution kernel for autonomous AI agents.
