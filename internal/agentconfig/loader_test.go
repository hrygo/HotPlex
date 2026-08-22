package agentconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoad(t *testing.T) {
	t.Parallel()

	t.Run("empty dir returns empty configs", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		cfg, err := Load(dir, "", "")
		require.NoError(t, err)
		require.True(t, cfg.IsEmpty())
	})

	t.Run("nonexistent dir returns empty configs", func(t *testing.T) {
		t.Parallel()
		cfg, err := Load("/nonexistent/path", "", "")
		require.NoError(t, err)
		require.True(t, cfg.IsEmpty())
	})

	t.Run("empty dir string returns empty configs", func(t *testing.T) {
		t.Parallel()
		cfg, err := Load("", "", "")
		require.NoError(t, err)
		require.True(t, cfg.IsEmpty())
	})

	t.Run("loads base files", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, dir, "SOUL.md", "---\nversion: 1\n---\nI am an AI assistant.")
		writeFile(t, dir, "AGENTS.md", "Workspace rules here.")
		writeFile(t, dir, "USER.md", "User profile data.")

		cfg, err := Load(dir, "", "")
		require.NoError(t, err)
		require.False(t, cfg.IsEmpty())
		require.Equal(t, "I am an AI assistant.", cfg.Soul)
		require.Equal(t, "Workspace rules here.", cfg.Agents)
		require.Equal(t, "User profile data.", cfg.User)
		require.Empty(t, cfg.Tools)
		require.Empty(t, cfg.Memory)
	})

	t.Run("strips yaml frontmatter", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, dir, "SOUL.md", "---\nversion: 1\ndescription: test\n---\nActual content.")

		cfg, err := Load(dir, "", "")
		require.NoError(t, err)
		require.Equal(t, "Actual content.", cfg.Soul)
	})

	t.Run("platform directory overrides global", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, dir, "SOUL.md", "Global soul.")
		writeFile(t, dir, "slack/SOUL.md", "Slack soul.")

		cfg, err := Load(dir, "slack", "")
		require.NoError(t, err)
		require.Equal(t, "Slack soul.", cfg.Soul)
	})

	t.Run("bot-level overrides platform-level", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, dir, "SOUL.md", "Global soul.")
		writeFile(t, dir, "slack/SOUL.md", "Platform soul.")
		writeFile(t, dir, "slack/U12345/SOUL.md", "Bot soul.")

		cfg, err := Load(dir, "slack", "U12345")
		require.NoError(t, err)
		require.Equal(t, "Bot soul.", cfg.Soul)
	})

	t.Run("falls back to global when platform dir missing", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, dir, "SOUL.md", "Global soul.")

		cfg, err := Load(dir, "slack", "")
		require.NoError(t, err)
		require.Equal(t, "Global soul.", cfg.Soul)
	})

	t.Run("each file resolves independently", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, dir, "SOUL.md", "Global soul.")
		writeFile(t, dir, "AGENTS.md", "Global agents.")
		writeFile(t, dir, "slack/SOUL.md", "Slack soul.")
		// AGENTS.md not in slack/ — should use global.

		cfg, err := Load(dir, "slack", "")
		require.NoError(t, err)
		require.Equal(t, "Slack soul.", cfg.Soul)
		require.Equal(t, "Global agents.", cfg.Agents)
	})

	t.Run("suffix files are not loaded", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, dir, "SOUL.md", "Base soul.")
		writeFile(t, dir, "SOUL.slack.md", "Old-style variant.")

		cfg, err := Load(dir, "slack", "")
		require.NoError(t, err)
		require.Equal(t, "Base soul.", cfg.Soul)
		// SOUL.slack.md is NOT loaded — old suffix mechanism removed.
	})

	t.Run("path traversal botName rejected", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		_, err := Load(dir, "slack", "../etc")
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid botName")
	})

	t.Run("path traversal with dots rejected", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		_, err := Load(dir, "slack", "foo/bar")
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid botName")
	})

	t.Run("empty file explicitly clears and stops fallback", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, dir, "SOUL.md", "Global soul.")
		writeFile(t, dir, "slack/SOUL.md", "---\n---\n") // frontmatter only = empty content

		cfg, err := Load(dir, "slack", "")
		require.NoError(t, err)
		require.Empty(t, cfg.Soul)
	})

	t.Run("loads canonical tools file", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, dir, "TOOLS.md", "Prefer native tools.")

		cfg, err := Load(dir, "", "")
		require.NoError(t, err)
		require.Equal(t, "Prefer native tools.", cfg.Tools)
	})

	t.Run("loads legacy skills file as tools fallback", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, dir, "SKILLS.md", "Legacy tool notes.")

		cfg, err := Load(dir, "", "")
		require.NoError(t, err)
		require.Equal(t, "Legacy tool notes.", cfg.Tools)
	})

	t.Run("canonical tools wins over legacy in same scope", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, dir, "TOOLS.md", "Canonical.")
		writeFile(t, dir, "SKILLS.md", "Legacy.")

		cfg, err := Load(dir, "", "")
		require.NoError(t, err)
		require.Equal(t, "Canonical.", cfg.Tools)
	})

	t.Run("empty canonical tools masks legacy in same scope", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, dir, "TOOLS.md", "")
		writeFile(t, dir, "SKILLS.md", "Legacy.")

		cfg, err := Load(dir, "", "")
		require.NoError(t, err)
		require.Empty(t, cfg.Tools)
	})

	t.Run("bot legacy tools beats platform canonical", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, dir, "slack/TOOLS.md", "Platform canonical.")
		writeFile(t, dir, "slack/U12345/SKILLS.md", "Bot legacy.")

		cfg, err := Load(dir, "slack", "U12345")
		require.NoError(t, err)
		require.Equal(t, "Bot legacy.", cfg.Tools)
	})

	t.Run("empty bot canonical tools masks lower scopes", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, dir, "TOOLS.md", "Global.")
		writeFile(t, dir, "slack/TOOLS.md", "Platform.")
		writeFile(t, dir, "slack/U12345/TOOLS.md", "")

		cfg, err := Load(dir, "slack", "U12345")
		require.NoError(t, err)
		require.Empty(t, cfg.Tools)
	})

	t.Run("flat directory backward compatible", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, dir, "SOUL.md", "Soul content.")
		writeFile(t, dir, "AGENTS.md", "Agents content.")

		cfg, err := Load(dir, "", "")
		require.NoError(t, err)
		require.Equal(t, "Soul content.", cfg.Soul)
		require.Equal(t, "Agents content.", cfg.Agents)
	})

	t.Run("missing files are skipped", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, dir, "AGENTS.md", "Rules.")

		cfg, err := Load(dir, "", "")
		require.NoError(t, err)
		require.Empty(t, cfg.Soul)
		require.Equal(t, "Rules.", cfg.Agents)
		require.Empty(t, cfg.User)
	})

	t.Run("permission denied returns error", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		filePath := filepath.Join(dir, "SOUL.md")
		err := os.WriteFile(filePath, []byte("Content"), 0o000)
		require.NoError(t, err)

		_, err = Load(dir, "", "")
		require.Error(t, err)
		require.Contains(t, err.Error(), "agentconfig: read")
	})

	t.Run("permission denied at bot-level returns error", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, dir, "slack/U12345/SOUL.md", "Bot soul.")
		writeFile(t, dir, "slack/SOUL.md", "Platform soul.")

		botFile := filepath.Join(dir, "slack", "U12345", "SOUL.md")
		require.NoError(t, os.Chmod(botFile, 0o000))
		t.Cleanup(func() { _ = os.Chmod(botFile, 0o644) })

		_, err := Load(dir, "slack", "U12345")
		require.Error(t, err)
		require.Contains(t, err.Error(), "agentconfig: read")
	})

	t.Run("botName ignored when platform is empty", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, dir, "SOUL.md", "Global soul.")

		cfg, err := Load(dir, "", "U12345")
		require.NoError(t, err)
		require.Equal(t, "Global soul.", cfg.Soul)
	})
}

func TestSizeLimits(t *testing.T) {
	t.Parallel()

	t.Run("per file limit truncates", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		longContent := strings.Repeat("x", MaxFileChars+1000)
		writeFile(t, dir, "SOUL.md", longContent)

		cfg, err := Load(dir, "", "")
		require.NoError(t, err)
		require.Equal(t, MaxFileChars, len(cfg.Soul))
	})

	t.Run("total limit enforced", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		content := strings.Repeat("a", MaxTotalChars/2+1)
		writeFile(t, dir, "SOUL.md", content)
		writeFile(t, dir, "AGENTS.md", content)

		cfg, err := Load(dir, "", "")
		require.NoError(t, err)
		total := len(cfg.Soul) + len(cfg.Agents) + len(cfg.Tools) + len(cfg.User) + len(cfg.Memory)
		require.LessOrEqual(t, total, MaxTotalChars)
	})
}

func TestStripFrontmatter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"no frontmatter", "Hello", "Hello"},
		{"yaml frontmatter", "---\nversion: 1\n---\nContent", "Content"},
		{"empty frontmatter", "---\n---\nContent", "Content"},
		{"malformed no close", "---\nversion: 1\nContent", "---\nversion: 1\nContent"},
		{"multiline content", "---\nv: 1\n---\nLine1\nLine2", "Line1\nLine2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := stripFrontmatter(tt.input)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestBuildSystemPrompt(t *testing.T) {
	t.Parallel()

	t.Run("nil configs returns empty", func(t *testing.T) {
		require.Empty(t, BuildSystemPrompt(nil))
	})

	t.Run("empty configs still include hotplex metacognition", func(t *testing.T) {
		prompt := BuildSystemPrompt(&AgentConfigs{})
		require.Contains(t, prompt, `<agent-configuration schema-version="2">`)
		require.Contains(t, prompt, "<hotplex>")
	})

	t.Run("assembles B+C with nested XML tags", func(t *testing.T) {
		cfg := &AgentConfigs{Soul: "Persona", Agents: "Rules", Tools: "Tools", User: "User data", Memory: "Memory data"}
		prompt := BuildSystemPrompt(cfg)
		require.Contains(t, prompt, `<agent-configuration schema-version="2">`)
		require.Contains(t, prompt, "</agent-configuration>")
		require.Contains(t, prompt, "<directives>")
		require.Contains(t, prompt, "</directives>")
		require.Contains(t, prompt, "<persona>")
		require.Contains(t, prompt, "Persona")
		require.Contains(t, prompt, "<rules>")
		require.Contains(t, prompt, "Rules")
		require.Contains(t, prompt, "<tool-guidance>")
		require.Contains(t, prompt, "Tools")
		require.NotContains(t, prompt, "<skills>")
		require.Contains(t, prompt, "<context>")
		require.Contains(t, prompt, "</context>")
		require.Contains(t, prompt, "<user>")
		require.Contains(t, prompt, "User data")
		require.Contains(t, prompt, "<memory>")
		require.Contains(t, prompt, "Memory data")
	})

	t.Run("B-channel only still includes hotplex metacognition", func(t *testing.T) {
		cfg := &AgentConfigs{Agents: "Rules only"}
		prompt := BuildSystemPrompt(cfg)
		require.Contains(t, prompt, "<directives>")
		require.Contains(t, prompt, "<rules>")
		require.Contains(t, prompt, "<hotplex>")
		require.NotContains(t, prompt, "<persona>")
		require.NotContains(t, prompt, "<context>")
		require.NotContains(t, prompt, "<user>")
		require.NotContains(t, prompt, "<memory>")
	})

	t.Run("C-channel only still injects hotplex into B-channel", func(t *testing.T) {
		cfg := &AgentConfigs{User: "User only", Memory: "Memory only"}
		prompt := BuildSystemPrompt(cfg)
		require.Contains(t, prompt, "<directives>")
		require.Contains(t, prompt, "<hotplex>")
		require.Contains(t, prompt, "<context>")
		require.Contains(t, prompt, "<user>")
		require.Contains(t, prompt, "User only")
		require.Contains(t, prompt, "<memory>")
		require.Contains(t, prompt, "Memory only")
		require.NotContains(t, prompt, "<persona>")
		require.NotContains(t, prompt, "<rules>")
		require.NotContains(t, prompt, "<tool-guidance>")
	})

	t.Run("directives before context", func(t *testing.T) {
		cfg := &AgentConfigs{Soul: "S", User: "U"}
		prompt := BuildSystemPrompt(cfg)
		bIdx := strings.Index(prompt, "<directives>")
		cIdx := strings.Index(prompt, "<context>")
		require.Less(t, bIdx, cIdx)
	})

	t.Run("behavioral directives present per section", func(t *testing.T) {
		cfg := &AgentConfigs{Soul: "S", Agents: "A", Tools: "K", User: "U", Memory: "M"}
		prompt := BuildSystemPrompt(cfg)
		require.Contains(t, prompt, "自然地代入并体现此人格定位")
		require.Contains(t, prompt, "视为强制性的工作空间行为约束")
		require.Contains(t, prompt, "环境工具使用指南")
		require.Contains(t, prompt, "提供个性化的服务体验")
		require.Contains(t, prompt, "确保任务执行的连贯性与深度")
	})
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	fullPath := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0o755))
	err := os.WriteFile(fullPath, []byte(content), 0o644)
	require.NoError(t, err)
}

func TestShouldExclude(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		baseName string
		exclude  []string
		want     bool
	}{
		{"nil exclude", "SOUL.md", nil, false},
		{"empty exclude", "SOUL.md", []string{}, false},
		{"exact match", "SOUL.md", []string{"SOUL.md"}, true},
		{"case insensitive", "soul.md", []string{"SOUL.MD"}, true},
		{"case insensitive reverse", "SOUL.MD", []string{"soul.md"}, true},
		{"mixed case", "Soul.Md", []string{"sOul.mD"}, true},
		{"no match", "SOUL.md", []string{"AGENTS.md"}, false},
		{"multiple exclude one match", "USER.md", []string{"SOUL.md", "USER.md"}, true},
		{"multiple exclude no match", "TOOLS.md", []string{"SOUL.md", "USER.md"}, false},
		{"canonical tools excluded by legacy name", "TOOLS.md", []string{"SKILLS.md"}, true},
		{"legacy tools excluded by canonical name", "SKILLS.md", []string{"TOOLS.md"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := shouldExclude(tt.baseName, tt.exclude)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestLoadWithInjectExclude(t *testing.T) {
	t.Parallel()

	t.Run("excludes specified file", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, dir, "SOUL.md", "I am soul.")
		writeFile(t, dir, "AGENTS.md", "Rules here.")
		writeFile(t, dir, "USER.md", "User data.")

		cfg, err := Load(dir, "", "", "SOUL.md")
		require.NoError(t, err)
		require.Empty(t, cfg.Soul, "SOUL.md should be excluded")
		require.Equal(t, "Rules here.", cfg.Agents)
		require.Equal(t, "User data.", cfg.User)
	})

	t.Run("excludes multiple files", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, dir, "SOUL.md", "Soul content.")
		writeFile(t, dir, "AGENTS.md", "Agents content.")
		writeFile(t, dir, "USER.md", "User content.")
		writeFile(t, dir, "MEMORY.md", "Memory content.")

		cfg, err := Load(dir, "", "", "SOUL.md", "MEMORY.md")
		require.NoError(t, err)
		require.Empty(t, cfg.Soul, "SOUL.md should be excluded")
		require.Empty(t, cfg.Memory, "MEMORY.md should be excluded")
		require.Equal(t, "Agents content.", cfg.Agents)
		require.Equal(t, "User content.", cfg.User)
	})

	t.Run("exclude is case insensitive", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, dir, "SOUL.md", "Soul content.")

		cfg, err := Load(dir, "", "", "soul.md")
		require.NoError(t, err)
		require.Empty(t, cfg.Soul, "soul.md (lowercase) should exclude SOUL.md")
	})

	t.Run("no exclude loads all files", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, dir, "SOUL.md", "Soul content.")
		writeFile(t, dir, "AGENTS.md", "Agents content.")

		cfg, err := Load(dir, "", "")
		require.NoError(t, err)
		require.Equal(t, "Soul content.", cfg.Soul)
		require.Equal(t, "Agents content.", cfg.Agents)
	})

	t.Run("exclude with platform fallback still works", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, dir, "SOUL.md", "Global soul.")
		writeFile(t, dir, "slack/SOUL.md", "Slack soul.")
		writeFile(t, dir, "AGENTS.md", "Rules.")

		cfg, err := Load(dir, "slack", "", "SOUL.md")
		require.NoError(t, err)
		require.Empty(t, cfg.Soul, "excluded file should not load from any level")
		require.Equal(t, "Rules.", cfg.Agents)
	})

	t.Run("either tools basename excludes the logical slot", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, dir, "TOOLS.md", "Canonical tools.")

		cfg, err := Load(dir, "", "", "SKILLS.md")
		require.NoError(t, err)
		require.Empty(t, cfg.Tools)
	})
}

func TestLoadForWorkspace(t *testing.T) {
	t.Parallel()

	t.Run("nil overrides inherits team defaults", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, dir, "SOUL.md", "team-soul")
		writeFile(t, dir, "AGENTS.md", "team-rules")

		cfg, err := LoadForWorkspace(dir, "webchat", nil)
		require.NoError(t, err)
		require.Equal(t, "team-soul", cfg.Soul)
		require.Equal(t, "team-rules", cfg.Agents)
	})

	t.Run("override replaces team default per-file", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, dir, "SOUL.md", "team-soul")
		writeFile(t, dir, "AGENTS.md", "team-rules")

		overrides := map[string]string{"SOUL.md": "ws-soul"}
		cfg, err := LoadForWorkspace(dir, "webchat", overrides)
		require.NoError(t, err)
		require.Equal(t, "ws-soul", cfg.Soul)      // overridden
		require.Equal(t, "team-rules", cfg.Agents) // inherited
	})

	t.Run("empty override value explicitly clears team default", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, dir, "SOUL.md", "team-soul")

		overrides := map[string]string{"SOUL.md": ""}
		cfg, err := LoadForWorkspace(dir, "webchat", overrides)
		require.NoError(t, err)
		require.Empty(t, cfg.Soul)
	})

	t.Run("legacy tools override is accepted", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, dir, "TOOLS.md", "team-tools")

		cfg, err := LoadForWorkspace(dir, "webchat", map[string]string{"SKILLS.md": "legacy-override"})
		require.NoError(t, err)
		require.Equal(t, "legacy-override", cfg.Tools)
	})

	t.Run("canonical tools override wins independently", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, dir, "SKILLS.md", "team-legacy")

		cfg, err := LoadForWorkspace(dir, "webchat", map[string]string{"TOOLS.md": "canonical-override"})
		require.NoError(t, err)
		require.Equal(t, "canonical-override", cfg.Tools)
	})

	t.Run("override without team default applies", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir() // no team files

		overrides := map[string]string{"USER.md": "ws-user"}
		cfg, err := LoadForWorkspace(dir, "webchat", overrides)
		require.NoError(t, err)
		require.Equal(t, "ws-user", cfg.User)
	})

	t.Run("injectExclude wins over override", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, dir, "SOUL.md", "team-soul")

		overrides := map[string]string{"SOUL.md": "ws-soul"}
		cfg, err := LoadForWorkspace(dir, "webchat", overrides, "SOUL.md")
		require.NoError(t, err)
		require.Empty(t, cfg.Soul) // excluded even though overridden
	})

	t.Run("unknown override keys ignored (defense-in-depth)", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		overrides := map[string]string{"META-COGNITION.md": "evil", "foo.md": "x", "SOUL.md": "ws-soul"}
		cfg, err := LoadForWorkspace(dir, "webchat", overrides)
		require.NoError(t, err)
		require.Equal(t, "ws-soul", cfg.Soul)
		// unknown keys silently ignored; META-COGNITION never appears in AgentConfigs
	})

	t.Run("platform-level team default resolves before override", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, dir, "webchat/AGENTS.md", "webchat-team-rules") // platform-level

		overrides := map[string]string{"SOUL.md": "ws-soul"}
		cfg, err := LoadForWorkspace(dir, "webchat", overrides)
		require.NoError(t, err)
		require.Equal(t, "ws-soul", cfg.Soul)              // override
		require.Equal(t, "webchat-team-rules", cfg.Agents) // platform-level team default
	})
}

func TestEnforceTotalLimit(t *testing.T) {
	t.Parallel()

	// enforceTotalLimit is defense-in-depth: under the 5-file whitelist with
	// MaxFileChars=8000, overrides can never push the merged total past
	// MaxTotalChars=40000 via LoadForWorkspace (5*8000=40000). This unit test
	// constructs an over-budget config directly to verify the truncation guard.
	over := strings.Repeat("a", MaxTotalChars)
	cfg := &AgentConfigs{Soul: over, Agents: over} // 2 * MaxTotalChars

	enforceTotalLimit(cfg)

	total := len(cfg.Soul) + len(cfg.Agents) + len(cfg.Tools) + len(cfg.User) + len(cfg.Memory)
	require.LessOrEqual(t, total, MaxTotalChars)
	require.Equal(t, MaxTotalChars, len(cfg.Soul)) // first field consumes full budget
	require.Empty(t, cfg.Agents)                   // subsequent fields truncated to 0
}
