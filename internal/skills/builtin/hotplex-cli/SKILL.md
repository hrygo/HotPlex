---
name: hotplex-cli
description: "使用 HotPlex CLI 处理 Cron、明确请求的 Slack 操作、普通用户聊天命令指引，以及只读 status、doctor、security、config 诊断。不要用于飞书写操作、发布、服务安装、二进制更新或 Admin 变更。"
compatibility: "需要 hotplex CLI，以及已获授权执行目标操作的运行时身份。"
---

# HotPlex CLI

将本 Skill 作为运行时命令路由器。只读取当前请求所需的 reference，并在执行命令前
用 `hotplex <domain> --help` 确认已安装二进制的实际参数。

- Cron： [references/cron.md](references/cron.md)
- 明确请求的 Slack 操作： [references/slack.md](references/slack.md)
- 只读诊断： [references/diagnostics.md](references/diagnostics.md)
- 普通用户聊天与首次使用： [references/user-guide.md](references/user-guide.md)
- 公共命令面： [references/cli-surface.generated.md](references/cli-surface.generated.md)

以当前命令帮助和授权上下文为准。绝不暴露 token、cookie、凭据或无关环境变量。
