package slack

import "fmt"

// SelectionOption represents an option for selection buttons.
type SelectionOption struct {
	Label string
	Value string
	Emoji string
}

// BuildApprovalBlock creates a block with Approve/Reject buttons.
// Used for: dangerous command approval requests.
func BuildApprovalBlock(requestID, title, description string) []map[string]interface{} {
	var blocks []map[string]interface{}

	// Header
	headerBlock := map[string]interface{}{
		"type": "header",
		"text": plainText("⚠️ Approval Required"),
	}
	blocks = append(blocks, headerBlock)

	// Description section
	sectionBlock := map[string]interface{}{
		"type": "section",
		"text": mrkdwnText(fmt.Sprintf("*%s*\n\n%s", title, description)),
	}
	blocks = append(blocks, sectionBlock)

	// Divider
	blocks = append(blocks, map[string]interface{}{"type": "divider"})

	// Actions with buttons
	actionsBlock := map[string]interface{}{
		"type": "actions",
		"elements": []map[string]interface{}{
			{
				"type":      "button",
				"text":      plainText("✅ Approve"),
				"style":     "primary",
				"action_id": "approve_" + requestID,
				"value":     requestID,
			},
			{
				"type":      "button",
				"text":      plainText("❌ Reject"),
				"style":     "danger",
				"action_id": "reject_" + requestID,
				"value":     requestID,
			},
		},
	}
	blocks = append(blocks, actionsBlock)

	return blocks
}

// BuildSelectionBlock creates a block with selection buttons.
// Used for: multi-choice prompts.
func BuildSelectionBlock(title string, options []SelectionOption) []map[string]interface{} {
	if len(options) == 0 {
		return []map[string]interface{}{}
	}

	var blocks []map[string]interface{}

	// Header
	headerBlock := map[string]interface{}{
		"type": "header",
		"text": plainText(title),
	}
	blocks = append(blocks, headerBlock)

	// Build button elements
	var buttonElements []map[string]interface{}
	for _, opt := range options {
		button := map[string]interface{}{
			"type":      "button",
			"text":      plainText(opt.Label),
			"action_id": "select_" + opt.Value,
			"value":     opt.Value,
		}
		if opt.Emoji != "" {
			button["text"] = map[string]interface{}{
				"type":  "plain_text",
				"text":  opt.Emoji + " " + opt.Label,
				"emoji": true,
			}
		}
		buttonElements = append(buttonElements, button)
	}

	// Actions block
	actionsBlock := map[string]interface{}{
		"type":     "actions",
		"elements": buttonElements,
	}
	blocks = append(blocks, actionsBlock)

	return blocks
}

// BuildConfirmationBlock creates a simple confirmation message.
func BuildConfirmationBlock(title, message string) []map[string]interface{} {
	var blocks []map[string]interface{}

	headerBlock := map[string]interface{}{
		"type": "header",
		"text": plainText(title),
	}
	blocks = append(blocks, headerBlock)

	sectionBlock := map[string]interface{}{
		"type": "section",
		"text": mrkdwnText(message),
	}
	blocks = append(blocks, sectionBlock)

	return blocks
}
