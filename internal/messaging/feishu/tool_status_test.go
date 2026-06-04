package feishu

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/pkg/events"
)

// ---------------------------------------------------------------------------
// renderToolActivity
// ---------------------------------------------------------------------------

func TestRenderToolActivity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		entries  []toolEntry
		expected string
	}{
		{
			name:     "empty entries returns empty string",
			entries:  nil,
			expected: "",
		},
		{
			name:     "zero-length slice returns empty string",
			entries:  []toolEntry{},
			expected: "",
		},
		{
			name: "single pending entry",
			entries: []toolEntry{
				{id: "1", name: "Read", text: "Read file.go", done: false, result: ""},
			},
			expected: "Read file.go",
		},
		{
			name: "single done entry without result",
			entries: []toolEntry{
				{id: "1", name: "Read", text: "Read file.go", done: true, result: ""},
			},
			expected: "Read file.go",
		},
		{
			name: "single done entry with result",
			entries: []toolEntry{
				{id: "1", name: "Read", text: "Read file.go", done: true, result: "42 lines"},
			},
			expected: "Read file.go · 42 lines",
		},
		{
			name: "multiple entries joined by newline",
			entries: []toolEntry{
				{id: "1", name: "Read", text: "Read a.go", done: true, result: "ok"},
				{id: "2", name: "Write", text: "Write b.go", done: false, result: ""},
				{id: "3", name: "Bash", text: "Bash make test", done: true, result: "passed"},
			},
			expected: "Read a.go · ok\nWrite b.go\nBash make test · passed",
		},
		{
			name: "long text is visually truncated",
			entries: []toolEntry{
				{id: "1", name: "Read", text: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", done: false, result: ""},
			},
			expected: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa…",
		},
		{
			name: "text at exactly maxCols limit is not truncated",
			entries: []toolEntry{
				{id: "1", name: "Read", text: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", done: false, result: ""},
			},
			expected: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
		{
			name: "CJK text is visually truncated",
			entries: []toolEntry{
				{id: "1", name: "Read", text: "这是一段很长很长很长很长很长很长很长很长很长很长的中文文本用来测试截断", done: false, result: ""},
			},
			expected: "这是一段很长很长很长很长很长很长很长很长很长很长的…",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := renderToolActivity(tt.entries)
			require.Equal(t, tt.expected, got)
		})
	}
}

// ---------------------------------------------------------------------------
// extractToolCallData
// ---------------------------------------------------------------------------

func TestExtractToolCallData(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		env       *events.Envelope
		wantID    string
		wantName  string
		wantInput map[string]any
	}{
		{
			name: "nil envelope data",
			env: &events.Envelope{
				Event: events.Event{Type: events.ToolCall, Data: nil},
			},
			wantID:    "",
			wantName:  "",
			wantInput: nil,
		},
		{
			name: "wrong data type",
			env: &events.Envelope{
				Event: events.Event{
					Type: events.ToolCall,
					Data: "not a map",
				},
			},
			wantID:    "",
			wantName:  "",
			wantInput: nil,
		},
		{
			name: "empty map data",
			env: &events.Envelope{
				Event: events.Event{
					Type: events.ToolCall,
					Data: map[string]any{},
				},
			},
			wantID:    "",
			wantName:  "",
			wantInput: nil,
		},
		{
			name: "valid tool call data via map",
			env: &events.Envelope{
				Event: events.Event{
					Type: events.ToolCall,
					Data: map[string]any{
						"id":    "call-123",
						"name":  "Read",
						"input": map[string]any{"path": "/tmp/test.go"},
					},
				},
			},
			wantID:   "call-123",
			wantName: "Read",
			wantInput: map[string]any{
				"path": "/tmp/test.go",
			},
		},
		{
			name: "valid tool call data as concrete type",
			env: &events.Envelope{
				Event: events.Event{
					Type: events.ToolCall,
					Data: events.ToolCallData{
						ID:   "call-456",
						Name: "Bash",
						Input: map[string]any{
							"command": "go test ./...",
						},
					},
				},
			},
			wantID:   "call-456",
			wantName: "Bash",
			wantInput: map[string]any{
				"command": "go test ./...",
			},
		},
		{
			name: "nil input field",
			env: &events.Envelope{
				Event: events.Event{
					Type: events.ToolCall,
					Data: map[string]any{
						"id":   "call-789",
						"name": "Glob",
					},
				},
			},
			wantID:    "call-789",
			wantName:  "Glob",
			wantInput: nil,
		},
		{
			name: "wrong event kind still decodes data",
			env: &events.Envelope{
				Event: events.Event{
					Type: events.State,
					Data: events.ToolCallData{
						ID:    "call-miscat",
						Name:  "Edit",
						Input: map[string]any{"file": "main.go"},
					},
				},
			},
			wantID:    "call-miscat",
			wantName:  "Edit",
			wantInput: map[string]any{"file": "main.go"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			id, name, input := extractToolCallData(tt.env)
			require.Equal(t, tt.wantID, id)
			require.Equal(t, tt.wantName, name)
			require.Equal(t, tt.wantInput, input)
		})
	}
}

// ---------------------------------------------------------------------------
// extractToolResultData
// ---------------------------------------------------------------------------

func TestExtractToolResultData(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		env       *events.Envelope
		wantID    string
		wantOut   any
		wantError string
	}{
		{
			name: "nil envelope data",
			env: &events.Envelope{
				Event: events.Event{Type: events.ToolResult, Data: nil},
			},
			wantID:    "",
			wantOut:   nil,
			wantError: "",
		},
		{
			name: "wrong data type",
			env: &events.Envelope{
				Event: events.Event{
					Type: events.ToolResult,
					Data: 42,
				},
			},
			wantID:    "",
			wantOut:   nil,
			wantError: "",
		},
		{
			name: "empty map data",
			env: &events.Envelope{
				Event: events.Event{
					Type: events.ToolResult,
					Data: map[string]any{},
				},
			},
			wantID:    "",
			wantOut:   nil,
			wantError: "",
		},
		{
			name: "valid tool result via map with output",
			env: &events.Envelope{
				Event: events.Event{
					Type: events.ToolResult,
					Data: map[string]any{
						"id":     "result-1",
						"output": "file contents here",
					},
				},
			},
			wantID:    "result-1",
			wantOut:   "file contents here",
			wantError: "",
		},
		{
			name: "valid tool result via map with error",
			env: &events.Envelope{
				Event: events.Event{
					Type: events.ToolResult,
					Data: map[string]any{
						"id":    "result-2",
						"error": "permission denied",
					},
				},
			},
			wantID:    "result-2",
			wantOut:   nil,
			wantError: "permission denied",
		},
		{
			name: "valid tool result as concrete type",
			env: &events.Envelope{
				Event: events.Event{
					Type: events.ToolResult,
					Data: events.ToolResultData{
						ID:     "result-3",
						Output: map[string]any{"lines": 100},
						Error:  "",
					},
				},
			},
			wantID:  "result-3",
			wantOut: map[string]any{"lines": 100},
		},
		{
			name: "valid tool result with both output and error",
			env: &events.Envelope{
				Event: events.Event{
					Type: events.ToolResult,
					Data: events.ToolResultData{
						ID:     "result-4",
						Output: "partial output",
						Error:  "timeout after 30s",
					},
				},
			},
			wantID:    "result-4",
			wantOut:   "partial output",
			wantError: "timeout after 30s",
		},
		{
			name: "nil output in concrete type",
			env: &events.Envelope{
				Event: events.Event{
					Type: events.ToolResult,
					Data: events.ToolResultData{
						ID:     "result-5",
						Output: nil,
					},
				},
			},
			wantID:    "result-5",
			wantOut:   nil,
			wantError: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			id, output, errMsg := extractToolResultData(tt.env)
			require.Equal(t, tt.wantID, id)
			require.Equal(t, tt.wantOut, output)
			require.Equal(t, tt.wantError, errMsg)
		})
	}
}

// ---------------------------------------------------------------------------
// runeWidth
// ---------------------------------------------------------------------------

func TestRuneWidth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		r    rune
		want int
	}{
		{name: "null byte", r: 0, want: 1},
		{name: "space", r: ' ', want: 1},
		{name: "ASCII digit", r: '7', want: 1},
		{name: "ASCII letter", r: 'A', want: 1},
		{name: "ASCII lowercase", r: 'z', want: 1},
		{name: "Latin-1 extended", r: 0xE9, want: 1}, // e-acute
		{name: "Latin-1 max", r: 0xFF, want: 1},      // y-umlaut
		{name: "CJK Han character", r: '中', want: 2},
		{name: "CJK Han another", r: '国', want: 2},
		{name: "Hangul syllable", r: '한', want: 2},
		{name: "Hiragana", r: 'あ', want: 2},
		{name: "Katakana", r: 'ア', want: 2},
		{name: "fullwidth A", r: 'Ａ', want: 2},
		{name: "fullwidth 0", r: '０', want: 2},
		{name: "fullwidth range middle", r: 'Ｐ', want: 2},
		{name: "emoji grinning face", r: '😀', want: 2},
		{name: "emoji rocket", r: '🚀', want: 2},
		{name: "emoji cyclone", r: '🌀', want: 2}, // U+1F300, start of emoji range
		{name: "general punctuation em dash", r: '—', want: 2},
		{name: "general punctuation ellipsis", r: '…', want: 2},
		{name: "Cyrillic A", r: 'А', want: 1},
		{name: "Thai character", r: 'ก', want: 1},
		{name: "Devanagari", r: 'अ', want: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := runeWidth(tt.r)
			require.Equal(t, tt.want, got)
		})
	}
}

// ---------------------------------------------------------------------------
// truncateVisual
// ---------------------------------------------------------------------------

func TestTruncateVisual(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		s       string
		maxCols int
		want    string
	}{
		{
			name:    "zero maxCols returns empty",
			s:       "hello",
			maxCols: 0,
			want:    "",
		},
		{
			name:    "negative maxCols returns empty",
			s:       "hello",
			maxCols: -5,
			want:    "",
		},
		{
			name:    "empty string within budget",
			s:       "",
			maxCols: 10,
			want:    "",
		},
		{
			name:    "ASCII fits exactly",
			s:       "abc",
			maxCols: 3,
			want:    "abc",
		},
		{
			name:    "ASCII within budget",
			s:       "abc",
			maxCols: 10,
			want:    "abc",
		},
		{
			name:    "ASCII truncated at boundary",
			s:       "abcdefgh",
			maxCols: 5,
			want:    "abcde…",
		},
		{
			name:    "single ASCII char budget",
			s:       "abcdef",
			maxCols: 1,
			want:    "a…",
		},
		{
			name:    "CJK fits exactly even number cols",
			s:       "你好",
			maxCols: 4,
			want:    "你好",
		},
		{
			name:    "CJK truncated mid-character",
			s:       "你好世界",
			maxCols: 5,
			want:    "你好…",
		},
		{
			name:    "CJK with ASCII mix truncated at boundary",
			s:       "Hi你好",
			maxCols: 5,
			want:    "Hi你…",
		},
		{
			name:    "CJK with ASCII mix fits",
			s:       "Hi你好",
			maxCols: 6,
			want:    "Hi你好",
		},
		{
			name:    "CJK with ASCII mix truncated",
			s:       "Hi你好世界",
			maxCols: 5,
			want:    "Hi你…",
		},
		{
			name:    "emoji fits",
			s:       "🚀",
			maxCols: 2,
			want:    "🚀",
		},
		{
			name:    "emoji truncated",
			s:       "🚀abc",
			maxCols: 3,
			want:    "🚀a…",
		},
		{
			name:    "fullwidth form truncated",
			s:       "ＡＢＣ",
			maxCols: 3,
			want:    "Ａ…",
		},
		{
			name:    "maxCols 1 with wide char returns empty truncation",
			s:       "你好",
			maxCols: 1,
			want:    "…",
		},
		{
			name:    "ellipsis char itself is wide",
			s:       "aaa…bbb",
			maxCols: 4,
			want:    "aaa…",
		},
		{
			name:    "byte length fast path short ASCII",
			s:       "hi",
			maxCols: 50,
			want:    "hi",
		},
		{
			name:    "long CJK string truncated to 50 cols",
			s:       "这是一段很长很长很长很长很长很长很长很长很长很长的中文文本用来测试截断效果",
			maxCols: 50,
			want:    "这是一段很长很长很长很长很长很长很长很长很长很长的…",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := truncateVisual(tt.s, tt.maxCols)
			require.Equal(t, tt.want, got)
		})
	}
}
