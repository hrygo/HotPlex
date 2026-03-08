// Package slack provides a high-performance, AI-native Slack adapter for the HotPlex engine.
// It supports bot-mode (HTTP) and Socket Mode (WebSocket), providing Slack-specific
// UI components (Block Kit), Assistant Threads, and streaming message capabilities.
package slack

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/hrygo/hotplex/chatapps/base"
	"github.com/hrygo/hotplex/chatapps/command"
	"github.com/hrygo/hotplex/chatapps/session"
	"github.com/hrygo/hotplex/engine"
	"github.com/hrygo/hotplex/plugins/storage"
	"github.com/hrygo/hotplex/types"

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

	// Message storage plugin (optional)
	storePlugin *base.MessageStorePlugin

	// Session manager for consistent session ID generation
	sessionMgr session.SessionManager

	// Thread ownership tracker (Phase 1: Bot Behavior Spec)
	ownershipTracker *ThreadOwnershipTracker

	// Background cleanup for thread ownership
	ownershipCleanupCtx    context.Context
	ownershipCleanupCancel context.CancelFunc

	channelToTeam sync.Map // Map channelID to TeamID for streaming functions
	channelToUser sync.Map // Map channelID to UserID for streaming functions

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
	// Validate config - fail fast on invalid configuration
	if err := config.Validate(); err != nil {
		logger.Error("Invalid Slack config", "error", err)
		// Return minimal but valid adapter to prevent nil pointer panics
		return &Adapter{
			Adapter:     base.NewAdapter("slack", base.Config{}, logger),
			config:      config,
			webhook:     base.NewWebhookRunner(logger),
			sender:      base.NewSenderWithMutex(),
			rateLimiter: NewSlashCommandRateLimiterWithConfig(config.SlashCommandRateLimit, rateBurst),
			cmdRegistry: command.NewRegistry(),
		}
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
		messageBuilder:   NewMessageBuilder(config), // Converts base.ChatMessage to Slack blocks using official SDK
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

	// Initialize message storage plugin if enabled (default: false)
	storageEnabled := config.Storage != nil && BoolValue(config.Storage.Enabled, false)
	if storageEnabled {
		if err := a.initStoragePlugin(config.Storage, logger); err != nil {
			logger.Error("Failed to initialize storage plugin, continuing without persistence",
				"error", err)
		} else {
			storageType := "memory"
			if config.Storage != nil {
				storageType = config.Storage.Type
			}
			logger.Info("Message storage plugin initialized", "type", storageType)
		}
	}

	// Initialize thread ownership tracker if enabled (Phase 1: Bot Behavior Spec)
	if config.IsThreadOwnershipEnabled() {
		a.ownershipTracker = NewThreadOwnershipTracker(config.GetThreadOwnershipTTL(), logger)
		a.ownershipCleanupCtx, a.ownershipCleanupCancel = context.WithCancel(context.Background())
		persist := false
		if config.ThreadOwnership != nil && config.ThreadOwnership.Persist != nil {
			persist = *config.ThreadOwnership.Persist
		}
		logger.Info("Thread ownership tracker initialized",
			"ttl", config.GetThreadOwnershipTTL(),
			"persist", persist)
	}

	return a
}

// initStoragePlugin initializes the message storage plugin based on config.
//
// Supported Storage Types:
//   - "memory": In-memory storage (default, no persistence)
//   - "sqlite": SQLite file storage (requires CGO)
//   - "postgresql": PostgreSQL storage (requires PostgreSQLURL)
//
// Graceful Degradation:
//   - Unknown types fall back to "memory"
//   - Initialization failure is logged but doesn't prevent adapter startup
//   - storePlugin remains nil if initialization fails
func (a *Adapter) initStoragePlugin(cfg *StorageConfig, logger *slog.Logger) error {
	// Create storage backend using factory
	registry := storage.GlobalRegistry()
	pluginConfig := storage.PluginConfig{}

	storageType := "memory"
	if cfg != nil && cfg.Type != "" {
		storageType = cfg.Type
	}

	// Set storage-specific config
	switch storageType {
	case "sqlite":
		if cfg != nil && cfg.SQLitePath != "" {
			pluginConfig["path"] = cfg.SQLitePath
		} else {
			pluginConfig["path"] = "data/slack_messages.db"
		}
	case "postgresql":
		if cfg != nil && cfg.PostgreSQLURL != "" {
			pluginConfig["url"] = cfg.PostgreSQLURL
		} else {
			return fmt.Errorf("postgresql storage requires PostgreSQLURL config")
		}
	case "memory":
		// No additional config needed
	default:
		logger.Warn("Unknown storage type, falling back to memory", "type", storageType)
		storageType = "memory"
	}

	store, err := registry.Get(storageType, pluginConfig)
	if err != nil {
		return err
	}
	if store == nil {
		logger.Warn("Storage plugin not found, using memory", "type", storageType)
		store, _ = registry.Get("memory", nil)
	}

	// Initialize storage backend (creates tables if needed)
	if err := store.Initialize(context.Background()); err != nil {
		return err
	}

	// Create session manager for consistent session ID generation
	sessionMgr := session.NewSessionManager("hotplex")

	// Create message store plugin
	var streamEnabled *bool
	var streamTimeout time.Duration
	if cfg != nil {
		streamEnabled = cfg.StreamEnabled
		streamTimeout = cfg.StreamTimeout
	}

	pluginCfg := base.MessageStorePluginConfig{
		Store:          store,
		SessionManager: sessionMgr,
		StreamEnabled:  BoolValue(streamEnabled, false),
		StreamTimeout:  streamTimeout,
		Logger:         logger,
	}

	plugin, err := base.NewMessageStorePlugin(pluginCfg)
	if err != nil {
		return err
	}

	a.storePlugin = plugin
	a.sessionMgr = sessionMgr
	return nil
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
	// Stop thread ownership tracker first (waits for cleanup goroutine)
	if a.ownershipTracker != nil {
		a.ownershipTracker.Stop()
	}

	// Stop ownership cleanup context (legacy, kept for compatibility)
	if a.ownershipCleanupCancel != nil {
		a.ownershipCleanupCancel()
	}

	// Stop rate limiter cleanup goroutine
	if a.rateLimiter != nil {
		a.rateLimiter.Stop()
	}

	// Close storage plugin
	if a.storePlugin != nil {
		if err := a.storePlugin.Close(); err != nil {
			a.Logger().Error("Failed to close storage plugin", "error", err)
		}
	}

	// Stop webhook runner (may be nil if adapter init failed)
	if a.webhook != nil {
		a.webhook.Stop()
	}
	return a.Adapter.Stop()
}

// storeUserMessage stores a user message to the persistent storage if enabled.
//
// Session ID Design:
//   - SessionID is derived from (platform, botUserID, channelID, threadTS) - NOT userID
//   - This ensures all messages in a thread share the same session, regardless of sender
//   - ChatUserID in MessageContext is set to msg.UserID to identify the actual sender
//   - This enables: (1) thread-based history retrieval, (2) user-filtered queries
func (a *Adapter) storeUserMessage(ctx context.Context, msg *base.ChatMessage) {
	if a.storePlugin == nil {
		return
	}

	channelID, _ := msg.Metadata["channel_id"].(string)
	threadTS, _ := msg.Metadata["thread_ts"].(string)

	// Generate session context with empty userID - session is thread-based, not user-based
	sessionCtx := a.sessionMgr.CreateSessionContext("slack", "", a.config.BotUserID, channelID, threadTS, "claude")

	msgCtx, err := base.NewMessageContextBuilder().
		WithChatSession(sessionCtx.ChatSessionID, sessionCtx.ChatPlatform, msg.UserID,
			sessionCtx.ChatBotUserID, sessionCtx.ChatChannelID, sessionCtx.ChatThreadID).
		WithEngineSession(sessionCtx.EngineSessionID, sessionCtx.EngineNamespace).
		WithProviderSession(sessionCtx.ProviderSessionID, sessionCtx.ProviderType).
		WithMessage(types.MessageTypeUserInput, base.DirectionUserToBot, msg.Content).
		Build()

	if err != nil {
		a.Logger().Warn("Failed to build message context", "error", err)
		return
	}

	if err := a.storePlugin.OnUserMessage(ctx, msgCtx); err != nil {
		a.Logger().Warn("Failed to store user message", "error", err)
	}
}

// storeBotResponse stores a bot response to the persistent storage if enabled.
//
// Session ID Design (same as storeUserMessage):
//   - SessionID is derived from (platform, botUserID, channelID, threadTS) - NOT userID
//   - Empty ChatUserID in MessageContext indicates bot as sender
//   - This ensures bot responses are grouped with user messages in the same thread session
func (a *Adapter) storeBotResponse(ctx context.Context, _ string, channelID, threadTS, content string) {
	if a.storePlugin == nil {
		return
	}

	// Generate session context with empty userID for consistent session ID
	sessionCtx := a.sessionMgr.CreateSessionContext("slack", "", a.config.BotUserID, channelID, threadTS, "claude")

	msgCtx, err := base.NewMessageContextBuilder().
		WithChatSession(sessionCtx.ChatSessionID, "slack", "", sessionCtx.ChatBotUserID, channelID, threadTS).
		WithEngineSession(sessionCtx.EngineSessionID, sessionCtx.EngineNamespace).
		WithProviderSession(sessionCtx.ProviderSessionID, sessionCtx.ProviderType).
		WithMessage(types.MessageTypeFinalResponse, base.DirectionBotToUser, content).
		Build()

	if err != nil {
		a.Logger().Warn("Failed to build message context", "error", err)
		return
	}

	if err := a.storePlugin.OnBotResponse(ctx, msgCtx); err != nil {
		a.Logger().Warn("Failed to store bot response", "error", err)
	}
}

// GetThreadHistory retrieves message history for a thread session.
// Returns messages in chronological order (oldest first) for AI context building.
//
// Query Design:
//   - SessionID is generated with empty userID to match how messages are stored
//   - This retrieves ALL messages in the thread (both user and bot)
//   - Use GetThreadHistoryByUser for user-filtered queries
func (a *Adapter) GetThreadHistory(ctx context.Context, channelID, threadTS string, limit int) ([]*storage.ChatAppMessage, error) {
	if a.storePlugin == nil {
		return nil, fmt.Errorf("storage not enabled")
	}
	if a.sessionMgr == nil {
		return nil, fmt.Errorf("session manager not initialized")
	}

	// Generate session ID with empty userID - must match storeUserMessage/storeBotResponse
	sessionID := a.sessionMgr.GetChatSessionID("slack", "", a.config.BotUserID, channelID, threadTS)

	if limit <= 0 {
		limit = 100
	}
	query := &storage.MessageQuery{
		ChatSessionID: sessionID,
		Limit:         limit,
		Ascending:     true, // Oldest first for conversation context
	}

	messages, err := a.storePlugin.ListMessages(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list messages: %w", err)
	}

	return messages, nil
}

// formatMessagesAsString formats messages as a human-readable string
func formatMessagesAsString(messages []*storage.ChatAppMessage) string {
	if len(messages) == 0 {
		return ""
	}

	var sb strings.Builder
	for _, msg := range messages {
		timestamp := msg.CreatedAt.Format("2006-01-02 15:04:05")
		var role string
		if msg.MessageType == types.MessageTypeUserInput {
			role = "User"
		} else {
			role = "Assistant"
		}
		fmt.Fprintf(&sb, "[%s] %s: %s\n", timestamp, role, msg.Content)
	}

	return sb.String()
}

// GetThreadHistoryAsString returns thread history as a formatted string
// Useful for providing context to AI models
func (a *Adapter) GetThreadHistoryAsString(ctx context.Context, channelID, threadTS string, limit int) (string, error) {
	messages, err := a.GetThreadHistory(ctx, channelID, threadTS, limit)
	if err != nil {
		return "", err
	}
	return formatMessagesAsString(messages), nil
}

// GetThreadHistoryByUser retrieves message history filtered by a specific user ID.
//
// Use Case:
//   - Query messages from a specific user in a multi-user thread
//   - Database-level filtering for better performance than in-memory filtering
//
// Design:
//   - SessionID matches GetThreadHistory (thread-based, empty userID)
//   - ChatUserID filter is applied at database level for efficiency
func (a *Adapter) GetThreadHistoryByUser(ctx context.Context, channelID, threadTS, userID string, limit int) ([]*storage.ChatAppMessage, error) {
	if a.storePlugin == nil {
		return nil, fmt.Errorf("storage not enabled")
	}
	if a.sessionMgr == nil {
		return nil, fmt.Errorf("session manager not initialized")
	}

	// Generate session ID with empty userID for thread-level session
	sessionID := a.sessionMgr.GetChatSessionID("slack", "", a.config.BotUserID, channelID, threadTS)

	if limit <= 0 {
		limit = 100
	}
	query := &storage.MessageQuery{
		ChatSessionID: sessionID,
		ChatUserID:    userID, // Database-level filter by actual sender
		Limit:         limit,
		Ascending:     true,
	}

	messages, err := a.storePlugin.ListMessages(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list messages: %w", err)
	}

	return messages, nil
}

// GetThreadHistoryByUserAsString returns user-filtered thread history as a formatted string
func (a *Adapter) GetThreadHistoryByUserAsString(ctx context.Context, channelID, threadTS, userID string, limit int) (string, error) {
	messages, err := a.GetThreadHistoryByUser(ctx, channelID, threadTS, userID, limit)
	if err != nil {
		return "", err
	}
	return formatMessagesAsString(messages), nil
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
	builder := NewMessageBuilder(a.config)
	blocks := builder.Build(msg)
	return a.UpdateMessageSDK(ctx, channelID, messageTS, blocks, msg.Content)
}

// SendThreadReply implements base.MessageOperations interface (Space Folding)
// Sends a plain text message as a reply inside a thread
func (a *Adapter) SendThreadReply(ctx context.Context, channelID, threadTS, text string) error {
	return a.SendToChannelSDK(ctx, channelID, text, threadTS)
}

// Note: SessionOperations methods (GetSession, FindSessionByUserAndChannel)
// are inherited from base.Adapter and should not be overridden here

// --- Thread Ownership Methods (Phase 1: Bot Behavior Spec) ---

// GetOwnershipTracker returns the thread ownership tracker.
// Returns nil if thread ownership is not enabled.
func (a *Adapter) GetOwnershipTracker() *ThreadOwnershipTracker {
	return a.ownershipTracker
}

// ShouldRespondToMessage determines if this bot should respond to a message.
// This implements the unified decision flow from docs/design/bot-behavior-spec.md.
//
// Decision Flow:
//  1. DM → always treat as @mention (owner policy still applies)
//  2. @mentioned → check owner policy, claim ownership, respond
//  3. Own thread → update last active, respond
//  4. Not own thread → silent
func (a *Adapter) ShouldRespondToMessage(channelType, channelID, threadTS, text, userID string) (shouldRespond bool, reason string) {
	// Get or create thread key
	threadID := threadTS
	if threadID == "" {
		// Main channel message - will create new thread
		return a.shouldRespondToMainChannel(channelType, text, userID)
	}

	// Thread message
	return a.shouldRespondToThreadMessage(channelType, channelID, threadID, text, userID)
}

// shouldRespondToMainChannel handles decision for main channel messages (no thread).
func (a *Adapter) shouldRespondToMainChannel(channelType, text, userID string) (bool, string) {
	// DM handling
	if channelType == "dm" || channelType == "im" {
		if a.config.CanRespond(userID) {
			return true, "dm_as_mention"
		}
		return false, "dm_blocked_by_policy"
	}

	// Channel/Group handling
	mentioned := ExtractMentionedUsers(text)
	isBotMentioned := a.config.ContainsBotMention(text)

	if isBotMentioned {
		// Bot is @mentioned - check owner policy
		if a.config.CanRespond(userID) {
			return true, "mentioned_allowed"
		}
		return false, "mentioned_blocked_by_policy"
	}

	// Not mentioned - must stay silent in main channel
	if len(mentioned) > 0 {
		return false, "other_mentioned"
	}
	return false, "no_mention_silent"
}

// shouldRespondToThreadMessage handles decision for thread messages.
func (a *Adapter) shouldRespondToThreadMessage(channelType, channelID, threadTS, text, userID string) (bool, string) {
	// DM handling
	if channelType == "dm" || channelType == "im" {
		if a.config.CanRespond(userID) {
			return true, "dm_thread_as_mention"
		}
		return false, "dm_thread_blocked_by_policy"
	}

	// Check if bot is @mentioned
	isBotMentioned := a.config.ContainsBotMention(text)
	mentioned := ExtractMentionedUsers(text)
	threadKey := NewThreadKey(channelID, threadTS)

	if isBotMentioned {
		// Bot is @mentioned - check owner policy
		if !a.config.CanRespond(userID) {
			return false, "thread_mentioned_blocked_by_policy"
		}

		// R3/R4: This bot claims ownership when @mentioned
		// In multi-bot scenarios, each bot independently tracks its owned threads
		if a.ownershipTracker != nil {
			a.ownershipTracker.Claim(threadKey)
		}
		return true, "thread_mentioned_claimed"
	}

	// Not @mentioned - check thread ownership
	if a.ownershipTracker == nil {
		// No ownership tracking - stay silent
		return false, "no_ownership_tracking"
	}

	// R5: Ownership release - if others are @mentioned but not us, release ownership
	if len(mentioned) > 0 {
		if a.ownershipTracker.Owns(threadKey) {
			a.ownershipTracker.Release(threadKey)
			a.Logger().Debug("Thread ownership released (R5)", "thread_key", threadKey, "mentioned", mentioned)
		}
		return false, "ownership_released_other_mentioned"
	}

	// Check if we own this thread
	if a.ownershipTracker.Owns(threadKey) {
		a.ownershipTracker.UpdateLastActive(threadKey)
		return true, "thread_owner"
	}

	// We don't own this thread - stay silent
	return false, "not_owner_silent"
}

// ClaimThreadOwnership claims ownership of a thread after responding.
// This should be called after a successful response is sent.
func (a *Adapter) ClaimThreadOwnership(channelID, threadTS string) {
	if a.ownershipTracker == nil {
		return
	}
	threadKey := NewThreadKey(channelID, threadTS)
	a.ownershipTracker.Claim(threadKey)
}

// stripBotMention removes bot mention from text
func (a *Adapter) stripBotMention(text string) string {
	if a.config.BotUserID == "" {
		return text
	}
	mention := fmt.Sprintf("<@%s>", a.config.BotUserID)
	return strings.TrimSpace(strings.ReplaceAll(text, mention, ""))
}
