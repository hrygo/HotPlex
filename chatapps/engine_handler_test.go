package chatapps

import (
	"testing"
)

func TestDetectTaskType(t *testing.T) {
	tests := []struct {
		name     string
		prompt   string
		expected string
	}{
		// Git tasks - highest priority (matched first after code exclusion)
		{"git commit", "git commit -m 'msg'", "git"},
		{"git push", "please push to origin", "git"},
		{"git merge", "merge the branch", "git"},
		{"git checkout", "checkout main", "git"},
		{"git status", "show git status", "git"},

		// Code tasks (non-git keywords)
		{"write function", "write a function to sort", "code"},
		{"create api", "create a new API endpoint", "code"},
		{"implement feature", "implement user authentication", "code"},
		{"refactor code", "refactor the handler", "code"},
		{"add test", "add unit test for service", "code"},

		// Debug tasks (when no code/git keywords - note: "debug" matches codePatterns first)
		// These inputs contain debug keywords but NOT code keywords
		{"stack trace", "analyze stack trace", "debug"},
		{"crash issue", "investigate crash problem", "debug"},
		{"exception handling", "exception occurred in handler", "debug"},
		{"error report", "show me the error report", "debug"},

		// Analysis tasks (when no git/code/debug keywords)
		{"explain concept", "explain how it works", "analysis"},
		{"compare options", "compare these approaches", "analysis"},
		{"what is this", "what is the purpose", "analysis"},

		// Chat/fallback
		{"hello", "hello there", "chat"},
		{"thanks", "thank you", "chat"},
		{"empty", "", "chat"},

		// Case insensitivity
		{"uppercase GIT", "GIT COMMIT", "git"},
		{"mixed case Code", "WrItE a FuNcTiOn", "code"},

		// Known priority behaviors (document current behavior)
		// Note: "log" matches gitPatterns
		{"debug with logs", "debug the error in logs", "git"},
		// Note: "fix" matches codePatterns
		{"fix bug", "fix the bug in payment", "code"},
		// Note: "code" matches codePatterns
		{"analyze code", "analyze the codebase", "code"},
		// Note: "pull" matches gitPatterns
		{"review pr", "review the pull request", "git"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detectTaskType(tt.prompt)
			if result != tt.expected {
				t.Errorf("detectTaskType(%q) = %q, want %q", tt.prompt, result, tt.expected)
			}
		})
	}
}
