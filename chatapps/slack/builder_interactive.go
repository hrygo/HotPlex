// Package slack provides the Slack adapter implementation for the hotplex engine.
// Interactive message builders for Slack Block Kit.
package slack

import (
	"fmt"
	"strings"

	"github.com/hrygo/hotplex/chatapps/base"
	"github.com/slack-go/slack"
)

// InteractiveMessageBuilder builds interactive Slack messages (DangerBlock, PermissionRequest)
type InteractiveMessageBuilder struct{}

// NewInteractiveMessageBuilder creates a new InteractiveMessageBuilder
func NewInteractiveMessageBuilder() *InteractiveMessageBuilder {
	return &InteractiveMessageBuilder{}
}

// BuildDangerBlockMessage builds a high-fidelity warning card for dangerous operations
// Implements the Safety Layer spec: header + quoted reason + danger-styled action buttons
func (b *InteractiveMessageBuilder) BuildDangerBlockMessage(msg *base.ChatMessage) []slack.Block {
	content := msg.Content
	if content == "" {
		content = "此操作需要您的确认"
	}

	// Extract session_id from metadata for button values
	sessionID := ""
	if msg.Metadata != nil {
		if sid, ok := msg.Metadata["session_id"].(string); ok {
			sessionID = sid
		}
	}

	// Header: high-alert visual weight
	headerText := slack.NewTextBlockObject("plain_text", "🚨 高危操作拦截", false, false)
	header := slack.NewHeaderBlock(headerText)

	// Quoted reason for context
	quoteContent := ""
	for _, line := range strings.Split(content, "\n") {
		quoteContent += "> " + line + "\n"
	}
	reasonText := slack.NewTextBlockObject("mrkdwn", quoteContent, false, false)
	reasonSection := slack.NewSectionBlock(reasonText, nil, nil)

	// Add confirm/cancel buttons with sessionID in value
	confirmValue := "confirm"
	cancelValue := "cancel"
	if sessionID != "" {
		confirmValue = "confirm:" + sessionID
		cancelValue = "cancel:" + sessionID
	}

	confirmBtn := slack.NewButtonBlockElement("danger_confirm", confirmValue,
		slack.NewTextBlockObject("plain_text", "⚠️ 确认执行", false, false))
	confirmBtn.Style = "danger"

	cancelBtn := slack.NewButtonBlockElement("danger_cancel", cancelValue,
		slack.NewTextBlockObject("plain_text", "🛑 取消", false, false))

	actionBlock := slack.NewActionBlock("danger_actions", confirmBtn, cancelBtn)

	return []slack.Block{
		header,
		reasonSection,
		slack.NewDividerBlock(),
		actionBlock,
	}
}

// BuildPermissionRequestMessageFromChat builds Slack blocks for a permission request from ChatMessage
// This is the main entry point for the Build() switch statement
// Implements EventTypePermissionRequest per spec (7)
func (b *InteractiveMessageBuilder) BuildPermissionRequestMessageFromChat(msg *base.ChatMessage) []slack.Block {
	// Extract data from metadata
	var tool, input, messageID, sessionID string
	var reason string

	if msg.Metadata != nil {
		if t, ok := msg.Metadata["tool_name"].(string); ok {
			tool = t
		}
		if i, ok := msg.Metadata["input"].(string); ok {
			input = i
		}
		if m, ok := msg.Metadata["message_id"].(string); ok {
			messageID = m
		}
		if s, ok := msg.Metadata["session_id"].(string); ok {
			sessionID = s
		}
		if r, ok := msg.Metadata["reason"].(string); ok {
			reason = r
		}
	}

	// Sanitize and truncate commands for preview
	safeInput := SanitizeCommand(input)
	displayInput := safeInput
	if base.RuneCount(displayInput) > 500 {
		displayInput = base.TruncateWithEllipsis(displayInput, 500)
	}

	var blocks []slack.Block

	// Header - per spec: header block
	headerText := slack.NewTextBlockObject("plain_text", ":warning: Permission Request", false, false)
	blocks = append(blocks, slack.NewHeaderBlock(headerText))

	// Tool information - per spec: section
	if tool != "" {
		toolText := slack.NewTextBlockObject("mrkdwn", fmt.Sprintf("Tool: `%s`", tool), false, false)
		blocks = append(blocks, slack.NewSectionBlock(toolText, nil, nil))
	}

	// Command/Action preview - per spec: section
	if displayInput != "" {
		cmdText := slack.NewTextBlockObject("mrkdwn", fmt.Sprintf("Command:\n```\n%s\n```", displayInput), false, false)
		blocks = append(blocks, slack.NewSectionBlock(cmdText, nil, nil))
	}

	// Decision reason (if available)
	if reason != "" {
		reasonText := slack.NewTextBlockObject("mrkdwn", fmt.Sprintf("Reason: %s", reason), false, false)
		blocks = append(blocks, slack.NewContextBlock("", []slack.MixedElement{
			reasonText,
		}...))
	}

	// Session info
	if sessionID != "" {
		displayID := sessionID
		if len(displayID) > 8 {
			displayID = displayID[:8]
		}
		sessionText := slack.NewTextBlockObject("mrkdwn", fmt.Sprintf("Session: %s", displayID), false, false)
		blocks = append(blocks, slack.NewContextBlock("", []slack.MixedElement{
			sessionText,
		}...))
	}

	// Action buttons - per spec: actions
	// action_id format per spec: perm_allow:{sessionID}:{messageID}
	blockID := ValidateBlockID(fmt.Sprintf("perm_%s", messageID))

	// Per spec, action_id should include sessionID and messageID
	approveActionID := fmt.Sprintf("perm_allow:%s:%s", sessionID, messageID)
	denyActionID := fmt.Sprintf("perm_deny:%s:%s", sessionID, messageID)

	approveBtn := slack.NewButtonBlockElement(approveActionID, "allow",
		slack.NewTextBlockObject("plain_text", "✅ Allow", false, false))
	approveBtn.Style = "primary"

	denyBtn := slack.NewButtonBlockElement(denyActionID, "deny",
		slack.NewTextBlockObject("plain_text", "🚫 Deny", false, false))
	denyBtn.Style = "danger"

	blocks = append(blocks, slack.NewActionBlock(blockID, approveBtn, denyBtn))

	return blocks
}
