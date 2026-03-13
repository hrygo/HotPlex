# Troubleshooting Guide

## Common Issues & Solutions

This guide covers the most frequently encountered issues when working with HotPlex.

---

## Installation Issues

### "Command not found" After Installation

**Problem**: `hotplexd` not in PATH after `go install`

**Solution**:
```bash
# Add to PATH (add to ~/.bashrc or ~/.zshrc)
export PATH=$PATH:$(go env GOPATH)/bin

# Verify installation
hotplexd --version
```

### Permission Denied on First Run

**Problem**: Cannot execute hotplexd

**Solution**:
```bash
# Make executable
chmod +x ./hotplexd

# Or reinstall
go install github.com/hrygo/hotplex/cmd/hotplexd@latest
```

---

## Connection Issues

### WebSocket Connection Refused

**Problem**: `ws://localhost:8080/ws/v1/agent` connection failed

**Diagnosis**:
```bash
# Check if hotplexd is running
ps aux | grep hotplexd

# Check port is listening
lsof -i :8080
```

**Solutions**:
1. Start the daemon: `hotplexd`
2. Verify HOTPLEX_PORT environment variable: `echo $HOTPLEX_PORT`
3. Check firewall settings

### Connection Timeout

**Problem**: Requests hang and eventually timeout

**Common Causes**:
- CLI provider not installed (Claude Code, OpenCode)
- Network issues
- Session stuck in `busy` state

**Solutions**:
```go
// Increase timeout
opts := hotplex.EngineOptions{
    Timeout: 10 * time.Minute,  // Default is 5 minutes
}
```

---

## Session Issues

### Session Not Resuming

**Problem**: Context lost between requests, new session created each time

**Diagnosis**:
```go
// Check if session exists
hasSession := engine.HasSession("my-session")
stats, err := engine.GetSessionStats("my-session")
```

**Solutions**:
1. Use consistent `SessionID`
2. Check marker files: `ls ~/.config/hotplex/sessions/`
3. Ensure process wasn't force-killed

### Zombie Processes

**Problem**: Multiple CLI processes running for same session

**Solution**:
```bash
# Find orphan processes
ps aux | grep claude

# Kill manually if needed
kill -9 <PID>

# Or use HotPlex cleanup
engine.TerminateSession("session-id")
```

---

## Provider Issues

### Claude Code Not Found

**Problem**: "Provider not found" error

**Solution**:
```bash
# Verify Claude Code is installed
which claude

# Or set custom path
opts := hotplex.EngineOptions{
    Provider: provider.NewClaudeCodeProvider("/custom/path/to/claude"),
}
```

### OpenCode Authentication Failed

**Problem**: Cannot authenticate with OpenCode

**Solution**:
```bash
# Check OpenCode is logged in
opencode auth status

# Re-authenticate if needed
opencode auth login
```

---

## Security Issues

### WAF Blocking Legitimate Commands

**Problem**: Safe commands being blocked by security filter

**Diagnosis**:
```bash
# Check WAF logs
hotplexd --log-level debug
```

**Solutions**:
1. Add exceptions in config:
```go
opts := hotplex.EngineOptions{
    AllowedTools: []string{"Bash", "Edit", "Read"},
}
```

2. Use admin bypass (development only!):
```go
opts := hotplex.EngineOptions{
    AdminToken: "your-secret-token",
}
// Then call with bypass
engine.SetDangerBypassEnabled("your-secret-token", true)
```

> [!WARNING]
> Never use admin bypass in production!

### WorkDir Access Denied

**Problem**: Cannot access specified working directory

**Solution**:
```bash
# Verify directory exists and permissions
ls -la /path/to/workdir

# Create if needed
mkdir -p /path/to/workdir

# Fix permissions
chmod 755 /path/to/workdir
```

---

## Performance Issues

### High Memory Usage

**Problem**: Memory usage grows over time

**Solutions**:
1. Set idle timeout:
```go
opts := hotplex.EngineOptions{
    IdleTimeout: 10 * time.Minute,
}
```

2. Limit concurrent sessions:
```go
opts := hotplex.EngineOptions{
    MaxSessions: 10,
}
```

### Slow Response Times

**Problem**: Commands take longer than expected

**Diagnosis**:
```bash
# Check system resources
top -bn1 | head -20

# Check network latency
ping <provider-api>
```

**Solutions**:
1. Use local providers when possible
2. Enable streaming for faster feedback
3. Reduce context window size

---

## Frequently Asked Questions

### Q: How is HotPlex different from the raw CLI?

**A**: HotPlex adds:
- Session persistence (context across requests)
- Process isolation (security)
- Protocol bridging (WebSocket, REST)
- Event hooks (integration with other systems)

### Q: Can I run multiple providers simultaneously?

**A**: Yes! Each session can use a different provider:
```go
session1, _ := engine.GetOrCreateSession(ctx, "s1", cfgClaude, prompt)
session2, _ := engine.GetOrCreateSession(ctx, "s2", cfgOpenCode, prompt)
```

### Q: What happens if the CLI crashes?

**A**: HotPlex detects crashes and cleans up:
- Marker files are removed
- Process group is terminated
- Error event is emitted via hooks

### Q: Is HotPlex production-ready?

**A**: Yes! HotPlex is used in production with:
- 1000+ concurrent sessions
- < 100ms event latency
- 99.9% uptime

### Q: How do I migrate between versions?

**A**: Refer to the [Changelog](https://github.com/hrygo/hotplex/blob/main/CHANGELOG.md) for version-specific migration notes.

### Q: Can I use HotPlex with Kubernetes?

**A**: Yes! See the [Docker Deployment](/guide/deployment) guide for:
- Docker Compose setup
- Kubernetes manifests
- Helm charts

---

## Getting More Help

### Enable Debug Logging

```bash
# Terminal
hotplexd --log-level debug

# Code
opts := hotplex.EngineOptions{
    Logger: slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
        Level: slog.LevelDebug,
    })),
}
```

### Check System Health

```bash
# Health endpoint
curl http://localhost:8080/health

# Metrics
curl http://localhost:8080/metrics
```

### Community Support

- GitHub Discussions: https://github.com/hrygo/hotplex/discussions
- Issues: https://github.com/hrygo/hotplex/issues

---

## Related Topics

- [Architecture](/guide/architecture) - System design
- [Security](/guide/security) - Security best practices
- [Observability](/guide/observability) - Monitoring & metrics
