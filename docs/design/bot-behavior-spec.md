# Bot Behavior Specification

> Issue: #241
> Status: v1.0
> Related: [Thread Ownership Policy](./thread-ownership-policy.md)

## Overview

HotPlex bots in Slack channels with multiple humans and multiple bots require clear behavioral rules. This spec defines **required** basic behaviors and **optional** advanced capabilities.

---

## Core Principles

1. **User Agency** - Humans control the conversation, bots respond
2. **Minimal Noise** - Bots speak only when relevant
3. **Clear Ownership** - Each thread has clear ownership
4. **Graceful Coordination** - Multiple bots coordinate without chaos

---

## Part 1: Basic Capabilities (Required)

### 1.0 Bot Owner Definition

| Role | Description | Config Key |
|------|-------------|------------|
| **Primary Owner** | Bot's专属主人 | `owner.primary` |
| **Trusted Users** | 可命令 Bot 的信任用户 | `owner.trusted` |
| **Others** | 其他频道成员 | - |

```yaml
bot:
  owner:
    primary: "U12345"       # Slack User ID
    trusted: ["U45678"]     # 可选
    policy: owner_only      # owner_only | trusted | public
```

| Policy | Primary Owner | Trusted | Others |
|--------|---------------|---------|--------|
| `owner_only` | ✓ 响应 | ✗ 沉默 | ✗ 沉默 |
| `trusted` | ✓ 响应 | ✓ 响应 | ✗ 沉默 |
| `public` | ✓ 响应 | ✓ 响应 | ✓ 响应 |

---

### 1.1 Channel Behavior (Required)

#### 1.1.1 Scenario Definition

```
Channel C1:
├── Humans: UserA, UserB, UserC
├── Bots: BotA (owner=UserA), BotB (owner=UserB), BotC (owner=UserC)
└── All members can speak freely
```

#### 1.1.2 Message Types

| Type | Condition | Example |
|------|-----------|---------|
| **Channel Message** | `thread_ts == null` | "Hello everyone" |
| **Thread Reply** | `thread_ts != null` | Reply under a message |

#### 1.1.3 Required Rules

**C1: @ Mention → MUST Respond**

```
UserA: "@BotA analyze this code"
→ BotA: MUST respond
→ BotB, BotC: MUST stay silent (not mentioned)
```

**C2: No @ → MUST Stay Silent**

```
UserA: "Can anyone help me?"
→ All bots: MUST stay silent
→ Wait for human response or explicit @
```

**C3: Respond in Thread**

```
UserA: "@BotA help me"
→ BotA: Response as Thread Reply (not new channel message)
→ Slack API: PostMessage with thread_ts=original_ts
```

**C4: Multi-Bot @ Mention**

```
UserA: "@BotA @BotB what do you think?"
→ BotA: MUST respond
→ BotB: MUST respond
→ BotC: MUST stay silent
```

**C5: Mixed @ Mention**

```
UserA: "@UserB @BotA please review"
→ BotA: MUST respond (mentioned)
→ UserB: Human decides whether to respond
```

**C6: Owner Priority**

```
UserA: "@BotA help" (UserA is BotA's owner)
→ BotA: MUST respond

UserB: "@BotA help" (UserB is NOT BotA's owner)
→ BotA: Check owner.policy:
  - owner_only → SILENT
  - trusted AND UserB in trusted[] → RESPOND
  - public → RESPOND
```

---

### 1.2 Thread Ownership (Basic)

Each bot maintains `owned_threads: Set<thread_key>`:

| Rule | Behavior |
|------|----------|
| **R1: First Response Claims** | Bot that first responds claims thread ownership |
| **R2: Owner Responds** | Only thread owner responds to non-@ messages |
| **R3: @ Transfers Ownership** | `@BotB` transfers ownership from BotA to BotB |
| **R4: Multi-Owner** | `@BotA @BotB` creates shared ownership |
| **R5: Owner Release** | @mentions excluding current owner → release ownership |

---

### 1.3 Unified Decision Flow

```
Message received
       │
       ▼
┌───────────────────────┐
│ Check silence state   │──── SILENCED ───►│ SILENT │
└───────────┬───────────┘
            │ NOT SILENCED
            ▼
┌───────────────────────┐
│ Is it a DM?           │──── YES ───►│ Treat as @ mention │
└───────────┬───────────┘
            │ NO (Channel)
            ▼
┌───────────────────────┐    YES    ┌─────────────────────┐
│ Am I @mentioned?      ├──────────►│ Check owner.policy  │
└───────────┬───────────┘           │ If allowed:         │
            │ NO                    │ - CLAIM ownership   │
            ▼                       │ - RESPOND           │
┌───────────────────────┐           └─────────────────────┘
│ Do I own this thread? │
└───────────┬───────────┘    NO     ┌─────────────────────┐
            ├──────────────────────►│ SILENT              │
            │ YES                   └─────────────────────┘
            ▼
┌───────────────────────┐
│ Update LastActive     │
│ RESPOND               │
└───────────────────────┘
```

---

### 1.4 Channel vs Thread Behavior Matrix

| Context | @ Mention | No @ |
|---------|-----------|------|
| Main Channel | Mentioned bots respond (per policy) | All bots silent |
| Owned Thread | Owner + mentioned bots respond | Owner responds |
| Unowned Thread | Mentioned bots claim & respond | All bots silent |

---

### 1.5 DM (Direct Message) Behavior

| Scenario | Behavior |
|----------|----------|
| Owner DM | All messages treated as @ mention |
| Non-owner DM | Check `owner.policy` |

```yaml
bot:
  dm:
    owner_as_mention: true   # Owner DM always triggers response
    others_policy: inherit   # inherit from owner.policy | always_respond | ignore
```

---

### 1.6 Edge Cases

| Case | Handling |
|------|----------|
| Empty @ mention (`@BotA` with no content) | Prompt user for more info |
| Bot self-mention | Ignore (bot won't @ itself) |
| @ mention to offline bot | Other bots respond normally |
| Thread TTL exceeded | Ownership expires, silent until re-@ |

---

## Part 2: Advanced Capabilities (Optional)

### 2.1 Implicit Trigger Detection

| Trigger Type | Detection | Confidence |
|--------------|-----------|------------|
| Name mention | Text contains bot name | 0.6 |
| Reply to bot | Message replies to bot's message | 0.9 |
| Expertise match | Keywords match bot's expertise | 0.4 |

```go
type ImplicitTrigger struct {
    Type       string
    Confidence float64
    Source     string
}

// Only trigger if confidence >= threshold
func (a *Adapter) shouldRespondToImplicit(triggers []ImplicitTrigger) bool {
    for _, t := range triggers {
        if t.Confidence >= a.config.ImplicitThreshold {
            return true
        }
    }
    return false
}
```

---

### 2.2 Silence Control

| Command | Effect | Scope |
|---------|--------|-------|
| `@bot silence` | Bot silent | Current thread |
| `@bot silence 30m` | Bot silent for 30 min | Current thread |
| `@bot silence channel` | Bot silent in channel | Current channel |
| `@bot silence all` | Bot silent everywhere | Global |
| `@bot unsilence` | Restore normal behavior | As specified |

---

### 2.3 Thread Lifecycle

| Phase | Condition | Behavior |
|-------|-----------|----------|
| Active | Recent activity | Normal ownership rules |
| Idle | No activity > TTL | Ownership expires |
| Closed | `@bot done` | Release ownership immediately |

Commands:
- `@bot done` - Mark complete, release ownership
- `@bot continue` - Reclaim ownership on idle thread

---

### 2.4 Multi-Bot Coordination

```go
type CoordinationConfig struct {
    ResponseDelay  time.Duration  // Wait before responding (default: 500ms)
    MaxResponders  int            // Max bots per @mention (default: 2)
    CheckDuplicate bool           // Avoid repeating content
}
```

Strategy:
1. Wait 500ms after @mention
2. Check if other bot responded
3. If yes: supplement, don't duplicate
4. If no: proceed

---

### 2.5 Proactive Behavior

| Trigger | Behavior |
|---------|----------|
| Scheduled reminder | Post at scheduled time |
| Monitor alert | Post when condition detected |
| Follow-up | Remind about pending items |

```yaml
bot:
  proactive:
    enabled: false
    requires_approval: true
    channels: []  # Empty = all channels
```

---

## Part 3: Implementation Phases

### Phase 1: MVP (Required)

- [ ] @mention detection and response
- [ ] Respond in thread (not channel message)
- [ ] Owner policy configuration
- [ ] Thread ownership tracking
- [ ] Ownership transfer on @mention
- [ ] Multi-owner support
- [ ] TTL expiration

### Phase 2: Enhanced UX

- [ ] Implicit trigger detection
- [ ] Silence commands
- [ ] Thread lifecycle (done/continue)
- [ ] DM behavior

### Phase 3: Advanced

- [ ] Multi-bot coordination
- [ ] Proactive behavior

---

## Part 4: Configuration Reference

```yaml
bot:
  # Response mode (deprecated: use owner.policy)
  # response_mode: normal

  # Owner configuration
  owner:
    primary: "U12345"
    trusted: []
    policy: owner_only  # owner_only | trusted | public

  # DM behavior
  dm:
    owner_as_mention: true
    others_policy: inherit

  # Thread ownership
  thread_ownership:
    enabled: true
    ttl: 24h
    persist: true

  # Silence control
  silence:
    default_duration: 1h
    max_duration: 24h

  # Implicit triggers (Phase 2)
  implicit_triggers:
    enabled: false
    confidence_threshold: 0.7

  # Multi-bot coordination (Phase 3)
  coordination:
    response_delay: 500ms
    max_responders: 2
    check_duplicates: true

  # Proactive behavior (Phase 3)
  proactive:
    enabled: false
    requires_approval: true
```

---

## Part 5: User Commands

| Command | Description |
|---------|-------------|
| `@bot <question>` | Ask bot a question |
| `@botA @botB <msg>` | Multi-bot question |
| `@bot silence [duration]` | Silence bot |
| `@bot unsilence` | Restore bot |
| `@bot done` | Close thread |
| `@bot status` | Show bot state |
| `@bot continue` | Reclaim ownership |

---

## Summary

| Category | Capabilities |
|----------|--------------|
| **Basic** | @ 响应、Thread 回复、Owner Policy、所有权跟踪、所有权转移、TTL |
| **Enhanced** | 隐式触发、静音控制、生命周期、DM 行为 |
| **Advanced** | 多 Bot 协调、主动行为 |

**核心原则**:
1. 主频道：无 @ 不响应，有 @ 必响应（检查 policy）
2. Thread：所有者响应，@ 转移所有权
3. Bot 默认专属主人，通过 policy 配置共享
