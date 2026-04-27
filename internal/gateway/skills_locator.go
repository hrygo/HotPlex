package gateway

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/hrygo/hotplex/internal/config"
	skutil "github.com/hrygo/hotplex/internal/skills"
)

// Source labels for skills.
const (
	skillSourceLocal   = "local"
	skillSourceProject = "project"
)

// FileSystemSkillsLocator discovers skills from Claude Code skills directories:
//   - ~/.agents/skills/
//   - ~/.claude/skills/ (symlinks skipped)
//   - ./.agents/skills/ (project-level)
//   - ./.claude/skills/ (project-level, symlinks skipped)
//
// Duplicate names are filtered (first wins). Skills are discovered from
// SKILL.md files within each skill directory.
type FileSystemSkillsLocator struct{}

// NewFileSystemSkillsLocator creates a new skills locator.
func NewFileSystemSkillsLocator(cfg *config.Config) *FileSystemSkillsLocator {
	return &FileSystemSkillsLocator{}
}

// List returns all skills discovered from standard skills directories.
func (l *FileSystemSkillsLocator) List(ctx context.Context, homeDir, workDir string) ([]Skill, error) {
	if homeDir == "" {
		homeDir, _ = os.UserHomeDir()
	}

	dirs := l.buildDirs(homeDir, workDir)
	seen := make(map[string]bool)

	var skills []Skill
	for _, dir := range dirs {
		l.scanDir(dir, &skills, seen)
	}
	return skills, nil
}

// buildDirs returns the list of directories to scan.
func (l *FileSystemSkillsLocator) buildDirs(homeDir, workDir string) []string {
	var dirs []string

	// User-level directories
	dirs = append(dirs,
		filepath.Join(homeDir, ".agents", "skills"),
		filepath.Join(homeDir, ".claude", "skills"),
	)

	// Project-level directories
	if workDir != "" {
		dirs = append(dirs,
			filepath.Join(workDir, ".agents", "skills"),
			filepath.Join(workDir, ".claude", "skills"),
		)
	}

	// Also check current working dir (hotplex repo root)
	if cwd, _ := os.Getwd(); cwd != "" && cwd != workDir {
		dirs = append(dirs,
			filepath.Join(cwd, ".agents", "skills"),
			filepath.Join(cwd, ".claude", "skills"),
		)
	}

	return dirs
}

// scanDir scans a skills directory for SKILL.md files, skipping symlinks.
func (l *FileSystemSkillsLocator) scanDir(dir string, skills *[]Skill, seen map[string]bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		// Skip symlinks and non-directories
		if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() {
			continue
		}

		skillDir := filepath.Join(dir, entry.Name())
		skillPath := filepath.Join(skillDir, "SKILL.md")
		name, desc, ok := skutil.ParseFrontmatter(skillPath)
		if !ok {
			continue
		}

		// Deduplicate by name (first wins)
		if seen[name] {
			continue
		}
		seen[name] = true

		// Determine source
		source := skillSourceLocal
		if strings.Contains(skillDir, string(filepath.Separator)+".agents"+string(filepath.Separator)) {
			source = skillSourceProject
		}

		*skills = append(*skills, Skill{
			Name:        name,
			Description: desc,
			Source:      source,
		})
	}
}
