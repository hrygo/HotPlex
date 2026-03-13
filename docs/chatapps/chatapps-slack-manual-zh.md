# 🚀 HotPlex Slack 机器人全功能手册

> 📅 基于 **Slack 2026 最新官方标准** 编写 | 最后更新: 2026-03-04
>
> 本手册涵盖 2026 年新特性：MCP Server、实时搜索 API、新 Block Kit 组件、AI 流式响应等

---

## ⚡ 应用配置清单 (App Manifest)

这是最推荐的安装方式。你无需手动点击几十个按钮，只需复制以下完整版代码即可一键配置。此版本不仅包含基础聊天能力，还开启了**App Home 面板**、**交互式审批 (HITL)** 以及 **AI 助手状态感知**。

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
    "long_description": "HotPlex 是一个高性能 AI Agent 控制平面，提供长生命周期进程会话、PGID 进程组隔离和正则表达式 WAF 安全保护。它包含沙盒审批工作流、产物挂载、全局监控、可观测性日志；支持 App Home 主页控制台和 MCP Server 集成。非常适合需要受控 AI 执行环境、企业级 AI 自动化工作流和深度安全审计的 Slack 团队组织。",
    "description": "HotPlex AI 助手 - 高性能 CLI 自动化助手",
    "background_color": "#1e293b"
  },
  "features": {
    "assistant_view": {
      "assistant_description": "HotPlex 是一个高性能 AI Agent 控制平面（Cli-as-a-Service）。它将 Claude Code 和 OpenCode 桥接到交互式 Slack 服务，提供长生命周期会话、PGID 隔离和正则表达式 WAF 安全保护。非常适合想要 AI 驱动开发且完全掌控的团队。",
      "suggested_prompts": [
        {
          "title": "💡 创意激发",
          "message": "以头脑风暴模式，帮我分析当前项目架构，识别三个可以改进的方向，说明改进价值和实现思路"
        },
        {
          "title": "📝 创建 Issue",
          "message": "创建一个 GitHub Issue，必须使用项目定义的 Issue 模板，描述项目中一个重要的 bug 或功能需求"
        },
        {
          "title": "🔀 创建 PR",
          "message": "基于当前代码改动创建 pull request，必须使用项目定义的 PR 模板"
        },
        {
          "title": "🔍 代码审查",
          "message": "对当前分支进行全面的代码审查，包括 DRY 原则、SOLID 原则、整洁架构、代码质量、安全漏洞和性能优化"
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
        "description": "当 AI 陷入异常或不可知状态时，立即终止当前执行进程",
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

### 此配置带来的核心能力：

1.  **全局监控中心 (`home_tab_enabled: true`)**：允许开发者在打开 Bot 时，渲染出包含"活跃会话数"、"安全拦截日志"和" MCP 挂载状态"的 Dashboard。
2.  **高危操作拦截与审批**：当 WAF 拦截到高危操作时，机器人将发送一张交互式卡片。用户必须点击 **"确认执行"** 按钮，任务才会继续执行。
3.  **富产物挂载 (`files:read` / `files:write`)**：支持日志附件的自动注入与 Agent 补丁包的直接生成推送。
4.  **高可用通讯架构**：支持 Socket Mode 与 HTTP Mode 动态切换，确保不同网络环境下的稳定连接。



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

HotPlex 支持两种通信模式，请在 `.env` 中通过 `HOTPLEX_SLACK_MODE` 切换：

### 模式 A：Socket Mode (推荐)
- **原理**：基于 WebSocket，无需公网 IP 也能在内网甚至本地开发环境流畅运行。
- **配置**：`HOTPLEX_SLACK_MODE=socket`, `HOTPLEX_SLACK_APP_TOKEN=xapp-...`。

### 模式 B：HTTP Mode (生产 Webhook)
- **原理**：通过回调 URL 接收请求，适合高可用负载均衡环境。
- **配置**：`HOTPLEX_SLACK_MODE=http`, `HOTPLEX_SLACK_SIGNING_SECRET=...`。
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
> 审批操作（Approve/Deny）目前通过消息卡片上的交互式按钮完成，无需手动输入命令。


---

## ✨ 交互反馈：如何读懂机器人

### 1. 表情语义 (Reactions)
机器人会通过点按你消息下的表情来告知进展：
- 📥 (`:inbox:`)：**第一感知**。请求已入队，环境准备中。
- 🧠 (`:brain:`)：**思维感知**。Engine 已接管，正在进行逻辑推演。
- ⚠️ (`:warning:`)：**风险感知**。触发 WAF 拦截或高危操作审批。
- ✅ (`:white_check_mark:`)：**终态感知**。任务成功完成。
- ❌ (`:x:`)：**故障感知**。内部错误或执行超时。

### 2. 消息分区 (Zones)
HotPlex 采用区域化渲染架构，确保复杂执行逻辑的清晰有序：
- **状态感知区**：基于 `assistant_status` 的即时描述（如 "Thinking...", "Executing bash..."），让你感知 AI “活着”。
- **思考区**：仅保留关键的 Plan 锚点（Context Block），避免冗长的推理日志干扰。
- **行动区**：展示工具调用。支持 **Space Folding (空间折叠)**，超长输出会自动存入 Thread 回复。
- **展示区**：AI 的核心回答，支持打字机效果流式输出。

### 3. 2026 新特性：AI 流式响应 (Streaming)
2026 年 Slack 引入了原生 AI 流式响应支持：

| API                 | 功能         |
| ------------------- | ------------ |
| `chat.startStream`  | 启动流式响应 |
| `chat.appendStream` | 追加流式内容 |
| `chat.stopStream`   | 结束流式响应 |

> 🤖 HotPlex 已支持打字机效果，通过 `chat.postMessage` + 实时更新实现平滑流式输出。

### 4. 2026 新特性：助手状态反馈 (Assistant Status)

2026 年，Slack 允许 AI 应用通过 `assistant:write` 权限更新即时状态。HotPlex 深入集成了该能力：

- **秒级反馈**：在你发送消息的瞬间，机器人底部的状态栏将显示 `Thinking...`。
- **动态感知**：当 AI 开始扫描全库或运行耗时工具时，状态会自动切换为 `Analyzing codebase...` 或 `Executing bash...`，让你时刻感受到 AI “活着”。
- **低噪音**：状态更新不会产生新消息，保持频道整洁。

### 5. 2026 新特性：MCP Server 集成
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

在代码库的 `configs/chatapps/slack.yaml` 中可进行细粒度控制：

### 🔧 核心参数

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

### 🧠 自定义 AI 身份与行为 (system_prompt)

> ⚠️ **重要**：配置文件中的 `system_prompt` 是 **示例模板**，你需要根据自己的项目需求进行定制！

```yaml
# configs/chatapps/slack.yaml
system_prompt: |
  You are [你的项目名称], an expert software engineer...

  ## Environment
  - 描述你的运行环境约束

  ## Slack Context
  - 描述你的 Slack 使用场景

  ## Git Workflow
  - 定义你的 Git 工作流程（分支命名、commit 规范等）

  ## Output
  - 定义输出格式要求
```

**定制要点**：
| 部分             | 说明                             |
| ---------------- | -------------------------------- |
| **身份定义**     | 告诉 AI 它是谁，负责什么项目     |
| **Environment**  | 运行环境约束（无头模式、超时等） |
| **Git Workflow** | 你的团队 Git 工作流程规范        |
| **Output**       | 消息格式要求（简洁、代码块等）   |

> 💡 **最佳实践**：参考 `configs/chatapps/slack.yaml` 中的示例，根据你的项目实际情况修改身份、工作流程和输出规范。

### 📝 完整高级配置示例 (slack.yaml)

以下是完整的 `slack.yaml` 配置文件示例，包含所有可用的配置选项。进阶用户可以参考此模板进行细粒度定制：

```yaml
# =============================================================================
# HotPlex Slack Adapter Configuration
# =============================================================================
# This file defines the behavior, security, and integration settings for the
# Slack platform adapter.
#
# Detailed Setup Guide: docs/chatapps/chatapps-slack.md
# =============================================================================

# -----------------------------------------------------------------------------
# 1. PLATFORM & CONNECTION [Essential]
# -----------------------------------------------------------------------------

# [Required] Platform identifier
platform: slack

# [Optional] Connection mode
# - "socket": (Default/Recommended) Standard Socket Mode for local/firewalled envs.
# - "http"  : HTTP Webhook mode for cloud-native production deployments.
mode: socket

# [Optional] HTTP Server address
# Only used when mode is "http" or for health check endpoints.
server_addr: :8080

# -----------------------------------------------------------------------------
# 2. AI IDENTITY & BEHAVIOR
# -----------------------------------------------------------------------------

# ⚠️ [ACTION REQUIRED]
# [Recommended] The Core System Identity
# Customize this prompt to define your AI's specialized engineer persona.
# This defines the AI's skills, workflow, and safety rules.
system_prompt: |
  You are HotPlex, an expert software engineer in a Slack conversation.

  ## Environment
  - Running under HotPlex engine (stdin/stdout)
  - Headless mode - cannot prompt for user input

  ## Slack Context
  - Replies go to thread automatically
  - Keep answers concise - user expects quick responses

  ## Git Workflow (Fork + Feature Branch)

  ### Repository Structure
  ```
  upstream (hrygo/hotplex)     ← Source of truth
      │
      └── origin (your fork)   ← Your remote
              │
              └── local        ← Your machine
  ```

  ### Before Starting New Work
  1. **Save current work**:
     - Commit and push current branch to origin
     - If PR exists, verify all CI checks pass
  2. **Sync main branches** (main is SYNC-ONLY, no development):
     ```bash
     git checkout main
     git fetch upstream
     git reset --hard upstream/main    # Force sync with upstream
     git push origin main --force      # Update fork's main
     ```

  ### Feature Development Flow
  1. **Create Issue** (if not exists):
     ```bash
     gh issue create -t "[type] description" -b "body"
     ```
     Types: `feat`, `fix`, `refactor`, `docs`, `test`, `chore`

  2. **Create Feature Branch**:
     ```bash
     git checkout -b <type>/<issue-id>-short-desc
     # Example: feat/123-add-user-auth
     ```

  3. **Commit (Atomic & Frequent)**:
     ```bash
     git commit -m "<type>(scope): description (Refs #ID)"
     # Example: feat(auth): add OAuth login (Refs #123)
     ```
     - One commit per independent logic unit
     - Use `wip:` prefix for checkpoints

  4. **Create Pull Request**:
     ```bash
     git push origin <branch>
     gh pr create --fill
     ```
     - Body must include: `Resolves #ID` or `Refs #ID`
     - PR targets `upstream/main`, NOT your origin/main

  ### Safety Rules
  - **FORBIDDEN**: `checkout .`, `reset --hard`, `clean -fd` (lose uncommitted work)
  - **REQUIRED**: `git status` before branch switching
  - **SYNC-ONLY**: main branch - no commits, no development
  - **PROTECTED**: upstream/main is the target - PR only

  ## Output
  - Be concise - short messages preferred
  - Use bullet lists over paragraphs
  - Use code blocks for code snippets
  - Avoid tables - use lists instead

# [Optional] Specialized Task Instructions
# These are appended to every interaction to maintain task quality.
task_instructions: |
  1. Understand before acting
  2. Avoid operations requiring user input
  3. Summarize tool output - don't dump raw data
  4. Write detailed content to docs/ directory

# -----------------------------------------------------------------------------
# 3. AI PROVIDER & ENGINE SETTINGS
# -----------------------------------------------------------------------------

# AI Backend Configuration
provider:
  # AI backend type:
  # - claude-code: Anthropic's Claude Code CLI (Default)
  # - opencode: OpenCode (supports multiple LLM backends via its own config)
  type: claude-code

  # Global settings (applied to all providers)
  enabled: true
  default_model: sonnet            # Model choice (e.g. sonnet, haiku, opus)
  
  # Permission Strategy
  # - bypassPermissions: Fully autonomous (Recommended for Docker/Sandbox)
  # - acceptEdits: Prompt for edits
  # - default: Default CLI behavior
  default_permission_mode: bypass-permissions 
  dangerously_skip_permissions: true

  # Tool filtering (Provider-level override)
  # allowed_tools: ["Bash", "Edit"]

# Engine Execution Parameters
engine:
  # ⚠️ [ACTION REQUIRED]
  # Your local working directory where the AI will perform tasks.
  # Ensure this path exists on your machine.
  work_dir: ~/projects/hotplex
  
  # Performance & Safety
  timeout: 30m                     # Max time for single AI task
  idle_timeout: 1h                 # Time before agent teardown

  # Tool filtering (Engine-level whitelist/blacklist)
  # allowed_tools: ["Bash", "Edit"]
  # disallowed_tools: ["Bash"]

# -----------------------------------------------------------------------------
# 4. SECURITY & ACCESS CONTROL
# -----------------------------------------------------------------------------

security:
  # Verify Slack request signatures (mandatory for HTTP mode)
  verify_signature: true

  # [Optional] Bot Ownership & Access Policy
  # See Part 1.0 of docs/design/bot-behavior-spec.md for details.
  owner:
    # ⚠️ [ACTION REQUIRED]
    # Your Slack User ID (e.g., U12345678).
    # To find it: Profile -> More (...) -> Copy member ID.
    primary: "U0AHCF4DPK2"

    # [Optional] List of trusted User IDs who can also command the bot
    trusted: []

    # Access Control Policy:
    # - "owner_only": Only the 'primary' owner can interact with the bot.
    # - "trusted"   : Both 'primary' owner and 'trusted' users can interact.
    # - "public"    : Anyone in the workspace can interact with the bot.
    policy: trusted

  # User & Channel Permissions
  permission:
    # DM Policy: How the bot behaves in Direct Messages
    # - "allow"  : Respond to all DMs
    # - "pairing": Only respond if explicitly paired
    # - "block"  : Total DM blackout
    dm_policy: allow

    # Group Policy: How the bot behaves in Channels/Groups
    # - "allow"   : Passive mode. Respond to ALL messages (potential noise).
    # - "mention" : Solo mode. Only responds when @this_bot is mentioned.
    #               Ignores messages intended for other bots.
    # - "multibot": (Recommended) Team mode. Intelligent multi-bot coordination.
    #               - Responds if @this_bot is mentioned.
    #               - SILENT and ignores messages mentioning OTHER bots (avoids cross-talk).
    #               - Triggers broadcast_response if NO bot is tagged.
    # - "block"   : Blackout mode. Completely silent in group channels.
    group_policy: multibot

    # ⚠️ [ACTION REQUIRED]
    # Bot's own Slack User ID (e.g., U12345678).
    # Essential for @mention detection. Recommend using environment variable.
    bot_user_id: ${HOTPLEX_SLACK_BOT_USER_ID}

    # [Optional] Thread Ownership Tracking
    # Advanced: Recommended for multi-bot rooms to prevent conflicting responses.
    thread_ownership:
      enabled: true               # Set to true to enable thread-level state
      ttl: 24h                    # Ownership expiration (default: 24h)
      persist: true               # Keep state across restarts

    # [Optional] Multi-bot Broadcast Message
    # Response sent when group_policy is "multibot" but no bot is tagged.
    # Set to "" to stay silent when no bot is tagged (Multi-bot silence).
    broadcast_response: ""


    # User Filtering (Whitelist/Blacklist)
    # Applied BEFORE Owner Policy checks.
    allowed_users: []      # Example: ["U12345", "U67890"]
    blocked_users: []

    # API Security: Rate Limiting (reqs/sec per user)
    slash_command_rate_limit: 10.0

# -----------------------------------------------------------------------------
# 5. FEATURE TOGGLES
# -----------------------------------------------------------------------------

features:
  # UI/UX Experience settings
  chunking:
    enabled: true                  # Split messages > 4000 chars
    max_chars: 4000
  
  threading:
    enabled: true                  # Always reply in threads

  rate_limit:
    enabled: true                  # Auto-retry on Slack API 429
    max_attempts: 3
    base_delay_ms: 500
    max_delay_ms: 5000

  markdown:
    enabled: true                  # Standard MD to Slack mrkdwn conversion

# -----------------------------------------------------------------------------
# 6. SESSION & STORAGE
# -----------------------------------------------------------------------------

# Internal Session Lifecycle [Optional]
session:
  timeout: 1h                      # Inactivity before cleanup
  cleanup_interval: 5m              # Periodic scan interval

# Message Storage (Persistent History) [Optional]
# Enables conversation retrieval and long-term memory.
message_store:
  enabled: true
  type: sqlite                    # sqlite | postgres | memory
  
  # Database configuration
  sqlite:
    path: ~/.config/hotplex/slack_messages.db
    max_size_mb: 512
    
  # postgres:
  #   dsn: postgres://user:pass@localhost:5432/hotplex
  #   max_connections: 10

  # History management
  strategy: default                # default | verbose | minimal
  streaming:
    enabled: true                  # Buffer streaming chunks
    timeout: 5m                    # Wait time for stream completion
    storage_policy: complete_only  # complete_only | all_chunks
```


---

## 🚑 常见故障排查

1. **机器人没有 ID？**
   - 进入 Slack，点击机器人头像查看 Profile，点击图标旁边的 `...` -> `Copy member ID`。
2. **"Dispatch failed"?**
   - 确认 `.env` 中的 `HOTPLEX_SLACK_MODE` 与你在 Slack 后台启用的功能匹配（例如开启了 Socket Mode 但配了 `http` 模式）。
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
