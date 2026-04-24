package agentconfig

import (
	"fmt"
	"strings"
)

// BuildOCSSystemPrompt assembles the B+C combined system prompt for OpenCode Server.
// B-channel (SOUL, AGENTS, SKILLS) and C-channel (USER, MEMORY) content are merged
// into a single string, injected via the system field of POST /session/:id/message.
//
// OCS has no hedging — all content reaches S2 (Call-level System) with equal weight,
// and is joined with S0 (Provider Prompt) into system[0].
//
// IMPORTANT: OCS system field has no cross-message persistence. HotPlex must attach
// the system field to every message, otherwise the injected context is lost.
func BuildOCSSystemPrompt(configs *AgentConfigs) string {
	if configs == nil || configs.IsEmpty() {
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

	if configs.User != "" {
		parts = append(parts, "# User Profile\n"+configs.User)
	}

	if configs.Memory != "" {
		parts = append(parts, "# Persistent Memory\n"+configs.Memory)
	}

	return strings.Join(parts, "\n\n")
}
