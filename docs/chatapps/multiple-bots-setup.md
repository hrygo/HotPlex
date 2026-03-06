# Multiple Bot Instances Setup Guide

This guide explains how to run multiple HotPlex bot instances with separate Slack Apps.

## Why Separate Slack Apps?

Slack Socket Mode only supports **one active WebSocket connection** per App Token. If you try to use the same token in multiple containers, Slack will only send messages to one of them.

## MultiBot @ Routing Mode (Recommended)

When running multiple bots in the same channel, use `group_policy: multibot` for intelligent routing.

### How It Works

| Message | BotA Response | BotB Response |
|---------|---------------|---------------|
| `hello` | Polite response | Polite response |
| `@BotA help` | Processes normally | Ignores |
| `@BotB help` | Ignores | Processes normally |
| `@BotA @BotB hi` | Processes normally | Processes normally |

### Configuration

In `chatapps/configs/slack.yaml`:

```yaml
security:
  permission:
    group_policy: multibot
    bot_user_id: UXXXXXXXXXX  # Each bot's own ID

    # Optional: Custom broadcast response
    broadcast_response: |
      Hello! I'm ready to help.
      Please @mention me if you'd like me to respond specifically to you.
```

### Broadcast Response

When a message has no @ mentions (broadcast), all bots send a polite response. This is handled by `BroadcastResponder`:

- **Default**: "Hello! I'm ready to help. Please @mention me if you'd like me to respond specifically to you."
- **Custom**: Set via `broadcast_response` in YAML config
- **Future**: Will integrate with native brain for intelligent responses

### Comparison with Other Policies

| Policy | Broadcast (no @) | @BotA | @OtherBot |
|--------|------------------|-------|-----------|
| `allow` | Respond | Respond | Respond |
| `mention` | Ignore | Respond | Ignore |
| `multibot` | Polite response | Respond | Ignore |
| `block` | Ignore | Ignore | Ignore |

## Step 1: Create Secondary Slack App

1. Go to https://api.slack.com/apps
2. Click "Create New App"
3. Fill in:
   - App Name: `HotPlex Bot 02` (or your preferred name)
   - Development Slack Workspace: Select your workspace

## Step 2: Configure Bot Permissions

In your new app, navigate to **OAuth & Permissions**:

### Bot Token Scopes
Add these scopes:
```
channels:history
channels:read
channels:write
chat:write
groups:history
groups:read
groups:write
im:history
im:read
im:write
mpim:history
mpim:read
mpim:write
users:read
users:write
users.profile:read
```

### Install to Workspace
Click "Install to Workspace" to generate the Bot User OAuth Token (`xoxb-*`)

## Step 3: Generate App-Level Token

1. Go to **Basic Information**
2. Scroll to **App-Level Tokens**
3. Click "Generate Token and Scopes"
4. Add scope: `connections:write`
5. Click "Generate"
6. Copy the generated token (`xapp-*`)

## Step 4: Get Signing Secret

1. Go to **Basic Information**
2. Copy the **Signing Secret**
3. Get Bot User ID from "App Home" or via API

## Step 5: Configure .env.secondary

```bash
# Slack Secondary Bot Configuration
HOTPLEX_SLACK_MODE=socket
HOTPLEX_SLACK_BOT_TOKEN=xoxb-your-secondary-bot-token
HOTPLEX_SLACK_APP_TOKEN=xapp-your-secondary-app-token
HOTPLEX_SLACK_SIGNING_SECRET=your-secondary-signing-secret
HOTPLEX_SLACK_BOT_USER_ID=UXXXXXXXXXX
```

## Step 6: Create slack_secondary.yaml

Create `~/.hotplex/configs/slack_secondary.yaml`:

```yaml
platform: slack

provider:
  type: claude-code
  enabled: true
  default_model: sonnet
  dangerously_skip_permissions: true

engine:
  work_dir: ~/projects/your-secondary-project
  timeout: 30m
  idle_timeout: 60m

system_prompt: |
  You are HotPlexBot02, an expert software engineer...
  (customize as needed)

features:
  chunking:
    enabled: true
    max_chars: 4000
  threading:
    enabled: true

security:
  verify_signature: true
  permission:
    dm_policy: allow
    group_policy: mention
    bot_user_id: ${HOTPLEX_SLACK_BOT_USER_ID:-UXXXXXXXXXX}
```

## Step 7: Restart Containers

```bash
docker compose down
docker compose up -d
```

## Troubleshooting

### Both bots not receiving messages
- Check that each container uses different tokens
- Verify tokens are not swapped between .env files

### Connection errors in logs
```bash
docker logs hotplex-secondary 2>&1 | grep -i "error\|fail"
```

### Messages going to wrong bot
- Check `bot_user_id` in config matches the actual bot user ID
- Verify group_policy settings
