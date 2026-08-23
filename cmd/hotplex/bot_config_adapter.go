package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

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

	attrs := extractBotAttrs(cfg, platform, name)
	excl := resolveInjectExcludeForAdmin(cfg, platform, name)
	agentName := name
	if entry.IsSingleBot {
		agentName = ""
	}
	summary := getAgentConfigSummary(platform, agentName, a.agentConfigDir, excl...)

	return &admin.BotConfigEntry{
		Name:         entry.Name,
		Platform:     platform,
		BotID:        entry.BotID,
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
		excl := resolveInjectExcludeForAdmin(cfg, platform, e.Name)
		agentName := e.Name
		if e.IsSingleBot {
			agentName = ""
		}
		summary := getAgentConfigSummary(platform, agentName, a.agentConfigDir, excl...)

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
	platform, ok := resolvePlatformForBot(botName)
	if !ok {
		return nil, fmt.Errorf("bot %q not found in registry", botName)
	}

	excl := resolveInjectExcludeForAdmin(a.cfgStore.Load(), platform, botName)
	configs, err := agentconfig.Load(a.agentConfigDir, platform, agentConfigBotName(botName), excl...)
	if err != nil {
		return nil, fmt.Errorf("load agent config: %w", err)
	}

	content := getConfigField(configs, file)
	source := agentconfig.ResolvedSource(a.agentConfigDir, platform, agentConfigBotName(botName), string(file))

	return &admin.AgentConfigFile{
		Content: content,
		Source:  source,
		Size:    len(content),
		File:    string(file),
	}, nil
}

// GetSystemPromptPreview returns the assembled B+C channel system prompt for the named bot.
func (a *botConfigAdapter) GetSystemPromptPreview(ctx context.Context, botName string) (string, error) {
	platform, ok := resolvePlatformForBot(botName)
	if !ok {
		return "", fmt.Errorf("bot %q not found in registry", botName)
	}

	excl := resolveInjectExcludeForAdmin(a.cfgStore.Load(), platform, botName)
	configs, err := agentconfig.Load(a.agentConfigDir, platform, agentConfigBotName(botName), excl...)
	if err != nil {
		return "", fmt.Errorf("load agent config: %w", err)
	}

	return agentconfig.BuildSystemPrompt(configs), nil
}

// UpdateBotConfig applies partial updates to an existing bot configuration.
func (a *botConfigAdapter) UpdateBotConfig(ctx context.Context, name string, attrs *admin.BotConfigAttrs) error {
	platform, ok := resolvePlatformForBot(name)
	if !ok {
		return fmt.Errorf("bot %q not found in registry", name)
	}

	cfg := a.cfgStore.Load()

	switch platform {
	case "slack":
		bot := resolveSlackBot(cfg, name)
		if bot == nil {
			return fmt.Errorf("bot %q not found in slack config", name)
		}
		applyBotAttrsToSlack(bot, attrs)
	case "feishu":
		bot := resolveFeishuBot(cfg, name)
		if bot == nil {
			return fmt.Errorf("bot %q not found in feishu config", name)
		}
		applyBotAttrsToFeishu(bot, attrs)
	default:
		return fmt.Errorf("unknown platform %q", platform)
	}

	return a.writeConfig(cfg)
}

// CreateBot registers a new bot with the given attributes.
func (a *botConfigAdapter) CreateBot(ctx context.Context, name string, attrs *admin.BotConfigAttrs) error {
	if name == "" {
		return fmt.Errorf("bot name must not be empty")
	}
	if err := agentconfig.ValidateBotName(name); err != nil {
		return fmt.Errorf("invalid bot name: %w", err)
	}

	// Check that the name does not already exist in the registry.
	registry := messaging.DefaultBotRegistry()
	if _, found := registry.GetByName(name); found {
		return fmt.Errorf("bot %q already exists", name)
	}

	// Determine which platform to create the bot on.

	const maxBotsPerPlatform = 10
	cfg := a.cfgStore.Load()
	platform := attrs.Platform
	if platform == "" {
		// Fallback: prefer the first enabled platform.
		switch {
		case cfg.Messaging.Feishu.Enabled:
			platform = "feishu"
		case cfg.Messaging.Slack.Enabled:
			platform = "slack"
		default:
			return fmt.Errorf("no messaging platform enabled; cannot create bot")
		}
	}

	// Enforce max bots per platform limit.

	switch platform {
	case "feishu":
		if len(cfg.Messaging.Feishu.Bots) >= maxBotsPerPlatform {
			return fmt.Errorf("feishu bot limit reached (max %d)", maxBotsPerPlatform)
		}
		if !cfg.Messaging.Feishu.Enabled {
			return fmt.Errorf("feishu platform is not enabled")
		}
		if resolveFeishuBot(cfg, name) != nil {
			return fmt.Errorf("bot %q already exists in feishu config", name)
		}
		newBot := config.FeishuBotConfig{Name: name}
		applyBotAttrsToFeishu(&newBot, attrs)
		cfg.Messaging.Feishu.Bots = append(cfg.Messaging.Feishu.Bots, newBot)
	case "slack":
		if len(cfg.Messaging.Slack.Bots) >= maxBotsPerPlatform {
			return fmt.Errorf("slack bot limit reached (max %d)", maxBotsPerPlatform)
		}
		if !cfg.Messaging.Slack.Enabled {
			return fmt.Errorf("slack platform is not enabled")
		}
		if resolveSlackBot(cfg, name) != nil {
			return fmt.Errorf("bot %q already exists in slack config", name)
		}
		newBot := config.SlackBotConfig{Name: name}
		applyBotAttrsToSlack(&newBot, attrs)
		cfg.Messaging.Slack.Bots = append(cfg.Messaging.Slack.Bots, newBot)
	default:
		return fmt.Errorf("unsupported platform %q", platform)
	}

	// Create agent-config directory for the bot.
	botDir := filepath.Join(a.agentConfigDir, platform, name)
	if err := os.MkdirAll(botDir, 0o755); err != nil {
		return fmt.Errorf("create agent config directory: %w", err)
	}

	return a.writeConfig(cfg)
}

// DeleteBot removes a bot registration by name.
func (a *botConfigAdapter) DeleteBot(ctx context.Context, name string) error {
	platform, ok := resolvePlatformForBot(name)
	if !ok {
		return fmt.Errorf("bot %q not found in registry", name)
	}

	// Check if the bot is currently running.
	registry := messaging.DefaultBotRegistry()
	entry, found := registry.GetByName(name)
	if found && entry.Status == messaging.BotStatusRunning {
		return fmt.Errorf("bot %q is running (status=%s); stop it before deleting: %w", name, entry.Status, admin.ErrBotRunning)
	}

	cfg := a.cfgStore.Load()

	switch platform {
	case "slack":
		idx := findSlackBotIndex(cfg, name)
		if idx < 0 {
			return fmt.Errorf("bot %q not found in slack config", name)
		}
		cfg.Messaging.Slack.Bots = append(cfg.Messaging.Slack.Bots[:idx], cfg.Messaging.Slack.Bots[idx+1:]...)
	case "feishu":
		idx := findFeishuBotIndex(cfg, name)
		if idx < 0 {
			return fmt.Errorf("bot %q not found in feishu config", name)
		}
		cfg.Messaging.Feishu.Bots = append(cfg.Messaging.Feishu.Bots[:idx], cfg.Messaging.Feishu.Bots[idx+1:]...)
	default:
		return fmt.Errorf("unknown platform %q", platform)
	}

	return a.writeConfig(cfg)
}

// WriteAgentConfigFile writes content to a single agent config file for the named bot.
func (a *botConfigAdapter) WriteAgentConfigFile(ctx context.Context, botName string, file admin.AgentConfigFileName, content string) error {
	platform, ok := resolvePlatformForBot(botName)
	if !ok {
		return fmt.Errorf("bot %q not found in registry", botName)
	}

	if err := agentconfig.WriteFile(a.agentConfigDir, platform, agentConfigBotName(botName), string(file), content, agentconfig.MaxFileChars); err != nil {
		return fmt.Errorf("write agent config file: %w", err)
	}
	return nil
}

// GetPlatformAgentConfigFile reads a single platform-level (channel team
// default) agent config file. Unlike GetAgentConfigFile it addresses the
// dir/{platform}/ layer directly with an empty botName, so platforms without a
// bot instance (e.g. webchat) are reachable without consulting the messaging
// registry. The platform must be a recognized identifier.
func (a *botConfigAdapter) GetPlatformAgentConfigFile(ctx context.Context, platform string, file admin.AgentConfigFileName) (*admin.AgentConfigFile, error) {
	if !agentconfig.IsValidPlatform(platform) {
		return nil, fmt.Errorf("unknown platform %q", platform)
	}

	// Empty botName: resolveFile skips the bot level and resolves platform →
	// global, matching LoadForWorkspace's team-default semantics for webchat.
	excl := resolveInjectExcludeForAdmin(a.cfgStore.Load(), platform, "")
	configs, err := agentconfig.Load(a.agentConfigDir, platform, "", excl...)
	if err != nil {
		return nil, fmt.Errorf("load platform agent config: %w", err)
	}

	content := getConfigField(configs, file)
	source := agentconfig.ResolvedSource(a.agentConfigDir, platform, "", string(file))

	return &admin.AgentConfigFile{
		Content: content,
		Source:  source,
		Size:    len(content),
		File:    string(file),
	}, nil
}

// WritePlatformAgentConfigFile writes content to a single platform-level
// agent config file (channel team default). A present empty file explicitly
// clears the slot and stops fallback, matching the three-state read semantics.
func (a *botConfigAdapter) WritePlatformAgentConfigFile(ctx context.Context, platform string, file admin.AgentConfigFileName, content string) error {
	if !agentconfig.IsValidPlatform(platform) {
		return fmt.Errorf("unknown platform %q", platform)
	}

	if err := agentconfig.WriteFile(a.agentConfigDir, platform, "", string(file), content, agentconfig.MaxFileChars); err != nil {
		return fmt.Errorf("write platform agent config file: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Helper functions
// ---------------------------------------------------------------------------

// resolvePlatformForBot looks up the bot name in the global registry and
// returns its platform type.
func resolvePlatformForBot(name string) (platform string, ok bool) {
	registry := messaging.DefaultBotRegistry()
	entry, found := registry.GetByName(name)
	if !found {
		return "", false
	}
	return string(entry.Platform), true
}

// agentConfigBotName returns the botName to pass to agentconfig.Load and
// friends. Single-bot entries (auto-wrapped from platform-level credentials)
// keep resolving against the platform-level directory (dir/<platform>/), so
// they take an empty botName; real multi-bot entries use their own name.
func agentConfigBotName(name string) string {
	registry := messaging.DefaultBotRegistry()
	if entry, ok := registry.GetByName(name); ok && entry.IsSingleBot {
		return ""
	}
	return name
}

// resolveInjectExcludeForAdmin resolves the inject_exclude list for a bot
// using 3-level fallback: bot → platform → global.
func resolveInjectExcludeForAdmin(cfg *config.Config, platform, botName string) []string {
	global := cfg.AgentConfig.InjectExclude
	var platformExcl, botExcl []string
	switch platform {
	case "slack":
		platformExcl = cfg.Messaging.Slack.InjectExclude
		if bc := resolveSlackBot(cfg, botName); bc != nil {
			botExcl = bc.InjectExclude
		}
	case "feishu":
		platformExcl = cfg.Messaging.Feishu.InjectExclude
		if bc := resolveFeishuBot(cfg, botName); bc != nil {
			botExcl = bc.InjectExclude
		}
	case "yuanxin":
		platformExcl = cfg.Messaging.Yuanxin.InjectExclude
	}
	return config.ResolveInjectExclude(global, platformExcl, botExcl)
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
func getAgentConfigSummary(platform, botName, agentConfigDir string, injectExclude ...string) *admin.AgentConfigSummary {
	configs, err := agentconfig.Load(agentConfigDir, platform, botName, injectExclude...)
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
		{admin.AgentConfigTools, configs.Tools},
		{admin.AgentConfigUser, configs.User},
		{admin.AgentConfigMemory, configs.Memory},
	} {
		source := agentconfig.ResolvedSource(agentConfigDir, platform, botName, string(file.field))
		if source == "" {
			continue
		}
		meta := &admin.AgentConfigMeta{
			Source: source,
			Size:   len(file.value),
		}
		switch file.field {
		case admin.AgentConfigSoul:
			summary.Soul = meta
		case admin.AgentConfigAgents:
			summary.Agents = meta
		case admin.AgentConfigTools:
			summary.Tools = meta
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
	case admin.AgentConfigTools:
		return configs.Tools
	case admin.AgentConfigUser:
		return configs.User
	case admin.AgentConfigMemory:
		return configs.Memory
	default:
		return ""
	}
}

// writeConfig atomically writes the config to disk: marshal YAML, write to a
// temp file in the same directory, then rename over the original.
func (a *botConfigAdapter) writeConfig(cfg *config.Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	cfgPath := a.configFilePath
	dir := filepath.Dir(cfgPath)
	tmp, err := os.CreateTemp(dir, "hotplex-config-*")
	if err != nil {
		return fmt.Errorf("create temp config file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp config file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync temp config file: %w", err)
	}
	_ = tmp.Close()

	if err := os.Rename(tmpName, cfgPath); err != nil {
		return fmt.Errorf("rename config file: %w", err)
	}
	return nil
}

// applyBotAttrsToSlack applies non-zero/non-nil fields from attrs to the Slack bot config.
func applyBotAttrsToSlack(bot *config.SlackBotConfig, attrs *admin.BotConfigAttrs) {
	if attrs.WorkerType != "" {
		bot.WorkerType = attrs.WorkerType
	}
	if attrs.WorkDir != "" {
		bot.WorkDir = attrs.WorkDir
	}
	if attrs.DMPolicy != "" {
		bot.DMPolicy = attrs.DMPolicy
	}
	if attrs.GroupPolicy != "" {
		bot.GroupPolicy = attrs.GroupPolicy
	}
	if attrs.RequireMention {
		bot.RequireMention = &attrs.RequireMention
	}
	if len(attrs.AllowFrom) > 0 {
		bot.AllowFrom = attrs.AllowFrom
	}
	if len(attrs.AllowDMFrom) > 0 {
		bot.AllowDMFrom = attrs.AllowDMFrom
	}
	if len(attrs.AllowGroupFrom) > 0 {
		bot.AllowGroupFrom = attrs.AllowGroupFrom
	}
	if attrs.STT != nil && attrs.STT.Provider != "" {
		bot.Provider = attrs.STT.Provider
	}
	if attrs.TTS != nil {
		if attrs.TTS.Provider != "" {
			bot.TTSProvider = attrs.TTS.Provider
		}
		if attrs.TTS.Voice != "" {
			bot.Voice = attrs.TTS.Voice
		}
	}
	// Credentials
	if attrs.BotToken != "" {
		bot.BotToken = attrs.BotToken
	}
	if attrs.AppToken != "" {
		bot.AppToken = attrs.AppToken
	}
}

// applyBotAttrsToFeishu applies non-zero/non-nil fields from attrs to the Feishu bot config.
func applyBotAttrsToFeishu(bot *config.FeishuBotConfig, attrs *admin.BotConfigAttrs) {
	if attrs.WorkerType != "" {
		bot.WorkerType = attrs.WorkerType
	}
	if attrs.WorkDir != "" {
		bot.WorkDir = attrs.WorkDir
	}
	if attrs.DMPolicy != "" {
		bot.DMPolicy = attrs.DMPolicy
	}
	if attrs.GroupPolicy != "" {
		bot.GroupPolicy = attrs.GroupPolicy
	}
	if attrs.RequireMention {
		bot.RequireMention = &attrs.RequireMention
	}
	if len(attrs.AllowFrom) > 0 {
		bot.AllowFrom = attrs.AllowFrom
	}
	if len(attrs.AllowDMFrom) > 0 {
		bot.AllowDMFrom = attrs.AllowDMFrom
	}
	if len(attrs.AllowGroupFrom) > 0 {
		bot.AllowGroupFrom = attrs.AllowGroupFrom
	}
	if attrs.STT != nil && attrs.STT.Provider != "" {
		bot.Provider = attrs.STT.Provider
	}
	if attrs.TTS != nil {
		if attrs.TTS.Provider != "" {
			bot.TTSProvider = attrs.TTS.Provider
		}
		if attrs.TTS.Voice != "" {
			bot.Voice = attrs.TTS.Voice
		}
	}
	// Credentials
	if attrs.AppID != "" {
		bot.AppID = attrs.AppID
	}
	if attrs.AppSecret != "" {
		bot.AppSecret = attrs.AppSecret
	}
}

// findSlackBotIndex returns the index of the Slack bot with the given name, or -1.
func findSlackBotIndex(cfg *config.Config, name string) int {
	for i := range cfg.Messaging.Slack.Bots {
		if cfg.Messaging.Slack.Bots[i].Name == name {
			return i
		}
	}
	return -1
}

// findFeishuBotIndex returns the index of the Feishu bot with the given name, or -1.
func findFeishuBotIndex(cfg *config.Config, name string) int {
	for i := range cfg.Messaging.Feishu.Bots {
		if cfg.Messaging.Feishu.Bots[i].Name == name {
			return i
		}
	}
	return -1
}
