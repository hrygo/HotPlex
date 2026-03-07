<div align="center">
  <img src="docs/images/hotplex_beaver_banner.webp" alt="HotPlex" width="100%"/>

  <h1>HotPlex</h1>

  <p><strong>AI 智能体控制平面 — 将 AI CLI 转化为生产级服务</strong></p>

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
    <a href="README.md">English</a> •
    <b>简体中文</b> •
    <a href="#快速开始">快速开始</a> •
    <a href="https://hrygo.github.io/hotplex/">文档</a> •
    <a href="docs/chatapps/slack-setup-beginner_zh.md">Slack 指南</a>
  </p>
</div>

---

## 概述

HotPlex 将 AI CLI 工具（Claude Code、OpenCode）从"运行即退出"的命令转化为**持久化、有状态的服务**，支持全双工流式传输。

**为什么选择 HotPlex？**

- **零启动开销** — 持久化会话池彻底消灭 3-5 秒的 CLI 冷启动延迟
- **Cli-as-a-Service** — 跨交互持续指令流和上下文保持
- **生产级安全** — 正则 WAF、PGID 进程隔离、文件系统边界
- **多平台 ChatApps** — 原生支持 Slack、Telegram、飞书、钉钉
- **简单集成** — Go SDK 嵌入或独立 WebSocket 服务器

## 快速开始

### 前置要求

- Go 1.25+
- Claude Code CLI 或 OpenCode CLI（可选，用于 AI 能力）

### 安装

```bash
# 一键安装 (Linux / macOS / WSL)
curl -sL https://raw.githubusercontent.com/hrygo/hotplex/main/install.sh | bash

# 指定版本
curl -sL https://raw.githubusercontent.com/hrygo/hotplex/main/install.sh | bash -s -- -v v0.21.0

# 自定义目录
curl -sL https://raw.githubusercontent.com/hrygo/hotplex/main/install.sh | bash -s -- -d ~/bin
```

详见 [INSTALL_zh.md](INSTALL_zh.md) 获取所有选项（干运行、强制重装、卸载）。

### 配置

```bash
# 复制环境变量模板
cp .env.example .env

# 编辑凭证
# ChatApps 配置在 chatapps/configs/*.yaml
```

### 运行

```bash
# 使用 ChatApps 启动（生产环境推荐）
./dist/hotplexd --config chatapps/configs

# 或启动独立服务器
./dist/hotplexd
```

**完成！** 你的 AI 智能体服务已运行。

## 功能特性

| 特性 | 描述 |
|------|------|
| **会话池** | 长生命周期 CLI 进程，即时重连 |
| **全双工流** | 通过 Go channel 实现亚秒级 token 投递 |
| **正则 WAF** | 拦截破坏性命令（`rm -rf /`、`mkfs` 等） |
| **PGID 隔离** | 干净的进程终止，无僵尸进程 |
| **ChatApps** | Slack（Block Kit、流式、Assistant Status）、Telegram、飞书、钉钉 |
| **Go SDK** | 零开销直接嵌入 Go 应用 |
| **WebSocket 网关** | 通过 `hotplexd` 守护进程实现语言无关访问 |
| **OpenTelemetry** | 内置指标和追踪支持 |

## 架构

```
┌─────────────────────────────────────────────────────────────┐
│                      接入层                                  │
│         Go SDK  │  WebSocket  │  ChatApps 适配器            │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                      引擎层                                  │
│    会话池  │  配置管理器  │  安全 WAF                        │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                     进程层                                   │
│    Claude Code  │  OpenCode  │  隔离工作空间                 │
└─────────────────────────────────────────────────────────────┘
```

## 使用示例

### Go SDK

```go
import "github.com/hrygo/hotplex"

engine, _ := hotplex.NewEngine(hotplex.EngineOptions{
    Timeout: 5 * time.Minute,
})

engine.Execute(ctx, cfg, "重构这个函数", func(event Event) {
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
  你是一个有帮助的编程助手。
```

```bash
export HOTPLEX_SLACK_BOT_USER_ID=B12345
export HOTPLEX_SLACK_BOT_TOKEN=xoxb-...
export HOTPLEX_SLACK_APP_TOKEN=xapp-...
hotplexd --config chatapps/configs
```

## 文档

| 资源 | 描述 |
|------|------|
| [架构深度解析](docs/architecture_zh.md) | 系统设计、安全协议、会话管理 |
| [SDK 开发指南](docs/sdk-guide_zh.md) | 完整 Go SDK 参考 |
| [ChatApps 手册](chatapps/README.md) | 多平台集成（Slack、钉钉、飞书） |
| [Slack 新手指南](docs/chatapps/slack-setup-beginner_zh.md) | 零基础上手 Slack |
| [Docker 多 Bot 部署](docs/docker-multi-bot-deployment_zh.md) | 一键运行多个机器人 |
| [Docker 部署](docs/docker-deployment_zh.md) | 容器和 Kubernetes 部署 |
| [生产环境指南](docs/production-guide_zh.md) | 生产最佳实践 |

## 安全

HotPlex 采用深度防御安全策略：

| 层级 | 实现 | 防护 |
|------|------|------|
| **工具治理** | `AllowedTools` 配置 | 限制智能体能力 |
| **危险 WAF** | 正则拦截 | 阻止 `rm -rf /`、`mkfs`、`dd` |
| **进程隔离** | 基于 PGID 终止 | 无孤儿进程 |
| **文件系统沙箱** | WorkDir 锁定 | 限制在项目根目录 |

## 贡献

欢迎贡献！请确保 CI 通过：

```bash
make lint    # 运行 golangci-lint
make test    # 运行单元测试
```

详见 [CONTRIBUTING.md](CONTRIBUTING.md)。

## 许可证

采用 [MIT License](LICENSE) 发布。

---

<div align="center">
  <i>为 AI 工程化社区倾力构建。</i>
</div>
