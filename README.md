<div align="center">
  <img src="docs/images/hotplex_beaver_banner.webp" alt="hotplex" width="100%"/>
  <h1>hotplex</h1>
  <p><b>Transforming AI CLI Agents into Production-Ready Interactive Services</b></p>
  <p><i>Break the limits of one-off CLI tasks. Leverage full-duplex, stateful sessions for instant interaction, secure isolation, and effortless system integration.</i></p>

  <p>
    <a href="https://github.com/hrygo/hotplex/releases"><img src="https://img.shields.io/github/v/release/hrygo/hotplex?style=for-the-badge&logo=go&color=00ADD8" alt="Latest Release"></a>
    <a href="https://goreportcard.com/report/github.com/hrygo/hotplex"><img src="https://goreportcard.com/badge/github.com/hrygo/hotplex?style=for-the-badge" alt="Go Report Card"></a>
    <a href="LICENSE"><img src="https://img.shields.io/github/license/hrygo/hotplex?style=for-the-badge&color=blue" alt="License"></a>
  </p>
  <p>
    <b>English</b> • <a href="README_zh.md">简体中文</a> • <a href="docs/sdk-guide.md">SDK Guide</a> • <a href="https://hrygo.github.io/hotplex/">Docs Site</a>
  </p>
</div>

<br/>

## ⚡ What is hotplex?

**hotplex** is more than just a process multiplexer; it is the **Strategic Bridge** for AI agent engineering.

Our **First Principle** is: **Upgrade existing AI CLI tools (like Claude Code, OpenCode) from "human-oriented terminal tools" into "system-ready, long-lived interactive services" (Cli-as-a-Service).**

Developers no longer need to endure the multi-second latency of restarting CLI environments in headless mode. By maintaining a persistent, full-duplex session pool, hotplex eliminates the interaction gap caused by cold starts and provides a unified integration layer. Whether building professional AI products or automated pipelines, hotplex makes elite agent capabilities as easy to call as a standard API.

<div align="center">
  <img src="docs/images/features.svg" alt="hotplex Features Outline" width="100%">
</div>

### Why hotplex?
- 🔄 **Cli-as-a-Service**: Shift from "run-and-exit" to persistent sessions with continuous instruction flow and context preservation.
- 🧩 **Ease of Integration**: A unified Go SDK and protocol gateway that plugs top-tier Agent capabilities into your product instantly.
- 🚀 **Zero Spin-up Overhead**: Eliminate the long wait times for Node.js/Python runtimes to provide sub-second user feedback.
- 🛡️ **Fast & Balanced Security**: Command-level WAF and PGID isolation provide a "protective glove" for AI shell operations.
- 💬 **ChatApps Integration**: Connect HotPlex to platforms like **Slack** (native Block Kit support), enabling AI collaboration directly in your team's workspace.
- 🔌 **Ready for Scale**: Support for native Go embedding or standalone Proxy mode with WebSocket and OpenCode-compatible protocols.

---

## 🏗️ Architecture Design

hotplex decouples the **access layer** from the **execution engine layer**, leveraging bounded Go channels and WaitGroups to achieve deterministic, safe concurrent I/O handling at scale.

### 1. System Topology
<div align="center">
  <img src="docs/images/topology.svg" alt="hotplex System Architecture" width="90%">
</div>

- **Access Layer**: Supports native Go SDK calls or remote API connections (`hotplexd`). Includes a dedicated **OpenCode HTTP/SSE compatibility handler**.
- **Engine Layer**: Singleton resource manager managing the session pool, configuration overrides, and security WAF.
- **Process Layer**: Sub-process worker isolated in PGID-level workspaces, locked to specific directory boundaries.

### 2. Full-Duplex Async Streaming
<div align="center">
  <img src="docs/images/async-stream.svg" alt="hotplex Full-Duplex Stream Engine" width="90%">
</div>

Unlike standard RPC or REST request-response cycles, hotplex taps directly into Go's non-blocking concurrency model. `stdin`, `stdout`, and `stderr` streams are piped continuously between the client and child process, ensuring sub-second token delivery from local LLM commands.

---

## 🚀 Quick Start

### Option A: Embed as a Go Library (SDK)
Drop into your Go backend for zero-overhead, memory-level orchestration of CLI agents.

**Install:**
```bash
go get github.com/hrygo/hotplex
```

**Usage Snippet:**
```go
package main

import (
    "context"
    "fmt"
    "time"
    "github.com/hrygo/hotplex"
)

func main() {
    // 1. Initialize engine singleton
    opts := hotplex.EngineOptions{
        Timeout:         5 * time.Minute,
        PermissionMode:  "bypass-permissions",
        AllowedTools:    []string{"Bash", "Edit", "Read", "FileSearch"},
    }
    engine, _ := hotplex.NewEngine(opts)
    defer engine.Close()

    // 2. Configure persistent session routing
    cfg := &hotplex.Config{
        WorkDir:          "/tmp/ai-sandbox",
        SessionID:        "user-123", // Automatically routes to the correct hot process
        TaskInstructions: "You are a senior Go systems engineer.",
    }

    // 3. Execute with streaming callback
    ctx := context.Background()
    err := engine.Execute(ctx, cfg, "Refactor the main.go to improve error handling", 
        func(eventType string, data any) error {
            if eventType == "answer" {
                fmt.Printf("🤖 Agent -> %v\n", data)
            }
            return nil
        })
    if err != nil {
        fmt.Printf("Execution failed: %v\n", err)
    }
}
```

### Option B: Standalone HotPlex Proxy Server
Operate `hotplexd` as an infrastructure daemon to serve cross-language clients (React, Node, Python, Rust) via WebSocket or OpenCode HTTP/SSE.

**Build & Run:**
```bash
make build
PORT=8080 ./dist/hotplexd
```

**Connect & Control:**
- **WebSocket**: Connect to `ws://localhost:8080/ws/v1/agent`.
- **OpenCode (HTTP/SSE)**: Configure your OpenCode client with `baseURL: "http://localhost:8080"`.

---

## 📖 Detailed Documentation

### Core Technical Manuals
- **[ChatApps Manual](chatapps/README.md)**: Multi-platform connector (Slack, Feishu, etc.) and message pipeline.
- **[Engine Manual](engine/README.md)**: Core control plane, process hot-multiplexing, and execution logic.
- **[Provider Manual](provider/README.md)**: AI agent abstraction (Claude Code, OpenCode) and event normalization.
- **[Internal Subsystems](internal/README.md)**: Foundational security WAF, session pooling, and system utilities.

### Guides & Manuals
- **[Architecture Deep Dive](docs/architecture.md)**: Explore the inner workings, security protocols, and session management logic.
- **[SDK Developer Guide](docs/sdk-guide.md)**: A comprehensive manual for integrating HotPlex into your Go applications.
- **[Slack Integration Manual](docs/chatapps/chatapps-slack-manual.md)**: Comprehensive guide for connecting HotPlex to Slack (Chinese).
- **[Quick Start Guide](docs/quick-start.md)**: Step-by-step tutorial for getting started.
- **[Observability Guide](docs/observability-guide.md)**: OpenTelemetry and Prometheus integration.
- **[Docker Deployment](docs/docker-deployment.md)**: Container and Kubernetes deployment.
- **[Production Guide](docs/production-guide.md)**: Production deployment best practices.

---

## 📂 Example Repositories

Explore our ready-to-use examples to accelerated your integration:

- **[go_claude_basic](_examples/go_claude_basic/main.go)**: Quick start with minimal configuration.
- **[go_claude_lifecycle](_examples/go_claude_lifecycle/main.go)**: Multi-turn, session recovery, and PGID management in Claude.
- **[go_opencode_basic](_examples/go_opencode_basic/main.go)**: Minimal OpenCode integration.
- **[go_opencode_lifecycle](_examples/go_opencode_lifecycle/main.go)**: Multi-turn and session persistence in OpenCode.
- **[node_claude_websocket](_examples/node_claude_websocket/enterprise_client.js)**: Full-duplex web integration.

---

## 🛡️ Security Posture

CLI Agents run raw shell commands generated by LLMs. **Security must not be an afterthought.** hotplex employs a deep defense-in-depth model:

| Layer                      | Implementation                                 | Defense Capability                                                  |
| :------------------------- | :--------------------------------------------- | :------------------------------------------------------------------ |
| **I. Tool Governance**     | `AllowedTools` configuration array             | Restricts agent's internal tool registry capabilities precisely     |
| **II. Danger WAF**         | Regex & Command string interception            | Hard blocks destructive commands like `rm -rf /`, `mkfs`, `dd`      |
| **III. Process Isolation** | `SIGKILL` routed via Process Group ID (`PGID`) | Prevents orphaned background daemons or zombie process leaks        |
| **IV. Filesystem Jail**    | Context Path Lockdown (`WorkDir`)              | Constrains the agent's view/edit scope strictly to the project root |

<br/>

<div align="center">
  <img src="docs/images/hotplex-security.svg" alt="hotplex Security Sandbox" width="95%">
</div>

---

## 💡 Use Cases & Scenarios

| Domain                     | Application                                                           | Benefit                                                            |
| :------------------------- | :-------------------------------------------------------------------- | :----------------------------------------------------------------- |
| 🌐 **Web-Based AI Clients** | Running "Claude Code" straight from a browser chat window.            | Maintains conversational state + session context persistently.     |
| 🔧 **DevOps Automation**    | AI-driven bash scripting and live Kubernetes manifest analysis.       | Rapid remote execution without repeated Node/Python spin-up costs. |
| 🚀 **CI/CD Intelligence**   | Smart code review, auto-formatting, and vulnerability auto-patching.  | Integrates effortlessly into GitHub Actions or GitLab CI runners.  |
| 🕵️ **AIOps & Log Triage**   | Continuous pod monitoring with safe, controlled remediation commands. | The regex WAF ensures no accidental production outages by the AI.  |

---

## 🗺️ Roadmap & Vision

We are actively evolving hotplex to become the definitive execution engine for the Local AI ecosystem.

### 🚀 Future (H2 2026)

- [ ] **L2/L3 Isolation**: Integrating Linux Namespaces (PID/Net) and WASM-based execution sandboxes.

---

## 🤝 Contributing

We welcome community contributions! Please ensure your PR passes the CI pipeline.

```bash
# Verify code formatting and linting
make lint

# Run unit tests and race detector
make test
```
Please read our [CONTRIBUTING.md](CONTRIBUTING.md) for architectural guidelines and PR conventions.

---

## 📄 License

hotplex is released under the [MIT License](LICENSE).

<div align="center">
  <i>Built with ❤️ for the AI Engineering community.</i>
</div>
