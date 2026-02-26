package slack

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/hrygo/hotplex/event"
	"github.com/hrygo/hotplex/provider"
)

const toolResultDurationThreshold = 500 // ms

// BlockBuilder builds Slack Block Kit messages for various event types
type BlockBuilder struct{}

// NewBlockBuilder creates a new BlockBuilder instance
func NewBlockBuilder() *BlockBuilder {
	return &BlockBuilder{}
}

// =============================================================================
// Text Object Helpers
// =============================================================================

// mrkdwnText creates a mrkdwn text object
// Used for section, context blocks that support formatting
func mrkdwnText(text string) map[string]any {
	return map[string]any{
		"type": "mrkdwn",
		"text": text,
	}
}

// plainText creates a plain_text text object
// Used for header, button text that doesn't support formatting
func plainText(text string) map[string]any {
	return map[string]any{
		"type":  "plain_text",
		"text":  text,
		"emoji": true,
	}
}

// =============================================================================
// Mrkdwn Formatting Utilities
// =============================================================================

// MrkdwnFormatter provides utilities for converting Markdown to Slack mrkdwn format
type MrkdwnFormatter struct{}

// NewMrkdwnFormatter creates a new MrkdwnFormatter
func NewMrkdwnFormatter() *MrkdwnFormatter {
	return &MrkdwnFormatter{}
}

// Format converts Markdown text to Slack mrkdwn format
// Handles: bold, italic, strikethrough, code blocks, links
// Order: links -> bold -> italic -> strikethrough -> escape (to preserve URLs and code)
func (f *MrkdwnFormatter) Format(text string) string {
	if text == "" {
		return ""
	}

	result := text

	// 1. Convert Links first (before escaping, to preserve URLs)
	result = f.convertLinks(result)

	// 2. Convert Bold: **text** or __text__ -> *text*
	result = f.convertBold(result)

	// 3. Convert Italic: *text* or _text_ -> _text_
	result = f.convertItalic(result)

	// 4. Convert Strikethrough: ~~text~~ -> ~text~
	result = f.convertStrikethrough(result)

	// 5. Escape special characters last (preserves code blocks)
	result = f.escapeSpecialChars(result)

	return result
}

// escapeSpecialChars escapes & < > for mrkdwn safely, preserving code blocks and Slack syntax
// Only skips valid Slack syntax: <!...>, <@...>, <#...>, <url|text>
func (f *MrkdwnFormatter) escapeSpecialChars(text string) string {
	var result strings.Builder
	inCodeBlock := false
	inInlineCode := false

	for i := 0; i < len(text); i++ {
		// Check for code block boundaries (```)
		if strings.HasPrefix(text[i:], "```") {
			inCodeBlock = !inCodeBlock
			result.WriteString("```")
			i += 2 // Skip the next two backticks
			continue
		}

		// Check for inline code boundaries (`)
		if text[i] == '`' && (i == 0 || text[i-1] != '\\') {
			inInlineCode = !inInlineCode
		}

		// Skip Slack special syntax: <!here>, <@user>, <#channel>, <url|text>
		if text[i] == '<' && !inCodeBlock && !inInlineCode {
			// Find closing >
			endIdx := -1
			for j := i + 1; j < len(text); j++ {
				if text[j] == '>' {
					endIdx = j
					break
				}
			}
			if endIdx != -1 {
				inner := text[i+1 : endIdx]
				// Only skip if it's valid Slack syntax
				if len(inner) > 0 && (inner[0] == '!' || inner[0] == '@' || inner[0] == '#' ||
					strings.Contains(inner, "|")) {
					result.WriteString(text[i : endIdx+1])
					i = endIdx
					continue
				}
			}
		}

		// Only escape special characters outside of code
		if !inCodeBlock && !inInlineCode {
			switch text[i] {
			case '&':
				result.WriteString("&amp;")
			case '<':
				result.WriteString("&lt;")
			case '>':
				result.WriteString("&gt;")
			default:
				result.WriteByte(text[i])
			}
			continue
		}

		// Preserve characters inside code as-is
		result.WriteByte(text[i])
	}
	return result.String()
}

// convertBold converts **text** or __text__ to *text*
func (f *MrkdwnFormatter) convertBold(text string) string {
	var result strings.Builder
	inCodeBlock := false
	i := 0
	for i < len(text) {
		// Toggle code block state
		if strings.HasPrefix(text[i:], "```") {
			inCodeBlock = !inCodeBlock
			result.WriteString("```")
			i += 3
			continue
		}
		if inCodeBlock {
			result.WriteByte(text[i])
			i++
			continue
		}

		// Handle ** or __
		if (strings.HasPrefix(text[i:], "**") || strings.HasPrefix(text[i:], "__")) && i+2 < len(text) {
			marker := text[i : i+2]
			endIdx := strings.Index(text[i+2:], marker)
			if endIdx != -1 {
				content := text[i+2 : i+2+endIdx]
				result.WriteByte('*')
				result.WriteString(content)
				result.WriteByte('*')
				i += 4 + endIdx
				continue
			}
		}
		result.WriteByte(text[i])
		i++
	}
	return result.String()
}

// convertItalic converts _text_ to _text_
// Does NOT convert *text* (Slack uses * for bold, not italic)
func (f *MrkdwnFormatter) convertItalic(text string) string {
	var result strings.Builder
	inCodeBlock := false
	i := 0
	for i < len(text) {
		// Toggle code block state
		if strings.HasPrefix(text[i:], "```") {
			inCodeBlock = !inCodeBlock
			result.WriteString("```")
			i += 3
			continue
		}
		if inCodeBlock {
			result.WriteByte(text[i])
			i++
			continue
		}

		// Handle _ for italic only (Slack uses * for bold, not italic)
		if text[i] == '_' && i+1 < len(text) {
			endIdx := strings.Index(text[i+1:], "_")
			if endIdx != -1 {
				content := text[i+1 : i+1+endIdx]
				result.WriteByte('_')
				result.WriteString(content)
				result.WriteByte('_')
				i = i + 1 + endIdx + 1
				continue
			}
		}

		result.WriteByte(text[i])
		i++
	}
	return result.String()
}

// convertStrikethrough converts ~~text~~ to ~text~
func (f *MrkdwnFormatter) convertStrikethrough(text string) string {
	var result strings.Builder
	inCodeBlock := false
	i := 0
	for i < len(text) {
		if strings.HasPrefix(text[i:], "```") {
			inCodeBlock = !inCodeBlock
			result.WriteString("```")
			i += 3
			continue
		}
		if inCodeBlock {
			result.WriteByte(text[i])
			i++
			continue
		}

		if strings.HasPrefix(text[i:], "~~") && i+2 < len(text) {
			endIdx := strings.Index(text[i+2:], "~~")
			if endIdx != -1 {
				content := text[i+2 : i+2+endIdx]
				result.WriteByte('~')
				result.WriteString(content)
				result.WriteByte('~')
				i += 4 + endIdx
				continue
			}
		}
		result.WriteByte(text[i])
		i++
	}
	return result.String()
}

// convertLinks converts [text](url) to <url|text> with URL validation
func (f *MrkdwnFormatter) convertLinks(text string) string {
	var result strings.Builder
	i := 0
	for i < len(text) {
		if text[i] == '[' {
			closeBracket := strings.Index(text[i:], "]")
			if closeBracket != -1 && i+closeBracket+1 < len(text) && text[i+closeBracket+1] == '(' {
				closeParen := strings.Index(text[i+closeBracket+1:], ")")
				if closeParen != -1 {
					linkText := text[i+1 : i+closeBracket]
					linkURL := text[i+closeBracket+2 : i+closeBracket+1+closeParen]

					// Validate URL scheme for security
					validScheme := false
					allowedSchemes := []string{"http://", "https://", "mailto:", "ftp://"}
					for _, scheme := range allowedSchemes {
						if strings.HasPrefix(linkURL, scheme) {
							validScheme = true
							break
						}
					}

					if validScheme {
						// Safe URL - convert to Slack format
						result.WriteByte('<')
						result.WriteString(linkURL)
						result.WriteByte('|')
						result.WriteString(linkText)
						result.WriteByte('>')
					} else {
						// Unsafe URL - keep as text
						result.WriteString(linkText)
						result.WriteString(" (")
						result.WriteString(linkURL)
						result.WriteString(")")
					}
					i += closeBracket + closeParen + 2
					continue
				}
			}
		}
		result.WriteByte(text[i])
		i++
	}
	return result.String()
}

// FormatCodeBlock formats code with optional language hint
func (f *MrkdwnFormatter) FormatCodeBlock(code, language string) string {
	if language == "" {
		return fmt.Sprintf("```\n%s\n```", code)
	}
	return fmt.Sprintf("```%s\n%s\n```", language, code)
}

// =============================================================================
// Block Builders - Event Type Mappings
// =============================================================================

// StatusType represents the type of status being displayed
type StatusType string

const (
	StatusThinking StatusType = "thinking"
	StatusToolUse  StatusType = "tool_use"
	StatusAnswer   StatusType = "answer"
)

// BuildStatusBlock builds a context block for status indicator
// Used for real-time status display: thinking, tool_use, answer
// This is the primary method for status messages - updated in-place via chat.update
func (b *BlockBuilder) BuildStatusBlock(statusType StatusType, content string) []map[string]any {
	var emoji, displayText string

	switch statusType {
	case StatusThinking:
		emoji = ":brain:"
		if content == "" {
			displayText = "Thinking..."
		} else {
			displayText = content
		}
	case StatusToolUse:
		emoji = getToolEmoji(content) // content is tool name
		displayText = "Tool: " + content
	case StatusAnswer:
		emoji = ":writing_hand:"
		displayText = "Answer..."
	default:
		emoji = ":hourglass:"
		displayText = content
	}

	return []map[string]any{
		{
			"type": "context",
			"elements": []map[string]any{
				mrkdwnText(fmt.Sprintf("%s _%s_", emoji, displayText)),
			},
		},
	}
}

// BuildThinkingBlock builds a context block for thinking status
// Used for: provider.EventTypeThinking
// Strategy: Send immediately (not aggregated) for instant feedback
// Deprecated: Use BuildStatusBlock(StatusThinking, content) instead
func (b *BlockBuilder) BuildThinkingBlock(content string) []map[string]any {
	return b.BuildStatusBlock(StatusThinking, content)
}

// getToolEmoji returns the appropriate emoji for a given tool name
func getToolEmoji(toolName string) string {
	mapping := map[string]string{
		"Bash":       ":computer:",
		"Edit":       ":pencil:",
		"MultiEdit":  ":pencil:",
		"Write":      ":page_facing_up:",
		"FileWrite":  ":page_facing_up:",
		"Read":       ":books:",
		"FileRead":   ":books:",
		"FileSearch": ":mag:",
		"Glob":       ":mag:",
		"WebFetch":   ":globe_with_meridians:",
		"WebSearch":  ":globe_with_meridians:",
		"Grep":       ":magnifying_glass_tilted_left:",
		"LS":         ":file_folder:",
		"List":       ":file_folder:",
		"Mkdir":      ":file_cabinet:",
		"Rmdir":      ":file_cabinet:",
		"Remove":     ":wastebasket:",
		"Delete":     ":wastebasket:",
		"Move":       ":arrow_right:",
		"Copy":       ":clipboard:",
		"Exit":       ":door:",
	}
	if emoji, ok := mapping[toolName]; ok {
		return emoji
	}
	return ":hammer_and_wrench:"
}

// BuildToolUseBlock builds a section block for tool invocation
// Used for: provider.EventTypeToolUse
// Strategy: Can be aggregated with similar tool events
func (b *BlockBuilder) BuildToolUseBlock(toolName, input string, truncated bool) []map[string]any {
	// Format input as code block
	formattedInput := fmt.Sprintf("```%s```", input)

	// Add truncation indicator if needed
	if truncated {
		formattedInput += "\n*_Output truncated..._*"
	}

	return []map[string]any{
		{
			"type": "section",
			"text": mrkdwnText(fmt.Sprintf("%s *Using tool:* `%s`", getToolEmoji(toolName), toolName)),
			"fields": []map[string]any{
				mrkdwnText("*Input:*\n" + formattedInput),
			},
		},
	}
}

// truncatePath truncates a file path for display
// Format: /very/long/path/to/file.go -> /very/long/pat.../file.go
func truncatePath(path string, maxLen int) string {
	if len(path) <= maxLen {
		return path
	}
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	// Truncate dir to fit maxLen
	halfLen := (maxLen - len(base) - 4) / 2 // 4 for "..."
	if halfLen < 3 {
		halfLen = 3
	}
	return fmt.Sprintf("%s...%s", dir[:halfLen], base)
}

// BuildToolResultBlock builds a section block for tool execution result

// BuildToolResultBlock builds a section block for tool execution result
// Used for: provider.EventTypeToolResult
// Strategy: Can be aggregated, includes optional button to expand output
func (b *BlockBuilder) BuildToolResultBlock(success bool, durationMs int64, output string, hasButton bool, toolName string, filePath string) []map[string]any {
	var blocks []map[string]any

	// Build status text
	status := "Completed"
	if !success {
		status = "Failed"
	}
	statusEmoji := getToolEmoji(toolName)
	statusText := fmt.Sprintf("*%s %s*", toolName, status) // e.g., "*Bash Completed*"

	resultBlock := map[string]any{
		"type": "section",
		"text": mrkdwnText(fmt.Sprintf("%s %s", statusEmoji, statusText)),
	}

	// Add output preview if available (truncated to 300 chars for better context)
	if output != "" {
		previewLen := 300
		preview := output
		if len(output) > previewLen {
			preview = output[:previewLen] + "..."
		}
		resultBlock["fields"] = []map[string]any{
			mrkdwnText("*Output:*\n```\n" + preview + "\n```"),
		}
	}

	blocks = append(blocks, resultBlock)

	// Add file path context block if provided
	if filePath != "" {
		displayPath := truncatePath(filePath, 50)
		blocks = append(blocks, map[string]any{
			"type": "context",
			"elements": []map[string]any{
				mrkdwnText(fmt.Sprintf(":page_facing_up: *File:* %s", displayPath)),
			},
		})
	}

	// Add metadata context block (Duration)
	// Only show duration if it exceeds threshold
	// Add metadata context block (Duration)
	// Only show duration if it exceeds threshold
	if durationMs > toolResultDurationThreshold {
		blocks = append(blocks, map[string]any{
			"type": "context",
			"elements": []map[string]any{
				mrkdwnText(fmt.Sprintf(":timer_clock: *Duration:* %s", formatDuration(durationMs))),
			},
		})
	}

	// Add action button if requested
	if hasButton && success {
		actionBlock := map[string]any{
			"type": "actions",
			"elements": []map[string]any{
				{
					"type":      "button",
					"text":      plainText("View Full Output"),
					"action_id": "view_tool_output",
					"value":     "expand_output",
				},
			},
		}
		blocks = append(blocks, actionBlock)
	}

	return blocks
}

// BuildErrorBlock builds blocks for error messages
// Used for: provider.EventTypeError, danger_block
// Strategy: Send immediately (not aggregated) for critical feedback
func (b *BlockBuilder) BuildErrorBlock(message string, isDangerBlock bool) []map[string]any {
	var blocks []map[string]any

	// Header block with emoji (Slack doesn't support style: danger for headers)
	headerEmoji := ":warning:"
	if isDangerBlock {
		headerEmoji = ":x:"
	}

	headerBlock := map[string]any{
		"type": "section",
		"text": mrkdwnText(fmt.Sprintf("*%s Execution Error*", headerEmoji)),
	}
	blocks = append(blocks, headerBlock)

	// Error message as section with mrkdwn (sanitized)
	safeMessage := SanitizeErrorMessage(fmt.Errorf("%s", message))
	errorBlock := map[string]any{
		"type": "section",
		"text": mrkdwnText(fmt.Sprintf("> %s", safeMessage)),
	}
	blocks = append(blocks, errorBlock)

	return blocks
}

// BuildAnswerBlock builds a section block for AI answer text
// Used for: provider.EventTypeAnswer
// Strategy: Stream updates via chat.update, supports mrkdwn formatting
func (b *BlockBuilder) BuildAnswerBlock(content string) []map[string]any {
	// Format content with mrkdwn
	formatter := NewMrkdwnFormatter()
	formattedContent := formatter.Format(content)

	return []map[string]any{
		{
			"type": "section",
			"text": mrkdwnText(formattedContent),
			// Enable expand for AI Assistant apps
			"expand": true,
		},
	}
}

// BuildStatsBlock builds a section block with statistics
// Used for: provider.EventTypeResult (end of turn)
// Strategy: Send as final summary
func (b *BlockBuilder) BuildStatsBlock(stats *event.EventMeta) []map[string]any {
	if stats == nil {
		return []map[string]any{}
	}

	var fields []map[string]any

	// Duration field
	if stats.TotalDurationMs > 0 {
		fields = append(fields, mrkdwnText(fmt.Sprintf("*Duration:*\n%s", formatDuration(stats.TotalDurationMs))))
	}

	// Token usage field
	if stats.InputTokens > 0 || stats.OutputTokens > 0 {
		tokenStr := fmt.Sprintf("%d in / %d out", stats.InputTokens, stats.OutputTokens)
		if stats.CacheReadTokens > 0 {
			tokenStr += fmt.Sprintf(" (cache: %d)", stats.CacheReadTokens)
		}
		fields = append(fields, mrkdwnText(fmt.Sprintf("*Tokens:*\n%s", tokenStr)))
	}

	// Cost field (if available)
	// Note: Cost tracking depends on provider implementation

	if len(fields) == 0 {
		return []map[string]any{}
	}

	return []map[string]any{
		{
			"type":   "section",
			"fields": fields,
		},
	}
}

// BuildDividerBlock creates a simple divider
func (b *BlockBuilder) BuildDividerBlock() []map[string]any {
	return []map[string]any{
		{
			"type": "divider",
		},
	}
}

// =============================================================================
// Session Statistics Blocks - Enhanced UI
// =============================================================================

// SessionStatsStyle defines the visual style for session statistics
type SessionStatsStyle string

const (
	// StatsStyleCompact - Minimal single-line summary
	StatsStyleCompact SessionStatsStyle = "compact"
	// StatsStyleCard - Rich card with emoji indicators (recommended)
	StatsStyleCard SessionStatsStyle = "card"
	// StatsStyleDetailed - Full breakdown with all metrics
	StatsStyleDetailed SessionStatsStyle = "detailed"
)

// BuildSessionStatsBlock builds a rich statistics summary block
// Used for: session_stats events at end of each turn
// Strategy: Send as final summary with visual polish
func (b *BlockBuilder) BuildSessionStatsBlock(stats *event.SessionStatsData, style SessionStatsStyle) []map[string]any {
	if stats == nil {
		return []map[string]any{}
	}

	switch style {
	case StatsStyleCompact:
		return b.buildCompactStats(stats)
	case StatsStyleDetailed:
		return b.buildDetailedStats(stats)
	case StatsStyleCard:
		fallthrough
	default:
		return b.buildCardStats(stats)
	}
}

// buildCompactStats creates a minimal single-line summary with Duration + Tokens only
// Compact style follows the design principle of showing only the most essential metrics
func (b *BlockBuilder) buildCompactStats(stats *event.SessionStatsData) []map[string]any {
	if stats.TotalTokens == 0 && stats.TotalDurationMs == 0 {
		return []map[string]any{}
	}

	var parts []string

	// Duration ONLY
	if stats.TotalDurationMs > 0 {
		parts = append(parts, fmt.Sprintf("⏱️ %s", formatDuration(stats.TotalDurationMs)))
	}

	// Tokens ONLY (In/Out)
	if stats.TotalTokens > 0 {
		parts = append(parts, fmt.Sprintf("📊 %d in / %d out", stats.InputTokens, stats.OutputTokens))
	}

	if len(parts) == 0 {
		return []map[string]any{}
	}

	return []map[string]any{
		{
			"type": "context",
			"elements": []map[string]any{
				mrkdwnText(strings.Join(parts, " • ")),
			},
		},
	}
}

// buildCardStats creates a visually appealing card-style summary (recommended)
func (b *BlockBuilder) buildCardStats(stats *event.SessionStatsData) []map[string]any {
	if stats.TotalTokens == 0 && stats.TotalDurationMs == 0 {
		return []map[string]any{}
	}

	var blocks []map[string]any

	// Header with session complete indicator
	headerBlock := map[string]any{
		"type": "header",
		"text": plainText("✅ Session Complete"),
	}
	blocks = append(blocks, headerBlock)

	// Build metrics grid (2 columns for better space usage)
	var fields []map[string]any

	// Row 1: Duration + Tokens
	fields = append(fields, mrkdwnText(fmt.Sprintf("*⏱️ Duration*\n%s", formatDuration(stats.TotalDurationMs))))
	fields = append(fields, mrkdwnText(fmt.Sprintf("*📊 Tokens*\n%d in / %d out", stats.InputTokens, stats.OutputTokens)))

	// Row 2: Cost + Model (if available)
	if stats.TotalCostUSD > 0.0001 {
		fields = append(fields, mrkdwnText(fmt.Sprintf("*💰 Cost*\n$%.4f", stats.TotalCostUSD)))
	} else {
		fields = append(fields, mrkdwnText("*💰 Cost*\n_Usage-based_"))
	}

	if stats.ModelUsed != "" {
		modelShort := stats.ModelUsed
		if len(modelShort) > 20 {
			modelShort = modelShort[:17] + "..."
		}
		fields = append(fields, mrkdwnText(fmt.Sprintf("*🤖 Model*\n%s", modelShort)))
	}

	// Row 3: Tools (if used)
	if len(stats.ToolsUsed) > 0 {
		toolsStr := strings.Join(stats.ToolsUsed, ", ")
		if len(toolsStr) > 40 {
			toolsStr = toolsStr[:37] + "..."
		}
		fields = append(fields, mrkdwnText(fmt.Sprintf("*🔧 Tools Used*\n%s", toolsStr)))
	}

	// Row 4: Files (if modified)
	if stats.FilesModified > 0 {
		fields = append(fields, mrkdwnText(fmt.Sprintf("*📁 Files Modified*\n%d file(s)", stats.FilesModified)))
	}

	// Ensure even number of fields for proper 2-column layout
	if len(fields)%2 != 0 {
		fields = append(fields, mrkdwnText("*_*\n_")) // Empty placeholder
	}

	if len(fields) > 0 {
		fieldsBlock := map[string]any{
			"type":   "section",
			"fields": fields,
		}
		blocks = append(blocks, fieldsBlock)
	}

	// Add cache info if present
	if stats.CacheReadTokens > 0 || stats.CacheWriteTokens > 0 {
		cacheText := "📦 *Cache: "
		if stats.CacheReadTokens > 0 {
			cacheText += fmt.Sprintf("read %d", stats.CacheReadTokens)
		}
		if stats.CacheWriteTokens > 0 {
			if stats.CacheReadTokens > 0 {
				cacheText += ", "
			}
			cacheText += fmt.Sprintf("write %d", stats.CacheWriteTokens)
		}
		cacheText += "*"

		cacheBlock := map[string]any{
			"type": "context",
			"elements": []map[string]any{
				mrkdwnText(cacheText),
			},
		}
		blocks = append(blocks, cacheBlock)
	}

	// Add file paths if available (up to 3)
	if len(stats.FilePaths) > 0 {
		var filesText string
		maxFiles := 3
		if len(stats.FilePaths) > maxFiles {
			filesText = fmt.Sprintf("📄 *%d files modified:* ", len(stats.FilePaths))
			for i := 0; i < maxFiles; i++ {
				if i > 0 {
					filesText += ", "
				}
				// Extract just filename from path
				parts := strings.Split(stats.FilePaths[i], "/")
				filesText += "`" + parts[len(parts)-1] + "`"
			}
			filesText += " _and more_"
		} else {
			filesText = "📄 *Files modified:* "
			for i, p := range stats.FilePaths {
				if i > 0 {
					filesText += ", "
				}
				parts := strings.Split(p, "/")
				filesText += "`" + parts[len(parts)-1] + "`"
			}
		}

		filesBlock := map[string]any{
			"type": "context",
			"elements": []map[string]any{
				mrkdwnText(filesText),
			},
		}
		blocks = append(blocks, filesBlock)
	}

	return blocks
}

// buildDetailedStats creates a comprehensive breakdown with all metrics
func (b *BlockBuilder) buildDetailedStats(stats *event.SessionStatsData) []map[string]any {
	if stats.TotalTokens == 0 && stats.TotalDurationMs == 0 {
		return []map[string]any{}
	}

	var blocks []map[string]any

	// Header
	headerBlock := map[string]any{
		"type": "header",
		"text": plainText("📊 Session Statistics Report"),
	}
	blocks = append(blocks, headerBlock)

	// Performance Section
	perfBlock := map[string]any{
		"type": "section",
		"text": mrkdwnText("*⚡ Performance*"),
	}
	blocks = append(blocks, perfBlock)

	var perfFields []map[string]any
	perfFields = append(perfFields, mrkdwnText(fmt.Sprintf("*Total Duration*\n%s", formatDuration(stats.TotalDurationMs))))

	if stats.ThinkingDurationMs > 0 {
		perfFields = append(perfFields, mrkdwnText(fmt.Sprintf("*Thinking*\n%s", formatDuration(stats.ThinkingDurationMs))))
	}
	if stats.ToolDurationMs > 0 {
		perfFields = append(perfFields, mrkdwnText(fmt.Sprintf("*Tool Execution*\n%s", formatDuration(stats.ToolDurationMs))))
	}
	if stats.GenerationDurationMs > 0 {
		perfFields = append(perfFields, mrkdwnText(fmt.Sprintf("*Generation*\n%s", formatDuration(stats.GenerationDurationMs))))
	}

	if len(perfFields)%2 != 0 {
		perfFields = append(perfFields, mrkdwnText("*_*\n_"))
	}
	perfBlock["fields"] = perfFields

	// Token Usage Section
	tokenBlock := map[string]any{
		"type": "section",
		"text": mrkdwnText("*📈 Token Usage*"),
	}
	blocks = append(blocks, tokenBlock)

	var tokenFields []map[string]any
	tokenFields = append(tokenFields, mrkdwnText(fmt.Sprintf("*Input*\n%d tokens", stats.InputTokens)))
	tokenFields = append(tokenFields, mrkdwnText(fmt.Sprintf("*Output*\n%d tokens", stats.OutputTokens)))
	tokenFields = append(tokenFields, mrkdwnText(fmt.Sprintf("*Total*\n%d tokens", stats.TotalTokens)))

	if stats.CacheReadTokens > 0 {
		tokenFields = append(tokenFields, mrkdwnText(fmt.Sprintf("*Cache Read*\n%d tokens", stats.CacheReadTokens)))
	}
	if stats.CacheWriteTokens > 0 {
		tokenFields = append(tokenFields, mrkdwnText(fmt.Sprintf("*Cache Write*\n%d tokens", stats.CacheWriteTokens)))
	}

	if len(tokenFields)%2 != 0 {
		tokenFields = append(tokenFields, mrkdwnText("*_*\n_"))
	}
	tokenBlock["fields"] = tokenFields

	// Cost Section
	if stats.TotalCostUSD > 0 {
		costBlock := map[string]any{
			"type": "section",
			"text": mrkdwnText(fmt.Sprintf("*💰 Total Cost*: `$%.4f USD`", stats.TotalCostUSD)),
		}
		blocks = append(blocks, costBlock)
	}

	// Tools Section
	if len(stats.ToolsUsed) > 0 {
		toolsBlock := map[string]any{
			"type": "section",
			"text": mrkdwnText(fmt.Sprintf("*🔧 Tools Invoked* (%d total)*\n`%s`",
				stats.ToolCallCount, strings.Join(stats.ToolsUsed, "`, `"))),
		}
		blocks = append(blocks, toolsBlock)
	}

	// Files Section
	if len(stats.FilePaths) > 0 {
		filesBlock := map[string]any{
			"type": "section",
			"text": mrkdwnText(fmt.Sprintf("*📁 Files Modified* (%d total)*\n`%s`",
				len(stats.FilePaths), strings.Join(stats.FilePaths, "`, `"))),
		}
		blocks = append(blocks, filesBlock)
	}

	return blocks
}

// =============================================================================
// Helper Functions
// =============================================================================

// formatDuration converts milliseconds to human-readable duration
func formatDuration(ms int64) string {
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	seconds := float64(ms) / 1000.0
	return fmt.Sprintf("%.1fs", seconds)
}

// TruncateText truncates text to max length with ellipsis
func TruncateText(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}
	return text[:maxLen] + "..."
}

// =============================================================================
// Permission Request Blocks (Issue #39)
// =============================================================================

// BuildPermissionRequestBlocks builds Slack Block Kit for a Claude Code permission request.
// It displays the tool name, command preview, and approval/denial buttons.
func BuildPermissionRequestBlocks(req *provider.PermissionRequest, sessionID string) []map[string]any {
	tool, input := req.GetToolAndInput()

	// Sanitize and truncate commands for preview
	safeInput := SanitizeCommand(input)
	displayInput := safeInput
	if RuneCount(displayInput) > 500 {
		displayInput = TruncateByRune(displayInput, 497) + "..."
	}

	blocks := []map[string]any{}

	// Header
	blocks = append(blocks, map[string]any{
		"type": "header",
		"text": plainText("⚠️ Permission Request"),
	})

	// Tool information
	if tool != "" {
		blocks = append(blocks, map[string]any{
			"type": "section",
			"text": mrkdwnText(fmt.Sprintf("*Tool:* `%s`", tool)),
		})
	}

	// Command/Action preview
	if displayInput != "" {
		blocks = append(blocks, map[string]any{
			"type": "section",
			"text": mrkdwnText(fmt.Sprintf("*Command:*\n```\n%s\n```", displayInput)),
		})
	}

	// Decision reason (if available)
	if req.Decision != nil && req.Decision.Reason != "" {
		blocks = append(blocks, map[string]any{
			"type": "context",
			"elements": []map[string]any{
				mrkdwnText(fmt.Sprintf("*Reason:* %s", req.Decision.Reason)),
			},
		})
	}

	// Session info
	blocks = append(blocks, map[string]any{
		"type": "context",
		"elements": []map[string]any{
			mrkdwnText(fmt.Sprintf("Session: `%s`", sessionID)),
		},
	})

	// Action buttons with validated block_id
	blockID := ValidateBlockID(fmt.Sprintf("perm_%s", req.MessageID))
	blocks = append(blocks, map[string]any{
		"type":     "actions",
		"block_id": blockID,
		"elements": []map[string]any{
			{
				"type":      "button",
				"text":      plainText("✅ Allow"),
				"action_id": "perm_allow",
				"style":     "primary",
				"value":     fmt.Sprintf("allow:%s:%s", sessionID, req.MessageID),
			},
			{
				"type":      "button",
				"text":      plainText("🚫 Deny"),
				"action_id": "perm_deny",
				"style":     "danger",
				"value":     fmt.Sprintf("deny:%s:%s", sessionID, req.MessageID),
			},
		},
	})

	return blocks
}

// BuildPermissionApprovedBlocks builds blocks to show after permission is approved.
func BuildPermissionApprovedBlocks(tool, input string) []map[string]any {
	// Truncate for display
	displayInput := input
	if len(displayInput) > 200 {
		displayInput = displayInput[:197] + "..."
	}

	return []map[string]any{
		{
			"type": "section",
			"text": mrkdwnText(fmt.Sprintf("✅ *Permission Granted*\n\nTool: `%s`\nCommand: `%s`", tool, displayInput)),
		},
	}
}

// BuildPermissionDeniedBlocks builds blocks to show after permission is denied.
func BuildPermissionDeniedBlocks(tool, input, reason string) []map[string]any {
	// Truncate for display
	displayInput := input
	if len(displayInput) > 200 {
		displayInput = displayInput[:197] + "..."
	}

	blocks := []map[string]any{
		{
			"type": "section",
			"text": mrkdwnText(fmt.Sprintf("🚫 *Permission Denied*\n\nTool: `%s`\nCommand: `%s`", tool, displayInput)),
		},
	}

	if reason != "" {
		blocks = append(blocks, map[string]any{
			"type": "context",
			"elements": []map[string]any{
				mrkdwnText(fmt.Sprintf("Reason: %s", reason)),
			},
		})
	}

	return blocks
}

// =============================================================================
// Chunking Integration
// =============================================================================

// BuildChunkedAnswerBlocks builds answer blocks with automatic chunking
// If content exceeds SlackTextLimit, it will be split into multiple blocks
func (b *BlockBuilder) BuildChunkedAnswerBlocks(content string) [][]map[string]any {
	if len(content) <= SlackTextLimit {
		return [][]map[string]any{b.BuildAnswerBlock(content)}
	}

	// Use chunker to split content
	chunks := ChunkMessageMarkdown(content, SlackTextLimit)

	var allBlocks [][]map[string]any
	for i, chunk := range chunks {
		// Add chunk indicator for multi-chunk messages
		if len(chunks) > 1 {
			chunkWithIndicator := fmt.Sprintf("%s\n\n*(Part %d of %d)*", chunk, i+1, len(chunks))
			allBlocks = append(allBlocks, b.BuildAnswerBlock(chunkWithIndicator))
		} else {
			allBlocks = append(allBlocks, b.BuildAnswerBlock(chunk))
		}
	}

	return allBlocks
}

// BuildChunkedSectionBlocks builds section blocks with automatic chunking
func (b *BlockBuilder) BuildChunkedSectionBlocks(text string) [][]map[string]any {
	if len(text) <= MaxSectionTextLen {
		return [][]map[string]any{BuildSectionBlock(text, nil, nil)}
	}

	truncated := TruncateMrkdwn(text, MaxSectionTextLen)
	return [][]map[string]any{BuildSectionBlock(truncated, nil, nil)}
}

// ValidateAndTruncateBlocks validates blocks and truncates if necessary
func ValidateAndTruncateBlocks(blocks []map[string]any) ([]map[string]any, error) {
	if err := ValidateBlocks(blocks, false); err != nil {
		// Try to fix by truncating text
		for i, block := range blocks {
			if text, ok := block["text"].(map[string]any); ok {
				if textStr, ok := text["text"].(string); ok {
					if len(textStr) > MaxSectionTextLen {
						blocks[i]["text"] = mrkdwnText(TruncateMrkdwn(textStr, MaxSectionTextLen))
					}
				}
			}
		}

		// Validate again
		if err := ValidateBlocks(blocks, false); err != nil {
			return blocks, err
		}
	}

	return blocks, nil
}
