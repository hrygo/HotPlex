package dbutil

import _ "embed"

//go:embed db-stats-skill-manual.md
var embeddedManual string

// SkillManual returns the complete database statistics analysis manual content.
func SkillManual() string { return embeddedManual }
