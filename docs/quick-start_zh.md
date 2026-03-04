*Read this in other languages: [English](quick-start.md), [简体中文](quick-start_zh.md).*

# 快速入门指南

在 5 分钟内快速启动并运行 HotPlex。

## 核心概念

HotPlex 是一个 **AI 智能体运行时 (Agent Runtime)**，支持多种接入方式：

| 接入方式 | 适用场景 | 推荐度 |
|---------|---------|--------|
| **ChatApps (Slack/Telegram/飞书等)** | 生产环境、多用户协作、自然交互 | ⭐⭐⭐ 推荐 |
| Go SDK | 嵌入式集成、自定义工作流 | ⭐⭐ |
| 独立服务端 | 多语言客户端、微服务架构 | ⭐⭐ |
| Python SDK | 快速原型、数据科学集成 | ⭐ |

**ChatApps 是 HotPlex 的主要接入渠道**：通过 Slack、Telegram、飞书等即时通讯平台，用户可以像与同事聊天一样与 AI 智能体交互，无需任何安装配置。

---

## 前置要求

在开始之前，请确保您已安装：

1. **Go 1.25+** 已安装
2. 已安装并认证的 **Claude Code CLI** 或 **OpenCode CLI**

### 安装 Claude Code CLI

```bash
# macOS / Linux / WSL
curl -fsSL https://claude.ai/install.sh | bash

# 认证
claude auth
```

### 安装 OpenCode CLI

```bash
# 使用 npm
npm install -g @opencode/opencode

# 或使用 Homebrew
brew install opencode
```

---

## 选项 1：ChatApps 平台接入 (推荐 ⭐)

通过 Slack、Telegram、飞书等即时通讯平台直接与 AI 智能体对话。这是 HotPlex 的**主要接入方式**，适合生产环境使用。

### 支持的平台

| 平台 | 协议 | 状态 |
|------|------|------|
| **Slack** | Socket Mode + Web API | ✅ 稳定 |
| **Telegram** | Bot API | ✅ 稳定 |
| **飞书** | 自定义机器人 | ✅ 稳定 |
| **DingTalk** | 回调 + Webhook | ✅ 稳定 |
| **Discord** | Bot API | 🔄 开发中 |
| **WhatsApp** | Business API | 🔄 开发中 |

### 第一步：配置环境变量

```bash
# Slack 为例
export SLACK_BOT_TOKEN=xoxb-xxx-xxx-xxx
export SLACK_APP_TOKEN=xapp-xxx-xxx-xxx
export SLACK_SIGNING_SECRET=xxx
```

### 第二步：启动服务

```bash
# 方式 1：使用 --config 参数指定配置目录（推荐，优先级最高）
hotplexd --config chatapps/configs

# 方式 2：使用环境变量
export CHATAPPS_CONFIG_DIR=chatapps/configs
export CHATAPPS_ENABLED=true
hotplexd
```

→ 确保配置目录下有平台配置文件（如 `slack.yaml`）

### 第三步：在平台中开始对话

以 Slack 为例：

1. 在 Slack 工作区安装你的 App
2. 在频道中 @你的机器人 或使用斜杠命令 `/ai`
3. 像与同事一样发送消息，AI 即时响应

```
用户: @hotplex 帮我写一个 Go 的 HTTP 服务器
AI: 好的，这是一个简单的 Go HTTP 服务器示例...
```

### Slack 特色功能

- **Block Kit UI**：富文本消息、按钮交互
- **原生流式输出**：实时看到 AI 生成内容
- **Assistant Status**：实时显示 AI 正在思考/响应
- **斜杠命令**：`/ai [问题]` 快速提问

---

## 选项 2：Go SDK (推荐 ⭐⭐)

### 第一步：安装

```bash
go get github.com/hrygo/hotplex
```

### 第二步：创建 `main.go`

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/hrygo/hotplex"
)

func main() {
    // 初始化引擎
    engine, err := hotplex.NewEngine(hotplex.EngineOptions{
        Timeout:        5 * time.Minute,
        PermissionMode: "bypass-permissions",
    })
    if err != nil {
        panic(err)
    }
    defer engine.Close()

    // 配置会话
    cfg := &hotplex.Config{
        WorkDir:   "/tmp/hotplex-demo",
        SessionID: "my-first-session",
    }

    // 执行提示词
    ctx := context.Background()
    err = engine.Execute(ctx, cfg, "用 Go 写一个 hello world", 
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

### 第三步：运行

```bash
go run main.go
```

---

## 选项 3：独立服务端

将 HotPlex 作为独立服务端运行，支持多语言客户端。

### 第一步：构建

```bash
git clone https://github.com/hrygo/hotplex.git
cd hotplex
make build
```

### 第二步：运行

```bash
PORT=8080 ./dist/hotplexd
```

### 第三步：连接

**WebSocket (任何语言):**
```
ws://localhost:8080/ws/v1/agent
```

**OpenCode HTTP/SSE:**
```
http://localhost:8080
```

---

## 选项 4：Python SDK

### 第一步：安装

```bash
pip install hotplex
```

### 第二步：创建 `main.py`

```python
from hotplex import HotPlexClient, Config

with HotPlexClient(url="ws://localhost:8080/ws/v1/agent") as client:
    for event in client.execute_stream(
        prompt="用 Python 写一个 hello world",
        config=Config(work_dir="/tmp", session_id="py-demo")
    ):
        if event.type == "answer":
            print(event.data, end="")
```

### 第三步：运行

```bash
python main.py
```

---

## 下一步

- [ChatApps 架构概览](chatapps/chatapps-architecture.md) - 了解多平台接入设计
- [Slack 集成手册](chatapps/chatapps-slack-manual-zh.md) - Slack 完整配置指南
- [飞书集成手册](chatapps/chatapps-feishu-manual.md) - 飞书配置指南
- [架构深度解析](architecture_zh.md) - 了解 HotPlex 的工作原理
- [SDK 开发者指南](sdk-guide_zh.md) - 完整的 SDK 参考
- [代码示例](../_examples/) - 更多代码示例
- [基准测试报告](benchmark-report_zh.md) - 性能数据

---

## 常见问题

### "claude: command not found"

安装 Claude Code CLI:
```bash
curl -fsSL https://claude.ai/install.sh | bash
claude auth
```

### "Permission denied"

确保工作目录存在且可写:
```bash
mkdir -p /tmp/hotplex-demo
```

### "Session not found"

会话通过 `SessionID` 标识。在多轮对话中使用相同的 ID。

---

## 需要帮助？

- [GitHub Issues](https://github.com/hrygo/hotplex/issues)
- [Discussions](https://github.com/hrygo/hotplex/discussions)
