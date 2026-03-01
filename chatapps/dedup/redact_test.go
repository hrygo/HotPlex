package dedup

import (
	"strings"
	"testing"
)

func TestRedactSensitiveData(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "no sensitive data",
			input:    "Hello world",
			expected: "Hello world",
		},
		{
			name:     "Slack bot token",
			input:    "token: xoxb-123-456-789",
			expected: "token: xoxb-***REDACTED***",
		},
		{
			name:     "GitHub personal token",
			input:    "ghp_abcdefghijklmnopqrstuvwxyz123456",
			expected: "ghp_***REDACTED***",
		},
		{
			name:     "GitHub OAuth token",
			input:    "gho_abcdefghijklmnopqrstuvwxyz123456",
			expected: "gho_***REDACTED***",
		},
		{
			name:     "Multiple tokens",
			input:    "xoxb-abc ghp_xyz",
			expected: "xoxb-***REDACTED*** ghp_***REDACTED***",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RedactSensitiveData(tt.input)
			if result != tt.expected {
				t.Errorf("RedactSensitiveData(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func BenchmarkRedactSensitiveData(b *testing.B) {
	input := "token: xoxb-123-456-789 and ghp_abcdefghijklmnopqrstuvwxyz123456"
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		RedactSensitiveData(input)
	}
}

func TestRedactSensitiveData_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains string // Should contain this (redacted)
	}{
		{
			name:     "Token at start",
			input:    "xoxb-abc def",
			contains: "xoxb-***REDACTED***",
		},
		{
			name:     "Token at end",
			input:    "def xoxb-abc",
			contains: "xoxb-***REDACTED***",
		},
		{
			name:     "Token in middle",
			input:    "def xoxb-abc ghi",
			contains: "xoxb-***REDACTED***",
		},
		{
			name:     "Token with quotes",
			input:    `"xoxb-abc"`,
			contains: "xoxb-***REDACTED***",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RedactSensitiveData(tt.input)
			if !strings.Contains(result, tt.contains) {
				t.Errorf("RedactSensitiveData(%q) should contain %q, got %q", tt.input, tt.contains, result)
			}
		})
	}
}
