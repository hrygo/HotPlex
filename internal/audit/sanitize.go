package audit

import (
	"regexp"
	"strings"
	"unicode"
)

const RedactedValue = "[REDACTED]"

var (
	prefixedSecretPattern = regexp.MustCompile(`(?i)(hpk_|sk-|sk_|AKIA|ghp_|gho_|ghs_|xox[baprs]-)([A-Za-z0-9._+/-]{4,})`)
	bearerSecretPattern   = regexp.MustCompile(`(?i)(Bearer\s+)([^\s"']{4,})`)
	assignmentPattern     = regexp.MustCompile(`(?i)((?:password|passwd|pwd|secret|token|api[_-]?key|authorization|cookie)\s*[:=]\s*["']?)([^\s"']{4,})`)
	urlUserInfoPattern    = regexp.MustCompile(`(?i)(https?://[^:/\s]+:)([^@\s/]+)(@)`)
	cookieHeaderPattern   = regexp.MustCompile(`(?i)((?:set-)?cookie\s*:\s*)([^\r\n]+)`)
	privateKeyPattern     = regexp.MustCompile(`(?s)(-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----).*?(-----END [A-Z0-9 ]*PRIVATE KEY-----)`)
)

// SanitizeValue recursively redacts credential-bearing keys and masks secrets
// embedded in string values. It accepts the JSON-compatible values produced by
// encoding/json and returns a detached copy.
func SanitizeValue(v any) any {
	switch value := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(value))
		for key, child := range value {
			if isSensitiveKey(key) {
				out[key] = RedactedValue
				continue
			}
			out[key] = SanitizeValue(child)
		}
		return out
	case []any:
		out := make([]any, len(value))
		for i := range value {
			out[i] = SanitizeValue(value[i])
		}
		return out
	case string:
		return MaskSensitiveText(value)
	default:
		return value
	}
}

func isSensitiveKey(key string) bool {
	canonical := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, key)
	for _, marker := range []string{
		"password", "passwd", "pwd", "secret", "token", "apikey",
		"authorization", "credential", "cookie", "privatekey",
	} {
		if strings.Contains(canonical, marker) {
			return true
		}
	}
	return false
}

// MaskSensitiveText handles credentials embedded in otherwise unstructured
// strings. Structural key redaction remains the primary defense.
func MaskSensitiveText(s string) string {
	s = privateKeyPattern.ReplaceAllString(s, "$1\n"+RedactedValue+"\n$2")
	s = cookieHeaderPattern.ReplaceAllString(s, "${1}"+RedactedValue)
	s = urlUserInfoPattern.ReplaceAllString(s, "${1}"+RedactedValue+"${3}")
	s = bearerSecretPattern.ReplaceAllString(s, "${1}"+RedactedValue)
	s = assignmentPattern.ReplaceAllString(s, "${1}"+RedactedValue)
	s = prefixedSecretPattern.ReplaceAllStringFunc(s, func(match string) string {
		parts := prefixedSecretPattern.FindStringSubmatch(match)
		if len(parts) != 3 {
			return RedactedValue
		}
		payload := []rune(parts[2])
		if len(payload) > 4 {
			return parts[1] + string(payload[:4]) + "…"
		}
		return parts[1] + RedactedValue
	})
	return s
}
