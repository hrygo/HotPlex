# MultiBot Mention Routing Design

## Problem

Multiple hotplex instances in one Slack channel need @ routing:
- `@BotA` → BotA responds, BotB ignores
- `@BotA @BotB` → Both respond
- No @ → All bots respond (broadcast)

## Solution

Add `multibot` to `GroupPolicy` enum.

## Changes

### 1. config.go - New Policy

```go
// GroupPolicy options:
// "allow" - All group messages
// "mention" - Only when bot is mentioned
// "block" - Block all group messages
// "multibot" - Multi-bot routing (NEW)
```

### 2. config.go - Helper Functions

```go
// ExtractMentionedUsers extracts all @user IDs from message text
func ExtractMentionedUsers(text string) []string

// ShouldRespondInMultibotMode returns true if this bot should respond
func (c *Config) ShouldRespondInMultibotMode(text string) bool
```

### 3. events.go - Filter Logic

In `handleEventCallback`, add multibot check:

```go
if a.config.GroupPolicy == "multibot" {
    if !a.config.ShouldRespondInMultibotMode(msgEvent.Text) {
        return // Not @me, skip
    }
}
```

### 4. socketmode.go - Same Filter

Same logic in `handleSocketModeMessageEvent`.

## Logic Table

| Message | @BotA? | @BotB? | BotA | BotB |
|---------|--------|--------|------|------|
| `hello` | No | No | Respond | Respond |
| `@BotA hello` | Yes | No | Respond | Skip |
| `@BotB hello` | No | Yes | Skip | Respond |
| `@BotA @BotB hi` | Yes | Yes | Respond | Respond |

## Files to Modify

1. `chatapps/slack/config.go` - Add `ExtractMentionedUsers`, `ShouldRespondInMultibotMode`
2. `chatapps/slack/events.go` - Add multibot filter in `handleEventCallback`
3. `chatapps/slack/socketmode.go` - Add multibot filter in `handleSocketModeMessageEvent`
4. `chatapps/slack/config_test.go` - Unit tests

## Testing

1. Unit test for `ExtractMentionedUsers`
2. Unit test for `ShouldRespondInMultibotMode`
3. Integration test with multiple bot configs
