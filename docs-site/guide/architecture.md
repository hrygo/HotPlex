# The Anatomy of a Stateful Agent

## System Architecture

HotPlex is a Go-based runtime that orchestrates AI CLI agents. It provides stateful sessions, process isolation, and protocol bridging between chat platforms and agent CLIs.

---

## Data Flow

To understand HotPlex, follow a user message through the system:

```
User Message → Access Layer → Engine Layer → Process Layer → Response
```

### Step-by-Step:

1. **Input**: A message arrives via WebSocket, REST API, or ChatApp (Slack/Feishu)
2. **Authentication**: Credentials are validated, session is retrieved or created
3. **Security Check**: WAF validates the request against dangerous patterns
4. **Execution**: The message is sent to the CLI process (Claude Code / OpenCode)
5. **Streaming**: Output is streamed back in real-time via events
6. **State Update**: Session state is persisted for continuity

---

## Architecture Overview

![Architecture Overview](/images/topology.svg)

### Layer Responsibilities

| Layer | Components | Purpose |
|-------|------------|---------|
| **Access** | WebSocket Gateway, HTTP Gateway, Auth | Protocol translation and authentication |
| **Engine** | Session Pool, WAF Security, Event Router | Request routing and security enforcement |
| **Process** | CLI Providers (Claude/OpenCode), PGID Isolation | Actual agent execution |

---

## Core Components

### Session Pool

Manages concurrent agent sessions with lifecycle control:

- **GetOrCreate**: Retrieves existing or creates new session
- **Terminate**: Clean shutdown via PGID kill
- **Stats**: Runtime metrics per session

### Security (WAF)

Multi-layer defense against malicious commands:

1. **Tool Whitelist**: Restricts available CLI tools
2. **Regex Detection**: Blocks dangerous patterns (`rm -rf`, `dd`, etc.)
3. **Path Restriction**: Limits file system access to WorkDir

### Process Isolation

Each session runs in an isolated process group:

```bash
# Session process hierarchy
hotplexd (parent)
  ├── cli (child, PGID=same)
  │   └── claude (grandchild)
  └── security-monitor
```

> [!TIP]
> Use `kill -PGID <pid>` to terminate the entire group safely.

---

## Session Lifecycle

![Session Lifecycle](/images/session-lifecycle.svg)

A session transitions through states:

| State | Description |
|-------|-------------|
| `starting` | Process spawning, CLI initializing (~1-2s) |
| `ready` | Awaiting commands, stdin/stdout piped |
| `busy` | Processing request, streaming events |
| `dead` | Terminated, cleanup in progress |

---

## Security Layers

![Security Model](/images/hotplex-security.svg)

HotPlex implements defense in depth:

1. **Input Validation**: Regex WAF blocks known dangerous patterns
2. **Tool Governance**: Whitelist of allowed CLI tools
3. **Process Isolation**: PGID-based termination prevents zombies
4. **WorkDir Jail**: File access restricted to configured directory

---

## Performance Characteristics

| Metric | Value |
|--------|-------|
| Event Latency | < 100ms |
| Session Startup | ~1-2s |
| Concurrent Sessions | 1000+ |
| Memory/Session | ~50-100MB |

---

## Related Topics

- [State Management](/guide/state) - Session persistence
- [Security Guide](/guide/security) - WAF patterns
- [API Reference](/reference/api) - Protocol details
- [Protocol Spec](/reference/protocol) - DMP format
