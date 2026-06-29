// Package agentconfig loads and assembles agent personality, rules, and context
// files from a shared configuration directory, injecting them into worker
// sessions via B-channel (system-level) and C-channel (context-level) paths.
package agentconfig

import (
	"bufio"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// ErrInvalidBotName is returned when botName contains path traversal components.
var ErrInvalidBotName = errors.New("agentconfig: invalid botName")

// LegacyDefaultBotName is the directory name used for single-bot agent-config
// before PR #679. Kept for backward compatibility during migration.
const LegacyDefaultBotName = "default"

// AgentConfigs holds loaded content for all agent config files.
type AgentConfigs struct {
	Soul   string // SOUL.md   (B channel)
	Agents string // AGENTS.md (B channel)
	Skills string // SKILLS.md (B channel)
	User   string // USER.md   (C channel)
	Memory string // MEMORY.md (C channel)
}

// MaxFileChars is the maximum character limit per file.
const MaxFileChars = 8_000

// MaxTotalChars is the maximum combined character limit across all files.
const MaxTotalChars = 40_000

// Load reads all config files from dir using 3-level per-file fallback:
//
//  1. dir/{platform}/{botName}/{file} — bot-level (highest priority)
//  2. dir/{platform}/{file}           — platform-level
//  3. dir/{file}                      — global-level
//
// Each file resolves independently. Missing files fall through to the next level.
// Platform can be "slack", "feishu", "webchat", or "" (no platform-level lookup).
// botName is the YAML config name (e.g., "my-bot"). When empty (single-bot mode),
// bot-level lookup is skipped and resolution falls through to platform-level.
// injectExclude lists file base names to skip (e.g., ["SOUL.md", "MEMORY.md"]).
// Files listed in injectExclude are not loaded; their corresponding config fields
// remain empty. META-COGNITION.md is never excluded (go:embed, always injected).
// Returns AgentConfigs with frontmatter stripped and size limits enforced.
func Load(dir, platform, botName string, injectExclude ...string) (*AgentConfigs, error) {
	if dir == "" {
		return &AgentConfigs{}, nil
	}

	// Defense-in-depth: sanitize platform to prevent path traversal.
	// Values are internal constants ("slack", "feishu", "webchat"), but
	// filepath.Base neutralizes any future injection without breaking empty string.
	if platform != "" {
		platform = filepath.Base(platform)
	}

	if err := ValidateBotName(botName); err != nil {
		return nil, err
	}

	c := &AgentConfigs{}
	var total int

	load := func(baseName string, target *string) error {
		if shouldExclude(baseName, injectExclude) {
			return nil
		}
		content, err := resolveFile(dir, platform, botName, baseName)
		if err != nil {
			return err
		}
		n := len(content)
		if n == 0 {
			return nil
		}
		if total+n > MaxTotalChars {
			origN := n
			n = MaxTotalChars - total
			if n <= 0 {
				slog.Warn("agentconfig: total budget exhausted, skipping file",
					"file", baseName, "total", total, "limit", MaxTotalChars)
				return nil
			}
			content = content[:n]
			slog.Warn("agentconfig: file truncated to fit total budget",
				"file", baseName, "original", origN, "truncated", n, "total_after", total+n)
		}
		total += n
		*target = content
		return nil
	}

	if err := load("SOUL.md", &c.Soul); err != nil {
		return nil, err
	}
	if err := load("AGENTS.md", &c.Agents); err != nil {
		return nil, err
	}
	if err := load("SKILLS.md", &c.Skills); err != nil {
		return nil, err
	}
	if err := load("USER.md", &c.User); err != nil {
		return nil, err
	}
	if err := load("MEMORY.md", &c.Memory); err != nil {
		return nil, err
	}

	return c, nil
}

// configFiles lists recognized agent config file names.
var configFiles = []string{"SOUL.md", "AGENTS.md", "SKILLS.md", "USER.md", "MEMORY.md"}

// KnownFiles returns the list of recognized config file names for validation/logging.
func KnownFiles() []string {
	return slices.Clone(configFiles)
}

// knownPlatforms lists platform identifiers that own a platform-level agent
// config directory (dir/<platform>/). These are first-class platforms whose
// team-default files resolve through Load/LoadForWorkspace. Order is stable so
// diagnostics render deterministically.
//
// Single source of truth for valid platforms: when adding one here, also
// update the duplicated enum in docs/swagger/swagger.json and the @Param
// Enums(...) annotations in bot_config_handlers.go (OpenAPI can't reference
// Go vars, so the list is mirrored in three places).
var knownPlatforms = []string{"slack", "feishu", "webchat"}

// KnownPlatforms returns the list of recognized platform identifiers for
// validation and diagnostics. The returned slice is a copy and may be mutated
// by callers without affecting the package state.
func KnownPlatforms() []string {
	return slices.Clone(knownPlatforms)
}

// IsValidPlatform reports whether platform is a recognized identifier that may
// own platform-level team-default files (dir/<platform>/). Used by admin
// channel-config endpoints to reject unknown platforms before path resolution.
func IsValidPlatform(platform string) bool {
	return slices.Contains(knownPlatforms, platform)
}

// shouldExclude reports whether a config file should be skipped from injection.
// baseName is matched case-insensitively against the exclude list.
// META-COGNITION.md is never excluded (it is go:embed, always injected outside Load).
func shouldExclude(baseName string, exclude []string) bool {
	if len(exclude) == 0 {
		return false
	}
	for _, name := range exclude {
		if strings.EqualFold(name, baseName) {
			return true
		}
	}
	return false
}

// ValidateExcludeList returns entries from exclude that do not match any known
// config file name. Matching is case-insensitive. Returns nil if all entries are valid.
func ValidateExcludeList(exclude []string) []string {
	var unknown []string
	for _, name := range exclude {
		found := false
		for _, cfg := range configFiles {
			if strings.EqualFold(name, cfg) {
				found = true
				break
			}
		}
		if !found {
			unknown = append(unknown, name)
		}
	}
	return unknown
}

// HasGlobalFiles reports whether any config file exists at the global level (dir/<file>).
func HasGlobalFiles(dir string) bool {
	for _, name := range configFiles {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return true
		}
	}
	return false
}

// resolveFile implements the 3-level per-file fallback.
// Returns the content of the first non-empty file found, or ("", nil) if none exist.
// Non-NotExist I/O errors (e.g., permission denied) are propagated immediately
// rather than falling through — a file that exists but is unreadable indicates
// a real configuration problem that should not be silently masked.
func resolveFile(dir, platform, botName, fileName string) (string, error) {
	// 1. Bot-level: dir/platform/botName/fileName
	if botName != "" && platform != "" {
		content, err := readFile(filepath.Join(dir, platform, botName), fileName)
		if err != nil {
			return "", err
		}
		if content != "" {
			return content, nil
		}
	}
	// 2. Platform-level: dir/platform/fileName
	if platform != "" {
		content, err := readFile(filepath.Join(dir, platform), fileName)
		if err != nil {
			return "", err
		}
		if content != "" {
			return content, nil
		}
		// 2b. Legacy backward compat: dir/platform/default/fileName
		// Before PR #679, single-bot mode used "default" as botName. If a user
		// created configs under {platform}/default/ between #678 and #679, this
		// fallback ensures they are still discovered. New deployments should use
		// platform-level (dir/platform/fileName) instead.
		if botName == "" {
			content, err := readFile(filepath.Join(dir, platform, LegacyDefaultBotName), fileName)
			if err != nil {
				return "", err
			}
			if content != "" {
				slog.Warn("agentconfig: legacy default/ directory detected; move files to platform-level",
					"platform", platform, "file", fileName)
				return content, nil
			}
		}
	}
	// 3. Global-level: dir/fileName
	return readFile(dir, fileName)
}

// readFile reads a file, strips YAML frontmatter, and enforces per-file size limit.
// Returns ("", nil) if the file does not exist (expected), ("", error) for other errors.
func readFile(dir, name string) (string, error) {
	path := filepath.Join(dir, name)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("agentconfig: read %s: %w", name, err)
	}
	s := stripFrontmatter(string(data))
	if len(s) > MaxFileChars {
		slog.Warn("agentconfig: file exceeds per-file limit, truncated",
			"file", name, "original", len(s), "limit", MaxFileChars)
		s = s[:MaxFileChars]
	}
	return s, nil
}

// stripFrontmatter removes YAML frontmatter (--- blocks) from markdown content.
func stripFrontmatter(s string) string {
	if !strings.HasPrefix(s, "---\n") && !strings.HasPrefix(s, "---\r") {
		return s
	}
	scanner := bufio.NewScanner(strings.NewReader(s))
	scanner.Scan() // skip opening ---
	for scanner.Scan() {
		line := scanner.Text()
		if line == "---" || line == "---\r" {
			rest := ""
			if scanner.Scan() {
				rest = scanner.Text()
			}
			var buf strings.Builder
			buf.WriteString(rest)
			for scanner.Scan() {
				if buf.Len() > 0 {
					buf.WriteByte('\n')
				}
				buf.WriteString(scanner.Text())
			}
			return buf.String()
		}
	}
	// Malformed frontmatter — return original content as-is.
	return s
}

// EnsureDir creates the config directory and its parents if they don't exist.
func EnsureDir(dir string) error {
	if dir == "" {
		return fmt.Errorf("agentconfig: empty dir")
	}
	return os.MkdirAll(dir, 0o755)
}

// IsEmpty returns true if all config fields are empty.
func (c *AgentConfigs) IsEmpty() bool {
	return c.Soul == "" && c.Agents == "" && c.Skills == "" &&
		c.User == "" && c.Memory == ""
}

// LoadForWorkspace resolves WebChat-track agent configs via two-level inheritance:
// team defaults (loaded from dir via Load with botName="") → workspace overrides.
//
// Each non-empty override entry replaces the corresponding team-default field.
// injectExclude has highest priority: an excluded file is never injected even if
// overridden. Unknown override keys are silently ignored (defense-in-depth —
// ValidateOverrides rejects them at write time).
//
// The Message Channel track calls Load directly; this function is WebChat-only.
// See design spec §5.
func LoadForWorkspace(dir, platform string, overrides map[string]string, injectExclude ...string) (*AgentConfigs, error) {
	base, err := Load(dir, platform, "", injectExclude...)
	if err != nil {
		return nil, err
	}
	applyOverrides(base, overrides, injectExclude)
	enforceTotalLimit(base)
	return base, nil
}

// applyOverrides applies per-file overrides onto base in place. Only keys in
// configFiles are applied; empty values do not override; excluded files are skipped.
func applyOverrides(base *AgentConfigs, overrides map[string]string, injectExclude []string) {
	set := func(baseName, val string, target *string) {
		if val == "" || shouldExclude(baseName, injectExclude) {
			return
		}
		*target = val
	}
	for k, v := range overrides {
		switch k {
		case "SOUL.md":
			set(k, v, &base.Soul)
		case "AGENTS.md":
			set(k, v, &base.Agents)
		case "SKILLS.md":
			set(k, v, &base.Skills)
		case "USER.md":
			set(k, v, &base.User)
		case "MEMORY.md":
			set(k, v, &base.Memory)
		}
	}
}

// enforceTotalLimit truncates merged config fields so the combined size stays within
// MaxTotalChars. Load already enforces this on team defaults, but overrides can grow
// individual fields beyond the budget (write-side ValidateOverrides caps each override
// at MaxFileChars, not the merged total); this re-checks the merged result as
// defense-in-depth. Truncates in field order (SOUL→AGENTS→SKILLS→USER→MEMORY).
func enforceTotalLimit(c *AgentConfigs) {
	fields := []struct {
		name   string
		target *string
	}{
		{"SOUL.md", &c.Soul},
		{"AGENTS.md", &c.Agents},
		{"SKILLS.md", &c.Skills},
		{"USER.md", &c.User},
		{"MEMORY.md", &c.Memory},
	}
	total := 0
	for _, f := range fields {
		n := len(*f.target)
		if rem := MaxTotalChars - total; n > rem {
			slog.Warn("agentconfig: merged config exceeds total limit after overrides, truncated",
				"file", f.name, "original", n, "remaining", rem, "limit", MaxTotalChars)
			*f.target = (*f.target)[:rem]
			n = rem
		}
		total += n
	}
}
