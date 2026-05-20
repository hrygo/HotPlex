package admin

import (
	"context"
)

// AgentConfigFileName identifies a recognized agent configuration file.
type AgentConfigFileName string

const (
	AgentConfigSoul   AgentConfigFileName = "SOUL.md"
	AgentConfigAgents AgentConfigFileName = "AGENTS.md"
	AgentConfigSkills AgentConfigFileName = "SKILLS.md"
	AgentConfigUser   AgentConfigFileName = "USER.md"
	AgentConfigMemory AgentConfigFileName = "MEMORY.md"
)

// ValidConfigFiles is the whitelist of agent config file names accepted by
// read/write endpoints. Entries outside this set are rejected.
var ValidConfigFiles = map[AgentConfigFileName]bool{
	AgentConfigSoul:   true,
	AgentConfigAgents: true,
	AgentConfigSkills: true,
	AgentConfigUser:   true,
	AgentConfigMemory: true,
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
	Soul   *AgentConfigMeta `json:"soul,omitempty"`
	Agents *AgentConfigMeta `json:"agents,omitempty"`
	Skills *AgentConfigMeta `json:"skills,omitempty"`
	User   *AgentConfigMeta `json:"user,omitempty"`
	Memory *AgentConfigMeta `json:"memory,omitempty"`
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

	// GetAgentConfigFile reads a single agent config file for a bot,
	// identified by the whitelisted file name.
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
	// for the named bot. The file name must appear in ValidConfigFiles.
	WriteAgentConfigFile(ctx context.Context, botName string, file AgentConfigFileName, content string) error
}
