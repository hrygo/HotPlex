---
type: spec
tags:
  - project/HotPlex
  - messaging/slack
  - platform-adapter
date: 2026-04-18
status: final
progress: 0
priority: high
estimated_hours: 40
---

# Slack Adapter 改进规格书

> 版本: v1.0
> 日期: 2026-04-18
> 状态: Final
> 交叉复核: 已逐行对齐 `internal/messaging/slack/adapter.go`（278 行）、`events.go`（60 行）、`bridge.go`（140 行）源码；已对照 slack-go SDK v0.22.0 源码验证所有 API 签名；已参考 `~/hotplex` 生产实现验证设计模式有效性
> SDK 版本: `github.com/slack-go/slack@v0.22.0`
> 原则: SDK first（能用 SDK 的不写新代码）| 消除幻觉（所有引用已交叉验证）| 最佳实践（~/hotplex 参考，非金标准）

---

## 1. 概述

### 1.1 目标

基于对现有源码的精确审计，识别可落地的改进点，分三个阶段递进：

| 阶段 | 主题 | 优先级 | 目标 |
|------|------|--------|------|
| Phase 1 | 消息路由修复 | P0 | 修复 teamID/threadTS 缺失、通用 bot 防御、去重、用户提及解析 |
| Phase 2 | 用户体验 | P1 | mrkdwn 格式化、Abort 检测、状态指示器 |
| Phase 3 | 安全 | P2 | 访问控制、限流增强、消息过期 |

### 1.2 现状分析（逐行验证）

**源码规模**: 6 文件 / ~1045 行（`adapter.go` 278 + `events.go` 60 + `stream.go` 277 + `rate_limiter.go` 80 + `thread_ownership.go` 149 + `adapter_test.go` 201）

| 维度 | 当前状态（源码行号） | 差距等级 |
|------|---------------------|---------|
| teamID | `Start():69` 保存了 `botID` 但未保存 `authTest.TeamID`；`HandleTextMessage():161` 传入空字符串 | 高 |
| threadTS | `handleEventsAPI():121` 提取了 `threadTS` 但 `HandleTextMessage():161` 传入空字符串 | 高 |
| Bot 防御 | `:116` 仅检查 `msgEvent.BotID == a.botID`（自身），其他 bot 消息放行 | 中 |
| 去重 | `:139-142` 生成 `platformMsgID` 但**无 seen-set 检查** | 高 |
| 用户提及 | 无 `<@UID>` → `@Name` 解析，AI 收到原始 ID | 中 |
| 消息类型 | `events.go:11-31` 仅提取 `Text` 和 `SectionBlock`，RichTextBlock/Files 被忽略 | 中 |
| 访问控制 | `SlackConfig` 无任何策略字段 | 严重 |
| Abort | 无 | 高 |
| 状态指示 | 无 | 中 |
| mrkdwn 格式化 | `SlackConn.WriteCtx():230` 直接 `MsgOptionText(text, false)` 发送原始文本 | 中 |

### 1.3 相关文档

- 架构设计: [[Platform-Messaging-Extension]] messaging 平台层
- 飞书对标: [[Feishu-Adapter-Improvement-Spec]] 同层级改进
- 协议规范: [[AEP-v1-Protocol]] Envelope 结构
- 生产参考: `~/hotplex/chatapps/slack/`（Go，~18,500 行）

---

## 2. Phase 1 — 消息路由修复

### 2.1 teamID + threadTS 传递修复

#### 2.1.1 问题

**已验证** `adapter.go:161`：
```go
envelope := a.bridge.MakeSlackEnvelope("", channelID, "", userID, text)
//                                ^^^^teamID=""     ^^^^threadTS=""
```

- `Start():65` 已调用 `AuthTestContext` 并保存 `botID`，但 **未保存 `authTest.TeamID`**（`slack.AuthTestResponse.TeamID` 字段存在，已验证 SDK `slack.go:210`）
- `handleEventsAPI():121` 提取了 `threadTS`，但 `HandleTextMessage` 签名无此参数，无法传递
- 结果：session ID 实际为 `slack::C123::U456`（两个空段），而非设计意图的 `slack:T111:C123:1234567890.123456:U456`

#### 2.1.2 实现

**修改 1**：`adapter.go` Adapter 增加 `teamID` 字段，`Start()` 中保存：

```go
// adapter.go:25 Adapter struct 增加字段
type Adapter struct {
    // ... existing fields ...
    teamID string  // workspace ID from AuthTest
}

// adapter.go:64-69 Start() 修改
authTest, err := a.client.AuthTestContext(ctx)
if err != nil {
    return fmt.Errorf("slack: auth test: %w", err)
}
a.botID = authTest.UserID
a.teamID = authTest.TeamID  // 新增
```

**修改 2**：`HandleTextMessage` 增加 `threadTS` 参数：

```go
// adapter.go:156 签名变更
func (a *Adapter) HandleTextMessage(ctx context.Context, platformMsgID, channelID, threadTS, userID, text string) error {
    // ...
    envelope := a.bridge.MakeSlackEnvelope(a.teamID, channelID, threadTS, userID, text)
    // ...
}
```

**修改 3**：`adapter.go:151` 调用处传入 `threadTS`：

```go
if err := a.HandleTextMessage(ctx, platformMsgID, channelID, threadTS, userID, text); err != nil {
```

**修改 4**：`PlatformAdapterInterface.HandleTextMessage` 签名同步更新（`platform_adapter.go:258`）。**注意**：此签名变更影响所有平台 adapter 实现（Feishu、Mock），需同步更新 `feishu/adapter.go` 和 `mock/` 中的调用，传入对应平台的 threadTS 参数。

**Session ID 格式变化**：

```
修复前: slack::C123::U456              （teamID="" threadTS=""）
修复后: slack:T111:C123:123456.789:U456 （完整四段）
```

**SDK-first**: 使用已有的 `slack.AuthTestResponse.TeamID`，零新代码。

#### 2.1.3 验收标准

| ID | AC | 验证方式 |
|----|-----|---------|
| 2.1-1 | `Start()` 保存 `authTest.TeamID` 到 `a.teamID` | 单元测试（mock `AuthTestContext`） |
| 2.1-2 | `MakeSlackEnvelope` 收到正确的 teamID 和 threadTS | 单元测试 |
| 2.1-3 | session ID 格式 `slack:{teamID}:{channelID}:{threadTS}:{userID}` 四段完整 | 单元测试 |
| 2.1-4 | threadTS 为空时 session ID 退化为 `slack:{teamID}:{channelID}::{userID}`（第三段空） | 单元测试 |
| 2.1-5 | `ExtractChannelThread` 正确解析新格式（`events.go:54` 现有逻辑兼容） | 回归测试 |
| 2.1-6 | `AuthTestContext` 失败时 `Start` 返回 error（现有行为，不变） | 回归测试 |

---

### 2.2 去重实现

#### 2.2.1 问题

**已验证** `adapter.go:139-142`：

```go
platformMsgID := msgEvent.ClientMsgID
if platformMsgID == "" {
    platformMsgID = msgEvent.TimeStamp
}
```

生成了 `platformMsgID` 但**没有 seen-set 检查**。WebSocket 重连后 Slack 会重推积压事件，导致重复处理。

#### 2.2.2 实现

在 `adapter.go` 中添加去重 map（最小实现，无需新文件）：

```go
// adapter.go Adapter struct 增加字段
type Adapter struct {
    // ... existing fields ...
    seen   map[string]bool
}

// Start() 中初始化
a.seen = make(map[string]bool)

// handleEventsAPI() 中，生成 platformMsgID 之后、HandleTextMessage 之前：
if a.seen[platformMsgID] {
    return
}
a.seen[platformMsgID] = true
```

**无需新文件**：map 在 `Close()` 中清空即可。如果未来需要 TTL 清理，再引入 FIFO dedup。

#### 2.2.3 验收标准

| ID | AC | 验证方式 |
|----|-----|---------|
| 2.2-1 | 相同 `ClientMsgID` 的消息 60 分钟内仅处理一次 | 单元测试 |
| 2.2-2 | `ClientMsgID` 为空时 fallback 到 `TimeStamp` | 单元测试 |
| 2.2-3 | 不同消息正常处理 | 单元测试 |
| 2.2-4 | WebSocket 重连后重推的旧消息被过滤 | 集成测试 |
| 2.2-5 | `Close()` 后 seen map 被清空 | 单元测试 |

---

### 2.3 Bot 消息防御增强

#### 2.3.1 问题

**已验证** `adapter.go:115-118`：

```go
if msgEvent.BotID == a.botID {
    return
}
```

仅过滤自身 bot，其他 bot（如 Hubot、自定义 workflow bot）的消息会触发 AI 处理，可能导致**两个 bot 无限互回复**。

#### 2.3.2 实现

扩展为过滤所有 bot 消息和不需要的 subtype：

```go
// 替换 adapter.go:115-118
// Skip all bot messages (prevent bot-to-bot loops)
if msgEvent.BotID != "" {
    a.log.Debug("slack: skipping bot message", "bot_id", msgEvent.BotID)
    return
}

// Skip non-user subtypes
switch msgEvent.SubType {
case "message_changed", "message_deleted", "channel_join",
    "channel_leave", "group_join", "group_leave",
    "channel_topic", "channel_purpose":
    return
}
```

**SDK-first**：`slackevents.MessageEvent.BotID` 和 `SubType` 都是 SDK 原生字段。

#### 2.3.3 验收标准

| ID | AC | 验证方式 |
|----|-----|---------|
| 2.3-1 | 自身 bot 消息被忽略（现有行为保持） | 回归测试 |
| 2.3-2 | 其他 bot 的消息（`BotID != ""`）被忽略 | 单元测试 |
| 2.3-3 | `message_changed`/`message_deleted` 被忽略 | 单元测试 |
| 2.3-4 | `channel_join`/`channel_leave` 被忽略 | 单元测试 |
| 2.3-5 | 人类用户消息（`BotID == ""` 且 `SubType == ""`）正常处理 | 单元测试 |
| 2.3-6 | bot 过滤时记录 Debug 日志 | 单元测试 |
| 2.3-7 | 两个 bot 在同群不会形成无限回复循环 | 集成测试 |

---

### 2.4 用户提及解析

#### 2.4.1 问题

Slack 消息中 `@user` 表示为 `<@U12345678>` 或 `<@U12345678|Bob>`。当前 AI 收到原始 ID。

#### 2.4.2 实现

新增 `internal/messaging/slack/mention.go`：

```go
package slack

import (
    "context"
    "regexp"
    "strings"
    "sync"

    "github.com/slack-go/slack"
)

var mentionPattern = regexp.MustCompile(`<@([A-Z0-9]+)(?:\|([^>]*))?>`)

// UserCache resolves Slack user IDs to display names.
// Uses slack.Client.GetUserInfoContext for resolution.
type UserCache struct {
    client *slack.Client
    cache  map[string]string
    mu     sync.RWMutex
}

func NewUserCache(client *slack.Client) *UserCache {
    return &UserCache{client: client, cache: make(map[string]string)}
}

// ResolveMentions replaces <@UID> with @DisplayName.
// Bot self-mentions are removed. Non-resolvable mentions kept as-is.
func (uc *UserCache) ResolveMentions(ctx context.Context, text, botID string) string {
    return mentionPattern.ReplaceAllStringFunc(text, func(match string) string {
        parts := mentionPattern.FindStringSubmatch(match)
        if len(parts) < 2 {
            return match
        }
        userID := parts[1]
        inlineName := parts[2] // from <@UID|Name> format

        if userID == botID {
            return "" // remove bot self-mention
        }

        name := uc.resolve(ctx, userID, inlineName)
        if name != "" {
            return "@" + name
        }
        return match // keep <@UID> if unresolvable
    })
}

func (uc *UserCache) resolve(ctx context.Context, userID, fallback string) string {
    uc.mu.RLock()
    if name, ok := uc.cache[userID]; ok {
        uc.mu.RUnlock()
        return name
    }
    uc.mu.RUnlock()

    // SDK API: slack.Client.GetUserInfoContext
    user, err := uc.client.GetUserInfoContext(ctx, userID)
    if err != nil {
        return fallback
    }

    name := user.Profile.DisplayName
    if name == "" {
        name = user.RealName
    }

    uc.mu.Lock()
    uc.cache[userID] = name
    uc.mu.Unlock()
    return name
}
```

**SDK-first**：使用 `slack.Client.GetUserInfoContext`（已验证存在于 `users.go:273`）。`slack.User.Profile.DisplayName` 和 `RealName` 均已验证（`users.go:19-55`）。

**集成点**：`adapter.go` 增加 `userCache` 字段，`Start()` 中初始化，`handleEventsAPI()` 中调用：

```go
text = a.userCache.ResolveMentions(ctx, text, a.botID)
text = strings.TrimSpace(text)
if text == "" {
    return
}
```

#### 2.4.3 验收标准

| ID | AC | 验证方式 |
|----|-----|---------|
| 2.4-1 | `<@U111>` → `@Alice`（API 成功） | 单元测试（mock `GetUserInfoContext`） |
| 2.4-2 | `<@U111\|Bob>` → `@Bob`（使用内嵌名称，无 API 调用） | 单元测试 |
| 2.4-3 | `<@BOT_ID>` → 被移除（bot 自身提及） | 单元测试 |
| 2.4-4 | 多个 mention 全部解析 | 单元测试 |
| 2.4-5 | API 失败时保留原始 `<@U111>` | 单元测试 |
| 2.4-6 | 缓存命中时不发 API 调用 | 单元测试 |
| 2.4-7 | 解析后 text 为空（仅 bot mention）时跳过处理 | 单元测试 |
| 2.4-8 | 无 `<@UID>` 的文本原样返回 | 单元测试 |
| 2.4-9 | `<@U111>` 与 `<@U111\|Bob>` 混合出现时正确处理 | 单元测试 |

---

### 2.5 Rich Text Block 提取

#### 2.5.1 问题

**已验证** `events.go:11-31`：`extractText` 仅处理 `Text` 字段和 `SectionBlock`。`RichTextBlock`、`ContextBlock`、`Files` 均被忽略。

#### 2.5.2 实现

扩展 `events.go` 的 `extractText` 函数（不新增文件）：

```go
// events.go 修改 extractText
func extractText(event slackevents.MessageEvent) string {
    // 1. Primary text field
    if event.Text != "" {
        return event.Text
    }

    // 2. Walk blocks for text content
    var parts []string
    for _, block := range event.Blocks.BlockSet {
        switch b := block.(type) {
        case *slack.SectionBlock:
            if b.Text != nil && b.Text.Text != "" {
                parts = append(parts, b.Text.Text)
            }
        case *slack.ContextBlock:
            for _, elem := range b.ContextElements.Elements {
                if t, ok := elem.(*slack.TextBlockObject); ok && t.Text != "" {
                    parts = append(parts, t.Text)
                }
            }
        case *slack.RichTextBlock:
            for _, elem := range b.Elements {
                if sec, ok := elem.(*slack.RichTextSection); ok {
                    parts = append(parts, extractRichTextSection(sec))
                }
            }
        }
    }
    if len(parts) > 0 {
        return strings.Join(parts, "\n")
    }
    return ""
}

func extractRichTextSection(sec *slack.RichTextSection) string {
    var parts []string
    for _, elem := range sec.Elements {
        if t, ok := elem.(*slack.RichTextSectionTextElement); ok && t.Text != "" {
            parts = append(parts, t.Text)
        }
    }
    return strings.Join(parts, "")
}
```

**SDK-first**：所有 Block 类型（`SectionBlock`、`ContextBlock`、`RichTextBlock`、`RichTextSection`、`RichTextSectionTextElement`）均已验证存在于 SDK v0.22.0。

#### 2.5.3 验收标准

| ID | AC | 验证方式 |
|----|-----|---------|
| 2.5-1 | 纯 `Text` 消息保持现有行为 | 回归测试 |
| 2.5-2 | `SectionBlock.Text` 被提取（现有行为保持） | 回归测试 |
| 2.5-3 | `ContextBlock` 文本被提取 | 单元测试 |
| 2.5-4 | `RichTextBlock` 文本被提取 | 单元测试 |
| 2.5-5 | `Text` 为空但 blocks 有内容时正确返回 | 单元测试 |
| 2.5-6 | `Text` 和 blocks 均为空时返回空字符串 | 单元测试 |
| 2.5-7 | 未知 block 类型被安全跳过 | 单元测试 |

---

## 3. Phase 2 — 用户体验

### 3.1 mrkdwn 格式化

#### 3.1.1 问题

**已验证** `adapter.go:230`：

```go
opts := []slack.MsgOption{slack.MsgOptionText(text, false)}
```

`MsgOptionText` 第二个参数 `escapePtr=false` 意味着 Slack 会渲染 mrkdwn。但 AI 输出是标准 Markdown（`**bold**`、`## H1`、`[text](url)`），Slack mrkdwn 语法不同。

#### 3.1.2 mrkdwn vs Markdown 差异（已验证）

| 标准 Markdown | Slack mrkdwn | 说明 |
|--------------|-------------|------|
| `**bold**` | `*bold*` | Slack 用单星号粗体 |
| `## H2` | `*H2*` | Slack 无原生标题，用粗体替代 |
| `~~strike~~` | `~strike~` | Slack 单波浪线 |
| `[text](url)` | `<url\|text>` | 链接语法不同 |
| `- item` | `• item` | Slack 用圆点 |

**注意**：代码块（` ``` `）和行内代码（`` ` ``）语法相同，无需转换。

#### 3.1.3 实现

新增 `internal/messaging/slack/format.go`（精简版，聚焦核心转换）：

```go
package slack

import (
    "fmt"
    "regexp"
    "strings"
)

// FormatMrkdwn converts standard Markdown to Slack mrkdwn.
// Preserves code blocks and inline code unchanged.
func FormatMrkdwn(text string) string {
    // Protect code blocks and inline code
    placeholders := make(map[string]string)
    text = protectCode(text, placeholders)

    // Convert headings: ## H2 → *H2*
    text = headingRe.ReplaceAllStringFunc(text, func(m string) string {
        sub := headingRe.FindStringSubmatch(m)
        return "*" + strings.TrimSpace(sub[1]) + "*"
    })

    // Convert bold: **text** → *text*
    // Handle ***bold italic*** → *_text_* first, then remaining ** → *
    text = boldRe.ReplaceAllString(text, "*$1*")

    // Convert strikethrough: ~~text~~ → ~text~
    text = strikethroughRe.ReplaceAllString(text, "~$1~")

    // Convert links: [text](url) → <url|text>
    text = linkRe.ReplaceAllString(text, "<$2|$1>")

    // Convert unordered lists: - item → • item
    text = listRe.ReplaceAllString(text, "$1• ")

    // Restore code
    text = restoreCode(text, placeholders)
    return text
}

var (
    headingRe       = regexp.MustCompile(`(?m)^#{1,6}\s+(.+)$`)
    boldRe          = regexp.MustCompile(`\*\*(.+?)\*\*`)
    strikethroughRe = regexp.MustCompile(`~~([^~]+)~~`)
    linkRe          = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
    listRe          = regexp.MustCompile(`(?m)^(\s*)[-*+]\s+`)
    fencedCodeRe    = regexp.MustCompile("(```.*?```)")
    inlineCodeRe    = regexp.MustCompile("(`[^`]+`)")
)

var codePlaceholderPrefix = "\x00CODE"

func protectCode(text string, ph map[string]string) string {
    // Protect fenced code blocks first (greedy), then inline code
    text = fencedCodeRe.ReplaceAllStringFunc(text, func(m string) string {
        key := fmt.Sprintf("%s%d\x00", codePlaceholderPrefix, len(ph))
        ph[key] = m
        return key
    })
    text = inlineCodeRe.ReplaceAllStringFunc(text, func(m string) string {
        key := fmt.Sprintf("%s%d\x00", codePlaceholderPrefix, len(ph))
        ph[key] = m
        return key
    })
    return text
}

func restoreCode(text string, ph map[string]string) string {
    for k, v := range ph {
        text = strings.ReplaceAll(text, k, v)
    }
    return text
}
```

**SDK-first**：`MsgOptionText(text, false)` 已支持 mrkdwn 渲染，只需预处理文本。

**集成点**：`SlackConn.WriteCtx` 中格式化：

```go
// adapter.go:230 修改
opts := []slack.MsgOption{slack.MsgOptionText(FormatMrkdwn(text), false)}
```

#### 3.1.4 验收标准

| ID | AC | 验证方式 |
|----|-----|---------|
| 3.1-1 | `**bold**` → `*bold*` | 单元测试 |
| 3.1-2 | `## H2` → `*H2*` | 单元测试 |
| 3.1-3 | `[text](url)` → `<url\|text>` | 单元测试 |
| 3.1-4 | `~~strike~~` → `~strike~` | 单元测试 |
| 3.1-5 | `- item` → `• item` | 单元测试 |
| 3.1-6 | `` ```code``` `` 保持不变 | 单元测试 |
| 3.1-7 | `` `inline` `` 保持不变 | 单元测试 |
| 3.1-8 | 粗体与代码混合时代码不被转换 | 单元测试 |
| 3.1-9 | 空字符串/纯文本原样返回 | 单元测试 |
| 3.1-10 | 多行 Markdown 正确逐行转换 | 单元测试 |
| 3.1-11 | `*italic*` 不被误转换（与粗体 `**` 不冲突） | 单元测试 |
| 3.1-12 | `***bold italic***` → `*_bold italic_*`（不丢失格式） | 单元测试 |
| 3.1-13 | 代码块内的 `**text**` 不被转换（代码保护） | 单元测试 |

---

### 3.2 Abort 检测

#### 3.2.1 问题

用户无法中止正在进行的 AI 回复。

#### 3.2.2 实现

新增 `internal/messaging/slack/abort.go`：

```go
package slack

import "strings"

var abortTriggers = map[string]bool{
    // English
    "stop": true, "abort": true, "halt": true, "cancel": true,
    "wait": true, "exit": true,
    "please stop": true, "stop please": true,
    // Chinese
    "停止": true, "取消": true, "中断": true, "等一下": true,
    "别说了": true, "停下来": true,
}

// IsAbortCommand checks if text is an abort trigger.
func IsAbortCommand(text string) bool {
    t := strings.TrimSpace(strings.ToLower(text))
    t = strings.TrimRight(t, ".!?…,，。;；:!：\"')]")
    return abortTriggers[t]
}
```

**集成点**：在 `handleEventsAPI` 中，去重之后、`HandleTextMessage` 之前：

```go
if IsAbortCommand(text) {
    a.log.Info("slack: abort command received", "channel", channelID)
    // TODO: Phase 2 后续集成 ChatQueue.Abort 或 worker cancel
    return
}
```

#### 3.2.3 验收标准

| ID | AC | 验证方式 |
|----|-----|---------|
| 3.2-1 | "stop" 匹配 | 单元测试 |
| 3.2-2 | "停止" 匹配 | 单元测试 |
| 3.2-3 | "Stop." 匹配（去标点） | 单元测试 |
| 3.2-4 | "please stop" 匹配 | 单元测试 |
| 3.2-5 | "hello" 不匹配 | 单元测试 |
| 3.2-6 | "stop it" 不匹配（非完整匹配） | 单元测试 |
| 3.2-7 | 空字符串不匹配 | 单元测试 |
| 3.2-8 | "STOP"（全大写）匹配 | 单元测试 |
| 3.2-9 | "stop，" 匹配（中文标点） | 单元测试 |

---

### 3.3 状态指示器

#### 3.3.1 问题

用户发送消息后无反馈，不知道 bot 是否在处理。

#### 3.3.2 实现

使用 `slack.Client.AddReactionContext` / `RemoveReactionContext`（已验证存在于 `reactions.go:146,182`），使用 `slack.ItemRef{Channel, Timestamp}`（已验证 `item.go:55-60`）。

在 `adapter.go` 中添加方法（无需新文件）：

```go
const statusEmoji = "eyes"

func (a *Adapter) addStatusReaction(ctx context.Context, channelID, timestamp string) {
    err := a.client.AddReactionContext(ctx, statusEmoji, slack.ItemRef{
        Channel:   channelID,
        Timestamp: timestamp,
    })
    if err != nil {
        a.log.Debug("slack: add reaction failed", "error", err)
    }
}

func (a *Adapter) removeStatusReaction(ctx context.Context, channelID, timestamp string) {
    err := a.client.RemoveReactionContext(ctx, statusEmoji, slack.ItemRef{
        Channel:   channelID,
        Timestamp: timestamp,
    })
    if err != nil {
        a.log.Debug("slack: remove reaction failed", "error", err)
    }
}
```

**SDK-first**：完全使用 SDK 原生 API，零自定义实现。

**集成点**：`handleEventsAPI` 中调用：

```go
a.addStatusReaction(ctx, channelID, msgEvent.TimeStamp)
// ... HandleTextMessage ...
// TODO: 异步回调中 removeStatusReaction
```

> **设计决策**：`removeStatusReaction` 需要在 AI 回复完成后触发。当前 `Bridge.Handle` 是同步的，在 `Handle` 返回后即可移除。后续集成流式输出时需在 `SlackConn.Close` 中触发。

#### 3.3.3 验收标准

| ID | AC | 验证方式 |
|----|-----|---------|
| 3.3-1 | 消息处理开始时用户消息出现 :eyes: reaction | 集成测试 |
| 3.3-2 | 消息处理结束后 :eyes: reaction 被移除 | 集成测试 |
| 3.3-3 | Reaction API 失败不阻断消息处理 | 错误测试 |
| 3.3-4 | DM 中 reaction 正常工作（`Channel` 以 D 开头） | 集成测试 |
| 3.3-5 | 消息无 TimeStamp 时跳过 reaction（不 panic） | 单元测试 |

---

## 4. Phase 3 — 安全

### 4.1 访问控制

#### 4.1.1 问题

当前 `SlackConfig`（`config.go:138-146`）无任何访问控制字段。

**已验证** `adapter.go:109-154`：`handleEventsAPI` 无 gate 检查，任何用户在任何频道都能触发 bot。

#### 4.1.2 配置扩展

```go
// config.go SlackConfig 扩展
type SlackConfig struct {
    // ... existing fields ...
    DMPolicy       string   `mapstructure:"dm_policy"`        // open | allowlist | disabled
    GroupPolicy    string   `mapstructure:"group_policy"`     // open | allowlist | disabled
    RequireMention bool     `mapstructure:"require_mention"`  // group must @bot
    AllowFrom      []string `mapstructure:"allow_from"`       // user_id whitelist
}
```

#### 4.1.3 Gate 实现

新增 `internal/messaging/slack/gate.go`：

```go
package slack

import "sync"  // only needed if allowFrom is dynamically updated in future

type Gate struct {
    dmPolicy       string
    groupPolicy    string
    requireMention bool
    allowFrom      map[string]bool
}

type GateResult struct {
    Allowed bool
    Reason  string
}

func NewGate(dmPolicy, groupPolicy string, requireMention bool, allowFrom []string) *Gate {
    g := &Gate{
        dmPolicy:       dmPolicy,
        groupPolicy:    groupPolicy,
        requireMention: requireMention,
        allowFrom:      make(map[string]bool),
    }
    for _, u := range allowFrom {
        g.allowFrom[u] = true
    }
    return g
}

func (g *Gate) Check(channelType, userID string, botMentioned bool) *GateResult {
    if channelType == "im" {
        switch g.dmPolicy {
        case "disabled":
            return &GateResult{false, "dm_disabled"}
        case "allowlist":
            if !g.allowFrom[userID] {
                return &GateResult{false, "not_in_allowlist"}
            }
        }
        return &GateResult{true, ""}
    }

    // Group/channel
    switch g.groupPolicy {
    case "disabled":
        return &GateResult{false, "group_disabled"}
    case "allowlist":
        if !g.allowFrom[userID] {
            return &GateResult{false, "not_in_allowlist"}
        }
    }
    if g.requireMention && !botMentioned {
        return &GateResult{false, "no_mention"}
    }
    return &GateResult{true, ""}
}
```

**集成点**：`handleEventsAPI` 中，thread ownership 检查之前：

```go
// Access control gate
botMentioned := strings.Contains(msgEvent.Text, "<@"+a.botID+">")
result := a.gate.Check(channelType, userID, botMentioned)
if !result.Allowed {
    a.log.Debug("slack: gate rejected", "reason", result.Reason, "user", userID)
    return
}
```

#### 4.1.4 验收标准

| ID | AC | 验证方式 |
|----|-----|---------|
| 4.1-1 | dm_policy=open 允许所有 DM | 单元测试 |
| 4.1-2 | dm_policy=disabled 拒绝所有 DM | 单元测试 |
| 4.1-3 | dm_policy=allowlist 仅允许白名单 | 单元测试 |
| 4.1-4 | group_policy=open 允许所有群消息 | 单元测试 |
| 4.1-5 | group_policy=disabled 拒绝所有群消息 | 单元测试 |
| 4.1-6 | group_policy=allowlist + 非白名单用户被拒 | 单元测试 |
| 4.1-7 | require_mention=true + 未 @bot → 拒绝 | 单元测试 |
| 4.1-8 | require_mention=true + 已 @bot → 允许 | 单元测试 |
| 4.1-9 | require_mention=false + 未 @bot → 允许 | 单元测试 |
| 4.1-10 | DM 中 require_mention 不生效（DM 总是视为 mentioned） | 单元测试 |
| 4.1-11 | 空配置（默认 open）允许所有消息 | 单元测试 |
| 4.1-12 | gate 被拒时仅 Debug 日志，不发错误消息给用户 | 单元测试 |
| 4.1-13 | MPIM（channelType="mpim"）与 group 策略一致 | 单元测试 |

---

### 4.2 消息过期检查

#### 4.2.1 问题

WebSocket 重连后 Slack 重推积压事件，bot 可能回复数小时前的旧消息。

#### 4.2.2 实现

在 `handleEventsAPI` 中添加时间戳检查：

```go
// Message expiry: skip messages older than 30 minutes
if msgEvent.TimeStamp != "" {
    if ts, err := parseSlackTS(msgEvent.TimeStamp); err == nil {
        if time.Since(ts) > 30*time.Minute {
            a.log.Debug("slack: skipping expired message", "ts", msgEvent.TimeStamp)
            return
        }
    }
}

func parseSlackTS(ts string) (time.Time, error) {
    parts := strings.SplitN(ts, ".", 2)
    sec, err := strconv.ParseInt(parts[0], 10, 64)
    if err != nil {
        return time.Time{}, err
    }
    return time.Unix(sec, 0), nil
}
```

#### 4.2.3 验收标准

| ID | AC | 验证方式 |
|----|-----|---------|
| 4.2-1 | 超过 30 分钟的旧消息被忽略 | 单元测试 |
| 4.2-2 | 30 分钟内的消息正常处理 | 单元测试 |
| 4.2-3 | 时间戳解析失败时不阻断（静默放行） | 单元测试 |
| 4.2-4 | 空 TimeStamp 时不 panic | 单元测试 |

---

## 5. 文件变动清单

### Phase 1 — 消息路由修复

| 文件 | 操作 | 说明 |
|------|------|------|
| `internal/messaging/slack/adapter.go` | 修改 | teamID 字段 + Start() 保存 teamID + HandleTextMessage 增加 threadTS 参数 + 去重 seen-set + bot 防御增强 |
| `internal/messaging/slack/events.go` | 修改 | extractText 扩展 RichTextBlock/ContextBlock 支持 |
| `internal/messaging/slack/mention.go` | 新增 | UserCache + ResolveMentions |
| `internal/messaging/platform_adapter.go` | 修改 | HandleTextMessage 签名增加 threadTS（接口变更） |
| `internal/messaging/feishu/adapter.go` | 修改 | HandleTextMessage 调用处增加 threadTS 参数 |
| `internal/messaging/mock/` | 修改 | Mock adapter HandleTextMessage 签名同步 |
| `internal/messaging/slack/adapter_test.go` | 修改 | 新增 AC 测试 |

### Phase 2 — 用户体验

| 文件 | 操作 | 说明 |
|------|------|------|
| `internal/messaging/slack/format.go` | 新增 | FormatMrkdwn Markdown → mrkdwn |
| `internal/messaging/slack/abort.go` | 新增 | IsAbortCommand |
| `internal/messaging/slack/adapter.go` | 修改 | SlackConn.WriteCtx 集成 FormatMrkdwn + status reaction |

### Phase 3 — 安全

| 文件 | 操作 | 说明 |
|------|------|------|
| `internal/messaging/slack/gate.go` | 新增 | Gate 访问控制 |
| `internal/config/config.go` | 修改 | SlackConfig 增加 DM/Group 策略字段 |
| `configs/config-dev.yaml` | 修改 | 新增 gate 配置项 |

---

## 6. handleEventsAPI 处理流水线（完成后）

基于**实际代码** `adapter.go:109-154` 的改造后流程：

```
runSocketMode (adapter.go:81)
  │
  ├─ socketmode.EventTypeEventsAPI
  │   ├─ Ack(evt.Request)                           // :96
  │   └─ handleEventsAPI(ctx, eventsAPI)             // :97
  │       │
  │       ├─ 1. InnerEvent → MessageEvent            // :110
  │       ├─ 2. Bot 防御 (BotID != "" → skip)        // :115 [增强]
  │       ├─ 3. Subtype 过滤 (join/leave/change → skip)  [新增]
  │       ├─ 4. 消息过期检查 (ts > 30min → skip)      [新增, Phase 3]
  │       ├─ 5. 提取 channelID/threadTS/userID/text   // :120-123
  │       ├─ 6. RichText block 提取                   [增强]
  │       ├─ 7. 用户提及解析 (ResolveMentions)         [新增]
  │       ├─ 8. text 为空 → return                   // :125
  │       ├─ 9. 去重检查 (seen[platformMsgID])         [新增]
  │       ├─ 10. 访问控制 (Gate.Check)                [新增, Phase 3]
  │       ├─ 11. Thread ownership 检查               // :134
  │       ├─ 12. Abort 快速路径 (IsAbortCommand)       [新增, Phase 2]
  │       ├─ 13. Status reaction ON                  [新增, Phase 2]
  │       ├─ 14. HandleTextMessage(teamID, channelID, threadTS, userID, text) [修复]
  │       │       └─ MakeSlackEnvelope(teamID, channelID, threadTS, userID, text) [修复]
  │       │           └─ Bridge.Handle → Session → Worker
  │       │               └─ SlackConn.WriteCtx
  │       │                   └─ FormatMrkdwn(text) → PostMessageContext [Phase 2]
  │       └─ 15. Status reaction OFF                 [新增, Phase 2]
  │
  ├─ socketmode.EventTypeConnecting   // :99
  └─ socketmode.EventTypeConnectionError  // :102
```

---

## 7. 依赖关系

```
Phase 1.1 (teamID+threadTS fix) ──→ Phase 1.4 (mention, needs botID)
Phase 1.2 (dedup)
Phase 1.3 (bot defense)
Phase 1.5 (rich text extract)
         ↓ (Phase 1 完成)
Phase 2.1 (mrkdwn format) ──→ Phase 2.3 (status indicator)
Phase 2.2 (abort detect)
         ↓ (Phase 2 完成)
Phase 3.1 (gate) ──→ Phase 3.2 (message expiry)
```

Phase 1 内部 1.1/1.2/1.3/1.5 可并行开发。Phase 2 依赖 Phase 1（需要 teamID 和 dedup 先就位）。Phase 3.1 可与 Phase 2 并行。

---

## 8. E2E 用户验收测试 (UAT)

测试人员通过 **Slack 桌面/Web 客户端**执行操作，基于**黑盒视角**验证。

### 8.1 核心业务交互 (Happy Paths)

| ID | 场景 | 操作步骤 | 验收标准 |
|----|------|---------|---------|
| **TC-1.1** | DM 基础交互 | 1. 在 Slack App 面板搜索 bot 打开 DM<br>2. 发送 `你好，用加粗和代码块回复` | 1. bot 回复中出现粗体文字<br>2. 代码块正确渲染 |
| **TC-1.2** | 群内 @提及 | 1. 在含 bot 的群中发闲聊（不 @）<br>2. 发送 `@bot 翻译成英文` | 1. 闲聊不被理睬<br>2. @bot 后正确回复，内容无 `<@U123>` 原始 ID |
| **TC-1.3** | Thread 回复 | 1. 在 bot 消息上点「Reply in thread」<br>2. 线程内发送 `补充说明` | bot 回复**出现在同一 thread 内**，不散落到频道主消息 |
| **TC-1.4** | 多人提及 | 1. 群内发送 `@bot 评价 @张三 的周报` | bot 理解为 `评价 @张三 的周报`（`<@U111>` → `@张三`） |
| **TC-1.5** | Rich Text 消息 | 1. 从其他 app 复制带格式的文字粘贴发送给 bot | bot 能提取并处理富文本内容 |
| **TC-1.6** | 多工作区 | 1. workspace A 和 B 同时安装 bot<br>2. 两边同时发消息 | 两边 session 隔离，回复不串台 |

### 8.2 用户体验 (UX)

| ID | 场景 | 操作步骤 | 验收标准 |
|----|------|---------|---------|
| **TC-2.1** | 状态反馈 | 1. 发送需要长时间的问题<br>2. 观察刚发出的消息 | 消息上出现 :eyes: reaction，处理完成后移除 |
| **TC-2.2** | Markdown 渲染 | 1. 让 bot 输出含标题、列表、链接、代码的内容 | 标题粗体、列表圆点、链接可点击、代码块正确 |
| **TC-2.3** | 急速停止 | 1. 让 bot 输出长内容<br>2. 过程中发送 `stop` | bot 停止输出 |

### 8.3 边缘场景与安全 (Edge Cases)

| ID | 场景 | 操作步骤 | 验收标准 |
|----|------|---------|---------|
| **TC-3.1** | 高频防竞态 | 1. 快速连发 5 条不同消息（1 秒内） | 不产生 5 份并行回复，去重后只处理有效消息 |
| **TC-3.2** | Bot 互防 | 1. 群内引入另一个自动回复 bot<br>2. 触发另一个 bot 的回复 | 两个 bot 不形成无限互回复循环 |
| **TC-3.3** | 积压消息重放 | 1. 模拟断连 30+ 分钟后重连 | 超时旧消息被静默忽略 |
| **TC-3.4** | 越权访问 | 1. 配置 allowlist<br>2. 非白名单用户 @bot | bot 已读不回 |
| **TC-3.5** | DM 控制 | 1. 配置 dm_policy=allowlist<br>2. 非白名单用户发 DM | bot 不回复 |
| **TC-3.6** | 自身提及清理 | 1. 群内 `@bot @张三 你好` | bot 收到的文本是 `@张三 你好`（自身提及被移除） |
| **TC-3.7** | 纯 bot mention | 1. 群内仅发送 `@bot`（无其他内容） | 解析后 text 为空，跳过处理，不报错 |
| **TC-3.8** | subtype 消息 | 1. 用户加入/离开频道<br>2. 用户编辑/删除消息 | bot 不响应 join/leave/edit/delete 事件 |
| **TC-3.9** | 未知 block 类型 | 1. 发送含 Slack 新 block 类型的消息（bot 无法解析） | 未知 block 被安全跳过，不 panic |

---

## 9. 开发顺序建议

Phase 1 内部可并行（1.1/1.2/1.3/1.5 独立），Phase 2 依赖 Phase 1（需要 teamID 和 dedup 先就位），Phase 3.1 可与 Phase 2 并行。
