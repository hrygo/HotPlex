# 5 分钟把 Claude Code 接入 Slack：打造团队 AI 编程助手

> 你是否想过：在 Slack 里直接调戏 Claude Code？

## 场景痛点

- ❌ 团队成员每人装一个 Claude Code？管理成本高
- ❌ 每次调用要等 3-5 秒冷启动？太慢了
- ❌ AI 执行的命令无法控制？生产环境不敢用

**解决方案：hotplex + Slack = 你的团队 AI 编程助手**

---

## 什么是 hotplex？

hotplex 是 AI CLI Agent 的**生产化执行引擎**，简单说就是：

> 把 Claude Code/OpenCode 从"终端工具"变成"可交互的持久服务"

核心能力：
- ⚡ **亚秒级响应** — 持久会话，零冷启动
- 🛡️ **生产级安全** — PGID 隔离 + 命令 WAF
- 💬 **多平台接入** — Slack/飞书/钉钉开箱即用

---

## 5 分钟极速接入

### 准备工作

1. 一个 Slack 工作区（管理员权限）
2. 一台运行 hotplex 的服务器/电脑

### 步骤 1：创建 Slack App

打开 [https://api.slack.com/apps](https://api.slack.com/apps)，点击 **"Create New App"** → 选择 **"From an app manifest"** → 选你的工作区。

复制下面的 JSON 配置（这是预置的 hotplex 配置）：

```json
{
  "display_information": {
    "name": "HotPlex",
    "description": "AI CLI Agent 执行引擎",
    "background_color": "#635BFF"
  },
  "features": {
    "bot_user": {
      "display_name": "HotPlex",
      "always_online": true
    },
    "socket_mode": {
      "enabled": true
    }
  },
  "oauth_config": {
    "scopes": {
      "bot": [
        "chat:write",
        "channels:history",
        "channels:read",
        "groups:history",
        "groups:read",
        "im:history",
        "im:read",
        "mpim:history",
        "mpim:read",
        "reactions:write",
        "users:read"
      ]
    }
  }
}
```

点击 **Create** → 完成！

### 步骤 2：开启 Socket Mode

左侧菜单找到 **Socket Mode** → 开启开关 → 填个名字（比如 `hotplex_socket`）→ **Generate**。

**复制生成的 App Token**（以 `xapp-` 开头）

### 步骤 3：安装到工作区

左侧菜单找到 **Install App** → **Install to Workspace** → **Allow**。

**复制 Bot Token**（以 `xoxb-` 开头）

### 步骤 4：配置 hotplex

在 hotplex 目录下创建/编辑 `.env` 文件：

```env
HOTPLEX_SLACK_MODE=socket
HOTPLEX_SLACK_BOT_TOKEN=xoxb-你复制的Bot-Token
HOTPLEX_SLACK_APP_TOKEN=xapp-你复制的App-Token
```

### 步骤 5：启动！

```bash
hotplexd --config chatapps/configs
```

### 步骤 6：在 Slack 开玩

1. 随便找个频道，拉机器人进群：`@HotPlex`
2. 输入 `/` 查看可用指令
3. 开聊：`Hi`，然后让它帮你写代码！

---

## 效果展示

```
🙋‍♂️: @HotPlex 帮我写个 Go 的 HTTP 服务器

🤖: 正在思考中...
// 以下是流式输出
package main

import (
    "fmt"
    "net/http"
)

func main() {
    http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
        fmt.Fprintf(w, "Hello, World!")
    })
    http.ListenAndServe(":8080", nil)
}
```

---

## 为什么选择 hotplex？

| 方案 | 冷启动 | 会话保持 | 安全控制 |
|------|--------|----------|----------|
| 直接用 Claude Code | 3-5 秒 | ❌ | ❌ |
| MCP | 有 | 部分 | 基础 |
| **hotplex** | **0 秒** | ✅ 持久 | ✅ PGID 隔离 + WAF |

---

## 下一步

- 想限制 AI 能执行哪些命令？→ 配置 `AllowedTools`
- 想锁定 AI 的工作目录？→ 配置 `WorkDir`
- 想接入钉钉/飞书？→ 同样的方式，一行配置

**GitHub 地址：** https://github.com/hrygo/hotplex

---

*有问题？欢迎提 Issue 或 PR！*
