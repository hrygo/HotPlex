---
title: "HotPlex 5 分钟快速上手"
weight: 2
description: "从零开始安装、配置并运行 HotPlex Worker Gateway"
---

# HotPlex 5 分钟快速上手

HotPlex 是 AI Coding Agent 统一接入层，让你通过 Slack、飞书或 WebChat 与 Claude Code / OpenCode Server 交互。

**读完本文你将**：在本地跑起 HotPlex Gateway，通过 WebChat 发出第一条 AI 对话。

## AI Agent

**AI Agent：** 阅读 [INSTALL.md](https://github.com/hrygo/hotplex/blob/main/INSTALL.md) 并完成 hotplex 安装和配置。

---

## Core -- 5 分钟跑起来

### 1. 安装

从 [GitHub Releases](https://github.com/hrygo/hotplex/releases/latest) 下载对应平台的归档文件并解压。
解压后二进制文件名为 `hotplex-{os}-{arch}`（如 `hotplex-darwin-arm64`），建议重命名：

**macOS (Apple Silicon)**
```bash
curl -L https://github.com/hrygo/hotplex/releases/latest/download/hotplex-darwin-arm64.tar.gz | tar xz
chmod +x hotplex-darwin-arm64 && mv hotplex-darwin-arm64 hotplex
```

**macOS (Intel)**
```bash
curl -L https://github.com/hrygo/hotplex/releases/latest/download/hotplex-darwin-amd64.tar.gz | tar xz
chmod +x hotplex-darwin-amd64 && mv hotplex-darwin-amd64 hotplex
```

**Linux (AMD64)**
```bash
curl -L https://github.com/hrygo/hotplex/releases/latest/download/hotplex-linux-amd64.tar.gz | tar xz
chmod +x hotplex-linux-amd64 && mv hotplex-linux-amd64 hotplex
```

**Linux (ARM64)**
```bash
curl -L https://github.com/hrygo/hotplex/releases/latest/download/hotplex-linux-arm64.tar.gz | tar xz
chmod +x hotplex-linux-arm64 && mv hotplex-linux-arm64 hotplex
```

**Windows (AMD64)**
```powershell
Invoke-WebRequest -Uri "https://github.com/hrygo/hotplex/releases/latest/download/hotplex-windows-amd64.zip" -OutFile "hotplex.zip"
Expand-Archive -Path hotplex.zip -DestinationPath .
Rename-Item hotplex-windows-amd64.exe hotplex.exe
```

**Windows (ARM64)**
```powershell
Invoke-WebRequest -Uri "https://github.com/hrygo/hotplex/releases/latest/download/hotplex-windows-arm64.zip" -OutFile "hotplex.zip"
Expand-Archive -Path hotplex.zip -DestinationPath .
Rename-Item hotplex-windows-arm64.exe hotplex.exe
```

或从源码构建：`git clone` → `make build`，产物在 `bin/` 目录。

验证：`hotplex version`，应输出版本号（如 `v1.28.0` 或更高版本）。

### 2. 环境配置

```bash
cp configs/env.example .env
```

编辑 `.env`，填入必填项：

```bash
# 生成命令: openssl rand -base64 32 | tr -d '/+=' | head -c 43
HOTPLEX_ADMIN_TOKEN_1=<your-admin-token>
```

### 3. 前置依赖

AI 功能依赖 Claude Code CLI，确认已安装：`claude --version`。
如未安装，参考 [Claude Code 官方文档](https://docs.anthropic.com/en/docs/claude-code)。

### 4. 交互式配置

```bash
hotplex onboard    # 默认写入 ~/.hotplex/config.yaml 与同目录 .env
hotplex doctor     # 验证：所有检查项应显示 PASS 或可接受的 WARN
```

### 5. 启动 Gateway

```bash
hotplex gateway start
```

验证：

```bash
curl http://localhost:8888/health   # → {"status":"ok"}
open http://localhost:8888          # → WebChat 界面
```

看到 WebChat 界面即启动成功，在输入框发送消息即可开始 AI 对话。

开发模式下可跳过认证：`hotplex dev`（等效 `gateway start --dev`，禁用 API Key 和 Admin Token 校验）。

---

## Platform Integration -- 连接 Slack / 飞书

### Slack

```bash
# .env 中添加
HOTPLEX_MESSAGING_SLACK_ENABLED=true
HOTPLEX_MESSAGING_SLACK_BOT_TOKEN=xoxb-...
HOTPLEX_MESSAGING_SLACK_APP_TOKEN=xapp-...
```

可重新运行 `hotplex onboard --enable-slack` 自动生成配置。详细步骤见 [Slack 集成教程](tutorials/slack-integration.md)。

### 飞书 (Feishu)

```bash
# .env 中添加
HOTPLEX_MESSAGING_FEISHU_ENABLED=true
HOTPLEX_MESSAGING_FEISHU_APP_ID=cli_xxxxxxxxxxxx
HOTPLEX_MESSAGING_FEISHU_APP_SECRET=<your-app-secret>
```

可重新运行 `hotplex onboard --non-interactive --enable-feishu --feishu-allow-from ou_xxx` 自动生成配置（凭据从同目录 `.env` 读取）。
详细步骤见 [飞书集成教程](tutorials/feishu-integration.md)。

### 重启生效

修改 `.env` 或 `config.yaml` 后：`hotplex gateway restart`

---

## Advanced Setup

### 系统服务

```bash
hotplex service install            # 用户级守护进程（无需 root），开机自启
hotplex service start
hotplex service status
hotplex service logs -f            # 实时日志
# 系统级服务: sudo hotplex service install --level system
```

### 自更新

```bash
hotplex update              # 交互式更新
hotplex update --check      # 仅检查
hotplex update -y --restart # 跳过确认并重启
```

### 定时任务 (Cron)

自然语言描述任务，Gateway 自动调度并将结果回传 Slack / 飞书：

```bash
hotplex cron create \
  --name "daily-report" --schedule "cron:0 9 * * 1-5" \
  -m "生成昨日项目日报" \
  --bot-id "$BOT_ID" --owner-id "$USER_ID"
```

### 自定义工作区根目录（HOTPLEX_HOME）

默认情况下，HotPlex 的所有状态都存放在 `~/.hotplex/`。设置 **`HOTPLEX_HOME`** 环境变量即可把整个工作区迁移到任意目录（如独立数据盘 `/data/hotplex`）——**一个环境变量控制全部状态**，不再出现"配置在 A、数据在 B"的分离。

| 内容 | 默认位置 | 设置 `HOTPLEX_HOME=/data/hotplex` 后 |
|------|---------|--------------------------------------|
| 默认配置文件 | `~/.hotplex/config.yaml` | `/data/hotplex/config.yaml` |
| 数据 / 日志 / PID | `~/.hotplex/data|logs|.pids/` | `/data/hotplex/data|logs|.pids/` |
| Agent 人格 | `~/.hotplex/agent-configs/` | `/data/hotplex/agent-configs/` |
| skills / phrases | `~/.hotplex/skills|phrases/` | `/data/hotplex/skills|phrases/` |
| Worker 默认工作目录 | `~/.hotplex/workspace` | `/data/hotplex/workspace` |
| WebChat 工作区沙箱 | `~/.hotplex/workspaces/` | `/data/hotplex/workspaces/` |

**配置方法**（shell 配置文件、systemd 或开发模式 `.env` 均可）：

```bash
# 方式一：shell 环境变量（当前会话生效）
export HOTPLEX_HOME=/data/hotplex
hotplex gateway start

# 方式二：写入 shell 配置文件（每次登录自动生效）
echo 'export HOTPLEX_HOME=/data/hotplex' >> ~/.zshrc
source ~/.zshrc

# 方式三：systemd 用户级服务
systemctl --user edit hotplex
#   添加两行：
#     [Service]
#     Environment=HOTPLEX_HOME=/data/hotplex
```

> 开发模式（`make dev`）由 `scripts/dev.sh` 加载项目根目录 `.env`；普通 Gateway/service
> 启动会读取与所用 `config.yaml` 同目录的 `.env`。因此 service 通常使用 `~/.hotplex/.env`，
> 直接指定仓库内配置时应确认 `.env` 位于该配置目录，或由启动脚本显式导出环境变量。

**迁移已有数据**：`HOTPLEX_HOME` 不会自动搬运旧数据，请手动迁移：

```bash
hotplex service stop
mv ~/.hotplex /data/hotplex
export HOTPLEX_HOME=/data/hotplex
hotplex service start
hotplex doctor        # 确认配置与数据目录一致、所有检查 PASS
```

**注意事项**：

- 未设置 `HOTPLEX_HOME` 时行为与旧版完全一致（回退 `~/.hotplex`）。
- 显式 `--config <path>` 始终优先于 `$HOTPLEX_HOME/config.yaml`。
- 请勿只设置变量而不迁移数据：设置后 gateway 会从新目录读取配置与数据，旧目录将被忽略。

### Agent 人格配置

通过 Markdown 文件定制 Agent 行为（目录为 `$HOTPLEX_HOME/agent-configs/`，未设置 `HOTPLEX_HOME` 时为 `~/.hotplex/agent-configs/`）：

```
~/.hotplex/agent-configs/
  SOUL.md      # 人格定义（B 通道）
  AGENTS.md    # 行为规则（B 通道）
  USER.md      # 用户信息（C 通道）
  MEMORY.md    # 长期记忆（C 通道）
```

修改后对新会话生效，运行中会话需 `/reset` 重载。

### 端口说明

| 服务 | 地址 | 用途 |
|------|------|------|
| Gateway | localhost:8888 | WebSocket + WebChat + API |
| Admin API | localhost:9999 | 管理 API（Cron、Session） |

---

## 下一步

- [使用指南](guides/user/chat-with-ai.md) -- 完整功能参考
- [架构概览](guides/contributor/architecture.md) -- 系统架构详解
- [安全模型](explanation/security-model.md) -- 安全策略与最佳实践
- [配置参考](reference/configuration.md) -- API 与配置参数手册
