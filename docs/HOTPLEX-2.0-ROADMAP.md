# hotplex 2.0 Roadmap

## From AI Agent Runtime to Agent Operating System

## Vision

hotplex 2.0 evolves from an AI CLI agent runtime into an Agent Operating System execution layer.

> Build the infrastructure where autonomous AI agents live, work, execute and collaborate.

## Current Foundation

hotplex already provides:

- Persistent agent sessions
- Provider abstraction
- Full duplex streaming
- Process lifecycle management
- Security controls
- Remote execution gateway

## Evolution Path

```
hotplex 1.x
Agent Runtime

        ↓

hotplex 2.x
Agent Operating System

        ↓

hotplex 3.x
Agent Cloud Infrastructure
```

## Phase 1: Runtime 2.0 (2026)

### Agent Manifest

Introduce declarative agent configuration:

```yaml
agent:
  name: coding-agent
runtime:
  provider: claude-code
sandbox:
  mode: isolated
policy:
  allowed_tools:
    - git
    - shell
```

### Agent Identity

Add:

- identity
- permissions
- quota
- ownership
- audit context

### Event Driven Runtime

Standardize events:

- agent.created
- agent.started
- agent.tool.called
- agent.failed
- agent.completed

### Agent Observability

Integrate OpenTelemetry style tracing:

```
Request
  |
Planning
  |
Tool Call
  |
Execution
  |
Result
```

## Phase 2: Agent OS (2027)

### Scheduler

Kubernetes-like scheduling for agents:

- task queue
- priority
- retry
- resource limits

### Memory Service

Unified memory layer:

- short memory
- conversation memory
- project memory
- skill memory
- knowledge memory

### Multi Agent Collaboration

Support:

```
Manager Agent
 |
 +-- Coding Agent
 +-- Testing Agent
 +-- Research Agent
 +-- DevOps Agent
```

## Phase 3: Enterprise Agent Cloud (2027-2028)

Capabilities:

- multi tenancy
- RBAC
- compliance audit
- private deployment
- agent marketplace

## Strategic Position

Avoid becoming a single AI coding assistant wrapper.

Become:

```
Docker       -> Containers
Kubernetes   -> Container orchestration
hotplex      -> Agent orchestration
```

## Success Metrics

2026:

- 1000 concurrent agents
- reliable runtime lifecycle
- provider ecosystem

2027:

- distributed agent scheduling
- enterprise security
- multi-agent workflows
