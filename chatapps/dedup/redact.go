package dedup

import (
	"strings"
)

// RedactSensitiveData redacts sensitive information from log messages
// This prevents tokens, secrets, and keys from being logged
func RedactSensitiveData(s string) string {
	if s == "" {
		return ""
	}

	// Redact tokens (Slack, GitHub, etc.)
	// Pattern: xoxb-*, xoxp-*, xoxa-*, ghp_*, gho_*, github_pat_*
	tokenPatterns := []string{
		"xoxb-", "xoxp-", "xoxa-", "xoxr-",
		"ghp_", "gho_", "github_pat_",
		"sk-", "Bearer ",
	}

	result := s
	for _, pattern := range tokenPatterns {
		idx := strings.Index(result, pattern)
		if idx >= 0 {
			// Find end of token (space, newline, or end of string)
			end := idx + len(pattern)
			for end < len(result) && result[end] != ' ' && result[end] != '\n' && result[end] != '"' && result[end] != '\'' {
				end++
			}
			// Replace token with redacted version
			result = result[:idx] + pattern + "***REDACTED***" + result[end:]
		}
	}

	return result
}
