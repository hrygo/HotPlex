package chatapps

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
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
	if token := os.Getenv("TELEGRAM_BOT_TOKEN"); token != "" {
		setupPlatform(ctx, "telegram", loader, manager, logger, func(pc *PlatformConfig) ChatAdapter {
			return telegram.NewAdapter(telegram.Config{
				BotToken:    token,
				WebhookURL:  os.Getenv("TELEGRAM_WEBHOOK_URL"),
				SecretToken: os.Getenv("TELEGRAM_SECRET_TOKEN"),
			}, logger, base.WithoutServer())
		})
	}

	// Discord
	if token := os.Getenv("DISCORD_BOT_TOKEN"); token != "" {
		setupPlatform(ctx, "discord", loader, manager, logger, func(pc *PlatformConfig) ChatAdapter {
			return discord.NewAdapter(discord.Config{
				BotToken:  token,
				PublicKey: os.Getenv("DISCORD_PUBLIC_KEY"),
			}, logger, base.WithoutServer())
		})
	}

	// Slack
	if token := os.Getenv("SLACK_BOT_TOKEN"); token != "" {
		setupPlatform(ctx, "slack", loader, manager, logger, func(pc *PlatformConfig) ChatAdapter {
			mode := os.Getenv("SLACK_MODE")
			if mode == "" {
				mode = "http" // default to http
			}
			return slack.NewAdapter(slack.Config{
				BotToken:      token,
				AppToken:      os.Getenv("SLACK_APP_TOKEN"),
				SigningSecret: os.Getenv("SLACK_SIGNING_SECRET"),
				Mode:          mode,
				ServerAddr:    os.Getenv("SLACK_SERVER_ADDR"),
			}, logger, base.WithoutServer())
		})
	}

	// DingTalk
	if appID := os.Getenv("DINGTALK_APP_ID"); appID != "" {
		setupPlatform(ctx, "dingtalk", loader, manager, logger, func(pc *PlatformConfig) ChatAdapter {
			return dingtalk.NewAdapter(dingtalk.Config{
				AppID:         appID,
				AppSecret:     os.Getenv("DINGTALK_APP_SECRET"),
				CallbackToken: os.Getenv("DINGTALK_CALLBACK_TOKEN"),
				CallbackKey:   os.Getenv("DINGTALK_CALLBACK_KEY"),
			}, logger, base.WithoutServer())
		})
	}

	// WhatsApp
	if phoneID := os.Getenv("WHATSAPP_PHONE_NUMBER_ID"); phoneID != "" {
		setupPlatform(ctx, "whatsapp", loader, manager, logger, func(pc *PlatformConfig) ChatAdapter {
			return whatsapp.NewAdapter(whatsapp.Config{
				PhoneNumberID: phoneID,
				AccessToken:   os.Getenv("WHATSAPP_ACCESS_TOKEN"),
				VerifyToken:   os.Getenv("WHATSAPP_VERIFY_TOKEN"),
			}, logger, base.WithoutServer())
		})
	}

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

	// 3. Create EngineMessageHandler
	msgHandler := NewEngineMessageHandler(eng, manager,
		WithConfigLoader(loader),
		WithLogger(logger),
		WithWorkDirFn(func(sessionID string) string {
			// Use work_dir from config if specified
			if pc.Engine.WorkDir != "" {
				// Expand ~ to home directory
				workDir := expandPath(pc.Engine.WorkDir)
				return workDir
			}
			// Default: use temp directory with platform/session isolation
			return filepath.Join("/tmp/hotplex-chatapps", platform, sessionID)
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

	opts := engine.EngineOptions{
		Timeout:          timeout,
		IdleTimeout:      idleTimeout,
		Logger:           logger,
		Namespace:        pc.Platform,
		BaseSystemPrompt: pc.SystemPrompt,
		Provider:         prv,
	}

	return engine.NewEngine(opts)
}

// expandPath expands ~ to the user's home directory and cleans the path.
// Supports both ~ and ~/path formats.
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

	// Clean the path to resolve any . or .. elements
	return filepath.Clean(path)
}
