package agentconfig

import (
	"fmt"
	"os"
	"path/filepath"
)

// InjectCRules writes C-channel content (USER.md, MEMORY.md) to the
// Claude Code rules directory. Claude Code auto-discovers .md files in
// workdir/.claude/rules/ and injects them into M0 User Context (hedged).
func InjectCRules(workdir string, configs *AgentConfigs) error {
	if configs == nil || !configs.HasCRules() {
		return nil
	}

	rulesDir := filepath.Join(workdir, ".claude", "rules")
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		return fmt.Errorf("agentconfig: mkdir rules: %w", err)
	}

	files := map[string]string{
		"hotplex-user.md":   configs.User,
		"hotplex-memory.md": configs.Memory,
	}
	for name, content := range files {
		if content == "" {
			continue
		}
		path := filepath.Join(rulesDir, name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return fmt.Errorf("agentconfig: write %s: %w", name, err)
		}
	}
	return nil
}
