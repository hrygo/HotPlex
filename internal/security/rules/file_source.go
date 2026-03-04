package rules

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"regexp"

	"github.com/hrygo/hotplex/internal/security"
)

// Compile-time interface verification
var _ security.RuleSource = (*FileRuleSource)(nil)

// FileRuleSource loads security rules from a file.
type FileRuleSource struct {
	filename string
	name     string
}

// RuleDefinition represents a rule loaded from a file.
type RuleDefinition struct {
	Pattern     string `json:"pattern"`
	Description string `json:"description"`
	Level       string `json:"level"`
	Category    string `json:"category"`
	Type        string `json:"type"` // "danger" or "safe"
}

// NewFileRuleSource creates a new FileRuleSource.
func NewFileRuleSource(filename string) *FileRuleSource {
	return &FileRuleSource{
		filename: filename,
		name:     "file:" + filename,
	}
}

// LoadRules loads rules from the configured file.
func (f *FileRuleSource) LoadRules(ctx context.Context) ([]security.SecurityRule, error) {
	file, err := os.Open(f.filename)
	if err != nil {
		return nil, errors.New("security: failed to open rules file: " + err.Error())
	}
	defer func() { _ = file.Close() }()

	// Try to parse as JSON first
	rules, err := f.loadJSONRules(file)
	if err == nil {
		return rules, nil
	}

	// Reset file for line-based parsing
	if _, err := file.Seek(0, 0); err != nil {
		return nil, errors.New("security: failed to seek rules file: " + err.Error())
	}

	return f.loadLineBasedRules(file)
}

// loadJSONRules attempts to load rules from a JSON file.
func (f *FileRuleSource) loadJSONRules(file *os.File) ([]security.SecurityRule, error) {
	decoder := json.NewDecoder(file)
	var defs []RuleDefinition
	if err := decoder.Decode(&defs); err != nil {
		return nil, err
	}

	rules := make([]security.SecurityRule, 0, len(defs))
	for _, def := range defs {
		rule, err := f.parseRuleDefinition(def)
		if err != nil {
			continue
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

// loadLineBasedRules loads rules from a line-based format.
// Format: "pattern|description|level|category|type"
func (f *FileRuleSource) loadLineBasedRules(file *os.File) ([]security.SecurityRule, error) {
	scanner := bufio.NewScanner(file)
	rules := make([]security.SecurityRule, 0)

	for scanner.Scan() {
		line := scanner.Text()
		line = removeComment(line)
		if line == "" {
			continue
		}

		def, err := parseLineRule(line)
		if err != nil {
			continue
		}

		rule, err := f.parseRuleDefinition(def)
		if err != nil {
			continue
		}
		rules = append(rules, rule)
	}

	return rules, scanner.Err()
}

// parseRuleDefinition converts a RuleDefinition to a SecurityRule.
func (f *FileRuleSource) parseRuleDefinition(def RuleDefinition) (security.SecurityRule, error) {
	re, err := regexp.Compile("(?i)" + def.Pattern)
	if err != nil {
		return nil, errors.New("invalid pattern: " + err.Error())
	}

	var level security.DangerLevel
	switch def.Level {
	case "critical":
		level = security.DangerLevelCritical
	case "high":
		level = security.DangerLevelHigh
	case "moderate":
		level = security.DangerLevelModerate
	case "safe":
		level = security.DangerLevelSafe
	default:
		level = security.DangerLevelModerate
	}

	if def.Type == "safe" || level == security.DangerLevelSafe {
		return &security.SafePatternRule{
			Pattern:     re,
			Description: def.Description,
			Category:    def.Category,
		}, nil
	}

	return &security.RegexRule{
		Pattern:     re,
		Description: def.Description,
		Level:       level,
		Category:    def.Category,
	}, nil
}

// parseLineRule parses a line-based rule definition.
func parseLineRule(line string) (RuleDefinition, error) {
	parts := splitLineRule(line)
	if len(parts) < 4 {
		return RuleDefinition{}, errors.New("invalid format")
	}

	def := RuleDefinition{
		Pattern:     parts[0],
		Description: parts[1],
		Level:       parts[2],
		Category:    parts[3],
	}
	if len(parts) > 4 {
		def.Type = parts[4]
	}
	return def, nil
}

// splitLineRule splits a line-based rule, handling escaped pipes.
func splitLineRule(line string) []string {
	var parts []string
	var current []rune
	escapeNext := false

	for _, r := range line {
		if escapeNext {
			current = append(current, r)
			escapeNext = false
			continue
		}
		if r == '\\' {
			escapeNext = true
			continue
		}
		if r == '|' {
			parts = append(parts, string(current))
			current = nil
			continue
		}
		current = append(current, r)
	}
	if len(current) > 0 {
		parts = append(parts, string(current))
	}
	return parts
}

// removeComment removes comments from a line.
func removeComment(line string) string {
	for i, r := range line {
		if r == '#' {
			return line[:i]
		}
	}
	return line
}

// Name returns the name of this rule source.
func (f *FileRuleSource) Name() string {
	return f.name
}
