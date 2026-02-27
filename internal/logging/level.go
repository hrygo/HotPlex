package logging

// SensitivityLevel defines the sensitivity of content for masking purposes.
type SensitivityLevel int

const (
	// LevelNone means no masking will be applied.
	LevelNone SensitivityLevel = iota
	// LevelLow shows first 4 and last 4 characters (e.g., sk-abc****xyz789).
	LevelLow
	// LevelMedium shows first 2 and last 2 characters (e.g., U0****K2).
	LevelMedium
	// LevelHigh shows only the first character (e.g., s****).
	LevelHigh
)

// String returns the string representation of SensitivityLevel.
func (s SensitivityLevel) String() string {
	switch s {
	case LevelNone:
		return "none"
	case LevelLow:
		return "low"
	case LevelMedium:
		return "medium"
	case LevelHigh:
		return "high"
	default:
		return "unknown"
	}
}
