# Docker 多 Bot 部署新手指南

> **5 分钟上手**：使用 Docker Compose 一键运行多个 AI 机器人
>
> **[English](docker-multi-bot-deployment.md)** | **简体中文**

---

## 📋 目录

- [准备工作](#准备工作)
- [快速开始](#快速开始)
- [配置详解](#配置详解)
- [添加新 Bot](#添加新-bot)
- [常见问题](#常见问题)

---

## 准备工作

### 必需环境

| 工具           | 版本   | 检查命令                 |
| -------------- | ------ | ------------------------ |
| Docker         | 20.10+ | `docker --version`       |
| Docker Compose | v2+    | `docker compose version` |

### 获取 Slack 凭证

每个 Bot 需要在 [Slack API](https://api.slack.com/apps) 创建独立的应用：

```
┌─────────────────────────────────────────────────────────────┐
│  步骤 1: 创建 Slack App                                      │
│  ─────────────────────────────────────────────────────────  │
│  1. 访问 https://api.slack.com/apps                         │
│  2. 点击 "Create New App" → "From scratch"                  │
│  3. 填写 App Name（如：HotPlex-Bot-01）                      │
│  4. 选择你的 Workspace                                       │
└─────────────────────────────────────────────────────────────┘
          │
          ▼
┌─────────────────────────────────────────────────────────────┐
│  步骤 2: 启用 Socket Mode                                    │
│  ─────────────────────────────────────────────────────────  │
│  1. 左侧菜单 → Socket Mode                                   │
│  2. 开启 Socket Mode                                         │
│  3. 生成 App Token → 复制保存 (xapp-...)                     │
└─────────────────────────────────────────────────────────────┘
          │
          ▼
┌─────────────────────────────────────────────────────────────┐
│  步骤 3: 配置 OAuth & Permissions                            │
│  ─────────────────────────────────────────────────────────  │
│  1. 左侧菜单 → OAuth & Permissions                           │
│  2. 添加以下 Bot Token Scopes：                              │
│     • channels:history     • groups:history                  │
│     • im:history           • mpim:history                    │
│     • chat:write           • users:read                      │
│     • assistant:write      • files:write                     │
│  3. 安装到 Workspace                                         │
│  4. 复制 Bot User OAuth Token (xoxb-...)                     │
└─────────────────────────────────────────────────────────────┘
          │
          ▼
┌─────────────────────────────────────────────────────────────┐
│  步骤 4: 获取 Bot User ID                                    │
│  ─────────────────────────────────────────────────────────  │
│  1. 左侧菜单 → App Home                                      │
│  2. 找到 "Bot User" 部分                                     │
│  3. 复制 User ID (UXXXXXXXXXX)                               │
└─────────────────────────────────────────────────────────────┘
          │
          ▼
┌─────────────────────────────────────────────────────────────┐
│  步骤 5: 订阅事件                                            │
│  ─────────────────────────────────────────────────────────  │
│  1. 左侧菜单 → Event Subscriptions                           │
│  2. 开启 Enable Events                                       │
│  3. 添加以下 Bot Events：                                    │
│     • message.channels    • message.groups                   │
│     • message.im          • message.mpim                     │
└─────────────────────────────────────────────────────────────┘
```

**每个 Bot 需要获取的 3 个凭证：**

| 凭证          | 格式          | 来源                     |
| ------------- | ------------- | ------------------------ |
| `BOT_USER_ID` | `UXXXXXXXXXX` | App Home 页面            |
| `BOT_TOKEN`   | `xoxb-...`    | OAuth & Permissions 页面 |
| `APP_TOKEN`   | `xapp-...`    | Socket Mode 页面         |

---

## 快速开始

### 1. 克隆项目

```bash
git clone https://github.com/hrygo/hotplex.git
cd hotplex
```

### 2. 创建第一个 Bot 配置

```bash
# 复制环境变量模板
cp .env.example .env

# 编辑配置
vim .env
```

**修改 `.env` 中的关键配置：**

```bash
# Bot 01 凭证
HOTPLEX_SLACK_BOT_USER_ID=UXXXXXXXXXX      # 你的 Bot User ID
HOTPLEX_SLACK_BOT_TOKEN=xoxb-...            # 你的 Bot Token
HOTPLEX_SLACK_APP_TOKEN=xapp-...            # 你的 App Token

# GitHub Token（用于 Git 操作）
GITHUB_TOKEN=ghp_xxxx                       # 你的 GitHub PAT

# 日志（调试时用 text）
HOTPLEX_LOG_LEVEL=INFO
HOTPLEX_LOG_FORMAT=text
```

### 3. 配置 Git 身份

```bash
# 运行脚本自动配置
./scripts/setup_gitconfig.sh

# 或手动创建
cat > ~/.gitconfig-hotplex << 'EOF'
[user]
    name = HotPlex Bot
    email = bot@example.com
[init]
    defaultBranch = main
EOF
```

### 4. 启动服务

```bash
# 构建并启动
make docker-up

# 查看日志
make docker-logs

# 或直接
docker compose logs -f hotplex
```

### 5. 验证运行

```bash
# 检查容器状态
docker compose ps

# 预期输出：
# NAME         STATUS    PORTS
# hotplex      healthy   127.0.0.1:18080->8080/tcp
```

**🎉 完成！** 在 Slack 中 @ 你的 Bot 试试吧！

---

## 配置详解

### 目录结构

```
hotplex/
├── docker-compose.yml     # 多 Bot 编排配置
├── .env                   # Bot 01 环境变量
├── .env.secondary         # Bot 02 环境变量
├── chatapps/configs/
│   ├── slack.yaml         # Slack 平台配置
│   └── ...
└── scripts/
    └── setup_gitconfig.sh # Git 配置脚本
```

### docker-compose.yml 核心概念

```yaml
# 共享配置模板（YAML Anchor）
x-hotplex-common: &hotplex-common
  image: ghcr.io/hrygo/hotplex:latest
  restart: unless-stopped
  # ... 共享设置

services:
  # Bot 01 - 主 Bot
  hotplex:
    <<: *hotplex-common     # 引用共享配置
    container_name: hotplex
    ports:
      - "127.0.0.1:18080:8080"
    env_file:
      - .env                # Bot 01 的环境变量
    volumes:
      # 共享目录
      - ${HOME}/.hotplex:/home/hotplex/.hotplex:rw
      - ${HOME}/.claude:/home/hotplex/.claude:rw
      # Bot 01 专属目录 ⚠️ 必须唯一！
      - ${HOME}/.slack/BOT_U0AHRCL1KCM:/home/hotplex/projects:rw
      - ${HOME}/.gitconfig-hotplex:/home/hotplex/.gitconfig:ro

  # Bot 02 - 副 Bot
  hotplex-secondary:
    <<: *hotplex-common
    container_name: hotplex-secondary
    ports:
      - "127.0.0.1:18081:8080"
    env_file:
      - .env.secondary      # Bot 02 的环境变量
    volumes:
      # 共享目录（同上）
      - ${HOME}/.hotplex:/home/hotplex/.hotplex:rw
      - ${HOME}/.claude:/home/hotplex/.claude:rw
      # Bot 02 专属目录 ⚠️ 必须唯一！
      - ${HOME}/.slack/BOT_U0AJVRH4YF6:/home/hotplex/projects:rw
      - ${HOME}/.gitconfig-hotplex-secondary:/home/hotplex/.gitconfig:ro
```

### ⚠️ 关键隔离规则

| 隔离项            | 原因                     | 配置位置                      |
| ----------------- | ------------------------ | ----------------------------- |
| **projects 目录** | 每个 Bot 有独立工作空间  | volumes 中的 `.slack/BOT_xxx` |
| **gitconfig**     | 每个 Bot 有独立 Git 身份 | volumes 中的 `.gitconfig-xxx` |
| **端口**          | 避免端口冲突             | ports 中的 `18080/18081`      |
| **env_file**      | 每个 Bot 有独立凭证      | `.env` / `.env.secondary`     |

**❌ 错误示例（会导致冲突）：**

```yaml
# 错误：使用变量，所有 Bot 共享同一个目录
volumes:
  - ${HOTPLEX_PROJECTS_DIR}:/home/hotplex/projects  # ❌
```

**✅ 正确示例（硬编码路径）：**

```yaml
# 正确：每个 Bot 硬编码专属路径
volumes:
  - ${HOME}/.slack/BOT_U0AHRCL1KCM:/home/hotplex/projects  # ✅ Bot 01
  - ${HOME}/.slack/BOT_U0AJVRH4YF6:/home/hotplex/projects  # ✅ Bot 02
```

---

## 添加新 Bot

### 步骤 1: 创建 Slack App

按照 [准备工作](#准备工作) 创建新的 Slack App，获取凭证。

### 步骤 2: 创建环境变量文件

```bash
# 复制模板
cp .env .env.tertiary

# 编辑新配置
vim .env.tertiary
```

**修改 `.env.tertiary`：**

```bash
# Bot 03 凭证（与新 Slack App 对应）
HOTPLEX_SLACK_BOT_USER_ID=UYYYYYYYYYY      # 新 Bot 的 User ID
HOTPLEX_SLACK_BOT_TOKEN=xoxb-yyyy          # 新 Bot 的 Token
HOTPLEX_SLACK_APP_TOKEN=xapp-yyyy          # 新 Bot 的 App Token
```

### 步骤 3: 创建工作目录

```bash
# 创建 Bot 专属项目目录
mkdir -p ~/.slack/BOT_UYYYYYYYYYY

# 创建 Git 配置
cat > ~/.gitconfig-hotplex-tertiary << 'EOF'
[user]
    name = HotPlex Bot 03
    email = bot03@example.com
[init]
    defaultBranch = main
EOF
```

### 步骤 4: 添加到 docker-compose.yml

```yaml
  # ============================================================================
  # Bot 03: Tertiary Bot
  # ============================================================================
  hotplex-tertiary:
    <<: *hotplex-common
    container_name: hotplex-tertiary
    depends_on:
      hotplex:
        condition: service_started
    ports:
      - "127.0.0.1:18082:8080"      # 新端口
    env_file:
      - .env.tertiary               # 新环境变量
    volumes:
      # 共享目录
      - ${HOME}/.hotplex:/home/hotplex/.hotplex:rw
      - ${HOME}/.claude:/home/hotplex/.claude:rw
      - ${HOME}/.claude/settings.json:/home/hotplex/.claude/settings.json:ro
      - hotplex-go-mod:/home/hotplex/go/pkg/mod:rw
      - hotplex-go-build:/home/hotplex/.cache/go-build:rw
      # Bot 03 专属目录
      - ${HOME}/.slack/BOT_UYYYYYYYYYY:/home/hotplex/projects:rw
      - ${HOME}/.gitconfig-hotplex-tertiary:/home/hotplex/.gitconfig:ro
    labels:
      - "hotplex.bot.role=tertiary"
      - "hotplex.bot.config=.env.tertiary"
```

### 步骤 5: 更新 Git 配置脚本

编辑 `scripts/setup_gitconfig.sh`，添加新 Bot：

```bash
BOT_CONFIGS=(
  "hotplex:HotPlex Bot:bot@example.com"
  "hotplex-secondary:HotPlex Bot 02:bot02@example.com"
  "hotplex-tertiary:HotPlex Bot 03:bot03@example.com"  # 新增
)
```

### 步骤 6: 启动新 Bot

```bash
# 启动所有 Bot
make docker-up

# 或只启动新 Bot
docker compose up -d hotplex-tertiary

# 查看日志
docker compose logs -f hotplex-tertiary
```

---

## 常用命令

### 服务管理

```bash
# 启动所有 Bot
make docker-up

# 停止所有 Bot
make docker-down

# 重启（并同步配置）
make docker-restart

# 查看日志
make docker-logs
docker compose logs -f hotplex           # Bot 01
docker compose logs -f hotplex-secondary # Bot 02

# 查看状态
docker compose ps

# 进入容器调试
docker exec -it hotplex /bin/sh
```

### 单独管理

```bash
# 只启动 Bot 01
docker compose up -d hotplex

# 只重启 Bot 02
docker compose restart hotplex-secondary

# 只查看 Bot 01 日志
docker compose logs -f hotplex
```

### 更新镜像

```bash
# 拉取最新镜像
docker pull ghcr.io/hrygo/hotplex:latest

# 重新启动
make docker-down
make docker-up
```

---

## 常见问题

### Q1: Bot 无响应

**检查步骤：**

```bash
# 1. 检查容器状态
docker compose ps

# 2. 查看日志
docker compose logs hotplex | tail -50

# 3. 常见原因
```

| 错误信息              | 原因                             | 解决方案                          |
| --------------------- | -------------------------------- | --------------------------------- |
| `invalid bot_user_id` | `HOTPLEX_SLACK_BOT_USER_ID` 错误 | 检查 User ID 格式                 |
| `invalid_auth`        | Token 无效                       | 重新安装 App 获取新 Token         |
| `missing scope`       | 权限不足                         | 添加所需 OAuth Scope              |
| `connection refused`  | Socket Mode 未启用               | 启用 Socket Mode 并生成 App Token |

### Q2: 多 Bot 消息混乱

**原因：** Bot 隔离配置错误

**检查：**

```bash
# 确认每个 Bot 有唯一的工作目录
docker exec hotplex ls -la /home/hotplex/projects
docker exec hotplex-secondary ls -la /home/hotplex/projects

# 应该看到不同的内容
```

**解决：** 确保 `docker-compose.yml` 中每个 Bot 的 volumes 路径唯一。

### Q3: Git 操作失败

**原因：** Git 配置缺失

**解决：**

```bash
# 检查 Git 配置是否存在
docker exec hotplex cat /home/hotplex/.gitconfig

# 重新生成
./scripts/setup_gitconfig.sh
```

### Q4: 代理配置

如果在中国或企业网络环境，需要配置代理：

```yaml
# docker-compose.yml 中取消注释
environment:
  ANTHROPIC_BASE_URL: http://host.docker.internal:15721
  HTTP_PROXY: http://host.docker.internal:7897
  HTTPS_PROXY: http://host.docker.internal:7897
```

**要求：**
1. 代理软件开启 "Allow LAN"
2. 端口与代理配置匹配

### Q5: 端口冲突

**错误：** `port is already allocated`

**解决：**

```bash
# 查找占用端口的进程
lsof -i :18080

# 修改 docker-compose.yml 使用不同端口
ports:
  - "127.0.0.1:18090:8080"  # 改为未占用端口
```

---

## 架构图

```
┌─────────────────────────────────────────────────────────────────────────┐
│                         Docker Compose                                   │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│  ┌────────────────────┐    ┌────────────────────┐    ┌────────────────┐ │
│  │  hotplex           │    │  hotplex-secondary │    │ hotplex-tertiary│ │
│  │  (Bot 01)          │    │  (Bot 02)          │    │ (Bot 03)       │ │
│  │                    │    │                    │    │                │ │
│  │  Port: 18080       │    │  Port: 18081       │    │ Port: 18082    │ │
│  │  .env              │    │  .env.secondary    │    │ .env.tertiary  │ │
│  │                    │    │                    │    │                │ │
│  │  projects/         │    │  projects/         │    │ projects/      │ │
│  │  BOT_U0AHRCL1KCM   │    │  BOT_U0AJVRH4YF6   │    │ BOT_UYYYYYYYYYY│ │
│  └────────┬───────────┘    └────────┬───────────┘    └───────┬────────┘ │
│           │                         │                        │          │
│           └─────────────┬───────────┴────────────────────────┘          │
│                         │                                               │
│                         ▼                                               │
│           ┌─────────────────────────────┐                               │
│           │     Shared Resources        │                               │
│           │  • ~/.hotplex (DB, configs) │                               │
│           │  • ~/.claude (sessions)     │                               │
│           │  • Go cache volumes         │                               │
│           └─────────────────────────────┘                               │
│                                                                          │
└─────────────────────────────────────────────────────────────────────────┘
                          │
                          ▼
           ┌─────────────────────────────┐
           │        Slack Workspace      │
           │                             │
           │  #general ── @Bot01 ──→ 响应│
           │  #random  ── @Bot02 ──→ 响应│
           │  #dev     ── @Bot03 ──→ 响应│
           └─────────────────────────────┘
```

---

## 相关文档

- [Docker 部署指南](docker-deployment_zh.md) - 单 Bot 部署
- [生产环境指南](production-guide_zh.md) - 生产最佳实践
- [Slack 新手指南](chatapps/slack-setup-beginner_zh.md) - Slack 配置详解
- [configuration_zh.md](configuration_zh.md) - 完整配置参考

---

<div align="center">
  <i>如有问题，请在 <a href="https://github.com/hrygo/hotplex/issues">GitHub Issues</a> 提出</i>
</div>
