# 🔥 HotPlex (Hot-Multiplexer)

> **From 5000ms 🐢 to 200ms 🚀. HotPlex keeps your AI agents hot.**

*Read this in other languages: [English](README.md), [简体中文](README_zh.md).*

**HotPlex** is a high-performance **Process Multiplexer** designed specifically for running heavy, local AI CLI Agents (like Claude Code, OpenCode, Aider) in long-lived server or web environments. 

It solves the "Cold Start" problem by keeping the underlying heavy Node.js or Python CLI processes alive and routing concurrent request streams (Hot-Multiplexing) into their Stdin/Stdout pipes.

## 🚀 Why HotPlex?

Running local CLI agents from a backend service (like a Go API) usually means spawning a new OS process for *every single interaction*. 

*   **The Problem:** Tools like `claude` (Claude Code) are heavy Node.js applications. Firing up `npx @anthropic-ai/claude-code` takes **3-5 seconds** just to boot up the V8 engine, read the filesystem context, and authenticate. For a real-time web UI, this latency makes the agent feel incredibly slow and unresponsive.
*   **The Solution:** HotPlex boots the CLI process *once* per user/session, keeps it alive in the background (within a secure `pgid`), and establishes a persistent pipeline. When the user sends a new message, HotPlex instantly injects it via `Stdin` and streams the JSON responses back via `Stdout`. Latency drops from **5000ms to < 200ms**.

## 💡 Vision & Application Scenarios

The original driving force behind HotPlex is to **empower AI applications to effortlessly integrate powerful CLI agents** (like Claude Code) as their external "muscles." Instead of reinventing the wheel to build coding, execution, and file-manipulation capabilities from scratch, your AI app can instantly borrow the immense capabilities of these mature CLI tools.

Key Application Scenarios include:

- **Web-based AI Agents**: Build a fully functional Web version of Claude Code. Users interact via a sleek browser UI while HotPlex reliably manages the persistent Claude CLI process in a sandboxed backend environment.
- **DevOps Toolchains**: Integrate AI directly into your DevOps workflows. Have an agent autonomously execute shell scripts, read Kubernetes logs, and troubleshoot infrastructure issues over a persistent HotPlex session.
- **CI/CD Pipelines**: Embed intelligent code review, automated testing, and dynamic bug fixing right into your Jenkins, GitLab, or GitHub Actions pipelines without the latency overhead of spinning up heavy Node.js tools repeatedly.
- **Intelligent Operations (AIOps)**: Create intelligent ops-bots that continuously monitor systems, analyze incident reports, and autonomously execute safe remediation commands via a controlled, hot-multiplexed terminal session.

## 🛠 Features

- **Blazing Fast Hot-Starts:** Instant response times after the initial boot.
- **Session Pooling (GC):** Automatically tracks idle processes and terminates them after a configurable timeout (default 30m) to save RAM.
- **Native Tool Constraints (v0.2.0+):** Hard-restrict agent capabilities (e.g., disabling `Bash` or `Internet` tools) at the engine level using native CLI flags.
- **WebSocket Gateway:** Includes a standalone batteries-included server (`hotplexd`) that exposes the multiplexer natively over WebSockets.
- **Native Go SDK:** Import `github.com/hrygo/hotplex/pkg/hotplex` to embed the engine directly into your Go backend.
- **Regex Security Firewall:** Built-in `danger.go` pre-flight interceptor blocks destructive commands (`rm -rf /`, fork bombs, etc.) before they even reach the agent.
- **Context Isolation:** Uses UUID v5 deterministic namespaces to guarantee sandboxed session isolation.

### 📦 Architecture

HotPlex is designed with a two-tier architecture:

![HotPlex Architecture](docs/images/topology.svg)

1.  **Core SDK (`pkg/hotplex`)**: The engine itself. It provides the `Engine` Singleton, `SessionPool`, and `Detector` (Security Firewall). It expects JSON streams from the CLI and emits strongly-typed Go events.
2.  **Standalone Server (`cmd/hotplexd`)**: A lightweight wrapper around the SDK that exposes it over standard WebSockets.

#### 🌊 Asynchronous Event Flow

HotPlex leverages Go's concurrency for true full-duplex streaming:

![HotPlex Event Flow](docs/images/async-stream.svg)

*Note: The current MVP is deeply optimized for **Claude Code's** full-duplex JSON protocol (`--input-format stream-json` & `--output-format stream-json`) but is designed with a future `Provider` interface abstraction in mind.*

## ⚡ Quick Start

### 1. Running the Standalone WebSocket Server

If you just want to run the server and connect to it from a frontend or Python script:

```bash
# Install Claude Code (Recommended: Native Installer)
# macOS / Linux / WSL:
curl -fsSL https://claude.ai/install.sh | bash

# OR via Homebrew:
brew install claude-code

# OR via NPM (legacy):
npm install -g @anthropic-ai/claude-code

# Build and run the daemon
cd cmd/hotplexd
go build -o hotplexd main.go
./hotplexd
```
Server runs on `ws://localhost:8080/ws/v1/agent`. Check `_examples/websocket_client/client.js` for an integration demo.

### 2. Native Go SDK Integration

Install the library:
```bash
go get github.com/hrygo/hotplex
```

Import and use:
```go
import "github.com/hrygo/hotplex/pkg/hotplex"

opts := hotplex.EngineOptions{
    Timeout: 5 * time.Minute,
    Logger:  logger,
    PermissionMode: "bypass-permissions", // Modern CLI permission handling
    AllowedTools: []string{"Bash", "Edit"}, // Restrict capabilities at the Engine level (v0.2.0+)
}

engine, _ := hotplex.NewEngine(opts)
defer engine.Close()

cfg := &hotplex.Config{
    WorkDir:          "/tmp/sandbox",
    SessionID:        "user_123_session", // Deterministic Hot-Multiplexing ID
    TaskSystemPrompt: "You are a senior developer.",
}

ctx := context.Background()

// 1. Send Prompt & handle streaming callback
err := engine.Execute(ctx, cfg, "Refactor the main.go file", func(eventType string, data any) error {
    if eventType == "answer" {
         fmt.Println("Agent is responding...")
    }
    return nil
})
```

## 🔒 Security Posture

HotPlex executes LLM-generated shell code on your machine. **Use with caution.**

We mitigate risks via:
1.  **Native Capability Governance:** As of v0.2.0, we prioritize native tool restrictions (`AllowedTools`) over unstable path interception. This ensures the agent is only granted the specific muscles it needs.
2.  **Danger Detector (WAF):** A regex-based layer intercepts and blocks destructive patterns (e.g., `mkfs`, `dd`, `rm -rf /`) before they reach the OS.
3.  **Process Groups (PGID):** When a session is terminated, HotPlex sends `SIGKILL` to the entire negative Process Group ID (`-pgid`), guaranteeing that the CLI and all child/grandchild processes are instantly eradicated.
4.  **Context WorkDirs:** The agent is physically locked to the `WorkDir` provided in the Config.

## Roadmap
- [ ] Provider interface extraction (support for `OpenCode`)
- [ ] Remote Docker sandbox execution (replacing local OS execution)
- [ ] REST API endpoints for session introspection
