// Package slack provides the Slack adapter implementation for the hotplex engine.
// System message builders for Slack Block Kit.
package slack

import (
	"fmt"
	"time"

	"github.com/hrygo/hotplex/chatapps/base"
	"github.com/slack-go/slack"
)

// SystemMessageBuilder builds system-related Slack messages (System, User, StepStart, StepFinish, Raw, UserMessageReceived)
type SystemMessageBuilder struct{}

// NewSystemMessageBuilder creates a new SystemMessageBuilder
func NewSystemMessageBuilder() *SystemMessageBuilder {
	return &SystemMessageBuilder{}
}

// BuildSystemMessage builds a message for system-level messages
// Implements EventTypeSystem per spec - uses context block for low visual weight
func (b *SystemMessageBuilder) BuildSystemMessage(msg *base.ChatMessage) []slack.Block {
	content := msg.Content
	if content == "" {
		return nil
	}

	// Use context block per spec for low visual weight
	text := slack.NewTextBlockObject("mrkdwn", "System: "+content, false, false)
	return []slack.Block{
		slack.NewContextBlock("", text),
	}
}

// BuildUserMessage builds a message for user message reflection
// Implements EventTypeUser per spec
func (b *SystemMessageBuilder) BuildUserMessage(msg *base.ChatMessage) []slack.Block {
	content := msg.Content
	if content == "" {
		return nil
	}

	// Format timestamp if available
	timestamp := ""
	if !msg.Timestamp.IsZero() {
		timestamp = msg.Timestamp.Format("3:04 PM")
	}

	// Use section + context per spec
	text := "User: " + content
	mrkdwn := slack.NewTextBlockObject("mrkdwn", text, false, false)

	var blocks []slack.Block
	blocks = append(blocks, slack.NewSectionBlock(mrkdwn, nil, nil))

	if timestamp != "" {
		timeObj := slack.NewTextBlockObject("mrkdwn", timestamp, false, false)
		blocks = append(blocks, slack.NewContextBlock("", timeObj))
	}

	return blocks
}

// BuildStepStartMessage builds a single-line compact Context Block for step start
// Format: ▶️ Step {n}/{total}: {content}
func (b *SystemMessageBuilder) BuildStepStartMessage(msg *base.ChatMessage) []slack.Block {
	content := msg.Content
	if content == "" {
		content = "Starting step..."
	}

	stepNum := 1
	totalSteps := 1
	if msg.Metadata != nil {
		if step, ok := msg.Metadata["step"].(int); ok {
			stepNum = step
		}
		if total, ok := msg.Metadata["total"].(int); ok {
			totalSteps = total
		}
	}

	line := fmt.Sprintf("▶️ Step %d/%d: %s", stepNum, totalSteps, content)
	text := slack.NewTextBlockObject("mrkdwn", line, false, false)
	return []slack.Block{slack.NewContextBlock("", text)}
}

// BuildStepFinishMessage builds a single-line compact Context Block for step completion
// Format: ✅ Step {n} 完成 (耗时: {dur})
func (b *SystemMessageBuilder) BuildStepFinishMessage(msg *base.ChatMessage) []slack.Block {
	content := msg.Content
	if content == "" {
		content = "Step completed"
	}

	stepNum := 1
	var durationMs int64
	if msg.Metadata != nil {
		if step, ok := msg.Metadata["step"].(int); ok {
			stepNum = step
		}
		if dur, ok := msg.Metadata["duration_ms"].(int64); ok {
			durationMs = dur
		}
	}

	line := fmt.Sprintf("✅ Step %d 完成: %s", stepNum, content)
	if durationMs > 0 {
		line += "  |  ⏱️ " + FormatDuration(durationMs)
	}
	text := slack.NewTextBlockObject("mrkdwn", line, false, false)
	return []slack.Block{slack.NewContextBlock("", text)}
}

// BuildRawMessage builds a message for raw/unparsed output
// Implements EventTypeRaw per spec - shows only type and length, not content
func (b *SystemMessageBuilder) BuildRawMessage(msg *base.ChatMessage) []slack.Block {
	content := msg.Content
	dataLen := len(content)

	// Format data length
	var dataLenStr string
	if dataLen > 1024*1024 {
		dataLenStr = fmt.Sprintf("%.1fMB", float64(dataLen)/(1024*1024))
	} else if dataLen > 1024 {
		dataLenStr = fmt.Sprintf("%.1fKB", float64(dataLen)/1024)
	} else {
		dataLenStr = fmt.Sprintf("%d bytes", dataLen)
	}

	// Per spec: show only type and length, NOT content
	text := ":page_facing_up: *Raw Output*\nData: " + dataLenStr + " (not displayed)"
	mrkdwn := slack.NewTextBlockObject("mrkdwn", text, false, false)

	return []slack.Block{
		slack.NewSectionBlock(mrkdwn, nil, nil),
	}
}

// BuildUserMessageReceivedMessage builds a message to acknowledge user message receipt
// Implements EventTypeUserMessageReceived per spec (0.6)
// Triggered immediately after user message is received
func (b *SystemMessageBuilder) BuildUserMessageReceivedMessage(msg *base.ChatMessage) []slack.Block {
	// Per spec: context block with :inbox: emoji
	// Very low latency acknowledgment
	text := slack.NewTextBlockObject("mrkdwn", ":inbox: _Message received_", false, false)
	return []slack.Block{
		slack.NewContextBlock("", text),
	}
}

// FormatDuration formats duration for display with smart human-readable output
// Examples: 500ms, 12s, 1m 30s, 30m, 1h 15m
func FormatDuration(durationMs int64) string {
	if durationMs < 1000 {
		return fmt.Sprintf("%dms", durationMs)
	}

	totalSeconds := durationMs / 1000
	hours := totalSeconds / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60

	switch {
	case hours > 0:
		if minutes > 0 {
			return fmt.Sprintf("%dh %dm", hours, minutes)
		}
		return fmt.Sprintf("%dh", hours)
	case minutes > 0:
		if seconds > 0 {
			return fmt.Sprintf("%dm %ds", minutes, seconds)
		}
		return fmt.Sprintf("%dm", minutes)
	default:
		return fmt.Sprintf("%ds", seconds)
	}
}

// TimeToSlackTimestamp converts time.Time to Slack timestamp format
func TimeToSlackTimestamp(t time.Time) string {
	return fmt.Sprintf("%d.%d", t.Unix(), t.Nanosecond()/1000000)
}
