package logging

import "strings"

// MaskString masks the input string based on the sensitivity level.
// Default masking rules:
//   - LevelLow: shows first 4 + last 4 characters (e.g., sk-abc****xyz789)
//   - LevelMedium: shows first 2 + last 2 characters (e.g., U0****K2)
//   - LevelHigh: shows only the first character (e.g., s****)
func MaskString(input string, level SensitivityLevel) string {
	if input == "" || level == LevelNone {
		return input
	}

	length := len(input)

	switch level {
	case LevelLow:
		if length <= 8 {
			return input
		}
		prefix := input[:4]
		suffix := input[length-4:]
		return prefix + "****" + suffix

	case LevelMedium:
		if length <= 4 {
			return input
		}
		prefix := input[:2]
		suffix := input[length-2:]
		return prefix + "****" + suffix

	case LevelHigh:
		if length == 0 {
			return ""
		}
		// Use string(rune(input[0])) to handle Unicode correctly
		return string(rune(input[0])) + "****"

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

// MaskSensitiveFields masks known sensitive fields in a map of attributes.
func MaskSensitiveFields(attrs map[string]any, level SensitivityLevel) map[string]any {
	if level == LevelNone || attrs == nil {
		return attrs
	}

	// List of sensitive field names to mask
	sensitiveFields := map[string]bool{
		"api_key":          true,
		"secret":           true,
		"password":         true,
		"token":            true,
		"access_token":     true,
		"refresh_token":    true,
		"private_key":      true,
		"authorization":    true,
		"x-api-key":        true,
		"session_id":       true,
		"provider_session": true,
		"user_token":       true,
		"credential":       true,
	}

	result := make(map[string]any, len(attrs))
	for k, v := range attrs {
		if sensitiveFields[strings.ToLower(k)] {
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
