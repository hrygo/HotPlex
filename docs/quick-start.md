*Read this in other languages: [English](quick-start.md), [简体中文](quick-start_zh.md).*

# Quick Start Guide

Get up and running with HotPlex in 5 minutes.

## Core Concepts

HotPlex is an **AI Agent Runtime** with multiple access channels:

| Access Channel | Use Case | Recommendation |
|---------------|----------|----------------|
| **ChatApps (Slack/Telegram/Feishu/etc.)** | Production, Multi-user, Natural Interaction | ⭐⭐⭐ Recommended |
| Go SDK | Embedded Integration, Custom Workflows | ⭐⭐ |
| Standalone Server | Multi-language Clients, Microservices | ⭐⭐ |
| Python SDK | Quick Prototyping, Data Science | ⭐ |

**ChatApps is HotPlex's primary access channel**: Through platforms like Slack, Telegram, and Feishu, users can interact with AI agents just like chatting with colleagues - no installation or configuration needed.

---

## Prerequisites

Before starting, ensure you have:

1. **Go 1.25+** installed
2. **Claude Code CLI** or **OpenCode CLI** installed and authenticated

### Install Claude Code CLI

```bash
# macOS / Linux / WSL
curl -fsSL https://claude.ai/install.sh | bash

# Authenticate
claude auth
```

### Install OpenCode CLI

```bash
# npm
npm install -g @opencode/opencode

# Or use Homebrew
brew install opencode
```

---

## Option 1: ChatApps Platform Integration (Recommended ⭐)

Interact with AI agents directly through Slack, Telegram, Feishu, and other messaging platforms. This is HotPlex's **primary access method**, ideal for production environments.

### Supported Platforms

| Platform | Protocol | Status |
|----------|----------|--------|
| **Slack** | Socket Mode + Web API | ✅ Stable |
| **Telegram** | Bot API | ✅ Stable |
| **Feishu** | Custom Bot | ✅ Stable |
| **DingTalk** | Callback + Webhook | ✅ Stable |
| **Discord** | Bot API | 🔄 In Development |
| **WhatsApp** | Business API | 🔄 In Development |

### Step 1: Configure Environment Variables

```bash
# Example: Slack
export SLACK_BOT_TOKEN=xoxb-xxx-xxx-xxx
export SLACK_APP_TOKEN=xapp-xxx-xxx-xxx
export SLACK_SIGNING_SECRET=xxx
```

### Step 2: Start the Service

```bash
# Method 1: Use --config flag to specify config directory (recommended, highest priority)
hotplexd --config chatapps/configs

# Method 2: Use environment variables
export CHATAPPS_CONFIG_DIR=chatapps/configs
export CHATAPPS_ENABLED=true
hotplexd
```

→ Make sure platform config files exist (e.g., `slack.yaml`)

### Step 3: Start Conversing

Example with Slack:

1. Install your App in the Slack workspace
2. @mention your bot in a channel or use slash command `/ai`
3. Send messages just like talking to a colleague

```
User: @hotplex write me a Go HTTP server
AI: Sure, here's a simple Go HTTP server example...
```

### Slack Special Features

- **Block Kit UI**: Rich text messages, button interactions
- **Native Streaming**: See AI output in real-time
- **Assistant Status**: Visual indicator when AI is thinking/responding
- **Slash Commands**: `/ai [question]` for quick queries

---

## Option 2: Go SDK (Recommended ⭐⭐)

### Step 1: Install

```bash
go get github.com/hrygo/hotplex
```

### Step 2: Create `main.go`

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/hrygo/hotplex"
)

func main() {
    // Initialize the engine
    engine, err := hotplex.NewEngine(hotplex.EngineOptions{
        Timeout:        5 * time.Minute,
        PermissionMode: "bypass-permissions",
    })
    if err != nil {
        panic(err)
    }
    defer engine.Close()

    // Configure the session
    cfg := &hotplex.Config{
        WorkDir:   "/tmp/hotplex-demo",
        SessionID: "my-first-session",
    }

    // Execute a prompt
    ctx := context.Background()
    err = engine.Execute(ctx, cfg, "Write a hello world in Go", 
        func(eventType string, data any) error {
            if eventType == "answer" {
                fmt.Print(data)
            }
            return nil
        })
    
    if err != nil {
        fmt.Printf("Error: %v\n", err)
    }
}
```

### Step 3: Run

```bash
go run main.go
```

---

## Option 3: Standalone Server

Run HotPlex as a standalone server for multi-language clients.

### Step 1: Build

```bash
git clone https://github.com/hrygo/hotplex.git
cd hotplex
make build
```

### Step 2: Run

```bash
PORT=8080 ./dist/hotplexd
```

### Step 3: Connect

**WebSocket (any language):**
```
ws://localhost:8080/ws/v1/agent
```

**OpenCode HTTP/SSE:**
```
http://localhost:8080
```

---

## Option 4: Python SDK

### Step 1: Install

```bash
pip install hotplex
```

### Step 2: Create `main.py`

```python
from hotplex import HotPlexClient, Config

with HotPlexClient(url="ws://localhost:8080/ws/v1/agent") as client:
    for event in client.execute_stream(
        prompt="Write a hello world in Python",
        config=Config(work_dir="/tmp", session_id="py-demo")
    ):
        if event.type == "answer":
            print(event.data, end="")
```

### Step 3: Run

```bash
python main.py
```

---

## What's Next?

- [ChatApps Architecture Overview](chatapps/chatapps-architecture.md) - Multi-platform integration design
- [Slack Integration Manual](chatapps/chatapps-slack-manual.md) - Complete Slack setup guide
- [Feishu Integration Manual](chatapps/chatapps-feishu-manual.md) - Feishu setup guide
- [Architecture Deep Dive](architecture.md) - Learn how HotPlex works
- [SDK Developer Guide](sdk-guide.md) - Complete SDK reference
- [Examples](../_examples/) - More code examples
- [Benchmark Report](benchmark-report.md) - Performance data

---

## Common Issues

### "claude: command not found"

Install Claude Code CLI:
```bash
curl -fsSL https://claude.ai/install.sh | bash
claude auth
```

### "Permission denied"

Make sure the work directory exists and is writable:
```bash
mkdir -p /tmp/hotplex-demo
```

### "Session not found"

Sessions are identified by `SessionID`. Use the same ID for multi-turn conversations.

---

## Need Help?

- [GitHub Issues](https://github.com/hrygo/hotplex/issues)
- [Discussions](https://github.com/hrygo/hotplex/discussions)
