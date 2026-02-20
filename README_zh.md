# 🔥 HotPlex (Hot-Multiplexer)

> **从 5000ms 🐢 锐减至 200ms 🚀。HotPlex 让你的 AI 智能体时刻保持“热启动”。**

*Read this in other languages: [English](README.md), [简体中文](README_zh.md).*

**HotPlex** 是一个高性能的**进程多路复用器 (Process Multiplexer)**，专为在长生命周期的服务器或 Web 环境中运行繁重的本地 AI CLI 代理（如 Claude Code、OpenCode、Aider）而设计。

它通过在后台保持繁重的 Node.js 或 Python CLI 进程常驻，并将并发的请求流（热重载多路复用）受控地路由到它们的标准输入/输出 (Stdin/Stdout) 管道中，从而彻底解决了“冷启动”问题。

## 🚀 为什么选用 HotPlex？

通常，从后端服务（如 Go API）运行本地 CLI 代理意味着必须为**每一次**交互都生成一个全新的操作系统进程。

*   **问题所在：** 像 `claude` (Claude Code) 这样的工具是重量级的 Node.js 应用。每次执行 `npx @anthropic-ai/claude-code` 都需要花费 **3-5 秒**，仅仅是为了启动 V8 引擎、读取文件系统上下文以及进行身份验证。对于实时的 Web UI 而言，这种延迟会让智能体感觉极其缓慢且不响应。
*   **解决方案：** HotPlex 为每个用户/会话仅仅启动一次 CLI 进程，将其保存在后台（被包裹在安全的进程组 `pgid` 中），并建立持久的双向管道。当用户发送新消息时，HotPlex 会立即通过 `Stdin` 注入消息，并通过 `Stdout` 将 JSON 响应流式传回。延迟从 **5000 毫秒锐减至 200 毫秒以内**。

## 💡 愿景与应用场景

创建 HotPlex 的原始驱动力是为了**赋能 AI 应用程序毫不费力地集成强大的 CLI 代理**（例如 Claude Code），作为其外部的“肌肉”。您的 AI 应用无需从零开始重复造轮子去构建编码、执行和文件操作能力，而是可以直接借用这些成熟 CLI 工具的强大功能。

关键应用场景包括：

- **Web 版 AI 智能体**: 构建全功能 Web 版的 Claude Code。用户通过流畅的浏览器 UI 进行交互，而 HotPlex 在安全沙盒化的后端环境中可靠地管理着持久的 Claude CLI 进程。
- **DevOps 工具链**: 将 AI 直接集成到您的 DevOps 工作流中。让智能体通过持久的 HotPlex 会话自动执行 Shell 脚本、读取 Kubernetes 日志并排查基础设施故障。
- **CI/CD 流水线**: 将智能代码审查、自动化测试和动态漏洞修复直接无缝嵌入您的 Jenkins、GitLab 或 GitHub Actions 流水线中，彻底免去每次重复启动笨重 Node.js 工具带来的延迟开销。
- **智能运维 (AIOps)**: 打造智能运维机器人 (ops-bots)，持续监控系统、分析事件报告，并通过受控的、多路复用的终端会话安全地自主执行恢复命令。

## 🛠 特性概览

- **极速热启动：** 初始启动后，后续指令响应达到毫秒级。
- **会话池自动回收 (GC)：** 自动跟踪空闲进程，并在超时（默认 30 分钟）后终止，节省内存。
- **原生工具约束 (v0.2.0+)：** 在引擎层级通过 CLI 原生参数硬性限制智能体能力（例如禁用 `Bash` 或网络访问工具）。
- **WebSocket 网关：** 包含一个开箱即用的服务器 (`hotplexd`)，原生支持 WebSockets 接入。
- **原生 Go SDK：** 通过 `import "github.com/hrygo/hotplex/pkg/hotplex"` 直接嵌入到 Go 后端。
- **正则安全防火墙：** 内置 `danger.go` 预检拦截器，在指令到达智能体前阻断破坏性命令（如 `rm -rf /`、Fork 炸弹等）。
- **上下文隔离：** 使用 UUID v5 确定性命名空间生成算法，确保会话沙箱的逻辑隔离。

## 📦 架构设计

HotPlex 采用两层架构设计：

![HotPlex Architecture](docs/images/topology.svg)

1.  **核心 SDK (`pkg/hotplex`)**：引擎本体，提供 `Engine` 单例、`SessionPool` 和 `Detector`（安全防火墙）。它接收 CLI 的 JSON 流并触发强类型的 Go 事件回调。
2.  **独立服务端 (`cmd/hotplexd`)**：基于 SDK 封装的轻量级 WebSocket 服务器。

#### 🌊 异步事件流机制

HotPlex 充分利用 Go 的并发特性实现真正的全双工流式交互：

![HotPlex Event Flow](docs/images/async-stream.svg)

*注意：当前 MVP 版本针对 **Claude Code** 的全双工 JSON 协议（同时开启 `--input-format stream-json` 与 `--output-format stream-json`）进行了深度优化，但设计上已预留 `Provider` 接口以支持 OpenCode 和 Aider。*

## ⚡ 快速开始

### 1. 运行 WebSocket 独立服务器

如果你想直接运行服务器并通过前端或脚本连接：

```bash
# 安装 Claude Code (推荐：官方原生安装脚本)
# macOS / Linux / WSL:
curl -fsSL https://claude.ai/install.sh | bash

# 或通过 Homebrew 安装:
brew install claude-code

# 或通过 NPM 安装 (传统方式):
npm install -g @anthropic-ai/claude-code

# 编译并运行守护进程
cd cmd/hotplexd
go build -o hotplexd main.go
./hotplexd
```
服务器运行在 `ws://localhost:8080/ws/v1/agent`。参考 `_examples/websocket_client/client.js` 查看集成示例。

### 2. 使用 Go SDK 原生集成

安装依赖包：
```bash
go get github.com/hrygo/hotplex
```

并在代码中引入：
```go
import "github.com/hrygo/hotplex/pkg/hotplex"

opts := hotplex.EngineOptions{
    Timeout: 5 * time.Minute,
    Logger:  logger,
    PermissionMode: "bypass-permissions", // v0.2.0+ 推荐的权限处理模式
    AllowedTools: []string{"Bash", "Edit"}, // 在引擎层级限制工具能力
}

engine, _ := hotplex.NewEngine(opts)
defer engine.Close()

cfg := &hotplex.Config{
    WorkDir:          "/tmp/sandbox",
    SessionID:        "user_123_session", // 确定性多路复用 ID
    TaskSystemPrompt: "你是一名资深开发工程师。",
}

ctx := context.Background()

// 1. 发送 Prompt 并处理流式回调
err := engine.Execute(ctx, cfg, "重构 main.go 文件", func(eventType string, data any) error {
    if eventType == "answer" {
         fmt.Println("智能体正在响应...")
    }
    return nil
})
```

## 🔒 安全性

HotPlex 在你的机器上执行 LLM 生成的 Shell 代码。**请谨慎使用。**

我们通过以下手段降低风险：
1.  **原生能力治理**：从 v0.2.0 开始，我们优先使用原生工具限制 (`AllowedTools`) 而非不稳定的路径拦截，确保智能体仅拥有必要的“肌肉”。
2.  **指令预检 (WAF)**：基于正则的防御层，在指令下发前拦截破坏性模式（如 `mkfs`, `dd`, `rm -rf /`）。
3.  **进程组隔离 (PGID)**：会话终止时，HotPlex 对整个进程组发送 `SIGKILL`，确保 CLI 及其产生的所有子进程被瞬间物理清除。
4.  **工作目录锁定**：智能体被限制在 Config 中指定的 `WorkDir` 路径内。

## 路线图 (Roadmap)
- [ ] 提取 Provider 接口 (增加对 `OpenCode` 的支持)
- [ ] 远程 Docker 沙盒执行能力 (取代本地操作系统执行)
- [ ] 提供 REST API 接口，用于会话自省管理和强制终止
