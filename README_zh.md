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
      <img src="https://img.shields.io/badge/go-report-brightgreen?style=flat-square" alt="Go Report">
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

## ⚡ 快速开始

### 前置要求

- **Go**: 1.25+
- **AI 工具**: 确保已在主机上安装 [Claude Code](https://github.com/anthropics/claude-code) 或 [OpenCode CLI](https://github.com/hrygo/opencode)。

### 安装与运行

```bash
# 1. 一键安装
curl -sL https://raw.githubusercontent.com/hrygo/hotplex/main/install.sh | bash

# 2. 启动会话
hotplexd --config chatapps/configs
```

---

## 💎 特性橱窗

![HotPlex Features](docs/images/features.svg)

---

## 🎯 我们的定位：AI 智能体“控制平面”

现有的 AI Agent 虽然强大，但往往缺乏生产级服务所需的**运行时稳定性**。HotPlex 通过提供以下能力填补了这一空白：

1.  **进程生命支持**：将瞬时的 CLI 交互转化为持久、有状态的会话。
2.  **安全性拦截**：为 Shell 指令提供可编程的正则防火墙 (Danger WAF)。
3.  **高效能复用**：通过优化的进程复用技术，在单台机器上轻松管理数百个并发 AI 会话。

---

## 💡 典型应用场景

| 场景                     | HotPlex 的价值                                                   |
| :----------------------- | :--------------------------------------------------------------- |
| **AI 编程助手**          | 将 AI 直接嵌入 IDE 终端，保持跨会话的历史上下文。                |
| **自动化运维 Autopilot** | 将系统日志通过管道输入 HotPlex，让 AI 在人工确认下执行修复脚本。 |
| **智能体总线**           | 在统一的 WebSocket/SSE 网关下管理不同品牌的 AI CLI 工具。        |
| **企业级 ChatOps**       | 将本地 AI 能力以工业级稳定性接入钉钉、飞书或 Slack。             |

---

## 🚀 功能特性

| 特性               | 描述                                                             |
| ------------------ | ---------------------------------------------------------------- |
| **会话池**         | 长生命周期 CLI 进程，即时重连                                    |
| **全双工流**       | 通过 Go channel 实现亚秒级 token 投递                            |
| **正则 WAF**       | 拦截破坏性命令（`rm -rf /`、`mkfs` 等）                          |
| **PGID 隔离**      | 干净的进程终止，无僵尸进程                                       |
| **ChatApps**       | Slack（Block Kit、流式、Assistant Status）、Telegram、飞书、钉钉 |
| **Go SDK**         | 零开销直接嵌入 Go 应用                                           |
| **WebSocket 网关** | 通过 `hotplexd` 守护进程实现语言无关访问                         |
| **OpenTelemetry**  | 内置指标和追踪支持                                               |

---

## 🏛 架构与安全

HotPlex 在高并发会话拓扑之上采用深度防御安全模型。

![HotPlex 安全架构](docs/images/hotplex-security.svg)

![HotPlex 系统拓扑](docs/images/topology.svg)

| 层级             | 实现                | 防护                          |
| ---------------- | ------------------- | ----------------------------- |
| **工具治理**     | `AllowedTools` 配置 | 限制智能体能力                |
| **危险 WAF**     | 正则拦截            | 阻止 `rm -rf /`、`mkfs`、`dd` |
| **进程隔离**     | 基于 PGID 终止      | 无孤儿进程                    |
| **文件系统沙箱** | WorkDir 锁定        | 限制在项目根目录              |
| **容器沙箱**     | Docker (BaaC) 架构  | 操作系统级隔离与资源限制      |

---

## 🛠 使用示例

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

---

## 📚 文档资源

| 资源                                                         | 描述                            |
| :----------------------------------------------------------- | :------------------------------ |
| [架构深度解析](docs/architecture_zh.md)                      | 系统设计、安全协议、会话管理    |
| [SDK 开发指南](docs/sdk-guide_zh.md)                         | 完整 Go SDK 参考                |
| [ChatApps 手册](chatapps/README.md)                          | 多平台集成（Slack、钉钉、飞书） |
| [Docker 多 Bot 部署](docs/docker-multi-bot-deployment_zh.md) | 一键运行多个机器人              |

---

## 🤝 社区与贡献

- **反馈问题/建议**：请使用 [GitHub Issues](https://github.com/hrygo/hotplex/issues)。
- **交流与讨论**：在 [GitHub Discussions](https://github.com/hrygo/hotplex/discussions) 提问或分享想法。
- **参与贡献**：我们欢迎各种形式的贡献！请确保 CI 通过 (`make lint`, `make test`)。详见 [贡献指南](CONTRIBUTING.md)。

---

## 📄 许可证

采用 [MIT License](LICENSE) 发布。

---

<div align="center">
  <img src="docs/images/hotplex_beaver_final.png" alt="HotPlex Mascot" width="120"/>
  <br/>
  <i>为 AI 工程化社区倾力构建。</i>
</div>
