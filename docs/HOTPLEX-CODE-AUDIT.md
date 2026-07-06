# HotPlex Deep Code Audit

## Audit Scope

This audit maps the current implementation to the proposed hotplex 2.0 evolution.

## Executive Summary

HotPlex is not starting from a simple CLI wrapper. The current codebase already contains several Agent Runtime primitives:

- Gateway control plane
- Worker abstraction
- Session lifecycle management
- Event protocol
- Multi-provider adapters
- Security controls
- Observability
- Cron scheduling

The correct evolution path is incremental: extend existing abstractions instead of introducing a separate Agent OS architecture.

---

# Current Architecture

```
Client(Web/Slack/Feishu/SDK)
          |
      AEP WebSocket
          |
      Gateway
          |
   Session Manager
          |
   Worker Adapter Layer
          |
 Claude / OpenCode / Codex / ACP
```

---

# Existing Assets

## 1. Worker Runtime Abstraction

Current foundation:

```
Worker interface
 |
 +-- Start
 +-- Input
 +-- Resume
 +-- Terminate
 +-- Kill
 +-- Health
```

The Worker abstraction is already the correct extension point for provider independence.

2.0 direction:

```
Worker
  |
Runtime Provider SDK
  |
Agent Runtime
```

Do not replace Worker.

---

## 2. Session Lifecycle

Current capabilities:

- deterministic session IDs
- reconnect
- resume
- lifecycle state machine
- persistence

Current state model:

```
CREATED
  |
RUNNING
  |
IDLE
  |
TERMINATED
  |
DELETED
```

2.0 evolution:

Add:

- execution queue
- task ownership
- agent identity binding

Avoid creating a new scheduler before extending session management.

---

## 3. Event System

Existing AEP events already provide the foundation for Agent Observability.

Evolution:

Current:

```
Session Events
```

Future:

```
Agent Events
Runtime Events
Security Events
Audit Events
```

---

## 4. Security Layer

Existing capabilities:

- API authentication
- SSRF protection
- tool allowlist
- environment isolation
- path protection

Evolution:

```
Security Manager
        |
Policy Engine
        |
Agent Permission Model
```

---

# Gap Analysis

| Capability | Current | Required Change |
|---|---|---|
| Provider abstraction | Exists | Extend SDK |
| Session runtime | Exists | Add task model |
| Event protocol | Exists | Add agent-level events |
| Observability | Exists | Add agent traces |
| Identity | Partial | Introduce Agent Identity |
| Scheduler | Cron only | Add execution scheduler |
| Memory | Worker-owned | Add abstraction layer |
| Manifest | Config based | Add AgentSpec |

---

# Revised hotplex 2.0 Strategy

## Phase 1: Runtime Evolution

Priority:

1. AgentSpec built on existing config
2. Agent Identity attached to sessions
3. AEP event extension
4. Runtime tracing

## Phase 2: Control Plane

Priority:

1. Task queue
2. Worker scheduling
3. Policy engine

## Phase 3: Agent OS

Only after runtime foundations:

- multi-agent workflow
- distributed scheduling
- cloud runtime

---

# Changes Required to Previous Roadmap

Previous roadmap items should be treated as vision documents.

Implementation issues should be rewritten around existing modules:

Instead of:

```
create Agent OS Scheduler
```

Use:

```
extend session execution queue
```

Instead of:

```
create Memory Service
```

Use:

```
introduce runtime context persistence interface
```

---

# Final Assessment

HotPlex already has approximately 60-70% of the foundation required for an Agent Runtime platform.

The main opportunity is not rebuilding architecture.

The opportunity is turning existing Gateway + Worker + Session + Event primitives into a stable Agent Runtime Kernel.
