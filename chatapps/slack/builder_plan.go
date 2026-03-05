// Package slack provides the Slack adapter implementation for the hotplex engine.
// Plan mode message builders for Slack Block Kit.
package slack

import (
	"fmt"
	"strings"

	"github.com/hrygo/hotplex/chatapps/base"
	"github.com/slack-go/slack"
)

// PlanMessageBuilder builds plan-related Slack messages (PlanMode, ExitPlanMode)
type PlanMessageBuilder struct{}

// NewPlanMessageBuilder creates a new PlanMessageBuilder
func NewPlanMessageBuilder() *PlanMessageBuilder {
	return &PlanMessageBuilder{}
}

// BuildPlanModeMessage builds a message for plan mode
// Implements EventTypePlanMode per spec - uses context block for low visual weight
func (b *PlanMessageBuilder) BuildPlanModeMessage(msg *base.ChatMessage) []slack.Block {
	content := msg.Content
	if content == "" {
		content = "Generating plan..."
	}

	// Format plan content as a quote block for better structure and Geek Transparency
	quoteContent := ""
	for _, line := range strings.Split(content, "\n") {
		quoteContent += "> " + line + "\n"
	}

	text := slack.NewTextBlockObject("mrkdwn", ":memo: *推演计划中...*\n\n"+quoteContent, false, false)
	return []slack.Block{
		slack.NewSectionBlock(text, nil, nil),
	}
}

// BuildExitPlanModeMessage builds a message for exit plan mode
// Implements EventTypeExitPlanMode per spec (15)
// Block type: header + section + divider + actions
func (b *PlanMessageBuilder) BuildExitPlanModeMessage(msg *base.ChatMessage) []slack.Block {
	content := msg.Content
	if content == "" {
		content = "Plan generated. Waiting for approval."
	}

	// Format plan content as a quote block for better structure and Geek Transparency
	quoteContent := ""
	for _, line := range strings.Split(content, "\n") {
		quoteContent += "> " + line + "\n"
	}

	// Extract session_id from metadata for button values
	sessionID := ""
	if msg.Metadata != nil {
		if sid, ok := msg.Metadata["session_id"].(string); ok {
			sessionID = sid
		}
	}

	// Per spec: header block with clipboard emoji
	headerText := slack.NewTextBlockObject("plain_text", "📝 作战计划已就绪", false, false)
	header := slack.NewHeaderBlock(headerText)

	// Section with quoted plan content
	sectionText := slack.NewTextBlockObject("mrkdwn", quoteContent, false, false)
	section := slack.NewSectionBlock(sectionText, nil, nil)

	// Add approve/deny buttons with sessionID in value
	// Format: approve:{sessionID} or deny:{sessionID}
	approveValue := "approve"
	denyValue := "deny"
	if sessionID != "" {
		approveValue = "approve:" + sessionID
		denyValue = "deny:" + sessionID
	}

	approveBtn := slack.NewButtonBlockElement("plan_approve", approveValue,
		slack.NewTextBlockObject("plain_text", "Approve", false, false))
	approveBtn.Style = "primary"

	denyBtn := slack.NewButtonBlockElement("plan_deny", denyValue,
		slack.NewTextBlockObject("plain_text", "Deny", false, false))
	denyBtn.Style = "danger"

	actionBlock := slack.NewActionBlock("plan_actions", approveBtn, denyBtn)

	// Per spec: header + section + divider + actions
	return []slack.Block{
		header,
		section,
		slack.NewDividerBlock(),
		actionBlock,
	}
}

// BuildAskUserQuestionMessage builds a message for user questions
func (b *PlanMessageBuilder) BuildAskUserQuestionMessage(msg *base.ChatMessage) []slack.Block {
	question := msg.Content
	if question == "" {
		question = "Please provide more information."
	}

	// Extract session_id from metadata for button values
	sessionID := ""
	if msg.Metadata != nil {
		if sid, ok := msg.Metadata["session_id"].(string); ok {
			sessionID = sid
		}
	}

	text := ":question: *Question*\n" + question
	mrkdwn := slack.NewTextBlockObject("mrkdwn", text, false, false)

	blocks := []slack.Block{
		slack.NewSectionBlock(mrkdwn, nil, nil),
	}

	// Add options as buttons if available in metadata
	if msg.Metadata != nil {
		if options, ok := msg.Metadata["options"].([]string); ok && len(options) > 0 {
			var buttons []slack.BlockElement
			for i, option := range options {
				// Include sessionID in value: option_index:sessionID:option_text
				value := fmt.Sprintf("%d", i)
				if sessionID != "" {
					value = fmt.Sprintf("%d:%s:%s", i, sessionID, option)
				}
				btn := slack.NewButtonBlockElement(fmt.Sprintf("question_option_%d", i), value,
					slack.NewTextBlockObject("plain_text", option, false, false))
				buttons = append(buttons, btn)
			}
			if len(buttons) > 0 {
				blocks = append(blocks, slack.NewActionBlock("question_options", buttons...))
			}
		}
	}

	return blocks
}
