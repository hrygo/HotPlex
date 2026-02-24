package slack

import (
	"context"
	"regexp"
	"strings"

	"github.com/hrygo/hotplex/chatapps/base"
)

// FormatConverterHandler converts message content to Slack-specific format
// Handles Markdown -> mrkdwn conversion
type FormatConverterHandler struct{}

// NewFormatConverterHandler creates a new FormatConverterHandler
func NewFormatConverterHandler() *FormatConverterHandler {
	return &FormatConverterHandler{}
}

// Handle converts Markdown to Slack mrkdwn format
func (h *FormatConverterHandler) Handle(ctx context.Context, msg *base.ChatMessage) (*base.ChatMessage, error) {
	if msg == nil || msg.Content == "" {
		return msg, nil
	}

	// Check if conversion is needed based on ParseMode
	if msg.RichContent != nil && msg.RichContent.ParseMode != base.ParseModeNone {
		switch msg.RichContent.ParseMode {
		case base.ParseModeMarkdown:
			msg.Content = convertMarkdownToMrkdwn(msg.Content)
		case base.ParseModeHTML:
			msg.Content = convertHtmlToMrkdwn(msg.Content)
		}
	}

	return msg, nil
}

// convertHtmlToMrkdwn converts HTML to Slack mrkdwn
func convertHtmlToMrkdwn(content string) string {
	// Simple HTML to mrkdwn conversions
	content = strings.ReplaceAll(content, "<b>", "*")
	content = strings.ReplaceAll(content, "</b>", "*")
	content = strings.ReplaceAll(content, "<strong>", "*")
	content = strings.ReplaceAll(content, "</strong>", "*")
	content = strings.ReplaceAll(content, "<i>", "_")
	content = strings.ReplaceAll(content, "</i>", "_")
	content = strings.ReplaceAll(content, "<em>", "_")
	content = strings.ReplaceAll(content, "</em>", "_")
	content = strings.ReplaceAll(content, "<code>", "`")
	content = strings.ReplaceAll(content, "</code>", "`")
	content = strings.ReplaceAll(content, "<br>", "\n")
	content = strings.ReplaceAll(content, "<br/>", "\n")
	content = strings.ReplaceAll(content, "<br />", "\n")

	// Links: <a href="url">text</a> -> <url|text>
	linkRegex := regexp.MustCompile(`<a href="([^"]+)">([^<]+)</a>`)
	content = linkRegex.ReplaceAllString(content, "<$1|$2>")

	return content
}

// SlackRichContentHandler processes Slack-specific RichContent
type SlackRichContentHandler struct{}

// NewSlackRichContentHandler creates a new SlackRichContentHandler
func NewSlackRichContentHandler() *SlackRichContentHandler {
	return &SlackRichContentHandler{}
}

// Handle processes Slack-specific rich content
func (h *SlackRichContentHandler) Handle(ctx context.Context, msg *base.ChatMessage) (*base.ChatMessage, error) {
	if msg == nil {
		return nil, nil
	}

	// Process Blocks if present
	if msg.RichContent != nil && len(msg.RichContent.Blocks) > 0 {
		// TODO: Transform RichContent.Blocks to Slack block kit format
		// Blocks are Slack-specific, pass through for now
		// The sender will handle JSON marshaling
		_ = msg.RichContent.Blocks
	}

	// Process Embeds if present (for Discord compatibility)
	if msg.RichContent != nil && len(msg.RichContent.Embeds) > 0 {
		// TODO: Convert Discord embeds to Slack blocks or attachments
		// For now, just pass through
		_ = msg.RichContent.Embeds
	}

	return msg, nil
}
