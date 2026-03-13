# The Quick Start Journey

## Experience HotPlex in 5 Minutes

Welcome. This guide is designed to get your first HotPlex agent up and running with surgical precision. We bypass the theoretical and focus on the immediate: a self-healing, stateful agent in your terminal.

---

## Prerequisites

Before we begin, ensure you have:

| Requirement | Version | Verification |
|-------------|---------|--------------|
| Go | ≥1.21 | `go version` |
| Git | Any | `git --version` |
| Claude Code (optional) | Latest | `claude --version` |

---

## Step 1: Acquire HotPlex

Choose your acquisition method:

::: code-group

```bash [Binary Release]
# Download the latest release for your platform
# Linux/macOS (x86_64)
curl -L -o hotplexd https://github.com/hrygo/hotplex/releases/latest/download/hotplexd-linux-amd64

# macOS (Apple Silicon)
curl -L -o hotplexd https://github.com/hrygo/hotplex/releases/latest/download/hotplexd-darwin-arm64

# Make executable
chmod +x hotplexd

# Verify
./hotplexd --version
```

```bash [Go Install]
# Install via Go (requires Go 1.21+)
go install github.com/hrygo/hotplex/cmd/hotplexd@latest

# Add to PATH if needed
export PATH=$PATH:$(go env GOPATH)/bin

# Verify
hotplexd --version
```

```bash [Build from Source]
# Clone the repository
git clone https://github.com/hrygo/hotplex.git
cd hotplex

# Build
make build

# Verify
./dist/hotplexd --version
```

:::

---

## Step 2: Launch the Daemon

Start the HotPlex daemon:

```bash
# Basic start (uses default port 8080)
./hotplexd

# Or specify a custom port
HOTPLEX_PORT=9000 ./hotplexd

# In production, run in background
nohup ./hotplexd > hotplexd.log 2>&1 &
```

### Verify Running

```bash
# Check health endpoint
curl http://localhost:8080/health

# Check metrics
curl http://localhost:8080/metrics
```

Expected response from `/health`:
```json
{
  "status": "healthy",
  "version": "0.17.0",
  "uptime": "1m23s"
}
```

> [!TIP]
> In dev mode, HotPlex uses an in-memory state store. For production, configure persistent storage.

---

## Step 3: Your First Session

### Option A: Use the SDK

Create a Go application:

```go
package main

import (
    "context"
    "fmt"
    "time"
    "github.com/hrygo/hotplex"
    "github.com/hrygo/hotplex/event"
)

func main() {
    // 1. Initialize engine
    engine := hotplex.NewEngine(hotplex.EngineOptions{
        Timeout:    5 * time.Minute,
        LogLevel:  "info",
    })
    defer engine.Close()

    // 2. Configure session
    cfg := &hotplex.Config{
        SessionID:        "my-first-agent",
        WorkDir:          "/tmp/hotplex-sandbox",
        TaskInstructions:  "You are a helpful coding assistant.",
    }

    // 3. Execute
    ctx := context.Background()
    err := engine.Execute(ctx, cfg, "What is the current directory?", 
        func(ev *event.EventWithMeta) error {
            if ev.Type == "answer" {
                fmt.Printf("🤖: %s\n", ev.Data)
            }
            return nil
        })
    
    if err != nil {
        fmt.Printf("Error: %v\n", err)
    }
}
```

Run it:
```bash
go run main.go
```

### Option B: Use WebSocket API

Connect via WebSocket for real-time streaming:

```javascript
// JavaScript/Node.js example
const ws = new WebSocket('ws://localhost:8080/ws/v1/agent');

ws.onopen = () => {
    // Send session config
    ws.send(JSON.stringify({
        type: 'session_start',
        session_id: 'my-session',
        work_dir: '/tmp/sandbox',
        instructions: 'You are a helpful assistant.'
    }));
    
    // Send first message
    ws.send(JSON.stringify({
        type: 'prompt',
        content: 'Hello! What can you help me with?'
    }));
};

ws.onmessage = (event) => {
    const data = JSON.parse(event.data);
    console.log(`[${data.type}]:`, data.payload?.content || data);
};
```

### Option C: Use cURL

For quick testing:

```bash
# Create session
curl -X POST http://localhost:8080/session \
  -H "Content-Type: application/json" \
  -d '{"session_id": "test", "work_dir": "/tmp/test"}'

# Send message
curl -X POST http://localhost:8080/v1/agent/chat \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer your-api-key" \
  -d '{"session_id": "test", "message": "Hello!"}'
```

---

## Step 4: Connect a Chat Platform

### Slack Integration

1. Create a Slack App at https://api.slack.com/apps
2. Enable Socket Mode and install to workspace
3. Configure HotPlex:

```bash
# Set environment variables
export HOTPLEX_SLACK_APP_TOKEN=xapp-xxx
export HOTPLEX_SLACK_BOT_TOKEN=xoxb-xxx

# Restart HotPlex
./hotplexd
```

4. Invite your bot to a channel:
```
/invite @your-bot-name
```

5. Start chatting!

### Configuration File

Alternatively, use a YAML config:

```yaml
# config.yaml
server:
  port: 8080
  
slack:
  enabled: true
  app_token: "${HOTPLEX_SLACK_APP_TOKEN}"
  bot_token: "${HOTPLEX_SLACK_BOT_TOKEN}"

engine:
  timeout: 5m
  idle_timeout: 30m
  
session:
  work_dir: "/var/lib/hotplex/sessions"
  marker_dir: "/var/lib/hotplex/markers"
```

```bash
./hotplexd --config config.yaml
```

---

## Step 5: Explore

Now that you have a running instance, explore further:

| Goal | Resource |
|------|----------|
| Understand the architecture | [Architecture Overview](/guide/architecture) |
| Deep dive into state management | [State & Persistence](/guide/state) |
| Secure your deployment | [Security Guide](/guide/security) |
| Add custom integrations | [Hooks System](/guide/hooks) |
| Monitor your instance | [Observability](/guide/observability) |

---

## Next Steps

### Learning Paths

**Quick Path (15 min)**: Quick Start → State Management → Slack Integration

**Integration Path (1 hour)**: Architecture → Go SDK → Custom Provider → Hooks

**Production Path (2 hours)**: Architecture → Security → Deployment → Observability

### Common First Steps

1. **Customize the prompt**: Modify `TaskInstructions` for your use case
2. **Add security**: Configure `AllowedTools` and WAF rules
3. **Enable webhooks**: Get notified on events via Slack, Feishu, etc.
4. **Monitor**: Set up Prometheus metrics and OpenTelemetry traces

---

## Troubleshooting

Stuck? Check the [Troubleshooting Guide](/guide/troubleshooting) for common issues.

---

> "We handle the state, you handle the soul." — The HotPlex Team
