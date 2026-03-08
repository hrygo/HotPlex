# Bot Behavior Specification

> Issue: #241
> Status: Final Design
> Related: [Thread Ownership Policy](./thread-ownership-policy.md)

## Overview

This document defines the behavioral specification for HotPlex bots in Slack channels with multiple humans and multiple bots. It covers both basic UX requirements and advanced capabilities.

---

## Core Principles

1. **User Agency** - Humans control the conversation, bots respond
2. **Minimal Noise** - Bots speak only when relevant
3. **Clear Ownership** - Each thread/conversation has clear ownership
4. **Graceful Coordination** - Multiple bots coordinate without chaos

---

## Part 1: Basic Capabilities (Required UX)

These are essential behaviors that every HotPlex bot MUST implement.

---

### 1.0 Channel Behavior (Required)

主频道（Main Channel）是所有成员可见的公共对话区域。以下是必须实现的行为规范。

#### 1.0.1 频道场景定义

```
Channel C1:
├── Humans: UserA, UserB, UserC
├── Bots: BotA (owner=UserA), BotB (owner=UserB), BotC (owner=UserC)
└── 所有成员自由发言
```

#### 1.0.2 主频道消息类型

| 消息类型 | 判断条件 | 示例 |
|---------|---------|------|
| **频道消息** | `thread_ts == null` | "大家好" |
| **Thread 回复** | `thread_ts != null` | 在某消息下回复 |

#### 1.0.3 主频道必须行为

**规则 C1：@ 提及必须响应**

```
UserA: "@BotA 分析这段代码"
→ BotA: 必须响应
→ BotB, BotC: 保持沉默（未被提及）
```

**规则 C2：无 @ 保持沉默**

```
UserA: "有人能帮我看看这个问题吗？"
→ BotA, BotB, BotC: 全部保持沉默
→ 等待人类回应或明确 @ 提及
```

**规则 C3：Bot 响应创建 Thread**

```
UserA: "@BotA 帮我分析"
→ BotA: 响应发送为 Thread Reply（非主频道新消息）
→ Slack API: PostMessage with thread_ts=原消息ts
```

**规则 C4：多 Bot @ 提及**

```
UserA: "@BotA @BotB 你们怎么看？"
→ BotA: 必须响应
→ BotB: 必须响应
→ BotC: 保持沉默
```

**规则 C5：混合 @ 提及**

```
UserA: "@UserB @BotA 请你们看看"
→ BotA: 必须响应（被 @）
→ UserB: 人类自行决定是否响应
```

#### 1.0.4 Bot 主人关注规则

**规则 C6：Bot 默认只关注主人的消息**

| 消息来源 | Bot 行为 |
|---------|---------|
| 主人 @ 提及 | 必须响应 |
| 主人无 @ | 保持沉默（除非在已拥有的 thread 中） |
| 他人 @ 提及 | 可配置：默认沉默 / 可选响应 |
| 他人无 @ | 保持沉默 |

**规则 C7：主人可以 @ 其他 Bot**

```
UserA: "@BotB 来帮我"  （UserA 是 BotA 的主人）
→ BotA: 保持沉默（未提及自己）
→ BotB: 响应（被 @ 提及）
```

#### 1.0.5 主频道决策流程

```
主频道消息接收 (thread_ts == null)
              │
              ▼
    ┌─────────────────┐
    │ 是否被 @ 提及？  │
    └────────┬────────┘
             │
     ┌───────┴───────┐
     ▼               ▼
   YES              NO
     │               │
     ▼               ▼
┌──────────┐   ┌──────────────┐
│ 必须响应  │   │ 保持沉默      │
│ 创建thread│   │ 等待 @ 提及   │
└──────────┘   └──────────────┘
```

#### 1.0.6 他人 @ 提及响应配置

Bot 是否响应非主人的 @ 提及，通过配置控制：

```yaml
bot:
  # 响应他人的 @ 提及策略
  respond_to_others: false  # 默认：只响应主人

  # 可选值：
  # - false: 仅响应主人的 @ 提及
  # - true: 响应所有人的 @ 提及
```

| 配置 | 主人 @ | 他人 @ | 说明 |
|------|-------|-------|------|
| `false` | ✓ 响应 | ✗ 沉默 | 默认，Bot 专属主人 |
| `true` | ✓ 响应 | ✓ 响应 | 共享 Bot 模式 |

---

### 1.1 Mention Response Rules

| Trigger | Behavior | Priority |
|---------|----------|----------|
| `@BotA` (single mention) | MUST respond | High |
| `@BotA @BotB` (multi mention) | MUST respond | High |
| `@BotA @human` (mixed mention) | MUST respond | High |
| No @ (in unowned thread) | MUST stay silent | - |
| No @ (in owned thread) | SHOULD respond | Medium |

### 1.2 Thread Ownership (Basic)

Each bot maintains `owned_threads: Set<thread_key>` with these rules:

| Rule | Behavior |
|------|----------|
| **R1: First Response Claims** | Bot that first responds to a thread claims ownership |
| **R2: Owner Responds** | Only thread owner responds to non-@ messages |
| **R3: @ Transfers Ownership** | `@BotB` transfers ownership from BotA to BotB |
| **R4: Multi-Owner** | `@BotA @BotB` creates shared ownership |
| **R5: Owner Release** | When @mentions exclude current owner, release ownership |

### 1.3 Response Decision Flow

```
Message received
       │
       ▼
┌──────────────────┐    YES    ┌────────────────────┐
│ Am I @mentioned? ├──────────►│ CLAIM ownership    │
└────────┬─────────┘           │ RESPOND            │
         │ NO                  └────────────────────┘
         ▼
┌──────────────────┐    NO     ┌────────────────────┐
│ Do I own thread? ├──────────►│ SILENT             │
└────────┬─────────┘           └────────────────────┘
         │ YES
         ▼
┌──────────────────┐
│ RESPOND          │
└──────────────────┘
```

### 1.4 Channel vs Thread Behavior

| Context | @mention | No @ |
|---------|----------|------|
| Main channel | All mentioned bots respond | All bots silent |
| Owned thread | Owner + mentioned bots respond | Owner responds |
| Unowned thread | Mentioned bots claim & respond | All bots silent |

### 1.5 Basic Configuration

```yaml
bot:
  response_mode: normal    # strict | normal | helpful

  # strict: Only respond to owner's @mentions
  # normal: Respond to owner @mentions + owned threads
  # helpful: Also respond to others' @mentions (if enabled)
```

---

## Part 2: Advanced Capabilities (Enhanced UX)

These features provide improved user experience and advanced functionality.

### 2.1 Implicit Trigger Detection

Beyond explicit @mentions, bots can detect implicit triggers:

| Trigger Type | Detection Method | Confidence |
|--------------|------------------|------------|
| Name mention | Text contains bot name/nickname | Medium |
| Reply to bot | Message is a reply to bot's message | High |
| Context match | Message matches bot's expertise area | Variable |
| Thread continuation | Message in active bot thread | High |

**Implementation:**

```go
type ImplicitTrigger struct {
    Type       string
    Confidence float64  // 0.0 - 1.0
    Source     string   // What triggered this
}

func (a *Adapter) detectImplicitTriggers(msg MessageEvent) []ImplicitTrigger {
    var triggers []ImplicitTrigger

    // Check if replying to bot's message
    if msg.ReplyToTS != "" && a.isBotMessage(msg.ReplyToTS) {
        triggers = append(triggers, ImplicitTrigger{
            Type:       "reply_to_bot",
            Confidence: 0.9,
            Source:     "reply_thread",
        })
    }

    // Check if bot name mentioned (not @ format)
    if strings.Contains(strings.ToLower(msg.Text), a.config.BotName) {
        triggers = append(triggers, ImplicitTrigger{
            Type:       "name_mention",
            Confidence: 0.6,
            Source:     "text_contains_name",
        })
    }

    // Check expertise keywords
    if matched, keyword := a.matchExpertiseKeywords(msg.Text); matched {
        triggers = append(triggers, ImplicitTrigger{
            Type:       "expertise_match",
            Confidence: 0.4,
            Source:     keyword,
        })
    }

    return triggers
}
```

### 2.2 Multi-Bot Coordination

#### Avoid Duplicate Responses

When multiple bots are @mentioned simultaneously:

```go
type CoordinationConfig struct {
    // Delay before responding to allow other bots
    ResponseDelay time.Duration  // Default: 500ms

    // Check for existing responses before sending
    CheckExistingResponses bool

    // Maximum bots that should respond to multi-@mention
    MaxResponders int  // Default: 2
}
```

**Strategy:**
1. Wait briefly (500ms) after receiving @mention
2. Check if another bot already responded
3. If yes: supplement don't duplicate
4. If no: proceed with response

#### Bot Role Specialization

```yaml
bot:
  role:
    name: "code-assistant"
    expertise: ["go", "typescript", "testing"]
    priority: 1  # Higher priority = preferred responder

  # When multi-bot @mention, only respond if:
  # - No higher-priority bot responded
  # - Message matches expertise
```

### 2.3 Response Mode Matrix

| Mode | Owner @ | Owner Non-@ | Others @ | Others Non-@ | Implicit |
|------|---------|-------------|----------|--------------|----------|
| **Strict** | ✓ | ✗ | ✗ | ✗ | ✗ |
| **Normal** | ✓ | Owner thread | Optional* | ✗ | ✗ |
| **Helpful** | ✓ | Owner thread | ✓ | ✗ | Low conf |
| **Chatty** | ✓ | ✓ | ✓ | Optional | Medium+ |

*Optional = controlled by `respond_to_others` flag

### 2.4 Silence Control

Users can control bot verbosity:

| Command | Effect | Scope |
|---------|--------|-------|
| `@bot silence` | Bot goes silent | Current thread |
| `@bot silence 30m` | Bot silent for 30 min | Current thread |
| `@bot silence all` | Bot silent everywhere | All channels |
| `@bot unsilence` | Restore normal behavior | As specified |

**Implementation:**

```go
type SilenceState struct {
    mu       sync.RWMutex
    silenced map[string]*SilenceInfo  // key: channelID or "global"
}

type SilenceInfo struct {
    Until     time.Time
    Scope     SilenceScope  // thread, channel, global
    Reason    string
}

func (s *SilenceState) IsSilenced(channelID, threadTS string) bool {
    s.mu.RLock()
    defer s.mu.RUnlock()

    // Check global silence
    if global, ok := s.silenced["global"]; ok {
        if time.Now().Before(global.Until) {
            return true
        }
    }

    // Check channel silence
    key := channelID
    if info, ok := s.silenced[key]; ok {
        if time.Now().Before(info.Until) {
            return true
        }
    }

    return false
}
```

### 2.5 Thread Lifecycle Management

| Phase | Bot Behavior |
|-------|--------------|
| **Active** | Normal ownership rules |
| **Idle** (no activity > 1h) | Ownership expires (TTL) |
| **Concluded** | Owner can mark thread as closed |
| **Archived** | Thread context available for reference |

**Commands:**
- `@bot done` - Mark thread as complete, release ownership
- `@bot continue` - Reclaim ownership on idle thread

### 2.6 Owner Switching

```go
// Owner can transfer bot ownership to another user
// This changes whose messages the bot prioritizes

type OwnerConfig struct {
    PrimaryOwner   string   // User ID
    TrustedUsers   []string // Can also command the bot
    ResponsePolicy string   // owner_only | trusted | public
}
```

### 2.7 Proactive Behavior (Advanced)

Bots can optionally initiate conversations:

| Trigger | Behavior |
|---------|----------|
| Scheduled reminder | Bot posts at scheduled time |
| Monitor alert | Bot posts when condition detected |
| Follow-up | Bot reminds about pending items |

**Configuration:**

```yaml
bot:
  proactive:
    enabled: true
    requires_approval: true  # Owner must approve proactive messages
    channels: ["C12345"]     # Only in specified channels
```

---

## Part 3: Implementation Phases

### Phase 1: MVP (Required)

**Channel Behavior:**
- [ ] @mention detection and response
- [ ] Respond in thread (not new channel message)
- [ ] Owner-only response mode
- [ ] `respond_to_others` config flag

**Thread Ownership:**
- [ ] Thread ownership tracking
- [ ] Ownership transfer on @mention
- [ ] Multi-owner support
- [ ] Basic TTL for ownership

### Phase 2: Enhanced UX

- [ ] Implicit trigger detection
- [ ] Response mode configuration
- [ ] Silence commands
- [ ] Thread lifecycle (done/continue)

### Phase 3: Advanced Coordination

- [ ] Multi-bot response coordination
- [ ] Role specialization
- [ ] Proactive behavior
- [ ] Owner management

---

## Part 4: Configuration Reference

### Complete Bot Behavior Config

```yaml
bot:
  # Basic Settings
  response_mode: normal      # strict | normal | helpful | chatty

  # Thread Ownership
  thread_ownership:
    enabled: true
    ttl: 24h                 # Ownership expires after idle
    persist: true            # Save to storage

  # Multi-Bot Coordination
  coordination:
    response_delay: 500ms    # Wait before responding
    max_responders: 2        # Max bots responding to same @mention
    check_duplicates: true   # Avoid repeating content

  # Implicit Triggers
  implicit_triggers:
    enabled: false           # Off by default
    confidence_threshold: 0.7
    expertise_keywords: []

  # Silence Control
  silence:
    default_duration: 1h
    max_duration: 24h

  # Owner Settings
  owner:
    primary: "U12345"
    trusted: []
    response_policy: owner_only  # owner_only | trusted | public

  # Proactive (Advanced)
  proactive:
    enabled: false
    requires_approval: true
```

---

## Part 5: User Commands Reference

| Command | Description | Example |
|---------|-------------|---------|
| `@bot <question>` | Ask bot a question | `@Claude explain this code` |
| `@botA @botB <msg>` | Multi-bot question | `@Claude @GPT compare approaches` |
| `@bot silence` | Silence bot in thread | `@Claude silence` |
| `@bot silence 30m` | Timed silence | `@Claude silence 30m` |
| `@bot unsilence` | Restore bot | `@Claude unsilence` |
| `@bot done` | Close thread | `@Claude done` |
| `@bot status` | Show bot state | `@Claude status` |

---

## Summary

| Category | Capabilities |
|----------|--------------|
| **Basic - Channel** | @mention 响应、Thread 回复、主人关注、他人响应开关 |
| **Basic - Thread** | 所有权跟踪、所有权转移、TTL 过期 |
| **Enhanced UX** | 隐式触发、响应模式、静音控制、生命周期 |
| **Advanced** | 多 Bot 协调、角色分工、主动行为 |

**核心原则**:
1. 主频道：无 @ 不响应，有 @ 必响应
2. Thread：所有者响应，@ 转移所有权
3. Bot 默认专属主人，可配置共享

**Key Insight**: Start with clear ownership rules (Phase 1), then layer on intelligence (Phase 2-3). The goal is predictable bot behavior that users can control.
