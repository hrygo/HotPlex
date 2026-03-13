# 🤖 多机器人实例 (1+n Matrix) 部署指南

本指南介绍如何在 HotPlex Matrix 架构下运行多个机器人实例，并为每个机器人配置独立的平台通道（如 Slack/飞书）。

---

## 🏗️ 架构概览：1+n Matrix

HotPlex 采用了 **1（主机器人）+ n（从机器人）** 的编排模式。
- **物理隔离**: 每个机器人拥有独立的宿主机数据路径 `~/.hotplex/instances/${HOTPLEX_BOT_ID}/`。
- **配置同步**: 全局规则（`configs/`）由所有机器人共享，敏感凭据由各自的 `.env-nn` 维护。
- **自动化流**: 通过 `Makefile` 自动化处理从目录创建到容器拉起的全过程。

---

## 🛠️ 第一步：创建额外的平台 App

1. **Slack**: 参照官方流程创建一个新的 App 并启用 Socket Mode。
2. **凭据准备**: 你需要以下 4 个核心参数：
   - `HOTPLEX_SLACK_BOT_TOKEN` (xoxb-...)
   - `HOTPLEX_SLACK_APP_TOKEN` (xapp-...)
   - `HOTPLEX_SLACK_SIGNING_SECRET`
   - `HOTPLEX_SLACK_BOT_USER_ID` (可以在 Slack App Home 查看)

---

## 🛠️ 第二步：配置环境文件

在 `docker/matrix/` 目录下，为每个机器人创建一个 `.env-nn` 文件（例如 `.env-02`）：

```bash
# 机器人唯一标识 (决定隔离路径名称)
HOTPLEX_BOT_ID=MySecondaryBot

# 该实例使用的镜像 (推荐使用 hotplex:go 或 hotplex:python)
HOTPLEX_IMAGE=hotplex:go

# 平台凭据
HOTPLEX_SLACK_MODE=socket
HOTPLEX_SLACK_BOT_TOKEN=xoxb-your-token
HOTPLEX_SLACK_APP_TOKEN=xapp-your-token
HOTPLEX_SLACK_SIGNING_SECRET=your-secret
HOTPLEX_SLACK_BOT_USER_ID=UXXXXXXXXXX
```

---

## 🛠️ 第三步：规则配置 (Matrix Sync)

编辑项目根目录下的 `configs/chatapps/slack.yaml`。由于所有实例共享此配置，我们推荐使用**多机器人路由策略**：

```yaml
security:
  permission:
    group_policy: multibot  # 开启智能路由
    bot_user_id: ${HOTPLEX_SLACK_BOT_USER_ID} # 动态插值
```

> [!TIP]
> **智能路由 (Multibot)**: 当在频道中 @ 被提到时，只有对应的机器人会响应；如果没有 @ 提到（广播），所有机器人都将发出简单的问候而非执行指令，避免刷屏。

---

## 🚀 第四步：一键启动

回到项目根目录，运行：

```bash
make docker-up
```

该指令会顺序执行：
1. **`docker-prepare`**: 根据你的 `.env-nn` 文件自动在 `~/.hotplex/instances/` 下创建隔离的目录。
2. **`docker-sync`**: 将 `configs/` 中的 YAML 规则分发出每个容器。
3. **`docker compose`**: 拉起所有机器人实例。

---

## 📂 进阶：如何管理多个机器人？

### 1. 物理路径检查
如果你需要查看某个机器人的数据库或日志，可以在宿主机访问：
```bash
ls ~/.hotplex/instances/${HOTPLEX_BOT_ID}/storage
```

### 2. 添加更多机器人
运行助手脚本：
```bash
./docker/matrix/add-bot.sh
```

### 3. 热更新规则
修改 `configs/` 下的文件后，无需重启容器，只需运行：
```bash
make docker-sync
```

---
*由 HotPlex 智控系统生成 - 2026-03*
