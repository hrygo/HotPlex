package chatapps

import (
	"context"
	"log/slog"
	"strings"

	"github.com/hrygo/hotplex/chatapps/base"
)

// FormatConversionProcessor converts message content to platform-specific formats
type FormatConversionProcessor struct {
	logger *slog.Logger
}

// NewFormatConversionProcessor creates a new FormatConversionProcessor
func NewFormatConversionProcessor(logger *slog.Logger) *FormatConversionProcessor {
	if logger == nil {
		logger = slog.Default()
	}
	return &FormatConversionProcessor{
		logger: logger,
	}
}

// Name returns the processor name
func (p *FormatConversionProcessor) Name() string {
	return "FormatConversionProcessor"
}

// Order returns the processor order
func (p *FormatConversionProcessor) Order() int {
	return int(OrderFormatConversion)
}

// Process converts message content based on platform
func (p *FormatConversionProcessor) Process(ctx context.Context, msg *base.ChatMessage) (*base.ChatMessage, error) {
	if msg.Content == "" {
		return msg, nil
	}

	// Check if content should be converted
	parseMode := base.ParseModeNone
	if msg.RichContent != nil {
		parseMode = msg.RichContent.ParseMode
	}

	// If no parse mode specified, check metadata for hints
	if parseMode == base.ParseModeNone {
		if mode, ok := msg.Metadata["parse_mode"].(string); ok {
			switch strings.ToLower(mode) {
			case "markdown":
				parseMode = base.ParseModeMarkdown
			case "html":
				parseMode = base.ParseModeHTML
			}
		}
	}

	// Convert based on platform and parse mode
	switch msg.Platform {
	case "slack":
		if parseMode == base.ParseModeMarkdown {
			msg.Content = convertMarkdownToSlackMrkdwn(msg.Content)
			p.logger.Debug("Converted markdown to Slack mrkdwn",
				"session_id", msg.SessionID,
				"content_len", len(msg.Content))
		}
	case "discord":
		// Discord uses standard markdown, no conversion needed
		// But we can enhance if needed
	case "telegram":
		// Telegram supports HTML or Markdown v2
		if parseMode == base.ParseModeHTML {
			// Content already in HTML format
			p.logger.Debug("Using HTML parse mode for Telegram",
				"session_id", msg.SessionID)
		}
	}

	return msg, nil
}

// convertMarkdownToSlackMrkdwn converts Markdown to Slack's mrkdwn format
// It preserves code blocks (```...```) and inline code (`...`) without conversion
func convertMarkdownToSlackMrkdwn(text string) string {
	// Split text into segments, preserving code blocks
	segments := splitPreservingCodeBlocks(text)

	var result strings.Builder
	result.Grow(len(text) * 2)

	for _, segment := range segments {
		if segment.isCodeBlock {
			// Code blocks are preserved as-is (but still need escaping)
			result.WriteString(escapeSlackChars(segment.text))
		} else {
			// Convert non-code segments
			// IMPORTANT: Order matters! italic must come before bold
			// because `*text*` would match in both patterns
			converted := segment.text
			converted = convertItalic(converted)
			converted = convertBold(converted)
			converted = convertLinks(converted)
			converted = escapeSlackChars(converted)
			result.WriteString(converted)
		}
	}

	return result.String()
}

// textSegment represents a portion of text with code block status
type textSegment struct {
	text        string
	isCodeBlock bool
}

// splitPreservingCodeBlocks splits text into code blocks and non-code segments
func splitPreservingCodeBlocks(text string) []textSegment {
	var segments []textSegment
	remaining := text

	for {
		// Find code block start
		codeStart := strings.Index(remaining, "```")
		if codeStart == -1 {
			// No more code blocks, add remaining text
			if len(remaining) > 0 {
				segments = append(segments, textSegment{text: remaining, isCodeBlock: false})
			}
			break
		}

		// Add text before code block
		if codeStart > 0 {
			segments = append(segments, textSegment{text: remaining[:codeStart], isCodeBlock: false})
		}

		// Find code block end (after the opening ```)
		afterStart := remaining[codeStart+3:]
		codeEnd := strings.Index(afterStart, "```")
		if codeEnd == -1 {
			// Unclosed code block, treat rest as code
			segments = append(segments, textSegment{text: remaining[codeStart:], isCodeBlock: true})
			break
		}

		// Add code block (including the ``` markers)
		codeBlock := remaining[codeStart : codeStart+3+codeEnd+3]
		segments = append(segments, textSegment{text: codeBlock, isCodeBlock: true})

		// Move past this code block
		remaining = remaining[codeStart+3+codeEnd+3:]
	}

	return segments
}

// escapeSlackChars escapes special characters for Slack
func escapeSlackChars(text string) string {
	result := strings.Builder{}
	result.Grow(len(text))

	for _, r := range text {
		switch r {
		case '&':
			result.WriteString("&amp;")
		case '<':
			result.WriteString("&lt;")
		case '>':
			result.WriteString("&gt;")
		default:
			result.WriteRune(r)
		}
	}

	return result.String()
}

// convertBold converts **text** to *text*
func convertBold(text string) string {
	// Match **text** but not **** (already bold markers)
	result := text
	for strings.Contains(result, "**") {
		start := strings.Index(result, "**")
		if start == -1 {
			break
		}
		end := strings.Index(result[start+2:], "**")
		if end == -1 {
			break
		}
		end += start + 2
		inner := result[start+2 : end]
		result = result[:start] + "*" + inner + "*" + result[end+2:]
	}
	return result
}

// convertItalic converts *text* to _text_ (but not ** or ***)
func convertItalic(text string) string {
	result := text
	// Simple implementation - match *text* but not ** or ***
	for {
		start := -1
		// Find * that's not followed by *
		for i := 0; i < len(result)-1; i++ {
			if result[i] == '*' && result[i+1] != '*' {
				// Also check it's not preceded by *
				if i > 0 && result[i-1] == '*' {
					continue
				}
				start = i
				break
			}
		}
		if start == -1 {
			break
		}

		end := -1
		for i := start + 1; i < len(result)-1; i++ {
			if result[i] == '*' && result[i+1] != '*' {
				end = i
				break
			}
		}
		if end == -1 {
			break
		}

		inner := result[start+1 : end]
		result = result[:start] + "_" + inner + "_" + result[end+1:]
	}
	return result
}

// convertLinks converts [text](url) to <url|text>
func convertLinks(text string) string {
	result := text
	for strings.Contains(result, "[") {
		textStart := strings.Index(result, "[")
		if textStart == -1 {
			break
		}
		textEnd := strings.Index(result[textStart:], "]")
		if textEnd == -1 {
			break
		}
		textEnd += textStart

		urlStart := strings.Index(result[textEnd:], "(")
		if urlStart == -1 {
			break
		}
		urlStart += textEnd

		urlEnd := strings.Index(result[urlStart:], ")")
		if urlEnd == -1 {
			break
		}
		urlEnd += urlStart

		linkText := result[textStart+1 : textEnd]
		linkURL := result[urlStart+1 : urlEnd]

		replacement := "<" + linkURL + "|" + linkText + ">"
		result = result[:textStart] + replacement + result[urlEnd+1:]
	}
	return result
}

// Verify FormatConversionProcessor implements MessageProcessor at compile time
var _ MessageProcessor = (*FormatConversionProcessor)(nil)
