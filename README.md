<div align="center">
  <img src="docs/images/hotplex_beaver_banner.webp" alt="HotPlex Banner"/>

  # HotPlex

  **Turn AI CLIs into Production-Ready Services**

  Bridge powerful AI CLIs (Claude Code, OpenCode) into persistent, secure, production-grade interactive services.

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

## ⚡ Quick Start

```bash
# One-line installation
curl -sL https://raw.githubusercontent.com/hrygo/hotplex/main/install.sh | bash

# Or build from source
make build

# Start with Slack or Feishu
export HOTPLEX_SLACK_PRIMARY_OWNER=U...
export HOTPLEX_SLACK_BOT_TOKEN=xoxb-...
export HOTPLEX_SLACK_APP_TOKEN=xapp-...
./hotplexd --config configs/server.yaml --config-dir configs/chatapps
```

### Requirements

| Component | Version |
| :-------- | :------ |
| Go | 1.25+ |
| AI CLI | [Claude Code](https://github.com/anthropics/claude-code) or [OpenCode](https://github.com/hrygo/opencode) |

---

## ✨ Features

| | |
| :--- | :--- |
| 🔄 **Session Pooling** | Long-lived CLI processes with instant reconnection |
| 🌊 **Full-Duplex Streaming** | Sub-second token delivery via Go channels |
| 🛡️ **Regex WAF** | Block destructive commands (`rm -rf /`, `mkfs`, etc.) |
| 🔒 **PGID Isolation** | Clean process termination, no zombies |
| 💬 **Multi-Platform** | Slack · Feishu |
| 📦 **Go SDK** | Embed directly in your Go app with zero overhead |
| 🔌 **WebSocket Gateway** | Language-agnostic access via `hotplexd` daemon |
| 📊 **OpenTelemetry** | Built-in metrics and tracing support |
| 🐳 **Docker 1+n** | 1 Base + n Stacks (`node`, `python`, `java`, `rust`, `full`) |

---

## 🎯 Why HotPlex?

> **The missing control plane for AI agents in production**

| Challenge | HotPlex Solution |
| :-------- | :--------------- |
| AI agents spin up fresh each request | **Persistent sessions** - CLI stays alive, reuses context |
| No safety for destructive commands | **Regex WAF** - Programmable firewall for shell instructions |
| Hard to scale AI interactions | **Process multiplexing** - Hundreds of concurrent sessions |
| Integration complexity | **ChatApps** - One codebase, Slack / Feishu support |
| Enterprise-grade security | **PGID isolation** + filesystem jail + container sandbox |

---

## 🏛 Architecture

```
│                        ChatApps Layer                           │
│                       Slack · Feishu                            │
└────────────────────────────┬────────────────────────────────────┘
                             │
┌────────────────────────────▼────────────────────────────────────┐
│                      WebSocket Gateway                          │
│                  (hotplexd daemon / Go SDK)                     │
└────────────────────────────┬────────────────────────────────────┘
                             │
┌────────────────────────────▼────────────────────────────────────┐
│                      Engine / Runner                            │
│         I/O Multiplexing · Session Pool · Events               │
└────────────────────────────┬────────────────────────────────────┘
                             │
        ┌────────────────────┼────────────────────┐
        ▼                    ▼                    ▼
   ┌─────────┐         ┌─────────┐         ┌─────────┐
   │ Claude  │         │ OpenCode│         │ Custom  │
   │   CLI   │         │   CLI   │         │ Provider│
   └─────────┘         └─────────┘         └─────────┘
```

### Security Layers

| Layer | Implementation |
| :---- | :------------- |
| Tool Governance | `AllowedTools` config - restrict agent capabilities |
| Danger WAF | Regex interception - block `rm -rf /`, `mkfs`, `dd` |
| Process Isolation | PGID-based termination - no orphaned processes |
| Filesystem Jail | `WorkDir` lockdown - confined to project root |
| Container Sandbox | Docker (BaaS) - OS-level isolation & limits |

---

## 📖 Usage Examples

### Go SDK

```go
import (
    "context"
    "fmt"
    "time"

    "github.com/hrygo/hotplex"
    "github.com/hrygo/hotplex/types"
)

engine, err := hotplex.NewEngine(hotplex.EngineOptions{
    Timeout:     5 * time.Minute,
    IdleTimeout: 30 * time.Minute,
})
if err != nil {
    panic(err)
}
defer engine.Close()

cfg := &types.Config{
    WorkDir:   "/path/to/project",
    SessionID: "user-session-123",
}

engine.Execute(context.Background(), cfg, "Refactor this function", func(eventType string, data any) error {
    if msg, ok := data.(*types.StreamMessage); ok {
        fmt.Print(msg.Content) // Streaming response
    }
    return nil
})
```

### Slack Bot

```yaml
# configs/chatapps/slack.yaml
platform: slack
mode: socket

provider:
  type: claude-code
  default_model: sonnet

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

```bash
export HOTPLEX_SLACK_BOT_USER_ID=B12345
export HOTPLEX_SLACK_BOT_TOKEN=xoxb-...
export HOTPLEX_SLACK_APP_TOKEN=xapp-...
./hotplexd --config configs/server.yaml --config-dir configs/chatapps
```

### WebSocket API

```javascript
const ws = new WebSocket('ws://localhost:8080/ws/v1/agent');

ws.onmessage = (event) => {
  const data = JSON.parse(event.data);
  console.log(data.type, data.content);
};

ws.send(JSON.stringify({
  type: 'execute',
  prompt: 'Hello, AI!'
}));
```

---

## 📚 Documentation

| | |
| :--- | :--- |
| [🚀 Deployment Guide](https://hrygo.github.io/hotplex/guide/deployment) | Docker, production setup |
| [💬 ChatApps Manual](chatapps/README.md) | Slack & Feishu integration |
| [🛠 Go SDK Reference](https://hrygo.github.io/hotplex/sdks/go-sdk) | Complete SDK documentation |
| [🔒 Security Guide](https://hrygo.github.io/hotplex/guide/security) | WAF, isolation, best practices |
| [📊 Observability](https://hrygo.github.io/hotplex/guide/observability) | Metrics, tracing, logging |

---

## 🤝 Contributing

Contributions are welcome! Please read our [Contributing Guide](CONTRIBUTING.md) for details.

```bash
# Development setup
go mod download   # Install dependencies
# Build the Node.js language stack
make stack S=node

# Build all language stacks
make stack-all
make test         # Run tests
make lint         # Run linter
make build        # Build binary

# Submit a PR
git checkout -b feat/your-feature
git commit -m "feat: add awesome feature"
gh pr create --fill
```

---

## 📄 License

MIT License © 2024-present [HotPlex Contributors](https://github.com/hrygo/hotplex/graphs/contributors)

---

<div align="center">
  <img src="docs/images/logo.svg" alt="HotPlex Logo" width="100"/>
  <br/>
  <sub>Built for the AI Engineering community</sub>
</div>
