// Package slack provides the Slack adapter implementation for the hotplex engine.
// Detailed message construction logic using Slack Block Kit.
package slack

import (
	"fmt"
	"strings"
	"time"

	"github.com/hrygo/hotplex/chatapps/base"
	"github.com/hrygo/hotplex/provider"
	"github.com/slack-go/slack"
)

// MessageBuilder translates platform-agnostic base.ChatMessage objects into
// rich Slack Block Kit structures, ensuring consistent UX across different message types.
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
	case base.MessageTypeSessionStart:
		return b.BuildSessionStartMessage(msg)
	case base.MessageTypeEngineStarting:
		return b.BuildEngineStartingMessage(msg)
	case base.MessageTypeUserMessageReceived:
		return b.BuildUserMessageReceivedMessage(msg)
	case base.MessageTypePermissionRequest:
		return b.BuildPermissionRequestMessageFromChat(msg)
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

	// Zone 1: 64 character fixed length sliding window
	// Truncate from the front so we can see the latest thinking progress
	runes := []rune(content)
	if len(runes) > 64 {
		content = "... " + string(runes[len(runes)-60:])
	}

	// Use an avatar block with animated WebP for the Thinking State as per new UX design
	text := slack.NewTextBlockObject("mrkdwn", ":hourglass_flowing_sand: _"+content+"_", false, false)
	return []slack.Block{
		slack.NewContextBlock("", text),
	}
}

// BuildStatusBubble builds a status bubble message from StatusType
func (b *MessageBuilder) BuildStatusBubble(status base.StatusType, text string) []slack.Block {
	if text == "" {
		text = getDefaultStatusText(status)
	}

	// Map StatusType to emoji
	emoji := getStatusEmoji(status)

	// Truncate to 64 chars
	runes := []rune(text)
	if len(runes) > 64 {
		text = "... " + string(runes[len(runes)-60:])
	}

	formatted := fmt.Sprintf("%s _%s_", emoji, text)
	textObj := slack.NewTextBlockObject("mrkdwn", formatted, false, false)
	return []slack.Block{
		slack.NewContextBlock("", textObj),
	}
}

func getStatusEmoji(status base.StatusType) string {
	switch status {
	case base.StatusThinking:
		return ":hourglass_flowing_sand:"
	case base.StatusToolUse:
		return ":toolbox:"
	case base.StatusToolResult:
		return ":white_check_mark:"
	case base.StatusAnswering:
		return ":speech_balloon:"
	default:
		return ":bell:"
	}
}

func getDefaultStatusText(status base.StatusType) string {
	switch status {
	case base.StatusThinking:
		return "Thinking..."
	case base.StatusToolUse:
		return "Using tool..."
	case base.StatusToolResult:
		return "Tool completed"
	case base.StatusAnswering:
		return "Answering..."
	default:
		return "Processing..."
	}
}

// =============================================================================
// Tool Use Message (Tool invocation started)
// =============================================================================

// BuildToolUseMessage builds a message for tool invocation
// Implements EventTypeToolUse per spec - uses fields dual-column layout, parameter summary 12 chars
// Supports aggregated messages: if metadata contains "_original_messages", builds blocks for each.
func (b *MessageBuilder) BuildToolUseMessage(msg *base.ChatMessage) []slack.Block {
	// Handle aggregated messages for batch display
	if msg.Metadata != nil {
		if rawMsgs, ok := msg.Metadata["_original_messages"]; ok {
			if messages, ok := rawMsgs.([]*base.ChatMessage); ok && len(messages) > 1 {
				var allBlocks []slack.Block
				for _, subMsg := range messages {
					allBlocks = append(allBlocks, b.buildSingleToolUseBlock(subMsg)...)
				}
				return allBlocks
			}
		}
	}

	// Single message case
	return b.buildSingleToolUseBlock(msg)
}

// buildSingleToolUseBlock is the internal logic for a single tool use section
func (b *MessageBuilder) buildSingleToolUseBlock(msg *base.ChatMessage) []slack.Block {
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

	// Truncate input for summary display
	inputSummary := input
	if len(inputSummary) > 64 {
		inputSummary = inputSummary[:64] + "..."
	}

	// Full-width single line: emoji + tool name + parameter summary
	// Format: :computer: *Bash* `ls -la...`
	line := toolEmoji + " *" + toolName + "*"
	if inputSummary != "" {
		line += "  `" + inputSummary + "`"
	}
	text := slack.NewTextBlockObject("mrkdwn", line, false, false)
	section := slack.NewSectionBlock(text, nil, nil)

	return []slack.Block{section}
}

// getToolEmoji returns the appropriate emoji for a tool type per spec
func getToolEmoji(toolName string) string {
	toolNameLower := strings.ToLower(toolName)
	switch {
	case strings.Contains(toolNameLower, "bash") || strings.Contains(toolNameLower, "shell") || strings.Contains(toolNameLower, "exec"):
		return ":keyboard:"
	case strings.Contains(toolNameLower, "edit") || strings.Contains(toolNameLower, "multiedit"):
		return ":pencil2:"
	case strings.Contains(toolNameLower, "write") || strings.Contains(toolNameLower, "filewrite"):
		return ":floppy_disk:"
	case strings.Contains(toolNameLower, "read") || strings.Contains(toolNameLower, "fileread") || strings.Contains(toolNameLower, "view"):
		return ":eyes:"
	case strings.Contains(toolNameLower, "search") || strings.Contains(toolNameLower, "glob") || strings.Contains(toolNameLower, "fileglob"):
		return ":mag:"
	case strings.Contains(toolNameLower, "webfetch") || strings.Contains(toolNameLower, "websearch") || strings.Contains(toolNameLower, "fetch"):
		return ":globe_with_meridians:"
	case strings.Contains(toolNameLower, "grep"):
		return ":mag_right:"
	case strings.Contains(toolNameLower, "ls") || strings.Contains(toolNameLower, "list") || strings.Contains(toolNameLower, "directory"):
		return ":file_folder:"
	default:
		return ":gear:"
	}
}

// =============================================================================
// Tool Result Message (Tool execution completed)
// =============================================================================

// BuildToolResultMessage builds a message for tool execution result
// Implements EventTypeToolResult per spec - shows status, duration, and data length
// Supports aggregated messages: if metadata contains "_original_messages", builds blocks for each.
func (b *MessageBuilder) BuildToolResultMessage(msg *base.ChatMessage) []slack.Block {
	// Handle aggregated messages for batch display
	if msg.Metadata != nil {
		if rawMsgs, ok := msg.Metadata["_original_messages"]; ok {
			if messages, ok := rawMsgs.([]*base.ChatMessage); ok && len(messages) > 1 {
				var allBlocks []slack.Block
				for _, subMsg := range messages {
					allBlocks = append(allBlocks, b.buildSingleToolResultBlock(subMsg)...)
				}
				return allBlocks
			}
		}
	}

	// Single message case
	return b.buildSingleToolResultBlock(msg)
}

// buildSingleToolResultBlock is the internal logic for a single tool result line
func (b *MessageBuilder) buildSingleToolResultBlock(msg *base.ChatMessage) []slack.Block {
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
		} else if d, ok := msg.Metadata["duration_ms"].(float64); ok {
			durationMs = int64(d)
		}
		if tn, ok := msg.Metadata["tool_name"].(string); ok {
			toolName = tn
		}
	}

	// Get data length from content or metadata preference
	dataLen := int64(len(msg.Content))
	if msg.Metadata != nil {
		if dl, ok := msg.Metadata["content_length"].(int64); ok {
			dataLen = dl
		} else if dl, ok := msg.Metadata["content_length"].(float64); ok {
			dataLen = int64(dl)
		}
	}

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
		icon = ":warning:"
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

	// Error passthrough: on failure, append the first 200 chars of error content
	// as a code block below the summary to aid debugging in Slack.
	if !success && msg.Content != "" {
		errPreview := msg.Content
		if len(errPreview) > 200 {
			errPreview = errPreview[:200] + "…"
		}
		errBlock := slack.NewTextBlockObject("mrkdwn", "```"+errPreview+"```", false, false)
		blocks = append(blocks, slack.NewContextBlock("", errBlock))
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
// Implements EventTypeExitPlanMode per spec (15)
// Block type: header + section + divider + actions
func (b *MessageBuilder) BuildExitPlanModeMessage(msg *base.ChatMessage) []slack.Block {
	content := msg.Content
	if content == "" {
		content = "Plan generated. Waiting for approval."
	}

	// Extract session_id from metadata for button values
	sessionID := ""
	if msg.Metadata != nil {
		if sid, ok := msg.Metadata["session_id"].(string); ok {
			sessionID = sid
		}
	}

	// Per spec: header block with clipboard emoji
	headerText := slack.NewTextBlockObject("plain_text", ":clipboard: Plan Ready", false, false)
	header := slack.NewHeaderBlock(headerText)

	// Section with plan content
	sectionText := slack.NewTextBlockObject("mrkdwn", content, false, false)
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

// =============================================================================
// Ask User Question Message
// =============================================================================

// BuildAskUserQuestionMessage builds a message for user questions
func (b *MessageBuilder) BuildAskUserQuestionMessage(msg *base.ChatMessage) []slack.Block {
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

// =============================================================================
// Danger Block Message
// =============================================================================

// BuildDangerBlockMessage builds a message for dangerous operations
func (b *MessageBuilder) BuildDangerBlockMessage(msg *base.ChatMessage) []slack.Block {
	content := msg.Content
	if content == "" {
		content = "This operation requires confirmation."
	}

	// Extract session_id from metadata for button values
	sessionID := ""
	if msg.Metadata != nil {
		if sid, ok := msg.Metadata["session_id"].(string); ok {
			sessionID = sid
		}
	}

	text := ":rotating_light: *Confirmation Required*\n" + content

	// Add confirm/cancel buttons with sessionID in value
	// Format: confirm:{sessionID} or cancel:{sessionID}
	confirmValue := "confirm"
	cancelValue := "cancel"
	if sessionID != "" {
		confirmValue = "confirm:" + sessionID
		cancelValue = "cancel:" + sessionID
	}

	confirmBtn := slack.NewButtonBlockElement("danger_confirm", confirmValue,
		slack.NewTextBlockObject("plain_text", "Confirm", false, false))
	confirmBtn.Style = "danger"

	cancelBtn := slack.NewButtonBlockElement("danger_cancel", cancelValue,
		slack.NewTextBlockObject("plain_text", "Cancel", false, false))

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
// Implements EventTypeResult (Turn Complete) per spec - compact single-line format
func (b *MessageBuilder) BuildSessionStatsMessage(msg *base.ChatMessage) []slack.Block {
	var blocks []slack.Block

	// Build compact stats line: ⏱️ duration • ⚡ tokens in/out • 📝 files • 🔧 tools
	if msg.Metadata != nil {
		var stats []string

		// Total Duration (from total_duration_ms in SessionStats.ToSummary)
		if duration := extractInt64(msg.Metadata, "total_duration_ms"); duration > 0 {
			stats = append(stats, "⏱️ "+FormatDuration(duration))
		}

		// Tokens (show in/out separately) - using input_tokens/output_tokens from SessionStats.ToSummary
		tokensIn := extractInt64(msg.Metadata, "input_tokens")
		tokensOut := extractInt64(msg.Metadata, "output_tokens")
		if tokensIn > 0 || tokensOut > 0 {
			stats = append(stats, fmt.Sprintf("⚡ %s/%s", formatTokenCount(tokensIn), formatTokenCount(tokensOut)))
		}

		// Files modified
		if files := extractInt64(msg.Metadata, "files_modified"); files > 0 {
			stats = append(stats, fmt.Sprintf("📝 %d files", files))
		}

		// Tool calls (from tool_call_count in SessionStats.ToSummary)
		if tools := extractInt64(msg.Metadata, "tool_call_count"); tools > 0 {
			stats = append(stats, fmt.Sprintf("🔧 %d tools", tools))
		}

		if len(stats) > 0 {
			statsText := slack.NewTextBlockObject("mrkdwn", strings.Join(stats, " • "), false, false)
			blocks = append(blocks, slack.NewContextBlock("", statsText))
		}
	}

	return blocks
}

// extractInt64 extracts int64 value from metadata, supporting both int32 and int64 types
func extractInt64(metadata map[string]any, key string) int64 {
	if v, ok := metadata[key].(int64); ok {
		return v
	}
	if v, ok := metadata[key].(int32); ok {
		return int64(v)
	}
	return 0
}

// formatTokenCount formats token count in compact form (1.2K)
func formatTokenCount(count int64) string {
	if count >= 1000000 {
		return fmt.Sprintf("%.1fM", float64(count)/1000000)
	}
	if count >= 1000 {
		return fmt.Sprintf("%.1fK", float64(count)/1000)
	}
	return fmt.Sprintf("%d", count)
}

// =============================================================================
// Command Progress Message (Slash command executing)
// =============================================================================

// BuildCommandProgressMessage builds a message for command progress updates
// Implements EventTypeCommandProgress per spec (17)
// Block type: section + context + actions
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

	headerText := "⚙️ " + commandName
	if commandName == "" {
		headerText = "⚙️ Executing"
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

			// Per spec: context block with progress indicator
			progressText := slack.NewTextBlockObject("mrkdwn", fmt.Sprintf("Progress: %d steps", len(steps)), false, false)
			blocks = append(blocks, slack.NewContextBlock("", progressText))
		}
	}

	// Per spec: do not add cancel button for command progress messages
	// Command execution cannot be cancelled by user
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

	headerText := "✓ " + commandName + " Complete"
	if commandName == "" {
		headerText = "✓ Command Complete"
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
	text := slack.NewTextBlockObject("mrkdwn", "System: "+content, false, false)
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

// =============================================================================
// Step Start Message (OpenCode step started)
// =============================================================================

// BuildStepStartMessage builds a message for step start
// Implements EventTypeStepStart per spec (11)
// Block type: section + context
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

	// Per spec: section block with step info
	text := fmt.Sprintf("Step %d/%d: %s", stepNum, totalSteps, content)
	mrkdwn := slack.NewTextBlockObject("mrkdwn", text, false, false)
	section := slack.NewSectionBlock(mrkdwn, nil, nil)

	// Per spec: context block with step progress indicator
	contextText := slack.NewTextBlockObject("mrkdwn", fmt.Sprintf("Step %d of %d", stepNum, totalSteps), false, false)
	context := slack.NewContextBlock("", contextText)

	// Return section + context per spec
	return []slack.Block{
		section,
		context,
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
	text := fmt.Sprintf("✓ Step %d/%d Complete: %s", stepNum, totalSteps, content)
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
// Session Start Message (Cold start / first message)
// =============================================================================

// BuildSessionStartMessage builds a message for session start
// Implements EventTypeSessionStart per spec (0.4)
// Triggered when user sends first message or CLI needs cold start
// Block type: context (lightweight)
func (b *MessageBuilder) BuildSessionStartMessage(msg *base.ChatMessage) []slack.Block {
	content := msg.Content
	if content == "" {
		content = "Starting session..."
	}

	// Get session ID from metadata if available
	sessionID := ""
	if msg.Metadata != nil {
		if sid, ok := msg.Metadata["session_id"].(string); ok {
			sessionID = sid
		}
	}

	// Per spec: Use context block for low visual weight during initialization
	var contextElems []slack.MixedElement

	// Add icon and status text
	statusText := slack.NewTextBlockObject("mrkdwn", ":rocket: _"+content+"_", false, false)
	contextElems = append(contextElems, statusText)

	// Add session ID with prefix
	if sessionID != "" {
		displayID := sessionID
		if len(displayID) > 8 {
			displayID = displayID[:8]
		}
		sessionText := slack.NewTextBlockObject("mrkdwn", " •  Session: "+displayID, false, false)
		contextElems = append(contextElems, sessionText)
	}

	return []slack.Block{
		slack.NewContextBlock("", contextElems...),
	}
}

// =============================================================================
// Engine Starting Message (CLI cold start in progress)
// =============================================================================

// BuildEngineStartingMessage builds a message for engine starting
// Implements EventTypeEngineStarting per spec (0.5)
// Triggered during CLI cold start when engine is being initialized
func (b *MessageBuilder) BuildEngineStartingMessage(msg *base.ChatMessage) []slack.Block {
	content := msg.Content
	if content == "" {
		content = "Engine starting..."
	}

	// Per spec: context block with :hourglass: emoji
	text := slack.NewTextBlockObject("mrkdwn", ":hourglass: _"+content+"_", false, false)
	return []slack.Block{
		slack.NewContextBlock("", text),
	}
}

// =============================================================================
// User Message Received Message (Acknowledgment)
// =============================================================================

// BuildUserMessageReceivedMessage builds a message to acknowledge user message receipt
// Implements EventTypeUserMessageReceived per spec (0.6)
// Triggered immediately after user message is received
func (b *MessageBuilder) BuildUserMessageReceivedMessage(msg *base.ChatMessage) []slack.Block {
	// Per spec: context block with :inbox: emoji
	// Very low latency acknowledgment
	text := slack.NewTextBlockObject("mrkdwn", ":inbox: _Message received_", false, false)
	return []slack.Block{
		slack.NewContextBlock("", text),
	}
}

// =============================================================================
// Plan Approval/Denial Messages (Interactive Callbacks)
// =============================================================================

// BuildPlanApprovedBlock builds blocks to show after plan is approved
func (b *MessageBuilder) BuildPlanApprovedBlock() []slack.Block {
	text := slack.NewTextBlockObject("mrkdwn", "✓ Plan Approved\n\nClaude is now executing...", false, false)
	return []slack.Block{
		slack.NewSectionBlock(text, nil, nil),
	}
}

// BuildPlanCancelledBlock builds blocks to show after plan is cancelled
func (b *MessageBuilder) BuildPlanCancelledBlock(reason string) []slack.Block {
	text := slack.NewTextBlockObject("mrkdwn", "✕ Plan Cancelled", false, false)
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

// BuildPermissionRequestMessageFromChat builds Slack blocks for a permission request from ChatMessage
// This is the main entry point for the Build() switch statement
// Implements EventTypePermissionRequest per spec (7)
func (b *MessageBuilder) BuildPermissionRequestMessageFromChat(msg *base.ChatMessage) []slack.Block {
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
	case provider.EventTypeSessionStart:
		return base.MessageTypeSessionStart
	case provider.EventTypeEngineStarting:
		return base.MessageTypeEngineStarting
	case provider.EventTypeUserMessageReceived:
		return base.MessageTypeUserMessageReceived
	default:
		return base.MessageTypeAnswer
	}
}

// TimeToSlackTimestamp converts time.Time to Slack timestamp format
func TimeToSlackTimestamp(t time.Time) string {
	return fmt.Sprintf("%d.%d", t.Unix(), t.Nanosecond()/1000000)
}
