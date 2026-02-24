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
	"strings"
	"time"

	"github.com/hrygo/hotplex/chatapps/base"
)

type Adapter struct {
	*base.Adapter
	config          Config
	eventPath       string
	interactivePath string
	sender          *base.SenderWithMutex
	webhook         *base.WebhookRunner
	socketMode      *SocketModeConnection
}

func NewAdapter(config Config, logger *slog.Logger, opts ...base.AdapterOption) *Adapter {
	// Validate config
	if err := config.Validate(); err != nil {
		logger.Error("Invalid Slack config", "error", err)
	}

	a := &Adapter{
		config:          config,
		eventPath:       "/events",
		interactivePath: "/interactive",
		sender:          base.NewSenderWithMutex(),
		webhook:         base.NewWebhookRunner(logger),
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
	}

	handlers := make(map[string]http.HandlerFunc)

	// Register HTTP handlers - they work as fallback when Socket Mode fails
	// Slack recommends using both Socket Mode and HTTP webhook together
	handlers[a.eventPath] = a.handleEvent
	handlers[a.interactivePath] = a.handleInteractive

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

	// Skip bot messages
	if msgEvent.BotID != "" {
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

	sessionID := a.GetOrCreateSession(msgEvent.Channel+":"+msgEvent.User, msgEvent.User)

	msg := &base.ChatMessage{
		Platform:  "slack",
		SessionID: sessionID,
		UserID:    msgEvent.User,
		Content:   msgEvent.Text,
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

	a.webhook.Run(ctx, a.Handler(), msg)
}

// Stop waits for pending webhook goroutines to complete
func (a *Adapter) Stop() error {
	// Stop Socket Mode if active
	if a.socketMode != nil {
		_ = a.socketMode.Stop()
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
	a.Logger().Debug("Socket Mode event received", "type", eventType)

	var msgEvent MessageEvent
	if err := json.Unmarshal(data, &msgEvent); err != nil {
		a.Logger().Error("Parse socket mode message event failed", "error", err)
		return
	}

	// Skip bot messages (unless it's a message we should process)
	if msgEvent.BotID != "" {
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

	sessionID := a.GetOrCreateSession(msgEvent.Channel+":"+msgEvent.User, msgEvent.User)

	msg := &base.ChatMessage{
		Platform:  "slack",
		SessionID: sessionID,
		UserID:    msgEvent.User,
		Content:   msgEvent.Text,
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

	handler := a.Handler()
	if handler == nil {
		a.Logger().Error("Handler is nil, message will not be processed")
		return
	}
	a.Logger().Info("Forwarding message to handler", "sessionID", sessionID, "content", msg.Content, "subtype", msgEvent.SubType)
	a.webhook.Run(context.Background(), handler, msg)
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
	a.Logger().Debug("Interactive payload received", "body", string(body))

	w.WriteHeader(http.StatusOK)
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
