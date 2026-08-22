---
version: 5
description: "HotPlex environment-specific tool usage guidance"
---

# TOOLS.md

本文件提供 HotPlex 环境中的工具使用指南、偏好和边界。它不是 Agent Skill
目录，也不保证某个工具在当前会话可用；实际能力以 Worker、MCP 或宿主暴露的
结构化工具定义为准。

## 架构

    用户 → 消息平台 (Slack/飞书/WebChat) → HotPlex 网关 → Worker (你)

网关管理连接、路由、心跳和 Session 生命周期。Worker 负责理解请求、调用当前
会话实际暴露的工具并生成结果，不应自行接管网关状态或传输协议。

## 常用工具指南

### Slack CLI

通过 `hotplex slack` 命令操作 Slack：

| 命令 | 用途 |
|------|------|
| `hotplex slack send-message --channel <id> --text "..."` | 发送消息（支持 mrkdwn） |
| `hotplex slack upload-file --file <path> --title "..."` | 上传文件 |
| `hotplex slack list-channels --types im,public_channel` | 列出频道 |
| `hotplex slack react add --channel <id> --ts <ts> --emoji <name>` | 添加 Emoji 反应 |
| `hotplex slack bookmark add/list/remove` | 书签管理 |
| `hotplex slack schedule-message --text "..." --at <RFC3339>` | 定时发送 |

### 飞书 CLI

通过 `lark-cli` 操作飞书：

| 命令 | 用途 |
|------|------|
| `lark-cli im +messages-send --chat-id <id> --markdown "..."` | 发送消息 |
| `lark-cli im +messages-reply --message-id <id> --text "..."` | 回复消息 |
| `lark-cli im +chat-search --query "..."` | 搜索群组 |
| `lark-cli docs` / `lark-cli drive` | 文档与云盘操作 |
| `lark-cli base` | 多维表格操作 |

### Cron 定时任务

通过 `hotplex cron` 创建定时、延迟或周期任务。先使用 `hotplex cron --help`
确认当前版本实际支持的子命令和参数。

### 语音

STT 可将语音转写为文本，TTS 可合成语音输出。是否启用及可用的 provider 以
当前运行时配置为准。

## 平台特性

| 平台 | 输出特点 |
|------|---------|
| Slack | 消息分块、Markdown 转换、限流流式 |
| 飞书 | 流式卡片、交互按钮、卡片 TTL |
| WebChat | 完整 Markdown、实时流式 |

## 网关命令

`/gc`、`/park`、`/reset`、`/new`、`/cd <path>` 由网关处理。Worker 不应
模拟这些命令的网关状态变更。

## 配置层级

本槽位支持全局、平台和 Bot 三级 fallback，高作用域完整替换低作用域：

- 全局级：`~/.hotplex/agent-configs/TOOLS.md`
- 平台级：`~/.hotplex/agent-configs/slack/TOOLS.md`
- Bot 级：`~/.hotplex/agent-configs/slack/<botName>/TOOLS.md`

文件缺失表示继承；文件存在但正文为空表示显式清空。修改后对新 Session 或
明确执行 `/reset` 后生效。

真实 Agent Skills 由独立的 `<name>/SKILL.md` 定义并按需加载，不在本文件中
声明或展开。需要调整 AgentConfig 时，应先检查有效来源并展示 diff，获得用户
批准后再通过受控配置接口写入。
