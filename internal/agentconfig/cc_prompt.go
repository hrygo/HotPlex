package agentconfig

import (
	"fmt"
	"strings"
)

// BuildCCBPrompt assembles the B-channel system prompt for Claude Code.
// Injected via --append-system-prompt into S3 (Dynamic Content) tail with
// no hedging declaration.
func BuildCCBPrompt(configs *AgentConfigs) string {
	if configs == nil || !configs.HasBPrompt() {
		return ""
	}
	return strings.Join(buildBPromptParts(configs), "\n\n")
}

// buildBPromptParts returns the B-channel sections shared by both CC and OCS.
func buildBPromptParts(configs *AgentConfigs) []string {
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
	return parts
}
