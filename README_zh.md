<div align="center">
  <img src="docs/images/logo.svg" alt="HotPlex" width="120"/>

  # HotPlex

  **将 AI CLI 转化为生产级服务**

  将强大的 AI CLI（Claude Code、OpenCode）桥接到持久、安全的生产级交互服务。

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
    <a href="#-快速开始">快速开始</a> ·
    <a href="#-特性">特性</a> ·
    <a href="#-架构">架构</a> ·
    <a href="https://hrygo.github.io/hotplex/">文档</a> ·
    <a href="https://github.com/hrygo/hotplex/discussions">讨论</a>
  </p>
</div>

---

## ⚡ 快速开始

```bash
# 一键安装
curl -sL https://raw.githubusercontent.com/hrygo/hotplex/main/install.sh | bash

# 构建 Node.js 语言栈
make stack S=node

# 构建所有语言栈
make stack-all
export HOTPLEX_SLACK_PRIMARY_OWNER=U...
export HOTPLEX_SLACK_BOT_TOKEN=xoxb-...
export HOTPLEX_SLACK_APP_TOKEN=xapp-...
./hotplexd --config configs/chatapps/slack.yaml
```

### 前置要求

| 组件 | 版本 |
| :--- | :--- |
| Go | 1.25+ |
| AI CLI | [Claude Code](https://github.com/anthropics/claude-code) 或 [OpenCode](https://github.com/hrygo/opencode) |

---

## ✨ 特性

| | |
| :--- | :--- |
| 🔄 **会话池** | 长生命周期 CLI 进程，即时重连 |
| 🌊 **全双工流** | 亚秒级 token 投递 via Go channels |
| 🛡️ **正则 WAF** | 拦截破坏性命令（`rm -rf /`、`mkfs` 等） |
| 🔒 **PGID 隔离** | 干净的进程终止，无僵尸进程 |
| 💬 **多平台** | Slack · 飞书 |
| 📦 **Go SDK** | 零开销直接嵌入 Go 应用 |
| 🔌 **WebSocket 网关** | 通过 `hotplexd` 守护进程实现语言无关访问 |
| 📊 **OpenTelemetry** | 内置指标和追踪支持 |
| 🐳 **Docker 1+n 架构** | 1 个基础镜像 + n 个语言栈 (`node`, `python`, `java`, `rust`, `full`) |

---

## 🎯 为什么选择 HotPlex？

> **AI 智能体在生产环境中缺失的控制平面**

| 挑战 | HotPlex 解决方案 |
| :--- | :--------------- |
| AI 每次请求都重新启动 | **持久会话** - CLI 保持存活，复用上下文 |
| 缺乏破坏性命令安全防护 | **正则 WAF** - 可编程防火墙拦截危险指令 |
| 难以扩展 AI 交互 | **进程复用** - 支持数百个并发会话 |
| 集成复杂度高 | **ChatApps** - 一个代码库，支持 Slack / 飞书 |
| 企业级安全需求 | **PGID 隔离** + 文件系统沙箱 + 容器隔离 |

---

## 🏛 架构

```
│                        ChatApps 层                               │
│                       Slack · 飞书                              │
└────────────────────────────┬────────────────────────────────────┘
                             │
┌────────────────────────────▼────────────────────────────────────┐
│                      WebSocket 网关                             │
│                  (hotplexd 守护进程 / Go SDK)                  │
└────────────────────────────┬────────────────────────────────────┘
                             │
┌────────────────────────────▼────────────────────────────────────┐
│                      引擎 / 运行器                              │
│         I/O 复用 · 会话池 · 事件流                              │
└────────────────────────────┬────────────────────────────────────┘
                             │
        ┌────────────────────┼────────────────────┐
        ▼                    ▼                    ▼
   ┌─────────┐         ┌─────────┐         ┌─────────┐
   │ Claude  │         │ OpenCode│         │  自定义  │
   │   CLI   │         │   CLI   │         │ Provider│
   └─────────┘         └─────────┘         └─────────┘
```

### 安全层级

| 层级 | 实现方式 |
| :--- | :------- |
| 工具治理 | `AllowedTools` 配置 - 限制智能体能力 |
| 危险 WAF | 正则拦截 - 阻止 `rm -rf /`、`mkfs`、`dd` |
| 进程隔离 | 基于 PGID 终止 - 无孤儿进程 |
| 文件系统沙箱 | `WorkDir` 锁定 - 限制在项目根目录 |
| 容器沙箱 | Docker (BaaS) - 操作系统级隔离与资源限制 |

---

## 📖 使用示例

### Go SDK

```go
import (
    "context"
    "fmt"
    "time"

    "github.com/hrygo/hotplex"
    "github.com/hrygo/hotplex/types"
)

engine, err := hotplex.NewEngine(hotplex.EngineOptions{
    Timeout:     5 * time.Minute,
    IdleTimeout: 30 * time.Minute,
})
if err != nil {
    panic(err)
}
defer engine.Close()

cfg := &types.Config{
    WorkDir:   "/path/to/project",
    SessionID: "user-session-123",
}

engine.Execute(context.Background(), cfg, "重构这个函数", func(eventType string, data any) error {
    if msg, ok := data.(*types.StreamMessage); ok {
        fmt.Print(msg.Content) // 流式响应
    }
    return nil
})
```

### Slack 机器人

```yaml
# configs/chatapps/slack.yaml
platform: slack
mode: socket

provider:
  type: claude-code
  default_model: sonnet

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

```bash
export HOTPLEX_SLACK_BOT_USER_ID=B12345
export HOTPLEX_SLACK_BOT_TOKEN=xoxb-...
export HOTPLEX_SLACK_APP_TOKEN=xapp-...
./hotplexd --config configs/chatapps/slack.yaml
```

### WebSocket API

```javascript
const ws = new WebSocket('ws://localhost:8080/ws/v1/agent');

ws.onmessage = (event) => {
  const data = JSON.parse(event.data);
  console.log(data.type, data.content);
};

ws.send(JSON.stringify({
  type: 'execute',
  prompt: '你好，AI！'
}));
```

---

## 📚 文档

| | |
| :--- | :--- |
| [🚀 部署指南](https://hrygo.github.io/hotplex/guide/deployment) | Docker、生产环境配置 |
| [💬 ChatApps 手册](chatapps/README.md) | Slack、飞书集成 |
| [🛠 Go SDK 参考](https://hrygo.github.io/hotplex/sdks/go-sdk) | 完整 SDK 文档 |
| [🔒 安全指南](https://hrygo.github.io/hotplex/guide/security) | WAF、隔离、最佳实践 |
| [📊 可观测性](https://hrygo.github.io/hotplex/guide/observability) | 指标、追踪、日志 |

---

## 🤝 贡献

欢迎贡献代码！请阅读 [贡献指南](CONTRIBUTING.md) 了解更多细节。

```bash
# 开发环境搭建
go mod download   # 安装依赖
make test         # 运行测试
make lint         # 运行检查器
make build        # 构建二进制

# 提交 PR
git checkout -b feat/your-feature
git commit -m "feat: add awesome feature"
gh pr create --fill
```

---

## 📄 许可证

MIT License © 2024-present [HotPlex 贡献者](https://github.com/hrygo/hotplex/graphs/contributors)

---

<div align="center">
  <img src="docs/images/hotplex_beaver_final.png" alt="HotPlex 吉祥物" width="100"/>
  <br/>
  <sub>为 AI 工程化社区倾力构建</sub>
</div>
