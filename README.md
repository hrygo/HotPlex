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
      <img src="https://img.shields.io/badge/go-report-brightgreen?style=flat-square" alt="Go Report">
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

## ⚡ Quick Start

### Prerequisites

- **Go**: 1.25+
- **AI Tool**: [Claude Code](https://github.com/anthropics/claude-code) or [OpenCode CLI](https://github.com/hrygo/opencode) installed on your host.

### Install & Run

```bash
# 1. One-click install
curl -sL https://raw.githubusercontent.com/hrygo/hotplex/main/install.sh | bash

# 2. Start your first session
hotplexd --config chatapps/configs
```

---

## 💎 Visual Showcase

![HotPlex Features](docs/images/features.svg)

---

## 🎯 Our Focus: AI Agent "Control Plane"

Existing AI agents are powerful but often lack the **runtime stability** required by production services. HotPlex fills this gap by providing:

1.  **Process Life-support**: Turns ephemeral CLI turns into persistent, stateful sessions.
2.  **Safety Interception**: A programmable regex-based firewall for shell instruction.
3.  **High-Efficiency Multiplexing**: Scale to hundreds of concurrent AI sessions through optimal process reuse.

---

## 💡 Standard Use Cases

| Scenario                | How HotPlex Helps                                                                        |
| :---------------------- | :--------------------------------------------------------------------------------------- |
| **AI Coding Assistant** | Embed AI directly into your VS Code / IDE terminal with persistent turn history.         |
| **Autopilot Ops**       | Feed system logs to HotPlex and let it execute remediation scripts with manual approval. |
| **Multi-Agent Bus**     | Centralize multiple AI CLI tools behind a single unified WebSocket/SSE gateway.          |
| **Enterprise ChatOps**  | Connect local AI agents to Slack/Feishu with enterprise-grade stability.                 |

---

## 🚀 Features

| Feature                   | Description                                                                |
| ------------------------- | -------------------------------------------------------------------------- |
| **Session Pooling**       | Long-lived CLI processes with instant reconnection                         |
| **Full-Duplex Streaming** | Sub-second token delivery via Go channels                                  |
| **Regex WAF**             | Block destructive commands (`rm -rf /`, `mkfs`, etc.)                      |
| **PGID Isolation**        | Clean process termination, no zombie processes                             |
| **ChatApps**              | Slack (Block Kit, Streaming, Assistant Status), Telegram, Feishu, DingTalk |
| **Go SDK**                | Embed directly in your Go application with zero overhead                   |
| **WebSocket Gateway**     | Language-agnostic access via `hotplexd` daemon                             |
| **OpenTelemetry**         | Built-in metrics and tracing support                                       |

---

## 🏛 Architecture & Security

HotPlex employs a defense-in-depth security model alongside a high-concurrency session topology.

![HotPlex Security Architecture](docs/images/hotplex-security.svg)

![HotPlex Topology](docs/images/topology.svg)

| Layer                 | Implementation         | Protection                     |
| --------------------- | ---------------------- | ------------------------------ |
| **Tool Governance**   | `AllowedTools` config  | Restrict agent capabilities    |
| **Danger WAF**        | Regex interception     | Block `rm -rf /`, `mkfs`, `dd` |
| **Process Isolation** | PGID-based termination | No orphaned processes          |
| **Filesystem Jail**   | WorkDir lockdown       | Confined to project root       |
| **Container Sandbox** | Docker (BaaC)          | OS-level isolation & limits    |

---

## 🛠 Usage Examples

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

---

## 📚 Documentation

| Resource                                                | Description                                           |
| :------------------------------------------------------ | :---------------------------------------------------- |
| [Architecture Deep Dive](docs/architecture.md)          | System design, security protocols, session management |
| [SDK Developer Guide](docs/sdk-guide.md)                | Complete Go SDK reference                             |
| [ChatApps Manual](chatapps/README.md)                   | Multi-platform integration (Slack, DingTalk, Feishu)  |
| [Docker Multi-Bot](docs/docker-multi-bot-deployment.md) | Run multiple bots with one command                    |

---

## 🤝 Community & Contributing

- **Bugs/Features**: Please use [GitHub Issues](https://github.com/hrygo/hotplex/issues).
- **Discussions**: Ask questions or share ideas in [GitHub Discussions](https://github.com/hrygo/hotplex/discussions).
- **Contributing**: We welcome contributions! Please ensure CI passes (`make lint`, `make test`). See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

---

## 📄 License

Released under the [MIT License](LICENSE).

---

<div align="center">
  <img src="docs/images/hotplex_beaver_final.png" alt="HotPlex Mascot" width="120"/>
  <br/>
  <i>Built for the AI Engineering community.</i>
</div>
