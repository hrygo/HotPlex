# HotPlex Configuration Reference

> Complete reference for all configuration options.
> For quick start, see [development.md](development.md).
>
> **[中文版](configuration_zh.md)** | **English**

## Table of Contents

- [Configuration Layers](#configuration-layers)
- [Environment Variables](#environment-variables)
- [YAML Configuration](#yaml-configuration)
- [Platform-Specific Configs](#platform-specific-configs)
- [Examples](#examples)

---

## Configuration Layers

HotPlex uses a layered configuration system with the following priority (highest to lowest):

```
1. Command-line flags     (--config, --port, etc.)
2. Environment variables  (HOTPLEX_*)
3. YAML config files      (configs/chatapps/*.yaml)
4. Default values         (built-in defaults)
```

### Layer Purposes

| Layer          | Contents                                                        |
| -------------- | --------------------------------------------------------------- |
| **`.env`**     | Global parameters, bot credentials, secrets, persistence config |
| **YAML files** | Platform behavior, permissions, features, AI prompts            |

---

## Environment Variables

### Core Server

| Variable             | Default      | Description                         |
| -------------------- | ------------ | ----------------------------------- |
| `HOTPLEX_PORT`       | `8080`       | Server listen port                  |
| `HOTPLEX_LOG_LEVEL`  | `INFO`       | Log level: DEBUG, INFO, WARN, ERROR |
| `HOTPLEX_LOG_FORMAT` | `json`       | Log format: json, text              |
| `HOTPLEX_API_KEY`    | *(required)* | API security token                  |
| `HOTPLEX_API_KEYS`   | *(optional)* | Multiple API keys (comma-separated) |

### Engine

| Variable                    | Default | Description                |
| --------------------------- | ------- | -------------------------- |
| `HOTPLEX_EXECUTION_TIMEOUT` | `30m`   | Max wait for AI response   |
| `HOTPLEX_IDLE_TIMEOUT`      | `1h`    | Session inactivity timeout |

### Provider

| Variable                                        | Default         | Description                            |
| ----------------------------------------------- | --------------- | -------------------------------------- |
| `HOTPLEX_PROVIDER_TYPE`                         | `claude-code`   | Provider: claude-code, opencode        |
| `HOTPLEX_PROVIDER_MODEL`                        | `sonnet`        | Default model: sonnet, haiku, opus     |
| `HOTPLEX_PROVIDER_BINARY`                       | *(auto-detect)* | Path to CLI binary                     |
| `HOTPLEX_PROVIDER_DANGEROUSLY_SKIP_PERMISSIONS` | `false`         | Skip all permission checks             |
| `HOTPLEX_OPENCODE_COMPAT_ENABLED`               | `true`          | Enable OpenCode HTTP API compatibility |

### Projects (Docker)

| Variable               | Description                         |
| ---------------------- | ----------------------------------- |
| `HOTPLEX_PROJECTS_DIR` | Project workspace directory         |
| `HOTPLEX_GITCONFIG`    | Path to git config for bot identity |

### Native Brain (Optional)

| Variable                          | Default       | Description                             |
| --------------------------------- | ------------- | --------------------------------------- |
| `HOTPLEX_BRAIN_API_KEY`           | *(unset)*     | Brain API key (enables brain when set)  |
| `HOTPLEX_BRAIN_PROVIDER`          | `openai`      | Brain provider: openai, anthropic, etc. |
| `HOTPLEX_BRAIN_MODEL`             | `gpt-4o-mini` | Brain model                             |
| `HOTPLEX_BRAIN_ENDPOINT`          | *(optional)*  | Custom API endpoint                     |
| `HOTPLEX_BRAIN_TIMEOUT_S`         | `10`          | Request timeout in seconds              |
| `HOTPLEX_BRAIN_CACHE_SIZE`        | `1000`        | Cache size                              |
| `HOTPLEX_BRAIN_MAX_RETRIES`       | `3`           | Max retry attempts                      |
| `HOTPLEX_BRAIN_RETRY_MIN_WAIT_MS` | `100`         | Min retry wait                          |
| `HOTPLEX_BRAIN_RETRY_MAX_WAIT_MS` | `5000`        | Max retry wait                          |

### Message Store

| Variable                                   | Default                          | Description                       |
| ------------------------------------------ | -------------------------------- | --------------------------------- |
| `HOTPLEX_MESSAGE_STORE_ENABLED`            | `true`                           | Enable message persistence        |
| `HOTPLEX_MESSAGE_STORE_TYPE`               | `sqlite`                         | Storage: sqlite, postgres, memory |
| `HOTPLEX_MESSAGE_STORE_SQLITE_PATH`        | `~/.config/hotplex/chatapp_messages.db` | SQLite database path              |
| `HOTPLEX_MESSAGE_STORE_SQLITE_MAX_SIZE_MB` | `1024`                           | Max database size                 |
| `HOTPLEX_MESSAGE_STORE_STREAMING_ENABLED`  | `true`                           | Enable streaming storage          |
| `HOTPLEX_MESSAGE_STORE_STREAMING_TIMEOUT`  | `5m`                             | Streaming timeout                 |

### CORS

| Variable                  | Description                       |
| ------------------------- | --------------------------------- |
| `HOTPLEX_ALLOWED_ORIGINS` | Allowed origins (comma-separated) |

---

## Platform Credentials

### Slack

| Variable                       | Required    | Description                     |
| ------------------------------ | ----------- | ------------------------------- |
| `HOTPLEX_SLACK_PRIMARY_OWNER` | **Yes**     | Slack User ID of the primary owner |
| `HOTPLEX_SLACK_BOT_USER_ID`    | **Yes**     | Bot User ID (UXXXXXXXXXX)       |
| `HOTPLEX_SLACK_BOT_TOKEN`      | **Yes**     | Bot Token (xoxb-...)            |
| `HOTPLEX_SLACK_APP_TOKEN`      | Socket Mode | App Token (xapp-...)            |
| `HOTPLEX_SLACK_SIGNING_SECRET` | HTTP Mode   | Signing secret for verification |

### Feishu

| Variable                            | Description        |
| ----------------------------------- | ------------------ |
| `HOTPLEX_FEISHU_APP_ID`             | App ID             |
| `HOTPLEX_FEISHU_APP_SECRET`         | App secret         |
| `HOTPLEX_FEISHU_VERIFICATION_TOKEN` | Verification token |
| `HOTPLEX_FEISHU_ENCRYPT_KEY`        | Encryption key     |

---

## YAML Configuration

### Structure

```yaml
# configs/chatapps/slack.yaml

# [Required] Platform identifier
platform: slack

# Provider settings
provider:
  type: claude-code
  enabled: true
  default_model: sonnet
  default_permission_mode: bypass-permissions

# Engine settings
engine:
  work_dir: ~/projects/myproject
  timeout: 30m
  idle_timeout: 1h

# Session lifecycle
session:
  timeout: 1h
  cleanup_interval: 5m

# Connection mode
mode: socket  # or "http"
server_addr: :8080

# AI behavior
system_prompt: |
  You are a helpful assistant...

# Feature toggles
features:
  chunking:
    enabled: true
    max_chars: 4000
  threading:
    enabled: true

# Security
security:
  verify_signature: true
  permission:
    dm_policy: allow
    group_policy: mention
    bot_user_id: ${HOTPLEX_SLACK_BOT_USER_ID}
```

### Provider Section

| Field                          | Description                                                                        |
| ------------------------------ | ---------------------------------------------------------------------------------- |
| `type`                         | Provider type: `claude-code`, `opencode`                                           |
| `enabled`                      | Enable/disable provider                                                            |
| `default_model`                | Default model ID                                                                   |
| `default_permission_mode`      | Permission mode: `bypass-permissions`, `acceptEdits`, `default`, `dontAsk`, `plan` |
| `dangerously_skip_permissions` | Skip all permission checks (Docker/CI)                                             |
| `binary_path`                  | Custom binary path                                                                 |
| `allowed_tools`                | Tool whitelist                                                                     |
| `disallowed_tools`             | Tool blacklist                                                                     |

### Engine Section

| Field              | Description                 |
| ------------------ | --------------------------- |
| `work_dir`         | Agent's working directory   |
| `timeout`          | Max execution time          |
| `idle_timeout`     | Session idle timeout        |
| `allowed_tools`    | Engine-level tool whitelist |
| `disallowed_tools` | Engine-level tool blacklist |

### Features Section

| Feature                    | Description                         |
| -------------------------- | ----------------------------------- |
| `chunking.enabled`         | Split long messages                 |
| `chunking.max_chars`       | Max chars per chunk (Slack: 4000)   |
| `threading.enabled`        | Reply in threads                    |
| `rate_limit.enabled`       | Enable rate limit handling          |
| `rate_limit.max_attempts`  | Max retry attempts                  |
| `rate_limit.base_delay_ms` | Initial retry delay                 |
| `rate_limit.max_delay_ms`  | Max retry delay                     |
| `markdown.enabled`         | Convert Markdown to platform format |

### Security Section

| Field                                 | Description                                           |
| ------------------------------------- | ----------------------------------------------------- |
| `verify_signature`                    | Verify platform signatures (HTTP mode)                |
| `permission.dm_policy`                | DM policy: `allow`, `pairing`, `block`                |
| `permission.group_policy`             | Group policy: `allow`, `mention`, `multibot`, `block` |
| `permission.bot_user_id`              | Bot's User ID (required)                              |
| `permission.allowed_users`            | User whitelist                                        |
| `permission.blocked_users`            | User blacklist                                        |
| `permission.slash_command_rate_limit` | Rate limit per user                                   |

---

## Platform-Specific Configs

### Slack (socket mode)

```yaml
platform: slack
mode: socket  # Recommended for development

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

### Slack (HTTP/webhook mode)

```yaml
platform: slack
mode: http
server_addr: :8080

security:
  verify_signature: true
```

---

## Examples

### Minimal Slack Config

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

### Multi-Bot Setup

```yaml
platform: slack
mode: socket

security:
  permission:
    bot_user_id: ${HOTPLEX_SLACK_BOT_USER_ID}
    group_policy: multibot  # Key setting for multi-bot
    broadcast_response: |
      Please @mention me if you'd like help.
```

### Docker Production Config

```yaml
platform: slack

provider:
  type: claude-code
  dangerously_skip_permissions: true  # For containerized environments

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

## Related Documentation

- [development.md](development.md) - Development guide
- [ARCHITECTURE.md](ARCHITECTURE.md) - Architecture overview
- [docker-deployment.md](docker-deployment.md) - Docker deployment
- [chatapps/slack-setup-beginner.md](chatapps/slack-setup-beginner.md) - Slack setup guide
