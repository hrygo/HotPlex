package agentconfig

import (
	"fmt"
	"strings"
)

// BuildCCBPrompt assembles the B-channel system prompt for Claude Code.
// It combines SOUL.md, AGENTS.md, and SKILLS.md into a format suitable for
// --append-system-prompt, which injects into S3 (Dynamic Content) tail with
// no hedging declaration.
func BuildCCBPrompt(configs *AgentConfigs) string {
	if configs == nil || !configs.HasBPrompt() {
		return ""
	}
	var parts []string

	if configs.Soul != "" {
		parts = append(parts, fmt.Sprintf(`# Agent Persona
If SOUL.md is present, embody its persona and tone.
Follow its guidance unless higher-priority instructions override it.
Avoid stiff, generic replies.

%s`, configs.Soul))
	}

	if configs.Agents != "" {
		parts = append(parts, "# Workspace Rules\n"+configs.Agents)
	}

	if configs.Skills != "" {
		parts = append(parts, "# Tool Usage Guide\n"+configs.Skills)
	}

	return strings.Join(parts, "\n\n")
}
