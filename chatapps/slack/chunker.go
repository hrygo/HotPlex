// Package slack provides the Slack adapter implementation for the hotplex engine.
// Logic for splitting large messages to respect Slack's 4000-character limit.
package slack

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/hrygo/hotplex/chatapps/base"
)

// SlackTextLimit is the maximum character limit for a single Slack message
const SlackTextLimit = 4000

// chunkMessage splits a text message into chunks that fit within Slack's limit.
// It delegates to base.ChunkMessage for the core logic.
// Each chunk is prefixed with [chunkNum/totalChunks] for reference.
func chunkMessage(text string, limit int) []string {
	return base.ChunkMessage(text, base.ChunkerConfig{
		MaxLen:        limit,
		PreserveWords: true,
		AddNumbering:  true,
	})
}

// chunkMessageMarkdown splits a markdown message into chunks,
// keeping code blocks together as much as possible.
func ChunkMessageMarkdown(text string, limit int) []string {
	if text == "" || utf8.RuneCountInString(text) <= limit {
		return []string{text}
	}

	// Check if text contains code blocks
	hasCodeBlock := strings.Contains(text, "```")

	if !hasCodeBlock {
		return chunkMessage(text, limit)
	}

	// Split by code blocks
	var parts []string
	var current strings.Builder
	inCodeBlock := false
	codeStart := 0

	for i := 0; i < len(text); i++ {
		if i+2 < len(text) && text[i:i+3] == "```" {
			if !inCodeBlock {
				// Save text before code block
				if current.Len() > 0 {
					parts = append(parts, current.String())
					current.Reset()
				}
				inCodeBlock = true
				codeStart = i
			} else {
				// End of code block
				parts = append(parts, text[codeStart:i+3])
				inCodeBlock = false
				current.Reset()
			}
			i += 2 // Skip the rest of ```
			continue
		}

		if !inCodeBlock {
			current.WriteByte(text[i])
		}
	}

	// Add remaining text
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}

	// If no code blocks found, use regular chunking
	if len(parts) == 0 {
		return chunkMessage(text, limit)
	}

	// Now chunk each part and combine
	var result []string
	chunkNum := 1

	for _, part := range parts {
		subChunks := chunkMessage(part, limit)
		for _, sub := range subChunks {
			if len(result) > 0 || chunkNum > 1 {
				result = append(result, fmt.Sprintf("[%d/?]\n%s", chunkNum, sub))
			} else {
				result = append(result, sub)
			}
			chunkNum++
		}
	}

	// Fix chunk numbers now that we know total
	total := len(result)
	for i := range result {
		if strings.HasPrefix(result[i], "[?/") {
			result[i] = strings.Replace(result[i], "[?/", fmt.Sprintf("[%d/", total), 1)
		}
	}

	return result
}
