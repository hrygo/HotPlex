package gateway

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/hrygo/hotplex/internal/config"
)

// FileSystemSkillsLocator discovers skills from the agent-configs directory.
type FileSystemSkillsLocator struct {
	configDir string
}

// NewFileSystemSkillsLocator creates a skills locator that reads from the agent config directory.
func NewFileSystemSkillsLocator(cfg *config.Config) *FileSystemSkillsLocator {
	dir := cfg.AgentConfig.ConfigDir
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".hotplex", "agent-configs")
	}
	return &FileSystemSkillsLocator{configDir: dir}
}

// List returns skills discovered from the agent-configs directory.
// It reads .md files and extracts name + description from frontmatter or headings.
func (l *FileSystemSkillsLocator) List(ctx context.Context, homeDir, workDir string) ([]Skill, error) {
	entries, err := os.ReadDir(l.configDir)
	if err != nil {
		return nil, nil // fall through silently — skills are optional
	}

	var skills []Skill
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		// Skip platform-specific variants (e.g. SOUL.slack.md)
		base := strings.TrimSuffix(entry.Name(), ".md")
		if strings.Contains(base, ".") {
			continue
		}
		path := filepath.Join(l.configDir, entry.Name())
		desc := extractDescription(path)

		name := strings.TrimSuffix(entry.Name(), ".md")
		skills = append(skills, Skill{
			Name:        name,
			Description: desc,
			Source:      "agent-configs",
		})
	}
	return skills, nil
}

// extractDescription reads the file and returns the description.
// For files with YAML frontmatter, it returns the description field.
// Otherwise, it returns the first non-empty line after the title.
func extractDescription(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	content := string(data)

	// Check for YAML frontmatter
	if strings.HasPrefix(content, "---\n") || strings.HasPrefix(content, "---\r\n") {
		if idx := strings.Index(content[4:], "\n---\n"); idx > 0 || strings.Index(content[4:], "\n---\r\n") > 0 {
			frontmatter := content[4:]
			var end int
			if idx2 := strings.Index(frontmatter, "\n---\n"); idx2 > 0 {
				end = idx2 + 5
			} else if idx2 := strings.Index(frontmatter, "\n---\r\n"); idx2 > 0 {
				end = idx2 + 6
			}
			if end > 0 {
				fm := frontmatter[:end]
				lines := strings.Split(fm, "\n")
				for _, line := range lines {
					line = strings.TrimSpace(line)
					if strings.HasPrefix(line, "description:") {
						desc := strings.TrimPrefix(line, "description:")
						desc = strings.Trim(desc, " \t\"")
						return desc
					}
				}
			}
		}
	}

	// Fallback: first H1 heading or first non-empty line
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.TrimPrefix(line, "# ")
		}
		if line != "" && !strings.HasPrefix(line, "---") {
			return strings.TrimSpace(line)
		}
	}
	return ""
}
