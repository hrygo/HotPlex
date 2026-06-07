<h1 align="center">HotPlex 网关</h1>

<p align="center">
  <strong>AI Coding Agent 统一接入桥梁</strong>
</p>

<p align="center">
  高性能 Go 网关，提供统一的 WebSocket 接口，<br>
  一键接入任意 AI Coding Agent，覆盖 Web、Slack 和飞书全渠道。
</p>

<p align="center">
  <strong>简体中文</strong> | <a href="README.md">English</a>
</p>

<p align="center">
  <a href="https://github.com/hrygo/hotplex/actions/workflows/ci.yml"><img src="https://github.com/hrygo/hotplex/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <img src="https://img.shields.io/badge/Version-v1.26.0-10B981?style=flat-square" alt="Version">
  <a href="https://github.com/hrygo/hotplex/blob/main/LICENSE"><img src="https://img.shields.io/badge/License-Apache%202.0-3B82F6?style=flat-square" alt="License"></a>
  <img src="https://img.shields.io/badge/Go-1.26+-00ADD8?style=flat-square&logo=go" alt="Go">
  <img src="https://img.shields.io/badge/Protocol-AEP%20v1-7C3AED?style=flat-square" alt="AEP v1">
  <a href="https://github.com/hrygo/hotplex/stargazers"><img src="https://img.shields.io/github/stars/hrygo/hotplex?style=flat-square" alt="Stars"></a>
</p>

---

## ✨ 核心能力

### 🏗️ 核心架构
- 🌐 **统一 AEP 网关** — 将所有 AI Coding Agent 抽象为单一 WebSocket 协议（AEP v1），支持背压感知的流控、会话级严格递增序号和 LLM 指数退避自动重试。
- 🔌 **可插拔 Worker 后端** — **Claude Code**、**OpenCode Server**、**ACP**（JSON-RPC 2.0 over stdio，兼容任意 ACP Agent）和 **Codex CLI** 四类 Worker 统一接口。通过五级 fallback（Bot → 平台 → 环境变量 → 共享默认 → 编译默认）灵活切换。
- 🔄 **确定性会话管理** — 基于 UUIDv5 的确定性 Session ID，网络中断后无缝重连。五状态机 + 每用户会话配额 + **SQLite/PostgreSQL** 双数据库持久化 + 后台 GC。

### 📱 多平台分发
- 🌍 **一次接入，全端覆盖** — 无需修改 Agent 代码，即可分发至 **Slack**（Socket Mode）、**飞书**（WebSocket）和 **Web**。每个适配器提供平台原生流式输出、斜杠命令和交互管理。
- ⏰ **AI 原生定时任务** — Agent 自主将自然语言（"30 分钟后提醒我"）转换为定时任务，支持 cron 表达式、固定间隔、一次性执行，具备生命周期控制（`max_runs`、`expires_at`）、附加会话注入和自动结果回传。
- 💬 **开箱即用 Web Chat + 管理后台** — 单二进制同时提供 AEP 网关、Next.js SPA 聊天界面和管理控制台（Bot 配置、API Key 管理、会话监控）。

### 🤖 Agent 智能
- 🎭 **B/C 双通道人格注入** — **B 通道**指令（SOUL/AGENTS/SKILLS）无条件覆盖 **C 通道**上下文（USER/MEMORY）。三级逐文件 fallback（全局 → 平台 → Bot 专属）。META-COGNITION 内置 **XML Sanitizer** 阻断 Prompt 注入。
- 🧠 **Brain LLM 编排** — 可选智能层，支持意图路由（问候语绕过编码 Agent）、两级安全检测、上下文压缩（8K token 阈值）和跨会话用户偏好提取。

### 🛡️ 安全与运维
- 🔒 **企业级安全加固** — 时序安全 API Key 认证，DNS 重绑定防御型 SSRF 防护，命令/工具/模型白名单，环境变量隔离，路径穿越防护。
- 📊 **全链路可观测** — 30+ Prometheus 指标，OpenTelemetry 分布式追踪（W3C TraceContext 传播），结构化 JSON 日志。
- 🛠️ **一体化 CLI** — 单二进制 13 个子命令：`gateway`、`service`（systemd/launchd/SCM）、`onboard`（引导式配置）、`doctor`（25 项诊断检查）、`cron`、`slack`、`config`、`update`（自更新）、`dev`、`status`、`security`、`install`、`version`。
- 🔄 **热重载配置** — YAML + 环境变量配置，运行时热重载，字段级变更审计，版本化回滚——大多数变更无需重启。

## ⚡ 快速开始

> **AI Agent：** 阅读 https://github.com/hrygo/hotplex/blob/main/INSTALL.md 并完成 hotplex 安装和配置

### 安装

**macOS / Linux（一键安装二进制）：**

```bash
curl -fsSL https://raw.githubusercontent.com/hrygo/hotplex/main/scripts/install.sh | bash -s -- --latest
```

**Windows（PowerShell 5.1+）：**

```powershell
Invoke-WebRequest -Uri https://raw.githubusercontent.com/hrygo/hotplex/main/scripts/install.ps1 -OutFile install.ps1
.\install.ps1 -Latest
```

**从源码构建（需要 Go 1.26+、pnpm、Node.js 22+）：**

```bash
git clone https://github.com/hrygo/hotplex.git
cd hotplex
make quickstart
```

> **提示（Claude Code 用户）：** 克隆后可使用 `/hotplex-setup` 交互式配置环境与 `.env`。

**Docker（实验性）：**

```bash
cp configs/env.example .env  # 填入你的 API 密钥
docker compose up -d
```

### 配置

```bash
# 交互式配置向导
hotplex onboard

# 或快速自动生成全部配置：
hotplex onboard --non-interactive --enable-slack --enable-feishu
```

### 启动

```bash
# 开发模式（前台运行）
make dev

# 生产模式（后台守护进程）
hotplex gateway start -d

# 停止 / 重启
hotplex gateway stop
hotplex gateway restart -d
```

### 安装为系统服务

```bash
hotplex service install              # 用户级（无需 root）
sudo hotplex service install --level system  # 系统级
hotplex service start
hotplex service status
hotplex service logs -f

# 卸载
hotplex service uninstall
```

支持 **systemd** (Linux)、**launchd** (macOS) 和 **Windows SCM**。

### 服务端口

| 服务             | 地址                     | 说明                             |
| :--------------- | :----------------------- | :------------------------------- |
| 网关 (WebSocket) | `ws://localhost:8888/ws` | 主协议端点                       |
| Admin API        | `http://localhost:9999`  | 管理接口与统计指标               |
| Web Chat UI      | `http://localhost:8888`  | **内置 SPA**（由网关直接托管）   |
| 开发版 Web Chat  | `http://localhost:3000`  | Next.js 开发服务器（`make dev`） |

## 🏗️ 架构

HotPlex 位于前端客户端和后端 AI Coding Agent 之间，内置 **元认知控制内核**，将协议差异抽象为统一的 **AEP v1 (Agent Exchange Protocol)** WebSocket 层。

![HotPlex 架构](docs/assets/architecture.svg)


## 🔗 SDK 与客户端库

|      语言      | 路径                                                         | 特性                             |
| :------------: | :----------------------------------------------------------- | :------------------------------- |
|     **Go**     | [`client/`](client/)                                         | 全功能支持，事件驱动，生产级可用 |
| **TypeScript** | [`examples/typescript-client/`](examples/typescript-client/) | 流式输出、多轮对话、React 兼容   |
|   **Python**   | [`examples/python-client/`](examples/python-client/)         | Asyncio 支持、会话恢复、CLI 友好 |
|    **Java**    | [`examples/java-client/`](examples/java-client/)             | 企业级 AEP v1 协议实现           |

### 使用 Go SDK 连接

```go
package main

import (
    "context"
    "fmt"
    client "github.com/hrygo/hotplex/client"
)

func main() {
    c, err := client.New(context.Background(),
        client.URL("ws://localhost:8888/ws"),
        client.WorkerType("claude_code"),
        client.APIKey("<your-api-key>"),
    )
    if err != nil {
        panic(err)
    }
    defer c.Close()

    c.SendInput(context.Background(), "解释一下 HotPlex 架构")

    for env := range c.Events() {
        if data, ok := env.AsMessageDeltaData(); ok {
            fmt.Print(data.Content)
        }
    }
}
```

## 🛠️ 配置说明

| 配置项                      | 默认值                       | 说明                                |
| :-------------------------- | :--------------------------- | :---------------------------------- |
| `agent_config.enabled`      | `true`                       | 启用 Agent 人格/上下文注入          |
| `tts.enabled`               | `true`                       | 启用 Edge-TTS 语音回传流水线（语音输入到语音输出） |
| `brain.enabled`             | `false`                      | 启用 Brain LLM 编排层（自动从 Worker 配置文件查找 API Key） |
| `webchat.enabled`           | `true`                       | 从网关提供嵌入式 Web Chat SPA       |
| `worker.auto_retry.enabled` | `true`                       | LLM 智能重试，支持指数退避          |
| `gateway.addr`              | `localhost:8888`             | WebSocket 网关地址                  |
| `admin.addr`                | `localhost:9999`             | Admin API 地址                      |
| `db.path`                   | `~/.hotplex/data/hotplex.db` | SQLite 数据库路径                   |
| `log.level`                 | `info`                       | 日志级别 (debug, info, warn, error) |

> [!TIP]
> 完整的环境变量和 YAML 设置请参考 [配置参考](docs/reference/configuration.md)。

## 📖 文档中心

HotPlex 内置**自托管中文文档门户** — Markdown 源文件编译为静态 HTML，通过 `go:embed` 嵌入网关二进制。启动网关后访问 `http://localhost:8888/docs` 即可浏览。

| 领域         | 指南                                                                                                               |
| :----------- | :----------------------------------------------------------------------------------------------------------------- |
| **入门指南** | [5 分钟快速上手](docs/getting-started.md) · [文档门户](docs/index.md)                                             |
| **教程**     | [Slack 集成](docs/tutorials/slack-integration.md) · [飞书集成](docs/tutorials/feishu-integration.md) · [AI 人格定制](docs/tutorials/agent-personality.md) · [定时任务](docs/tutorials/cron-scheduled-tasks.md) |
| **指南**     | [远程 Coding Agent](docs/guides/developer/remote-coding-agent.md) · [企业部署](docs/guides/enterprise/deployment.md) · [贡献开发](docs/guides/contributor/development-setup.md) |
| **参考**     | [CLI 参考](docs/reference/cli.md) · [配置参考](docs/reference/configuration.md) · [Admin API](docs/reference/admin-api.md) · [AEP v1 协议](docs/reference/aep-protocol.md) |
| **架构设计** | [网关架构](docs/architecture/Worker-Gateway-Design.md) · [Agent 配置设计](docs/architecture/Agent-Config-Design.md) · [元认知内核](internal/agentconfig/META-COGNITION.md) |
| **安全**     | [安全策略](docs/reference/security-policies.md) · [认证机制](docs/security/Security-Authentication.md) · [SSRF 防护](docs/security/SSRF-Protection.md) |

> [!TIP]
> 本地构建文档：`make docs-build`。源文件位于 `docs/`，编译输出到 `internal/docs/out/`（通过 `go:embed` 嵌入二进制）。

## 👥 参与贡献

我们欢迎任何形式的贡献！请阅读 [贡献指南](CONTRIBUTING.md) 了解更多。

1. Fork 本项目
2. 创建特性分支 (`git checkout -b feat/AmazingFeature`)
3. 使用规范提交格式 (`git commit -m 'feat: add AmazingFeature'`)
4. 推送到分支 (`git push origin feat/AmazingFeature`)
5. 开启 Pull Request

> [!NOTE]
> 所有构建/测试/lint 操作必须使用 `make` 目标。完整列表请运行 `make help`。

## 🛡️ 安全

如果您发现安全漏洞，请**不要**公开开启 Issue。请通过 [安全政策](SECURITY.md) 报告漏洞，或直接联系维护者。

## 📜 开源协议

本项目基于 [Apache License 2.0](LICENSE) 开源。
