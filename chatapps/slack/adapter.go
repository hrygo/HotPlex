package slack

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hrygo/hotplex/chatapps/base"
	"github.com/hrygo/hotplex/engine"
	"github.com/hrygo/hotplex/internal/panicx"
	"github.com/hrygo/hotplex/telemetry"
)

type Adapter struct {
	*base.Adapter
	config              *Config
	eventPath           string
	interactivePath     string
	slashCommandPath    string
	sender              *base.SenderWithMutex
	webhook             *base.WebhookRunner
	socketMode          *SocketModeConnection
	slashCommandHandler func(cmd SlashCommand)
	eng                 *engine.Engine
	rateLimiter         *SlashCommandRateLimiter
}

func NewAdapter(config *Config, logger *slog.Logger, opts ...base.AdapterOption) *Adapter {
	// Validate config
	if err := config.Validate(); err != nil {
		logger.Error("Invalid Slack config", "error", err)
	}

	a := &Adapter{
		config:           config,
		eventPath:        "/events",
		interactivePath:  "/interactive",
		slashCommandPath: "/slack",
		sender:           base.NewSenderWithMutex(),
		webhook:          base.NewWebhookRunner(logger),
		rateLimiter:      NewSlashCommandRateLimiterWithConfig(config.SlashCommandRateLimit, rateBurst),
	}

	// Initialize Socket Mode if configured
	if config.IsSocketMode() {
		a.socketMode = NewSocketModeConnection(SocketModeConfig{
			AppToken: config.AppToken,
			BotToken: config.BotToken,
		}, logger)

		// Register message handlers
		// "message" handles DM and channel messages
		a.socketMode.RegisterHandler("message", a.handleSocketModeEvent)
		// "app_mention" handles @mentions in channels
		a.socketMode.RegisterHandler("app_mention", a.handleSocketModeEvent)
		// "slash_commands" handles slash commands (/reset, /dc, etc.)
		a.socketMode.RegisterHandler("slash_commands", a.handleSocketModeSlashCommand)
	}

	handlers := make(map[string]http.HandlerFunc)

	// Register HTTP handlers - they work as fallback when Socket Mode fails
	// Slack recommends using both Socket Mode and HTTP webhook together
	handlers[a.eventPath] = a.handleEvent
	handlers[a.interactivePath] = a.handleInteractive
	handlers[a.slashCommandPath] = a.handleSlashCommand

	// Build HTTP handler map
	for path, handler := range handlers {
		opts = append(opts, base.WithHTTPHandler(path, handler))
	}

	a.Adapter = base.NewAdapter("slack", base.Config{
		ServerAddr:   config.ServerAddr,
		SystemPrompt: config.SystemPrompt,
	}, logger, opts...)

	// Set default sender if BotToken is configured
	if config.BotToken != "" {
		a.sender.SetSender(a.defaultSender)
	}

	return a
}

// SetEngine sets the engine for the adapter (used for slash commands)
func (a *Adapter) SetEngine(eng *engine.Engine) {
	a.eng = eng
}

func (a *Adapter) SendMessage(ctx context.Context, sessionID string, msg *base.ChatMessage) error {
	return a.sender.SendMessage(ctx, sessionID, msg)
}

func (a *Adapter) SetSender(fn func(ctx context.Context, sessionID string, msg *base.ChatMessage) error) {
	a.sender.SetSender(fn)
}

// defaultSender sends message via Slack API
func (a *Adapter) defaultSender(ctx context.Context, sessionID string, msg *base.ChatMessage) error {
	if a.config.BotToken == "" {
		return fmt.Errorf("slack bot token not configured")
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
			if err := a.AddReaction(ctx, reaction); err != nil {
				a.Logger().Error("Failed to add reaction", "error", err, "reaction", reaction.Name)
			}
		}
	}

	// Send media/attachments if present
	if msg.RichContent != nil && len(msg.RichContent.Attachments) > 0 {
		for _, attachment := range msg.RichContent.Attachments {
			if err := a.SendAttachment(ctx, channelID, threadTS, attachment); err != nil {
				return fmt.Errorf("failed to send attachment: %w", err)
			}
		}
		// Send text content after attachments
		if msg.Content != "" {
			return a.SendToChannel(ctx, channelID, msg.Content, threadTS)
		}
		return nil
	}

	// Send Block Kit blocks if present
	if msg.RichContent != nil && len(msg.RichContent.Blocks) > 0 {
		// If we have message_ts, update existing message instead of creating new one
		if messageTS != "" {
			return a.UpdateMessage(ctx, channelID, messageTS, msg.RichContent.Blocks, msg.Content)
		}
		// Otherwise send new message and store ts in metadata
		ts, err := a.sendBlocksAndGetTS(ctx, channelID, msg.RichContent.Blocks, threadTS, msg.Content)
		if err != nil {
			return err
		}
		// Store ts in metadata for future updates
		if ts != "" && msg.Metadata != nil {
			msg.Metadata["message_ts"] = ts
		}
		return nil
	}

	return a.SendToChannel(ctx, channelID, msg.Content, threadTS)
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

// sendBlocksAndGetTS sends Block Kit blocks to Slack and returns the message timestamp
func (a *Adapter) sendBlocksAndGetTS(ctx context.Context, channelID string, blocks []any, threadTS, fallbackText string) (string, error) {
	payload := map[string]any{
		"channel": channelID,
		"text":    fallbackText,
		"blocks":  blocks,
	}
	if threadTS != "" {
		payload["thread_ts"] = threadTS
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://slack.com/api/chat.postMessage", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.config.BotToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		return "", fmt.Errorf("rate limited: 429")
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("send failed: %d %s", resp.StatusCode, string(respBody))
	}

	var slackResp struct {
		OK    bool   `json:"ok"`
		Error string `json:"error,omitempty"`
		TS    string `json:"ts,omitempty"`
	}
	if err := json.Unmarshal(respBody, &slackResp); err != nil {
		a.Logger().Warn("Failed to parse Slack response", "body", string(respBody))
		return "", nil
	}

	if !slackResp.OK {
		return "", fmt.Errorf("slack API error: %s", slackResp.Error)
	}

	a.Logger().Debug("Blocks sent successfully", "channel", channelID, "ts", slackResp.TS)
	return slackResp.TS, nil
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
		signature := r.Header.Get("X-Slack-Signature")
		timestamp := r.Header.Get("X-Slack-Request-Timestamp")
		if signature == "" || timestamp == "" || !a.verifySignature(body, timestamp, signature) {
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
	}

	// Special handling for #reset command in threads (HTTP webhook path)
	if processedText == "/reset" || strings.HasPrefix(processedText, "/reset ") {
		a.Logger().Info("Detected reset command in thread message (HTTP webhook)",
			"original", msgEvent.Text, "converted", processedText)
		cmd := SlashCommand{
			Command:     "/reset",
			UserID:      msgEvent.User,
			ChannelID:   msgEvent.Channel,
			ResponseURL: "",
		}
		panicx.SafeGo(a.Logger(), func() {
			if err := a.handleResetCommand(cmd); err != nil {
				a.Logger().Error("handleResetCommand failed", "error", err)
			}
		})
		return
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
	// Stop Socket Mode if active
	if a.socketMode != nil {
		_ = a.socketMode.Stop()
	}

	// Stop rate limiter cleanup goroutine
	if a.rateLimiter != nil {
		a.rateLimiter.Stop()
	}

	a.webhook.Stop()
	return a.Adapter.Stop()
}

// Start starts the adapter (overrides base.Adapter.Start to support Socket Mode)
func (a *Adapter) Start(ctx context.Context) error {
	// Start Socket Mode if configured
	if a.socketMode != nil {
		if err := a.socketMode.Start(ctx); err != nil {
			// Log error but continue - HTTP handlers are registered and will handle events
			a.Logger().Error("Socket Mode connection failed, using HTTP webhook", "error", err)
			a.socketMode = nil
		}
	}

	return a.Adapter.Start(ctx)
}

// handleSocketModeEvent handles incoming events from Socket Mode
func (a *Adapter) handleSocketModeEvent(eventType string, data json.RawMessage) {
	a.Logger().Debug("[SLACK_SOCKET_MODE] Socket Mode event received",
		"event_type", eventType,
		"data_len", len(data))

	var msgEvent MessageEvent
	if err := json.Unmarshal(data, &msgEvent); err != nil {
		a.Logger().Error("Parse socket mode message event failed", "error", err)
		return
	}

	// Skip bot messages (unless it's a message we should process)
	if msgEvent.BotID != "" || msgEvent.User == a.config.BotUserID {
		a.Logger().Debug("Skipping bot message", "bot_id", msgEvent.BotID)
		return
	}

	// Skip certain subtypes that don't need processing
	// Reference: OpenClaw allows file_share and bot_message, skips message_changed/deleted/thread_broadcast
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
		a.Logger().Debug("User blocked", "user_id", msgEvent.User)
		return
	}

	// Check channel permission
	if !a.config.ShouldProcessChannel(msgEvent.ChannelType, msgEvent.Channel) {
		a.Logger().Debug("Channel blocked by policy", "channel_type", msgEvent.ChannelType, "channel_id", msgEvent.Channel)
		return
	}

	// Check mention policy for group/channel messages
	if msgEvent.ChannelType == "channel" || msgEvent.ChannelType == "group" {
		if a.config.GroupPolicy == "mention" && !a.config.ContainsBotMention(msgEvent.Text) {
			a.Logger().Debug("Message ignored - bot not mentioned", "channel_type", msgEvent.ChannelType, "policy", "mention")
			return
		}
	}

	// Convert #<command> prefix to /<command> for thread support
	// Slack threads don't support slash commands, so we allow #reset, #dc, etc.
	processedText, conversionMetadata := preprocessMessageText(msgEvent.Text)
	if _, converted := conversionMetadata["converted_from_hash"]; converted {
		a.Logger().Debug("Converted # prefix to / prefix", "original", msgEvent.Text, "converted", processedText)
	}

	// Special handling for #reset command in threads (Socket Mode path)
	if processedText == "/reset" || strings.HasPrefix(processedText, "/reset ") {
		a.Logger().Info("Detected reset command in thread message (Socket Mode)",
			"original", msgEvent.Text, "converted", processedText)
		cmd := SlashCommand{
			Command:     "/reset",
			UserID:      msgEvent.User,
			ChannelID:   msgEvent.Channel,
			ResponseURL: "",
		}
		panicx.SafeGo(a.Logger(), func() {
			if err := a.handleResetCommand(cmd); err != nil {
				a.Logger().Error("handleResetCommand failed", "error", err)
			}
		})
		return
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

	handler := a.Handler()
	if handler == nil {
		a.Logger().Error("Handler is nil, message will not be processed")
		return
	}
	a.Logger().Info("Forwarding message to handler", "sessionID", sessionID, "content", msg.Content, "subtype", msgEvent.SubType)
	a.webhook.Run(context.Background(), handler, msg)
}

// handleSocketModeSlashCommand handles slash commands from Socket Mode
func (a *Adapter) handleSocketModeSlashCommand(eventType string, data json.RawMessage) {
	a.Logger().Debug("Socket Mode slash command received")

	// Parse slash command data from Socket Mode
	// Socket Mode sends slash commands as: {"type": "slash_commands", "command": "/reset", "user_id": "U123", "channel_id": "C123", ...}
	var slashData map[string]any
	if err := json.Unmarshal(data, &slashData); err != nil {
		a.Logger().Error("Parse socket mode slash command failed", "error", err)
		return
	}

	// Extract command fields
	cmd := SlashCommand{
		Command:     getStringField(slashData, "command"),
		Text:        getStringField(slashData, "text"),
		UserID:      getStringField(slashData, "user_id"),
		ChannelID:   getStringField(slashData, "channel_id"),
		ResponseURL: getStringField(slashData, "response_url"),
	}

	a.Logger().Debug("Socket Mode slash command parsed", "command", cmd.Command, "user", cmd.UserID)

	// Process the slash command (same as HTTP handler)
	go a.processSlashCommand(cmd)
}

// getStringField extracts a string field from a map[string]any
func getStringField(data map[string]any, key string) string {
	if v, ok := data[key].(string); ok {
		return v
	}
	return ""
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

	// Check if this is a permission request callback
	if action.ActionID == "perm_allow" || action.ActionID == "perm_deny" {
		a.handlePermissionCallback(callback, action, w)
		return
	}

	// Check if this is a plan mode callback
	if action.ActionID == "plan_approve" || action.ActionID == "plan_modify" || action.ActionID == "plan_cancel" {
		a.handlePlanModeCallback(callback, action, w)
		return
	}

	// Handle other block actions here
	a.Logger().Info("Unhandled block action",
		"action_id", action.ActionID,
		"value", action.Value,
	)

	w.WriteHeader(http.StatusOK)
}

// handlePermissionCallback handles permission approval/denial button clicks
func (a *Adapter) handlePermissionCallback(callback *SlackInteractionCallback, action SlackAction, w http.ResponseWriter) {
	userID := callback.User.ID
	channelID := callback.Channel.ID
	messageTS := callback.Message.Ts
	_ = messageTS // Reserved for future use
	value := action.Value

	a.Logger().Info("Permission callback received",
		"user_id", userID,
		"channel_id", channelID,
		"message_ts", messageTS,
		"value", value,
		"action_id", action.ActionID,
	)

	// Parse and validate value: "allow:sessionID:messageID" or "deny:sessionID:messageID"
	behavior, sessionID, messageID, err := ValidateButtonValue(value)
	if err != nil {
		a.Logger().Error("Invalid permission button value", "value", value, "error", err)
		w.WriteHeader(http.StatusOK)
		return
	}

	// Update the message to remove buttons and show result
	var blocks []map[string]any

	if behavior == "allow" {
		blocks = BuildPermissionApprovedBlocks("", "")
	} else {
		blocks = BuildPermissionDeniedBlocks("", "", "User denied permission")
	}

	// Update the Slack message
	if err := a.UpdateMessage(context.Background(), channelID, messageTS, interfaceSlice(blocks), ""); err != nil {
		a.Logger().Error("Update message failed", "error", err)
	}

	a.Logger().Info("Permission request processed",
		"behavior", behavior,
		"session_id", sessionID,
		"message_id", messageID,
	)

	w.WriteHeader(http.StatusOK)
}

// handlePlanModeCallback handles plan mode approval/denial button clicks
// TODO: Implement stdin response after confirming the response format via experiment
func (a *Adapter) handlePlanModeCallback(callback *SlackInteractionCallback, action SlackAction, w http.ResponseWriter) {
	userID := callback.User.ID
	channelID := callback.Channel.ID
	messageTS := callback.Message.Ts
	_ = messageTS
	value := action.Value

	a.Logger().Info("Plan mode callback received",
		"user_id", userID,
		"channel_id", channelID,
		"message_ts", messageTS,
		"value", value,
		"action_id", action.ActionID,
	)

	// Parse value: "approve:sessionID" or "modify:sessionID" or "cancel:sessionID"
	parts := strings.Split(value, ":")
	if len(parts) < 2 {
		a.Logger().Error("Invalid plan mode button value", "value", value)
		w.WriteHeader(http.StatusOK)
		return
	}

	actionType := parts[0]
	sessionID := parts[1]

	// Get block builder for creating response blocks
	blockBuilder := NewBlockBuilder()

	// Update the message to show result and send stdin response
	var blocks []map[string]any

	switch actionType {
	case "approve":
		blocks = blockBuilder.BuildPlanApprovedBlock()
		// TODO: Send stdin response to approve plan
		// Based on research, the format is likely similar to permission:
		// {"behavior": "allow"} - but needs experimental verification
		a.Logger().Info("Plan approved - stdin response format needs verification",
			"session_id", sessionID)

	case "modify":
		blocks = blockBuilder.BuildPlanCancelledBlock("User requested changes")
		// TODO: Open modal for user to specify changes
		a.Logger().Info("Plan modification requested - modal not implemented yet",
			"session_id", sessionID)

	case "cancel":
		blocks = blockBuilder.BuildPlanCancelledBlock("User cancelled")
		// TODO: Send stdin response to deny/cancel plan
		// {"behavior": "deny"} - but needs experimental verification
		a.Logger().Info("Plan cancelled - stdin response format needs verification",
			"session_id", sessionID)

	default:
		a.Logger().Error("Unknown plan mode action", "action", actionType)
		w.WriteHeader(http.StatusOK)
		return
	}

	// Update the Slack message
	if err := a.UpdateMessage(context.Background(), channelID, messageTS, interfaceSlice(blocks), ""); err != nil {
		a.Logger().Error("Update message failed", "error", err)
	}

	a.Logger().Info("Plan mode request processed",
		"action", actionType,
		"session_id", sessionID,
	)

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

// interfaceSlice converts []map[string]any to []any for Block Kit compatibility.
func interfaceSlice(blocks []map[string]any) []any {
	result := make([]any, len(blocks))
	for i, block := range blocks {
		result[i] = block
	}
	return result
}

func (a *Adapter) verifySignature(body []byte, timestamp, signature string) bool {
	parsedTS := strings.TrimPrefix(timestamp, "v0=")
	var ts int64
	if _, err := fmt.Sscanf(parsedTS, "%d", &ts); err != nil {
		a.Logger().Warn("Failed to parse timestamp", "timestamp", parsedTS)
		return false
	}

	now := time.Now().Unix()
	if now-ts > 60*5 {
		a.Logger().Warn("Timestamp too old")
		return false
	}

	baseString := fmt.Sprintf("v0:%s:%s", parsedTS, string(body))
	h := hmac.New(sha256.New, []byte(a.config.SigningSecret))
	h.Write([]byte(baseString))
	signatureComputed := "v0=" + hex.EncodeToString(h.Sum(nil))

	return hmac.Equal([]byte(signatureComputed), []byte(signature))
}

func (a *Adapter) SendToChannel(ctx context.Context, channelID, text, threadTS string) error {
	// Retry configuration for rate limiting
	retryConfig := RetryConfig{
		MaxAttempts: 3,
		BaseDelay:   500 * time.Millisecond,
		MaxDelay:    5 * time.Second,
	}

	return retryWithBackoff(ctx, retryConfig, func() error {
		return a.sendToChannelOnce(ctx, channelID, text, threadTS)
	})
}

// sendToChannelOnce sends a single message to Slack (without retry)
func (a *Adapter) sendToChannelOnce(ctx context.Context, channelID, text, threadTS string) error {
	payload := map[string]any{
		"channel": channelID,
		"text":    text,
	}

	// Add thread_ts if provided to reply in thread
	if threadTS != "" {
		payload["thread_ts"] = threadTS
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://slack.com/api/chat.postMessage", bytes.NewReader(body))
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

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	// Check for rate limit (429)
	if resp.StatusCode == http.StatusTooManyRequests {
		return fmt.Errorf("rate limited: 429")
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("send failed: %d %s", resp.StatusCode, string(respBody))
	}

	// Parse Slack API response to check "ok" field
	// Slack API may return HTTP 200 with {"ok": false, "error": "..."}
	var slackResp struct {
		OK    bool   `json:"ok"`
		Error string `json:"error,omitempty"`
	}
	if err := json.Unmarshal(respBody, &slackResp); err != nil {
		a.Logger().Warn("Failed to parse Slack response", "body", string(respBody))
		// Don't fail - message might have been sent
		return nil
	}

	if !slackResp.OK {
		return fmt.Errorf("slack API error: %s", slackResp.Error)
	}

	a.Logger().Debug("Message sent successfully", "channel", channelID)
	return nil
}

// AddReaction adds a reaction to a message
func (a *Adapter) AddReaction(ctx context.Context, reaction base.Reaction) error {
	if a.config.BotToken == "" {
		return fmt.Errorf("slack bot token not configured")
	}

	if reaction.Channel == "" || reaction.Timestamp == "" {
		return fmt.Errorf("channel and timestamp are required for reaction")
	}

	payload := map[string]any{
		"channel": reaction.Channel,
		"name":    reaction.Name,
		"ts":      reaction.Timestamp,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://slack.com/api/reactions.add", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.config.BotToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("reaction add failed: %d %s", resp.StatusCode, string(respBody))
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

	a.Logger().Debug("Reaction added", "emoji", reaction.Name, "channel", reaction.Channel)
	return nil
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
	switch cmd.Command {
	case "/reset":
		if err := a.handleResetCommand(cmd); err != nil {
			a.Logger().Error("handleResetCommand failed", "command", cmd.Command, "error", err)
		}
	case "/dc":
		if err := a.handleDisconnectCommand(cmd); err != nil {
			a.Logger().Error("handleDisconnectCommand failed", "command", cmd.Command, "error", err)
		}
	default:
		a.handleUnknownCommand(cmd)
	}
}

// handleResetCommand processes /reset command to perform a hard reset of conversation context.
//
// /reset performs a physical reset by:
// 1. Deleting the Claude Code project session file (~/.claude/projects/{workspace}/{ProviderSessionID}.jsonl)
// 2. Deleting the HotPlex session marker (~/.hotplex/sessions/{sessionID}.lock)
// 3. Terminating the session process
//
// Next message will cold-start with a fresh context.
func (a *Adapter) handleResetCommand(cmd SlashCommand) error {
	if a.eng == nil {
		a.Logger().Error("Engine is nil")
		return a.sendCommandResponse(cmd.ResponseURL, cmd.ChannelID, "❌ Internal error: Engine not initialized")
	}

	baseSession := a.FindSessionByUserAndChannel(cmd.UserID, cmd.ChannelID)
	if baseSession == nil {
		a.Logger().Error("No active session found for /reset",
			"channel_id", cmd.ChannelID, "user_id", cmd.UserID)
		return a.sendCommandResponse(cmd.ResponseURL, cmd.ChannelID, "ℹ️ No active session found")
	}

	sessionID := baseSession.SessionID
	a.Logger().Info("Found session for /reset",
		"session_id", sessionID, "channel_id", cmd.ChannelID, "user_id", cmd.UserID)

	// Get ProviderSessionID for Claude Code file deletion
	sess, exists := a.eng.GetSession(sessionID)
	if !exists {
		a.Logger().Warn("Session not found in engine, proceeding with cleanup",
			"session_id", sessionID)
	}

	// Step 1: Delete Claude Code project session file (using ProviderSessionID)
	providerSessionID := ""
	if sess != nil {
		providerSessionID = sess.ProviderSessionID
	}
	deletedCount := a.deleteClaudeCodeSessionFile(providerSessionID)
	a.Logger().Debug("Deleted Claude Code session files",
		"session_id", sessionID, "provider_session_id", providerSessionID, "count", deletedCount)

	// Step 2: Delete HotPlex session marker (use providerSessionID, not sessionID)
	// Marker files are named after providerSessionID
	markerDeleted := a.deleteHotPlexMarker(providerSessionID)

	// Step 3: Terminate the session process
	if err := a.eng.StopSession(sessionID, "user_requested_reset"); err != nil {
		a.Logger().Error("Failed to terminate session",
			"session_id", sessionID, "error", err)
		return a.sendCommandResponse(cmd.ResponseURL, cmd.ChannelID,
			fmt.Sprintf("⚠️ Session termination failed: %v", err))
	}

	a.Logger().Info("Physical cleanup for /reset completed",
		"session_id", sessionID,
		"claude_session_deleted", deletedCount > 0,
		"marker_deleted", markerDeleted)

	return a.sendCommandResponse(cmd.ResponseURL, cmd.ChannelID,
		"✅ Context reset. Ready for fresh start!")
}

// deleteClaudeCodeSessionFile renames (not deletes) the project session file for a given session.
// This preserves audit trail by adding .deleted suffix.
// The workspace directory name is derived from the working directory path.
func (a *Adapter) deleteClaudeCodeSessionFile(providerSessionID string) int {
	projectsDir := filepath.Join(os.Getenv("HOME"), ".claude", "projects")

	// Use current working directory as workspace key
	// Format: /Users/huangzhonghui/HotPlex -> -Users-huangzhonghui-HotPlex
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "/Users/huangzhonghui/HotPlex" // Default fallback
	}
	workspaceKey := strings.ReplaceAll(cwd, "/", "-")
	workspaceDir := filepath.Join(projectsDir, workspaceKey)

	if providerSessionID == "" {
		a.Logger().Debug("No providerSessionID, skipping Claude Code file cleanup")
		return 0
	}

	sessionFile := filepath.Join(workspaceDir, providerSessionID+".jsonl")
	deletedFile := sessionFile + ".deleted"

	// Rename instead of delete to preserve audit trail
	if err := os.Rename(sessionFile, deletedFile); err == nil {
		a.Logger().Info("Renamed Claude Code session file",
			"from", sessionFile,
			"to", deletedFile)
		return 1
	}
	if os.IsNotExist(err) {
		a.Logger().Debug("Claude Code session file not found (may not exist yet)",
			"path", sessionFile)
	} else {
		a.Logger().Warn("Failed to rename Claude Code session file",
			"path", sessionFile, "error", err)
	}
	return 0
}

// deleteHotPlexMarker deletes the HotPlex session marker file.
func (a *Adapter) deleteHotPlexMarker(sessionID string) bool {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	markerPath := filepath.Join(homeDir, ".hotplex", "sessions", sessionID+".lock")
	deletedPath := markerPath + ".deleted"

	// Rename instead of delete to preserve audit trail
	if err := os.Rename(markerPath, deletedPath); err == nil {
		a.Logger().Info("Renamed HotPlex marker file",
			"from", markerPath,
			"to", deletedPath)
		return true
	}
	if os.IsNotExist(err) {
		a.Logger().Debug("HotPlex marker file not found (may not exist yet)",
			"path", markerPath)
		return true // Not existing is also "deleted"
	}
	a.Logger().Warn("Failed to rename HotPlex marker file",
		"path", markerPath, "error", err)
	return false
}
func (a *Adapter) handleUnknownCommand(cmd SlashCommand) {
	a.Logger().Debug("Unknown slash command", "command", cmd.Command)
	// Silently ignore unknown commands - Slack may send other commands
}

// handleDisconnectCommand processes /dc command to disconnect from AI CLI
// This terminates the CLI process but preserves conversation context
func (a *Adapter) handleDisconnectCommand(cmd SlashCommand) error {
	// Check if engine is set
	if a.eng == nil {
		a.Logger().Error("Engine is nil")
		return a.sendEphemeralMessage(cmd.ResponseURL, "❌ Internal error: Engine not initialized")
	}
	// Find session by matching user_id and channel_id
	// New key format is "platform:user_id:bot_user_id:channel_id", so we need to search
	baseSession := a.FindSessionByUserAndChannel(cmd.UserID, cmd.ChannelID)
	if baseSession == nil {
		a.Logger().Error("No active session found for /dc",
			"channel_id", cmd.ChannelID,
			"user_id", cmd.UserID)
		return a.sendEphemeralMessage(cmd.ResponseURL, "ℹ️ No active session found")
	}

	sessionID := baseSession.SessionID
	a.Logger().Info("Found session for /dc",
		"session_id", sessionID,
		"channel_id", cmd.ChannelID,
		"user_id", cmd.UserID)

	// Get session from engine
	sess, exists := a.eng.GetSession(sessionID)
	if !exists {
		a.Logger().Error("Session disappeared after lookup", "session_id", sessionID)
		return a.sendEphemeralMessage(cmd.ResponseURL, "ℹ️ Session not found")
	}

	// Terminate the CLI process (but context is preserved in marker file)
	// Next message will resume with same context
	if err := a.eng.StopSession(sessionID, "user_requested_disconnect"); err != nil {
		a.Logger().Error("Failed to disconnect session", "session_id", sessionID, "error", err)
		return a.sendEphemeralMessage(cmd.ResponseURL,
			fmt.Sprintf("❌ Failed to disconnect: %v", err))
	}

	a.Logger().Info("Disconnected from CLI process",
		"session_id", sessionID,
		"provider_session_id", sess.ProviderSessionID)

	// Send success response
	return a.sendEphemeralMessage(cmd.ResponseURL,
		"🔌 Disconnected from CLI. Context preserved. Next message will resume.")
}

// UpdateMessage updates an existing Slack message using chat.update API
// Used for streaming AI responses with "typing indicator" UX
func (a *Adapter) UpdateMessage(ctx context.Context, channelID, messageTS string, blocks []any, fallbackText string) error {
	payload := map[string]any{
		"channel": channelID,
		"ts":      messageTS,
		"text":    fallbackText,
		"blocks":  blocks,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://slack.com/api/chat.update", bytes.NewReader(body))
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

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		return fmt.Errorf("rate limited: 429")
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("update failed: %d %s", resp.StatusCode, string(respBody))
	}

	var slackResp struct {
		OK    bool   `json:"ok"`
		Error string `json:"error,omitempty"`
		TS    string `json:"ts,omitempty"`
	}
	if err := json.Unmarshal(respBody, &slackResp); err != nil {
		a.Logger().Warn("Failed to parse Slack response", "body", string(respBody))
		return nil
	}

	if !slackResp.OK {
		return fmt.Errorf("slack API error: %s", slackResp.Error)
	}

	a.Logger().Debug("Message updated successfully", "channel", channelID, "ts", slackResp.TS)
	return nil
}

// DeleteMessage deletes a Slack message using chat.delete API
func (a *Adapter) DeleteMessage(ctx context.Context, channelID, messageTS string) error {
	payload := map[string]any{
		"channel": channelID,
		"ts":      messageTS,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://slack.com/api/chat.delete", bytes.NewReader(body))
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

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		return fmt.Errorf("rate limited: 429")
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("delete failed: %d %s", resp.StatusCode, string(respBody))
	}

	var slackResp struct {
		OK    bool   `json:"ok"`
		Error string `json:"error,omitempty"`
	}
	if err := json.Unmarshal(respBody, &slackResp); err != nil {
		a.Logger().Warn("Failed to parse Slack response", "body", string(respBody))
		return nil
	}

	if !slackResp.OK {
		return fmt.Errorf("slack API error: %s", slackResp.Error)
	}

	a.Logger().Debug("Message deleted successfully", "channel", channelID, "ts", messageTS)
	return nil
}

// SUPPORTED_COMMANDS lists all slash commands supported by the system.

// SUPPORTED_COMMANDS lists all slash commands supported by the system.
// Used for matching #<command> prefix in messages (thread support).
var SUPPORTED_COMMANDS = []string{"/reset", "/dc"}

// isSupportedCommand checks if a command (with / prefix) is in the supported commands list.
func isSupportedCommand(cmd string) bool {
	for _, supported := range SUPPORTED_COMMANDS {
		if supported == cmd {
			return true
		}
	}
	return false
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
	firstSpace := strings.Index(rest, " ")
	var potentialCmd string
	if firstSpace == -1 {
		potentialCmd = rest
	} else {
		potentialCmd = rest[:firstSpace]
	}

	// Add / prefix and check if supported
	cmdWithSlash := "/" + potentialCmd
	if isSupportedCommand(cmdWithSlash) {
		// Replace # with / in the original text
		return "/" + rest, true
	}

	return text, false
}

// preprocessMessageText handles #<command> to /<command> conversion and returns
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
