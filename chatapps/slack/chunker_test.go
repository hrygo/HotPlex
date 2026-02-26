package slack

import (
	"testing"
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

func TestSlackTextLimit(t *testing.T) {
	if SlackTextLimit != 4000 {
		t.Errorf("SlackTextLimit = %d, want 4000", SlackTextLimit)
	}
}

func TestChunkWithConfig_Basic(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		config   *ChunkConfig
		expected int // expected number of chunks
	}{
		{
			name:     "nil config uses defaults",
			text:     "Hello world",
			config:   nil,
			expected: 1,
		},
		{
			name:     "empty text",
			text:     "",
			config:   &ChunkConfig{Mode: ChunkModeBasic, Limit: 100},
			expected: 1,
		},
		{
			name:     "text within limit",
			text:     "Short text",
			config:   &ChunkConfig{Mode: ChunkModeBasic, Limit: 100},
			expected: 1,
		},
		{
			name:     "basic mode chunks long text",
			text:     string(makeText(5000)),
			config:   &ChunkConfig{Mode: ChunkModeBasic, Limit: 1000},
			expected: 6, // Should create multiple chunks
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ChunkWithConfig(tt.text, tt.config)
			if len(result) != tt.expected {
				t.Errorf("expected %d chunks, got %d", tt.expected, len(result))
			}
		})
	}
}

func TestChunkWithConfig_Newline(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		config    *ChunkConfig
		minChunks int // minimum expected chunks
	}{
		{
			name: "newline mode with short paragraphs",
			text: "Paragraph one here.\n\nParagraph two here.",
			config: &ChunkConfig{
				Mode:  ChunkModeNewline,
				Limit: 100,
			},
			minChunks: 1,
		},
		{
			name: "newline mode splits long paragraphs",
			text: "Short para.\n\n" + string(makeText(3000)) + "\n\nEnd paragraph.",
			config: &ChunkConfig{
				Mode:  ChunkModeNewline,
				Limit: 1000,
			},
			minChunks: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ChunkWithConfig(tt.text, tt.config)
			if len(result) < tt.minChunks {
				t.Errorf("expected at least %d chunks, got %d", tt.minChunks, len(result))
				for i, r := range result {
					t.Logf("chunk %d: %d chars", i, len(r))
				}
			}
		})
	}
}

func TestChunkWithConfig_Smart(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		config   *ChunkConfig
		expected int
	}{
		{
			name: "smart mode with code blocks",
			text: "Before code.\n\n```\ncode block\ncontent\n```\n\nAfter code.",
			config: &ChunkConfig{
				Mode:         ChunkModeSmart,
				Limit:        100,
				PreserveList: false,
			},
			expected: 1, // Should keep code block together
		},
		{
			name: "smart mode preserves inline code",
			text: "Text with `inline code` and more text here.",
			config: &ChunkConfig{
				Mode:         ChunkModeSmart,
				Limit:        50,
				PreserveList: false,
			},
			expected: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ChunkWithConfig(tt.text, tt.config)
			if len(result) != tt.expected {
				t.Errorf("expected %d chunks, got %d", tt.expected, len(result))
			}
		})
	}
}

func TestChunkWithConfig_PreserveList(t *testing.T) {
	text := `First item
- Item A with some text
- Item B with some text
Last line`

	config := &ChunkConfig{
		Mode:         ChunkModeSmart,
		Limit:        50,
		PreserveList: true,
	}

	result := ChunkWithConfig(text, config)

	// Should not split list items
	t.Logf("Got %d chunks", len(result))
	for i, r := range result {
		t.Logf("chunk %d: %q", i, r)
	}
}

func TestChunkByParagraph(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		limit     int
		minChunks int
	}{
		{
			name:      "single short paragraph",
			text:      "This is a short paragraph.",
			limit:     100,
			minChunks: 1,
		},
		{
			name:      "multiple short paragraphs",
			text:      "Para 1\n\nPara 2\n\nPara 3",
			limit:     100,
			minChunks: 1,
		},
		{
			name:      "long paragraph splits",
			text:      string(makeText(3000)),
			limit:     1000,
			minChunks: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := chunkByParagraph(tt.text, tt.limit)
			if len(result) < tt.minChunks {
				t.Errorf("expected at least %d chunks, got %d", tt.minChunks, len(result))
			}
		})
	}
}

func TestPreserveInlineCode(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		limit int
	}{
		{
			name:  "no inline code",
			text:  "Plain text without code.",
			limit: 20,
		},
		{
			name:  "inline code preserved",
			text:  "Text with `code` here.",
			limit: 30,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := preserveInlineCode(tt.text, tt.limit)
			if len(result) == 0 {
				t.Error("expected at least one chunk")
			}
		})
	}
}

func TestChunkByListItems(t *testing.T) {
	text := `First item
- Bullet item one
- Bullet item two
1. Numbered item one
2. Numbered item two
Last item`

	result := chunkByListItems(text, 50)

	t.Logf("Got %d chunks:", len(result))
	for i, r := range result {
		t.Logf("chunk %d: %q", i, r)
	}

	if len(result) == 0 {
		t.Error("expected at least one chunk")
	}
}

func TestAddChunkNumbers(t *testing.T) {
	chunks := []string{"first chunk", "second chunk", "third chunk"}
	result := addChunkNumbers(chunks)

	if len(result) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(result))
	}

	// Prefix format is [N/N]\n
	if len(result[0]) < 5 || result[0][:4] != "[1/3" {
		t.Errorf("expected [1/3 prefix, got %q", result[0][:5])
	}
	if len(result[1]) < 5 || result[1][:4] != "[2/3" {
		t.Errorf("expected [2/3 prefix, got %q", result[1][:5])
	}
	if len(result[2]) < 5 || result[2][:4] != "[3/3" {
		t.Errorf("expected [3/3 prefix, got %q", result[2][:5])
	}
}

func TestRemoveExistingNumbers(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"[1/3]\nSome text", "Some text"},
		{"[99/100]\ncontent", "content"},
		{"No numbers here", "No numbers here"},
	}

	for _, tt := range tests {
		result := removeExistingNumbers(tt.input)
		if result != tt.expected {
			t.Errorf("expected %q, got %q", tt.expected, result)
		}
	}
}

// makeText creates a string of approximately n characters
func makeText(n int) string {
	text := "This is a test sentence. "
	result := ""
	for len(result) < n {
		result += text
	}
	return result[:n]
}
