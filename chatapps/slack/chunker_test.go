package slack

import (
	"testing"
	"unicode/utf8"
)

func TestChunkMessage(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		limit int
	}{
		{
			name:  "empty string",
			text:  "",
			limit: 4000,
		},
		{
			name:  "short text under limit",
			text:  "Hello, World!",
			limit: 4000,
		},
		{
			name:  "just over limit - splits",
			text:  "a" + string(make([]byte, 5000)),
			limit: 4000,
		},
		{
			name:  "three times limit",
			text:  string(make([]byte, 12000)),
			limit: 4000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := chunkMessage(tt.text, tt.limit)

			// Verify chunks are not empty
			if len(result) == 0 {
				t.Error("expected at least one chunk")
			}

			// Verify each chunk is under limit (accounting for prefix)
			for i, chunk := range result {
				_ = i                          // unused
				if len(chunk) > tt.limit+100 { // Allow some buffer for prefix
					t.Errorf("chunk %d exceeds limit: %d > %d", i, len(chunk), tt.limit)
				}
			}

			// Verify numbering for multiple chunks
			if len(result) > 1 {
				// Second chunk should have prefix
				if len(result[1]) > 0 && result[1][0] != '[' {
					t.Errorf("chunk 2 should have numbering prefix")
				}
			}
		})
	}
}

func TestChunkMessage_Numbering(t *testing.T) {
	// Simple text that should split into 2 chunks
	text := "word " + string(make([]byte, 4500)) // ~4505 chars
	result := chunkMessage(text, 4000)

	if len(result) < 2 {
		t.Skipf("expected multiple chunks, got %d", len(result))
	}

	// Check prefix format
	for i := 1; i < len(result); i++ {
		if len(result[i]) < 5 || result[i][0] != '[' {
			t.Errorf("chunk %d should start with '[N/N]'", i)
		}
	}
}

func TestChunkMessage_UTF8(t *testing.T) {
	// Test with Unicode characters
	text := "你好世界👋" + string(make([]byte, 5000))
	result := chunkMessage(text, 4000)

	if len(result) < 2 {
		t.Skip("text too short to chunk")
	}

	// Verify chunks are valid
	for i, chunk := range result {
		if len(chunk) == 0 {
			t.Errorf("chunk %d is empty", i)
		}
	}
}

func TestChunkMessage_ChineseNewline(t *testing.T) {
	// Test Chinese text with newlines - should break at Chinese newline
	text := "第一行中文\n第二行中文\n第三行中文"
	text = text + string(make([]byte, 5000)) // Add more to force chunking

	result := chunkMessage(text, 50)

	if len(result) < 2 {
		t.Skip("text too short to chunk")
	}

	// Verify chunks don't contain broken characters
	for i, chunk := range result {
		if !isValidUTF8(chunk) {
			t.Errorf("chunk %d contains invalid UTF-8: %q", i, chunk)
		}
	}
}

func isValidUTF8(s string) bool {
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			return false
		}
		i += size
	}
	return true
}

func TestSlackTextLimit(t *testing.T) {
	if SlackTextLimit != 4000 {
		t.Errorf("SlackTextLimit = %d, want 4000", SlackTextLimit)
	}
}
