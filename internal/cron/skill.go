package cron

import (
	"log/slog"
	"os"
	"path/filepath"

	_ "embed"
)

//go:embed cron-skill-manual.md
var embeddedManual string

// SkillManual returns the complete cron management manual content.
func SkillManual() string { return embeddedManual }

// ReleaseSkillManual writes the cron skill manual to the user's skill directory.
func ReleaseSkillManual(log *slog.Logger) {
	home, err := os.UserHomeDir()
	if err != nil {
		log.Warn("cron: cannot determine home dir for skill manual release", "err", err)
		return
	}
	dir := filepath.Join(home, ".hotplex", "skills")
	_ = os.MkdirAll(dir, 0o755)
	path := filepath.Join(dir, "cron.md")
	if err := os.WriteFile(path, []byte(SkillManual()), 0o644); err != nil {
		log.Warn("cron: failed to release skill manual", "path", path, "err", err)
		return
	}
	log.Debug("cron: skill manual released", "path", path)
}
