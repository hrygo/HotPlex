package diag

import (
	"regexp"
	"strings"
)

// RedactionLevel controls how aggressive the redaction is.
type RedactionLevel int

const (
	// RedactStandard redacts common sensitive patterns.
	RedactStandard RedactionLevel = iota
	// RedactAggressive redacts more patterns including potential internal IPs.
	RedactAggressive
)

// redactionPattern defines a pattern to redact.
type redactionPattern struct {
	pattern     *regexp.Regexp
	replacement string
}

// Redactor handles sensitive information redaction.
type Redactor struct {
	patterns []redactionPattern
	level    RedactionLevel
}

// NewRedactor creates a new Redactor with the specified level.
func NewRedactor(level RedactionLevel) *Redactor {
	r := &Redactor{
		level: level,
	}

	// Standard patterns (always applied)
	r.patterns = []redactionPattern{
		// API Keys - various formats
		{
			pattern:     regexp.MustCompile(`(?i)(api[_-]?key|apikey)[\s:=]+["']?[\w-]{20,}["']?`),
			replacement: "[REDACTED_API_KEY]",
		},
		// Bearer tokens
		{
			pattern:     regexp.MustCompile(`(?i)bearer\s+[\w-\.]+`),
			replacement: "bearer [REDACTED_TOKEN]",
		},
		// Slack tokens (xoxb, xoxp, xoxa, xoxr)
		{
			pattern:     regexp.MustCompile(`xox[baprs]-[\w-]+`),
			replacement: "[REDACTED_SLACK_TOKEN]",
		},
		// Generic tokens
		{
			pattern:     regexp.MustCompile(`(?i)(token|access_token|auth_token)[\s:=]+["']?[\w-]{20,}["']?`),
			replacement: "[REDACTED_TOKEN]",
		},
		// Secrets
		{
			pattern:     regexp.MustCompile(`(?i)(secret|password|passwd|pwd)[\s:=]+["']?[^\s"']{8,}["']?`),
			replacement: "[REDACTED_SECRET]",
		},
		// Private keys
		{
			pattern:     regexp.MustCompile(`-----BEGIN\s+(?:RSA\s+)?PRIVATE\s+KEY-----[\s\S]*?-----END\s+(?:RSA\s+)?PRIVATE\s+KEY-----`),
			replacement: "[REDACTED_PRIVATE_KEY]",
		},
		// AWS Access Key ID
		{
			pattern:     regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
			replacement: "[REDACTED_AWS_KEY]",
		},
		// AWS Secret Access Key
		{
			pattern:     regexp.MustCompile(`(?i)aws[_-]?secret[_-]?access[_-]?key[\s:=]+["']?[\w/+=]{40}["']?`),
			replacement: "[REDACTED_AWS_SECRET]",
		},
		// GitHub tokens
		{
			pattern:     regexp.MustCompile(`ghp_[\w]{36}`),
			replacement: "[REDACTED_GITHUB_TOKEN]",
		},
		// GitHub OAuth tokens
		{
			pattern:     regexp.MustCompile(`gho_[\w]{36}`),
			replacement: "[REDACTED_GITHUB_TOKEN]",
		},
		// GitHub App tokens
		{
			pattern:     regexp.MustCompile(`ghu_[\w]{36}`),
			replacement: "[REDACTED_GITHUB_TOKEN]",
		},
		// Anthropic API keys
		{
			pattern:     regexp.MustCompile(`sk-ant-api[\w-]+`),
			replacement: "[REDACTED_ANTHROPIC_KEY]",
		},
		// OpenAI API keys
		{
			pattern:     regexp.MustCompile(`sk-[a-zA-Z0-9]{48,}`),
			replacement: "[REDACTED_OPENAI_KEY]",
		},
		// Connection strings with passwords
		{
			pattern:     regexp.MustCompile(`(?i)(postgres|mysql|mongodb|redis)://[^:]+:[^@]+@`),
			replacement: "$1://[REDACTED_USER]:[REDACTED_PASS]@",
		},
		// JWT tokens
		{
			pattern:     regexp.MustCompile(`eyJ[a-zA-Z0-9_-]*\.eyJ[a-zA-Z0-9_-]*\.[a-zA-Z0-9_-]*`),
			replacement: "[REDACTED_JWT]",
		},
		// Email addresses
		{
			pattern:     regexp.MustCompile(`[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`),
			replacement: "[REDACTED_EMAIL]",
		},
		// Credit card numbers (basic pattern)
		{
			pattern:     regexp.MustCompile(`\b[0-9]{4}[-\s]?[0-9]{4}[-\s]?[0-9]{4}[-\s]?[0-9]{4}\b`),
			replacement: "[REDACTED_CC]",
		},
	}

	// Add aggressive patterns if requested
	if level == RedactAggressive {
		ipPatterns := []redactionPattern{
			{
				pattern:     regexp.MustCompile(`\b10\.\d{1,3}\.\d{1,3}\.\d{1,3}\b`),
				replacement: "[REDACTED_IP]",
			},
			{
				pattern:     regexp.MustCompile(`\b172\.(1[6-9]|2\d|3[01])\.\d{1,3}\.\d{1,3}\b`),
				replacement: "[REDACTED_IP]",
			},
			{
				pattern:     regexp.MustCompile(`\b192\.168\.\d{1,3}\.\d{1,3}\b`),
				replacement: "[REDACTED_IP]",
			},
			{
				pattern:     regexp.MustCompile(`\blocalhost\b`),
				replacement: "[REDACTED_HOST]",
			},
		}
		r.patterns = append(r.patterns, ipPatterns...)
	}

	return r
}

// Redact applies all redaction patterns to the input string.
func (r *Redactor) Redact(input string) string {
	result := input
	for _, rp := range r.patterns {
		result = rp.pattern.ReplaceAllString(result, rp.replacement)
	}
	return result
}

// RedactBytes applies redaction to a byte slice.
func (r *Redactor) RedactBytes(input []byte) []byte {
	return []byte(r.Redact(string(input)))
}

// RedactMapValues redacts values in a map that match sensitive keys.
func (r *Redactor) RedactMapValues(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}

	result := make(map[string]any, len(m))
	for k, v := range m {
		lowerKey := strings.ToLower(k)
		if isSensitiveKey(lowerKey) {
			result[k] = "[REDACTED]"
		} else {
			switch val := v.(type) {
			case string:
				result[k] = r.Redact(val)
			case map[string]any:
				result[k] = r.RedactMapValues(val)
			default:
				result[k] = v
			}
		}
	}
	return result
}

// isSensitiveKey checks if a key name suggests sensitive data.
func isSensitiveKey(key string) bool {
	sensitivePatterns := []string{
		"password", "passwd", "pwd",
		"secret", "token", "api_key", "apikey",
		"private_key", "privatekey",
		"access_key", "accesskey",
		"auth", "credential", "cred",
	}

	for _, pattern := range sensitivePatterns {
		if strings.Contains(key, pattern) {
			return true
		}
	}
	return false
}

// Global default redactor for convenience
var defaultRedactor = NewRedactor(RedactStandard)

// Redact is a convenience function using the default redactor.
func Redact(input string) string {
	return defaultRedactor.Redact(input)
}

// RedactBytes is a convenience function using the default redactor.
func RedactBytes(input []byte) []byte {
	return defaultRedactor.RedactBytes(input)
}
