// Package slack provides the Slack adapter implementation for the hotplex engine.
// Tool message builders for Slack Block Kit.
package slack

import (
	"fmt"
	"strings"

	"github.com/hrygo/hotplex/chatapps/base"
	"github.com/slack-go/slack"
)

// ToolMessageBuilder builds tool-related Slack messages (ToolUse, ToolResult)
type ToolMessageBuilder struct {
	formatter *MrkdwnFormatter
}

// NewToolMessageBuilder creates a new ToolMessageBuilder
func NewToolMessageBuilder(formatter *MrkdwnFormatter) *ToolMessageBuilder {
	return &ToolMessageBuilder{
		formatter: formatter,
	}
}

// BuildToolUseMessage builds a message for tool invocation
// Implements EventTypeToolUse per spec - uses fields dual-column layout, parameter summary 12 chars
// Supports aggregated messages: if metadata contains "_original_messages", builds blocks for each.
func (b *ToolMessageBuilder) BuildToolUseMessage(msg *base.ChatMessage) []slack.Block {
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

// buildSingleToolUseBlock renders a single tool invocation as an ultra-compact single-line Context Block
// Format: 🛠️ {tool} | args: {summary}
func (b *ToolMessageBuilder) buildSingleToolUseBlock(msg *base.ChatMessage) []slack.Block {
	toolName := msg.Content
	if toolName == "" {
		toolName = "Unknown Tool"
	}

	toolEmoji := getToolEmoji(toolName)

	// Extract tool input from metadata
	input := ""
	if msg.Metadata != nil {
		if summary, ok := msg.Metadata["input_summary"].(string); ok && summary != "" {
			input = summary
		} else if in, ok := msg.Metadata["input"].(string); ok {
			input = in
		}
	}

	// Truncate input for summary display
	if len(input) > 60 {
		input = input[:60] + "…"
	}

	// Single-line compact Context Block: 🛠️ tool_name | args: summary
	line := toolEmoji + " `" + toolName + "`"
	if input != "" {
		line += "  |  `" + input + "`"
	}
	text := slack.NewTextBlockObject("mrkdwn", line, false, false)
	return []slack.Block{slack.NewContextBlock("", text)}
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

// BuildToolResultMessage builds a message for tool execution result
// Implements EventTypeToolResult per spec - shows status, duration, and data length
// Supports aggregated messages: if metadata contains "_original_messages", builds blocks for each.
func (b *ToolMessageBuilder) BuildToolResultMessage(msg *base.ChatMessage) []slack.Block {
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
func (b *ToolMessageBuilder) buildSingleToolResultBlock(msg *base.ChatMessage) []slack.Block {
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

	// Check if this is a skill tool call (simplify output for skill tools)
	isSkillTool := strings.HasPrefix(toolName, "skill:") ||
		strings.Contains(strings.ToLower(toolName), "skill") ||
		strings.HasPrefix(toolName, "simplify") ||
		strings.HasPrefix(toolName, "loop") ||
		strings.HasPrefix(toolName, "commit") ||
		strings.HasPrefix(toolName, "avatar")

	// For skill tools, show simplified output: just tool name and status
	if isSkillTool {
		icon := ":white_check_mark:"
		if !success {
			icon = ":warning:"
		}

		// Extract skill name from tool name (e.g., "skill:simplify" -> "simplify")
		displayName := toolName
		if strings.HasPrefix(toolName, "skill:") {
			displayName = strings.TrimPrefix(toolName, "skill:")
		}

		statusText := fmt.Sprintf("%s *Skill:* `%s`", icon, displayName)
		statusObj := slack.NewTextBlockObject("mrkdwn", statusText, false, false)
		blocks = append(blocks, slack.NewSectionBlock(statusObj, nil, nil))
		return blocks
	}

	// Original logic for non-skill tools
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
