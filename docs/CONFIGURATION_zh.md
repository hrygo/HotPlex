# HotPlex 配置参考

> 完整的配置选项参考。
> 快速开始请参阅 [development_zh.md](development_zh.md)。
>
> **中文版** | **[English](configuration.md)**

## 目录

- [配置层级](#配置层级)
- [环境变量](#环境变量)
- [YAML 配置](#yaml-配置)
- [平台特定配置](#平台特定配置)
- [示例](#示例)

---

## 配置层级

HotPlex 使用分层配置系统，优先级从高到低：

```
1. 命令行参数     (--config, --port 等)
2. 环境变量       (HOTPLEX_*)
3. YAML 配置文件  (configs/chatapps/*.yaml)
4. 默认值         (内置默认值)
```

### 各层用途

| 层级          | 内容                                    |
| ------------- | --------------------------------------- |
| **`.env`**    | 全局参数、bot 凭证、密钥、持久化配置    |
| **YAML 文件** | 平台行为、权限策略、功能开关、AI 提示词 |

---

## 环境变量

### 核心服务器

| 变量                 | 默认值   | 描述                               |
| -------------------- | -------- | ---------------------------------- |
| `HOTPLEX_PORT`       | `8080`   | 服务器监听端口                     |
| `HOTPLEX_LOG_LEVEL`  | `INFO`   | 日志级别：DEBUG, INFO, WARN, ERROR |
| `HOTPLEX_LOG_FORMAT` | `json`   | 日志格式：json, text               |
| `HOTPLEX_API_KEY`    | *(必填)* | API 安全令牌                       |
| `HOTPLEX_API_KEYS`   | *(可选)* | 多个 API 密钥（逗号分隔）          |

### 引擎

| 变量                        | 默认值 | 描述                |
| --------------------------- | ------ | ------------------- |
| `HOTPLEX_EXECUTION_TIMEOUT` | `30m`  | AI 响应最大等待时间 |
| `HOTPLEX_IDLE_TIMEOUT`      | `1h`   | 会话空闲超时        |

### Provider

| 变量                                            | 默认值        | 描述                            |
| ----------------------------------------------- | ------------- | ------------------------------- |
| `HOTPLEX_PROVIDER_TYPE`                         | `claude-code` | Provider：claude-code, opencode |
| `HOTPLEX_PROVIDER_MODEL`                        | `sonnet`      | 默认模型：sonnet, haiku, opus   |
| `HOTPLEX_PROVIDER_BINARY`                       | *(自动检测)*  | CLI 二进制路径                  |
| `HOTPLEX_PROVIDER_DANGEROUSLY_SKIP_PERMISSIONS` | `false`       | 跳过所有权限检查                |
| `HOTPLEX_OPENCODE_COMPAT_ENABLED`               | `true`        | 启用 OpenCode HTTP API 兼容     |

### 项目目录 (Docker)

| 变量                   | 描述                          |
| ---------------------- | ----------------------------- |
| `HOTPLEX_PROJECTS_DIR` | 项目工作空间目录              |
| `HOTPLEX_GITCONFIG`    | git 配置路径（用于 bot 身份） |

### Native Brain（可选）

| 变量                              | 默认值        | 描述                                 |
| --------------------------------- | ------------- | ------------------------------------ |
| `HOTPLEX_BRAIN_API_KEY`           | *(未设置)*    | Brain API 密钥（设置后启用）         |
| `HOTPLEX_BRAIN_PROVIDER`          | `openai`      | Brain provider：openai, anthropic 等 |
| `HOTPLEX_BRAIN_MODEL`             | `gpt-4o-mini` | Brain 模型                           |
| `HOTPLEX_BRAIN_ENDPOINT`          | *(可选)*      | 自定义 API 端点                      |
| `HOTPLEX_BRAIN_TIMEOUT_S`         | `10`          | 请求超时（秒）                       |
| `HOTPLEX_BRAIN_CACHE_SIZE`        | `1000`        | 缓存大小                             |
| `HOTPLEX_BRAIN_MAX_RETRIES`       | `3`           | 最大重试次数                         |
| `HOTPLEX_BRAIN_RETRY_MIN_WAIT_MS` | `100`         | 最小重试等待                         |
| `HOTPLEX_BRAIN_RETRY_MAX_WAIT_MS` | `5000`        | 最大重试等待                         |

### 消息存储

| 变量                                       | 默认值                           | 描述                           |
| ------------------------------------------ | -------------------------------- | ------------------------------ |
| `HOTPLEX_MESSAGE_STORE_ENABLED`            | `true`                           | 启用消息持久化                 |
| `HOTPLEX_MESSAGE_STORE_TYPE`               | `sqlite`                         | 存储：sqlite, postgres, memory |
| `HOTPLEX_MESSAGE_STORE_SQLITE_PATH`        | `~/.config/hotplex/chatapp_messages.db` | SQLite 数据库路径              |
| `HOTPLEX_MESSAGE_STORE_SQLITE_MAX_SIZE_MB` | `1024`                           | 最大数据库大小                 |
| `HOTPLEX_MESSAGE_STORE_STREAMING_ENABLED`  | `true`                           | 启用流式存储                   |
| `HOTPLEX_MESSAGE_STORE_STREAMING_TIMEOUT`  | `5m`                             | 流式超时                       |

### CORS

| 变量                      | 描述                 |
| ------------------------- | -------------------- |
| `HOTPLEX_ALLOWED_ORIGINS` | 允许的源（逗号分隔） |

---

## 平台凭证

### Slack

| 变量                           | 必填        | 描述                      |
| ------------------------------ | ----------- | ------------------------- |
| `HOTPLEX_SLACK_PRIMARY_OWNER`  | **是**      | 主要所有者的 Slack 用户 ID |
| `HOTPLEX_SLACK_BOT_USER_ID`    | **是**      | Bot 用户 ID (UXXXXXXXXXX)   |
| `HOTPLEX_SLACK_BOT_TOKEN`      | **是**      | Bot Token (xoxb-...)        |
| `HOTPLEX_SLACK_APP_TOKEN`      | Socket Mode | App Token (xapp-...)        |
| `HOTPLEX_SLACK_SIGNING_SECRET` | HTTP Mode   | 签名验证密钥                |

### 飞书

| 变量                                | 描述       |
| ----------------------------------- | ---------- |
| `HOTPLEX_FEISHU_APP_ID`             | App ID     |
| `HOTPLEX_FEISHU_APP_SECRET`         | App secret |
| `HOTPLEX_FEISHU_VERIFICATION_TOKEN` | 验证 token |
| `HOTPLEX_FEISHU_ENCRYPT_KEY`        | 加密 key   |

---

## YAML 配置

### 结构

```yaml
# configs/chatapps/slack.yaml

# [必填] 平台标识
platform: slack

# Provider 设置
provider:
  type: claude-code
  enabled: true
  default_model: sonnet
  default_permission_mode: bypass-permissions

# 引擎设置
engine:
  work_dir: ~/projects/myproject
  timeout: 30m
  idle_timeout: 1h

# 会话生命周期
session:
  timeout: 1h
  cleanup_interval: 5m

# 连接模式
mode: socket  # 或 "http"
server_addr: :8080

# AI 行为
system_prompt: |
  你是一个有帮助的助手...

# 功能开关
features:
  chunking:
    enabled: true
    max_chars: 4000
  threading:
    enabled: true

# 安全
security:
  verify_signature: true
  permission:
    dm_policy: allow
    group_policy: mention
    bot_user_id: ${HOTPLEX_SLACK_BOT_USER_ID}
```

### Provider 部分

| 字段                           | 描述                                                                        |
| ------------------------------ | --------------------------------------------------------------------------- |
| `type`                         | Provider 类型：`claude-code`, `opencode`                                    |
| `enabled`                      | 启用/禁用 provider                                                          |
| `default_model`                | 默认模型 ID                                                                 |
| `default_permission_mode`      | 权限模式：`bypass-permissions`, `acceptEdits`, `default`, `dontAsk`, `plan` |
| `dangerously_skip_permissions` | 跳过所有权限检查（Docker/CI）                                               |
| `binary_path`                  | 自定义二进制路径                                                            |
| `allowed_tools`                | 工具白名单                                                                  |
| `disallowed_tools`             | 工具黑名单                                                                  |

### Engine 部分

| 字段               | 描述             |
| ------------------ | ---------------- |
| `work_dir`         | Agent 工作目录   |
| `timeout`          | 最大执行时间     |
| `idle_timeout`     | 会话空闲超时     |
| `allowed_tools`    | 引擎级工具白名单 |
| `disallowed_tools` | 引擎级工具黑名单 |

### Features 部分

| 功能                       | 描述                          |
| -------------------------- | ----------------------------- |
| `chunking.enabled`         | 分割长消息                    |
| `chunking.max_chars`       | 每块最大字符数（Slack: 4000） |
| `threading.enabled`        | 在线程中回复                  |
| `rate_limit.enabled`       | 启用速率限制处理              |
| `rate_limit.max_attempts`  | 最大重试次数                  |
| `rate_limit.base_delay_ms` | 初始重试延迟                  |
| `rate_limit.max_delay_ms`  | 最大重试延迟                  |
| `markdown.enabled`         | 转换 Markdown 为平台格式      |

### Security 部分

| 字段                                  | 描述                                              |
| ------------------------------------- | ------------------------------------------------- |
| `verify_signature`                    | 验证平台签名（HTTP 模式）                         |
| `permission.dm_policy`                | 私聊策略：`allow`, `pairing`, `block`             |
| `permission.group_policy`             | 群聊策略：`allow`, `mention`, `multibot`, `block` |
| `permission.bot_user_id`              | Bot 用户 ID（必填）                               |
| `permission.allowed_users`            | 用户白名单                                        |
| `permission.blocked_users`            | 用户黑名单                                        |
| `permission.slash_command_rate_limit` | 每用户速率限制                                    |

---

## 平台特定配置

### Slack（Socket 模式）

```yaml
platform: slack
mode: socket  # 开发环境推荐

provider:
  type: claude-code

security:
  permission:
    bot_user_id: ${HOTPLEX_SLACK_BOT_USER_ID}
    group_policy: multibot

features:
  chunking:
    enabled: true
    max_chars: 4000
  threading:
    enabled: true
```

### Slack（HTTP/Webhook 模式）

```yaml
platform: slack
mode: http
server_addr: :8080

security:
  verify_signature: true
```

---

## 示例

### 最小 Slack 配置

```yaml
platform: slack
mode: socket

provider:
  type: claude-code

engine:
  work_dir: ~/projects/myproject

security:
  permission:
    bot_user_id: ${HOTPLEX_SLACK_BOT_USER_ID}
```

### 多 Bot 配置

```yaml
platform: slack
mode: socket

security:
  permission:
    bot_user_id: ${HOTPLEX_SLACK_BOT_USER_ID}
    group_policy: multibot  # 多 bot 关键设置
    broadcast_response: |
      请 @mention 我来获取帮助。
```

### Docker 生产配置

```yaml
platform: slack

provider:
  type: claude-code
  dangerously_skip_permissions: true  # 容器化环境

engine:
  work_dir: /app/workspace
  timeout: 30m
  idle_timeout: 2h

features:
  chunking:
    enabled: true
  rate_limit:
    enabled: true
    max_attempts: 5
```

---

## 相关文档

- [development_zh.md](development_zh.md) - 开发指南
- [architecture_zh.md](architecture_zh.md) - 架构概览
- [docker-deployment_zh.md](docker-deployment_zh.md) - Docker 部署
- [chatapps/slack-setup-beginner_zh.md](chatapps/slack-setup-beginner_zh.md) - Slack 设置指南
