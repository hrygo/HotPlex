package dedup

import (
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
