---
title: Bot 话术定制教程
weight: 5
description: 使用 Phrases 系统定制 Bot 的问候语、欢迎卡片、状态提示等 UI 话术，10 分钟上手
---

# Bot 话术定制教程

HotPlex 的 **Phrases 系统**让 Bot 运维者无需改代码即可定制 Bot 在飞书/Slack 中显示的所有 UI 话术——问候语、欢迎卡片、状态提示、完成语等。

**前置条件**：HotPlex v1.14.0+ 已安装并运行。

## 核心概念

Phrases 是一个**加权随机话术池**。每个 Bot 在展示 UI 时（如发送占位卡片、欢迎卡片），从对应分类中随机选择一条话术。

**三级 cascade 加载**，越具体优先级越高：

```
~/.hotplex/phrases/
├── PHRASES.md              ← 全局（所有 Bot 共享，权重 2）
├── feishu/
│   ├── PHRASES.md          ← 平台级（该平台所有 Bot，权重 1）
│   └── ou_xxxxxxx/
│       └── PHRASES.md      ← Bot 级（仅此 Bot，权重 4）
└── slack/
    └── U12345/
        └── PHRASES.md
```

**权重决定选中概率**：Bot 级（4）的条目被选中概率约 4 倍于平台级（1）。

**合并规则**：同一 category 下，三级条目**累加**而非替换。如果外部文件为某个 category 提供了任何条目，代码内置默认值**全部排除**。

## 内置分类一览

| 分类 | 用途 | 平台 | 默认条目数 |
|------|------|------|-----------|
| `greetings` | 占位卡片第一行 | 飞书 | 8 |
| `tips` | 占位卡片第二行（CLI 提示） | 飞书 | 17 |
| `persona` | 处理中的状态提示 | 飞书 | 8 |
| `closings` | 任务完成时的结束语 | 飞书 | 8 |
| `status` | Assistant API 状态文本 | Slack | 4 |
| `welcome` | 新用户欢迎卡片问候 | 飞书 | 2 |
| `welcome_back` | 回访用户欢迎卡片问候 | 飞书 | 2 |
| `capabilities_header` | 欢迎卡片「能力」区块标题 | 飞书 | 1 |
| `capabilities` | 欢迎卡片能力描述列表 | 飞书 | 3 |
| `commands_header` | 欢迎卡片「命令」区块标题 | 飞书 | 1 |
| `quick_commands` | 欢迎卡片快捷命令列表 | 飞书 | 3 |
| `closing_line` | 欢迎卡片结尾行 | 飞书 | 1 |

`welcome` 和 `welcome_back` 支持 `{bot_name}` 占位符，发送时自动替换为 Bot 名称。

## 文件格式

PHRASES.md 使用 Markdown 格式，`## 分类名` 标题 + `- 条目` 列表：

```markdown
## Greetings
- 来啦～
- 交给我～
- 收到，马上处理！

## Tips
- 输入 /help 查看所有可用命令
- 输入 /reset 可重置上下文

## Welcome
- Hi，我是 {bot_name}，你的专属 AI 助手！
- 欢迎来到 {bot_name} 的世界～
```

分类名不区分大小写。非 `## ` 标题和非 `- ` 列表的行会被静默忽略。

## 实操 1：全局自定义话术

为所有 Bot 添加自定义问候语：

```bash
mkdir -p ~/.hotplex/phrases
```

创建 `~/.hotplex/phrases/PHRASES.md`：

```markdown
## Greetings
- 你好！我是你的编程搭子
- 在的，随时可以开始

## Closings
- 搞定了！有事随时 @我
- 任务完成，喝茶去了 ☕
```

重启 gateway 生效：`hotplex gateway restart`

此时所有 Bot 的 `greetings` 和 `closings` 池使用你的自定义条目（默认值被排除），其余 category 仍使用内置默认。

## 实操 2：Bot 级定制欢迎卡片

为特定飞书 Bot 定制完整的欢迎卡片话术：

```bash
mkdir -p ~/.hotplex/phrases/feishu/ou_xxxxxxx
```

创建 `~/.hotplex/phrases/feishu/ou_xxxxxxx/PHRASES.md`：

```markdown
## Welcome
- 👋 你好！我是 {bot_name}，你的代码审查专家

## Welcome_back
- 欢迎回来～上次我们讨论的 PR 有新进展了吗？

## Capabilities_header
- 我擅长以下工作：

## Capabilities
- 🔍 深度代码审查，发现潜在问题
- 🏗️ 架构设计建议和重构方案
- 📊 性能分析和优化建议

## Commands_header
- 常用命令：

## Quick_commands
- /help — 查看帮助
- /reset — 重置对话
- /cd — 切换项目目录

## Closing_line
- 发消息给我，我们开始吧 🚀
```

替换 `ou_xxxxxxx` 为你的飞书 Bot 实际 ID。重启后该 Bot 使用 Bot 级话术（权重 4，约 67% 选中率），其他 Bot 不受影响。

## 实操 3：自定义处理状态

让占位卡片显示更有趣的状态：

```markdown
## Persona
- 🧠 正在回忆上次对话...
- 📋 加载技能库...
- 🔧 调试模式启动中...
- ☕ 泡杯咖啡先...
- 🎯 锁定目标，准备开干！
```

每次请求随机展示 2 条不同的 persona 状态（系统自动去重）。

## 实操 4：让 Bot 自行管理话术

Bot 通过 B 通道技能手册（`~/.hotplex/skills/phrases.md`）了解完整规范。你可以直接让 Bot 自己修改话术：

> "帮我把飞书 Bot 的问候语换成更活泼的风格"

Bot 会读取技能手册，然后修改对应的 PHRASES.md 文件。你只需执行 `hotplex gateway restart` 即可生效。

## 注意事项

- **生效方式**：修改后需要重启 gateway（`hotplex gateway restart`），Phrases 不支持热更新
- **路径安全**：Bot ID 中的 `/` 和 `..` 会被拒绝，防止路径穿越
- **部分覆盖**：只为某些 category 提供条目不会影响其他 category 的默认值
- **空文件**：PHRASES.md 可以是空的，此时该级别的所有 category 不提供条目
