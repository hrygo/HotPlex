package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/hrygo/hotplex/chatapps/base"
	"github.com/hrygo/hotplex/chatapps/command"
	"github.com/hrygo/hotplex/engine"
	"github.com/hrygo/hotplex/event"
	"github.com/hrygo/hotplex/internal/panicx"
	"github.com/hrygo/hotplex/internal/telemetry"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

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

func (a *Adapter) SendMessage(ctx context.Context, sessionID string, msg *base.ChatMessage) error {
	return a.sender.SendMessage(ctx, sessionID, msg)
}

func (a *Adapter) SetSender(fn func(ctx context.Context, sessionID string, msg *base.ChatMessage) error) {
	a.sender.SetSender(fn)
}

// defaultSender sends message via Slack API using MessageBuilder
func (a *Adapter) defaultSender(ctx context.Context, sessionID string, msg *base.ChatMessage) error {
	if a.client == nil {
		return fmt.Errorf("slack client not initialized")
	}

	// Extract channel_id from session or message metadata
	channelID := a.extractChannelID(sessionID, msg)
	if channelID == "" {
		return fmt.Errorf("channel_id not found in session")
	}

	// Extract thread_ts from metadata if present
	threadTS := ""
	if msg.Metadata != nil {
		if ts, ok := msg.Metadata["thread_ts"].(string); ok {
			threadTS = ts
		}
	}

	// Check if this is a message update (has message_ts in metadata)
	var messageTS string
	if msg.Metadata != nil {
		if ts, ok := msg.Metadata["message_ts"].(string); ok {
			messageTS = ts
		}
	}

	// Send reactions if present
	if msg.RichContent != nil && len(msg.RichContent.Reactions) > 0 {
		for _, reaction := range msg.RichContent.Reactions {
			reaction.Channel = channelID
			if err := a.AddReactionSDK(ctx, reaction); err != nil {
				a.Logger().Error("Failed to add reaction", "error", err, "reaction", reaction.Name)
			}
		}
	}

	// Send media/attachments if present
	if msg.RichContent != nil && len(msg.RichContent.Attachments) > 0 {
		for _, attachment := range msg.RichContent.Attachments {
			if err := a.SendAttachmentSDK(ctx, channelID, threadTS, attachment); err != nil {
				return fmt.Errorf("failed to send attachment: %w", err)
			}
		}
		// Send text content after attachments
		if msg.Content != "" {
			return a.SendToChannelSDK(ctx, channelID, msg.Content, threadTS)
		}
		return nil
	}

	// Use MessageBuilder to convert base.ChatMessage to Slack blocks
	if a.messageBuilder != nil {
		blocks := a.messageBuilder.Build(msg)
		if len(blocks) > 0 {
			// Build fallback text for Slack API (required for notifications and accessibility)
			// Use msg.Content if available, otherwise generate from message type
			fallbackText := msg.Content
			if fallbackText == "" {
				// Generate fallback text from message type to avoid empty text
				switch msg.Type {
				case base.MessageTypeToolUse:
					fallbackText = "Using tool..."
				case base.MessageTypeToolResult:
					fallbackText = "Tool completed"
				case base.MessageTypeThinking:
					fallbackText = "Thinking..."
				case base.MessageTypeError:
					fallbackText = "Error occurred"
				default:
					fallbackText = "Message"
				}
			}

			// If we have message_ts, update existing message instead of creating new one
			if messageTS != "" {
				return a.UpdateMessageSDK(ctx, channelID, messageTS, blocks, fallbackText)
			}
			// Otherwise send new message and store ts in metadata
			ts, err := a.sendBlocksSDK(ctx, channelID, blocks, threadTS, fallbackText)
			if err != nil {
				return err
			}
			// Store ts in metadata for future updates
			if ts != "" && msg.Metadata != nil {
				msg.Metadata["message_ts"] = ts
			}
			return nil
		}
	}

	// Fallback: send plain text
	return a.SendToChannelSDK(ctx, channelID, msg.Content, threadTS)
}

// SendAttachment sends an attachment to a Slack channel
func (a *Adapter) SendAttachment(ctx context.Context, channelID, threadTS string, attachment base.Attachment) error {
	// Upload file to Slack using files.upload API
	// For external URLs, we can use the url parameter
	// For local files, we would need to read and upload

	payload := map[string]any{
		"channel": channelID,
	}

	// If there's a URL, use it directly
	if attachment.URL != "" {
		payload["url"] = attachment.URL
		payload["title"] = attachment.Title
		if threadTS != "" {
			payload["thread_ts"] = threadTS
		}
		return a.sendFileFromURL(ctx, payload)
	}

	// For now, just log that we received an attachment request
	a.Logger().Debug("Attachment received", "type", attachment.Type, "title", attachment.Title)
	return nil
}

// sendFileFromURL sends a file from URL to Slack
func (a *Adapter) sendFileFromURL(ctx context.Context, payload map[string]any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://slack.com/api/files.upload", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.config.BotToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("file upload failed: %d %s", resp.StatusCode, string(respBody))
	}

	var slackResp struct {
		OK    bool   `json:"ok"`
		Error string `json:"error,omitempty"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&slackResp); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}

	if !slackResp.OK {
		return fmt.Errorf("slack API error: %s", slackResp.Error)
	}

	return nil
}

// sendEphemeralMessage sends a message visible only to the user who issued the command
// via the Slack response_url (typically used in slash command responses)
func (a *Adapter) sendEphemeralMessage(responseURL, text string) error {
	payload := map[string]any{
		"response_type": "ephemeral",
		"text":          text,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		a.Logger().Error("Failed to marshal ephemeral message", "error", err)
		return err
	}

	resp, err := http.Post(responseURL, "application/json", bytes.NewReader(body))
	if err != nil {
		a.Logger().Error("Failed to send ephemeral message", "error", err)
		return err
	}
	defer func() {
		_ = resp.Body.Close() // Ignore close error on response body
	}()

	return nil
}

// sendCommandResponse sends a response to a command, using response_url if available,
// or falling back to sending directly to the channel.
// This is used when commands can be triggered from both slash commands (with response_url)
// and thread messages (without response_url).
func (a *Adapter) sendCommandResponse(responseURL, channelID, text string) error {
	// If response_url is available, use it for ephemeral message
	if responseURL != "" {
		return a.sendEphemeralMessage(responseURL, text)
	}

	// Fallback: send directly to channel
	if channelID == "" {
		return fmt.Errorf("cannot send response: both response_url and channel_id are empty")
	}

	a.Logger().Debug("No response_url, sending to channel directly", "channel_id", channelID)
	return a.SendToChannel(context.Background(), channelID, text, "")
}

// extractChannelID extracts channel_id from session or message metadata
func (a *Adapter) extractChannelID(_ string, msg *base.ChatMessage) string {
	if msg.Metadata == nil {
		return ""
	}
	if channelID, ok := msg.Metadata["channel_id"].(string); ok {
		return channelID
	}
	return ""
}

type Event struct {
	Token     string          `json:"token"`
	TeamID    string          `json:"team_id"`
	APIAppID  string          `json:"api_app_id"`
	Type      string          `json:"type"`
	EventID   string          `json:"event_id"`
	EventTime int64           `json:"event_time"`
	Event     json.RawMessage `json:"event"`
	Challenge string          `json:"challenge"`
}

type MessageEvent struct {
	Type        string `json:"type"`
	Channel     string `json:"channel"`
	ChannelType string `json:"channel_type"`
	User        string `json:"user"`
	Text        string `json:"text"`
	TS          string `json:"ts"`
	EventTS     string `json:"event_ts"`
	BotID       string `json:"bot_id,omitempty"`
	SubType     string `json:"subtype,omitempty"`
	ThreadTS    string `json:"thread_ts,omitempty"`      // Thread identifier
	ParentUser  string `json:"parent_user_id,omitempty"` // Parent message user
	BotUserID   string `json:"bot_user_id,omitempty"`    // Bot user ID for mentions
}

func (a *Adapter) handleEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		a.Logger().Error("Read body failed", "error", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	if a.config.SigningSecret != "" {
		if !a.verifySignature(r, body) {
			a.Logger().Warn("Invalid signature")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
	}

	var event Event
	if err := json.Unmarshal(body, &event); err != nil {
		a.Logger().Error("Parse event failed", "error", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	if event.Challenge != "" {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(event.Challenge))
		return
	}

	if event.Token != a.config.BotToken && event.Token != a.config.AppToken {
		a.Logger().Warn("Invalid token")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if event.Type == "event_callback" {
		a.handleEventCallback(r.Context(), event.Event)
	}

	w.WriteHeader(http.StatusOK)
}

func (a *Adapter) handleEventCallback(ctx context.Context, eventData json.RawMessage) {
	var msgEvent MessageEvent
	if err := json.Unmarshal(eventData, &msgEvent); err != nil {
		a.Logger().Error("Parse message event failed", "error", err)
		return
	}

	// Structured logging for Slack HTTP webhook message
	a.Logger().Debug("[SLACK_HTTP_WEBHOOK] HTTP webhook event received",
		"event_type", msgEvent.Type,
		"channel", msgEvent.Channel,
		"channel_type", msgEvent.ChannelType,
		"user", msgEvent.User,
		"text", msgEvent.Text,
		"ts", msgEvent.TS,
		"thread_ts", msgEvent.ThreadTS,
		"subtype", msgEvent.SubType)

	// Skip bot messages
	if msgEvent.BotID != "" || msgEvent.User == a.config.BotUserID {
		a.Logger().Debug("Skipping bot message", "bot_id", msgEvent.BotID)
		return
	}

	// Skip certain subtypes that don't need processing
	switch msgEvent.SubType {
	case "message_changed", "message_deleted", "thread_broadcast":
		a.Logger().Debug("Skipping message subtype", "subtype", msgEvent.SubType)
		return
	}

	if msgEvent.Text == "" {
		return
	}

	// Check user permission
	if !a.config.IsUserAllowed(msgEvent.User) {
		telemetry.GetMetrics().IncSlackPermissionBlockedUser()
		a.Logger().Debug("User blocked", "user_id", msgEvent.User)
		return
	}
	telemetry.GetMetrics().IncSlackPermissionAllowed()

	// Check channel permission
	if !a.config.ShouldProcessChannel(msgEvent.ChannelType, msgEvent.Channel) {
		if msgEvent.ChannelType == "dm" {
			telemetry.GetMetrics().IncSlackPermissionBlockedDM()
		}
		a.Logger().Debug("Channel blocked by policy", "channel_type", msgEvent.ChannelType, "channel_id", msgEvent.Channel)
		return
	}

	// Check mention policy for group/channel messages
	if msgEvent.ChannelType == "channel" || msgEvent.ChannelType == "group" {
		if a.config.GroupPolicy == "mention" && !a.config.ContainsBotMention(msgEvent.Text) {
			telemetry.GetMetrics().IncSlackPermissionBlockedMention()
			a.Logger().Debug("Message ignored - bot not mentioned", "channel_type", msgEvent.ChannelType, "policy", "mention")
			return
		}
	}

	// Convert #<command> prefix to /<command> for thread support
	// Slack threads don't support slash commands, so we allow #reset, #dc, etc.
	processedText, conversionMetadata := preprocessMessageText(msgEvent.Text)
	if _, converted := conversionMetadata["converted_from_hash"]; converted {
		a.Logger().Debug("Converted # prefix to / prefix", "original", msgEvent.Text, "converted", processedText)

		// Check if converted command should be executed immediately
		if a.processHashCommand(processedText, msgEvent.User, msgEvent.Channel) {
			return
		}
	}

	sessionID := a.GetOrCreateSession(msgEvent.User, msgEvent.BotUserID, msgEvent.Channel)

	msg := &base.ChatMessage{
		Platform:  "slack",
		SessionID: sessionID,
		UserID:    msgEvent.User,
		Content:   processedText,
		MessageID: msgEvent.TS,
		Timestamp: time.Now(),
		Metadata: map[string]any{
			"channel_id":   msgEvent.Channel,
			"channel_type": msgEvent.ChannelType,
			"message_ts":   msgEvent.TS, // Required for reaction feedback
		},
	}

	// Add thread info if present
	if msgEvent.ThreadTS != "" {
		msg.Metadata["thread_ts"] = msgEvent.ThreadTS
	}

	// Add subtype info for downstream processing
	if msgEvent.SubType != "" {
		msg.Metadata["subtype"] = msgEvent.SubType
	}

	// Merge conversion metadata
	for k, v := range conversionMetadata {
		msg.Metadata[k] = v
	}

	a.webhook.Run(ctx, a.Handler(), msg)
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

// startSocketMode starts the Socket Mode event loop
func (a *Adapter) startSocketMode(ctx context.Context) {
	a.socketModeMu.Lock()
	if a.socketModeRunning {
		a.socketModeMu.Unlock()
		a.Logger().Warn("Socket Mode already running")
		return
	}
	a.socketModeRunning = true
	a.socketModeCtx, a.socketModeCancel = context.WithCancel(ctx)
	a.socketModeMu.Unlock()

	go func() {
		defer panicx.Recover(a.Logger(), "Slack Socket Mode")
		defer func() {
			a.socketModeMu.Lock()
			a.socketModeRunning = false
			a.socketModeMu.Unlock()
		}()

		for {
			select {
			case <-a.socketModeCtx.Done():
				a.Logger().Info("Socket Mode event loop stopped")
				return
			case evt, ok := <-a.socketModeClient.Events:
				if !ok {
					a.Logger().Info("Socket Mode events channel closed")
					return
				}
				a.handleSocketModeEvent(evt)
			}
		}
	}()

	// Run Socket Mode client
	go func() {
		defer panicx.Recover(a.Logger(), "Slack Socket Mode Run")
		if err := a.socketModeClient.RunContext(a.socketModeCtx); err != nil {
			a.Logger().Error("Socket Mode client error", "error", err)
		}
	}()

	a.Logger().Info("Socket Mode started")
}

// handleSocketModeEvent dispatches Socket Mode events
func (a *Adapter) handleSocketModeEvent(evt socketmode.Event) {
	switch evt.Type {
	case socketmode.EventTypeHello:
		a.Logger().Info("Socket Mode connected")

	case socketmode.EventTypeConnecting:
		a.Logger().Info("Connecting to Slack with Socket Mode...")

	case socketmode.EventTypeConnected:
		a.Logger().Info("Connected to Slack with Socket Mode")

	case socketmode.EventTypeConnectionError:
		a.Logger().Error("Socket Mode connection error")

	case socketmode.EventTypeEventsAPI:
		a.handleSocketModeEventsAPI(evt)

	case socketmode.EventTypeSlashCommand:
		a.handleSocketModeSlashCommand(evt)

	case socketmode.EventTypeInteractive:
		a.handleSocketModeInteractive(evt)

	default:
		a.Logger().Debug("Unhandled Socket Mode event", "type", evt.Type)
	}
}

// handleSocketModeEventsAPI handles Events API events from Socket Mode
func (a *Adapter) handleSocketModeEventsAPI(evt socketmode.Event) {
	eventsAPIEvent, ok := evt.Data.(slackevents.EventsAPIEvent)
	if !ok {
		a.Logger().Error("Failed to cast EventsAPI event")
		return
	}

	// Acknowledge the event
	a.socketModeClient.Ack(*evt.Request)

	switch eventsAPIEvent.Type {
	case slackevents.CallbackEvent:
		innerEvent := eventsAPIEvent.InnerEvent
		switch ev := innerEvent.Data.(type) {
		case *slackevents.AppMentionEvent:
			a.handleAppMentionEvent(ev)
		case *slackevents.MessageEvent:
			a.handleSocketModeMessageEvent(ev)
		}
	default:
		a.Logger().Debug("Unhandled EventsAPI type", "type", eventsAPIEvent.Type)
	}
}

// handleAppMentionEvent handles app_mention events
func (a *Adapter) handleAppMentionEvent(ev *slackevents.AppMentionEvent) {
	a.Logger().Debug("App mention received", "user", ev.User, "channel", ev.Channel)

	if !a.config.IsUserAllowed(ev.User) {
		a.Logger().Debug("User blocked", "user_id", ev.User)
		return
	}

	// Generate session and process message
	sessionID := a.GetOrCreateSession(ev.User, a.config.BotUserID, ev.Channel)
	userText := a.stripBotMention(ev.Text)

	msg := &base.ChatMessage{
		Platform:  "slack",
		SessionID: sessionID,
		UserID:    ev.User,
		Content:   userText,
		MessageID: ev.TimeStamp,
		Timestamp: time.Now(),
		Metadata: map[string]any{
			"channel_id":   ev.Channel,
			"channel_type": "channel", // App mentions are always in channels
			"message_ts":   ev.TimeStamp,
			"thread_ts":    ev.ThreadTimeStamp, // May be empty if not in thread
		},
	}

	// Send to handler via webhook.Run (not directly to Slack API)
	a.webhook.Run(a.socketModeCtx, a.Handler(), msg)
}

// handleSocketModeMessageEvent handles message events via Socket Mode
func (a *Adapter) handleSocketModeMessageEvent(ev *slackevents.MessageEvent) {
	// Skip bot messages
	if ev.BotID != "" || ev.User == a.config.BotUserID {
		return
	}

	// Skip subtypes
	switch ev.SubType {
	case "message_changed", "message_deleted", "thread_broadcast", "bot_message":
		return
	}

	if ev.Text == "" {
		return
	}

	if !a.config.IsUserAllowed(ev.User) {
		a.Logger().Debug("User blocked", "user_id", ev.User)
		return
	}

	if !a.config.ShouldProcessChannel(ev.ChannelType, ev.Channel) {
		a.Logger().Debug("Channel blocked by policy", "channel_type", ev.ChannelType)
		return
	}

	// Convert #<command> prefix to /<command> for thread support
	// Slack threads don't support slash commands, so we allow #reset, #dc, etc.
	processedText, conversionMetadata := preprocessMessageText(ev.Text)
	if _, converted := conversionMetadata["converted_from_hash"]; converted {
		a.Logger().Debug("Converted # prefix to / prefix (Socket Mode)",
			"original", ev.Text, "converted", processedText)

		// Check if converted command should be executed immediately
		if a.processHashCommand(processedText, ev.User, ev.Channel) {
			return
		}
	}

	sessionID := a.GetOrCreateSession(ev.User, a.config.BotUserID, ev.Channel)
	userText := processedText

	msg := &base.ChatMessage{
		Platform:  "slack",
		SessionID: sessionID,
		UserID:    ev.User,
		Content:   userText,
		MessageID: ev.TimeStamp,
		Timestamp: time.Now(),
		Metadata: map[string]any{
			"channel_id":   ev.Channel,
			"channel_type": ev.ChannelType,
			"message_ts":   ev.TimeStamp, // Required for reaction feedback
		},
	}

	// Add thread info if present
	if ev.ThreadTimeStamp != "" {
		msg.Metadata["thread_ts"] = ev.ThreadTimeStamp
	}

	// Merge conversion metadata
	for k, v := range conversionMetadata {
		msg.Metadata[k] = v
	}

	if ev.ThreadTimeStamp != "" {
		msg.Metadata["thread_ts"] = ev.ThreadTimeStamp
	}

	// Send to handler via webhook.Run (not directly to Slack API)
	a.webhook.Run(a.socketModeCtx, a.Handler(), msg)
}

// handleSocketModeSlashCommand handles slash commands via Socket Mode
func (a *Adapter) handleSocketModeSlashCommand(evt socketmode.Event) {
	cmd, ok := evt.Data.(slack.SlashCommand)
	if !ok {
		a.Logger().Error("Failed to cast SlashCommand")
		return
	}

	a.Logger().Debug("Slash command via Socket Mode", "command", cmd.Command, "text", cmd.Text)

	// Check rate limit
	if !a.rateLimiter.Allow(cmd.UserID) {
		a.Logger().Warn("Rate limit exceeded", "user_id", cmd.UserID)
		a.socketModeClient.Ack(*evt.Request, map[string]interface{}{
			"text": "⚠️ Rate limit exceeded. Please wait a moment.",
		})
		return
	}

	// Acknowledge with loading message
	a.socketModeClient.Ack(*evt.Request, map[string]interface{}{
		"text": "Processing command...",
	})

	// Find session for command execution
	baseSession := a.FindSessionByUserAndChannel(cmd.UserID, cmd.ChannelID)
	var sessionID string
	if baseSession != nil {
		sessionID = baseSession.SessionID
	}

	// Create command request
	req := &command.Request{
		Command:     cmd.Command,
		Text:        cmd.Text,
		UserID:      cmd.UserID,
		ChannelID:   cmd.ChannelID,
		SessionID:   sessionID,
		ResponseURL: cmd.ResponseURL,
	}

	// Create callback for progress events
	var progressTS string
	callback := func(eventType string, data any) error {
		return a.handleCommandProgress(cmd.ChannelID, &progressTS, eventType, data)
	}

	// Execute command via registry
	result, err := a.cmdRegistry.Execute(context.Background(), req, callback)
	if err != nil {
		a.Logger().Error("Command execution failed", "command", cmd.Command, "error", err)
	} else if result != nil && result.Message != "" {
		_ = a.sendCommandResponse(cmd.ResponseURL, cmd.ChannelID, result.Message)
	}
}

// handleSocketModeInteractive handles interactive events via Socket Mode
func (a *Adapter) handleSocketModeInteractive(evt socketmode.Event) {
	callback, ok := evt.Data.(slack.InteractionCallback)
	if !ok {
		a.Logger().Error("Failed to cast InteractionCallback")
		return
	}

	a.Logger().Debug("Interactive via Socket Mode", "type", callback.Type)

	// Acknowledge the event
	a.socketModeClient.Ack(*evt.Request)

	switch callback.Type {
	case slack.InteractionTypeBlockActions:
		// Handle permission request buttons
		for _, action := range callback.ActionCallback.BlockActions {
			if action.ActionID == "perm_allow" || action.ActionID == "perm_deny" {
				// Build a simple callback for permission handling
				slackCallback := &SlackInteractionCallback{
					Type:    "block_actions",
					User:    CallbackUser{ID: callback.User.ID},
					Channel: CallbackChannel{ID: callback.Channel.ID},
					Message: CallbackMessage{Ts: callback.Message.Timestamp},
					Actions: []SlackAction{
						{
							ActionID: action.ActionID,
							BlockID:  action.BlockID,
							Value:    action.Value,
						},
					},
				}
				a.handlePermissionCallback(slackCallback, slackCallback.Actions[0], nil)
				return
			}
		}
	default:
		a.Logger().Debug("Unhandled interaction type", "type", callback.Type)
	}
}

// stripBotMention removes bot mention from text
func (a *Adapter) stripBotMention(text string) string {
	if a.config.BotUserID == "" {
		return text
	}
	mention := fmt.Sprintf("<@%s>", a.config.BotUserID)
	return strings.TrimSpace(strings.ReplaceAll(text, mention, ""))
}

func (a *Adapter) handleInteractive(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		a.Logger().Error("Read body failed", "error", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	defer func() { _ = r.Body.Close() }()

	// Parse the payload
	payload := r.FormValue("payload")
	if payload == "" {
		// Try to parse as JSON directly
		payload = string(body)
	}

	var callback SlackInteractionCallback
	if err := json.Unmarshal([]byte(payload), &callback); err != nil {
		a.Logger().Error("Parse callback failed", "error", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// Validate actions array
	if len(callback.Actions) == 0 {
		a.Logger().Warn("No actions in callback")
		w.WriteHeader(http.StatusOK)
		return
	}

	a.Logger().Debug("Interaction callback parsed",
		"type", callback.Type,
		"user", callback.User.ID,
		"channel", callback.Channel.ID,
		"action_id", callback.Actions[0].ActionID,
		"block_id", callback.Actions[0].BlockID,
		"value", callback.Actions[0].Value,
	)

	// Handle based on interaction type
	switch callback.Type {
	case "block_actions":
		a.handleBlockActions(&callback, w)
	default:
		a.Logger().Warn("Unknown interaction type", "type", callback.Type)
		w.WriteHeader(http.StatusOK)
	}
}

// handleBlockActions handles Slack block_actions callbacks (button clicks, etc.)
func (a *Adapter) handleBlockActions(callback *SlackInteractionCallback, w http.ResponseWriter) {
	action := callback.Actions[0]
	userID := callback.User.ID
	channelID := callback.Channel.ID
	messageTS := callback.Message.Ts
	_ = messageTS // Reserved for future use

	a.Logger().Debug("Block action received",
		"action_id", action.ActionID,
		"block_id", action.BlockID,
		"value", action.Value,
		"user_id", userID,
		"channel_id", channelID,
	)

	actionID := action.ActionID

	// Check if this is a permission request callback
	// Format: perm_allow:{sessionID}:{messageID} or perm_deny:{sessionID}:{messageID}
	if strings.HasPrefix(actionID, "perm_allow:") || strings.HasPrefix(actionID, "perm_deny:") {
		a.handlePermissionCallback(callback, action, w)
		return
	}

	// Check if this is a plan mode callback
	// Format: plan_approve, plan_modify, plan_cancel
	if actionID == "plan_approve" || actionID == "plan_modify" || actionID == "plan_cancel" {
		a.handlePlanModeCallback(callback, action, w)
		return
	}

	// Check if this is a danger block callback
	// Format: danger_confirm:{sessionID} or danger_cancel:{sessionID}
	if strings.HasPrefix(actionID, "danger_confirm") || strings.HasPrefix(actionID, "danger_cancel") {
		a.handleDangerBlockCallback(callback, action, w)
		return
	}

	// Check if this is an ask user question callback
	// Format: question_option_{i}
	if strings.HasPrefix(actionID, "question_option_") {
		a.handleAskUserQuestionCallback(callback, action, w)
		return
	}

	// Handle other block actions here
	a.Logger().Info("Unhandled block action",
		"action_id", actionID,
		"value", action.Value,
	)

	w.WriteHeader(http.StatusOK)
}

// handlePermissionCallback handles permission approval/denial button clicks
// ActionID format: perm_allow:{sessionID}:{messageID} or perm_deny:{sessionID}:{messageID}
// Value format: "allow" or "deny"
func (a *Adapter) handlePermissionCallback(callback *SlackInteractionCallback, action SlackAction, w http.ResponseWriter) {
	userID := callback.User.ID
	channelID := callback.Channel.ID
	messageTS := callback.Message.Ts
	actionID := action.ActionID

	a.Logger().Info("Permission callback received",
		"user_id", userID,
		"channel_id", channelID,
		"message_ts", messageTS,
		"action_id", actionID,
	)

	// Parse actionID: perm_allow:{sessionID}:{messageID} or perm_deny:{sessionID}:{messageID}
	parts := strings.Split(actionID, ":")
	if len(parts) < 3 {
		a.Logger().Error("Invalid permission action_id format", "action_id", actionID)
		w.WriteHeader(http.StatusOK)
		return
	}

	behavior := parts[0] // "perm_allow" or "perm_deny"
	sessionID := parts[1]
	messageID := parts[2]

	// Map behavior to actual permission response
	var permissionBehavior string
	if strings.HasSuffix(behavior, "allow") {
		permissionBehavior = "allow"
	} else {
		permissionBehavior = "deny"
	}

	// Send permission response to engine via stdin
	if a.eng != nil {
		if sess, ok := a.eng.GetSession(sessionID); ok {
			response := map[string]any{
				"type":       "permission_response",
				"message_id": messageID,
				"behavior":   permissionBehavior,
			}
			if err := sess.WriteInput(response); err != nil {
				a.Logger().Error("Failed to send permission response to engine", "error", err)
			} else {
				a.Logger().Info("Sent permission response to engine",
					"session_id", sessionID,
					"behavior", permissionBehavior)
			}
		} else {
			a.Logger().Warn("Session not found for permission response", "session_id", sessionID)
		}
	}

	// Use MessageBuilder for creating response blocks
	var slackBlocks []slack.Block
	if permissionBehavior == "allow" {
		slackBlocks = a.messageBuilder.BuildPermissionApprovedMessage("", "")
	} else {
		slackBlocks = a.messageBuilder.BuildPermissionDeniedMessage("", "", "User denied permission")
	}

	// Update the Slack message using SDK
	if err := a.UpdateMessageSDK(context.Background(), channelID, messageTS, slackBlocks, ""); err != nil {
		a.Logger().Error("Update message failed", "error", err)
	}

	a.Logger().Info("Permission request processed",
		"behavior", permissionBehavior,
		"session_id", sessionID,
		"message_id", messageID,
	)

	w.WriteHeader(http.StatusOK)
}

// handlePlanModeCallback handles plan mode approval/denial button clicks
// Value format: approve:{sessionID} or deny:{sessionID}
func (a *Adapter) handlePlanModeCallback(callback *SlackInteractionCallback, action SlackAction, w http.ResponseWriter) {
	userID := callback.User.ID
	channelID := callback.Channel.ID
	messageTS := callback.Message.Ts
	value := action.Value

	a.Logger().Info("Plan mode callback received",
		"user_id", userID,
		"channel_id", channelID,
		"message_ts", messageTS,
		"value", value,
		"action_id", action.ActionID,
	)

	// Parse value: "approve:{sessionID}" or "deny:{sessionID}"
	parts := strings.Split(value, ":")
	if len(parts) < 2 {
		a.Logger().Error("Invalid plan mode button value", "value", value)
		w.WriteHeader(http.StatusOK)
		return
	}

	actionType := parts[0]
	sessionID := parts[1]

	// Determine behavior for engine response
	var behavior string
	switch actionType {
	case "approve":
		behavior = "allow"
	case "deny", "cancel":
		behavior = "deny"
	case "modify":
		behavior = "deny" // Modify means deny and request changes
	default:
		behavior = "deny"
	}

	// Send plan mode response to engine via stdin
	if a.eng != nil {
		if sess, ok := a.eng.GetSession(sessionID); ok {
			response := map[string]any{
				"type":     "plan_response",
				"behavior": behavior,
			}
			if err := sess.WriteInput(response); err != nil {
				a.Logger().Error("Failed to send plan response to engine", "error", err)
			} else {
				a.Logger().Info("Sent plan response to engine",
					"session_id", sessionID,
					"behavior", behavior)
			}
		} else {
			a.Logger().Warn("Session not found for plan response", "session_id", sessionID)
		}
	}

	// Use MessageBuilder for creating response blocks
	var slackBlocks []slack.Block
	switch actionType {
	case "approve":
		slackBlocks = a.messageBuilder.BuildPlanApprovedBlock()
	case "modify":
		slackBlocks = a.messageBuilder.BuildPlanCancelledBlock("User requested changes")
	case "deny", "cancel":
		slackBlocks = a.messageBuilder.BuildPlanCancelledBlock("User cancelled")
	}

	// Update the Slack message
	if err := a.UpdateMessageSDK(context.Background(), channelID, messageTS, slackBlocks, ""); err != nil {
		a.Logger().Error("Update message failed", "error", err)
	}

	a.Logger().Info("Plan mode request processed",
		"action", actionType,
		"session_id", sessionID,
	)

	w.WriteHeader(http.StatusOK)
}

// handleDangerBlockCallback handles danger block confirmation button clicks
// Value format: confirm:{sessionID} or cancel:{sessionID}
func (a *Adapter) handleDangerBlockCallback(callback *SlackInteractionCallback, action SlackAction, w http.ResponseWriter) {
	userID := callback.User.ID
	channelID := callback.Channel.ID
	messageTS := callback.Message.Ts
	actionID := action.ActionID
	value := action.Value

	a.Logger().Info("Danger block callback received",
		"user_id", userID,
		"channel_id", channelID,
		"message_ts", messageTS,
		"action_id", actionID,
		"value", value,
	)

	// Parse value: confirm:{sessionID} or cancel:{sessionID}
	parts := strings.Split(value, ":")
	if len(parts) < 2 {
		a.Logger().Error("Invalid danger button value", "value", value)
		w.WriteHeader(http.StatusOK)
		return
	}

	actionType := parts[0] // "confirm" or "cancel"
	sessionID := parts[1]

	// Map behavior to actual response
	var permissionBehavior string
	if actionType == "confirm" {
		permissionBehavior = "allow"
	} else {
		permissionBehavior = "deny"
	}

	// Send response to engine via stdin
	if a.eng != nil {
		if sess, ok := a.eng.GetSession(sessionID); ok {
			response := map[string]any{
				"type":     "danger_response",
				"behavior": permissionBehavior,
			}
			if err := sess.WriteInput(response); err != nil {
				a.Logger().Error("Failed to send danger response to engine", "error", err)
			} else {
				a.Logger().Info("Sent danger response to engine",
					"session_id", sessionID,
					"behavior", permissionBehavior)
			}
		} else {
			a.Logger().Warn("Session not found for danger response", "session_id", sessionID)
		}
	}

	// Update the Slack message to show the action taken
	statusText := ":white_check_mark: Confirmed"
	if permissionBehavior == "deny" {
		statusText = ":x: Cancelled"
	}
	statusObj := slack.NewTextBlockObject("mrkdwn", statusText, false, false)
	slackBlocks := []slack.Block{slack.NewSectionBlock(statusObj, nil, nil)}

	if err := a.UpdateMessageSDK(context.Background(), channelID, messageTS, slackBlocks, ""); err != nil {
		a.Logger().Error("Update message failed", "error", err)
	}

	w.WriteHeader(http.StatusOK)
}

// handleAskUserQuestionCallback handles ask user question option selection
// ActionID format: question_option_{i}
func (a *Adapter) handleAskUserQuestionCallback(callback *SlackInteractionCallback, action SlackAction, w http.ResponseWriter) {
	userID := callback.User.ID
	channelID := callback.Channel.ID
	messageTS := callback.Message.Ts
	actionID := action.ActionID
	value := action.Value

	a.Logger().Info("Ask user question callback received",
		"user_id", userID,
		"channel_id", channelID,
		"message_ts", messageTS,
		"action_id", actionID,
		"value", value,
	)

	// Parse actionID: question_option_{i}
	// The value contains the selected option index or text
	selectedOption := value
	if selectedOption == "" {
		// Try to extract from actionID
		if opt, found := strings.CutPrefix(actionID, "question_option_"); found {
			selectedOption = opt
		}
	}

	// Send response to engine via stdin
	// The sessionID should be stored in the message metadata or derived from channel/user
	baseSession := a.FindSessionByUserAndChannel(userID, channelID)
	if baseSession == nil {
		a.Logger().Warn("No active session found for question response",
			"user_id", userID,
			"channel_id", channelID)
	} else if a.eng != nil {
		if sess, ok := a.eng.GetSession(baseSession.SessionID); ok {
			response := map[string]any{
				"type":    "question_response",
				"option":  selectedOption,
				"user_id": userID,
			}
			if err := sess.WriteInput(response); err != nil {
				a.Logger().Error("Failed to send question response to engine", "error", err)
			} else {
				a.Logger().Info("Sent question response to engine",
					"session_id", baseSession.SessionID,
					"option", selectedOption)
			}
		}
	}

	// Update the Slack message to show the selection
	statusText := fmt.Sprintf(":white_check_mark: Selected: %s", selectedOption)
	statusObj := slack.NewTextBlockObject("mrkdwn", statusText, false, false)
	slackBlocks := []slack.Block{slack.NewSectionBlock(statusObj, nil, nil)}

	if err := a.UpdateMessageSDK(context.Background(), channelID, messageTS, slackBlocks, ""); err != nil {
		a.Logger().Error("Update message failed", "error", err)
	}

	w.WriteHeader(http.StatusOK)
}

// SlackInteractionCallback represents a Slack interaction callback payload.
type SlackInteractionCallback struct {
	Type        string          `json:"type"`
	User        CallbackUser    `json:"user"`
	Channel     CallbackChannel `json:"channel"`
	Message     CallbackMessage `json:"message"`
	ResponseURL string          `json:"response_url"`
	TriggerID   string          `json:"trigger_id"`
	Actions     []SlackAction   `json:"actions"`
	Team        CallbackTeam    `json:"team"`
}

// CallbackUser represents the user in a Slack callback.
type CallbackUser struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

// CallbackChannel represents the channel in a Slack callback.
type CallbackChannel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// CallbackMessage represents the message in a Slack callback.
type CallbackMessage struct {
	Ts   string `json:"ts"`
	Type string `json:"type"`
	Text string `json:"text"`
}

// CallbackTeam represents the team in a Slack callback.
type CallbackTeam struct {
	ID     string `json:"id"`
	Domain string `json:"domain"`
}

// SlackAction represents an action within a Slack interaction callback.
type SlackAction struct {
	ActionID string `json:"action_id"`
	BlockID  string `json:"block_id"`
	ID       string `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Value    string `json:"value"`
	Style    string `json:"style"`
}

// verifySignature verifies the request signature using Slack SDK's SecretsVerifier
func (a *Adapter) verifySignature(r *http.Request, body []byte) bool {
	signature := r.Header.Get("X-Slack-Signature")
	timestamp := r.Header.Get("X-Slack-Request-Timestamp")

	if signature == "" || timestamp == "" {
		return false
	}

	header := http.Header{
		"X-Slack-Signature":         []string{signature},
		"X-Slack-Request-Timestamp": []string{timestamp},
	}

	sv, err := slack.NewSecretsVerifier(header, a.config.SigningSecret)
	if err != nil {
		a.Logger().Warn("Failed to create SecretsVerifier", "error", err)
		return false
	}

	if _, err := sv.Write(body); err != nil {
		a.Logger().Warn("Failed to write to SecretsVerifier", "error", err)
		return false
	}

	if err := sv.Ensure(); err != nil {
		a.Logger().Warn("Signature verification failed", "error", err)
		return false
	}

	return true
}

func (a *Adapter) SendToChannel(ctx context.Context, channelID, text, threadTS string) error {
	// Use SDK implementation with retry
	return a.SendToChannelSDK(ctx, channelID, text, threadTS)
}

// AddReaction adds a reaction to a message
func (a *Adapter) AddReaction(ctx context.Context, reaction base.Reaction) error {
	// Use SDK implementation
	return a.AddReactionSDK(ctx, reaction)
}

// SlashCommand represents a Slack slash command
type SlashCommand struct {
	Command     string
	Text        string
	UserID      string
	ChannelID   string
	ResponseURL string
}

// SetSlashCommandHandler sets the handler for slash commands
func (a *Adapter) SetSlashCommandHandler(fn func(cmd SlashCommand)) {
	a.slashCommandHandler = fn
}

// handleSlashCommand processes incoming slash commands
func (a *Adapter) handleSlashCommand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseForm(); err != nil {
		a.Logger().Error("Parse slash command form failed", "error", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	cmd := SlashCommand{
		Command:     r.FormValue("command"),
		Text:        r.FormValue("text"),
		UserID:      r.FormValue("user_id"),
		ChannelID:   r.FormValue("channel_id"),
		ResponseURL: r.FormValue("response_url"),
	}

	a.Logger().Debug("Slash command received",
		"command", cmd.Command,
		"text", cmd.Text,
		"user", cmd.UserID)

	// Check rate limit before processing
	if !a.rateLimiter.Allow(cmd.UserID) {
		a.Logger().Warn("Rate limit exceeded", "user_id", cmd.UserID)
		_ = a.sendEphemeralMessage(cmd.ResponseURL, "⚠️ Rate limit exceeded. Please wait a moment.")
		return
	}
	// Acknowledge immediately (Slack requires response within 3 seconds)
	w.WriteHeader(http.StatusOK)

	// Process command in background
	go a.processSlashCommand(cmd)
}

// processSlashCommand handles the slash command logic
func (a *Adapter) processSlashCommand(cmd SlashCommand) {
	// Find session for command execution
	baseSession := a.FindSessionByUserAndChannel(cmd.UserID, cmd.ChannelID)
	var sessionID string
	if baseSession != nil {
		sessionID = baseSession.SessionID
	}

	// Create command request
	req := &command.Request{
		Command:     cmd.Command,
		Text:        cmd.Text,
		UserID:      cmd.UserID,
		ChannelID:   cmd.ChannelID,
		SessionID:   sessionID,
		ResponseURL: cmd.ResponseURL,
	}

	// Create callback for progress events
	var progressTS string
	callback := func(eventType string, data any) error {
		return a.handleCommandProgress(cmd.ChannelID, &progressTS, eventType, data)
	}

	// Execute command via registry
	result, err := a.cmdRegistry.Execute(context.Background(), req, callback)
	if err != nil {
		a.Logger().Error("Command execution failed", "command", cmd.Command, "error", err)
		_ = a.sendCommandResponse(cmd.ResponseURL, cmd.ChannelID, "Command execution failed: "+err.Error())
		return
	}

	// Send response
	if result != nil && result.Message != "" {
		_ = a.sendCommandResponse(cmd.ResponseURL, cmd.ChannelID, result.Message)
	}
}

// =============================================================================
// Command Constants
// =============================================================================

const (
	// CommandReset represents the /reset command
	CommandReset = "/reset"
	// CommandDisconnect represents the /dc command
	CommandDisconnect = "/dc"
)

// =============================================================================
// Supported Commands List
// =============================================================================

// SUPPORTED_COMMANDS lists all slash commands supported by the system.
// Used for matching #<command> prefix in messages (thread support).
var SUPPORTED_COMMANDS = []string{CommandReset, CommandDisconnect}

// isSupportedCommand checks if a command (with / prefix) is in the supported commands list.
func isSupportedCommand(cmd string) bool {
	return slices.Contains(SUPPORTED_COMMANDS, cmd)
}

// convertHashPrefixToSlash checks if the message starts with #<command>
// and converts it to /<command> if the command is supported.
// Returns the converted text and true if conversion happened,
// otherwise returns original text and false.
func convertHashPrefixToSlash(text string) (string, bool) {
	if !strings.HasPrefix(text, "#") {
		return text, false
	}

	// Extract potential command: #reset ... -> /reset ...
	// Find first space or use entire remaining text
	rest := text[1:] // Remove # prefix
	if rest == "" {
		return text, false
	}

	// Find command boundary (first space or end)
	potentialCmd, _, _ := strings.Cut(rest, " ")

	// Add / prefix and check if supported
	cmdWithSlash := "/" + potentialCmd
	if isSupportedCommand(cmdWithSlash) {
		// Replace # with / in the original text
		return "/" + rest, true
	}

	return text, false
}

// processHashCommand executes a command that was converted from #command to /command
// Returns true if a command was processed, false otherwise
func (a *Adapter) processHashCommand(cmd string, userID, channelID string) bool {
	// Check if it's a supported command
	if !isSupportedCommand(cmd) {
		return false
	}

	a.Logger().Info("Executing converted command", "command", cmd, "user_id", userID, "channel_id", channelID)

	// Find session for command execution
	baseSession := a.FindSessionByUserAndChannel(userID, channelID)
	var sessionID string
	if baseSession != nil {
		sessionID = baseSession.SessionID
	}

	// Create command request
	req := &command.Request{
		Command:     cmd,
		UserID:      userID,
		ChannelID:   channelID,
		SessionID:   sessionID,
		ResponseURL: "",
	}

	// Create callback for progress events
	var progressTS string
	callback := func(eventType string, data any) error {
		return a.handleCommandProgress(channelID, &progressTS, eventType, data)
	}

	// Execute command via registry (async)
	panicx.SafeGo(a.Logger(), func() {
		result, err := a.cmdRegistry.Execute(context.Background(), req, callback)
		if err != nil {
			a.Logger().Error("Command execution failed", "command", cmd, "error", err)
			return
		}
		// Send response if needed
		if result != nil && result.Message != "" {
			_ = a.sendCommandResponse("", channelID, result.Message)
		}
	})

	return true
}

// handleCommandProgress handles progress events from command execution
func (a *Adapter) handleCommandProgress(channelID string, progressTS *string, eventType string, data any) error {
	// Build progress message using MessageBuilder
	msg := &base.ChatMessage{
		Type:      base.MessageTypeCommandProgress,
		Content:   fmt.Sprintf("%v", data),
		Metadata:  map[string]any{"event_type": eventType},
		Timestamp: time.Now(),
	}

	// Check if data is EventWithMeta for richer info
	if ewm, ok := data.(*event.EventWithMeta); ok {
		msg.Content = ewm.EventData
		if ewm.Meta != nil {
			msg.Metadata["progress"] = ewm.Meta.Progress
			msg.Metadata["total_steps"] = ewm.Meta.TotalSteps
			msg.Metadata["current_step"] = ewm.Meta.CurrentStep
		}
	}

	blocks := a.messageBuilder.Build(msg)
	if len(blocks) == 0 {
		a.Logger().Debug("No blocks generated for command progress", "event_type", eventType)
		return nil
	}

	// Update existing message or post new one
	if *progressTS != "" {
		if err := a.UpdateMessageSDK(context.Background(), channelID, *progressTS, blocks, "Command progress"); err != nil {
			a.Logger().Debug("Failed to update progress message", "error", err, "ts", *progressTS)
			return err
		}
		return nil
	}

	ts, err := a.sendBlocksSDK(context.Background(), channelID, blocks, "", "Command progress")
	if err != nil {
		a.Logger().Debug("Failed to send progress message", "error", err)
		return err
	}
	*progressTS = ts
	return nil
}

// the processed text along with metadata additions for the message.
// Returns the processed text and a metadata map.
func preprocessMessageText(originalText string) (string, map[string]any) {
	metadata := make(map[string]any)
	processed, converted := convertHashPrefixToSlash(originalText)
	if converted {
		metadata["converted_from_hash"] = true
		metadata["original_text"] = originalText
	}
	return processed, metadata
}

// =============================================================================
// Slack SDK Methods - Using github.com/slack-go/slack
// =============================================================================

// SendToChannelSDK sends a text message using Slack SDK
func (a *Adapter) SendToChannelSDK(ctx context.Context, channelID, text, threadTS string) error {
	if a.client == nil {
		return fmt.Errorf("slack client not initialized")
	}

	opts := []slack.MsgOption{
		slack.MsgOptionText(text, false),
	}

	if threadTS != "" {
		opts = append(opts, slack.MsgOptionTS(threadTS))
	}

	_, _, err := a.client.PostMessageContext(ctx, channelID, opts...)
	if err != nil {
		return fmt.Errorf("post message: %w", err)
	}

	a.Logger().Debug("Message sent via SDK", "channel", channelID)
	return nil
}

// sendBlocksSDK sends blocks using Slack SDK and returns message timestamp
func (a *Adapter) sendBlocksSDK(ctx context.Context, channelID string, blocks []slack.Block, threadTS, fallbackText string) (string, error) {
	if a.client == nil {
		return "", fmt.Errorf("slack client not initialized")
	}

	opts := []slack.MsgOption{
		slack.MsgOptionBlocks(blocks...),
		slack.MsgOptionText(fallbackText, false),
	}

	if threadTS != "" {
		opts = append(opts, slack.MsgOptionTS(threadTS))
	}

	channel, ts, err := a.client.PostMessageContext(ctx, channelID, opts...)
	if err != nil {
		return "", fmt.Errorf("post blocks: %w", err)
	}

	a.Logger().Debug("Blocks sent via SDK", "channel", channel, "ts", ts)
	return ts, nil
}

// UpdateMessageSDK updates an existing message using Slack SDK
func (a *Adapter) UpdateMessageSDK(ctx context.Context, channelID, messageTS string, blocks []slack.Block, fallbackText string) error {
	if a.client == nil {
		return fmt.Errorf("slack client not initialized")
	}

	_, _, _, err := a.client.UpdateMessageContext(ctx, channelID, messageTS,
		slack.MsgOptionBlocks(blocks...),
		slack.MsgOptionText(fallbackText, false),
	)
	if err != nil {
		return fmt.Errorf("update message: %w", err)
	}

	a.Logger().Debug("Message updated via SDK", "channel", channelID, "ts", messageTS)
	return nil
}

// AddReactionSDK adds a reaction using Slack SDK
func (a *Adapter) AddReactionSDK(ctx context.Context, reaction base.Reaction) error {
	if a.client == nil {
		return fmt.Errorf("slack client not initialized")
	}

	if reaction.Channel == "" || reaction.Timestamp == "" {
		return fmt.Errorf("channel and timestamp are required for reaction")
	}

	err := a.client.AddReactionContext(ctx,
		reaction.Name,
		slack.ItemRef{
			Channel:   reaction.Channel,
			Timestamp: reaction.Timestamp,
		},
	)
	if err != nil {
		return fmt.Errorf("add reaction: %w", err)
	}

	a.Logger().Debug("Reaction added via SDK", "channel", reaction.Channel, "ts", reaction.Timestamp)
	return nil
}

// RemoveReactionSDK removes a reaction using Slack SDK
func (a *Adapter) RemoveReactionSDK(ctx context.Context, reaction base.Reaction) error {
	if a.client == nil {
		return fmt.Errorf("slack client not initialized")
	}

	if reaction.Channel == "" || reaction.Timestamp == "" {
		return fmt.Errorf("channel and timestamp are required for reaction")
	}

	err := a.client.RemoveReactionContext(ctx,
		reaction.Name,
		slack.ItemRef{
			Channel:   reaction.Channel,
			Timestamp: reaction.Timestamp,
		},
	)
	if err != nil {
		return fmt.Errorf("remove reaction: %w", err)
	}

	a.Logger().Debug("Reaction removed via SDK", "channel", reaction.Channel, "ts", reaction.Timestamp)
	return nil
}

// =============================================================================
// Typing Indicator (0.1 Slack UX Feature)
// Note: Slack's typing indicator is not directly supported by the slack-go SDK.
// As an alternative, we use reactions to provide visual feedback.
// Per spec section 0.1, the typing indicator shows next to bot name when processing.
// Per spec section 0.2, reactions provide lightweight feedback.
// =============================================================================

// PostTypingIndicator sends a visual indicator that the bot is processing
// Per spec: Triggered when user message received, during processing
// Note: Uses ephemeral context message as typing indicator alternative
func (a *Adapter) PostTypingIndicator(ctx context.Context, channelID, threadTS string) error {
	if a.client == nil {
		return fmt.Errorf("slack client not initialized")
	}
	if channelID == "" {
		return fmt.Errorf("channel_id is required for typing indicator")
	}

	// Since Slack's typing indicator API is not directly available,
	// we skip this and rely on reactions + status messages instead.
	// The spec suggests using :brain: reaction or context block for thinking state.
	a.Logger().Debug("Typing indicator requested (using reactions/status instead)", "channel", channelID)
	return nil
}

// SendTypingIndicatorForSession sends typing indicator for a session
// Uses session to resolve channel ID
func (a *Adapter) SendTypingIndicatorForSession(ctx context.Context, sessionID string) error {
	// Get session from base adapter
	session, ok := a.GetSession(sessionID)
	if !ok || session == nil {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	// For typing indicator, we need channel_id which is stored in session metadata
	// Since base.Session doesn't have Metadata, we return nil (no-op)
	// Typing indicator is optional UX enhancement
	a.Logger().Debug("Typing indicator for session (no-op)", "session_id", sessionID)
	return nil
}

// SendAttachmentSDK sends an attachment using Slack SDK
// Note: Simplified implementation - uses existing custom method
func (a *Adapter) SendAttachmentSDK(ctx context.Context, channelID, threadTS string, attachment base.Attachment) error {
	// Fallback to existing implementation
	return a.SendAttachment(ctx, channelID, threadTS, attachment)
}

// DeleteMessageSDK deletes a message using Slack SDK
func (a *Adapter) DeleteMessageSDK(ctx context.Context, channelID, messageTS string) error {
	if a.client == nil {
		return fmt.Errorf("slack client not initialized")
	}

	_, _, err := a.client.DeleteMessageContext(ctx, channelID, messageTS)
	if err != nil {
		return fmt.Errorf("delete message: %w", err)
	}

	a.Logger().Debug("Message deleted via SDK", "channel", channelID, "ts", messageTS)
	return nil
}

// PostEphemeralSDK posts an ephemeral message using Slack SDK
func (a *Adapter) PostEphemeralSDK(ctx context.Context, channelID, userID, text string, blocks []slack.Block) error {
	if a.client == nil {
		return fmt.Errorf("slack client not initialized")
	}

	opts := []slack.MsgOption{
		slack.MsgOptionText(text, false),
	}
	if len(blocks) > 0 {
		opts = append(opts, slack.MsgOptionBlocks(blocks...))
	}

	_, err := a.client.PostEphemeralContext(ctx, channelID, userID, opts...)
	if err != nil {
		return fmt.Errorf("post ephemeral: %w", err)
	}

	a.Logger().Debug("Ephemeral message sent via SDK", "channel", channelID, "user", userID)
	return nil
}

// Compile-time interface compliance checks
var (
	_ base.ChatAdapter       = (*Adapter)(nil)
	_ base.MessageOperations = (*Adapter)(nil)
	_ base.SessionOperations = (*Adapter)(nil)
)

// MessageOperations implementation for Slack

// DeleteMessage implements base.MessageOperations interface
func (a *Adapter) DeleteMessage(ctx context.Context, channelID, messageTS string) error {
	return a.DeleteMessageSDK(ctx, channelID, messageTS)
}

// RemoveReaction implements base.MessageOperations interface
func (a *Adapter) RemoveReaction(ctx context.Context, reaction base.Reaction) error {
	return a.RemoveReactionSDK(ctx, reaction)
}

// UpdateMessage implements base.MessageOperations interface
func (a *Adapter) UpdateMessage(ctx context.Context, channelID, messageTS string, msg *base.ChatMessage) error {
	builder := NewMessageBuilder()
	blocks := builder.Build(msg)
	return a.UpdateMessageSDK(ctx, channelID, messageTS, blocks, msg.Content)
}

// Note: SessionOperations methods (GetSession, FindSessionByUserAndChannel)
// are inherited from base.Adapter and should not be overridden here
