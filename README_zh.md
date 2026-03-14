<div align="center">
  <img src="docs/images/hotplex_beaver_banner.webp" alt="HotPlex 横幅"/>

  # HotPlex

  **高性能 AI Agent 执行运行时**

  HotPlex 是一个高性能的 AI Agent 执行运行时，旨在将 Claude Code 等终端 AI 工具转化为生产级的交互式服务。其核心架构基于 Go 语言开发，采用 Cli-as-a-Service 理念，通过持久化进程池消除 CLI 启动延迟，并利用 PGID 隔离与 Regex WAF 确保执行安全。该系统通过双协议网关支持 WebSocket 与 HTTP/SSE 通信，并提供 Python 和 TypeScript SDK 供开发者集成。在应用层，HotPlex 深度适配了 Slack 与飞书，支持原生流式输出、交互式卡片及多机器人协作协议。此外，项目遵循 Uber Go 编码规范，具备完善的事件钩子、监控统计与 Docker 容器化部署能力，构建了从底层执行到上层应用的完整 Agent 生态。

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
    <a href="README.md">English</a> ·
    <b>简体中文</b> ·
    <a href="docs/quick-start_zh.md">快速开始</a> ·
    <a href="https://hrygo.github.io/hotplex/zh/guide/features">特性</a> ·
    <a href="docs/architecture_zh.md">架构</a> ·
    <a href="https://hrygo.github.io/hotplex/">文档</a> ·
    <a href="https://github.com/hrygo/hotplex/discussions">讨论</a>
  </p>
</div>

---

## 目录

- [快速开始](#-快速开始)
- [核心概念](#-核心概念)
- [项目结构](#-项目结构)
- [特性](#-特性)
- [架构](#-架构)
- [使用示例](#-使用示例)
- [开发指南](#-开发指南)
- [文档](#-文档)
- [贡献指南](#-贡献指南)

---

## ⚡ 快速开始

```bash
# 一键安装
curl -sL https://raw.githubusercontent.com/hrygo/hotplex/main/install.sh | bash

# 或源码构建
make build

# 启动 Slack 机器人
export HOTPLEX_SLACK_BOT_USER_ID=B12345
export HOTPLEX_SLACK_BOT_TOKEN=xoxb-...
export HOTPLEX_SLACK_APP_TOKEN=xapp-...
./hotplexd --config configs/server.yaml --config-dir configs/chatapps

# 或仅启动 WebSocket 网关
./hotplexd --config configs/server.yaml
```

### 前置要求

| 组件 | 版本 | 说明 |
| :--- | :--- | :--- |
| Go | 1.25+ | 运行时与 SDK |
| AI CLI | [Claude Code](https://github.com/anthropics/claude-code) 或 [OpenCode](https://github.com/hrygo/opencode) | 执行目标 |
| Docker | 24.0+ | 可选，用于容器部署 |

### 首次运行清单

```bash
# 1. 克隆并构建
git clone https://github.com/hrygo/hotplex.git
cd hotplex
make build

# 2. 复制环境模板
cp .env.example .env

# 3. 配置 AI CLI
# 确保 Claude Code 或 OpenCode 在 PATH 中

# 4. 运行守护进程
./hotplexd --config configs/server.yaml
```

---

## 🧠 核心概念

理解这些概念对于高效使用 HotPlex 至关重要。

### 会话池化

HotPlex 维护**长生命周期 CLI 进程**，而非每次请求都创建新实例。这消除了：
- 冷启动延迟（通常每次调用 2-5 秒）
- 请求间的上下文丢失
- 重复初始化的资源浪费

```
请求 1 → CLI 进程 1 (创建，持久化)
请求 2 → CLI 进程 1 (复用，即时)
请求 3 → CLI 进程 1 (复用，即时)
```

### I/O 复用

`Runner` 组件处理双向通信：
- **上游**：用户请求（WebSocket/HTTP/ChatApp 事件）
- **下游**：CLI stdin/stdout/stderr 流

```go
// 每个会话有专属的 I/O 通道
type Session struct {
    Stdin  io.Writer
    Stdout io.Reader
    Stderr io.Reader
    Events chan *Event  // 内部事件总线
}
```

### PGID 隔离

进程组 ID (PGID) 隔离确保**干净终止**：
- CLI 进程以 `Setpgid: true` 启动
- 终止时发送信号到整个进程组 (`kill -PGID`)
- 无孤儿或僵尸进程

### 正则 WAF

Web 应用防火墙层在危险命令到达 CLI 之前进行拦截：
- 拦截模式：`rm -rf /`、`mkfs`、`dd`、`:(){:|:&};:`
- 通过配置 `security.danger_waf` 自定义
- 与 CLI 原生工具限制 (`AllowedTools`) 配合使用

### ChatApps 抽象

多平台机器人集成的统一接口：

```go
type ChatAdapter interface {
    // 平台特定事件处理
    HandleEvent(event Event) error
    // 统一消息格式
    SendMessage(msg *ChatMessage) error
}
```

### MessageOperations (可选)

高级平台实现流式和消息管理：

```go
type MessageOperations interface {
    StartStream(ctx, channelID, threadTS) (messageTS, error)
    AppendStream(ctx, channelID, messageTS, content) error
    StopStream(ctx, channelID, messageTS) error
    UpdateMessage(ctx, channelID, messageTS, msg) error
    DeleteMessage(ctx, channelID, messageTS) error
}

---

## 📂 项目结构

```
hotplex/
├── cmd/
│   └── hotplexd/           # 守护进程入口
├── internal/               # 核心实现（私有）
│   ├── engine/             # 会话池与运行器
│   ├── server/             # WebSocket 与 HTTP 网关
│   ├── security/           # WAF 与隔离
│   ├── config/             # 配置加载
│   ├── sys/                # 系统信号
│   ├── telemetry/          # OpenTelemetry
│   └── ...
├── brain/                  # Native Brain 编排
├── cache/                  # 缓存层
├── provider/               # AI 提供商适配器
│   ├── claudecode/         # Claude Code 协议
│   ├── opencode/           # OpenCode 协议
│   └── ...
├── chatapps/               # 平台适配器
│   ├── slack/              # Slack 机器人
│   ├── feishu/             # 飞书机器人
│   └── base/               # 公共接口
├── types/                  # 公共类型定义
├── event/                  # 事件系统
├── plugins/                # 扩展点
│   └── storage/            # 消息持久化
├── sdks/                   # 多语言绑定
│   ├── go/                 # Go SDK (内嵌)
│   ├── python/             # Python SDK
│   └── typescript/         # TypeScript SDK
├── docker/                 # 容器定义
├── configs/                # 配置示例
└── docs/                  # 架构文档
```

### 关键目录

| 目录 | 用途 | 公共 API |
|------|------|----------|
| `types/` | 核心类型与接口 | ✅ 是 |
| `event/` | 事件定义 | ✅ 是 |
| `hotplex.go` | SDK 入口 | ✅ 是 |
| `internal/engine/` | 会话管理 | ❌ 内部 |
| `internal/server/` | 网络协议 | ❌ 内部 |
| `provider/` | CLI 适配器 | ⚠️ Provider 接口 |

---

## ✨ 特性

| 特性 | 描述 | 使用场景 |
|------|------|----------|
| 🔄 **会话池化** | 长生命周期 CLI 进程，即时复用 | 高频 AI 交互 |
| 🌊 **全双工流** | 亚秒级 token 投递 via Go channels | 实时 UI 更新 |
| 🛡️ **正则 WAF** | 拦截破坏性命令（`rm -rf /`、`mkfs` 等） | 安全加固 |
| 🔒 **PGID 隔离** | 干净的进程终止，无僵尸进程 | 生产可靠性 |
| 💬 **多平台** | Slack · 飞书 | 团队协作 |
| 📦 **Go SDK** | 零开销直接嵌入 Go 应用 | 自定义集成 |
| 🔌 **WebSocket 网关** | 通过 `hotplexd` 守护进程实现语言无关访问 | Web 前端 |
| 📊 **OpenTelemetry** | 内置指标和追踪支持 | 可观测性 |
| 🐳 **Docker 1+n 架构** | 1 个基础镜像 + n 个语言栈 | 多语言支持 |

---

## 🏛 架构

```
┌─────────────────────────────────────────────────────────────────┐
│                        ChatApps 层                               │
│                   Slack 机器人 · 飞书机器人 · Web                │
│              (Event → ChatMessage → Session ID)                 │
└────────────────────────────┬────────────────────────────────────┘
                             │
┌────────────────────────────▼────────────────────────────────────┐
│                      WebSocket 网关                             │
│                hotplexd 守护进程 / Go SDK / HTTP                 │
│              (协议转换、限流、认证)                              │
└────────────────────────────┬────────────────────────────────────┘
                             │
┌────────────────────────────▼────────────────────────────────────┐
│                      引擎 / 运行器                               │
│    ┌─────────────────────────────────────────────────────┐      │
│    │  会话池 (map[SessionID]*Session)                     │      │
│    │  - 生命周期管理                                      │      │
│    │  - 空闲超时与垃圾回收                                 │      │
│    └─────────────────────────────────────────────────────┘      │
│    ┌─────────────────────────────────────────────────────┐      │
│    │  运行器 (I/O 复用器)                                 │      │
│    │  - stdin/stdout/stderr 管道                         │      │
│    │  - 事件序列化                                        │      │
│    └─────────────────────────────────────────────────────┘      │
└────────────────────────────┬────────────────────────────────────┘
                             │
        ┌────────────────────┼────────────────────┐
        ▼                    ▼                    ▼
   ┌─────────┐         ┌─────────┐         ┌─────────┐
   │ Claude  │         │ OpenCode│         │  自定义  │
   │   CLI   │         │   CLI   │         │ Provider│
   └─────────┘         └─────────┘         └─────────┘
```

### 数据流：请求到响应

```
用户 → 网关 → 会话池 → 运行器 → CLI
CLI → 运行器 → 网关 → 用户 (流式)
```

### 安全层级

| 层级 | 实现方式 | 配置项 |
|------|----------|--------|
| 工具治理 | `AllowedTools` 配置 | `security.allowed_tools` |
| 危险 WAF | 正则拦截 | `security.danger_waf` |
| 进程隔离 | 基于 PGID 终止 | 自动 |
| 文件系统沙箱 | `WorkDir` 锁定 | `engine.work_dir` |
| 容器沙箱 | Docker (BaaS) | `docker/` |

---

## 📖 使用示例

### Go SDK（可嵌入）

```go
import (
    "context"
    "fmt"
    "time"

    "github.com/hrygo/hotplex"
    "github.com/hrygo/hotplex/types"
)

func main() {
    // 初始化引擎
    engine, err := hotplex.NewEngine(hotplex.EngineOptions{
        Timeout:     5 * time.Minute,
        IdleTimeout: 30 * time.Minute,
    })
    if err != nil {
        panic(err)
    }
    defer engine.Close()

    // 执行提示词
    cfg := &types.Config{
        WorkDir:   "/path/to/project",
        SessionID: "user-session-123",
    }

    engine.Execute(context.Background(), cfg, "解释这个函数", func(eventType string, data any) error {
        switch eventType {
        case "message":
            if msg, ok := data.(*types.StreamMessage); ok {
                fmt.Print(msg.Content)  // 流式输出
            }
        case "error":
            if errMsg, ok := data.(string); ok {
                fmt.Printf("错误: %s\n", errMsg)
            }
        case "usage":
            if stats, ok := data.(*types.UsageStats); ok {
                fmt.Printf("Token: 输入 %d, 输出 %d\n", stats.InputTokens, stats.OutputTokens)
            }
        }
        return nil
    })
}
```

### Slack 机器人配置

```yaml
# configs/chatapps/slack.yaml
platform: slack
mode: socket

provider:
  type: claude-code
  default_model: sonnet
  allowed_tools:
    - Read
    - Edit
    - Glob
    - Grep
    - Bash

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

### WebSocket API

```javascript
// 连接
const ws = new WebSocket('ws://localhost:8080/ws/v1/agent');

// 监听消息
ws.onmessage = (event) => {
  const data = JSON.parse(event.data);
  switch (data.type) {
    case 'message':
      console.log(data.content);
      break;
    case 'error':
      console.error(data.error);
      break;
    case 'done':
      console.log('执行完成');
      break;
  }
};

// 执行提示词
ws.send(JSON.stringify({
  type: 'execute',
  session_id: '可选的会话ID',
  prompt: '列出当前目录文件'
}));
```

### HTTP SSE API

```bash
curl -N -X POST http://localhost:8080/api/v1/execute \
  -H "Content-Type: application/json" \
  -d '{"prompt": "你好，AI！", "session_id": "session-123"}'
```

---

## 💻 开发指南

### 常见任务

```bash
# 运行测试
make test

# 运行竞态检测
make test-race

# 构建二进制
make build

# 运行检查器
make lint

# 构建 Docker 镜像
make docker-build

# 启动 Docker 栈
make docker-up
```

### 添加新的 ChatApp 平台

1. 在 `chatapps/<platform>/` 实现适配器接口：

```go
type Adapter struct {
    client *platform.Client
    engine *engine.Engine
}

// 实现 base.ChatAdapter 接口
var _ base.ChatAdapter = (*Adapter)(nil)

func (a *Adapter) HandleEvent(event base.Event) error {
    // 平台特定事件解析
}

func (a *Adapter) SendMessage(msg *base.ChatMessage) error {
    // 平台特定消息发送
}
```

2. 在 `chatapps/setup.go` 注册：

```go
func init() {
    registry.Register("platform-name", NewAdapter)
}
```

3. 在 `configs/chatapps/` 添加配置：

```yaml
platform: platform-name
mode: socket  # 或 http
# ... 平台特定配置
```

### 添加新的 Provider

1. 实现 `provider/<name>/parser.go`：

```go
type Parser struct{}

func (p *Parser) ParseStream(line string) (*types.StreamMessage, error) {
    // Provider 特定输出解析
}
```

2. 在 `provider/factory.go` 注册：

```go
func init() {
    providers.Register("provider-name", NewProvider)
}
```

---

## 📚 文档

| 指南 | 说明 |
|------|------|
| [🚀 部署指南](https://hrygo.github.io/hotplex/guide/deployment) | Docker、生产环境配置 |
| [💬 ChatApps](chatapps/README.md) | Slack、飞书集成 |
| [🛠 Go SDK](https://hrygo.github.io/hotplex/sdks/go-sdk) | SDK 参考 |
| [🔒 安全指南](https://hrygo.github.io/hotplex/guide/security) | WAF、隔离 |
| [📊 可观测性](https://hrygo.github.io/hotplex/guide/observability) | 指标、追踪 |
| [⚙️ 配置参考](docs/configuration.md) | 完整配置说明 |

---

## 🤝 贡献指南

欢迎贡献代码！请按以下步骤操作：

```bash
# 1. Fork 并克隆
git clone https://github.com/hrygo/hotplex.git

# 2. 创建功能分支
git checkout -b feat/your-feature

# 3. 修改并测试
make test
make lint

# 4. 使用约定格式提交
git commit -m "feat(engine): add session priority support"

# 5. 提交 PR
gh pr create --fill
```

### 提交信息格式

```
<类型>(<范围>): <描述>

类型: feat, fix, refactor, docs, test, chore
范围: engine, server, chatapps, provider 等
```

### 代码规范

- 遵循 [Uber Go 编码规范](.agent/rules/uber-go-style-guide.md)
- 所有接口需要编译时验证
- 提交前运行 `make test-race`

---

## 📄 许可证

MIT License © 2024-present [HotPlex 贡献者](https://github.com/hrygo/hotplex/graphs/contributors)

---

<div align="center">
  <img src="docs/images/logo.svg" alt="HotPlex 图标" width="100"/>
  <br/>
  <sub>为 AI 工程化社区倾力构建</sub>
</div>
