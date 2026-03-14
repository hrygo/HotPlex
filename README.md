<div align="center">
  <img src="docs/images/hotplex_beaver_banner.webp" alt="HotPlex Banner"/>

  # HotPlex

  **High-Performance AI Agent Runtime**

  HotPlex transforms terminal AI tools (Claude Code, OpenCode) into production services. Built with Go using the Cli-as-a-Service paradigm, it eliminates CLI startup latency through persistent process pooling and ensures execution safety via PGID isolation and Regex WAF. The system supports WebSocket/HTTP/SSE communication with Python and TypeScript SDKs. At the application layer, HotPlex integrates with Slack and Feishu, supporting streaming output, interactive cards, and multi-bot protocols.

  <p>
    <a href="https://github.com/hrygo/hotplex/releases/latest">
      <img src="https://img.shields.io/github/v/release/hrygo/hotplex?style=flat-square&logo=go&color=00ADD8" alt="Release">
    </a>
    <a href="https://pkg.go.dev/github.com/hrygo/hotplex">
      <img src="https://img.shields.io/badge/go-reference-00ADD8?style=flat-square&logo=go" alt="Go Reference">
    </a>
    <a href="https://goreportcard.com/report/github.com/hrygo/hotplex">
      <img src="https://img.shields.io/badge/go-report-brightgreen?style=flat-square" alt="Go Report">
    </a>
    <a href="https://github.com/hrygo/hotplex/actions/workflows/test.yml">
      <img src="https://img.shields.io/github/actions/workflow/status/hrygo/hotplex/test.yml?style=flat-square" alt="Tests">
    </a>
    <a href="https://codecov.io/gh/hrygo/hotplex">
      <img src="https://img.shields.io/codecov/c/github/hrygo/hotplex?style=flat-square" alt="Coverage">
    </a>
    <a href="LICENSE">
      <img src="https://img.shields.io/github/license/hrygo/hotplex?style=flat-square&color=blue" alt="License">
    </a>
    <a href="https://github.com/hrygo/hotplex/stargazers">
      <img src="https://img.shields.io/github/stars/hrygo/hotplex?style=flat-square" alt="Stars">
    </a>
  </p>

  <p>
    <a href="docs/quick-start.md">Quick Start</a> ·
    <a href="https://hrygo.github.io/hotplex/guide/features">Features</a> ·
    <a href="docs/architecture.md">Architecture</a> ·
    <a href="https://hrygo.github.io/hotplex/">Docs</a> ·
    <a href="https://github.com/hrygo/hotplex/discussions">Discussions</a> ·
    <a href="README_zh.md">简体中文</a>
  </p>
</div>

---

## Table of Contents

- [Quick Start](#-quick-start)
- [Core Concepts](#-core-concepts)
- [Project Structure](#-project-structure)
- [Features](#-features)
- [Architecture](#-architecture)
- [Usage Examples](#-usage-examples)
- [Development Guide](#-development-guide)
- [Documentation](#-documentation)
- [Contributing](#-contributing)

---

## ⚡ Quick Start

```bash
# One-line installation
curl -sL https://raw.githubusercontent.com/hrygo/hotplex/main/install.sh | bash

# Or build from source
make build

# Start with Slack
export HOTPLEX_SLACK_BOT_USER_ID=B12345
export HOTPLEX_SLACK_BOT_TOKEN=xoxb-...
export HOTPLEX_SLACK_APP_TOKEN=xapp-...
./hotplexd --config configs/server.yaml --config-dir configs/chatapps

# Or start with WebSocket gateway only
./hotplexd --config configs/server.yaml
```

### Requirements

| Component | Version | Notes |
| :-------- | :------ | :-----|
| Go | 1.25+ | Runtime & SDK |
| AI CLI | [Claude Code](https://github.com/anthropics/claude-code) or [OpenCode](https://github.com/hrygo/opencode) | Execution target |
| Docker | 24.0+ | Optional, for container deployment |

### First Run Checklist

```bash
# 1. Clone and build
git clone https://github.com/hrygo/hotplex.git
cd hotplex
make build

# 2. Copy environment template
cp .env.example .env

# 3. Configure your AI CLI
# Ensure Claude Code or OpenCode is in PATH

# 4. Run the daemon
./hotplexd --config configs/server.yaml
```

---

## 🧠 Core Concepts

Understanding these concepts is essential for effective HotPlex development.

### Session Pooling

HotPlex maintains **long-lived CLI processes** instead of spawning fresh instances per request. This eliminates:
- Cold start latency (typically 2-5 seconds per invocation)
- Context loss between requests
- Resource waste from repeated initialization

```
Request 1 → CLI Process 1 (spawned, persistent)
Request 2 → CLI Process 1 (reused, instant)
Request 3 → CLI Process 1 (reused, instant)
```

### I/O Multiplexing

The `Runner` component handles bidirectional communication between:
- **Upstream**: User requests (WebSocket/HTTP/ChatApp events)
- **Downstream**: CLI stdin/stdout/stderr streams

```go
// Each session has dedicated I/O channels
type Session struct {
    Stdin  io.Writer
    Stdout io.Reader
    Stderr io.Reader
    Events chan *Event  // Internal event bus
}
```

### PGID Isolation

Process Group ID (PGID) isolation ensures **clean termination**:
- CLI processes are spawned with `Setpgid: true`
- Termination sends signal to entire process group (`kill -PGID`)
- No orphaned or zombie processes

### Regex WAF

Web Application Firewall layer intercepts dangerous commands before they reach the CLI:
- Block patterns: `rm -rf /`, `mkfs`, `dd`, `:(){:|:&};:`
- Configurable via `security.danger_waf` in config
- Works alongside CLI's native tool restrictions (`AllowedTools`)

### ChatApps Abstraction

Unified interface for multi-platform bot integration:

```go
type ChatAdapter interface {
    // Platform-specific event handling
    HandleEvent(event Event) error
    // Unified message format
    SendMessage(msg *ChatMessage) error
}

### MessageOperations (Optional)

Advanced platforms implement streaming and message management:

```go
type MessageOperations interface {
    StartStream(ctx, channelID, threadTS) (messageTS, error)
    AppendStream(ctx, channelID, messageTS, content) error
    StopStream(ctx, channelID, messageTS) error
    UpdateMessage(ctx, channelID, messageTS, msg) error
    DeleteMessage(ctx, channelID, messageTS) error
}
```

---

## 📂 Project Structure

```
hotplex/
├── cmd/
│   └── hotplexd/           # Daemon entrypoint
├── internal/               # Core implementation (private)
│   ├── engine/             # Session pool & runner
│   ├── server/             # WebSocket & HTTP gateway
│   ├── security/           # WAF & isolation
│   ├── config/             # Configuration loading
│   ├── sys/                # OS signals
│   ├── telemetry/          # OpenTelemetry
│   └── ...
├── brain/                  # Native Brain orchestration
├── cache/                  # Caching layer
├── provider/               # AI provider adapters
│   ├── claudecode/         # Claude Code protocol
│   ├── opencode/           # OpenCode protocol
│   └── ...
├── chatapps/               # Platform adapters
│   ├── slack/              # Slack Bot
│   ├── feishu/             # Feishu Bot
│   └── base/               # Common interfaces
├── types/                  # Public type definitions
├── event/                  # Event system
├── plugins/                # Extension points
│   └── storage/            # Message persistence
├── sdks/                   # Language bindings
│   ├── go/                 # Go SDK (embedded)
│   ├── python/             # Python SDK
│   └── typescript/         # TypeScript SDK
├── docker/                 # Container definitions
├── configs/                # Configuration examples
└── docs/                  # Architecture docs
```

### Key Directories

| Directory | Purpose | Public API |
|-----------|---------|-----------|
| `types/` | Core types & interfaces | ✅ Yes |
| `event/` | Event definitions | ✅ Yes |
| `hotplex.go` | SDK entry point | ✅ Yes |
| `internal/engine/` | Session management | ❌ Internal |
| `internal/server/` | Network protocols | ❌ Internal |
| `provider/` | CLI adapters | ⚠️ Provider interface |

---

## ✨ Features

| Feature | Description | Use Case |
| :------ | :---------- | :------- |
| 🔄 **Session Pooling** | Long-lived CLI processes with instant reconnection | High-frequency AI interactions |
| 🌊 **Full-Duplex Streaming** | Sub-second token delivery via Go channels | Real-time UI updates |
| 🛡️ **Regex WAF** | Block destructive commands (`rm -rf /`, `mkfs`, etc.) | Security hardening |
| 🔒 **PGID Isolation** | Clean process termination, no zombies | Production reliability |
| 💬 **Multi-Platform** | Slack · Feishu | Team communication |
| 📦 **Go SDK** | Embed directly in your Go app with zero overhead | Custom integrations |
| 🔌 **WebSocket Gateway** | Language-agnostic access via `hotplexd` daemon | Web frontend |
| 📊 **OpenTelemetry** | Built-in metrics and tracing support | Observability |
| 🐳 **Docker 1+n** | 1 Base + n Stacks (`node`, `python`, `java`, `rust`, `full`) | Multi-language |

---

## 🏛 Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                      ChatApps Layer                              │
│                 Slack Bot · Feishu Bot · Web                    │
│            (Event → ChatMessage → Session ID)                  │
└────────────────────────────┬────────────────────────────────────┘
                             │
┌────────────────────────────▼────────────────────────────────────┐
│                    WebSocket Gateway                            │
│              hotplexd daemon / Go SDK / HTTP                   │
│         (Protocol translation, Rate limiting, Auth)            │
└────────────────────────────┬────────────────────────────────────┘
                             │
┌────────────────────────────▼────────────────────────────────────┐
│                      Engine / Runner                            │
│    ┌─────────────────────────────────────────────────────┐    │
│    │  Session Pool (map[SessionID]*Session)              │    │
│    │  - Lifecycle management                              │    │
│    │  - Idle timeout & GC                                 │    │
│    └─────────────────────────────────────────────────────┘    │
│    ┌─────────────────────────────────────────────────────┐    │
│    │  Runner (I/O Multiplexer)                           │    │
│    │  - stdin/stdout/stderr piping                       │    │
│    │  - Event serialization                               │    │
│    └─────────────────────────────────────────────────────┘    │
└────────────────────────────┬────────────────────────────────────┘
                             │
        ┌────────────────────┼────────────────────┐
        ▼                    ▼                    ▼
   ┌─────────┐         ┌─────────┐         ┌─────────┐
   │ Claude  │         │ OpenCode│         │ Custom  │
   │   CLI   │         │   CLI   │         │ Provider│
   └─────────┘         └─────────┘         └─────────┘
```

### Data Flow: Request to Response

```
User → Gateway → Session Pool → Runner → CLI
CLI → Runner → Gateway → User (streaming)
```

### Security Layers

| Layer | Implementation | Configuration |
| :---- | :------------- | :------------ |
| Tool Governance | `AllowedTools` config | `security.allowed_tools` |
| Danger WAF | Regex interception | `security.danger_waf` |
| Process Isolation | PGID-based termination | Automatic |
| Filesystem Jail | `WorkDir` lockdown | `engine.work_dir` |
| Container Sandbox | Docker (BaaS) | `docker/` |

---

## 📖 Usage Examples

### Go SDK (Embeddable)

```go
import (
    "context"
    "fmt"
    "time"

    "github.com/hrygo/hotplex"
    "github.com/hrygo/hotplex/types"
)

func main() {
    // Initialize engine
    engine, err := hotplex.NewEngine(hotplex.EngineOptions{
        Timeout:     5 * time.Minute,
        IdleTimeout: 30 * time.Minute,
    })
    if err != nil {
        panic(err)
    }
    defer engine.Close()

    // Execute prompt
    cfg := &types.Config{
        WorkDir:   "/path/to/project",
        SessionID: "user-session-123",
    }

    engine.Execute(context.Background(), cfg, "Explain this function", func(eventType string, data any) error {
        switch eventType {
        case "message":
            if msg, ok := data.(*types.StreamMessage); ok {
                fmt.Print(msg.Content)  // Streaming output
            }
        case "error":
            if errMsg, ok := data.(string); ok {
                fmt.Printf("Error: %s\n", errMsg)
            }
        case "usage":
            if stats, ok := data.(*types.UsageStats); ok {
                fmt.Printf("Tokens: %d input, %d output\n", stats.InputTokens, stats.OutputTokens)
            }
        }
        return nil
    })
}
```

### Slack Bot Configuration

```yaml
# configs/chatapps/slack.yaml
platform: slack
mode: socket

provider:
  type: claude-code
  default_model: sonnet
  allowed_tools:
    - Read
    - Edit
    - Glob
    - Grep
    - Bash

engine:
  work_dir: ~/projects/hotplex
  timeout: 30m
  idle_timeout: 1h

security:
  owner:
    primary: ${HOTPLEX_SLACK_PRIMARY_OWNER}
    policy: trusted

assistant:
  bot_user_id: ${HOTPLEX_SLACK_BOT_USER_ID}
  dm_policy: allow
  group_policy: multibot
```

### WebSocket API

```javascript
// Connect
const ws = new WebSocket('ws://localhost:8080/ws/v1/agent');

// Listen for messages
ws.onmessage = (event) => {
  const data = JSON.parse(event.data);
  switch (data.type) {
    case 'message':
      console.log(data.content);
      break;
    case 'error':
      console.error(data.error);
      break;
    case 'done':
      console.log('Execution complete');
      break;
  }
};

// Execute prompt
ws.send(JSON.stringify({
  type: 'execute',
  session_id: 'optional-session-id',
  prompt: 'List files in current directory'
}));
```

### HTTP SSE API

```bash
curl -N -X POST http://localhost:8080/api/v1/execute \
  -H "Content-Type: application/json" \
  -d '{"prompt": "Hello, AI!", "session_id": "session-123"}'
```

---

## 💻 Development Guide

### Common Tasks

```bash
# Run tests
make test

# Run with race detector
make test-race

# Build binary
make build

# Run linter
make lint

# Build Docker images
make docker-build

# Start Docker stack
make docker-up
```

### Adding a New ChatApp Platform

1. **Implement the adapter interface** in `chatapps/<platform>/`:

```go
type Adapter struct {
    client *platform.Client
    engine *engine.Engine
}

// Implement base.ChatAdapter interface
var _ base.ChatAdapter = (*Adapter)(nil)

func (a *Adapter) HandleEvent(event base.Event) error {
    // Platform-specific event parsing
}

func (a *Adapter) SendMessage(msg *base.ChatMessage) error {
    // Platform-specific message sending
}
```

2. **Register in** `chatapps/setup.go`:

```go
func init() {
    registry.Register("platform-name", NewAdapter)
}
```

3. **Add configuration** in `configs/chatapps/`:

```yaml
platform: platform-name
mode: socket  # or http
# ... platform-specific config
```

### Adding a New Provider

1. **Implement** `provider/<name>/parser.go`:

```go
type Parser struct{}

func (p *Parser) ParseStream(line string) (*types.StreamMessage, error) {
    // Provider-specific output parsing
}
```

2. **Register** in `provider/factory.go`:

```go
func init() {
    providers.Register("provider-name", NewProvider)
}
```

---

## 📚 Documentation

| Guide | Description |
| :---- | :---------- |
| [🚀 Deployment](https://hrygo.github.io/hotplex/guide/deployment) | Docker, production setup |
| [💬 ChatApps](chatapps/README.md) | Slack & Feishu integration |
| [🛠 Go SDK](https://hrygo.github.io/hotplex/sdks/go-sdk) | SDK reference |
| [🔒 Security](https://hrygo.github.io/hotplex/guide/security) | WAF, isolation |
| [📊 Observability](https://hrygo.github.io/hotplex/guide/observability) | Metrics, tracing |
| [⚙️ Configuration](docs/configuration.md) | Full config reference |

---

## 🤝 Contributing

We welcome contributions! Please follow these steps:

```bash
# 1. Fork and clone
git clone https://github.com/hrygo/hotplex.git

# 2. Create a feature branch
git checkout -b feat/your-feature

# 3. Make changes and test
make test
make lint

# 4. Commit with conventional format
git commit -m "feat(engine): add session priority support"

# 5. Submit PR
gh pr create --fill
```

### Commit Message Format

```
<type>(<scope>): <description>

Types: feat, fix, refactor, docs, test, chore
Scope: engine, server, chatapps, provider, etc.
```

### Code Standards

- Follow [Uber Go Style Guide](.agent/rules/uber-go-style-guide.md)
- All interfaces require compile-time verification
- Run `make test-race` before submitting

---

## 📄 License

MIT License © 2024-present [HotPlex Contributors](https://github.com/hrygo/hotplex/graphs/contributors)

---

<div align="center">
  <img src="docs/images/logo.svg" alt="HotPlex Logo" width="100"/>
  <br/>
  <sub>Built for the AI Engineering community</sub>
</div>
