// Package security provides sanitization utilities for redacting sensitive data.
//
// TODO: Wire RedactSensitive into output pipeline (bridge_forward.go or XML sanitizer)
// to sanitize Worker responses before they reach end users.
package security

import "regexp"

// sensitivePatterns detects secrets, credentials, and internal addresses in text output.
// Extracted from brain/guard.go for standalone use without Brain dependency.
var sensitivePatterns = []*regexp.Regexp{
	// API Keys (api_key=xxx, secret_key=xxx, access_key=xxx)
	regexp.MustCompile(`(?i)(api[_-]?key|apikey|secret[_-]?key|access[_-]?key)[\s:=]+['"]?[a-zA-Z0-9_-]{20,}['"]?`),
	// AWS Access Keys (AKIA...)
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	// Private Keys (PEM blocks)
	regexp.MustCompile(`-{5}BEGIN\s+(RSA\s+)?PRIVATE\s+KEY-{5}`),
	// JWT Tokens (eyJ...)
	regexp.MustCompile(`eyJ[a-zA-Z0-9_-]*\.eyJ[a-zA-Z0-9_-]*\.[a-zA-Z0-9_-]*`),
	// Internal IP Addresses (RFC 1918)
	regexp.MustCompile(`\b(10\.\d{1,3}\.\d{1,3}\.\d{1,3}|172\.(1[6-9]|2[0-9]|3[0-1])\.\d{1,3}\.\d{1,3}|192\.168\.\d{1,3}\.\d{1,3})\b`),
	// Database connection strings with credentials
	regexp.MustCompile(`(?i)(postgres|mysql|mongodb|redis)://\S+:\S+@\S+`),
	// Generic secrets (password=xxx, passwd=xxx, pwd=xxx)
	regexp.MustCompile(`(?i)(password|passwd|pwd)[\s:=]+['"]?[^\s'"]{8,}['"]?`),
}

// RedactSensitive replaces API keys, JWTs, private keys, internal IPs,
// database credentials, and passwords with "[REDACTED]".
// Returns the sanitized string and true if any replacements were made.
func RedactSensitive(input string) (string, bool) {
	found := false
	result := input
	for _, pattern := range sensitivePatterns {
		if pattern.MatchString(result) {
			found = true
			result = pattern.ReplaceAllString(result, "[REDACTED]")
		}
	}
	return result, found
}
