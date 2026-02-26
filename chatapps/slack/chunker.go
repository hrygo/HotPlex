package slack

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// ChunkMode defines the message chunking strategy.
type ChunkMode string

const (
	ChunkModeBasic   ChunkMode = "basic"   // Basic character-based splitting
	ChunkModeNewline ChunkMode = "newline" // Paragraph-first, prioritize semantic integrity
	ChunkModeSmart   ChunkMode = "smart"   // Smart chunking with code/list awareness
)

// ChunkConfig defines configuration for message chunking.
type ChunkConfig struct {
	Mode         ChunkMode // Chunking mode
	Limit        int       // Character limit (default 4000)
	PreserveList bool      // Keep list items intact
}

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

		// Try to break at word boundary
		if end < totalRunes {
			chunk := string(runes[i:end])
			lastSpace := strings.LastIndex(chunk, "\n")
			if lastSpace > 0 {
				// Break at newline if possible
				end = i + lastSpace + 1
			} else {
				lastSpace = strings.LastIndex(chunk, " ")
				if lastSpace > chunkSize/2 {
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

// ChunkWithConfig splits a message using the specified configuration.
// This is the main entry point for configurable chunking.
func ChunkWithConfig(text string, config *ChunkConfig) []string {
	if text == "" {
		return []string{text}
	}

	// Apply defaults
	if config == nil {
		config = &ChunkConfig{
			Mode:  ChunkModeBasic,
			Limit: SlackTextLimit,
		}
	}
	if config.Limit <= 0 {
		config.Limit = SlackTextLimit
	}

	// Check if text fits in single chunk
	if utf8.RuneCountInString(text) <= config.Limit {
		return []string{text}
	}

	switch config.Mode {
	case ChunkModeNewline:
		return chunkByParagraph(text, config.Limit)
	case ChunkModeSmart:
		return chunkSmart(text, config.Limit, config.PreserveList)
	case ChunkModeBasic:
		fallthrough
	default:
		return chunkMessage(text, config.Limit)
	}
}

// chunkByParagraph splits text by paragraphs, prioritizing semantic integrity.
// It only splits within paragraphs when they exceed the limit.
func chunkByParagraph(text string, limit int) []string {
	if text == "" || utf8.RuneCountInString(text) <= limit {
		return []string{text}
	}

	// Split by double newlines (paragraphs)
	paragraphs := strings.Split(text, "\n\n")
	var chunks []string
	var current strings.Builder
	reservedSpace := 15 // For "[999/999]\n" prefix

	for _, para := range paragraphs {
		para = strings.TrimRight(para, " \t")
		if para == "" {
			continue
		}

		paraLen := utf8.RuneCountInString(para)
		currentLen := utf8.RuneCountInString(current.String())

		// Check if adding this paragraph would exceed limit
		if currentLen > 0 && currentLen+paraLen+2 > limit-reservedSpace {
			// Current chunk is full, finalize it
			chunk := current.String()
			chunks = append(chunks, chunk)
			current.Reset()
		}

		// If single paragraph exceeds limit, split it further
		if paraLen > limit-reservedSpace {
			// First flush any existing content
			if current.Len() > 0 {
				chunks = append(chunks, current.String())
				current.Reset()
			}
			// Split the long paragraph
			chunks = append(chunks, splitLongParagraph(para, limit, reservedSpace)...)
		} else {
			// Add paragraph to current chunk
			if current.Len() > 0 {
				current.WriteString("\n\n")
			}
			current.WriteString(para)
		}
	}

	// Add remaining content
	if current.Len() > 0 {
		chunks = append(chunks, current.String())
	}

	// Add chunk numbering
	return addChunkNumbers(chunks)
}

// splitLongParagraph splits a paragraph that exceeds the limit.
// It tries to split at sentence boundaries first, then at word boundaries.
func splitLongParagraph(text string, limit, reservedSpace int) []string {
	lines := strings.Split(text, "\n")
	var chunks []string
	var current strings.Builder

	for _, line := range lines {
		line = strings.TrimRight(line, " \t")
		if line == "" {
			continue
		}

		lineLen := utf8.RuneCountInString(line)
		currentLen := utf8.RuneCountInString(current.String())

		if currentLen > 0 && currentLen+lineLen+1 > limit-reservedSpace {
			chunks = append(chunks, current.String())
			current.Reset()
		}

		// If single line exceeds limit, split by words
		if lineLen > limit-reservedSpace {
			if current.Len() > 0 {
				chunks = append(chunks, current.String())
				current.Reset()
			}
			chunks = append(chunks, chunkMessage(line, limit-reservedSpace)...)
		} else {
			if current.Len() > 0 {
				current.WriteString("\n")
			}
			current.WriteString(line)
		}
	}

	if current.Len() > 0 {
		chunks = append(chunks, current.String())
	}

	return chunks
}

// preserveInlineCode keeps inline code (backtick-enclosed) intact.
// It avoids splitting within inline code blocks.
func preserveInlineCode(text string, limit int) []string {
	if text == "" || utf8.RuneCountInString(text) <= limit {
		return []string{text}
	}

	// Regex to find inline code: backtick-enclosed text
	inlineCodeRegex := regexp.MustCompile("`[^`]+`")

	// Find all inline code positions
	matches := inlineCodeRegex.FindAllStringIndex(text, -1)
	if len(matches) == 0 {
		return chunkMessage(text, limit)
	}

	// Split text into segments (code and non-code)
	var segments []segment
	lastEnd := 0

	for _, match := range matches {
		// Add non-code segment before this match
		if match[0] > lastEnd {
			segments = append(segments, segment{
				content: text[lastEnd:match[0]],
				isCode:  false,
			})
		}
		// Add code segment
		segments = append(segments, segment{
			content: text[match[0]:match[1]],
			isCode:  true,
		})
		lastEnd = match[1]
	}

	// Add remaining text after last match
	if lastEnd < len(text) {
		segments = append(segments, segment{
			content: text[lastEnd:],
			isCode:  false,
		})
	}

	// Now chunk while preserving code segments
	var chunks []string
	var current strings.Builder

	for _, seg := range segments {
		segLen := utf8.RuneCountInString(seg.content)
		currentLen := utf8.RuneCountInString(current.String())

		// If adding this segment would exceed limit
		if currentLen > 0 && currentLen+segLen > limit-15 {
			chunks = append(chunks, current.String())
			current.Reset()
		}

		// If segment itself exceeds limit and is code, we must split
		if segLen > limit-15 && seg.isCode {
			if current.Len() > 0 {
				chunks = append(chunks, current.String())
				current.Reset()
			}
			// For code, we have to split - but try to keep logical units
			subChunks := chunkMessage(seg.content, limit-15)
			chunks = append(chunks, subChunks...)
		} else {
			if current.Len() > 0 && !seg.isCode {
				current.WriteString(" ")
			}
			current.WriteString(seg.content)
		}
	}

	if current.Len() > 0 {
		chunks = append(chunks, current.String())
	}

	return addChunkNumbers(chunks)
}

// segment represents a text segment with its type (code/non-code).
type segment struct {
	content string
	isCode  bool
}

// chunkSmart provides intelligent chunking with awareness of
// code blocks, inline code, and list items.
func chunkSmart(text string, limit int, preserveList bool) []string {
	if text == "" || utf8.RuneCountInString(text) <= limit {
		return []string{text}
	}

	// First, handle code blocks (```...```)
	chunks := splitByCodeBlocks(text, limit)
	if len(chunks) <= 1 {
		return chunks
	}

	// Process each chunk with appropriate strategy
	var result []string
	for _, chunk := range chunks {
		processed := processChunkSmart(chunk, limit, preserveList)
		result = append(result, processed...)
	}

	return addChunkNumbersDedup(result)
}

// splitByCodeBlocks separates code blocks from regular text.
func splitByCodeBlocks(text string, limit int) []string {
	var parts []string
	var current strings.Builder
	inCodeBlock := false
	codeStart := 0

	for i := 0; i < len(text); i++ {
		if i+2 < len(text) && text[i:i+3] == "```" {
			if !inCodeBlock {
				if current.Len() > 0 {
					parts = append(parts, current.String())
					current.Reset()
				}
				inCodeBlock = true
				codeStart = i
			} else {
				parts = append(parts, text[codeStart:i+3])
				inCodeBlock = false
				current.Reset()
			}
			i += 2
			continue
		}

		if !inCodeBlock {
			current.WriteByte(text[i])
		}
	}

	if current.Len() > 0 {
		parts = append(parts, current.String())
	}

	// If no code blocks found, return as single chunk
	if len(parts) == 0 {
		return []string{text}
	}

	return parts
}

// processChunkSmart applies smart chunking to a non-code block.
func processChunkSmart(text string, limit int, preserveList bool) []string {
	// First try inline code preservation
	result := preserveInlineCode(text, limit)
	if len(result) <= 1 {
		return result
	}

	// If preserveList is enabled, handle list items
	if preserveList {
		var listChunks []string
		for _, chunk := range result {
			listChunks = append(listChunks, chunkByListItems(chunk, limit)...)
		}
		return listChunks
	}

	return result
}

// chunkByListItems keeps list items (numbered or bulleted) intact.
func chunkByListItems(text string, limit int) []string {
	if text == "" || utf8.RuneCountInString(text) <= limit {
		return []string{text}
	}

	// Regex for list item markers
	// Matches: 1. 2. - * etc at start of line
	listItemRegex := regexp.MustCompile(`(?m)^(\s*[-*]|\s*\d+\.)\s+`)

	// Find all list item positions
	matches := listItemRegex.FindAllStringIndex(text, -1)
	if len(matches) == 0 {
		return chunkMessage(text, limit)
	}

	// Split by list items
	var items []string
	lastEnd := 0

	for _, match := range matches {
		if match[0] > lastEnd {
			// Add content before this list item
			items = append(items, text[lastEnd:match[0]])
		}
		// Add the list item line
		lineEnd := match[1]
		if lineEnd < len(text) && text[lineEnd] != '\n' {
			// Find end of line
			nextNewline := strings.Index(text[lineEnd:], "\n")
			if nextNewline == -1 {
				lineEnd = len(text)
			} else {
				lineEnd = lineEnd + nextNewline
			}
		}
		items = append(items, text[match[0]:lineEnd])
		lastEnd = lineEnd
	}

	// Add remaining text
	if lastEnd < len(text) {
		items = append(items, text[lastEnd:])
	}

	// Now chunk while keeping list items together
	var chunks []string
	var current strings.Builder

	for _, item := range items {
		itemLen := utf8.RuneCountInString(item)
		currentLen := utf8.RuneCountInString(current.String())

		if currentLen > 0 && currentLen+itemLen+1 > limit-15 {
			chunks = append(chunks, current.String())
			current.Reset()
		}

		if current.Len() > 0 {
			current.WriteString("\n")
		}
		current.WriteString(item)
	}

	if current.Len() > 0 {
		chunks = append(chunks, current.String())
	}

	return chunks
}

// addChunkNumbers adds chunk numbering to chunks.
func addChunkNumbers(chunks []string) []string {
	if len(chunks) <= 1 {
		return chunks
	}

	result := make([]string, len(chunks))
	for i, chunk := range chunks {
		result[i] = fmt.Sprintf("[%d/%d]\n%s", i+1, len(chunks), chunk)
	}
	return result
}

// addChunkNumbersDedup adds chunk numbering and removes duplicate prefixes.
func addChunkNumbersDedup(chunks []string) []string {
	if len(chunks) <= 1 {
		// Remove any existing numbering from single chunk
		return removeChunkPrefix(chunks)
	}

	result := make([]string, len(chunks))
	for i, chunk := range chunks {
		// Remove existing numbering first
		chunk = removeExistingNumbers(chunk)
		result[i] = fmt.Sprintf("[%d/%d]\n%s", i+1, len(chunks), chunk)
	}
	return result
}

// removeChunkPrefix removes chunk numbering from a single chunk.
func removeChunkPrefix(chunks []string) []string {
	if len(chunks) == 0 {
		return chunks
	}
	result := make([]string, len(chunks))
	for i, chunk := range chunks {
		result[i] = removeExistingNumbers(chunk)
	}
	return result
}

// removeExistingNumbers removes existing [n/m] prefixes.
func removeExistingNumbers(text string) string {
	// Match patterns like [1/3] or [99/99]
	re := regexp.MustCompile(`^\[\d+/\d+\]\n?`)
	return re.ReplaceAllString(text, "")
}
