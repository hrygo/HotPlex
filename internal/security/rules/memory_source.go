package rules

import (
	"context"
	"regexp"
	"sync"

	"github.com/hrygo/hotplex/internal/security"
)

// Compile-time interface verification
var _ security.RuleSource = (*MemoryRuleSource)(nil)

// MemoryRuleSource provides in-memory security rules.
type MemoryRuleSource struct {
	mu    sync.RWMutex
	rules []security.SecurityRule
	name  string
}

// NewMemoryRuleSource creates a new MemoryRuleSource with the given rules.
func NewMemoryRuleSource(name string, rules []security.SecurityRule) *MemoryRuleSource {
	return &MemoryRuleSource{
		rules: rules,
		name:  name,
	}
}

// LoadRules returns the rules loaded in memory.
func (m *MemoryRuleSource) LoadRules(ctx context.Context) ([]security.SecurityRule, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]security.SecurityRule, len(m.rules))
	copy(result, m.rules)
	return result, nil
}

// Name returns the name of this rule source.
func (m *MemoryRuleSource) Name() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.name
}

// AddRule adds a new rule to the source.
func (m *MemoryRuleSource) AddRule(rule security.SecurityRule) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rules = append(m.rules, rule)
}

// DefaultDevelopToolsRules returns the default safe development tool rules.
func DefaultDevelopToolsRules() []security.SecurityRule {
	patterns := []struct {
		pattern     string
		description string
		category    string
	}{
		// Go commands
		{`^go\s+(build|run|test|vet|fmt|mod|get|install|list|version|env|tool|bug|clean|doc|generate|help|init|link)\b`, "Go build tool", "develop-tools"},
		{`^go\s+mod\s+(download|init|tidy|graph|why|verify)\b`, "Go mod command", "develop-tools"},

		// Node commands
		{`^(npm|yarn|pnpm)\s+(install|run|test|build|start|dev|serve|lint|format|add|remove|update)\b`, "Node package manager", "develop-tools"},
		{`^node\s+[^\-]`, "Node.js runtime (safe invocation)", "develop-tools"},
		{`^npx\s+`, "Node package executor", "develop-tools"},

		// Python commands
		{`^python[23]?\s+(file\.py|script\.py|module| -m )\b`, "Python safe invocation", "develop-tools"},
		{`^pip[23]?\s+(install|uninstall|freeze|list|show|check)\b`, "Python pip", "develop-tools"},
		{`^poetry\s+(install|run|build|publish)\b`, "Python Poetry", "develop-tools"},
		{`^pipenv\s+(install|run|shell)\b`, "Python Pipenv", "develop-tools"},

		// Docker commands
		{`^docker\s+(build|ps|logs|images|volume|network|inspect|exec)\s+`, "Docker container", "develop-tools"},
		{`^docker-compose\s+`, "Docker Compose", "develop-tools"},

		// Git commands
		{`^git\s+(status|log|diff|show|branch|checkout|fetch|pull|push|clone|init|add|commit|merge|rebase|stash|cherry-pick)\b`, "Git version control", "develop-tools"},

		// System tools
		{`^(ls|cd|pwd|mkdir|rmdir|touch|head|tail|grep|find|awk|sed|sort|uniq|wc|cut|tr)\b`, "Unix utilities", "develop-tools"},
		{`^(date|time|which|whoami|id|hostname|uname|uptime)\b`, "System utilities", "develop-tools"},
	}

	rules := make([]security.SecurityRule, 0, len(patterns))
	for _, p := range patterns {
		re, err := regexp.Compile(p.pattern)
		if err != nil {
			continue
		}
		rules = append(rules, &security.SafePatternRule{
			Pattern:     re,
			Description: p.description,
			Category:    p.category,
		})
	}
	return rules
}
