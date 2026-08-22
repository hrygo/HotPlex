package admin

import (
	"context"
	"errors"
)

// ErrBotRunning indicates a delete or update was attempted on a bot that is
// currently running. Callers should check with errors.Is and return 409.
var ErrBotRunning = errors.New("bot is currently running")

// AgentConfigFileName identifies a recognized agent configuration file.
type AgentConfigFileName string

const (
	AgentConfigSoul         AgentConfigFileName = "SOUL.md"
	AgentConfigAgents       AgentConfigFileName = "AGENTS.md"
	AgentConfigTools        AgentConfigFileName = "TOOLS.md"
	AgentConfigLegacySkills AgentConfigFileName = "SKILLS.md"
	AgentConfigUser         AgentConfigFileName = "USER.md"
	AgentConfigMemory       AgentConfigFileName = "MEMORY.md"
)

// ValidConfigFiles is the canonical whitelist accepted by AgentConfig write
// endpoints. AgentConfigLegacySkills is intentionally read-only during the
// compatibility window; it is not a real Agent Skill definition.
var ValidConfigFiles = map[AgentConfigFileName]bool{
	AgentConfigSoul:   true,
	AgentConfigAgents: true,
	AgentConfigTools:  true,
	AgentConfigUser:   true,
	AgentConfigMemory: true,
}

// IsReadableConfigFile reports whether an AgentConfig file endpoint may read
// the name. SKILLS.md remains a deprecated alias for the logical Tools slot.
func IsReadableConfigFile(file AgentConfigFileName) bool {
	return ValidConfigFiles[file] || file == AgentConfigLegacySkills
}

// ---------------------------------------------------------------------------
// DTO structs
// ---------------------------------------------------------------------------

// BotConfigEntry is the serialized representation of a single bot's
// configuration, returned by list and detail endpoints.
type BotConfigEntry struct {
	Name         string              `json:"name"`
	Platform     string              `json:"platform"`
	BotID        string              `json:"bot_id"`
	Status       string              `json:"status"`
	ConnectedAt  string              `json:"connected_at,omitempty"`
	Config       *BotConfigAttrs     `json:"config,omitempty"`
	AgentConfigs *AgentConfigSummary `json:"agent_configs,omitempty"`
}

// BotConfigAttrs holds the mutable attributes of a bot configuration.
type BotConfigAttrs struct {
	Platform       string    `json:"platform,omitempty"`
	WorkerType     string    `json:"worker_type,omitempty"`
	WorkDir        string    `json:"work_dir,omitempty"`
	DMPolicy       string    `json:"dm_policy,omitempty"`
	GroupPolicy    string    `json:"group_policy,omitempty"`
	RequireMention bool      `json:"require_mention,omitempty"`
	AllowFrom      []string  `json:"allow_from,omitempty"`
	AllowDMFrom    []string  `json:"allow_dm_from,omitempty"`
	AllowGroupFrom []string  `json:"allow_group_from,omitempty"`
	STT            *STTAttrs `json:"stt,omitempty"`
	TTS            *TTSAttrs `json:"tts,omitempty"`

	// Credentials — only used during creation; never returned in GET responses.
	BotToken  string `json:"bot_token,omitempty"`
	AppToken  string `json:"app_token,omitempty"`
	AppID     string `json:"app_id,omitempty"`
	AppSecret string `json:"app_secret,omitempty"`
}

// STTAttrs holds speech-to-text configuration.
type STTAttrs struct {
	Provider string `json:"provider,omitempty"`
}

// TTSAttrs holds text-to-speech configuration.
type TTSAttrs struct {
	Provider string `json:"provider,omitempty"`
	Voice    string `json:"voice,omitempty"`
}

// AgentConfigSummary provides per-file metadata for each of the five agent
// config files. nil entries indicate the file was not found.
type AgentConfigSummary struct {
	Soul         *AgentConfigMeta `json:"soul,omitempty"`
	Agents       *AgentConfigMeta `json:"agents,omitempty"`
	Tools        *AgentConfigMeta `json:"tools,omitempty"`
	LegacySkills *AgentConfigMeta `json:"skills,omitempty"` // Deprecated: legacy AgentConfig basename metadata.
	User         *AgentConfigMeta `json:"user,omitempty"`
	Memory       *AgentConfigMeta `json:"memory,omitempty"`
}

// AgentConfigMeta describes a single agent config file's provenance and size.
type AgentConfigMeta struct {
	Source string `json:"source"`
	Size   int    `json:"size"`
}

// AgentConfigFile is the full content response for a single agent config file.
type AgentConfigFile struct {
	Content string `json:"content"`
	Source  string `json:"source"`
	Size    int    `json:"size"`
	File    string `json:"file"`
}

// ---------------------------------------------------------------------------
// Provider interface
// ---------------------------------------------------------------------------

// BotConfigProvider abstracts bot configuration CRUD and agent config file
// access for the admin API. Implementations bridge the admin layer to the
// messaging config and agentconfig packages without creating import cycles.
type BotConfigProvider interface {
	// GetBotConfig returns the full configuration for the named bot.
	GetBotConfig(ctx context.Context, name string) (*BotConfigEntry, error)

	// ListBotConfigs returns all registered bot configurations.
	ListBotConfigs(ctx context.Context) ([]BotConfigEntry, error)

	// GetAgentConfigFile reads a single agent config file for a bot. Canonical
	// names and the deprecated SKILLS.md read alias are accepted.
	GetAgentConfigFile(ctx context.Context, botName string, file AgentConfigFileName) (*AgentConfigFile, error)

	// GetSystemPromptPreview returns the assembled B+C channel system prompt
	// for the named bot, suitable for previewing before edits take effect.
	GetSystemPromptPreview(ctx context.Context, botName string) (string, error)

	// UpdateBotConfig applies partial updates to an existing bot configuration.
	UpdateBotConfig(ctx context.Context, name string, attrs *BotConfigAttrs) error

	// CreateBot registers a new bot with the given attributes.
	CreateBot(ctx context.Context, name string, attrs *BotConfigAttrs) error

	// DeleteBot removes a bot registration by name.
	DeleteBot(ctx context.Context, name string) error

	// WriteAgentConfigFile writes content to a single agent config file
	// for the named bot. The file name must appear in ValidConfigFiles;
	// deprecated aliases are never writable.
	WriteAgentConfigFile(ctx context.Context, botName string, file AgentConfigFileName, content string) error

	// GetPlatformAgentConfigFile reads a single platform-level (channel team
	// default) agent config file, identified by platform rather than a bot
	// name. This addresses the dir/{platform}/ layer directly and does not
	// require a registered bot — webchat, for example, owns dir/webchat/ team
	// defaults but has no bot instance in the messaging registry. The platform
	// must be a recognized identifier (see agentconfig.IsValidPlatform).
	GetPlatformAgentConfigFile(ctx context.Context, platform string, file AgentConfigFileName) (*AgentConfigFile, error)

	// WritePlatformAgentConfigFile writes content to a single platform-level
	// agent config file. The platform must be recognized and the file name
	// must appear in ValidConfigFiles; deprecated aliases are never writable.
	// Writes serve as channel team defaults, overridden per-workspace by
	// LoadForWorkspace's existing precedence.
	WritePlatformAgentConfigFile(ctx context.Context, platform string, file AgentConfigFileName, content string) error
}
