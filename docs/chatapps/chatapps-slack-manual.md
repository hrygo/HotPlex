# 🚀 HotPlex Slack 机器人全功能手册

> 📅 基于 **Slack 2026 最新官方标准** 编写 | 最后更新: 2026-03-04
>
> 本手册涵盖 2026 年新特性：MCP Server、实时搜索 API、新 Block Kit 组件、AI 流式响应等

---

## ⚡ 快捷集成：使用应用配置清单 (App Manifest)

这是最推荐的安装方式。你无需手动点击几十个按钮，只需复制以下代码即可一键配置。

> ⚠️ **2026 重要提醒**：Classic Apps 将于 **2026年11月16日** 停用，请确保使用新版 App Manifest。

1.  访问 [Slack API 控制台](https://api.slack.com/apps) -> **Create New App** -> **From an app manifest**。
2.  选择你的 Workspace，在 JSON 选项卡中粘贴以下内容：

```json
{
  "_metadata": {
    "major_version": 2,
    "minor_version": 1
  },
  "display_information": {
    "name": "HotPlex",
    "long_description": "HotPlex is a high-performance AI Agent control plane that bridges powerful AI CLIs (Claude Code, OpenCode) into production-ready interactive services. It provides long-lived process sessions, WebSocket gateway, regex WAF, and PGID isolation for safe execution. Perfect for developers who want AI-powered coding assistance with full control.",
    "description": "HotPlex AI Agent - High-performance CLI automation assistant",
    "background_color": "#000000"
  },
  "features": {
    "assistant_view": {
      "assistant_description": "HotPlex is a high-performance AI Agent Control Plane (Cli-as-a-Service). It bridges Claude Code and OpenCode into interactive Slack services with long-lived sessions, PGID isolation, and regex WAF security. Perfect for teams wanting AI-powered development with full control.",
      "suggested_prompts": [
        {
          "title": "Write Code",
          "message": "Write a Go function that handles HTTP requests with proper error handling"
        },
        {
          "title": "Review PR",
          "message": "Review my latest PR for security issues and code quality"
        },
        {
          "title": "Debug",
          "message": "Debug this error and suggest a fix"
        },
        {
          "title": "Refactor",
          "message": "Refactor this code to follow Go best practices"
        }
      ]
    },
    "app_home": {
      "home_tab_enabled": false,
      "messages_tab_enabled": true,
      "messages_tab_read_only_enabled": false
    },
    "bot_user": {
      "display_name": "HotPlex",
      "always_online": true
    },
    "slash_commands": [
      {
        "command": "/reset",
        "description": "重置当前会话上下文并冷启动",
        "should_escape": false
      },
      {
        "command": "/dc",
        "description": "强制终止后台 CLI 进程但保留进度",
        "should_escape": false
      }
    ]
  },
  "oauth_config": {
    "scopes": {
      "bot": [
        "assistant:write",
        "app_mentions:read",
        "chat:write",
        "chat:write.public",
        "channels:read",
        "groups:read",
        "im:read",
        "im:write",
        "reactions:write",
        "im:history",
        "channels:history",
        "groups:history",
        "mpim:history",
        "files:write",
        "commands"
      ]
    }
  },
  "settings": {
    "event_subscriptions": {
      "bot_events": [
        "app_mention",
        "message.channels",
        "message.groups",
        "message.im",
        "assistant_thread_started",
        "assistant_thread_context_changed"
      ]
    },
    "org_deploy_enabled": false,
    "socket_mode_enabled": true
  }
}
```
---

### (进阶) HotPlex “Craw 层”高级治理版配置

如果您的团队希望完全释放 HotPlex 作为**底层执行引擎 (Craw Layer)** 的能力（如：沙盒审批、产物回传、全局监控），请使用以下增强版 App Manifest。

此版本开启了**App Home 主页控制台**、**深度的权限分离**以及**全功能的扩展命令**。

```json
{
  "_metadata": {
    "major_version": 2,
    "minor_version": 1
  },
  "display_information": {
    "name": "HotPlex",
    "long_description": "HotPlex is a high-performance AI Agent control plane with advanced governance features. It provides sandbox approval workflows, artifact mounting, global monitoring, and full runtime state inspection via /pgid command. Perfect for teams requiring controlled AI execution environments.",
    "description": "Agentic Craw Layer & Execution Engine",
    "background_color": "#1e293b"
  },
  "features": {
    "assistant_view": {
      "assistant_description": "HotPlex is a high-performance AI Agent Control Plane (Cli-as-a-Service) with advanced governance. Features include: long-lived sessions with PGID isolation, regex WAF security, sandbox approval workflows, and full runtime state inspection via /pgid command.",
      "suggested_prompts": [
        {
          "title": "Write Code",
          "message": "Write a Go function that handles HTTP requests with proper error handling"
        },
        {
          "title": "Review PR",
          "message": "Review my latest PR for security issues and code quality"
        },
        {
          "title": "Debug",
          "message": "Debug this error and suggest a fix"
        },
        {
          "title": "Check Status",
          "message": "Show current session status with /pgid"
        }
      ]
    },
    "app_home": {
      "home_tab_enabled": true,
      "messages_tab_enabled": true,
      "messages_tab_read_only_enabled": false
    },
    "bot_user": {
      "display_name": "HotPlex",
      "always_online": true
    },
    "slash_commands": [
      {
        "command": "/reset",
        "description": "彻底销毁当前 Session 的 PGID 及上下文",
        "should_escape": false
      },
      {
        "command": "/dc",
        "description": "对当前执行进程发送 SIGTERM (暂停执行)",
        "should_escape": false
      },
      {
        "command": "/pgid",
        "description": "打印当前会话底层的 CPU/内存 及进程树状态",
        "should_escape": false
      },
      {
        "command": "/approve",
        "description": "批准挂起中的高危工具操作 (HITL 审批)",
        "should_escape": false
      }
    ]
  },
  "oauth_config": {
    "scopes": {
      "bot": [
        "assistant:write",
        "app_mentions:read",
        "chat:write",
        "chat:write.public",
        "channels:read",
        "groups:read",
        "im:read",
        "im:write",
        "reactions:write",
        "im:history",
        "channels:history",
        "groups:history",
        "mpim:history",
        "commands",
        "files:read",
        "files:write",
        "users:read",
        "team:read"
      ]
    }
  },
  "settings": {
    "event_subscriptions": {
      "bot_events": [
        "app_mention",
        "message.channels",
        "message.groups",
        "message.im",
        "app_home_opened",
        "assistant_thread_started",
        "assistant_thread_context_changed"
      ]
    },
    "interactivity": {
      "is_enabled": true
    },
    "socket_mode_enabled": true
  }
}
```

### 进阶版配置带来的新能力：

1.  **全局监控中心 (`home_tab_enabled: true`)**：允许开发者在打开 Bot 时，渲染出包含“活跃会话数”、“安全拦截日志”和“ MCP 挂载状态”的 Dashboard。需要监听 `app_home_opened` 事件。
2.  **审批守门员 (`/approve`)**：结合互动消息能力 (Interactivity)，在执行写库、删除等敏感调用前强制拦截并要求核心开发者确认。
3.  **富产物挂载 (`files:read` / `files:write`)**：允许工程师直接向 Slack 丢报错日志附件，HotPlex 将其自动注入正在执行的沙盒文件系统中；Agent 也可直接生成并推送补丁包 (`.patch` 或 `zip`) 给团队。
4.  **运行时状态透视 (`/pgid`)**：一键穿透 LLM 迷雾，直接查询对应操作系统的资源开销，提供极客级别的排障手段。

---

## 🗝️ 第一步：获取权限密钥 (Tokens)

如果你通过上面的 Manifest 创建了应用，请直接前往以下页面复制密钥：

| 变量名             | 推荐格式   | 获取路径              | 作用说明                                                           |
| :----------------- | :--------- | :-------------------- | :----------------------------------------------------------------- |
| **Bot Token**      | `xoxb-...` | `OAuth & Permissions` | **APP 核心令牌**：用于发送消息和更新 UI。                          |
| **App Token**      | `xapp-...` | `Basic Information`   | **Socket 令牌**：启用 Socket Mode 必需（含 `connections:write`）。 |
| **Signing Secret** | 字符串     | `Basic Information`   | **安全验证**：HTTP 模式必需，必须 > 32 位字符。                    |

> 🔐 **2026 安全最佳实践**：
> - **禁止硬编码**：永远不要把 Token 写进代码仓库
> - **环境变量**：开发用 `.env`，生产用 Vault/ Secrets Manager
> - **IP 白名单**：在 OAuth & Permissions 中配置最多 10 个 CIDR 范围
> - **权限最小化**：只申请功能必需的 Scope

---

## 📡 第二步：运行模式配置

HotPlex 支持两种通信模式，请在 `.env` 中通过 `SLACK_MODE` 切换：

### 模式 A：Socket Mode (推荐)
- **原理**：基于 WebSocket，无需公网 IP 也能在内网甚至本地开发环境流畅运行。
- **配置**：`SLACK_MODE=socket`, `SLACK_APP_TOKEN=xapp-...`。

### 模式 B：HTTP Mode (生产 Webhook)
- **原理**：通过回调 URL 接收请求，适合高可用负载均衡环境。
- **配置**：`SLACK_MODE=http`, `SLACK_SIGNING_SECRET=...`。
- **URL 填写**：在 `Event Subscriptions` 中填写 `https://你的域名/webhook/slack/events`。

> 💡 **2026 推荐**：开发/本地用 **Socket Mode**，生产环境用 **HTTP Mode** + IP 白名单

---

## ⌨️ 第三步：全场景指令 (Slash & Thread)

为了解决 Slack 在 **Thread (消息列)** 中不支持斜杠命令的原生限制，HotPlex 提供了双模触发方案：

| 场景              | 触发方式     | 说明                                                         |
| :---------------- | :----------- | :----------------------------------------------------------- |
| **主频道/私聊**   | `/reset`     | 输入 `/` 会弹出自动补全，操作门槛最低。                      |
| **消息列/侧边栏** | **`#reset`** | 由于 Slack 限制，需手动输入 `#` 指令，适配器会自动拦截处理。 |

> [!NOTE]
> `/dc` 与 `#dc` 同理。用于在 AI 运行耗时任务（如扫描全库）时强制中断其后台工作流。

---

## ✨ 交互反馈：如何读懂机器人

### 1. 表情语义 (Reactions)
机器人会通过点按你消息下的表情来告知进展：
- 📥 (`:inbox:`)：请求已排队，正在准备计算环境。
- 🧠 (`:brain:`)：深度逻辑推理或大代码库扫描中。
- ✅ (`:white_check_mark:`)：逻辑闭环，任务圆满完成。
- ❌ (`:x:`)：内部错误或超时，请查看详细报错 Block。

### 2. 消息分区 (Zones)
你会看到一条消息内包含多个变动区域：
- **思考区**：展示推理路径（前序记录会自动折叠，仅保留首条锚点）。
- **行动区**：展示 `Bash`、`FileRead` 等工具调用状态。
- **展示区**：AI 的核心回答，支持打字机效果流式输出。

### 3. 2026 新特性：AI 流式响应 (Streaming)
2026 年 Slack 引入了原生 AI 流式响应支持：

| API                 | 功能         |
| ------------------- | ------------ |
| `chat.startStream`  | 启动流式响应 |
| `chat.appendStream` | 追加流式内容 |
| `chat.stopStream`   | 结束流式响应 |

> 🤖 HotPlex 已支持打字机效果，通过 `chat.postMessage` + 实时更新实现平滑流式输出。

### 4. 2026 新特性：MCP Server 集成
Slack 于 2026年2月17日 发布了官方 MCP Server，支持：
- AI 代理实时访问工作区数据
- 用户授权的数据操作
- 安全的上下文检索

> 📎 **相关 Scope**：`assistant:write` (AI 助手核心权限)

> ⚠️ **重要**：Slack 2026 要求 `assistant:write` 必须启用 "Agents & AI Apps" 功能：
> 1. 前往 [Slack API Console](https://api.slack.com/apps) → 你的 App
> 2. 打开 **"Agents & AI Apps"** 开关（需要付费版 Slack）
> 3. 或者在 App Manifest 的 `features.assistant_view` 中配置 `assistant_description`

---

## ✅ 高级配置全解 (slack.yaml)

在代码库的 `chatapps/configs/slack.yaml` 中可进行细粒度控制：

| 参数                   | 可选值            | 说明                                                                              |
| :--------------------- | :---------------- | :-------------------------------------------------------------------------------- |
| **`bot_user_id`**      | `U...`            | **强烈建议填写**。用于精准识别 Mention，避免环路。而在 Slack 机器人详情页可复制。 |
| **`dm_policy`**        | `allow`/`pairing` | `pairing` 模式下，仅限在频道中 @ 过机器人的用户可进行私聊，保障私密性。           |
| **`group_policy`**     | `allow`/`mention` | `mention` 模式下，机器人只响应明确被 @ 的消息，不监听频道闲聊。                   |
| **`allowed_users`**    | ID 列表           | 用户白名单，仅限这些 ID 的用户可以使用机器人（ID 格式如 `U01234567`）。           |
| **`allowed_tools`**    | 字符串数组        | 工具白名单。如果设置，Agent 仅能使用这些工具（如 `["Bash", "Edit"]`）。           |
| **`disallowed_tools`** | 字符串数组        | 工具黑名单。如果设置，Agent 将被禁止使用这些工具。                                |

> [!TIP]
> **工具过滤优先级**：`provider` 层面的工具过滤配置（`provider.allowed_tools`）会优先于 `engine` 层面的配置。如果两者都未设置，则默认允许所有工具。

---

## 🚑 常见故障排查

1. **机器人没有 ID？**
   - 进入 Slack，点击机器人头像查看 Profile，点击图标旁边的 `...` -> `Copy member ID`。
2. **"Dispatch failed"?**
   - 确认 `.env` 中的 `SLACK_MODE` 与你在 Slack 后台启用的功能匹配（例如开启了 Socket Mode 但配了 `http` 模式）。
3. **消息不更新或权限不足？**
   - 检查 `Bot Token` 是否失效。
   - **重要提醒**：如果你在 Slack 后台更新了 `Scopes`（权限范围），必须点击 **"Reinstall to Workspace"** 重新安装 App，新权限才会生效。
4. **🔴 2026 经典应用停用**
   - Classic Apps 将于 **2026年11月16日** 停用
   - 检查 [Slack App Dashboard](https://api.slack.com/apps) 确认你的 App 类型
   - 如果仍在使用旧版 Manifest，请重新创建并迁移配置

---

## 📚 相关参考
- [Slack 官方Scopes文档](https://docs.slack.dev/reference/scopes)
- [Slack 安全最佳实践](https://docs.slack.dev/security)
- [Slack AI 开发指南](https://docs.slack.dev/ai)
- [Slack Changelog 2026](https://docs.slack.dev/changelog)
- [Slack MCP Server](https://api.slack.com/mcp)
- [Slack UX 事件列表与渲染建议](./chatapps-architecture.md#6-事件类型映射-event-types)
- [Slack 区域化交互 (Zone) 架构架构详情](./chatapps-slack-architecture.md#3-交互分层架构-zone-architecture)
- [ChatApps 插件化流水线原理](./chatapps-architecture.md#3-消息处理流水线-message-processor-pipeline)
