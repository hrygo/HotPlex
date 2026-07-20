package skills

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type skillFrontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// scanDirs scans all skill directories and returns deduplicated skills.
// Order: global dirs first, then project dirs (project overrides global by name).
// The managed flag marks entries under .agents/skills (writable region) so the
// list can distinguish managed skills (UI may create/replace/delete) from
// external read-only sources (.claude/.hotplex that the user manages by hand).
func scanDirs(homeDir, workDir string) []Skill {
	type dirEntry struct {
		path    string
		source  string
		managed bool
	}

	dirs := make([]dirEntry, 0, 7)

	// Global skill dirs require a valid home directory
	if homeDir != "" {
		dirs = append(dirs,
			dirEntry{filepath.Join(homeDir, ".claude", "skills"), SourceGlobal, false},
			dirEntry{filepath.Join(homeDir, ".agents", "skills"), SourceGlobal, true},
			dirEntry{filepath.Join(homeDir, ".hotplex", "skills"), SourceGlobal, false},
		)
	}

	// workDir 为空时仅扫描全局（home）目录，不触碰进程 cwd——用于无项目上下文的
	// 合并列表（GET /api/skills），避免把 hotplex 自身仓库的 skill 误纳入用户视图。
	if workDir != "" {
		dirs = append(dirs,
			dirEntry{filepath.Join(workDir, ".claude", "skills"), SourceProject, false},
			dirEntry{filepath.Join(workDir, ".agents", "skills"), SourceProject, true},
		)

		// Also check current working dir (hotplex repo root) if distinct from workDir
		if cwd, _ := os.Getwd(); cwd != "" && cwd != workDir {
			dirs = append(dirs,
				dirEntry{filepath.Join(cwd, ".claude", "skills"), SourceProject, false},
				dirEntry{filepath.Join(cwd, ".agents", "skills"), SourceProject, true},
			)
		}
	}

	var all []Skill
	for _, d := range dirs {
		skills, err := scanDir(d.path, d.source, d.managed)
		if err != nil {
			continue
		}
		all = append(all, skills...)
	}
	return dedup(all)
}

// scanWorkspaceInstalled 仅扫描 workspace 的受管 skill 目录
// （<workDir>/.agents/skills，source=project & managed=true），即该 workspace
// 「安装的」skill。不含全局目录、不含 <workDir>/.claude/skills 只读目录、不含其他
// workspace。用于 GET /api/workspaces/{wid}/skills：workspace 管理面只列/只管本
// workspace 安装的 skill（issue #918）。目录不存在时返回空切片而非错误。
func scanWorkspaceInstalled(workDir string) []Skill {
	if workDir == "" {
		return []Skill{}
	}
	skills, err := scanDir(filepath.Join(workDir, ".agents", "skills"), SourceProject, true)
	if err != nil {
		return []Skill{}
	}
	return dedup(skills)
}

// scanDir reads all .md files from a single skill directory.
// Skips symlink files to avoid duplicates from linked directories.
func scanDir(dir, source string, managed bool) ([]Skill, error) {
	fi, err := os.Lstat(dir)
	if err != nil || !fi.IsDir() {
		return nil, fmt.Errorf("not a directory: %s", dir)
	}

	var result []Skill
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		fullPath := filepath.Join(dir, entry.Name())

		// Skip symlinks — .agents is often a symlink to .claude
		if isSymlink(fullPath) {
			continue
		}

		if entry.IsDir() {
			// Subdirectory: look for SKILL.md or skill.md
			for _, name := range []string{"SKILL.md", "skill.md"} {
				candidate := filepath.Join(fullPath, name)
				if s := parseSkillFile(candidate, source, managed); s != nil {
					result = append(result, *s)
					break
				}
			}
		} else if strings.HasSuffix(entry.Name(), ".md") {
			if s := parseSkillFile(fullPath, source, managed); s != nil {
				result = append(result, *s)
			}
		}
	}
	return result, nil
}

// isSymlink returns true if the path is a symbolic link.
func isSymlink(path string) bool {
	fi, err := os.Lstat(path)
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeSymlink != 0
}

// parseSkillFile reads a .md file and extracts name/description from YAML frontmatter.
// Returns nil if the file cannot be read or has no valid frontmatter.
func parseSkillFile(path, source string, managed bool) *Skill {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	fm := extractFrontmatter(data)
	if fm == nil {
		return nil
	}

	if fm.Name == "" {
		return nil
	}

	desc := strings.TrimSpace(fm.Description)
	// Unfold YAML folded/scalar blocks
	desc = strings.ReplaceAll(desc, "\n", " ")
	desc = CollapseSpaces(desc)

	return &Skill{
		Name:        fm.Name,
		Description: desc,
		Source:      source,
		Managed:     managed,
		FilePath:    path,
	}
}

// ParseFrontmatter reads a SKILL.md file and extracts name + description.
// Returns ok=false if the file cannot be read, has no frontmatter, or name is empty.
func ParseFrontmatter(path string) (name, description string, ok bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", false
	}

	fm := extractFrontmatter(data)
	if fm == nil || fm.Name == "" {
		return "", "", false
	}

	desc := strings.TrimSpace(fm.Description)
	desc = strings.ReplaceAll(desc, "\n", " ")
	desc = CollapseSpaces(desc)

	if len([]rune(desc)) > 120 {
		runes := []rune(desc)
		desc = string(runes[:117]) + "..."
	}
	return fm.Name, desc, true
}

// extractFrontmatter extracts and parses YAML frontmatter from markdown content.
// Frontmatter is delimited by `---` on its own line at the start of the file.
func extractFrontmatter(data []byte) *skillFrontmatter {
	if !bytes.HasPrefix(data, []byte("---")) {
		return nil
	}

	// Find closing ---
	end := bytes.Index(data[3:], []byte("\n---"))
	if end < 0 {
		return nil
	}
	yamlBlock := data[3 : end+3]

	var fm skillFrontmatter
	if err := yaml.Unmarshal(yamlBlock, &fm); err != nil {
		return nil
	}
	return &fm
}

// dedup removes duplicate skills by name. Project skills override global ones.
func dedup(skills []Skill) []Skill {
	seen := make(map[string]int) // name -> index in result
	var result []Skill

	for _, s := range skills {
		if idx, ok := seen[s.Name]; ok {
			// Project overrides global
			if s.Source == SourceProject && result[idx].Source == SourceGlobal {
				result[idx] = s
			}
		} else {
			seen[s.Name] = len(result)
			result = append(result, s)
		}
	}
	return result
}

func CollapseSpaces(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	prev := false
	for _, r := range s {
		if r == ' ' || r == '\t' {
			if prev {
				continue
			}
			prev = true
		} else {
			prev = false
		}
		b.WriteRune(r)
	}
	return b.String()
}
