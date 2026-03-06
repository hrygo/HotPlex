# MultiBot Mention Routing Implementation Plan

> **Status:** ✅ COMPLETED (2026-03-06)

**Goal:** Implement @ routing for multiple hotplex bots in one Slack channel - only respond when @'d, or broadcast with polite response when no @ present.

**Architecture:** Add `multibot` option to `GroupPolicy`. Extract mentioned users from message text via regex `<@USER_ID>`. Check if self is in mention list. For broadcast (no @), send polite response via BroadcastResponder interface.

**Tech Stack:** Go 1.25, slack-go SDK, regexp

---

## Commits

1. `dfb7f3d` - feat(slack): add ExtractMentionedUsers and ShouldRespondInMultibotMode helpers
2. `4acbb2d` - feat(slack): add multibot filter in HTTP mode (events.go)
3. `eb23de1` - feat(slack): add multibot filter in Socket Mode (socketmode.go)
4. `b4a626c` - feat(slack): add BroadcastResponder interface for multibot broadcast mode
5. `f879eec` - feat(slack): integrate BroadcastResponder for multibot broadcast messages

---

## Implementation Summary

### New Files
- `chatapps/slack/broadcast_responder.go` - BroadcastResponder interface and StaticBroadcastResponder
- `chatapps/slack/broadcast_responder_test.go` - Unit tests

### Modified Files
- `chatapps/slack/config.go` - Added multibot helpers and BroadcastResponder integration
- `chatapps/slack/config_test.go` - Added tests for new functions
- `chatapps/slack/events.go` - Added multibot filter and broadcast response
- `chatapps/slack/socketmode.go` - Added multibot filter and broadcast response

---

## Usage

```yaml
# In your slack adapter config
group_policy: multibot
bot_user_id: "U1234567890"  # Your bot's user ID

# Optional: custom broadcast response
# broadcast_response: "Hello! How can I help?"
```

### Behavior

| Message | @BotA? | @BotB? | BotA Response | BotB Response |
|---------|--------|--------|---------------|---------------|
| `hello` | No | No | Polite response | Polite response |
| `@BotA help` | Yes | No | Process normally | Ignore |
| `@BotB help` | No | Yes | Ignore | Process normally |
| `@BotA @BotB hi` | Yes | Yes | Process normally | Process normally |

---

## Future Integration

The BroadcastResponder interface allows future integration with native brain:

```go
// Example: Brain-powered responder
type BrainBroadcastResponder struct {
    brain brain.Brain
}

func (r *BrainBroadcastResponder) Respond(ctx context.Context, userMessage string) (string, error) {
    prompt := fmt.Sprintf("Generate a brief, friendly greeting for: %s", userMessage)
    return r.brain.Chat(ctx, prompt)
}
```

## Task 1: Add Helper Functions in config.go

**Files:**
- Modify: `chatapps/slack/config.go`
- Test: `chatapps/slack/config_test.go`

**Step 1: Write the failing tests**

Add to `chatapps/slack/config_test.go`:

```go
func TestExtractMentionedUsers(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected []string
	}{
		{
			name:     "no mentions",
			text:     "hello world",
			expected: nil,
		},
		{
			name:     "single mention",
			text:     "<@U1234567890> hello",
			expected: []string{"U1234567890"},
		},
		{
			name:     "multiple mentions",
			text:     "<@U1111111111> <@U2222222222> hi",
			expected: []string{"U1111111111", "U2222222222"},
		},
		{
			name:     "mention with bang prefix",
			text:     "<@!U1234567890> hello",
			expected: []string{"U1234567890"},
		},
		{
			name:     "mixed mentions",
			text:     "<@U1111111111> <@!U2222222222> hi",
			expected: []string{"U1111111111", "U2222222222"},
		},
		{
			name:     "duplicate mentions",
			text:     "<@U1234567890> <@U1234567890> hi",
			expected: []string{"U1234567890", "U1234567890"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractMentionedUsers(tt.text)
			if !equalSlices(result, tt.expected) {
				t.Errorf("ExtractMentionedUsers() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestShouldRespondInMultibotMode(t *testing.T) {
	cfg := &Config{BotUserID: "U9999999999"}

	tests := []struct {
		name     string
		text     string
		expected bool
	}{
		{
			name:     "no mentions - broadcast",
			text:     "hello world",
			expected: true,
		},
		{
			name:     "mentioned self",
			text:     "<@U9999999999> help me",
			expected: true,
		},
		{
			name:     "mentioned self with bang",
			text:     "<@!U9999999999> help me",
			expected: true,
		},
		{
			name:     "mentioned other bot",
			text:     "<@U8888888888> help me",
			expected: false,
		},
		{
			name:     "mentioned multiple including self",
			text:     "<@U8888888888> <@U9999999999> help",
			expected: true,
		},
		{
			name:     "mentioned multiple excluding self",
			text:     "<@U7777777777> <@U8888888888> help",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cfg.ShouldRespondInMultibotMode(tt.text)
			if result != tt.expected {
				t.Errorf("ShouldRespondInMultibotMode() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
```

**Step 2: Run tests to verify they fail**

```bash
cd /Users/huangzhonghui/HotPlex && go test ./chatapps/slack/... -run "TestExtractMentionedUsers|TestShouldRespondInMultibotMode" -v
```

Expected: FAIL with "undefined: ExtractMentionedUsers"

**Step 3: Implement helper functions**

Add to `chatapps/slack/config.go` (after `ContainsBotMention` function):

```go
// mentionUserRegex matches <@USERID> or <@!USERID> format
var mentionUserRegex = regexp.MustCompile(`<@!?([A-Z][A-Z0-9]+)>`)

// ExtractMentionedUsers extracts all mentioned user IDs from message text.
// Slack mention format: <@U1234567890> or <@!U1234567890>
func ExtractMentionedUsers(text string) []string {
	matches := mentionUserRegex.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil
	}
	users := make([]string, 0, len(matches))
	for _, m := range matches {
		if len(m) > 1 {
			users = append(users, m[1])
		}
	}
	return users
}

// ShouldRespondInMultibotMode determines if this bot should respond in multibot mode.
// Returns true if:
// - No mentions in message (broadcast mode - all bots respond)
// - Bot is explicitly mentioned
// Returns false if:
// - Other bots are mentioned but not this one
func (c *Config) ShouldRespondInMultibotMode(text string) bool {
	mentioned := ExtractMentionedUsers(text)
	if len(mentioned) == 0 {
		return true // Broadcast: no @ means all bots respond
	}
	// Check if we are in the mention list
	for _, userID := range mentioned {
		if userID == c.BotUserID {
			return true
		}
	}
	return false
}
```

**Step 4: Run tests to verify they pass**

```bash
cd /Users/huangzhonghui/HotPlex && go test ./chatapps/slack/... -run "TestExtractMentionedUsers|TestShouldRespondInMultibotMode" -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add chatapps/slack/config.go chatapps/slack/config_test.go
git commit -m "feat(slack): add ExtractMentionedUsers and ShouldRespondInMultibotMode helpers"
```

---

## Task 2: Add multibot Filter in HTTP Mode (events.go)

**Files:**
- Modify: `chatapps/slack/events.go:144-153`

**Step 1: Write the failing test**

Add to `chatapps/slack/adapter_test.go`:

```go
func TestHandleEventCallback_MultibotMode(t *testing.T) {
	tests := []struct {
		name        string
		botUserID   string
		messageText string
		shouldProcess bool
	}{
		{
			name:        "no mention - broadcast",
			botUserID:   "U9999999999",
			messageText: "hello everyone",
			shouldProcess: true,
		},
		{
			name:        "mentioned self",
			botUserID:   "U9999999999",
			messageText: "<@U9999999999> help",
			shouldProcess: true,
		},
		{
			name:        "mentioned other - skip",
			botUserID:   "U9999999999",
			messageText: "<@U8888888888> help",
			shouldProcess: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				BotToken:    "xoxb-123456-789012-abcdef",
				SigningSecret: "test-secret-12345678901234567890",
				BotUserID:   tt.botUserID,
				GroupPolicy: "multibot",
			}

			result := cfg.ShouldRespondInMultibotMode(tt.messageText)
			if result != tt.shouldProcess {
				t.Errorf("ShouldRespondInMultibotMode() = %v, want %v", result, tt.shouldProcess)
			}
		})
	}
}
```

**Step 2: Run test**

```bash
cd /Users/huangzhonghui/HotPlex && go test ./chatapps/slack/... -run "TestHandleEventCallback_MultibotMode" -v
```

Expected: PASS (uses existing helper)

**Step 3: Add multibot filter in events.go**

In `handleEventCallback` function, after the existing `GroupPolicy == "mention"` check (around line 144-153), add:

```go
	// Group policy check: if GroupPolicy is "mention", only process messages that mention the bot
	// Note: HTTP mode does not receive app_mention events, so we must handle mentions here
	if msgEvent.ChannelType == "channel" || msgEvent.ChannelType == "group" {
		if a.config.GroupPolicy == "mention" {
			if !a.config.ContainsBotMention(msgEvent.Text) {
				telemetry.GetMetrics().IncSlackPermissionBlockedMention()
				a.Logger().Debug("Message ignored - bot not mentioned", "channel_type", msgEvent.ChannelType, "policy", "mention")
				return
			}
			// Bot is mentioned in channel/group - process this message
		}
		// Multibot mode: respond if no mentions (broadcast) or mentioned self
		if a.config.GroupPolicy == "multibot" {
			if !a.config.ShouldRespondInMultibotMode(msgEvent.Text) {
				a.Logger().Debug("Message ignored - other bot mentioned", "channel_type", msgEvent.ChannelType, "policy", "multibot")
				return
			}
		}
	}
```

**Step 4: Verify build**

```bash
cd /Users/huangzhonghui/HotPlex && go build ./...
```

Expected: Success

**Step 5: Commit**

```bash
git add chatapps/slack/events.go
git commit -m "feat(slack): add multibot filter in HTTP mode (events.go)"
```

---

## Task 3: Add multibot Filter in Socket Mode (socketmode.go)

**Files:**
- Modify: `chatapps/slack/socketmode.go:187-197`

**Step 1: Add multibot filter in socketmode.go**

In `handleSocketModeMessageEvent` function, after the existing `GroupPolicy == "mention"` check (around line 187-197), add:

```go
	// Group policy check: if GroupPolicy is "mention", only process messages that mention the bot
	// Note: app_mention events are handled separately by handleAppMentionEvent
	if (ev.ChannelType == "channel" || ev.ChannelType == "group") && a.config.GroupPolicy == "mention" {
		if !a.config.ContainsBotMention(ev.Text) {
			a.Logger().Debug("Message ignored - bot not mentioned", "channel_type", ev.ChannelType, "policy", "mention")
			return
		}
		// Bot is mentioned - skip here, let app_mention handler process it
		a.Logger().Debug("Skipping 'message' event with mention (handled by 'app_mention')", "ts", ev.TimeStamp)
		return
	}

	// Multibot mode: respond if no mentions (broadcast) or mentioned self
	if (ev.ChannelType == "channel" || ev.ChannelType == "group") && a.config.GroupPolicy == "multibot" {
		if !a.config.ShouldRespondInMultibotMode(ev.Text) {
			a.Logger().Debug("Message ignored - other bot mentioned", "channel_type", ev.ChannelType, "policy", "multibot")
			return
		}
	}
```

**Step 2: Verify build and run tests**

```bash
cd /Users/huangzhonghui/HotPlex && go build ./... && go test ./chatapps/slack/... -v
```

Expected: Success + PASS

**Step 3: Commit**

```bash
git add chatapps/slack/socketmode.go
git commit -m "feat(slack): add multibot filter in Socket Mode (socketmode.go)"
```

---

## Task 4: Update Config Docs and Validation

**Files:**
- Modify: `chatapps/slack/config.go` (comments)

**Step 1: Update GroupPolicy comment**

Update the GroupPolicy field comment in `chatapps/slack/config.go`:

```go
	// Permission Policy for Group Messages
	// "allow" - Allow all group messages (default)
	// "mention" - Only allow when bot is mentioned
	// "multibot" - Multi-bot routing: broadcast if no @, respond only if @self
	// "block" - Block all group messages
	GroupPolicy string
```

**Step 2: Verify build**

```bash
cd /Users/huangzhonghui/HotPlex && go build ./...
```

**Step 3: Commit**

```bash
git add chatapps/slack/config.go
git commit -m "docs(slack): document multibot GroupPolicy option"
```

---

## Task 5: Final Verification

**Step 1: Run all tests**

```bash
cd /Users/huangzhonghui/HotPlex && go test ./... -v
```

Expected: All PASS

**Step 2: Run race detector**

```bash
cd /Users/huangzhonghui/HotPlex && go test -race ./chatapps/...
```

Expected: PASS, no race conditions

**Step 3: Run linter**

```bash
cd /Users/huangzhonghui/HotPlex && make lint
```

Expected: No errors

---

## Task 6: Add BroadcastResponder Interface

**Files:**
- Create: `chatapps/slack/broadcast_responder.go`
- Test: `chatapps/slack/broadcast_responder_test.go`

**Step 1: Write the failing test**

Create `chatapps/slack/broadcast_responder_test.go`:

```go
package slack

import (
	"context"
	"testing"
)

func TestStaticBroadcastResponder(t *testing.T) {
	responder := NewStaticBroadcastResponder("Hello! How can I help you?")

	tests := []struct {
		name     string
		ctx      context.Context
		userMsg  string
		expected string
	}{
		{
			name:     "basic response",
			ctx:      context.Background(),
			userMsg:  "hello",
			expected: "Hello! How can I help you?",
		},
		{
			name:     "empty message",
			ctx:      context.Background(),
			userMsg:  "",
			expected: "Hello! How can I help you?",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := responder.Respond(tt.ctx, tt.userMsg)
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
			if result != tt.expected {
				t.Errorf("Respond() = %q, want %q", result, tt.expected)
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

```bash
cd /Users/huangzhonghui/HotPlex && go test ./chatapps/slack/... -run "TestStaticBroadcastResponder" -v
```

Expected: FAIL with "undefined: NewStaticBroadcastResponder"

**Step 3: Create interface and implementation**

Create `chatapps/slack/broadcast_responder.go`:

```go
package slack

import (
	"context"
)

// BroadcastResponder generates a polite response for broadcast messages
// (messages without explicit @ mentions in multibot mode).
// This allows for future integration with native brain for intelligent responses.
type BroadcastResponder interface {
	// Respond generates a response for the given user message.
	// In broadcast mode, this is called when no bot is explicitly mentioned.
	Respond(ctx context.Context, userMessage string) (string, error)
}

// StaticBroadcastResponder is a simple implementation that returns a fixed response.
// Use this for basic deployments or as a fallback.
type StaticBroadcastResponder struct {
	response string
}

// NewStaticBroadcastResponder creates a responder with a fixed response.
func NewStaticBroadcastResponder(response string) *StaticBroadcastResponder {
	return &StaticBroadcastResponder{response: response}
}

// Respond returns the configured static response.
func (r *StaticBroadcastResponder) Respond(_ context.Context, _ string) (string, error) {
	return r.response, nil
}

// DefaultBroadcastResponse is the default polite response for broadcast messages.
const DefaultBroadcastResponse = "Hello! I'm ready to help. Please @mention me if you'd like me to respond specifically to you."
```

**Step 4: Run test to verify it passes**

```bash
cd /Users/huangzhonghui/HotPlex && go test ./chatapps/slack/... -run "TestStaticBroadcastResponder" -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add chatapps/slack/broadcast_responder.go chatapps/slack/broadcast_responder_test.go
git commit -m "feat(slack): add BroadcastResponder interface for multibot broadcast mode"
```

---

## Task 7: Integrate BroadcastResponder in Config and Events

**Files:**
- Modify: `chatapps/slack/config.go`
- Modify: `chatapps/slack/events.go`
- Modify: `chatapps/slack/socketmode.go`

**Step 1: Add BroadcastResponder to Config**

Add to `chatapps/slack/config.go`:

```go
// BroadcastResponder generates responses for broadcast messages (no @ mention).
// If nil, uses DefaultBroadcastResponse.
BroadcastResponder BroadcastResponder

// BroadcastResponse is a convenience setter for static response text.
// Creates a StaticBroadcastResponder internally.
func (c *Config) SetBroadcastResponse(text string) {
	c.BroadcastResponder = NewStaticBroadcastResponder(text)
}

// GetBroadcastResponse returns the response for broadcast messages.
func (c *Config) GetBroadcastResponse(ctx context.Context, userMessage string) string {
	if c.BroadcastResponder == nil {
		return DefaultBroadcastResponse
	}
	resp, err := c.BroadcastResponder.Respond(ctx, userMessage)
	if err != nil {
		return DefaultBroadcastResponse
	}
	return resp
}
```

**Step 2: Add helper to check if message is broadcast**

Add to `chatapps/slack/config.go`:

```go
// IsBroadcastMessage returns true if this is a broadcast message (no @ mentions).
// Only meaningful in multibot mode.
func (c *Config) IsBroadcastMessage(text string) bool {
	return len(ExtractMentionedUsers(text)) == 0
}
```

**Step 3: Modify events.go to send broadcast response**

In `handleEventCallback`, modify the multibot block to check for broadcast:

```go
// Multibot mode: respond if no mentions (broadcast) or mentioned self
if a.config.GroupPolicy == "multibot" {
	if !a.config.ShouldRespondInMultibotMode(msgEvent.Text) {
		a.Logger().Debug("Message ignored - other bot mentioned", "channel_type", msgEvent.ChannelType, "policy", "multibot")
		return
	}
	// If broadcast (no @), send polite response instead of processing
	if a.config.IsBroadcastMessage(msgEvent.Text) {
		a.Logger().Debug("Broadcast message - sending polite response", "channel", msgEvent.Channel)
		response := a.config.GetBroadcastResponse(r.Context(), msgEvent.Text)
		// Send directly to channel (bypass engine)
		_ = a.SendToChannel(r.Context(), msgEvent.Channel, response, threadID)
		return
	}
}
```

**Step 4: Modify socketmode.go similarly**

In `handleSocketModeMessageEvent`, add the same broadcast handling:

```go
// Multibot mode: respond if no mentions (broadcast) or mentioned self
if (ev.ChannelType == "channel" || ev.ChannelType == "group") && a.config.GroupPolicy == "multibot" {
	if !a.config.ShouldRespondInMultibotMode(ev.Text) {
		a.Logger().Debug("Message ignored - other bot mentioned", "channel_type", ev.ChannelType, "policy", "multibot")
		return
	}
	// If broadcast (no @), send polite response instead of processing
	if a.config.IsBroadcastMessage(ev.Text) {
		a.Logger().Debug("Broadcast message - sending polite response", "channel", ev.Channel)
		response := a.config.GetBroadcastResponse(a.socketModeCtx, ev.Text)
		_ = a.SendToChannel(a.socketModeCtx, ev.Channel, response, threadID)
		return
	}
}
```

**Step 5: Verify build**

```bash
cd /Users/huangzhonghui/HotPlex && go build ./...
```

Expected: Success

**Step 6: Commit**

```bash
git add chatapps/slack/config.go chatapps/slack/events.go chatapps/slack/socketmode.go
git commit -m "feat(slack): integrate BroadcastResponder for multibot broadcast messages"
```

---

## Task 8: Final Verification

**Step 1: Run all tests**

```bash
cd /Users/huangzhonghui/HotPlex && go test ./... -v
```

Expected: All PASS

**Step 2: Run race detector**

```bash
cd /Users/huangzhonghui/HotPlex && go test -race ./chatapps/...
```

Expected: PASS, no race conditions

**Step 3: Run linter**

```bash
cd /Users/huangzhonghui/HotPlex && make lint
```

Expected: No errors

---

## Summary

| Task | Description | Files |
|------|-------------|-------|
| 1 | Add helper functions + tests | config.go, config_test.go |
| 2 | Add multibot filter (HTTP) | events.go |
| 3 | Add multibot filter (Socket) | socketmode.go |
| 4 | Update docs | config.go |
| 5 | Final verification | - |
| 6 | Add BroadcastResponder interface | broadcast_responder.go, broadcast_responder_test.go |
| 7 | Integrate BroadcastResponder | config.go, events.go, socketmode.go |
| 8 | Final verification | - |
