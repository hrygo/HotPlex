<div align="center">
  <img src="docs/images/hotplex_beaver_banner.webp" alt="HotPlex" width="100%"/>

  <h1>HotPlex</h1>

  <p><strong>AI Agent Control Plane — Turn AI CLIs into Production-Ready Services</strong></p>

  <p>
    <a href="https://github.com/hrygo/hotplex/releases/latest">
      <img src="https://img.shields.io/github/v/release/hrygo/hotplex?style=flat-square&logo=go&color=00ADD8" alt="Release">
    </a>
    <a href="https://pkg.go.dev/github.com/hrygo/hotplex">
      <img src="https://img.shields.io/badge/go-reference-00ADD8?style=flat-square&logo=go" alt="Go Reference">
    </a>
    <a href="https://goreportcard.com/report/github.com/hrygo/hotplex">
      <img src="https://goreportcard.com/badge/github.com/hrygo/hotplex?style=flat-square" alt="Go Report">
    </a>
    <a href="LICENSE">
      <img src="https://img.shields.io/github/license/hrygo/hotplex?style=flat-square&color=blue" alt="License">
    </a>
    <a href="https://github.com/hrygo/hotplex/stargazers">
      <img src="https://img.shields.io/github/stars/hrygo/hotplex?style=flat-square" alt="Stars">
    </a>
  </p>

  <p>
    <a href="#quick-start">Quick Start</a> •
    <a href="#features">Features</a> •
    <a href="https://hrygo.github.io/hotplex/">Docs</a> •
    <a href="docs/chatapps/slack-setup-beginner.md">Slack Guide</a> •
    <a href="README_zh.md">简体中文</a>
  </p>
</div>

---

## Overview

HotPlex transforms AI CLI tools (Claude Code, OpenCode) from "run-and-exit" commands into **persistent, stateful services** with full-duplex streaming.

**Why HotPlex?**

- **Zero Spin-up Overhead** — Eliminate 3-5 second CLI cold starts with persistent session pooling
- **Cli-as-a-Service** — Continuous instruction flow and context preservation across interactions
- **Production-Ready Security** — Regex WAF, PGID process isolation, and filesystem boundaries
- **Multi-Platform ChatApps** — Native Slack, Telegram, Feishu, DingTalk integration
- **Simple Integration** — Go SDK embedding or standalone WebSocket server

## Quick Start

### Prerequisites

- Go 1.25+
- Claude Code CLI or OpenCode CLI (optional, for AI capabilities)

### Install

```bash
# One-click install (Linux / macOS / WSL)
curl -sL https://raw.githubusercontent.com/hrygo/hotplex/main/install.sh | bash
```

More options: specific version, custom directory, dry-run - see [INSTALL.md](INSTALL.md).

### Configure

```bash
# Copy example environment
cp .env.example .env

# Edit with your credentials
# For ChatApps, configure chatapps/configs/*.yaml
```

### Run

```bash
# Start with ChatApps (recommended for production)
./dist/hotplexd --config chatapps/configs

# Or start standalone server
./dist/hotplexd
```

**That's it!** Your AI agent service is now running.

## Features

| Feature | Description |
|---------|-------------|
| **Session Pooling** | Long-lived CLI processes with instant reconnection |
| **Full-Duplex Streaming** | Sub-second token delivery via Go channels |
| **Regex WAF** | Block destructive commands (`rm -rf /`, `mkfs`, etc.) |
| **PGID Isolation** | Clean process termination, no zombie processes |
| **ChatApps** | Slack (Block Kit, Streaming, Assistant Status), Telegram, Feishu, DingTalk |
| **Go SDK** | Embed directly in your Go application with zero overhead |
| **WebSocket Gateway** | Language-agnostic access via `hotplexd` daemon |
| **OpenTelemetry** | Built-in metrics and tracing support |

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                      Access Layer                           │
│         Go SDK  │  WebSocket  │  ChatApps Adapters          │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                      Engine Layer                           │
│    Session Pool  │  Config Manager  │  Security WAF         │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                     Process Layer                           │
│    Claude Code  │  OpenCode  │  Isolated Workspaces         │
└─────────────────────────────────────────────────────────────┘
```

## Usage Examples

### Go SDK

```go
import "github.com/hrygo/hotplex"

engine, _ := hotplex.NewEngine(hotplex.EngineOptions{
    Timeout: 5 * time.Minute,
})

engine.Execute(ctx, cfg, "Refactor this function", func(event Event) {
    fmt.Println(event.Content)
})
```

### ChatApps (Slack)

```yaml
# chatapps/configs/slack.yaml
platform: slack
mode: socket
bot_user_id: ${HOTPLEX_SLACK_BOT_USER_ID}
system_prompt: |
  You are a helpful coding assistant.
```

```bash
export HOTPLEX_SLACK_BOT_USER_ID=B12345
export HOTPLEX_SLACK_BOT_TOKEN=xoxb-...
export HOTPLEX_SLACK_APP_TOKEN=xapp-...
hotplexd --config chatapps/configs
```

## Documentation

| Resource | Description |
|----------|-------------|
| [Architecture Deep Dive](docs/architecture.md) | System design, security protocols, session management |
| [SDK Developer Guide](docs/sdk-guide.md) | Complete Go SDK reference |
| [ChatApps Manual](chatapps/README.md) | Multi-platform integration (Slack, DingTalk, Feishu) |
| [Slack Beginner Guide](docs/chatapps/slack-setup-beginner.md) | Zero-to-Hero Slack setup |
| [Docker Multi-Bot Deployment](docs/docker-multi-bot-deployment.md) | Run multiple bots with one command |
| [Docker Deployment](docs/docker-deployment.md) | Container and Kubernetes deployment |
| [Production Guide](docs/production-guide.md) | Production best practices |

## Security

HotPlex employs defense-in-depth security:

| Layer | Implementation | Protection |
|-------|----------------|------------|
| **Tool Governance** | `AllowedTools` config | Restrict agent capabilities |
| **Danger WAF** | Regex interception | Block `rm -rf /`, `mkfs`, `dd` |
| **Process Isolation** | PGID-based termination | No orphaned processes |
| **Filesystem Jail** | WorkDir lockdown | Confined to project root |

## Contributing

We welcome contributions! Please ensure CI passes:

```bash
make lint    # Run golangci-lint
make test    # Run unit tests
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

Released under the [MIT License](LICENSE).

---

<div align="center">
  <i>Built for the AI Engineering community.</i>
</div>
