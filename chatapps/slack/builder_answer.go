// Package slack provides the Slack adapter implementation for the hotplex engine.
// Answer message builders for Slack Block Kit.
package slack

import (
	"strings"

	"github.com/hrygo/hotplex/chatapps/base"
	"github.com/slack-go/slack"
)

// AnswerMessageBuilder builds answer-related Slack messages
type AnswerMessageBuilder struct {
	formatter *MrkdwnFormatter
}

// NewAnswerMessageBuilder creates a new AnswerMessageBuilder
func NewAnswerMessageBuilder(formatter *MrkdwnFormatter) *AnswerMessageBuilder {
	return &AnswerMessageBuilder{
		formatter: formatter,
	}
}

// BuildAnswerMessage builds a message for AI answer
func (b *AnswerMessageBuilder) BuildAnswerMessage(msg *base.ChatMessage) []slack.Block {
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
func (b *AnswerMessageBuilder) buildChunkedAnswerBlocks(content string) []slack.Block {
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
func (b *AnswerMessageBuilder) chunkText(text string, maxLen int) []string {
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

// BuildErrorMessage builds a message for errors
// Implements EventTypeError per spec - uses quote format for emphasis
func (b *AnswerMessageBuilder) BuildErrorMessage(msg *base.ChatMessage) []slack.Block {
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
