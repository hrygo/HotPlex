// Package slack provides the Slack adapter implementation for the hotplex engine.
// Logic for splitting large messages to respect Slack's 4000-character limit.
package slack

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// SlackTextLimit is the maximum character limit for a single Slack message
const SlackTextLimit = 4000

// chunkMessage splits a text message into chunks that fit within Slack's limit.
// It attempts to split at word boundaries to avoid breaking words.
// Each chunk is prefixed with [chunkNum/totalChunks] for reference.
func chunkMessage(text string, limit int) []string {
	if text == "" || utf8.RuneCountInString(text) <= limit {
		return []string{text}
	}

	// Calculate approximate number of chunks
	runes := []rune(text)
	totalRunes := len(runes)

	var chunks []string
	chunkSize := limit - 15 // Reserve space for "[999/999]\n" prefix

	for i := 0; i < totalRunes; i += chunkSize {
		end := i + chunkSize
		if end > totalRunes {
			end = totalRunes
		}

		// Try to break at word boundary using rune indices
		if end < totalRunes {
			chunkRunes := runes[i:end]

			// Find last newline in chunk (use rune-based search)
			lastNewline := -1
			for j := len(chunkRunes) - 1; j >= 0; j-- {
				if chunkRunes[j] == '\n' {
					lastNewline = j
					break
				}
			}
			if lastNewline > 0 {
				// Break at newline if possible
				end = i + lastNewline + 1
			} else {
				// Find last space in chunk
				lastSpace := -1
				for j := len(chunkRunes) - 1; j >= 0; j-- {
					if chunkRunes[j] == ' ' {
						lastSpace = j
						break
					}
				}
				if lastSpace > len(chunkRunes)/2 {
					// Only break at space if more than half the chunk is used
					end = i + lastSpace
				}
			}
		}

		chunkStr := string(runes[i:end])
		chunkStr = strings.TrimRight(chunkStr, " \t")

		chunks = append(chunks, chunkStr)
	}

	// Add chunk numbering
	result := make([]string, len(chunks))
	for i, chunk := range chunks {
		if len(chunks) > 1 {
			result[i] = fmt.Sprintf("[%d/%d]\n%s", i+1, len(chunks), chunk)
		} else {
			result[i] = chunk
		}
	}

	return result
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
