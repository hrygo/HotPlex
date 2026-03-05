package base

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// ChunkerConfig holds configuration for message chunking.
type ChunkerConfig struct {
	// MaxLen is the maximum character limit per chunk.
	// If 0, defaults to DefaultChunkLimit.
	MaxLen int

	// PreserveWords attempts to break at word boundaries.
	// If false, may break in the middle of words.
	PreserveWords bool

	// AddNumbering prefixes chunks with [1/N] notation.
	AddNumbering bool
}

// DefaultChunkLimit is the default maximum chunk size.
const DefaultChunkLimit = 4000

// ChunkMessage splits a text message into chunks that fit within the limit.
// It attempts to split at word boundaries to avoid breaking words.
// Each chunk is prefixed with [chunkNum/totalChunks] if numbering is enabled.
func ChunkMessage(text string, cfg ChunkerConfig) []string {
	limit := cfg.MaxLen
	if limit <= 0 {
		limit = DefaultChunkLimit
	}

	if text == "" || utf8.RuneCountInString(text) <= limit {
		return []string{text}
	}

	runes := []rune(text)
	totalRunes := len(runes)

	// Reserve space for numbering prefix if enabled
	chunkSize := limit
	if cfg.AddNumbering {
		chunkSize = limit - 15 // Reserve space for "[999/999]\n" prefix
	}

	var chunks []string

	for i := 0; i < totalRunes; i += chunkSize {
		end := i + chunkSize
		if end > totalRunes {
			end = totalRunes
		}

		// Try to break at word boundary if enabled
		if cfg.PreserveWords && end < totalRunes {
			chunkRunes := runes[i:end]

			// Find last newline in chunk
			lastNewline := -1
			for j := len(chunkRunes) - 1; j >= 0; j-- {
				if chunkRunes[j] == '\n' {
					lastNewline = j
					break
				}
			}
			if lastNewline > 0 {
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
					end = i + lastSpace
				}
			}
		}

		chunkStr := string(runes[i:end])
		chunkStr = strings.TrimRight(chunkStr, " \t")
		chunks = append(chunks, chunkStr)
	}

	// Add chunk numbering if enabled
	if !cfg.AddNumbering || len(chunks) <= 1 {
		return chunks
	}

	result := make([]string, len(chunks))
	for i, chunk := range chunks {
		result[i] = fmt.Sprintf("[%d/%d]\n%s", i+1, len(chunks), chunk)
	}
	return result
}

// ChunkMessageSimple splits text by byte length without word boundary preservation.
// Use this for platforms that don't need word-aware splitting.
func ChunkMessageSimple(text string, maxLen int) []string {
	if maxLen <= 0 {
		maxLen = DefaultChunkLimit
	}

	if len(text) <= maxLen {
		return []string{text}
	}

	var chunks []string
	for len(text) > maxLen {
		chunks = append(chunks, text[:maxLen])
		text = text[maxLen:]
	}
	if len(text) > 0 {
		chunks = append(chunks, text)
	}
	return chunks
}
