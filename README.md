<div align="center">
  <img src=".github/assets/hotplex-logo.svg" alt="hotplex" width="160" style="background: #0D1117; border-radius: 24px; padding: 20px;"/>
  <h1>hotplex</h1>
  <p><b>Production-Grade Control Plane for Elite AI CLI Agents</b></p>
  <p><i>Don't reinvent the wheel. Leverage powerful tools like Claude Code with millisecond latency, secure isolation, and full-duplex integration.</i></p>

  <p>
    <a href="https://github.com/hrygo/hotplex/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/hrygo/hotplex/ci.yml?branch=main&style=for-the-badge&logo=github&label=Build" alt="Build Status"></a>
    <a href="https://github.com/hrygo/hotplex/releases"><img src="https://img.shields.io/github/v/release/hrygo/hotplex?style=for-the-badge&logo=go&color=00ADD8" alt="Latest Release"></a>
    <a href="https://pkg.go.dev/github.com/hrygo/hotplex"><img src="https://img.shields.io/badge/Go-Reference-00ADD8?style=for-the-badge&logo=go" alt="Go Reference"></a>
    <a href="https://goreportcard.com/report/github.com/hrygo/hotplex"><img src="https://goreportcard.com/badge/github.com/hrygo/hotplex?style=for-the-badge" alt="Go Report Card"></a>
    <a href="LICENSE"><img src="https://img.shields.io/github/license/hrygo/hotplex?style=for-the-badge&color=blue" alt="License"></a>
  </p>
  <p>
    <b>English</b> • <a href="README_zh.md">简体中文</a> • <a href="docs/sdk-guide.md">SDK Guide</a>
  </p>
</div>

<br/>

## ⚡ What is hotplex?

**hotplex** is more than just a process multiplexer; it is the **"Last Mile" Adapter** for AI agent engineering.

Our **First Principle** is: **Leverage existing, state-of-the-art AI CLI tools (like Claude Code, Aider, OpenCode) and upgrade them from "terminal tools for humans" to "cloud-native operators for systems."**

Developers no longer need to build complex agent runtimes from scratch or reinvent file manipulation logic. By maintaining a persistent, thread-safe process pool, hotplex eliminates the interaction gap caused by cold starts and provides a unified security framework and streaming I/O abstraction. Whether building a personal AI assistant or enterprise-grade CI/CD pipelines, hotplex brings elite agent capabilities to your infrastructure with minimal overhead.

<div align="center">
  <img src="docs/images/features.svg" alt="hotplex Features Outline" width="100%">
</div>

### Why hotplex?
- 🧩 **Leverage vs. Build**: Directly integrate elite tools like Claude Code, skipping months of agent logic development.
- 🚀 **200ms Instant Response**: Eliminate Node.js/Python spin-up latencies for a fluid, real-time experience.
- ♻️ **Stateful Session Pool**: Managed lifecycle with persistent VFS state and context across requests.
- 🔒 **Security Control Center**: Built-in Command WAF and PGID isolation to provide a hard sandbox for AI operations.
- 🔌 **Production Ready**: Embed via **Go SDK** or deploy as a **WebSocket Gateway** for modern microservice architectures.

---

## 🏗️ Architecture Design

hotplex decouples the **access layer** from the **execution engine layer**, leveraging bounded Go channels and WaitGroups to achieve deterministic, safe concurrent I/O handling at scale.

### 1. System Topology
<div align="center">
  <img src="docs/images/topology.svg" alt="hotplex System Architecture" width="90%">
</div>

- **Access Layer**: Supports native Go SDK calls or remote WebSocket connections (`hotplexd`).
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
        TaskSystemPrompt: "You are a senior Go systems engineer.",
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

### Option B: Standalone WebSocket Gateway
Operate `hotplexd` as an infrastructure daemon to serve cross-language clients (React, Node, Python, Rust).

**Build & Run:**
```bash
make build
./bin/hotplexd --port 8080 --allowed-tools "Bash,Edit"
```

**Connect & Control:**
Connect your websocket client to `ws://localhost:8080/ws/v1/agent`. Check out `_examples/websocket_client/` for a fully functional web demo implementation.

---

## 📖 Detailed Documentation

- **[Architecture Deep Dive](docs/architecture.md)**: Explore the inner workings, security protocols, and session management logic.
- **[SDK Developer Guide](docs/sdk-guide.md)**: A comprehensive manual for integrating HotPlex into your Go applications.

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

We are actively evolving hotplex to become the definitive execution engine for the Local AI ecosystem:

- [x] **Provider Abstraction**: Decoupled engine from specific CLI tools; native support for Claude Code and OpenCode.
- [ ] **L2/L3 Isolation**: Integrating Linux Namespaces (PID/Net) and WASM-based execution sandboxes.
- [ ] **Event Hooks**: Plugin system for custom audit sinks, session telemetry, and real-time notifications (Slack/Webhook).
- [ ] **Observability (OTel)**: Native OpenTelemetry tracing for the entire prompt-to-tool execution pipeline.
- [ ] **Remote Execution**: Secure SSH/Docker payload delivery for isolated remote sandbox execution.

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
