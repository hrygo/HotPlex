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
	"github.com/hrygo/hotplex/chatapps/dingtalk"
	"github.com/hrygo/hotplex/chatapps/discord"
	"github.com/hrygo/hotplex/chatapps/slack"
	"github.com/hrygo/hotplex/chatapps/telegram"
	"github.com/hrygo/hotplex/chatapps/whatsapp"
	"github.com/hrygo/hotplex/engine"
	"github.com/hrygo/hotplex/provider"
)

// Setup initializes all enabled ChatApps and their dedicated Engines.
// It returns an http.Handler that handles all webhook routes.
func Setup(ctx context.Context, logger *slog.Logger) (http.Handler, *AdapterManager, error) {
	configDir := os.Getenv("CHATAPPS_CONFIG_DIR")
	if configDir == "" {
		configDir = "chatapps/configs"
	}

	loader, err := NewConfigLoader(configDir, logger)
	if err != nil {
		logger.Warn("Failed to load platform configs, using defaults", "error", err)
		// Don't fail completely, try to continue with env-based config
	}

	manager := NewAdapterManager(logger)

	// Telegram
	setupPlatform(ctx, "telegram", loader, manager, logger, func(pc *PlatformConfig) ChatAdapter {
		token := os.Getenv("TELEGRAM_BOT_TOKEN")
		if token == "" {
			return nil
		}
		cfg := telegram.Config{
			BotToken:    token,
			WebhookURL:  os.Getenv("TELEGRAM_WEBHOOK_URL"),
			SecretToken: os.Getenv("TELEGRAM_SECRET_TOKEN"),
		}
		if pc != nil {
			cfg.SystemPrompt = pc.SystemPrompt
		}
		return telegram.NewAdapter(cfg, logger, base.WithoutServer())
	})

	// Discord
	setupPlatform(ctx, "discord", loader, manager, logger, func(pc *PlatformConfig) ChatAdapter {
		token := os.Getenv("DISCORD_BOT_TOKEN")
		if token == "" {
			return nil
		}
		cfg := discord.Config{
			BotToken:  token,
			PublicKey: os.Getenv("DISCORD_PUBLIC_KEY"),
		}
		if pc != nil {
			cfg.SystemPrompt = pc.SystemPrompt
		}
		return discord.NewAdapter(cfg, logger, base.WithoutServer())
	})

	// Slack
	setupPlatform(ctx, "slack", loader, manager, logger, func(pc *PlatformConfig) ChatAdapter {
		token := os.Getenv("SLACK_BOT_TOKEN")
		if token == "" {
			return nil
		}

		mode := os.Getenv("SLACK_MODE")
		if mode == "" {
			mode = "http" // default to http
		}
		config := &slack.Config{
			BotToken:      token,
			AppToken:      os.Getenv("SLACK_APP_TOKEN"),
			SigningSecret: os.Getenv("SLACK_SIGNING_SECRET"),
			Mode:          mode,
			ServerAddr:    os.Getenv("SLACK_SERVER_ADDR"),
		}

		// Apply YAML config if available
		if pc != nil {
			config.SystemPrompt = pc.SystemPrompt

			// Map Security & Permission from YAML
			config.BotUserID = pc.Security.Permission.BotUserID
			config.DMPolicy = pc.Security.Permission.DMPolicy
			config.GroupPolicy = pc.Security.Permission.GroupPolicy
			config.AllowedUsers = pc.Security.Permission.AllowedUsers
			config.BlockedUsers = pc.Security.Permission.BlockedUsers
			config.SlashCommandRateLimit = pc.Security.Permission.SlashCommandRateLimit

			// AppToken fallback
			if config.AppToken == "" && pc.Options != nil {
				if appToken, ok := pc.Options["app_token"].(string); ok {
					config.AppToken = os.ExpandEnv(appToken)
				}
			}
		}

		return slack.NewAdapter(config, logger, base.WithoutServer())
	})

	// DingTalk
	setupPlatform(ctx, "dingtalk", loader, manager, logger, func(pc *PlatformConfig) ChatAdapter {
		appID := os.Getenv("DINGTALK_APP_ID")
		appSecret := os.Getenv("DINGTALK_APP_SECRET")
		if pc != nil && pc.DingTalk.AppID != "" {
			appID = pc.DingTalk.AppID
			appSecret = pc.DingTalk.AppSecret
		}

		if appID == "" {
			return nil
		}

		cfg := dingtalk.Config{
			AppID:         appID,
			AppSecret:     appSecret,
			CallbackToken: os.Getenv("DINGTALK_CALLBACK_TOKEN"),
			CallbackKey:   os.Getenv("DINGTALK_CALLBACK_KEY"),
		}
		if pc != nil {
			cfg.SystemPrompt = pc.SystemPrompt
			if pc.DingTalk.CallbackToken != "" {
				cfg.CallbackToken = pc.DingTalk.CallbackToken
			}
			if pc.DingTalk.CallbackKey != "" {
				cfg.CallbackKey = pc.DingTalk.CallbackKey
			}
			if pc.DingTalk.MaxMessageLen > 0 {
				cfg.MaxMessageLen = pc.DingTalk.MaxMessageLen
			}
		}
		return dingtalk.NewAdapter(cfg, logger, base.WithoutServer())
	})

	// WhatsApp
	setupPlatform(ctx, "whatsapp", loader, manager, logger, func(pc *PlatformConfig) ChatAdapter {
		phoneID := os.Getenv("WHATSAPP_PHONE_NUMBER_ID")
		accessToken := os.Getenv("WHATSAPP_ACCESS_TOKEN")
		if pc != nil && pc.WhatsApp.PhoneNumberID != "" {
			phoneID = pc.WhatsApp.PhoneNumberID
			accessToken = pc.WhatsApp.AccessToken
		}

		if phoneID == "" {
			return nil
		}

		cfg := whatsapp.Config{
			PhoneNumberID: phoneID,
			AccessToken:   accessToken,
			VerifyToken:   os.Getenv("WHATSAPP_VERIFY_TOKEN"),
		}
		if pc != nil {
			cfg.SystemPrompt = pc.SystemPrompt
			if pc.WhatsApp.VerifyToken != "" {
				cfg.VerifyToken = pc.WhatsApp.VerifyToken
			}
			if pc.WhatsApp.APIVersion != "" {
				cfg.APIVersion = pc.WhatsApp.APIVersion
			}
		}
		return whatsapp.NewAdapter(cfg, logger, base.WithoutServer())
	})

	if err := manager.StartAll(ctx); err != nil {
		return nil, nil, fmt.Errorf("start all adapters: %w", err)
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
) {
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
		logger.Debug("Platform not initialized (likely missing credentials)", "platform", platform)
		return
	}

	// Wire up Engine for slash command support (platform-agnostic via interface)
	// Only adapters that implement EngineSupport will receive the engine
	if engineSupport, ok := adapter.(base.EngineSupport); ok {
		engineSupport.SetEngine(eng)
		logger.Debug("Engine injected", "platform", platform)
	} else {
		logger.Debug("Adapter does not implement EngineSupport", "platform", platform)
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
				// Expand ~ to home directory and resolve . to absolute path
				workDir := expandPath(pc.Engine.WorkDir)
				logger.Debug("Using work_dir from config",
					"platform", platform,
					"config_value", pc.Engine.WorkDir,
					"resolved_path", workDir)
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
	}
}

func createEngineForPlatform(pc *PlatformConfig, logger *slog.Logger) (*engine.Engine, error) {
	// Initialize Provider
	pCfg := pc.Provider
	if pCfg.Type == "" {
		pCfg.Type = provider.ProviderTypeClaudeCode
	}
	pCfg.Enabled = true // Ensure it's enabled

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
		PermissionMode:  pc.Provider.DefaultPermissionMode,
		AllowedTools:    allowedTools,
		DisallowedTools: disallowedTools,
	}

	return engine.NewEngine(opts)
}

// expandPath expands ~ to the user's home directory and cleans the path.
// Supports both ~ and ~/path formats.
// Returns an empty string if the path contains traversal attacks.
func expandPath(path string) string {
	if len(path) == 0 {
		return path
	}

	// Handle ~ expansion
	if path[0] == '~' {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return path // Return original path if home dir cannot be determined
		}

		if len(path) == 1 {
			return homeDir
		}

		// Handle ~/path
		if path[1] == '/' || path[1] == filepath.Separator {
			return filepath.Join(homeDir, path[2:])
		}

		// Handle ~username/path (not commonly used, but supported)
		return filepath.Join(homeDir, path[1:])
	}

	// Handle special case: "." should be expanded to current working directory
	if path == "." {
		cwd, err := os.Getwd()
		if err != nil {
			return path // Return original if we can't get cwd
		}
		return cwd
	}

	// Clean the path to resolve any . or .. elements
	cleaned := filepath.Clean(path)

	// Security check: detect path traversal attempts
	// After cleaning, paths starting with / are absolute
	// Paths starting with .. are attempting to escape the current directory
	if strings.HasPrefix(cleaned, "/") {
		// Absolute path - check for common system directories
		if isSensitivePath(cleaned) {
			return "" // Block access to sensitive paths
		}
	}

	return cleaned
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
