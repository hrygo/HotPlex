package agentconfig

import (
	"strings"
)

// BuildOCSSystemPrompt assembles the B+C combined system prompt for OpenCode Server.
// Injected via the system field of POST /session/:id/message into S2 (Call-level System).
//
// OCS has no hedging — all content reaches the model with equal weight.
// OCS system field has no cross-message persistence — must attach to every message.
func BuildOCSSystemPrompt(configs *AgentConfigs) string {
	if configs == nil || configs.IsEmpty() {
		return ""
	}

	parts := buildBPromptParts(configs)

	if configs.User != "" {
		parts = append(parts, "# User Profile\n"+configs.User)
	}
	if configs.Memory != "" {
		parts = append(parts, "# Persistent Memory\n"+configs.Memory)
	}

	return strings.Join(parts, "\n\n")
}
