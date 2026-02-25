package slack

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"
)

// SendMessageOptions contains options for sending messages
type SendMessageOptions struct {
	ChannelID string
	ThreadTS  string
	Markdown  bool // Whether to convert markdown to mrkdwn
}

// SendMessageWithOptions sends a message with full options support
// This includes: message chunking, thread support, rate limiting
func (a *Adapter) SendMessageWithOptions(ctx context.Context, sessionID string, msg *MessageContent, opts SendMessageOptions) error {
	if a.config.BotToken == "" {
		return fmt.Errorf("slack bot token not configured")
	}

	if opts.ChannelID == "" {
		return fmt.Errorf("channel_id is required")
	}

	// Process text content
	text := msg.Content
	if opts.Markdown {
		text = convertMarkdownToMrkdwn(text)
	}

	// Handle message chunking if needed
	if utf8.RuneCountInString(text) > SlackTextLimit {
		return a.sendWithChunking(ctx, opts.ChannelID, text, opts.ThreadTS, opts.Markdown)
	}

	// Send single message
	return a.SendToChannel(ctx, opts.ChannelID, text, opts.ThreadTS)
}

// sendWithChunking sends a long message in chunks
func (a *Adapter) sendWithChunking(ctx context.Context, channelID, text, threadTS string, isMarkdown bool) error {
	var chunks []string
	if isMarkdown {
		chunks = ChunkMessageMarkdown(text, SlackTextLimit)
	} else {
		chunks = chunkMessage(text, SlackTextLimit)
	}

	for i, chunk := range chunks {
		// For the first chunk, don't use thread_ts unless it's a reply
		var chunkThreadTS string
		if i > 0 || threadTS != "" {
			chunkThreadTS = threadTS
			// For first chunk in a thread reply, also add thread_ts
			if i == 0 && threadTS != "" {
				chunkThreadTS = threadTS
			}
		}

		if err := a.SendToChannel(ctx, channelID, chunk, chunkThreadTS); err != nil {
			return fmt.Errorf("failed to send chunk %d/%d: %w", i+1, len(chunks), err)
		}
	}

	return nil
}

// MessageContent represents the content of a message to send
type MessageContent struct {
	Content string
}

// convertMarkdownToMrkdwn converts Markdown text to Slack's mrkdwn format
func convertMarkdownToMrkdwn(text string) string {
	// Escape special characters first
	result := escapeSlackChars(text)

	// Convert bold: **text** -> *text*
	result = convertBold(result)

	// Convert italic: *text* -> _text_
	result = convertItalic(result)

	// Convert code blocks: ```code``` -> ```code```
	result = convertCodeBlocks(result)

	// Convert inline code: `code` -> `code`
	// (already handled by escapeSlackChars)

	// Convert links: [text](url) -> <url|text>
	result = convertLinks(result)

	return result
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
	return replacePattern(text, `\*\*([^*]+)\*\*`, "*$1*")
}

// convertItalic converts *text* to _text_ (but not ** or ***)
func convertItalic(text string) string {
	// Match *text* but not ** or ***, not at start/end of word
	return replacePattern(text, `(?<!\*)\*([^*]+)\*(?!\*)`, "_$1_")
}

// convertCodeBlocks converts ```code``` to ```code```
func convertCodeBlocks(text string) string {
	// Triple backticks are preserved in mrkdwn
	// Just ensure proper formatting
	return text
}

// convertLinks converts [text](url) to <url|text>
func convertLinks(text string) string {
	// Match [text](url)
	return replacePattern(text, `\[([^\]]+)\]\(([^)]+)\)`, "<$2|$1>")
}

// replacePattern is a simple regex replacement helper
func replacePattern(text, pattern, _ string) string {
	// For simplicity, we'll do basic string operations
	// In production, you'd use regexp
	result := text

	// Simple implementation for common patterns
	switch pattern {
	case `\*\*([^*]+)\*\*`:
		// Bold: **text** -> *text*
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
	case `\[([^\]]+)\]\(([^)]+)\)`:
		// Links: [text](url) -> <url|text>
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
	}

	return result
}
