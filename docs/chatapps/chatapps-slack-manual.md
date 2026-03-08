# 🚀 HotPlex Slack Bot Complete Manual

> 📅 Based on **Slack 2026 Official Standards** | Last Updated: 2026-03-04
>
> This manual covers 2026 new features: MCP Server, Real-time Search API, New Block Kit Components, AI Streaming Response, and more

---

## ⚡ App Manifest

This is the recommended installation method. No need to click dozens of buttons manually—just copy the complete code below for one-click configuration. This version includes basic chat capabilities, **App Home Dashboard**, **Interactive Approvals (HITL)**, and **AI Assistant Status Feedback**.

> ⚠️ **2026 Important Reminder**: Classic Apps will be deprecated on **November 16, 2026**. Please ensure you use the new App Manifest version.

1.  Visit [Slack API Console](https://api.slack.com/apps) -> **Create New App** -> **From an app manifest**.
2.  Select your Workspace, paste the following in the JSON tab:

```json
{
  "_metadata": {
    "major_version": 2,
    "minor_version": 1
  },
  "display_information": {
    "name": "HotPlex",
    "long_description": "HotPlex is a high-performance AI Agent control plane with advanced governance features. It provides long-lived process sessions, PGID process group isolation, and regex WAF security. Includes sandbox approval workflows, artifact mounting, global monitoring, and observability logs. Supports App Home dashboard and MCP Server integration. Perfect for Slack team organizations requiring controlled AI execution environments, enterprise AI automation workflows, and deep security audits.",
    "description": "HotPlex AI Assistant - High-performance CLI Automation",
    "background_color": "#1e293b"
  },
  "features": {
    "assistant_view": {
      "assistant_description": "HotPlex is a high-performance AI Agent Control Plane (Cli-as-a-Service) with advanced governance. Features include: long-lived sessions with PGID isolation, regex WAF security, and sandbox approval workflows.",
      "suggested_prompts": [
        {
          "title": "💡 Brainstorm",
          "message": "In brainstorming mode, analyze the current project architecture, identify three areas for improvement, and explain the value and implementation approach"
        },
        {
          "title": "📝 Create Issue",
          "message": "Create a GitHub Issue using the project's defined Issue template, describing an important bug or feature request in the project"
        },
        {
          "title": "🔀 Create PR",
          "message": "Create a pull request based on current code changes using the project's defined PR template"
        },
        {
          "title": "🔍 Code Review",
          "message": "Conduct a comprehensive code review of the current branch, including DRY principles, SOLID principles, clean architecture, code quality, security vulnerabilities, and performance optimization"
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
        "description": "Completely destroy current Session PGID and context",
        "should_escape": false
      },
      {
        "command": "/dc",
        "description": "When AI falls into an abnormal or unknown state, immediately terminate the current execution process",
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

### Key Capabilities with this Configuration:

1.  **Global Monitoring Center (`home_tab_enabled: true`)**: Allows developers to render a Dashboard with "Active Sessions", "Security Block Logs", and "MCP Status" when opening the Bot.
2.  **High-Risk Operation Interception & Approval**: When WAF intercepts high-risk operations, the bot will send an interactive card. Users must click **"Confirm Execution"** to proceed.
3.  **Rich Artifact Mounting (`files:read` / `files:write`)**: Supports automatic injection of error log attachments and direct generation of Agent patches.



---

## 🗝️ Step 1: Get Permission Keys (Tokens)

If you created the app via the Manifest above, copy the keys from these pages:

| Variable Name      | Recommended Format | Acquisition Path      | Description                                                                 |
| :----------------- | :----------------- | :-------------------- | :-------------------------------------------------------------------------- |
| **Bot Token**      | `xoxb-...`         | `OAuth & Permissions` | **APP Core Token**: Used for sending messages and updating UI.              |
| **App Token**      | `xapp-...`         | `Basic Information`   | **Socket Token**: Required for Socket Mode (includes `connections:write`).  |
| **Signing Secret** | String             | `Basic Information`   | **Security Verification**: Required for HTTP mode, must be > 32 characters. |

> 🔐 **2026 Security Best Practices**:
> - **No Hardcoding**: Never put Tokens in code repositories
> - **Environment Variables**: Use `.env` for development, Vault/Secrets Manager for production
> - **IP Whitelist**: Configure up to 10 CIDR ranges in OAuth & Permissions
> - **Least Privilege**: Only request scopes required for functionality

---

## 📡 Step 2: Runtime Mode Configuration

HotPlex supports two communication modes, switch via `HOTPLEX_SLACK_MODE` in `.env`:

### Mode A: Socket Mode (Recommended)
- **Principle**: Based on WebSocket, runs smoothly in intranet or local development environments without public IP.
- **Config**: `HOTPLEX_SLACK_MODE=socket`, `HOTPLEX_SLACK_APP_TOKEN=xapp-...`.

### Mode B: HTTP Mode (Production Webhook)
- **Principle**: Receives requests via callback URL, suitable for high-availability load-balanced environments.
- **Config**: `HOTPLEX_SLACK_MODE=http`, `HOTPLEX_SLACK_SIGNING_SECRET=...`.
- **URL**: Fill in `https://your-domain/webhook/slack/events` in Event Subscriptions.

> 💡 **2026 Recommendation**: Use **Socket Mode** for development/local, **HTTP Mode** + IP whitelist for production

---

## ⌨️ Step 3: Full Scenario Commands (Slash & Thread)

To solve Slack's native limitation of not supporting slash commands in **Threads**, HotPlex provides a dual-mode triggering solution:

| Scenario            | Trigger      | Description                                                                        |
| :------------------ | :----------- | :--------------------------------------------------------------------------------- |
| **Main Channel/DM** | `/reset`     | Type `/` for auto-complete, lowest barrier to entry.                               |
| **Thread/Sidebar**  | **`#reset`** | Due to Slack limitations, manually input `#` command, adapter will auto-intercept. |

> [!NOTE]
> `/dc` and `#dc` work the same way. Used to forcefully interrupt AI background workflows.
> Approval operations (Approve/Deny) are currently handled via interactive buttons on message cards, no manual command input required.


---

## ✨ Interaction Feedback: How to Understand the Bot

### 1. Reaction Semantics (Reactions)
The bot will inform you of progress through reactions on your messages:
- 📥 (`:inbox:`): **First Perception**. Request queued, preparing compute environment.
- 🧠 (`:brain:`): **Thought Perception**. Engine has taken over, logical reasoning in progress.
- ⚠️ (`:warning:`): **Risk Perception**. Triggered WAF interception or high-risk operation approval.
- ✅ (`:white_check_mark:`): **Finality Perception**. Task successfully finished.
- ❌ (`:x:`): **Failure Perception**. Internal error or execution timeout.

### 2. Message Zones
HotPlex adopts a zoned rendering architecture to ensure clear and orderly complex execution logic:
- **Status Perception Zone**: Instant descriptions based on `assistant_status` (e.g., "Thinking...", "Executing bash..."), making you feel the AI is "alive".
- **Thinking Zone**: Only preserves key Plan anchors (Context Block), avoiding long reasoning logs.
- **Action Zone**: Shows tool calls. Supports **Space Folding**, where extra-long output is auto-saved in Thread replies.
- **Display Zone**: AI's core response, supports typewriter streaming effect.

### 3. 2026 New Feature: AI Streaming Response
2026 introduces native AI streaming response support:

| API                 | Function         |
| ------------------- | ---------------- |
| `chat.startStream`  | Start streaming  |
| `chat.appendStream` | Append streaming |
| `chat.stopStream`   | Stop streaming   |

> 🤖 HotPlex supports typewriter effect through `chat.postMessage` + real-time updates for smooth streaming output.

### 4. 2026 New Feature: Assistant Status Feedback

In 2026, Slack allows AI apps to update instant status via the `assistant:write` permission. HotPlex deeply integrates this capability:

- **Instant Feedback**: The moment you send a message, the status bar at the bottom of the bot will show `Thinking...`.
- **Dynamic Perception**: When the AI starts scanning the entire repository or running time-consuming tools, the status automatically switches to `Analyzing codebase...` or `Executing bash...`, so you always feel the AI is "alive".
- **Low Noise**: Status updates do not create new messages, keeping the channel clean.

### 5. 2026 New Feature: MCP Server Integration
Slack released official MCP Server on February 17, 2026, supporting:
- AI agents real-time access to workspace data
- User-authorized data operations
- Secure context retrieval

> 📎 **Related Scope**: `assistant:write` (AI Assistant Core Permission)

> ⚠️ **Important**: Slack 2026 requires `assistant:write` to enable "Agents & AI Apps" feature:
> 1. Go to [Slack API Console](https://api.slack.com/apps) → Your App
> 2. Enable **"Agents & AI Apps"** (requires paid Slack)
> 3. Or configure `assistant_description` in App Manifest's `features.assistant_view`

---

## ✅ Advanced Configuration (slack.yaml)

Fine-grained control available in `chatapps/configs/slack.yaml`:

### 🔧 Core Parameters

| Parameter              | Optional Values   | Description                                                                                                |
| :--------------------- | :---------------- | :--------------------------------------------------------------------------------------------------------- |
| **`bot_user_id`**      | `U...`            | **Highly Recommended**. Used for precise Mention identification, avoid loops. Copy from Slack bot profile. |
| **`dm_policy`**        | `allow`/`pairing` | In `pairing` mode, only users who have @ mentioned the bot in channels can DM, ensuring privacy.           |
| **`group_policy`**     | `allow`/`mention` | In `mention` mode, bot only responds to explicitly @ mentioned messages, not channel chatter.              |
| **`allowed_users`**    | ID List           | User whitelist, only these IDs can use the bot (ID format like `U01234567`).                               |
| **`allowed_tools`**    | String Array      | Tool whitelist. If set, Agent can only use these tools (e.g., `["Bash", "Edit"]`).                         |
| **`disallowed_tools`** | String Array      | Tool blacklist. If set, Agent is prohibited from using these tools.                                        |

> [!TIP]
> **Tool Filter Priority**: `provider` level tool filter config (`provider.allowed_tools`) takes precedence over `engine` level config. If both are unset, all tools are allowed by default.

### 🧠 Customize AI Identity & Behavior (system_prompt)

> ⚠️ **Important**: The `system_prompt` in the config file is an **EXAMPLE TEMPLATE**. You MUST customize it for your project!

```yaml
# chatapps/configs/slack.yaml
system_prompt: |
  You are [Your Project Name], an expert software engineer...

  ## Environment
  - Describe your runtime constraints

  ## Slack Context
  - Describe your Slack usage scenario

  ## Git Workflow
  - Define your Git workflow (branch naming, commit conventions, etc.)

  ## Output
  - Define output format requirements
```

**Customization Points**:
| Section          | Description                                              |
| ---------------- | -------------------------------------------------------- |
| **Identity**     | Tell AI who it is and what project it's working on       |
| **Environment**  | Runtime constraints (headless mode, timeouts, etc.)      |
| **Git Workflow** | Your team's Git workflow conventions                     |
| **Output**       | Message format requirements (concise, code blocks, etc.) |

> 💡 **Best Practice**: Refer to the example in `chatapps/configs/slack.yaml` and modify the identity, workflow, and output specifications according to your project's actual needs.

---

## 🚑 Troubleshooting

1. **Bot has no ID?**
   - In Slack, click bot avatar → Profile → Click `...` next to icon → `Copy member ID`.
2. **"Dispatch failed"?**
   - Confirm `.env` `HOTPLEX_SLACK_MODE` matches enabled features in Slack console (e.g., enabled Socket Mode but configured `http` mode).
3. **Messages not updating or insufficient permissions?**
   - Check if Bot Token has expired.
   - **Important Reminder**: If you update Scopes in Slack console, you must click **"Reinstall to Workspace"** for new permissions to take effect.
4. **🔴 2026 Classic Apps Deprecation**
   - Classic Apps will be deprecated on **November 16, 2026**
   - Check [Slack App Dashboard](https://api.slack.com/apps) to confirm your App type
   - If still using old Manifest, please recreate and migrate configuration

---

## 📚 References
- [Slack Official Scopes Documentation](https://docs.slack.dev/reference/scopes)
- [Slack Security Best Practices](https://docs.slack.dev/security)
- [Slack AI Development Guide](https://docs.slack.dev/ai)
- [Slack Changelog 2026](https://docs.slack.dev/changelog)
- [Slack MCP Server](https://api.slack.com/mcp)
- [Slack UX Event Types and Rendering Suggestions](./chatapps-architecture.md#6-事件类型映射-event-types)
- [Slack Zone Architecture Details](./chatapps-slack-architecture.md#3-交互分层架构-zone-architecture)
- [ChatApps Plugin Pipeline Principles](./chatapps-architecture.md#3-消息处理流水线-message-processor-pipeline)
