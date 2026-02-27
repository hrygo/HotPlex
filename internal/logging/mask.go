package logging

import (
	"regexp"
	"strings"
)

// MaskString masks the input string based on the sensitivity level.
// Masking rules:
//   - LevelLow: shows first 4 + last 4 characters (min 9 chars to mask)
//   - LevelMedium: shows first 2 + last 2 characters (min 5 chars to mask)
//   - LevelHigh: shows only the first character (always masks, even short strings)
func MaskString(input string, level SensitivityLevel) string {
	if input == "" || level == LevelNone {
		return input
	}

	runes := []rune(input)
	length := len(runes)

	switch level {
	case LevelLow:
		if length <= 8 {
			// Short strings: show first 2 + last 2 with masking
			if length <= 4 {
				return string(runes[:1]) + "****"
			}
			return string(runes[:2]) + "****" + string(runes[length-2:])
		}
		return string(runes[:4]) + "****" + string(runes[length-4:])

	case LevelMedium:
		if length <= 4 {
			// Short strings: show first char only
			return string(runes[:1]) + "****"
		}
		return string(runes[:2]) + "****" + string(runes[length-2:])

	case LevelHigh:
		if length == 0 {
			return ""
		}
		// Always mask, showing only first character
		return string(runes[:1]) + "****"

	default:
		return input
	}
}

// MaskStrings masks all strings in a map based on the sensitivity level.
func MaskStrings(input map[string]string, level SensitivityLevel) map[string]string {
	if level == LevelNone || input == nil {
		return input
	}

	result := make(map[string]string, len(input))
	for k, v := range input {
		result[k] = MaskString(v, level)
	}
	return result
}

// sensitiveFieldPatterns matches common sensitive field name patterns.
// Uses regex to catch variations like apiKey, api-key, API_KEY, etc.
var sensitiveFieldPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^api[-_]?key$`),
	regexp.MustCompile(`(?i)^secret[-_]?.*$`),
	regexp.MustCompile(`(?i)^.*[-_]?secret$`),
	regexp.MustCompile(`(?i)^.*secret.*key$`), // mySecretKey, secret_api_key
	regexp.MustCompile(`(?i)^password$`),
	regexp.MustCompile(`(?i)^passwd$`),
	regexp.MustCompile(`(?i)^pwd$`),
	regexp.MustCompile(`(?i)^token$`),
	regexp.MustCompile(`(?i)^.*[-_]?token$`),
	regexp.MustCompile(`(?i)^token[-_]?.*$`),
	regexp.MustCompile(`(?i)^access[-_]?token$`),
	regexp.MustCompile(`(?i)^refresh[-_]?token$`),
	regexp.MustCompile(`(?i)^auth[-_]?token$`),
	regexp.MustCompile(`(?i)^bearer$`),
	regexp.MustCompile(`(?i)^private[-_]?key$`),
	regexp.MustCompile(`(?i)^api[-_]?secret$`),
	regexp.MustCompile(`(?i)^client[-_]?secret$`),
	regexp.MustCompile(`(?i)^authorization$`),
	regexp.MustCompile(`(?i)^x[-_]?api[-_]?key$`),
	regexp.MustCompile(`(?i)^session[-_]?id$`),
	regexp.MustCompile(`(?i)^provider[-_]?session$`),
	regexp.MustCompile(`(?i)^user[-_]?token$`),
	regexp.MustCompile(`(?i)^credential$`),
	regexp.MustCompile(`(?i)^credentials$`),
	regexp.MustCompile(`(?i)^api[-_]?credentials$`),
}

// isSensitiveField checks if a field name matches sensitive patterns.
func isSensitiveField(field string) bool {
	fieldLower := strings.ToLower(field)
	for _, pattern := range sensitiveFieldPatterns {
		if pattern.MatchString(fieldLower) {
			return true
		}
	}
	return false
}

// MaskSensitiveFields masks known sensitive fields in a map of attributes.
// Uses pattern matching to detect field name variations.
func MaskSensitiveFields(attrs map[string]any, level SensitivityLevel) map[string]any {
	if level == LevelNone || attrs == nil {
		return attrs
	}

	result := make(map[string]any, len(attrs))
	for k, v := range attrs {
		if isSensitiveField(k) {
			if str, ok := v.(string); ok {
				result[k] = MaskString(str, level)
			} else {
				result[k] = v
			}
		} else {
			result[k] = v
		}
	}

	return result
}
