package slack

import (
	"fmt"
	"strings"
	"time"

	"github.com/hrygo/hotplex/chatapps/base"
	"github.com/hrygo/hotplex/provider"
	"github.com/slack-go/slack"
)

// MessageBuilder builds Slack-specific messages from platform-agnostic ChatMessage
type MessageBuilder struct {
	formatter *MrkdwnFormatter
}

// NewMessageBuilder creates a new MessageBuilder
func NewMessageBuilder() *MessageBuilder {
	return &MessageBuilder{
		formatter: NewMrkdwnFormatter(),
	}
}

// Build builds Slack blocks from a ChatMessage based on its type
func (b *MessageBuilder) Build(msg *base.ChatMessage) []slack.Block {
	switch msg.Type {
	case base.MessageTypeThinking:
		return b.BuildThinkingMessage(msg)
	case base.MessageTypeToolUse:
		return b.BuildToolUseMessage(msg)
	case base.MessageTypeToolResult:
		return b.BuildToolResultMessage(msg)
	case base.MessageTypeAnswer:
		return b.BuildAnswerMessage(msg)
	case base.MessageTypeError:
		return b.BuildErrorMessage(msg)
	case base.MessageTypePlanMode:
		return b.BuildPlanModeMessage(msg)
	case base.MessageTypeExitPlanMode:
		return b.BuildExitPlanModeMessage(msg)
	case base.MessageTypeAskUserQuestion:
		return b.BuildAskUserQuestionMessage(msg)
	case base.MessageTypeDangerBlock:
		return b.BuildDangerBlockMessage(msg)
	case base.MessageTypeSessionStats:
		return b.BuildSessionStatsMessage(msg)
	case base.MessageTypeCommandProgress:
		return b.BuildCommandProgressMessage(msg)
	case base.MessageTypeCommandComplete:
		return b.BuildCommandCompleteMessage(msg)
	case base.MessageTypeSystem:
		return b.BuildSystemMessage(msg)
	case base.MessageTypeUser:
		return b.BuildUserMessage(msg)
	case base.MessageTypeStepStart:
		return b.BuildStepStartMessage(msg)
	case base.MessageTypeStepFinish:
		return b.BuildStepFinishMessage(msg)
	case base.MessageTypeRaw:
		return b.BuildRawMessage(msg)
	default:
		// Default to answer message for unknown types
		return b.BuildAnswerMessage(msg)
	}
}

// =============================================================================
// Thinking Message (AI is reasoning)
// =============================================================================

// BuildThinkingMessage builds a status indicator for thinking state
// Implements EventTypeThinking per spec - uses context block for low visual weight
func (b *MessageBuilder) BuildThinkingMessage(msg *base.ChatMessage) []slack.Block {
	content := msg.Content
	if content == "" {
		content = "Thinking..."
	}

	// Use context block per spec for low visual weight
	text := slack.NewTextBlockObject("mrkdwn", ":brain: "+content, false, false)
	return []slack.Block{
		slack.NewContextBlock("", text),
	}
}

// =============================================================================
// Tool Use Message (Tool invocation started)
// =============================================================================

// BuildToolUseMessage builds a message for tool invocation
// Implements EventTypeToolUse per spec - uses fields dual-column layout, parameter summary 12 chars
func (b *MessageBuilder) BuildToolUseMessage(msg *base.ChatMessage) []slack.Block {
	toolName := msg.Content
	if toolName == "" {
		toolName = "Unknown Tool"
	}

	// Get tool emoji based on tool name per spec
	toolEmoji := getToolEmoji(toolName)

	// Extract tool input from metadata or RichContent
	input := ""
	if msg.Metadata != nil {
		if in, ok := msg.Metadata["input"].(string); ok {
			input = in
		}
		if summary, ok := msg.Metadata["input_summary"].(string); ok && summary != "" {
			input = summary
		}
	}

	// Truncate input to 12 characters per spec for summary
	inputSummary := input
	if len(inputSummary) > 12 {
		inputSummary = inputSummary[:12] + "..."
	}

	// Use fields dual-column layout per spec
	toolNameText := slack.NewTextBlockObject("mrkdwn", fmt.Sprintf("*%s*", toolName), false, false)
	inputText := slack.NewTextBlockObject("mrkdwn", "```"+inputSummary+"```", false, false)

	section := slack.NewSectionBlock(nil, []*slack.TextBlockObject{toolNameText, inputText}, nil)

	// Add detail section with full input if available
	var blocks []slack.Block
	blocks = append(blocks, section)

	if input != "" && len(input) > 12 {
		detailText := fmt.Sprintf("%s %s", toolEmoji, "*Using tool:* `"+toolName+"`")
		if len(input) > 200 {
			input = input[:200] + "..."
		}
		detailText += fmt.Sprintf("\n```\n%s\n```", input)
		mrkdwn := slack.NewTextBlockObject("mrkdwn", detailText, false, false)
		blocks = append(blocks, slack.NewSectionBlock(mrkdwn, nil, nil))
	}

	return blocks
}

// getToolEmoji returns the appropriate emoji for a tool type per spec
func getToolEmoji(toolName string) string {
	toolNameLower := strings.ToLower(toolName)
	switch {
	case strings.Contains(toolNameLower, "bash") || strings.Contains(toolNameLower, "shell") || strings.Contains(toolNameLower, "exec"):
		return ":computer:"
	case strings.Contains(toolNameLower, "edit") || strings.Contains(toolNameLower, "multiedit"):
		return ":pencil:"
	case strings.Contains(toolNameLower, "write") || strings.Contains(toolNameLower, "filewrite"):
		return ":page_facing_up:"
	case strings.Contains(toolNameLower, "read") || strings.Contains(toolNameLower, "fileread"):
		return ":books:"
	case strings.Contains(toolNameLower, "search") || strings.Contains(toolNameLower, "glob") || strings.Contains(toolNameLower, "fileglob"):
		return ":mag:"
	case strings.Contains(toolNameLower, "webfetch") || strings.Contains(toolNameLower, "websearch") || strings.Contains(toolNameLower, "fetch"):
		return ":globe_with_meridians:"
	case strings.Contains(toolNameLower, "grep"):
		return ":magnifying_glass_tilted_left:"
	case strings.Contains(toolNameLower, "ls") || strings.Contains(toolNameLower, "list") || strings.Contains(toolNameLower, "directory"):
		return ":file_folder:"
	default:
		return ":hammer_and_wrench:"
	}
}

// =============================================================================
// Tool Result Message (Tool execution completed)
// =============================================================================

// BuildToolResultMessage builds a message for tool execution result
// Implements EventTypeToolResult per spec - shows status, duration, and data length
func (b *MessageBuilder) BuildToolResultMessage(msg *base.ChatMessage) []slack.Block {
	var blocks []slack.Block

	// Check metadata for success status
	success := true
	if msg.Metadata != nil {
		if s, ok := msg.Metadata["success"].(bool); ok {
			success = s
		}
	}

	// Get duration and tool name from metadata
	durationMs := int64(0)
	toolName := ""
	if msg.Metadata != nil {
		if d, ok := msg.Metadata["duration_ms"].(int64); ok {
			durationMs = d
		}
		if tn, ok := msg.Metadata["tool_name"].(string); ok {
			toolName = tn
		}
	}

	// Get data length from content
	dataLen := len(msg.Content)
	var dataLenStr string
	if dataLen > 1024*1024 {
		dataLenStr = fmt.Sprintf("%.1fMB", float64(dataLen)/(1024*1024))
	} else if dataLen > 1024 {
		dataLenStr = fmt.Sprintf("%.1fKB", float64(dataLen)/1024)
	} else {
		dataLenStr = fmt.Sprintf("%d bytes", dataLen)
	}

	icon := ":white_check_mark:"
	if !success {
		icon = ":x:"
	}

	// Format: icon + tool name + duration (>500ms per spec) + data length
	toolNameStr := toolName
	if toolNameStr == "" {
		toolNameStr = "Tool"
	}

	statusText := fmt.Sprintf("%s *%s*", icon, toolNameStr)

	// Add duration only if > 500ms per spec
	if durationMs > 500 {
		if durationMs > 1000 {
			statusText += fmt.Sprintf(" (%.2fs)", float64(durationMs)/1000)
		} else {
			statusText += fmt.Sprintf(" (%dms)", durationMs)
		}
	}

	// Add data length per spec
	statusText += fmt.Sprintf(" • %s", dataLenStr)

	statusObj := slack.NewTextBlockObject("mrkdwn", statusText, false, false)
	blocks = append(blocks, slack.NewSectionBlock(statusObj, nil, nil))

	// Add content if not empty
	content := msg.Content
	if content != "" {
		// Truncate if too long for display
		if len(content) > 3000 {
			content = content[:3000] + "\n... (truncated)"
		}
		// Format as code block
		codeText := slack.NewTextBlockObject("mrkdwn", "```\n"+content+"\n```", false, false)
		blocks = append(blocks, slack.NewSectionBlock(codeText, nil, nil))
	}

	return blocks
}

// =============================================================================
// Answer Message (Final text output)
// =============================================================================

// BuildAnswerMessage builds a message for AI answer
func (b *MessageBuilder) BuildAnswerMessage(msg *base.ChatMessage) []slack.Block {
	content := msg.Content
	if content == "" {
		return nil
	}

	// Convert Markdown to mrkdwn
	formattedContent := b.formatter.Format(content)

	// Check if content is too long for a single message
	if len(formattedContent) > 4000 {
		// Split into chunks
		return b.buildChunkedAnswerBlocks(formattedContent)
	}

	mrkdwn := slack.NewTextBlockObject("mrkdwn", formattedContent, false, false)
	return []slack.Block{
		slack.NewSectionBlock(mrkdwn, nil, nil),
	}
}

// buildChunkedAnswerBlocks splits long content into chunks
func (b *MessageBuilder) buildChunkedAnswerBlocks(content string) []slack.Block {
	var blocks []slack.Block

	chunks := b.chunkText(content, 3500)
	for i, chunk := range chunks {
		if i > 0 {
			// Add divider between chunks
			blocks = append(blocks, slack.NewDividerBlock())
		}
		mrkdwn := slack.NewTextBlockObject("mrkdwn", chunk, false, false)
		blocks = append(blocks, slack.NewSectionBlock(mrkdwn, nil, nil))
	}

	return blocks
}

// chunkText splits text into chunks at word boundaries
func (b *MessageBuilder) chunkText(text string, maxLen int) []string {
	if len(text) <= maxLen {
		return []string{text}
	}

	var chunks []string
	lines := strings.Split(text, "\n")
	currentChunk := ""

	for _, line := range lines {
		if len(currentChunk)+len(line)+1 > maxLen {
			if currentChunk != "" {
				chunks = append(chunks, currentChunk)
				currentChunk = ""
			}
		}
		if currentChunk != "" {
			currentChunk += "\n"
		}
		currentChunk += line
	}

	if currentChunk != "" {
		chunks = append(chunks, currentChunk)
	}

	return chunks
}

// =============================================================================
// Error Message
// =============================================================================

// BuildErrorMessage builds a message for errors
// Implements EventTypeError per spec - uses quote format for emphasis
func (b *MessageBuilder) BuildErrorMessage(msg *base.ChatMessage) []slack.Block {
	content := msg.Content
	if content == "" {
		content = "An error occurred"
	}

	// Use quote format (> ) per spec for emphasis
	// Split content by newlines and add > prefix to each line
	lines := strings.Split(content, "\n")
	var quotedLines []string
	for _, line := range lines {
		quotedLines = append(quotedLines, "> "+line)
	}
	quotedContent := strings.Join(quotedLines, "\n")

	text := ":warning: *Error*\n" + quotedContent
	mrkdwn := slack.NewTextBlockObject("mrkdwn", text, false, false)

	return []slack.Block{
		slack.NewSectionBlock(mrkdwn, nil, nil),
	}
}

// =============================================================================
// Plan Mode Message
// =============================================================================

// BuildPlanModeMessage builds a message for plan mode
// Implements EventTypePlanMode per spec - uses context block for low visual weight
func (b *MessageBuilder) BuildPlanModeMessage(msg *base.ChatMessage) []slack.Block {
	content := msg.Content
	if content == "" {
		content = "Generating..."
	}

	// Use context block per spec for low visual weight
	text := slack.NewTextBlockObject("mrkdwn", ":mag_right: _Plan Mode: "+content+"_", false, false)
	return []slack.Block{
		slack.NewContextBlock("", text),
	}
}

// =============================================================================
// Exit Plan Mode Message (Requesting user approval)
// =============================================================================

// BuildExitPlanModeMessage builds a message for exit plan mode
func (b *MessageBuilder) BuildExitPlanModeMessage(msg *base.ChatMessage) []slack.Block {
	content := msg.Content
	if content == "" {
		content = "Plan generated. Waiting for approval."
	}

	text := ":clipboard: *Plan Ready*\n" + content

	// Add approve/deny buttons
	approveBtn := slack.NewButtonBlockElement("plan_approve", "approve",
		slack.NewTextBlockObject("plain_text", "Approve", false, true))
	approveBtn.Style = "primary"

	denyBtn := slack.NewButtonBlockElement("plan_deny", "deny",
		slack.NewTextBlockObject("plain_text", "Deny", false, true))
	denyBtn.Style = "danger"

	actionBlock := slack.NewActionBlock("plan_actions", approveBtn, denyBtn)

	mrkdwn := slack.NewTextBlockObject("mrkdwn", text, false, false)

	return []slack.Block{
		slack.NewSectionBlock(mrkdwn, nil, nil),
		slack.NewDividerBlock(),
		actionBlock,
	}
}

// =============================================================================
// Ask User Question Message
// =============================================================================

// BuildAskUserQuestionMessage builds a message for user questions
func (b *MessageBuilder) BuildAskUserQuestionMessage(msg *base.ChatMessage) []slack.Block {
	question := msg.Content
	if question == "" {
		question = "Please provide more information."
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
				btn := slack.NewButtonBlockElement(fmt.Sprintf("question_option_%d", i), option,
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

// =============================================================================
// Danger Block Message
// =============================================================================

// BuildDangerBlockMessage builds a message for dangerous operations
func (b *MessageBuilder) BuildDangerBlockMessage(msg *base.ChatMessage) []slack.Block {
	content := msg.Content
	if content == "" {
		content = "This operation requires confirmation."
	}

	text := ":rotating_light: *Confirmation Required*\n" + content

	// Add confirm/cancel buttons
	confirmBtn := slack.NewButtonBlockElement("danger_confirm", "confirm",
		slack.NewTextBlockObject("plain_text", "Confirm", false, true))
	confirmBtn.Style = "danger"

	cancelBtn := slack.NewButtonBlockElement("danger_cancel", "cancel",
		slack.NewTextBlockObject("plain_text", "Cancel", false, true))

	actionBlock := slack.NewActionBlock("danger_actions", confirmBtn, cancelBtn)

	mrkdwn := slack.NewTextBlockObject("mrkdwn", text, false, false)

	return []slack.Block{
		slack.NewSectionBlock(mrkdwn, nil, nil),
		slack.NewDividerBlock(),
		actionBlock,
	}
}

// =============================================================================
// Session Stats Message
// =============================================================================

// BuildSessionStatsMessage builds a message for session statistics
// Implements EventTypeResult (Turn Complete) per spec
func (b *MessageBuilder) BuildSessionStatsMessage(msg *base.ChatMessage) []slack.Block {
	content := msg.Content
	if content == "" {
		return nil
	}

	// Use section + context per spec for EventTypeResult
	text := ":white_check_mark: *Turn Complete*\n" + content
	mrkdwn := slack.NewTextBlockObject("mrkdwn", text, false, false)

	// Add context with stats if available
	var blocks []slack.Block
	blocks = append(blocks, slack.NewSectionBlock(mrkdwn, nil, nil))

	// Add duration and token counts from metadata if available
	if msg.Metadata != nil {
		var contextElems []slack.MixedElement
		if duration, ok := msg.Metadata["duration_ms"].(int64); ok && duration > 0 {
			durationStr := FormatDuration(duration)
			contextElems = append(contextElems, slack.NewTextBlockObject("mrkdwn", "⏱️ "+durationStr, false, false))
		}
		if tokensIn, ok := msg.Metadata["tokens_in"].(int64); ok {
			if tokensOut, ok := msg.Metadata["tokens_out"].(int64); ok {
				contextElems = append(contextElems, slack.NewTextBlockObject("mrkdwn", fmt.Sprintf("📊 %d in / %d out", tokensIn, tokensOut), false, false))
			}
		}
		if len(contextElems) > 0 {
			blocks = append(blocks, slack.NewContextBlock("", contextElems...))
		}
	}

	return blocks
}

// =============================================================================
// Command Progress Message (Slash command executing)
// =============================================================================

// BuildCommandProgressMessage builds a message for command progress updates
// Implements EventTypeCommandProgress per spec
func (b *MessageBuilder) BuildCommandProgressMessage(msg *base.ChatMessage) []slack.Block {
	title := msg.Content
	if title == "" {
		title = "Executing command..."
	}

	// Get command name from metadata
	commandName := ""
	if msg.Metadata != nil {
		if cmd, ok := msg.Metadata["command"].(string); ok {
			commandName = cmd
		}
	}

	headerText := ":gear: *" + commandName + "*"
	if commandName == "" {
		headerText = ":gear: *Command Progress*"
	}

	mrkdwn := slack.NewTextBlockObject("mrkdwn", headerText+"\n"+title, false, false)

	var blocks []slack.Block
	blocks = append(blocks, slack.NewSectionBlock(mrkdwn, nil, nil))

	// Add progress steps from metadata if available
	if msg.Metadata != nil {
		if steps, ok := msg.Metadata["steps"].([]string); ok && len(steps) > 0 {
			var stepTexts []string
			for i, step := range steps {
				stepTexts = append(stepTexts, fmt.Sprintf("○ Step %d: %s", i+1, step))
			}
			stepsText := strings.Join(stepTexts, "\n")
			stepsObj := slack.NewTextBlockObject("mrkdwn", stepsText, false, false)
			blocks = append(blocks, slack.NewSectionBlock(stepsObj, nil, nil))

			// Add cancel button
			cancelBtn := slack.NewButtonBlockElement("cmd_cancel", "cancel",
				slack.NewTextBlockObject("plain_text", "Cancel", false, true))
			actionBlock := slack.NewActionBlock("cmd_actions", cancelBtn)
			blocks = append(blocks, actionBlock)
		}
	}

	return blocks
}

// =============================================================================
// Command Complete Message (Slash command finished)
// =============================================================================

// BuildCommandCompleteMessage builds a message for command completion
// Implements EventTypeCommandComplete per spec
func (b *MessageBuilder) BuildCommandCompleteMessage(msg *base.ChatMessage) []slack.Block {
	title := msg.Content
	if title == "" {
		title = "Command completed"
	}

	// Get command name and stats from metadata
	commandName := ""
	var durationMs int64
	var completedSteps, totalSteps int
	if msg.Metadata != nil {
		if cmd, ok := msg.Metadata["command"].(string); ok {
			commandName = cmd
		}
		if dur, ok := msg.Metadata["duration_ms"].(int64); ok {
			durationMs = dur
		}
		if completed, ok := msg.Metadata["completed_steps"].(int); ok {
			completedSteps = completed
		}
		if total, ok := msg.Metadata["total_steps"].(int); ok {
			totalSteps = total
		}
	}

	headerText := ":white_check_mark: *" + commandName + " Complete*"
	if commandName == "" {
		headerText = ":white_check_mark: *Command Complete*"
	}

	mrkdwn := slack.NewTextBlockObject("mrkdwn", headerText+"\n"+title, false, false)

	var blocks []slack.Block
	blocks = append(blocks, slack.NewSectionBlock(mrkdwn, nil, nil))

	// Add stats in context block
	var contextElems []slack.MixedElement
	if durationMs > 0 {
		contextElems = append(contextElems, slack.NewTextBlockObject("mrkdwn", "⏱️ "+FormatDuration(durationMs), false, false))
	}
	if totalSteps > 0 {
		contextElems = append(contextElems, slack.NewTextBlockObject("mrkdwn", fmt.Sprintf("✓ %d/%d steps", completedSteps, totalSteps), false, false))
	}
	if len(contextElems) > 0 {
		blocks = append(blocks, slack.NewContextBlock("", contextElems...))
	}

	return blocks
}

// =============================================================================
// System Message
// =============================================================================

// BuildSystemMessage builds a message for system-level messages
// Implements EventTypeSystem per spec - uses context block for low visual weight
func (b *MessageBuilder) BuildSystemMessage(msg *base.ChatMessage) []slack.Block {
	content := msg.Content
	if content == "" {
		return nil
	}

	// Use context block per spec for low visual weight
	text := slack.NewTextBlockObject("mrkdwn", ":gear: System: "+content, false, false)
	return []slack.Block{
		slack.NewContextBlock("", text),
	}
}

// =============================================================================
// User Message (User message reflection)
// =============================================================================

// BuildUserMessage builds a message for user message reflection
// Implements EventTypeUser per spec
func (b *MessageBuilder) BuildUserMessage(msg *base.ChatMessage) []slack.Block {
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
	text := ":bust_in_silhouette: *User:*\n" + content
	mrkdwn := slack.NewTextBlockObject("mrkdwn", text, false, false)

	var blocks []slack.Block
	blocks = append(blocks, slack.NewSectionBlock(mrkdwn, nil, nil))

	if timestamp != "" {
		timeObj := slack.NewTextBlockObject("mrkdwn", timestamp, false, false)
		blocks = append(blocks, slack.NewContextBlock("", timeObj))
	}

	return blocks
}

// =============================================================================
// Step Start Message (OpenCode step started)
// =============================================================================

// BuildStepStartMessage builds a message for step start
// Implements EventTypeStepStart per spec
func (b *MessageBuilder) BuildStepStartMessage(msg *base.ChatMessage) []slack.Block {
	content := msg.Content
	if content == "" {
		content = "Starting step..."
	}

	// Get step info from metadata
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

	// Use section + context per spec
	text := fmt.Sprintf(":arrow_right: *Step %d/%d:*\n%s", stepNum, totalSteps, content)
	mrkdwn := slack.NewTextBlockObject("mrkdwn", text, false, false)

	return []slack.Block{
		slack.NewSectionBlock(mrkdwn, nil, nil),
	}
}

// =============================================================================
// Step Finish Message (OpenCode step completed)
// =============================================================================

// BuildStepFinishMessage builds a message for step completion
// Implements EventTypeStepFinish per spec
func (b *MessageBuilder) BuildStepFinishMessage(msg *base.ChatMessage) []slack.Block {
	content := msg.Content
	if content == "" {
		content = "Step completed"
	}

	// Get step info and duration from metadata
	stepNum := 1
	totalSteps := 1
	var durationMs int64
	if msg.Metadata != nil {
		if step, ok := msg.Metadata["step"].(int); ok {
			stepNum = step
		}
		if total, ok := msg.Metadata["total"].(int); ok {
			totalSteps = total
		}
		if dur, ok := msg.Metadata["duration_ms"].(int64); ok {
			durationMs = dur
		}
	}

	// Use section + context per spec
	text := fmt.Sprintf(":white_check_mark: *Step %d/%d Complete*\n%s", stepNum, totalSteps, content)
	mrkdwn := slack.NewTextBlockObject("mrkdwn", text, false, false)

	var blocks []slack.Block
	blocks = append(blocks, slack.NewSectionBlock(mrkdwn, nil, nil))

	// Add duration in context
	if durationMs > 0 {
		durationObj := slack.NewTextBlockObject("mrkdwn", "⏱️ "+FormatDuration(durationMs), false, false)
		blocks = append(blocks, slack.NewContextBlock("", durationObj))
	}

	return blocks
}

// =============================================================================
// Raw Message (Unparsed raw output)
// =============================================================================

// BuildRawMessage builds a message for raw/unparsed output
// Implements EventTypeRaw per spec - shows only type and length, not content
func (b *MessageBuilder) BuildRawMessage(msg *base.ChatMessage) []slack.Block {
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

// =============================================================================
// Plan Approval/Denial Messages (Interactive Callbacks)
// =============================================================================

// BuildPlanApprovedBlock builds blocks to show after plan is approved
func (b *MessageBuilder) BuildPlanApprovedBlock() []slack.Block {
	text := slack.NewTextBlockObject("mrkdwn", "✅ *Plan Approved*\n\nClaude is now executing the plan...", false, false)
	return []slack.Block{
		slack.NewSectionBlock(text, nil, nil),
	}
}

// BuildPlanCancelledBlock builds blocks to show after plan is cancelled
func (b *MessageBuilder) BuildPlanCancelledBlock(reason string) []slack.Block {
	text := slack.NewTextBlockObject("mrkdwn", "❌ *Plan Cancelled*", false, false)
	blocks := []slack.Block{
		slack.NewSectionBlock(text, nil, nil),
	}

	if reason != "" {
		reasonText := slack.NewTextBlockObject("mrkdwn", "Reason: "+reason, false, false)
		blocks = append(blocks, slack.NewSectionBlock(reasonText, nil, nil))
	}

	return blocks
}

// =============================================================================
// Permission Request Messages (Interactive Callbacks)
// =============================================================================

// BuildPermissionRequestMessage builds Slack blocks for a permission request
// Displays tool name, command preview, and approval/denial buttons
func (b *MessageBuilder) BuildPermissionRequestMessage(req *provider.PermissionRequest, sessionID string) []slack.Block {
	tool, input := req.GetToolAndInput()

	// Sanitize and truncate commands for preview
	safeInput := SanitizeCommand(input)
	displayInput := safeInput
	if RuneCount(displayInput) > 500 {
		displayInput = TruncateByRune(displayInput, 497) + "..."
	}

	var blocks []slack.Block

	// Header
	headerText := slack.NewTextBlockObject("plain_text", "⚠️ Permission Request", true, false)
	blocks = append(blocks, slack.NewHeaderBlock(headerText))

	// Tool information
	if tool != "" {
		toolText := slack.NewTextBlockObject("mrkdwn", fmt.Sprintf("*Tool:* `%s`", tool), false, false)
		blocks = append(blocks, slack.NewSectionBlock(toolText, nil, nil))
	}

	// Command/Action preview
	if displayInput != "" {
		cmdText := slack.NewTextBlockObject("mrkdwn", fmt.Sprintf("*Command:*\n```\n%s\n```", displayInput), false, false)
		blocks = append(blocks, slack.NewSectionBlock(cmdText, nil, nil))
	}

	// Decision reason (if available)
	if req.Decision != nil && req.Decision.Reason != "" {
		reasonText := slack.NewTextBlockObject("mrkdwn", fmt.Sprintf("*Reason:* %s", req.Decision.Reason), false, false)
		blocks = append(blocks, slack.NewContextBlock("", []slack.MixedElement{
			reasonText,
		}...))
	}

	// Session info
	sessionText := slack.NewTextBlockObject("mrkdwn", fmt.Sprintf("Session: `%s`", sessionID), false, false)
	blocks = append(blocks, slack.NewContextBlock("", []slack.MixedElement{
		sessionText,
	}...))

	// Action buttons with validated block_id
	blockID := ValidateBlockID(fmt.Sprintf("perm_%s", req.MessageID))

	approveBtn := slack.NewButtonBlockElement("perm_allow", fmt.Sprintf("allow:%s:%s", sessionID, req.MessageID),
		slack.NewTextBlockObject("plain_text", "✅ Allow", true, false))
	approveBtn.Style = "primary"

	denyBtn := slack.NewButtonBlockElement("perm_deny", fmt.Sprintf("deny:%s:%s", sessionID, req.MessageID),
		slack.NewTextBlockObject("plain_text", "🚫 Deny", true, false))
	denyBtn.Style = "danger"

	blocks = append(blocks, slack.NewActionBlock(blockID, approveBtn, denyBtn))

	return blocks
}

// BuildPermissionApprovedMessage builds blocks to show after permission is approved
func (b *MessageBuilder) BuildPermissionApprovedMessage(tool, input string) []slack.Block {
	// Truncate for display
	displayInput := input
	if len(displayInput) > 200 {
		displayInput = displayInput[:197] + "..."
	}

	text := slack.NewTextBlockObject("mrkdwn", fmt.Sprintf("✅ *Permission Granted*\n\nTool: `%s`\nCommand: `%s`", tool, displayInput), false, false)
	return []slack.Block{
		slack.NewSectionBlock(text, nil, nil),
	}
}

// BuildPermissionDeniedMessage builds blocks to show after permission is denied
func (b *MessageBuilder) BuildPermissionDeniedMessage(tool, input, reason string) []slack.Block {
	// Truncate for display
	displayInput := input
	if len(displayInput) > 200 {
		displayInput = displayInput[:197] + "..."
	}

	text := slack.NewTextBlockObject("mrkdwn", fmt.Sprintf("🚫 *Permission Denied*\n\nTool: `%s`\nCommand: `%s`", tool, displayInput), false, false)
	blocks := []slack.Block{
		slack.NewSectionBlock(text, nil, nil),
	}

	if reason != "" {
		reasonText := slack.NewTextBlockObject("mrkdwn", "Reason: "+reason, false, false)
		blocks = append(blocks, slack.NewContextBlock("", []slack.MixedElement{
			reasonText,
		}...))
	}

	return blocks
}

// =============================================================================
// Helper: Extract tool metadata from provider event
// =============================================================================

// ExtractToolInfo extracts tool name and input from ChatMessage metadata
func ExtractToolInfo(msg *base.ChatMessage) (toolName, input string) {
	toolName = msg.Content

	if msg.Metadata != nil {
		if name, ok := msg.Metadata["tool_name"].(string); ok {
			toolName = name
		}
		if in, ok := msg.Metadata["input"].(string); ok {
			input = in
		}
	}

	return toolName, input
}

// =============================================================================
// Constants for compatibility
// =============================================================================

// ToolResultDurationThreshold is the threshold for showing duration
const ToolResultDurationThreshold = 500 // ms

// IsLongRunningTool checks if a tool is considered long-running
func IsLongRunningTool(durationMs int64) bool {
	return durationMs > ToolResultDurationThreshold
}

// FormatDuration formats duration for display
func FormatDuration(durationMs int64) string {
	if durationMs > 1000 {
		return fmt.Sprintf("%.2fs", float64(durationMs)/1000)
	}
	return fmt.Sprintf("%dms", durationMs)
}

// ParseProviderEventType converts provider event type to base message type
func ParseProviderEventType(eventType provider.ProviderEventType) base.MessageType {
	switch eventType {
	case provider.EventTypeThinking:
		return base.MessageTypeThinking
	case provider.EventTypeToolUse:
		return base.MessageTypeToolUse
	case provider.EventTypeToolResult:
		return base.MessageTypeToolResult
	case provider.EventTypeAnswer:
		return base.MessageTypeAnswer
	case provider.EventTypeError:
		return base.MessageTypeError
	case provider.EventTypePlanMode:
		return base.MessageTypePlanMode
	case provider.EventTypeExitPlanMode:
		return base.MessageTypeExitPlanMode
	case provider.EventTypeAskUserQuestion:
		return base.MessageTypeAskUserQuestion
	case provider.EventTypeResult:
		return base.MessageTypeSessionStats
	case provider.EventTypeCommandProgress:
		return base.MessageTypeCommandProgress
	case provider.EventTypeCommandComplete:
		return base.MessageTypeCommandComplete
	case provider.EventTypeSystem:
		return base.MessageTypeSystem
	case provider.EventTypeUser:
		return base.MessageTypeUser
	case provider.EventTypeStepStart:
		return base.MessageTypeStepStart
	case provider.EventTypeStepFinish:
		return base.MessageTypeStepFinish
	case provider.EventTypeRaw:
		return base.MessageTypeRaw
	default:
		return base.MessageTypeAnswer
	}
}

// TimeToSlackTimestamp converts time.Time to Slack timestamp format
func TimeToSlackTimestamp(t time.Time) string {
	return fmt.Sprintf("%d.%d", t.Unix(), t.Nanosecond()/1000000)
}
