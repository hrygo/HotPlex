package base

import (
	"unicode/utf8"
)

// RuneCount counts Unicode runes (characters) instead of bytes
func RuneCount(s string) int {
	return utf8.RuneCountInString(s)
}

// TruncateByRune truncates string by rune count, not byte count (no ellipsis)
func TruncateByRune(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	if RuneCount(s) <= maxRunes {
		return s
	}
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes])
}

// TruncateWithEllipsis truncates string by rune count with ellipsis
func TruncateWithEllipsis(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	if RuneCount(s) <= maxRunes {
		return s
	}
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes-3]) + "..."
}
