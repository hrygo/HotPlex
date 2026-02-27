# 🚀 Slack 机器人全功能手册 (HotPlex)

本手册基于 **[Slack 2026 最新官方标准](https://api.slack.com/)** 编写，旨在引导你快速完成从零到一的集成。

---

## 🗝️ 第一步：获取权限密钥 (Tokens)

访问 [Slack API 控制台](https://api.slack.com/apps)，创建一个 **From scratch** 应用后，按需准备以下密钥：

| 变量名             | 格式       | 获取位置              | 作用                                                    |
| :----------------- | :--------- | :-------------------- | :------------------------------------------------------ |
| **Bot Token**      | `xoxb-...` | `OAuth & Permissions` | 核心令牌，用于发送消息和执行指令。                      |
| **App Token**      | `xapp-...` | `Basic Information`   | 启用 **Socket Mode** 必需，需勾选 `connections:write`。 |
| **Signing Secret** | 字符串     | `Basic Information`   | **HTTP Mode** 必需，用于验证 Slack 请求的合法性。       |

---

## 🛠️ 第二步：应用核心配置 (必读)

### 1. 权限范围 (Scopes)
在 `OAuth & Permissions` -> `Bot Token Scopes` 中勾选：
- `app_mentions:read` (检测 @ 机器人)
- `chat:write` (发送消息回复)
- `channels:history`, `groups:history`, `im:history` (读取消息上下文)
- `reactions:write` (给消息点赞/加状态表情)

### 2. 交互设置 (App Home)
必须在 `App Home` 页面：
- 开启 **Messages Tab**。
- 勾选 **Allow users to send Slash commands and messages from the messages tab**。
- *否则你无法在私聊中直接与机器人对话。*

### 3. 事件订阅 (Events)
在 `Event Subscriptions` 中开启 **Enable Events** 并订阅：
- `app_mention`
- `message.im`
- `message.channels`
- `message.groups`

---

## 📡 第三步：两种运行模式

### 模式 A：Socket Mode (推荐部署方式)
**最适合本地调试或企业内网环境。**
1.  在 `Socket Mode` 页面将其设为 **Enable**。
2.  生成 App Token (xapp) 时确保包含了 `connections:write` 权限。
3.  `.env` 配置：`SLACK_MODE=socket`, `SLACK_APP_TOKEN=xapp-...`。

### 模式 B：HTTP Mode (传统 Webhook)
**适合具备公网 IP/域名的生产环境。**
1.  在 `Event Subscriptions` -> `Request URL` 填写：`https://你的域名/webhook/slack/events`。
2.  确保 `slack.yaml` 中的 `verify_signature` 为 `true`。
3.  `.env` 配置：`SLACK_MODE=http`, `SLACK_SIGNING_SECRET=...`。

---

## ⌨️ 第四步：Slash Commands (快捷指令)

HotPlex 内置了两个强大的运维指令，请手动在 `Slash Commands` 页面添加：

| 指令     | 作用                                                         | Request URL (限 HTTP 模式)             |
| :------- | :----------------------------------------------------------- | :------------------------------------- |
| `/reset` | **彻底重置会话** (清空历史上下文，下次对话将冷启动)          | `https://你的域名/webhook/slack/slack` |
| `/dc`    | **断开连接并保留进度** (仅终止 CLI 进程，下次对话将自动恢复) | `https://你的域名/webhook/slack/slack` |

> [!NOTE]
> 在 **Socket Mode** 下，指令会自动通过 WebSocket 传输，但你仍需在控制面板中“声明”这两个指令。
> - **`/reset`**：当 AI 记忆混乱或需要开启新任务时使用。
> - **`/dc`**：用于强制杀死后台挂起的 CLI 进程，系统会保留当前进度供下次恢复。

---

## ✅ 配置清单 (slack.yaml)

所有的可选配置（如白名单、频率限制）均在 `chatapps/configs/slack.yaml` 中：

- **`bot_user_id`**: **强烈建议填写**。在 Slack 中点击 `查看机器人详情` -> `更多` -> `复制成员 ID` 获得。
- **`dm_policy`**: 设置为 `pairing` 可实现“邀请制”私聊（仅在频道中交互过的用户可私聊）。
- **`slash_command_rate_limit`**: 默认 10.0 次/秒，防止恶意刷指令。

---

## 🚑 常见故障排查

*   **无法发送指令？** 确认已重新安装：`OAuth & Permissions` -> `Reinstall to Workspace`。
*   **私聊没反应？** 确认 `App Home` 里的聊天框开关已打开。
*   **机器人不理我？** 邀请它进入频道：`/invite @机器人名字`。
*   **Token 错误？** 检查 `xoxb` 和 `xapp` 是否混淆，`xapp` 必须在 `Basic Information` 下生成。

---

## 🎯 支持的事件类型

HotPlex 完整支持 21 种 Engine 事件类型在 Slack 上的渲染：

### UX 优化事件 (即时反馈)
| 事件 | Emoji | 说明 |
|------|-------|------|
| `session_start` | :rocket: | 会话启动/冷启动 |
| `engine_starting` | :hourglass: | 引擎启动中 |
| `user_message_received` | :inbox: | 消息已收到 |
| `thinking` | :brain: | AI 推理中 |

### 核心交互事件
| 事件 | Emoji | Block 类型 | 说明 |
|------|-------|-----------|------|
| `answer` | - | section | AI 文本输出 |
| `tool_use` | :computer: | section | 工具调用开始 |
| `tool_result` | :white_check_mark: | section | 工具执行结果 |
| `error` | :warning: | section | 错误发生 |
| `result` | :white_check_mark: | section+context | Turn 完成 |

### 交互按钮事件
| 事件 | Emoji | Block 类型 | 按钮 |
|------|-------|-----------|------|
| `permission_request` | :warning: | header+actions | Allow/Deny |
| `exit_plan_mode` | :clipboard: | header+actions | Approve/Deny |
| `ask_user_question` | :question: | section+actions | Options |
| `danger_block` | :rotating_light: | section+actions | Confirm/Cancel |

### 命令事件
| 事件 | Emoji | 说明 |
|------|-------|------|
| `command_progress` | :gear: | 命令执行进度 |
| `command_complete` | :white_check_mark: | 命令执行完成 |

### 其他事件
| 事件 | Emoji | 说明 |
|------|-------|------|
| `system` | :gear: | 系统级消息 |
| `user` | :bust_in_silhouette: | 用户消息反射 |
| `step_start` | :arrow_right: | 步骤开始 (OpenCode) |
| `step_finish` | :white_check_mark: | 步骤完成 (OpenCode) |
| `plan_mode` | :mag_right: | 计划生成中 |
| `raw` | :page_facing_up: | 原始输出 |

> 💡 详细 UX 规范请参考 [engine-events-slack-ux-spec.md](./engine-events-slack-ux-spec.md)
