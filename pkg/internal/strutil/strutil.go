package strutil

import "unicode/utf8"

// Truncate strings at rune level to avoid invalid UTF-8.
// If maxLen < 4, returns a raw byte slice without appending "..." to avoid panic.
func Truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen < 4 {
		return s[:maxLen]
	}

	if utf8.ValidString(s) {
		runes := []rune(s)
		if len(runes) > maxLen {
			return string(runes[:maxLen-3]) + "..."
		}
		return s
	}

	// Fallback to byte truncation if not valid UTF-8
	return s[:maxLen-3] + "..."
}
