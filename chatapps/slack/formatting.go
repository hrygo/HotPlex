// Package slack provides the Slack adapter implementation for the hotplex engine.
// Markdown to Slack mrkdwn conversion utilities with CommonMark support.
package slack

import (
	"fmt"
	"strings"
)

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
//
// Conversion order follows CommonMark specification precedence:
// 1. Block-level structures first (headings, lists, blockquotes, code blocks)
// 2. Code spans protected (highest inline precedence per CommonMark)
// 3. Links (before emphasis, as link content shouldn't be emphasized)
// 4. Emphasis (bold, italic, strikethrough)
// 5. Special character escaping (last, to avoid breaking syntax)
//
// Reference: https://spec.commonmark.org/0.31.2/chapter-6/
func (f *MrkdwnFormatter) Format(text string) string {
	if text == "" {
		return ""
	}

	result := text

	// ================================================================
	// PHASE 1: Block-level structures (line-based transformations)
	// These must run first as they define document structure
	// ================================================================

	// 1. Convert Headings: # H1 -> *H1* (Slack uses bold for headings)
	// Must run before inline formatting to avoid processing heading content
	result = f.convertHeadings(result)

	// 2. Convert Lists: - item -> • item
	// Block-level list items, runs before inline formatting
	result = f.convertLists(result)

	// 3. Convert Blockquotes: > quote -> > quote
	// Uses BLOCKQUOTE_START marker to protect from escaping
	result = f.convertBlockquotes(result)

	// ================================================================
	// PHASE 2: Inline structures (within-line transformations)
	// Order follows CommonMark precedence rules
	// ================================================================

	// 4. Protect code spans by extracting them
	// Code spans have HIGHEST precedence per CommonMark spec
	// Content inside `code` and ```code``` should not be parsed
	codePlaceholders, result := f.extractCodeSpans(result)

	// 5. Convert Links: [text](url) -> <url|text>
	// Links take precedence over emphasis - link content shouldn't be bold/italic
	result = f.convertLinks(result)

	// 6. Convert Bold: **text** or __text__ -> *text* (Slack bold format)
	// Emphasis has lower precedence than code and links
	result = f.convertBold(result)

	// 7. Convert Italic: _text_ -> _text_ (Slack italic format)
	// Single underscore italic only (*text* is Slack bold, not Markdown italic)
	result = f.convertItalic(result)

	// 8. Convert Strikethrough: ~~text~~ -> ~text~ (Slack strike format)
	result = f.convertStrikethrough(result)

	// ================================================================
	// PHASE 3: Restoration and escaping
	// ================================================================

	// 9. Escape special characters: & < > -> &amp; &lt; &gt;
	// Must run BEFORE restoring code spans
	// Placeholders protect the code content from being escaped
	result = f.escapeSpecialChars(result)

	// 10. Restore code spans (replace placeholders with original code)
	// This brings back the original code with its special characters intact
	result = f.restoreCodeSpans(result, codePlaceholders)

	// 11. Restore blockquote markers
	result = strings.ReplaceAll(result, "BLOCKQUOTE_START", "> ")

	return result
}

// escapeSpecialChars escapes & < > for mrkdwn safely
// Code spans are already protected via placeholders, so we only need to:
// 1. Escape special characters outside of Slack syntax
// 2. Preserve Slack syntax: <!...>, <@...>, <#...>, <url|text>
func (f *MrkdwnFormatter) escapeSpecialChars(text string) string {
	var result strings.Builder

	for i := 0; i < len(text); i++ {
		// Skip Slack special syntax: <!here>, <@user>, <#channel>, <url|text>
		if text[i] == '<' {
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

		// Escape special characters
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

// convertItalic converts _text_ to _text_ (Slack italic format)
// Does NOT convert *text* because:
//   - **text** was already converted to *text* (Slack bold) by convertBold
//   - *text* in Markdown is italic, but in Slack mrkdwn *text* is bold
//   - So we only convert _text_ (underscore italic) to _text_ (Slack italic)
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

		// Handle _text_ (but not __text__ which is handled by convertBold)
		if text[i] == '_' && i+1 < len(text) && text[i+1] != '_' {
			endIdx := strings.Index(text[i+1:], "_")
			if endIdx != -1 {
				content := text[i+1 : i+1+endIdx]
				result.WriteByte('_')
				result.WriteString(content)
				result.WriteByte('_')
				i += 2 + endIdx
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

		// Handle ~~
		if strings.HasPrefix(text[i:], "~~") {
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

// convertHeadings converts Markdown headings to Slack bold format
// # H1 -> *H1*, ## H2 -> *H2*, ### H3 -> *H3*, etc.
// Slack mrkdwn doesn't support headings, so we use bold as the closest equivalent
func (f *MrkdwnFormatter) convertHeadings(text string) string {
	var result strings.Builder
	inCodeBlock := false
	lines := strings.Split(text, "\n")

	for lineIdx, line := range lines {
		if lineIdx > 0 {
			result.WriteByte('\n')
		}

		// Check if we're in a code block
		if strings.HasPrefix(line, "```") {
			inCodeBlock = !inCodeBlock
			result.WriteString(line)
			continue
		}

		if inCodeBlock {
			result.WriteString(line)
			continue
		}

		// Match headings: # H1, ## H2, ### H3, #### H4, ##### H5, ###### H6
		// Must be at the start of line and followed by a space
		trimmed := strings.TrimLeft(line, "#")
		hashCount := len(line) - len(trimmed)

		if hashCount > 0 && hashCount <= 6 && len(trimmed) > 0 && trimmed[0] == ' ' {
			headingText := strings.TrimSpace(trimmed)
			// Convert to *heading* (bold in mrkdwn)
			result.WriteString("*" + headingText + "*")
		} else {
			result.WriteString(line)
		}
	}

	return result.String()
}

// convertLists converts Markdown lists to Slack bullet format
// - item -> • item
// * item -> • item
// + item -> • item
// 1. item -> • item
// 2. item -> • item
func (f *MrkdwnFormatter) convertLists(text string) string {
	var result strings.Builder
	inCodeBlock := false
	lines := strings.Split(text, "\n")

	for lineIdx, line := range lines {
		if lineIdx > 0 {
			result.WriteByte('\n')
		}

		// Check if we're in a code block
		if strings.HasPrefix(line, "```") {
			inCodeBlock = !inCodeBlock
			result.WriteString(line)
			continue
		}

		if inCodeBlock {
			result.WriteString(line)
			continue
		}

		// Match unordered lists: -, *, + followed by space
		trimmed := strings.TrimLeft(line, " \t")
		if len(trimmed) > 1 {
			firstChar := trimmed[0]
			secondChar := trimmed[1]
			if (firstChar == '-' || firstChar == '*' || firstChar == '+') && secondChar == ' ' {
				// Replace with bullet point
				listContent := strings.TrimSpace(trimmed[2:])
				indent := len(line) - len(trimmed)
				result.WriteString(strings.Repeat(" ", indent) + "• " + listContent)
				continue
			}
		}

		// Match ordered lists: 1. item, 2. item, etc.
		if len(trimmed) > 2 {
			// Check for digit(s) followed by ". "
			dotIdx := strings.Index(trimmed, ". ")
			if dotIdx > 0 {
				isOrdered := true
				for _, ch := range trimmed[:dotIdx] {
					if ch < '0' || ch > '9' {
						isOrdered = false
						break
					}
				}
				if isOrdered {
					listContent := strings.TrimSpace(trimmed[dotIdx+2:])
					indent := len(line) - len(trimmed)
					result.WriteString(strings.Repeat(" ", indent) + "• " + listContent)
					continue
				}
			}
		}

		result.WriteString(line)
	}

	return result.String()
}

// convertBlockquotes converts Markdown blockquotes to Slack format
// > quote -> > quote
// Slack mrkdwn supports blockquotes with the > prefix
// This must run BEFORE escapeSpecialChars to prevent > from being escaped
func (f *MrkdwnFormatter) convertBlockquotes(text string) string {
	var result strings.Builder
	inCodeBlock := false
	lines := strings.Split(text, "\n")

	for lineIdx, line := range lines {
		if lineIdx > 0 {
			result.WriteByte('\n')
		}

		// Check if we're in a code block
		if strings.HasPrefix(line, "```") {
			inCodeBlock = !inCodeBlock
			result.WriteString(line)
			continue
		}

		if inCodeBlock {
			result.WriteString(line)
			continue
		}

		// Match blockquote: > followed by space or end of line
		trimmed := strings.TrimLeft(line, " \t")
		if len(trimmed) > 0 && trimmed[0] == '>' {
			// Already a blockquote, ensure proper format
			quoteContent := strings.TrimSpace(trimmed[1:])
			// Use a placeholder that won't be escaped, then convert back later
			// Actually, we need to protect the > from escapeSpecialChars
			// Strategy: use a special marker
			result.WriteString("BLOCKQUOTE_START" + quoteContent)
		} else {
			result.WriteString(line)
		}
	}

	return result.String()
}

// convertLinks converts [text](url) to <url|text>
func (f *MrkdwnFormatter) convertLinks(text string) string {
	var result strings.Builder
	i := 0
	for i < len(text) {
		// Check for [text](url) pattern
		if text[i] == '[' {
			closeBracket := strings.Index(text[i+1:], "]")
			if closeBracket != -1 {
				linkText := text[i+1 : i+1+closeBracket]
				openParen := i + 1 + closeBracket + 1
				if openParen < len(text) && text[openParen] == '(' {
					closeParen := strings.Index(text[openParen+1:], ")")
					if closeParen != -1 {
						url := text[openParen+1 : openParen+1+closeParen]
						// Only convert if URL is not empty
						if url != "" {
							link := "<" + url + "|" + linkText + ">"
							result.WriteString(link)
							i = openParen + 1 + closeParen + 1
							continue
						}
					}
				}
			}
		}
		result.WriteByte(text[i])
		i++
	}
	return result.String()
}

// =============================================================================
// Code Span Protection (CommonMark Highest Precedence)
// =============================================================================

// codeSpanPlaceholder is used to temporarily replace code spans during processing
type codeSpanPlaceholder struct {
	original string
}

// extractCodeSpans extracts inline code spans (`code`) and code blocks (```code```)
// and replaces them with placeholders to protect them from other transformations
// Returns the map of placeholders to original code, and the text with replacements
func (f *MrkdwnFormatter) extractCodeSpans(text string) (map[string]codeSpanPlaceholder, string) {
	placeholders := make(map[string]codeSpanPlaceholder)
	var result strings.Builder
	placeholderCounter := 0

	inCodeBlock := false
	codeBlockStart := -1

	i := 0
	for i < len(text) {
		// Check for code block boundaries (```)
		if strings.HasPrefix(text[i:], "```") {
			if !inCodeBlock {
				// Starting a code block
				inCodeBlock = true
				codeBlockStart = i
				// Find the end of the opening ```
				i += 3
				// Skip language identifier if present (until newline)
				newlineIdx := strings.Index(text[i:], "\n")
				if newlineIdx != -1 {
					i = i + newlineIdx + 1
				}
				continue
			} else {
				// Ending a code block
				// Extract the full code block including markers
				fullBlock := text[codeBlockStart : i+3]
				placeholder := fmt.Sprintf("%%CODESPAN%d%%", placeholderCounter)
				placeholders[placeholder] = codeSpanPlaceholder{original: fullBlock}
				result.WriteString(placeholder)
				placeholderCounter++
				inCodeBlock = false
				i += 3
				continue
			}
		}

		if inCodeBlock {
			// Accumulate code block content (already handled language line)
			// Just move to the next character
			i++
			continue
		}

		// Check for inline code (`code`)
		if text[i] == '`' {
			// Count consecutive backticks
			backtickCount := 0
			for j := i; j < len(text) && text[j] == '`'; j++ {
				backtickCount++
			}

			if backtickCount == 1 {
				// Single backtick - inline code
				// Find closing backtick
				endIdx := -1
				for j := i + 1; j < len(text); j++ {
					if text[j] == '`' && (j == 0 || text[j-1] != '\\') {
						endIdx = j
						break
					}
				}
				if endIdx != -1 {
					// Found closing backtick
					inlineCode := text[i : endIdx+1]
					placeholder := fmt.Sprintf("%%CODESPAN%d%%", placeholderCounter)
					placeholders[placeholder] = codeSpanPlaceholder{original: inlineCode}
					result.WriteString(placeholder)
					placeholderCounter++
					i = endIdx + 1
					continue
				}
			}
		}

		result.WriteByte(text[i])
		i++
	}

	// Handle unclosed code block (treat rest as code)
	if inCodeBlock {
		fullBlock := text[codeBlockStart:]
		placeholder := fmt.Sprintf("%%CODESPAN%d%%", placeholderCounter)
		placeholders[placeholder] = codeSpanPlaceholder{original: fullBlock}
		result.WriteString(placeholder)
	}

	return placeholders, result.String()
}

// restoreCodeSpans replaces placeholders with original code spans
func (f *MrkdwnFormatter) restoreCodeSpans(text string, placeholders map[string]codeSpanPlaceholder) string {
	result := text
	for placeholder, span := range placeholders {
		result = strings.ReplaceAll(result, placeholder, span.original)
	}
	return result
}

// FormatCodeBlock formats a code block with optional language
func (f *MrkdwnFormatter) FormatCodeBlock(code, language string) string {
	if language == "" {
		return fmt.Sprintf("```\n%s\n```", code)
	}
	return fmt.Sprintf("```%s\n%s\n```", language, code)
}

// =============================================================================
// Slack Special Syntax Formatters
// =============================================================================

// FormatChannelMention creates a channel mention: <#C123|channel-name>
func FormatChannelMention(channelID, channelName string) string {
	return fmt.Sprintf("<#%s|%s>", channelID, channelName)
}

// FormatChannelMentionByID creates a channel mention with just ID: <#C123>
func FormatChannelMentionByID(channelID string) string {
	return fmt.Sprintf("<#%s>", channelID)
}

// FormatUserMention creates a user mention: <@U123|username>
func FormatUserMention(userID, userName string) string {
	return fmt.Sprintf("<@%s|%s>", userID, userName)
}

// FormatUserMentionByID creates a user mention with just ID: <@U123>
func FormatUserMentionByID(userID string) string {
	return fmt.Sprintf("<@%s>", userID)
}

// FormatSpecialMention creates a special mention: <!here>, <!channel>, <!everyone>
func FormatSpecialMention(mentionType string) string {
	// mentionType: "here", "channel", "everyone"
	return fmt.Sprintf("<!%s>", mentionType)
}

// FormatHereMention creates a @here mention
func FormatHereMention() string {
	return "<!here>"
}

// FormatChannelMention creates a @channel mention
func FormatChannelAllMention() string {
	return "<!channel>"
}

// FormatEveryoneMention creates a @everyone mention
func FormatEveryoneMention() string {
	return "<!everyone>"
}

// FormatDateTime creates a date formatting: <!date^timestamp^format|fallback>
// Reference: https://api.slack.com/reference/surfaces/formatting#date-formatting
func FormatDateTime(timestamp int64, format, fallback string) string {
	return fmt.Sprintf("<!date^%d^%s|%s>", timestamp, format, fallback)
}

// FormatDateTimeWithLink creates a date formatting with link: <!date^timestamp^format^link|fallback>
func FormatDateTimeWithLink(timestamp int64, format, linkURL, fallback string) string {
	return fmt.Sprintf("<!date^%d^%s^%s|%s>", timestamp, format, linkURL, fallback)
}

// FormatDate creates a simple date formatting
func FormatDate(timestamp int64) string {
	return FormatDateTime(timestamp, "{date}", "Unknown date")
}

// FormatDateShort creates a short date formatting (e.g., "Jan 1, 2024")
func FormatDateShort(timestamp int64) string {
	return FormatDateTime(timestamp, "{date_short}", "Unknown date")
}

// FormatDateLong creates a long date formatting (e.g., "Monday, January 1, 2024")
func FormatDateLong(timestamp int64) string {
	return FormatDateTime(timestamp, "{date_long}", "Unknown date")
}

// FormatTime creates a time formatting (e.g., "2:30 PM")
func FormatTime(timestamp int64) string {
	return FormatDateTime(timestamp, "{time}", "Unknown time")
}

// FormatDateTimeCombined creates combined date and time formatting
func FormatDateTimeCombined(timestamp int64) string {
	return FormatDateTime(timestamp, "{date} at {time}", "Unknown datetime")
}

// FormatURL creates a link: <url|text> or <url>
func FormatURL(url, text string) string {
	if text == "" {
		return fmt.Sprintf("<%s>", url)
	}
	return fmt.Sprintf("<%s|%s>", url, text)
}

// FormatEmail creates an email link
func FormatEmail(email string) string {
	return fmt.Sprintf("<mailto:%s|%s>", email, email)
}

// FormatCommand creates a command formatting
func FormatCommand(command string) string {
	return fmt.Sprintf("</%s>", command)
}

// FormatSubteamMention creates a user group mention: <!subteam^S123|@group>
func FormatSubteamMention(subteamID, subteamHandle string) string {
	return fmt.Sprintf("<!subteam^%s|%s>", subteamID, subteamHandle)
}

// FormatObject creates an object mention (for boards, clips, etc.)
func FormatObject(objectType, objectID, objectText string) string {
	return fmt.Sprintf("<%s://%s|%s>", objectType, objectID, objectText)
}
