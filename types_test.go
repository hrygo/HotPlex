package hotplex

import (
	"testing"
	"unicode/utf8"
)

func TestTruncateString(t *testing.T) {
	tests := []struct {
		input       string
		maxLen      int
		shouldTrunc bool
	}{
		{"hello", 10, false},
		{"hello world", 5, true},
		{"", 5, false},
		{"short", 100, false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := TruncateString(tt.input, tt.maxLen)

			// Check that result doesn't exceed maxLen
			if len(result) > tt.maxLen {
				t.Errorf("TruncateString(%q, %d) = %q (len=%d), exceeds maxLen",
					tt.input, tt.maxLen, result, len(result))
			}

			// For non-empty input that fits, result should be unchanged
			if !tt.shouldTrunc && tt.input != "" && result != tt.input {
				t.Errorf("TruncateString(%q, %d) = %q, want unchanged", tt.input, tt.maxLen, result)
			}
		})
	}
}

func TestTruncateString_UTF8(t *testing.T) {
	// Test UTF-8 safety - Truncate handles UTF-8 correctly when maxLen >= 4
	utf8str := "中文测试很长的一段文字"
	result := TruncateString(utf8str, 10)

	// Result should be valid UTF-8
	if !isValidUTF8(result) {
		t.Errorf("TruncateString produced invalid UTF-8: %q", result)
	}
}

func TestTruncateString_NoTruncation(t *testing.T) {
	// When string fits, it should be returned unchanged
	shortStr := "hello"
	result := TruncateString(shortStr, 100)
	if result != shortStr {
		t.Errorf("TruncateString(%q, 100) = %q, want %q", shortStr, result, shortStr)
	}
}

func isValidUTF8(s string) bool {
	for _, r := range s {
		if r == utf8.RuneError {
			return false
		}
	}
	return true
}

func TestSummarizeInput(t *testing.T) {
	tests := []struct {
		name         string
		input        map[string]any
		wantNotEmpty bool
	}{
		{"nil input", nil, false},
		{"empty map", map[string]any{}, false},
		{"with command", map[string]any{"command": "ls -la"}, true},
		{"with query", map[string]any{"query": "SELECT * FROM users"}, true},
		{"with path", map[string]any{"path": "/tmp/file.txt"}, true},
		{"with other", map[string]any{"foo": "bar"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SummarizeInput(tt.input)
			if tt.wantNotEmpty && result == "" {
				t.Errorf("SummarizeInput() returned empty for %v", tt.input)
			}
			if !tt.wantNotEmpty && result != "" {
				t.Errorf("SummarizeInput() = %q, want empty", result)
			}
		})
	}
}

func TestSummarizeInput_Truncation(t *testing.T) {
	// Long command should be truncated
	longCmd := "this is a very long command that should be truncated to fit within the limit"
	input := map[string]any{"command": longCmd}
	result := SummarizeInput(input)

	if len(result) > 50 {
		t.Errorf("SummarizeInput() result too long: %d chars", len(result))
	}
}

func TestStreamMessage_GetContentBlocks(t *testing.T) {
	tests := []struct {
		name    string
		msg     StreamMessage
		wantLen int
	}{
		{
			name: "from Message field",
			msg: StreamMessage{
				Message: &AssistantMessage{
					Content: []ContentBlock{{Type: "text", Text: "hello"}},
				},
			},
			wantLen: 1,
		},
		{
			name: "from Content field",
			msg: StreamMessage{
				Content: []ContentBlock{{Type: "text", Text: "world"}},
			},
			wantLen: 1,
		},
		{
			name: "Message takes precedence",
			msg: StreamMessage{
				Message: &AssistantMessage{
					Content: []ContentBlock{{Type: "text", Text: "from message"}},
				},
				Content: []ContentBlock{{Type: "text", Text: "from content"}},
			},
			wantLen: 1,
		},
		{
			name:    "empty",
			msg:     StreamMessage{},
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blocks := tt.msg.GetContentBlocks()
			if len(blocks) != tt.wantLen {
				t.Errorf("GetContentBlocks() returned %d blocks, want %d", len(blocks), tt.wantLen)
			}
		})
	}
}

func TestContentBlock_GetUnifiedToolID(t *testing.T) {
	tests := []struct {
		name     string
		block    ContentBlock
		expected string
	}{
		{"ToolUseID takes precedence", ContentBlock{ToolUseID: "tool-123", ID: "block-456"}, "tool-123"},
		{"ID as fallback", ContentBlock{ID: "block-456"}, "block-456"},
		{"empty", ContentBlock{}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.block.GetUnifiedToolID()
			if result != tt.expected {
				t.Errorf("GetUnifiedToolID() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestUsageStats_Fields(t *testing.T) {
	stats := UsageStats{
		InputTokens:           100,
		OutputTokens:          50,
		CacheWriteInputTokens: 20,
		CacheReadInputTokens:  10,
	}

	if stats.InputTokens != 100 {
		t.Errorf("InputTokens = %d, want 100", stats.InputTokens)
	}
	if stats.OutputTokens != 50 {
		t.Errorf("OutputTokens = %d, want 50", stats.OutputTokens)
	}
}

func TestAssistantMessage_Fields(t *testing.T) {
	msg := AssistantMessage{
		ID:      "msg-123",
		Type:    "message",
		Role:    "assistant",
		Content: []ContentBlock{{Type: "text", Text: "Hello"}},
	}

	if msg.ID != "msg-123" {
		t.Errorf("ID = %q, want msg-123", msg.ID)
	}
	if len(msg.Content) != 1 {
		t.Errorf("len(Content) = %d, want 1", len(msg.Content))
	}
}

func TestContentBlock_Fields(t *testing.T) {
	block := ContentBlock{
		Type:      "tool_use",
		Name:      "bash",
		ID:        "tool-abc",
		ToolUseID: "use-xyz",
		Input:     map[string]any{"command": "ls"},
		Content:   "result",
		IsError:   true,
	}

	if block.Type != "tool_use" {
		t.Errorf("Type = %q, want tool_use", block.Type)
	}
	if block.Name != "bash" {
		t.Errorf("Name = %q, want bash", block.Name)
	}
	if !block.IsError {
		t.Error("IsError should be true")
	}
}

func TestStreamMessage_Fields(t *testing.T) {
	msg := StreamMessage{
		Type:         "tool_use",
		Timestamp:    "2024-01-01T00:00:00Z",
		SessionID:    "session-123",
		Role:         "assistant",
		Name:         "bash",
		Output:       "command output",
		Status:       "success",
		Error:        "",
		Duration:     1000,
		Subtype:      "result",
		IsError:      false,
		TotalCostUSD: 0.05,
		Usage: &UsageStats{
			InputTokens:  100,
			OutputTokens: 50,
		},
		Result: "final result",
	}

	if msg.Type != "tool_use" {
		t.Errorf("Type = %q, want tool_use", msg.Type)
	}
	if msg.Duration != 1000 {
		t.Errorf("Duration = %d, want 1000", msg.Duration)
	}
	if msg.TotalCostUSD != 0.05 {
		t.Errorf("TotalCostUSD = %f, want 0.05", msg.TotalCostUSD)
	}
	if msg.Usage == nil {
		t.Error("Usage should not be nil")
	}
}

func TestConfig_Fields(t *testing.T) {
	cfg := Config{
		WorkDir:          "/tmp/work",
		SessionID:        "session-123",
		TaskSystemPrompt: "You are helpful",
	}

	if cfg.WorkDir != "/tmp/work" {
		t.Errorf("WorkDir = %q, want /tmp/work", cfg.WorkDir)
	}
	if cfg.SessionID != "session-123" {
		t.Errorf("SessionID = %q, want session-123", cfg.SessionID)
	}
	if cfg.TaskSystemPrompt != "You are helpful" {
		t.Errorf("TaskSystemPrompt = %q", cfg.TaskSystemPrompt)
	}
}

func TestErrors_Defined(t *testing.T) {
	// Verify all sentinel errors are defined
	if ErrDangerBlocked == nil {
		t.Error("ErrDangerBlocked should not be nil")
	}
	if ErrSessionTerminated == nil {
		t.Error("ErrSessionTerminated should not be nil")
	}
	if ErrContextCancelled == nil {
		t.Error("ErrContextCancelled should not be nil")
	}
	if ErrInvalidConfig == nil {
		t.Error("ErrInvalidConfig should not be nil")
	}

	// Verify error messages
	if ErrDangerBlocked.Error() == "" {
		t.Error("ErrDangerBlocked should have a message")
	}
}
