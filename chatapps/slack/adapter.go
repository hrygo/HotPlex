// Package slack provides a high-performance, AI-native Slack adapter for the HotPlex engine.
// It supports bot-mode (HTTP) and Socket Mode (WebSocket), providing Slack-specific
// UI components (Block Kit), Assistant Threads, and streaming message capabilities.
package slack

import (
	"context"
	"log/slog"
	"net/http"
	"sync"

	"github.com/hrygo/hotplex/chatapps/base"
	"github.com/hrygo/hotplex/chatapps/command"
	"github.com/hrygo/hotplex/engine"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"
)

// Adapter implements the base.ChatAdapter interface for Slack.
// It acts as the central coordinator, orchestrating messaging, events,
// slash commands, and interactive components through specialized modules.
type Adapter struct {
	*base.Adapter
	config              *Config
	eventPath           string
	interactivePath     string
	slashCommandPath    string
	sender              *base.SenderWithMutex
	webhook             *base.WebhookRunner
	slashCommandHandler func(cmd SlashCommand)
	eng                 *engine.Engine
	rateLimiter         *SlashCommandRateLimiter

	// Command registry
	cmdRegistry *command.Registry

	// Slack SDK clients
	client            *slack.Client      // Official Slack SDK client (HTTP mode)
	socketModeClient  *socketmode.Client // Socket Mode client (WebSocket)
	messageBuilder    *MessageBuilder    // Converts base.ChatMessage to Slack blocks
	socketModeCtx     context.Context    // Socket Mode context for cancellation
	socketModeCancel  context.CancelFunc // Socket Mode cancel function
	socketModeRunning bool               // Whether Socket Mode is running
	socketModeMu      sync.Mutex         // Protects socketModeRunning
}

// Compile-time check: ensure Adapter implements StatusProvider
var _ base.StatusProvider = (*Adapter)(nil)

func NewAdapter(config *Config, logger *slog.Logger, opts ...base.AdapterOption) *Adapter {
	// Validate config
	if err := config.Validate(); err != nil {
		logger.Error("Invalid Slack config", "error", err)
	}

	// Initialize base adapter fields
	a := &Adapter{
		config:           config,
		eventPath:        "/events",
		interactivePath:  "/interactive",
		slashCommandPath: "/slack",
		sender:           base.NewSenderWithMutex(),
		webhook:          base.NewWebhookRunner(logger),
		rateLimiter:      NewSlashCommandRateLimiterWithConfig(config.SlashCommandRateLimit, rateBurst),
		messageBuilder:   NewMessageBuilder(), // Converts base.ChatMessage to Slack blocks using official SDK
		cmdRegistry:      command.NewRegistry(),
	}

	// Initialize Slack SDK client (github.com/slack-go/slack)
	if config.BotToken != "" {
		opts := []slack.Option{slack.OptionAppLevelToken(config.AppToken)}
		a.client = slack.New(config.BotToken, opts...)
	}

	// Prepare HTTP handlers for HTTP mode (not needed for Socket Mode)
	var httpOpts []base.AdapterOption
	if !config.IsSocketMode() || config.AppToken == "" {
		handlers := make(map[string]http.HandlerFunc)
		handlers[a.eventPath] = a.handleEvent
		handlers[a.interactivePath] = a.handleInteractive
		handlers[a.slashCommandPath] = a.handleSlashCommand

		// Build HTTP handler options
		for path, handler := range handlers {
			httpOpts = append(httpOpts, base.WithHTTPHandler(path, handler))
		}
	}

	// Combine user options with HTTP options
	allOpts := append(opts, httpOpts...)

	// Create base adapter first (needed for Logger)
	a.Adapter = base.NewAdapter("slack", base.Config{
		ServerAddr:   config.ServerAddr,
		SystemPrompt: config.SystemPrompt,
	}, logger, allOpts...)

	// Initialize Socket Mode client if enabled (preferred mode)
	if config.IsSocketMode() && config.AppToken != "" {
		a.Logger().Info("Initializing Socket Mode client", "mode", config.Mode)
		a.socketModeClient = socketmode.New(a.client)
	}

	// Set default sender that uses MessageBuilder + Slack SDK
	if config.BotToken != "" {
		a.sender.SetSender(a.defaultSender)
	}

	return a
}

// SetEngine sets the engine for the adapter (used for slash commands)
func (a *Adapter) SetEngine(eng *engine.Engine) {
	a.eng = eng

	// Register command executors after engine is set
	a.registerCommands()
}

// registerCommands registers all command executors to the registry
func (a *Adapter) registerCommands() {
	if a.eng == nil || a.cmdRegistry == nil {
		return
	}

	// Get workDir from config or use default (empty string will use os.Getwd() in executor)
	workDir := ""
	_ = workDir // reserved for future config.WorkDir

	// Register /reset command
	a.cmdRegistry.Register(command.NewResetExecutor(a.eng, workDir))

	// Register /dc command
	a.cmdRegistry.Register(command.NewDisconnectExecutor(a.eng))
}

// Stop waits for pending webhook goroutines to complete
func (a *Adapter) Stop() error {
	// Stop rate limiter cleanup goroutine
	if a.rateLimiter != nil {
		a.rateLimiter.Stop()
	}

	a.webhook.Stop()
	return a.Adapter.Stop()
}

// Start starts the adapter
func (a *Adapter) Start(ctx context.Context) error {
	// Start Socket Mode if enabled (preferred mode)
	if a.socketModeClient != nil {
		a.startSocketMode(ctx)
	}

	// Start HTTP server if needed (for HTTP mode or fallback)
	return a.Adapter.Start(ctx)
}

func (a *Adapter) sendStatusBubble(ctx context.Context, channelID, threadTS string, status base.StatusType, text string) error {
	// Build the message with blocks
	blocks := a.messageBuilder.BuildStatusBubble(status, text)
	if blocks == nil {
		// Fallback to plain text if builder returns nil
		return a.SendToChannelSDK(ctx, channelID, text, threadTS)
	}

	// Send as blocks
	_, err := a.sendBlocksSDK(ctx, channelID, blocks, threadTS, text)
	return err
}

// Compile-time interface compliance checks
var (
	_ base.ChatAdapter       = (*Adapter)(nil)
	_ base.EngineSupport     = (*Adapter)(nil)
	_ base.MessageOperations = (*Adapter)(nil)
	_ base.SessionOperations = (*Adapter)(nil)
	_ base.WebhookProvider   = (*Adapter)(nil)
)

// MessageOperations implementation for Slack

// DeleteMessage implements base.MessageOperations interface
func (a *Adapter) DeleteMessage(ctx context.Context, channelID, messageTS string) error {
	return a.DeleteMessageSDK(ctx, channelID, messageTS)
}

// UpdateMessage implements base.MessageOperations interface
func (a *Adapter) UpdateMessage(ctx context.Context, channelID, messageTS string, msg *base.ChatMessage) error {
	builder := NewMessageBuilder()
	blocks := builder.Build(msg)
	return a.UpdateMessageSDK(ctx, channelID, messageTS, blocks, msg.Content)
}

// Note: SessionOperations methods (GetSession, FindSessionByUserAndChannel)
// are inherited from base.Adapter and should not be overridden here
