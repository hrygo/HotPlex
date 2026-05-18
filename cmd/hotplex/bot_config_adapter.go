package main

import (
	"context"
	"fmt"

	"github.com/hrygo/hotplex/internal/admin"
	"github.com/hrygo/hotplex/internal/agentconfig"
	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/messaging"
)

// botConfigAdapter bridges the admin BotConfigProvider interface to the
// messaging config and agentconfig packages, avoiding import cycles.
type botConfigAdapter struct {
	cfgStore       *config.ConfigStore
	agentConfigDir string
	configFilePath string
}

// newBotConfigAdapter creates a BotConfigProvider backed by the given config
// store, agent config directory, and config file path.
func newBotConfigAdapter(cfgStore *config.ConfigStore, agentConfigDir, configFilePath string) *botConfigAdapter {
	return &botConfigAdapter{
		cfgStore:       cfgStore,
		agentConfigDir: agentConfigDir,
		configFilePath: configFilePath,
	}
}

// GetBotConfig returns the full configuration for the named bot.
func (a *botConfigAdapter) GetBotConfig(ctx context.Context, name string) (*admin.BotConfigEntry, error) {
	registry := messaging.DefaultBotRegistry()
	entry, ok := registry.GetByName(name)
	if !ok {
		return nil, fmt.Errorf("bot %q not found", name)
	}

	cfg := a.cfgStore.Load()
	platform := string(entry.Platform)
	botID := entry.BotID

	attrs := extractBotAttrs(cfg, platform, name)
	summary := getAgentConfigSummary(platform, botID, a.agentConfigDir)

	return &admin.BotConfigEntry{
		Name:         entry.Name,
		Platform:     platform,
		BotID:        botID,
		Status:       string(entry.Status),
		ConnectedAt:  entry.ConnectedAt.Format("2006-01-02T15:04:05Z"),
		Config:       attrs,
		AgentConfigs: summary,
	}, nil
}

// ListBotConfigs returns all registered bot configurations.
func (a *botConfigAdapter) ListBotConfigs(ctx context.Context) ([]admin.BotConfigEntry, error) {
	registry := messaging.DefaultBotRegistry()
	entries := registry.ListAll()
	cfg := a.cfgStore.Load()

	result := make([]admin.BotConfigEntry, 0, len(entries))
	for _, e := range entries {
		platform := string(e.Platform)
		attrs := extractBotAttrs(cfg, platform, e.Name)
		summary := getAgentConfigSummary(platform, e.BotID, a.agentConfigDir)

		result = append(result, admin.BotConfigEntry{
			Name:         e.Name,
			Platform:     platform,
			BotID:        e.BotID,
			Status:       string(e.Status),
			ConnectedAt:  e.ConnectedAt.Format("2006-01-02T15:04:05Z"),
			Config:       attrs,
			AgentConfigs: summary,
		})
	}
	return result, nil
}

// GetAgentConfigFile reads a single agent config file for a bot.
func (a *botConfigAdapter) GetAgentConfigFile(ctx context.Context, botName string, file admin.AgentConfigFileName) (*admin.AgentConfigFile, error) {
	platform, botID, ok := resolvePlatformAndBotID(botName)
	if !ok {
		return nil, fmt.Errorf("bot %q not found in registry", botName)
	}

	configs, err := agentconfig.Load(a.agentConfigDir, platform, botID)
	if err != nil {
		return nil, fmt.Errorf("load agent config: %w", err)
	}

	content := getConfigField(configs, file)
	source := agentconfig.ResolvedSource(a.agentConfigDir, platform, botID, string(file))

	return &admin.AgentConfigFile{
		Content: content,
		Source:  source,
		Size:    len(content),
		File:    string(file),
	}, nil
}

// GetSystemPromptPreview returns the assembled B+C channel system prompt for the named bot.
func (a *botConfigAdapter) GetSystemPromptPreview(ctx context.Context, botName string) (string, error) {
	platform, botID, ok := resolvePlatformAndBotID(botName)
	if !ok {
		return "", fmt.Errorf("bot %q not found in registry", botName)
	}

	configs, err := agentconfig.Load(a.agentConfigDir, platform, botID)
	if err != nil {
		return "", fmt.Errorf("load agent config: %w", err)
	}

	return agentconfig.BuildSystemPrompt(configs), nil
}

// UpdateBotConfig applies partial updates to an existing bot configuration.
func (a *botConfigAdapter) UpdateBotConfig(ctx context.Context, name string, attrs *admin.BotConfigAttrs) error {
	return fmt.Errorf("not implemented")
}

// CreateBot registers a new bot with the given attributes.
func (a *botConfigAdapter) CreateBot(ctx context.Context, name string, attrs *admin.BotConfigAttrs) error {
	return fmt.Errorf("not implemented")
}

// DeleteBot removes a bot registration by name.
func (a *botConfigAdapter) DeleteBot(ctx context.Context, name string) error {
	return fmt.Errorf("not implemented")
}

// WriteAgentConfigFile writes content to a single agent config file for the named bot.
func (a *botConfigAdapter) WriteAgentConfigFile(ctx context.Context, botName string, file admin.AgentConfigFileName, content string) error {
	platform, botID, ok := resolvePlatformAndBotID(botName)
	if !ok {
		return fmt.Errorf("bot %q not found in registry", botName)
	}

	if err := agentconfig.WriteFile(a.agentConfigDir, platform, botID, string(file), content, agentconfig.MaxFileChars); err != nil {
		return fmt.Errorf("write agent config file: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Helper functions
// ---------------------------------------------------------------------------

// resolvePlatformAndBotID looks up the bot name in the global registry and
// returns its platform and bot ID.
func resolvePlatformAndBotID(name string) (platform, botID string, ok bool) {
	registry := messaging.DefaultBotRegistry()
	entry, found := registry.GetByName(name)
	if !found {
		return "", "", false
	}
	return string(entry.Platform), entry.BotID, true
}

// extractBotAttrs builds BotConfigAttrs from the config for a specific bot.
func extractBotAttrs(cfg *config.Config, platform, name string) *admin.BotConfigAttrs {
	attrs := &admin.BotConfigAttrs{}

	switch platform {
	case "slack":
		bot := resolveSlackBot(cfg, name)
		if bot != nil {
			attrs.WorkerType = bot.WorkerType
			attrs.WorkDir = bot.WorkDir
			attrs.DMPolicy = bot.DMPolicy
			attrs.GroupPolicy = bot.GroupPolicy
			if bot.RequireMention != nil {
				attrs.RequireMention = *bot.RequireMention
			}
			attrs.AllowFrom = bot.AllowFrom
			attrs.AllowDMFrom = bot.AllowDMFrom
			attrs.AllowGroupFrom = bot.AllowGroupFrom
			if bot.Provider != "" {
				attrs.STT = &admin.STTAttrs{Provider: bot.Provider}
			}
			if bot.TTSProvider != "" {
				attrs.TTS = &admin.TTSAttrs{
					Provider: bot.TTSProvider,
					Voice:    bot.Voice,
				}
			}
		} else {
			// Fall back to platform-level config.
			sc := &cfg.Messaging.Slack.MessagingPlatformConfig
			attrs.WorkerType = sc.WorkerType
			attrs.WorkDir = sc.WorkDir
			attrs.DMPolicy = sc.DMPolicy
			attrs.GroupPolicy = sc.GroupPolicy
			attrs.RequireMention = sc.RequireMention
			attrs.AllowFrom = sc.AllowFrom
			attrs.AllowDMFrom = sc.AllowDMFrom
			attrs.AllowGroupFrom = sc.AllowGroupFrom
			if sc.Provider != "" {
				attrs.STT = &admin.STTAttrs{Provider: sc.Provider}
			}
			if sc.TTSProvider != "" {
				attrs.TTS = &admin.TTSAttrs{
					Provider: sc.TTSProvider,
					Voice:    sc.Voice,
				}
			}
		}

	case "feishu":
		bot := resolveFeishuBot(cfg, name)
		if bot != nil {
			attrs.WorkerType = bot.WorkerType
			attrs.WorkDir = bot.WorkDir
			attrs.DMPolicy = bot.DMPolicy
			attrs.GroupPolicy = bot.GroupPolicy
			if bot.RequireMention != nil {
				attrs.RequireMention = *bot.RequireMention
			}
			attrs.AllowFrom = bot.AllowFrom
			attrs.AllowDMFrom = bot.AllowDMFrom
			attrs.AllowGroupFrom = bot.AllowGroupFrom
			if bot.Provider != "" {
				attrs.STT = &admin.STTAttrs{Provider: bot.Provider}
			}
			if bot.TTSProvider != "" {
				attrs.TTS = &admin.TTSAttrs{
					Provider: bot.TTSProvider,
					Voice:    bot.Voice,
				}
			}
		} else {
			// Fall back to platform-level config.
			fc := &cfg.Messaging.Feishu.MessagingPlatformConfig
			attrs.WorkerType = fc.WorkerType
			attrs.WorkDir = fc.WorkDir
			attrs.DMPolicy = fc.DMPolicy
			attrs.GroupPolicy = fc.GroupPolicy
			attrs.RequireMention = fc.RequireMention
			attrs.AllowFrom = fc.AllowFrom
			attrs.AllowDMFrom = fc.AllowDMFrom
			attrs.AllowGroupFrom = fc.AllowGroupFrom
			if fc.Provider != "" {
				attrs.STT = &admin.STTAttrs{Provider: fc.Provider}
			}
			if fc.TTSProvider != "" {
				attrs.TTS = &admin.TTSAttrs{
					Provider: fc.TTSProvider,
					Voice:    fc.Voice,
				}
			}
		}
	}

	return attrs
}

// getAgentConfigSummary returns a summary of all agent config files for a bot.
func getAgentConfigSummary(platform, botID, agentConfigDir string) *admin.AgentConfigSummary {
	configs, err := agentconfig.Load(agentConfigDir, platform, botID)
	if err != nil || configs == nil {
		return nil
	}

	summary := &admin.AgentConfigSummary{}

	for _, file := range []struct {
		field admin.AgentConfigFileName
		value string
	}{
		{admin.AgentConfigSoul, configs.Soul},
		{admin.AgentConfigAgents, configs.Agents},
		{admin.AgentConfigSkills, configs.Skills},
		{admin.AgentConfigUser, configs.User},
		{admin.AgentConfigMemory, configs.Memory},
	} {
		if file.value == "" {
			continue
		}
		source := agentconfig.ResolvedSource(agentConfigDir, platform, botID, string(file.field))
		meta := &admin.AgentConfigMeta{
			Source: source,
			Size:   len(file.value),
		}
		switch file.field {
		case admin.AgentConfigSoul:
			summary.Soul = meta
		case admin.AgentConfigAgents:
			summary.Agents = meta
		case admin.AgentConfigSkills:
			summary.Skills = meta
		case admin.AgentConfigUser:
			summary.User = meta
		case admin.AgentConfigMemory:
			summary.Memory = meta
		}
	}

	return summary
}

// getConfigField extracts the content of a specific agent config file from
// the loaded AgentConfigs.
func getConfigField(configs *agentconfig.AgentConfigs, file admin.AgentConfigFileName) string {
	switch file {
	case admin.AgentConfigSoul:
		return configs.Soul
	case admin.AgentConfigAgents:
		return configs.Agents
	case admin.AgentConfigSkills:
		return configs.Skills
	case admin.AgentConfigUser:
		return configs.User
	case admin.AgentConfigMemory:
		return configs.Memory
	default:
		return ""
	}
}
