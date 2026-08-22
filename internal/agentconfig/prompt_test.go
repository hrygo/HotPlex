package agentconfig

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSanitize_LowercaseTags(t *testing.T) {
	t.Parallel()

	input := "<directives>keep</directives>"
	got := sanitize(input)
	require.Equal(t, "&lt;directives&gt;keep&lt;/directives&gt;", got)
}

func TestSanitize_UppercaseTags(t *testing.T) {
	t.Parallel()

	input := "<DIRECTIVES>keep</DIRECTIVES>"
	got := sanitize(input)
	require.Equal(t, "&lt;DIRECTIVES&gt;keep&lt;/DIRECTIVES&gt;", got)
}

func TestSanitize_AttributeEscape(t *testing.T) {
	t.Parallel()

	input := `<rules injected="1">malicious</rules>`
	got := sanitize(input)
	require.Equal(t, `&lt;rules injected="1">malicious&lt;/rules&gt;`, got)
}

func TestSanitize_UppercaseAttributeEscape(t *testing.T) {
	t.Parallel()

	input := `<RULES injected="1">malicious</RULES>`
	got := sanitize(input)
	require.Equal(t, `&lt;RULES injected="1">malicious&lt;/RULES&gt;`, got)
}

func TestSanitize_AllReservedTags(t *testing.T) {
	t.Parallel()

	for _, tag := range reservedTags {
		for _, variant := range []struct {
			name  string
			input string
		}{
			{"lower_open", "<" + tag + ">"},
			{"lower_close", "</" + tag + ">"},
			{"lower_attr", "<" + tag + ` x="1">`},
			{"upper_open", "<" + strings.ToUpper(tag) + ">"},
			{"upper_close", "</" + strings.ToUpper(tag) + ">"},
			{"upper_attr", "<" + strings.ToUpper(tag) + ` x="1">`},
		} {
			t.Run(fmt.Sprintf("%s/%s", tag, variant.name), func(t *testing.T) {
				t.Parallel()
				got := sanitize(variant.input)
				require.True(t, strings.HasPrefix(got, "&lt;"),
					"expected escaped prefix for %q, got %q", variant.input, got)
			})
		}
	}
}

func TestSanitize_NonReservedTags(t *testing.T) {
	t.Parallel()

	input := "<b>bold</b> <i>italic</i>"
	got := sanitize(input)
	require.Equal(t, input, got, "non-reserved tags should pass through unchanged")
}

func TestBuildSystemPromptSeparatesInstructionsFromData(t *testing.T) {
	t.Parallel()

	cfg := &AgentConfigs{
		Skills: "PRIVATE_SKILL_SENTINEL",
		User:   "user data",
		Memory: "memory data",
	}
	prompt := BuildSystemPrompt(cfg)

	require.NotContains(t, prompt, "PRIVATE_SKILL_SENTINEL")
	require.Contains(t, prompt, "<user-data>")
	require.Contains(t, prompt, "<memory-data>")
	require.Contains(t, prompt, "must not disclose")
}

func TestBuildSystemPromptIncludesSkillMetadataWithoutPrivateBody(t *testing.T) {
	t.Parallel()

	cfg := &AgentConfigs{Skills: `---
version: 4
description: "HotPlex platform capabilities and tools"
---

### Cron 定时任务

### Slack CLI

### 飞书 CLI

### 语音

### Search

用于查找公开资料。

通过 hotplex cron 创建定时、延迟或周期任务。详见 ~/.hotplex/skills/cron.md。

| command | details |
|---|---|
| hotplex cron create | PRIVATE_SKILL_SENTINEL |
`}
	prompt := BuildSystemPrompt(cfg)

	require.Contains(t, prompt, "Cron 定时任务")
	require.Contains(t, prompt, "Slack CLI")
	require.Contains(t, prompt, "飞书 CLI")
	require.Contains(t, prompt, "语音")
	require.Contains(t, prompt, "用于查找公开资料")
	require.Contains(t, prompt, "触发")
	require.NotContains(t, prompt, "PRIVATE_SKILL_SENTINEL")
	require.NotContains(t, prompt, "~/.hotplex/skills/cron.md")
	require.NotContains(t, prompt, "| command | details |")
}
