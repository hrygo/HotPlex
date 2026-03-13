# 🚀 快速上手 (Quick Start)

本文档将帮助你在 5 分钟内启动并运行 HotPlex。

---

## 🛠️ 第一步：选择安装方式

HotPlex 支持两种主要的运行方式：

### 方案 A：Docker 容器运行 (推荐 ⭐⭐⭐)
**适用场景**：生产部署、隔离开发、多机器人编排（1+n）。
- **优势**：开箱即用，内置全套 2026 开发工具链 (`uv`, `bun`, `trivy` 等)，环境高度一致。
- **快速开始**：请直接查阅 [**Docker 部署手册**](../docker/README.md)。

---

### 方案 B：本地二进制/SDK 运行 (⭐⭐)
**适用场景**：源码定制、嵌入式集成、极简原型开发。
- **前置准备**：
  1. 安装 **Go 1.25+**。
  2. 安装并登录 [Claude Code](https://claude.ai/install.sh) 或 [OpenCode](https://opencode.ai)。

#### 1. 快速编译
```bash
git clone https://github.com/hrygo/hotplex.git
cd hotplex
make build
```

#### 2. 配置环境
```bash
cp .env.example .env
# 编辑 .env 填入你的 Slack/Feishu 凭据
```

#### 3. 启动引擎
```bash
./dist/hotplexd --config configs/chatapps
```

---

## 📖 下一步指南

- **1+n 机器人编排**：查阅 [HotPlex Matrix 指南](../docker/matrix/README.md)。
- **ChatApps 深入配置**：查阅 [Slack 接入手册](./chatapps/chatapps-slack-manual.md)。
- **架构深度解析**：查阅 [核心架构文档](./architecture.md)。

---
*由 HotPlex 智控系统生成 - 2026-03*
