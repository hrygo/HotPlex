---
version: 6
description: "HotPlex capability routing and safety guidance"
---

# TOOLS.md

本文件是 HotPlex 的常驻操作路由，不是工具或 Agent Skill 清单，也不保证某个工具在当前会话可用。能力以可信 runtime facts、当前 Worker/Gateway/MCP 查询和宿主实际暴露的结构化接口为准。

## 能力发现与 Agent Skills

按以下顺序确认能力：runtime facts → live Worker/Gateway query → `/skills` 或 `/mcp` → 已激活的 Agent Skill → 本文件路由 → `hotplex <domain> --help`。只有状态为 `callable` 的 Skill 才可调用；`discoverable` 或 `unavailable` 必须说明限制并停止猜测。Worker 原生机制负责 Skill metadata 和 `SKILL.md` 加载；本文件不复制 Skill 正文。

## Gateway 命令

`/help`、`/stop`、`/reset`、`/new`、`/gc`、`/park`、`/cd <path>`、`/skills`、`/mcp`、`/worker <name>` 由当前 Gateway/Worker 路由决定。先查询当前会话能力；不要模拟 Gateway 状态或把命令发送给 Worker 作为普通文本。

## 平台与 CLI 路由

- Slack 操作使用当前暴露的 `hotplex slack` 能力；只在请求明确涉及 Slack 时调用，并先用命令帮助确认参数。
- Feishu 操作使用 `lark-cli`，不把它当作 `hotplex-cli` 的别名。
- Cron 使用 `hotplex cron`。创建前确认 schedule、目标平台和授权；创建后用 `hotplex cron get <id|name> --json` 独立核对任务状态、schedule、platform 和 platform key。二次确认失败时报告 degraded，不重复创建。
- 若适用任务已激活 `hotplex-cli` Agent Skill，先按其流程执行；未激活时以当前二进制的 `--help` 为事实来源，不猜测隐藏参数。
- `status`、`doctor`、`security`、配置读取等诊断优先保持只读；Admin、服务、主机或其他特权操作需要对应的认证和明确授权。

## 运行时依赖

STT/TTS 是否可用取决于当前 runtime facts、Worker 和平台配置；未声明时说明不可用，不自行安装或替换 provider。不同平台的输出格式、限流和交互行为以当前平台能力为准。

## AgentConfig 生效

五个配置文件逐文件按 global → platform → Bot fallback；缺失才继承，present-empty 显式清空。配置修改只在新 Session 或明确执行 `/reset` 后生效；修改前读取有效来源，修改后独立验证。
