<div align="center">
  <img src=".github/assets/hotplex-logo.svg" alt="hotplex" width="160"/>
  <h1>hotplex</h1>
  <p><b>High-Performance Process Multiplexer for AI CLI Agents</b></p>
  <p><i>From 5000ms 🐢 to 200ms 🚀 — keep your AI agents hot.</i></p>

  <p>
    <a href="https://github.com/hrygo/hotplex/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/hrygo/hotplex/ci.yml?branch=main&style=for-the-badge&logo=github&label=Build" alt="Build Status"></a>
    <a href="https://github.com/hrygo/hotplex/releases"><img src="https://img.shields.io/github/v/release/hrygo/hotplex?style=for-the-badge&logo=go&color=00ADD8" alt="Latest Release"></a>
    <a href="https://pkg.go.dev/github.com/hrygo/hotplex"><img src="https://img.shields.io/badge/Go-Reference-00ADD8?style=for-the-badge&logo=go" alt="Go Reference"></a>
    <a href="https://goreportcard.com/report/github.com/hrygo/hotplex"><img src="https://goreportcard.com/badge/github.com/hrygo/hotplex?style=for-the-badge" alt="Go Report Card"></a>
    <a href="LICENSE"><img src="https://img.shields.io/github/license/hrygo/hotplex?style=for-the-badge&color=blue" alt="License"></a>
  </p>
  <p>
    <b>English</b> • <a href="README_zh.md">简体中文</a>
  </p>
</div>

<br/>

## ⚡ What is hotplex?

**hotplex** solves the **"Cold Start" problem** for heavy AI CLI agents like Claude Code, Aider, and OpenCode. 
Instead of spawning a new process and initializing the Node.js or Python runtime for every request, hotplex maintains a persistent, thread-safe process pool. This enables **millisecond-level response times** and **full-duplex async streaming** for seamless integration with web backends and orchestrators.

<div align="center">
  <img src="docs/images/features.svg" alt="hotplex Features Outline" width="100%">
</div>

### Why hotplex?
- 🚀 **200ms Hot-Start**: Instant response matching API-level latencies.
- ♻️ **Session Pool**: Managed OS process pool with automatic Garbage Collection cleanup.
- 🔒 **Sandboxed Execution**: Built-in Danger Detector (WAF) and Process Group ID (PGID) Isolation.
- 🔌 **Full-Duplex I/O**: Asynchronous streaming for real-time `stdin`, `stdout`, and `stderr`.
- 🛠️ **Dual Mode**: Embed transparently as a **Go SDK** or deploy as a **WebSocket Gateway**.

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
  <img src="docs/images/async-stream.svg" alt="hotplex Async Stream Engine" width="90%">
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
    "github.com/hrygo/hotplex/pkg/hotplex"
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

## 🛡️ Security Posture

CLI Agents run raw shell commands generated by LLMs. **Security must not be an afterthought.** hotplex employs a deep defense-in-depth model:

| Layer                      | Implementation                                 | Defense Capability                                                  |
| :------------------------- | :--------------------------------------------- | :------------------------------------------------------------------ |
| **I. Tool Governance**     | `AllowedTools` configuration array             | Restricts agent's internal tool registry capabilities precisely     |
| **II. Danger WAF**         | Regex & Command string interception            | Hard blocks destructive commands like `rm -rf /`, `mkfs`, `dd`      |
| **III. Process Isolation** | `SIGKILL` routed via Process Group ID (`PGID`) | Prevents orphaned background daemons or zombie process leaks        |
| **IV. Filesystem Jail**    | Context Path Lockdown (`WorkDir`)              | Constrains the agent's view/edit scope strictly to the project root |

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

- [ ] **Provider Abstraction**: Expand beyond Claude Code to native adapters for Aider, OpenCode, Docker-based runtimes.
- [ ] **Remote Execution Hooks**: Secure SSH/Docker payload delivery for isolated sandbox execution.
- [ ] **Introspection API**: A unified REST interface to list, manage, and forcibly terminate active sessions.
- [ ] **Framework Integration**: Native Firebase Genkit & LangChain plugin support.

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
