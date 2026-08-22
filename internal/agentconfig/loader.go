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

const (
	FileSoul         = "SOUL.md"
	FileAgents       = "AGENTS.md"
	FileTools        = "TOOLS.md"
	LegacyFileSkills = "SKILLS.md"
	FileUser         = "USER.md"
	FileMemory       = "MEMORY.md"
)

// AgentConfigs holds loaded content for all agent config files.
type AgentConfigs struct {
	Soul   string // SOUL.md   (B channel)
	Agents string // AGENTS.md (B channel)
	Tools  string // TOOLS.md  (B channel; SKILLS.md is a legacy read alias)
	User   string // USER.md   (C channel)
	Memory string // MEMORY.md (C channel)
}

type fileState struct {
	content string
	found   bool
	legacy  bool
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
		state, err := resolveFile(dir, platform, botName, baseName)
		if err != nil {
			return err
		}
		if !state.found {
			return nil
		}
		content := state.content
		n := len(content)
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

	if err := load(FileSoul, &c.Soul); err != nil {
		return nil, err
	}
	if err := load(FileAgents, &c.Agents); err != nil {
		return nil, err
	}
	if err := load(FileTools, &c.Tools); err != nil {
		return nil, err
	}
	if err := load(FileUser, &c.User); err != nil {
		return nil, err
	}
	if err := load(FileMemory, &c.Memory); err != nil {
		return nil, err
	}

	return c, nil
}

// configFiles lists recognized agent config file names.
var configFiles = []string{FileSoul, FileAgents, FileTools, FileUser, FileMemory}

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
	baseCanonical, baseKnown := canonicalFileName(baseName)
	for _, name := range exclude {
		nameCanonical, nameKnown := canonicalFileName(name)
		if baseKnown && nameKnown && strings.EqualFold(nameCanonical, baseCanonical) {
			return true
		}
		if !baseKnown && !nameKnown && strings.EqualFold(name, baseName) {
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
		if _, found := canonicalFileName(name); !found {
			unknown = append(unknown, name)
		}
	}
	return unknown
}

// HasGlobalFiles reports whether any config file exists at the global level (dir/<file>).
func HasGlobalFiles(dir string) bool {
	for _, name := range configFiles {
		for _, candidate := range readAliases(name) {
			if _, err := os.Stat(filepath.Join(dir, candidate)); err == nil {
				return true
			}
		}
	}
	return false
}

// resolveFile implements the 3-level per-file fallback.
// A present file resolves the slot even when its content is empty; only a
// missing file falls through to the next scope.
// Non-NotExist I/O errors (e.g., permission denied) are propagated immediately
// rather than falling through — a file that exists but is unreadable indicates
// a real configuration problem that should not be silently masked.
func resolveFile(dir, platform, botName, fileName string) (fileState, error) {
	// 1. Bot-level: dir/platform/botName/fileName
	if botName != "" && platform != "" {
		state, err := readLogicalFile(filepath.Join(dir, platform, botName), fileName)
		if err != nil {
			return fileState{}, err
		}
		if state.found {
			return state, nil
		}
	}
	// 2. Platform-level: dir/platform/fileName
	if platform != "" {
		state, err := readLogicalFile(filepath.Join(dir, platform), fileName)
		if err != nil {
			return fileState{}, err
		}
		if state.found {
			return state, nil
		}
		// 2b. Legacy backward compat: dir/platform/default/fileName
		// Before PR #679, single-bot mode used "default" as botName. If a user
		// created configs under {platform}/default/ between #678 and #679, this
		// fallback ensures they are still discovered. New deployments should use
		// platform-level (dir/platform/fileName) instead.
		if botName == "" {
			state, err := readLogicalFile(filepath.Join(dir, platform, LegacyDefaultBotName), fileName)
			if err != nil {
				return fileState{}, err
			}
			if state.found {
				slog.Warn("agentconfig: legacy default/ directory detected; move files to platform-level",
					"platform", platform, "file", fileName)
				return state, nil
			}
		}
	}
	// 3. Global-level: dir/fileName
	return readLogicalFile(dir, fileName)
}

func readLogicalFile(dir, name string) (fileState, error) {
	aliases := readAliases(name)
	for i, candidate := range aliases {
		state, err := readFileState(dir, candidate)
		if err != nil {
			return fileState{}, err
		}
		if !state.found {
			continue
		}
		state.legacy = i > 0
		if i == 0 && len(aliases) > 1 {
			if _, statErr := os.Stat(filepath.Join(dir, aliases[1])); statErr == nil {
				slog.Warn("agentconfig: canonical and legacy tools files coexist; using canonical",
					"dir", dir, "canonical", FileTools, "legacy", LegacyFileSkills)
			}
		}
		if state.legacy {
			slog.Warn("agentconfig: legacy tools filename detected; migrate to TOOLS.md",
				"dir", dir, "file", LegacyFileSkills)
		}
		return state, nil
	}
	return fileState{}, nil
}

func readAliases(name string) []string {
	canonical, known := canonicalFileName(name)
	if known && canonical == FileTools {
		return []string{FileTools, LegacyFileSkills}
	}
	return []string{name}
}

func canonicalFileName(name string) (string, bool) {
	if strings.EqualFold(name, LegacyFileSkills) {
		return FileTools, true
	}
	for _, candidate := range configFiles {
		if strings.EqualFold(name, candidate) {
			return candidate, true
		}
	}
	return "", false
}

// readFileState reads a file, strips YAML frontmatter, and enforces per-file size limit.
// Missing and present-empty files remain distinct so an empty file can stop fallback.
func readFileState(dir, name string) (fileState, error) {
	path := filepath.Join(dir, name)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fileState{}, nil
		}
		return fileState{}, fmt.Errorf("agentconfig: read %s: %w", name, err)
	}
	s := stripFrontmatter(string(data))
	if len(s) > MaxFileChars {
		slog.Warn("agentconfig: file exceeds per-file limit, truncated",
			"file", name, "original", len(s), "limit", MaxFileChars)
		s = s[:MaxFileChars]
	}
	return fileState{content: s, found: true}, nil
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

// EffectiveContentEmpty reports whether a loaded Markdown value has no
// meaningful body after the same frontmatter normalization used by Load.
// Diagnostics use this helper so migration warnings match loader semantics.
func EffectiveContentEmpty(s string) bool {
	return strings.TrimSpace(stripFrontmatter(s)) == ""
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
	return c.Soul == "" && c.Agents == "" && c.Tools == "" &&
		c.User == "" && c.Memory == ""
}

// LoadForWorkspace resolves WebChat-track agent configs via two-level inheritance:
// team defaults (loaded from dir via Load with botName="") → workspace overrides.
//
// Each present override entry replaces the corresponding team-default field;
// an empty value explicitly clears the slot.
// injectExclude has highest priority: an excluded file is never injected even if
// overridden. Unknown override keys are silently ignored (defense-in-depth —
// ValidateOverrides rejects them at write time).
//
// The Message Channel track calls Load directly; this function is WebChat-only.
// See design spec §5.
func LoadForWorkspace(dir, platform string, overrides map[string]string, injectExclude ...string) (*AgentConfigs, error) {
	if _, hasTools := overrides[FileTools]; hasTools {
		if _, hasLegacy := overrides[LegacyFileSkills]; hasLegacy {
			return nil, fmt.Errorf("%w: %s and %s", ErrConflictingConfigFiles, FileTools, LegacyFileSkills)
		}
	}
	base, err := Load(dir, platform, "", injectExclude...)
	if err != nil {
		return nil, err
	}
	applyOverrides(base, overrides, injectExclude)
	enforceTotalLimit(base)
	return base, nil
}

// applyOverrides applies per-file overrides onto base in place. Only canonical
// keys and the legacy Tools alias are applied; excluded files are skipped.
func applyOverrides(base *AgentConfigs, overrides map[string]string, injectExclude []string) {
	set := func(baseName, val string, target *string) {
		if shouldExclude(baseName, injectExclude) {
			return
		}
		*target = val
	}
	for k, v := range overrides {
		switch k {
		case FileSoul:
			set(k, v, &base.Soul)
		case FileAgents:
			set(k, v, &base.Agents)
		case FileTools, LegacyFileSkills:
			set(k, v, &base.Tools)
		case FileUser:
			set(k, v, &base.User)
		case FileMemory:
			set(k, v, &base.Memory)
		}
	}
}

// enforceTotalLimit truncates merged config fields so the combined size stays within
// MaxTotalChars. Load already enforces this on team defaults, but overrides can grow
// individual fields beyond the budget (write-side ValidateOverrides caps each override
// at MaxFileChars, not the merged total); this re-checks the merged result as
// defense-in-depth. Truncates in field order (SOUL→AGENTS→TOOLS→USER→MEMORY).
func enforceTotalLimit(c *AgentConfigs) {
	fields := []struct {
		name   string
		target *string
	}{
		{FileSoul, &c.Soul},
		{FileAgents, &c.Agents},
		{FileTools, &c.Tools},
		{FileUser, &c.User},
		{FileMemory, &c.Memory},
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
