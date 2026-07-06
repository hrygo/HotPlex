# HotPlex 2.0 Implementation Roadmap

## Principle

HotPlex 2.0 is an incremental evolution of the existing Gateway + Worker + Session + Event architecture.

It is not a rewrite into a new Agent OS.

```
Existing Runtime Kernel
        |
        v
Agent Runtime Platform
        |
        v
Agent Operating System
```

---

# Phase 1: Runtime Kernel Evolution

## AgentSpec

Build on existing configuration models.

Goals:

- formalize agent configuration
- provider selection
- permission configuration
- runtime metadata

Do not replace existing worker configuration.

---

## Session Context Extension

Extend existing Session lifecycle:

Current:

```
CREATED
RUNNING
IDLE
TERMINATED
DELETED
```

Add:

- task context
- execution metadata
- agent identity binding

---

## AEP Event Extension

Extend existing event protocol:

Current:

```
Session events
```

Add:

```
Agent events
Runtime events
Security events
Audit events
```

---

# Phase 2: Runtime Control Plane

## Execution Queue

First scheduling layer:

```
Session
  |
Execution Queue
  |
Worker
```

Capabilities:

- ordered execution
- retry
- timeout
- resource tracking

---

## Policy Engine

Extend current security controls:

```
Security Manager
        |
Policy Engine
        |
Agent Permissions
```

---

# Phase 3: Agent Platform

Only after runtime stability:

- multi-agent workflows
- distributed scheduling
- external memory providers
- enterprise deployment

---

# Engineering Rules

1. Extend Worker abstraction, do not replace it.
2. Extend Session model, do not bypass it.
3. Extend AEP events, do not introduce a parallel event system.
4. Preserve provider neutrality.
5. Prefer small PRs over architecture rewrites.
