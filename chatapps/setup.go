package chatapps

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hrygo/hotplex/chatapps/base"
	"github.com/hrygo/hotplex/chatapps/feishu"
	"github.com/hrygo/hotplex/chatapps/slack"
	"github.com/hrygo/hotplex/chatapps/slack/apphome"
	"github.com/hrygo/hotplex/engine"
	"github.com/hrygo/hotplex/internal/sys"
	"github.com/hrygo/hotplex/provider"
)

// IsEnabled returns true if ChatApps should be activated based on environment variables or flags.
// It returns true if any of the following is true:
// 1. HOTPLEX_CHATAPPS_ENABLED environment variable is "true"
// 2. configDir parameter is not empty (explicitly set via --config flag)
// 3. HOTPLEX_CHATAPPS_CONFIG_DIR environment variable is not empty
func IsEnabled(configDir string) bool {
	if os.Getenv("HOTPLEX_CHATAPPS_ENABLED") == "true" {
		return true
	}
	if configDir != "" {
		return true
	}
	if os.Getenv("HOTPLEX_CHATAPPS_CONFIG_DIR") != "" {
		return true
	}
	return false
}

// Setup initializes all enabled ChatApps and their dedicated Engines.
// It returns an http.Handler that handles all webhook routes.
// The configDir parameter takes priority over HOTPLEX_CHATAPPS_CONFIG_DIR environment variable.
func Setup(ctx context.Context, logger *slog.Logger, configDir ...string) (http.Handler, *AdapterManager, error) {
	// Config directory search priority:
	// 1. configDir parameter (--config flag, highest)
	// 2. HOTPLEX_CHATAPPS_CONFIG_DIR environment variable
	// 3. ~/.hotplex/configs (user config)
	// 4. ./configs/chatapps (default)
	dir := ""

	// 1. configDir parameter (highest priority)
	if len(configDir) > 0 && configDir[0] != "" {
		dir = configDir[0]
	}

	// 2. HOTPLEX_CHATAPPS_CONFIG_DIR env var
	if dir == "" {
		dir = os.Getenv("HOTPLEX_CHATAPPS_CONFIG_DIR")
	}

	// 3. User config directory
	if dir == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			logger.Debug("Could not determine user home directory", "cause", err)
		} else {
			userConfigDir := filepath.Join(homeDir, ".hotplex", "configs")
			if _, err := os.Stat(userConfigDir); err != nil {
				logger.Debug("User config directory does not exist", "path", userConfigDir, "cause", err)
			} else {
				dir = userConfigDir
				logger.Info("Using user config directory", "path", dir)
			}
		}
	}

	// 4. Default config directory
	if dir == "" {
		dir = "configs/chatapps"
		// Check if default config directory exists
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			logger.Info("Default config directory not found, skipping config loading", "path", dir)
			dir = ""
		}
	}

	var loader *ConfigLoader
	var err error
	if dir != "" {
		loader, err = NewConfigLoader(dir, logger)
		if err != nil {
			logger.Info("Could not load configuration from directory", "path", dir, "cause", err)
			// Don't fail completely, try to continue with env-based config
		}
	}

	manager := NewAdapterManager(logger)

	// Slack
	setupPlatform(ctx, "slack", loader, manager, logger, func(pc *PlatformConfig) ChatAdapter {
		token := os.Getenv("HOTPLEX_SLACK_BOT_TOKEN")
		if token == "" {
			return nil
		}

		mode := os.Getenv("HOTPLEX_SLACK_MODE")
		if mode == "" {
			mode = "http" // default to http
		}
		config := &slack.Config{
			BotToken:      token,
			AppToken:      os.Getenv("HOTPLEX_SLACK_APP_TOKEN"),
			SigningSecret: os.Getenv("HOTPLEX_SLACK_SIGNING_SECRET"),
			Mode:          mode,
			ServerAddr:    os.Getenv("HOTPLEX_SLACK_SERVER_ADDR"),
		}

		// Apply YAML config if available
		if pc != nil {
			config.SystemPrompt = pc.SystemPrompt

			// Map Security & Permission from YAML
			config.BotUserID = pc.Security.Permission.BotUserID
			config.VerifySignature = pc.Security.VerifySignature
			config.DMPolicy = pc.Security.Permission.DMPolicy
			config.GroupPolicy = pc.Security.Permission.GroupPolicy
			config.AllowedUsers = pc.Security.Permission.AllowedUsers
			config.BlockedUsers = pc.Security.Permission.BlockedUsers
			config.SlashCommandRateLimit = pc.Security.Permission.SlashCommandRateLimit

			// Map Owner Configuration (Phase 1: Bot Behavior Spec)
			if pc.Security.Owner != nil {
				config.Owner = &slack.OwnerConfig{
					Primary: pc.Security.Owner.Primary,
					Trusted: pc.Security.Owner.Trusted,
					Policy:  slack.OwnerPolicy(pc.Security.Owner.Policy),
				}
			}

			// Map Thread Ownership Configuration (Phase 1: Bot Behavior Spec)
			if pc.Security.Permission.ThreadOwnership != nil {
				config.ThreadOwnership = &slack.ThreadOwnershipConfig{
					Enabled: pc.Security.Permission.ThreadOwnership.Enabled,
					TTL:     pc.Security.Permission.ThreadOwnership.TTL,
					Persist: pc.Security.Permission.ThreadOwnership.Persist,
				}
			}

			// Map Features (Phase 2)
			config.Features = slack.FeaturesConfig{
				Chunking: slack.ChunkingConfig{
					Enabled:  pc.Features.Chunking.Enabled,
					MaxChars: pc.Features.Chunking.MaxChars,
				},
				Threading: slack.ThreadingConfig{
					Enabled: pc.Features.Threading.Enabled,
				},
				RateLimit: slack.RateLimitConfig{
					Enabled:     pc.Features.RateLimit.Enabled,
					MaxAttempts: pc.Features.RateLimit.MaxAttempts,
					BaseDelayMs: pc.Features.RateLimit.BaseDelayMs,
					MaxDelayMs:  pc.Features.RateLimit.MaxDelayMs,
				},
				Markdown: slack.MarkdownConfig{
					Enabled: pc.Features.Markdown.Enabled,
				},
			}

			// Map Message Storage (Phase 3)
			if pc.MessageStore.Enabled != nil {
				config.Storage = &slack.StorageConfig{
					Enabled:       pc.MessageStore.Enabled,
					Type:          pc.MessageStore.Type,
					SQLitePath:    pc.MessageStore.SQLite.Path,
					PostgreSQLURL: pc.MessageStore.Postgres.DSN,
					StreamEnabled: pc.MessageStore.Streaming.Enabled,
					StreamTimeout: pc.MessageStore.Streaming.Timeout,
				}
			}

			// Debug: Log GroupPolicy value
			logger.Info("Slack config loaded from YAML",
				"group_policy", config.GroupPolicy,
				"bot_user_id", config.BotUserID,
				"dm_policy", config.DMPolicy,
				"owner_policy", config.GetOwnerPolicy(),
				"thread_ownership", config.IsThreadOwnershipEnabled())

			// Set broadcast response for multibot mode (Empty string means silence)
			config.SetBroadcastResponse(pc.Security.Permission.BroadcastResponse)

			// AppToken fallback
			if config.AppToken == "" && pc.Options != nil {
				if appToken, ok := pc.Options["app_token"].(string); ok {
					config.AppToken = os.ExpandEnv(appToken)
				}
			}

			// Mode from YAML overrides env var (if set)
			if pc.Mode != "" {
				config.Mode = pc.Mode
			}
		}

		var opts []base.AdapterOption
		if pc != nil {
			opts = append(opts, base.WithSessionTimeout(pc.Session.Timeout))
			opts = append(opts, base.WithCleanupInterval(pc.Session.CleanupInterval))
		}
		opts = append(opts, base.WithoutServer())
		return slack.NewAdapter(config, logger, opts...)
	}, "HOTPLEX_SLACK_BOT_TOKEN")
	// Feishu
	setupPlatform(ctx, "feishu", loader, manager, logger, func(pc *PlatformConfig) ChatAdapter {
		appID := os.Getenv("HOTPLEX_FEISHU_APP_ID")
		if appID == "" {
			return nil
		}
		config := &feishu.Config{
			AppID:             appID,
			AppSecret:         os.Getenv("HOTPLEX_FEISHU_APP_SECRET"),
			VerificationToken: os.Getenv("HOTPLEX_FEISHU_VERIFICATION_TOKEN"),
			EncryptKey:        os.Getenv("HOTPLEX_FEISHU_ENCRYPT_KEY"),
			ServerAddr:        os.Getenv("HOTPLEX_FEISHU_SERVER_ADDR"),
		}

		if pc != nil {
			config.SystemPrompt = pc.SystemPrompt
		}

		var opts []base.AdapterOption
		if pc != nil {
			opts = append(opts, base.WithSessionTimeout(pc.Session.Timeout))
			opts = append(opts, base.WithCleanupInterval(pc.Session.CleanupInterval))
		}
		opts = append(opts, base.WithoutServer())
		adapter, _ := feishu.NewAdapter(config, logger, opts...)
		return adapter
	}, "HOTPLEX_FEISHU_APP_ID")

	if err := manager.StartAll(ctx); err != nil {
		return nil, nil, fmt.Errorf("start all adapters: %w", err)
	}

	if len(manager.ListPlatforms()) == 0 {
		logger.Error("No ChatApp platforms were successfully initialized. Please check your configuration.")
	} else {
		logger.Info("ChatApps setup completed", "platforms", manager.ListPlatforms())
	}

	return manager.Handler(), manager, nil
}

func setupPlatform(
	_ context.Context,
	platform string,
	loader *ConfigLoader,
	manager *AdapterManager,
	logger *slog.Logger,
	adapterFactory func(*PlatformConfig) ChatAdapter,
	requiredEnvVars ...string,
) {
	// Early exit if required environment variables are not set
	// This avoids unnecessary YAML config loading and engine creation
	if len(requiredEnvVars) > 0 {
		missing := false
		for _, envVar := range requiredEnvVars {
			if os.Getenv(envVar) == "" {
				missing = true
				break
			}
		}
		if missing {
			logger.Info("Platform skipped (missing required env vars)", "platform", platform, "required", requiredEnvVars)
			return
		}
	}

	var pc *PlatformConfig
	if loader != nil {
		pc = loader.GetConfig(platform)
	}
	if pc == nil {
		pc = &PlatformConfig{Platform: platform}
	}

	// 1. Create dedicated Engine for this platform
	eng, err := createEngineForPlatform(pc, logger)
	if err != nil {
		logger.Error("Failed to create engine for platform", "platform", platform, "error", err)
		return
	}
	manager.RegisterEngine(eng)

	// 2. Create Adapter
	adapter := adapterFactory(pc)
	if adapter == nil {
		logger.Info("Platform not initialized (likely missing credentials)", "platform", platform)
		return
	}

	// Wire up Engine for slash command support (platform-agnostic via interface)
	// Only adapters that implement EngineSupport will receive the engine
	if engineSupport, ok := adapter.(base.EngineSupport); ok {
		engineSupport.SetEngine(eng)
		logger.Info("Engine injected", "platform", platform)
	} else {
		logger.Info("Adapter does not implement EngineSupport", "platform", platform)
	}

	// 3. Create EngineMessageHandler
	// Wrap engine.Engine to implement chatapps.Engine interface
	wrappedEng := &engineWrapper{eng: eng}
	msgHandler := NewEngineMessageHandler(wrappedEng, manager,
		WithConfigLoader(loader),
		WithLogger(logger),
		WithWorkDirFn(func(sessionID string) string {
			// Use work_dir from config if specified
			if pc.Engine.WorkDir != "" {
				// Expand environment variables, ~ to home, and resolve .
				workDir := sys.ExpandPath(pc.Engine.WorkDir)
				logger.Info("Work directory resolved",
					"platform", platform,
					"config", pc.Engine.WorkDir,
					"path", workDir)
				return workDir
			}
			// Default: use temp directory with platform/session isolation
			defaultDir := filepath.Join("/tmp/hotplex-chatapps", platform, sessionID)
			logger.Debug("Using default temp work_dir",
				"platform", platform,
				"default_path", defaultDir)
			return defaultDir
		}),
	)

	// 4. Link everything
	adapter.SetHandler(msgHandler.Handle)

	if err := manager.Register(adapter); err != nil {
		logger.Error("Failed to register adapter", "platform", platform, "error", err)
	} else {
		// Setup AppHome capability center for Slack after registration
		if platform == "slack" {
			if slackAdapter, ok := adapter.(*slack.Adapter); ok {
				client := slackAdapter.GetSlackClient()
				if client != nil {
					appHomeConfig := apphome.Config{
						Enabled:           true,
						CapabilitiesPath:  os.Getenv("HOTPLEX_SLACK_CAPABILITIES_PATH"),
					}
					// Pass nil for brain - can be set later if needed
					handler, _, _ := apphome.Setup(client, nil, appHomeConfig, logger)
					if handler != nil {
						slackAdapter.SetAppHomeHandler(handler)
						logger.Info("AppHome capability center initialized", "platform", platform)
					}
				}
			}
		}

		if pc != nil && pc.SourceFile != "" {
			logger.Info("Platform successfully initialized from configuration file", "platform", platform, "file", pc.SourceFile)
		} else {
			logger.Info("Platform successfully initialized from environment variables", "platform", platform)
		}
	}
}

func createEngineForPlatform(pc *PlatformConfig, logger *slog.Logger) (*engine.Engine, error) {
	// Initialize Provider
	pCfg := pc.Provider
	if pCfg.Type == "" {
		pCfg.Type = provider.ProviderTypeClaudeCode
	}
	if pCfg.Enabled == nil {
		enabled := true
		pCfg.Enabled = &enabled
	}

	prv, err := provider.CreateProvider(pCfg)
	if err != nil {
		return nil, fmt.Errorf("create provider: %w", err)
	}

	// Engine options with defaults
	timeout := pc.Engine.Timeout
	if timeout == 0 {
		timeout = 30 * time.Minute
	}
	idleTimeout := pc.Engine.IdleTimeout
	if idleTimeout == 0 {
		idleTimeout = 30 * time.Minute
	}

	// Tool Filtering Logic: Provider-level takes precedence over Engine-level
	allowedTools := pc.Provider.AllowedTools
	if len(allowedTools) == 0 {
		allowedTools = pc.Engine.AllowedTools
	}
	disallowedTools := pc.Provider.DisallowedTools
	if len(disallowedTools) == 0 {
		disallowedTools = pc.Engine.DisallowedTools
	}

	opts := engine.EngineOptions{
		Timeout:          timeout,
		IdleTimeout:      idleTimeout,
		Logger:           logger,
		Namespace:        pc.Platform,
		BaseSystemPrompt: pc.SystemPrompt,
		Provider:         prv,
		// Pass permission settings from YAML config
		PermissionMode:             pc.Provider.DefaultPermissionMode,
		DangerouslySkipPermissions: BoolValue(pc.Provider.DangerouslySkipPermissions, true),
		AllowedTools:               allowedTools,
		DisallowedTools:            disallowedTools,
	}

	return engine.NewEngine(opts)
}

// ExpandPath expands ~ to the user's home directory and cleans the path.
// Supports both ~ and ~/path formats.
// Returns an empty string if the path contains traversal attacks.
func ExpandPath(path string) string {
	if path == "" {
		return ""
	}
	expanded := sys.ExpandPath(path)
	if expanded == "" {
		return ""
	}
	if strings.HasPrefix(expanded, "/") && isSensitivePath(expanded) {
		return "" // Block access to sensitive paths
	}
	return filepath.Clean(expanded)
}

// isSensitivePath checks if a path points to a sensitive system location
func isSensitivePath(path string) bool {
	// List of sensitive directories to block
	sensitivePrefixes := []string{
		"/etc/",
		"/etc",
		"/var/",
		"/var",
		"/usr/",
		"/usr",
		"/bin",
		"/sbin",
		"/root",
		"/proc/",
		"/proc",
		"/sys/",
		"/sys",
		"/boot",
		"/dev/",
		"/dev",
	}

	lowerPath := strings.ToLower(path)
	for _, prefix := range sensitivePrefixes {
		if strings.HasPrefix(lowerPath, prefix) {
			return true
		}
	}
	return false
}
