# Docker Multi-Bot Deployment Guide

> **5-Minute Setup**: Run multiple AI bots with a single Docker Compose command
>
> **English** | **[简体中文](docker-multi-bot-deployment_zh.md)**

---

## 📋 Table of Contents

- [Prerequisites](#prerequisites)
- [Quick Start](#quick-start)
- [Configuration Deep Dive](#configuration-deep-dive)
- [Adding New Bots](#adding-new-bots)
- [Troubleshooting](#troubleshooting)

---

## Prerequisites

### Required Environment

| Tool           | Version | Check Command            |
| -------------- | ------- | ------------------------ |
| Docker         | 20.10+  | `docker --version`       |
| Docker Compose | v2+     | `docker compose version` |

### Get Slack Credentials

Each bot needs a separate Slack App created at [Slack API](https://api.slack.com/apps):

```
┌─────────────────────────────────────────────────────────────┐
│  Step 1: Create Slack App                                    │
│  ─────────────────────────────────────────────────────────  │
│  1. Visit https://api.slack.com/apps                        │
│  2. Click "Create New App" → "From scratch"                 │
│  3. Enter App Name (e.g., HotPlex-Bot-01)                   │
│  4. Select your Workspace                                    │
└─────────────────────────────────────────────────────────────┘
          │
          ▼
┌─────────────────────────────────────────────────────────────┐
│  Step 2: Enable Socket Mode                                  │
│  ─────────────────────────────────────────────────────────  │
│  1. Sidebar → Socket Mode                                    │
│  2. Enable Socket Mode                                        │
│  3. Generate App Token → Copy and save (xapp-...)           │
└─────────────────────────────────────────────────────────────┘
          │
          ▼
┌─────────────────────────────────────────────────────────────┐
│  Step 3: Configure OAuth & Permissions                       │
│  ─────────────────────────────────────────────────────────  │
│  1. Sidebar → OAuth & Permissions                            │
│  2. Add these Bot Token Scopes:                              │
│     • channels:history     • groups:history                  │
│     • im:history           • mpim:history                    │
│     • chat:write           • users:read                      │
│     • assistant:write      • files:write                     │
│  3. Install to Workspace                                      │
│  4. Copy Bot User OAuth Token (xoxb-...)                     │
└─────────────────────────────────────────────────────────────┘
          │
          ▼
┌─────────────────────────────────────────────────────────────┐
│  Step 4: Get Bot User ID                                     │
│  ─────────────────────────────────────────────────────────  │
│  1. Sidebar → App Home                                       │
│  2. Find "Bot User" section                                   │
│  3. Copy User ID (UXXXXXXXXXX)                               │
└─────────────────────────────────────────────────────────────┘
          │
          ▼
┌─────────────────────────────────────────────────────────────┐
│  Step 5: Subscribe to Events                                 │
│  ─────────────────────────────────────────────────────────  │
│  1. Sidebar → Event Subscriptions                            │
│  2. Enable Events                                             │
│  3. Add these Bot Events:                                    │
│     • message.channels    • message.groups                   │
│     • message.im          • message.mpim                     │
└─────────────────────────────────────────────────────────────┘
```

**Each bot requires 3 credentials:**

| Credential    | Format        | Source                   |
| ------------- | ------------- | ------------------------ |
| `BOT_USER_ID` | `UXXXXXXXXXX` | App Home page            |
| `BOT_TOKEN`   | `xoxb-...`    | OAuth & Permissions page |
| `APP_TOKEN`   | `xapp-...`    | Socket Mode page         |

---

## Quick Start

### 1. Clone Project

```bash
git clone https://github.com/hrygo/hotplex.git
cd hotplex
```

### 2. Create First Bot Config

```bash
# Copy environment template
cp .env.example .env

# Edit configuration
vim .env
```

**Update key settings in `.env`:**

```bash
# Bot 01 Credentials
HOTPLEX_SLACK_BOT_USER_ID=UXXXXXXXXXX      # Your Bot User ID
HOTPLEX_SLACK_BOT_TOKEN=xoxb-...            # Your Bot Token
HOTPLEX_SLACK_APP_TOKEN=xapp-...            # Your App Token

# GitHub Token (for Git operations)
GITHUB_TOKEN=ghp_xxxx                       # Your GitHub PAT

# Logging (use text for debugging)
HOTPLEX_LOG_LEVEL=INFO
HOTPLEX_LOG_FORMAT=text
```

### 3. Configure Git Identity

```bash
# Run setup script
./scripts/setup_gitconfig.sh

# Or manually create
cat > ~/.gitconfig-hotplex << 'EOF'
[user]
    name = HotPlex Bot
    email = bot@example.com
[init]
    defaultBranch = main
EOF
```

### 4. Start Service

```bash
# Build and start
make docker-up

# View logs
make docker-logs

# Or directly
docker compose logs -f hotplex
```

### 5. Verify Running

```bash
# Check container status
docker compose ps

# Expected output:
# NAME         STATUS    PORTS
# hotplex      healthy   127.0.0.1:18080->8080/tcp
```

**🎉 Done!** Try @mentioning your bot in Slack!

---

## Configuration Deep Dive

### Directory Structure

```
hotplex/
├── docker-compose.yml     # Multi-bot orchestration
├── .env                   # Bot 01 environment
├── .env.secondary         # Bot 02 environment
├── chatapps/configs/
│   ├── slack.yaml         # Slack platform config
│   └── ...
└── scripts/
    └── setup_gitconfig.sh # Git config script
```

### docker-compose.yml Core Concepts

```yaml
# Shared config template (YAML Anchor)
x-hotplex-common: &hotplex-common
  image: ghcr.io/hrygo/hotplex:latest
  restart: unless-stopped
  # ... shared settings

services:
  # Bot 01 - Primary Bot
  hotplex:
    <<: *hotplex-common     # Reference shared config
    container_name: hotplex
    ports:
      - "127.0.0.1:18080:8080"
    env_file:
      - .env                # Bot 01's environment
    volumes:
      # Shared directories
      - ${HOME}/.hotplex:/home/hotplex/.hotplex:rw
      - ${HOME}/.claude:/home/hotplex/.claude:rw
      # Bot 01 exclusive directory ⚠️ MUST be unique!
      - ${HOME}/.slack/BOT_U0AHRCL1KCM:/home/hotplex/projects:rw
      - ${HOME}/.gitconfig-hotplex:/home/hotplex/.gitconfig:ro

  # Bot 02 - Secondary Bot
  hotplex-secondary:
    <<: *hotplex-common
    container_name: hotplex-secondary
    ports:
      - "127.0.0.1:18081:8080"
    env_file:
      - .env.secondary      # Bot 02's environment
    volumes:
      # Shared directories (same as above)
      - ${HOME}/.hotplex:/home/hotplex/.hotplex:rw
      - ${HOME}/.claude:/home/hotplex/.claude:rw
      # Bot 02 exclusive directory ⚠️ MUST be unique!
      - ${HOME}/.slack/BOT_U0AJVRH4YF6:/home/hotplex/projects:rw
      - ${HOME}/.gitconfig-hotplex-secondary:/home/hotplex/.gitconfig:ro
```

### ⚠️ Critical Isolation Rules

| Isolation Item   | Reason                                | Config Location           |
| ---------------- | ------------------------------------- | ------------------------- |
| **projects dir** | Each bot has independent workspace    | volumes `.slack/BOT_xxx`  |
| **gitconfig**    | Each bot has independent Git identity | volumes `.gitconfig-xxx`  |
| **port**         | Avoid port conflicts                  | ports `18080/18081`       |
| **env_file**     | Each bot has independent credentials  | `.env` / `.env.secondary` |

**❌ Wrong (causes conflicts):**

```yaml
# Wrong: Using variable, all bots share same directory
volumes:
  - ${HOTPLEX_PROJECTS_DIR}:/home/hotplex/projects  # ❌
```

**✅ Correct (hardcoded paths):**

```yaml
# Correct: Each bot has hardcoded unique path
volumes:
  - ${HOME}/.slack/BOT_U0AHRCL1KCM:/home/hotplex/projects  # ✅ Bot 01
  - ${HOME}/.slack/BOT_U0AJVRH4YF6:/home/hotplex/projects  # ✅ Bot 02
```

---

## Adding New Bots

### Step 1: Create Slack App

Follow [Prerequisites](#prerequisites) to create a new Slack App and get credentials.

### Step 2: Create Environment File

```bash
# Copy template
cp .env .env.tertiary

# Edit new config
vim .env.tertiary
```

**Update `.env.tertiary`:**

```bash
# Bot 03 Credentials (matching new Slack App)
HOTPLEX_SLACK_BOT_USER_ID=UYYYYYYYYYY      # New Bot's User ID
HOTPLEX_SLACK_BOT_TOKEN=xoxb-yyyy          # New Bot's Token
HOTPLEX_SLACK_APP_TOKEN=xapp-yyyy          # New Bot's App Token
```

### Step 3: Create Working Directory

```bash
# Create bot-specific project directory
mkdir -p ~/.slack/BOT_UYYYYYYYYYY

# Create Git config
cat > ~/.gitconfig-hotplex-tertiary << 'EOF'
[user]
    name = HotPlex Bot 03
    email = bot03@example.com
[init]
    defaultBranch = main
EOF
```

### Step 4: Add to docker-compose.yml

```yaml
  # ============================================================================
  # Bot 03: Tertiary Bot
  # ============================================================================
  hotplex-tertiary:
    <<: *hotplex-common
    container_name: hotplex-tertiary
    depends_on:
      hotplex:
        condition: service_started
    ports:
      - "127.0.0.1:18082:8080"      # New port
    env_file:
      - .env.tertiary               # New environment
    volumes:
      # Shared directories
      - ${HOME}/.hotplex:/home/hotplex/.hotplex:rw
      - ${HOME}/.claude:/home/hotplex/.claude:rw
      - ${HOME}/.claude/settings.json:/home/hotplex/.claude/settings.json:ro
      - hotplex-go-mod:/home/hotplex/go/pkg/mod:rw
      - hotplex-go-build:/home/hotplex/.cache/go-build:rw
      # Bot 03 exclusive directory
      - ${HOME}/.slack/BOT_UYYYYYYYYYY:/home/hotplex/projects:rw
      - ${HOME}/.gitconfig-hotplex-tertiary:/home/hotplex/.gitconfig:ro
    labels:
      - "hotplex.bot.role=tertiary"
      - "hotplex.bot.config=.env.tertiary"
```

### Step 5: Update Git Config Script

Edit `scripts/setup_gitconfig.sh`, add new bot:

```bash
BOT_CONFIGS=(
  "hotplex:HotPlex Bot:bot@example.com"
  "hotplex-secondary:HotPlex Bot 02:bot02@example.com"
  "hotplex-tertiary:HotPlex Bot 03:bot03@example.com"  # New
)
```

### Step 6: Start New Bot

```bash
# Start all bots
make docker-up

# Or only new bot
docker compose up -d hotplex-tertiary

# View logs
docker compose logs -f hotplex-tertiary
```

---

## Common Commands

### Service Management

```bash
# Start all bots
make docker-up

# Stop all bots
make docker-down

# Restart (and sync config)
make docker-restart

# View logs
make docker-logs
docker compose logs -f hotplex           # Bot 01
docker compose logs -f hotplex-secondary # Bot 02

# Check status
docker compose ps

# Enter container for debugging
docker exec -it hotplex /bin/sh
```

### Individual Management

```bash
# Start only Bot 01
docker compose up -d hotplex

# Restart only Bot 02
docker compose restart hotplex-secondary

# View only Bot 01 logs
docker compose logs -f hotplex
```

### Update Image

```bash
# Pull latest image
docker pull ghcr.io/hrygo/hotplex:latest

# Restart
make docker-down
make docker-up
```

---

## Troubleshooting

### Q1: Bot Not Responding

**Check Steps:**

```bash
# 1. Check container status
docker compose ps

# 2. View logs
docker compose logs hotplex | tail -50

# 3. Common causes
```

| Error Message         | Cause                               | Solution                                  |
| --------------------- | ----------------------------------- | ----------------------------------------- |
| `invalid bot_user_id` | Invalid `HOTPLEX_SLACK_BOT_USER_ID` | Check User ID format                      |
| `invalid_auth`        | Invalid token                       | Reinstall App to get new token            |
| `missing scope`       | Insufficient permissions            | Add required OAuth Scopes                 |
| `connection refused`  | Socket Mode not enabled             | Enable Socket Mode and generate App Token |

### Q2: Multi-Bot Message Confusion

**Cause:** Bot isolation misconfigured

**Check:**

```bash
# Confirm each bot has unique working directory
docker exec hotplex ls -la /home/hotplex/projects
docker exec hotplex-secondary ls -la /home/hotplex/projects

# Should see different content
```

**Solution:** Ensure each bot in `docker-compose.yml` has unique volume paths.

### Q3: Git Operations Fail

**Cause:** Git config missing

**Solution:**

```bash
# Check if git config exists
docker exec hotplex cat /home/hotplex/.gitconfig

# Regenerate
./scripts/setup_gitconfig.sh
```

### Q4: Proxy Configuration

If in China or corporate network, configure proxy:

```yaml
# Uncomment in docker-compose.yml
environment:
  ANTHROPIC_BASE_URL: http://host.docker.internal:15721
  HTTP_PROXY: http://host.docker.internal:7897
  HTTPS_PROXY: http://host.docker.internal:7897
```

**Requirements:**
1. Proxy software with "Allow LAN" enabled
2. Ports matching your proxy config

### Q5: Port Conflict

**Error:** `port is already allocated`

**Solution:**

```bash
# Find process using port
lsof -i :18080

# Modify docker-compose.yml to use different port
ports:
  - "127.0.0.1:18090:8080"  # Change to unused port
```

---

## Architecture Diagram

```
┌─────────────────────────────────────────────────────────────────────────┐
│                         Docker Compose                                   │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│  ┌────────────────────┐    ┌────────────────────┐    ┌────────────────┐ │
│  │  hotplex           │    │  hotplex-secondary │    │ hotplex-tertiary│ │
│  │  (Bot 01)          │    │  (Bot 02)          │    │ (Bot 03)       │ │
│  │                    │    │                    │    │                │ │
│  │  Port: 18080       │    │  Port: 18081       │    │ Port: 18082    │ │
│  │  .env              │    │  .env.secondary    │    │ .env.tertiary  │ │
│  │                    │    │                    │    │                │ │
│  │  projects/         │    │  projects/         │    │ projects/      │ │
│  │  BOT_U0AHRCL1KCM   │    │  BOT_U0AJVRH4YF6   │    │ BOT_UYYYYYYYYYY│ │
│  └────────┬───────────┘    └────────┬───────────┘    └───────┬────────┘ │
│           │                         │                        │          │
│           └─────────────┬───────────┴────────────────────────┘          │
│                         │                                               │
│                         ▼                                               │
│           ┌─────────────────────────────┐                               │
│           │     Shared Resources        │                               │
│           │  • ~/.hotplex (DB, configs) │                               │
│           │  • ~/.claude (sessions)     │                               │
│           │  • Go cache volumes         │                               │
│           └─────────────────────────────┘                               │
│                                                                          │
└─────────────────────────────────────────────────────────────────────────┘
                          │
                          ▼
           ┌─────────────────────────────┐
           │        Slack Workspace      │
           │                             │
           │  #general ── @Bot01 ──→ Responds│
           │  #random  ── @Bot02 ──→ Responds│
           │  #dev     ── @Bot03 ──→ Responds│
           └─────────────────────────────┘
```

---

## Related Documentation

- [Docker Deployment Guide](docker-deployment.md) - Single bot deployment
- [Production Guide](production-guide.md) - Production best practices
- [Slack Beginner Guide](chatapps/slack-setup-beginner.md) - Slack configuration details
- [configuration.md](configuration.md) - Complete configuration reference

---

<div align="center">
  <i>Questions? Open an <a href="https://github.com/hrygo/hotplex/issues">GitHub Issue</a></i>
</div>
