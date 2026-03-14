package slack

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/hrygo/hotplex/chatapps/base"
	"github.com/hrygo/hotplex/chatapps/slack/apphome"
	"github.com/hrygo/hotplex/internal/telemetry"
	"github.com/slack-go/slack"
)

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
	if !base.CheckMethodPOST(w, r) {
		return
	}

	body, ok := base.ReadBodyWithLog(w, r, a.Logger())
	if !ok {
		return
	}

	if a.config.SigningSecret != "" && BoolValue(a.config.VerifySignature, true) {
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
		a.handleEventCallback(r.Context(), event.TeamID, event.Event)
	}

	w.WriteHeader(http.StatusOK)
}

func (a *Adapter) handleEventCallback(ctx context.Context, teamID string, eventData json.RawMessage) {
	// First, extract event type to route to appropriate handler
	var eventType struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(eventData, &eventType); err != nil {
		a.Logger().Error("Parse event type failed", "error", err)
		return
	}

	// Handle app_home_opened event
	if eventType.Type == "app_home_opened" {
		a.handleAppHomeOpened(ctx, eventData)
		return
	}

	// For all other events, parse as message event
	var msgEvent MessageEvent
	if err := json.Unmarshal(eventData, &msgEvent); err != nil {
		a.Logger().Error("Parse message event failed", "error", err)
		return
	}

	if msgEvent.Channel != "" {
		if teamID != "" {
			a.channelToTeam.Store(msgEvent.Channel, teamID)
		}
		if msgEvent.User != "" {
			a.channelToUser.Store(msgEvent.Channel, msgEvent.User)
		}
	}

	a.Logger().Debug("[SLACK_HTTP_WEBHOOK] HTTP webhook event received",
		"event_type", msgEvent.Type,
		"channel", msgEvent.Channel,
		"channel_type", msgEvent.ChannelType,
		"user", msgEvent.User,
		"text", msgEvent.Text,
		"ts", msgEvent.TS,
		"thread_ts", msgEvent.ThreadTS,
		"subtype", msgEvent.SubType)

	if msgEvent.BotID != "" || msgEvent.User == a.config.BotUserID {
		a.Logger().Debug("Skipping bot message", "bot_id", msgEvent.BotID)
		return
	}

	switch msgEvent.SubType {
	case "message_changed", "message_deleted", "thread_broadcast":
		a.Logger().Debug("Skipping message subtype", "subtype", msgEvent.SubType)
		return
	}

	if msgEvent.Text == "" {
		return
	}

	if !a.config.IsUserAllowed(msgEvent.User) {
		telemetry.GetMetrics().IncSlackPermissionBlockedUser()
		a.Logger().Debug("User blocked", "user_id", msgEvent.User)
		return
	}
	telemetry.GetMetrics().IncSlackPermissionAllowed()

	// Channel/DM policy check (must happen before GroupPolicy check)
	if !a.config.ShouldProcessChannel(msgEvent.ChannelType, msgEvent.Channel) {
		if msgEvent.ChannelType == "dm" {
			telemetry.GetMetrics().IncSlackPermissionBlockedDM()
		}
		a.Logger().Debug("Channel blocked by policy", "channel_type", msgEvent.ChannelType, "channel_id", msgEvent.Channel)
		return
	}

	// Group policy check: if GroupPolicy is "mention", only process messages that mention the bot
	// Note: HTTP mode does not receive app_mention events, so we must handle mentions here
	if msgEvent.ChannelType == "channel" || msgEvent.ChannelType == "group" {
		if a.config.GroupPolicy == "mention" {
			if !a.config.ContainsBotMention(msgEvent.Text) {
				telemetry.GetMetrics().IncSlackPermissionBlockedMention()
				a.Logger().Debug("Message ignored - bot not mentioned", "channel_type", msgEvent.ChannelType, "policy", "mention")
				return
			}
			// Bot is mentioned in channel/group - process this message
		}
		// Multibot mode: respond if no mentions (broadcast) or mentioned self
		if a.config.GroupPolicy == "multibot" {
			if !a.config.ShouldRespondInMultibotMode(msgEvent.Text) {
				a.Logger().Debug("Message ignored - other bot mentioned", "channel_type", msgEvent.ChannelType, "policy", "multibot")
				return
			}
			// If broadcast (no @), send polite response instead of processing
			if a.config.IsBroadcastMessage(msgEvent.Text) {
				response := a.config.GetBroadcastResponse(ctx, msgEvent.Text)
				// Only send if response is not empty. If empty, stay silent (as requested via config)
				if response != "" {
					threadID := msgEvent.ThreadTS
					if threadID == "" {
						threadID = msgEvent.TS
					}
					a.Logger().Debug("Broadcast message - sending polite response", "channel", msgEvent.Channel)
					_ = a.SendToChannel(ctx, msgEvent.Channel, response, threadID)
					return
				}
				// If response is empty, continue to thread ownership logic or return
				a.Logger().Debug("Broadcast message - broadcast_response is empty, staying silent", "channel", msgEvent.Channel)
				return
			}

		}
	}

	// Thread ownership decision (Phase 1: Bot Behavior Spec)
	// This replaces and enhances the previous GroupPolicy checks when ThreadOwnership is enabled
	if a.config.IsThreadOwnershipEnabled() {
		shouldRespond, reason := a.ShouldRespondToMessage(
			msgEvent.ChannelType,
			msgEvent.Channel,
			msgEvent.ThreadTS,
			msgEvent.Text,
			msgEvent.User,
		)
		if !shouldRespond {
			a.Logger().Debug("Message ignored by thread ownership policy",
				"reason", reason,
				"channel", msgEvent.Channel,
				"thread_ts", msgEvent.ThreadTS,
				"user", msgEvent.User)
			return
		}
		a.Logger().Debug("Message accepted by thread ownership policy",
			"reason", reason,
			"channel", msgEvent.Channel,
			"thread_ts", msgEvent.ThreadTS)
	}

	// Strip bot mention if present (standardize across modes)
	msgText := a.stripBotMention(msgEvent.Text)

	threadID := msgEvent.ThreadTS
	if threadID == "" {
		threadID = msgEvent.TS
	}

	processedText, conversionMetadata := preprocessMessageText(msgText)
	// Defense-in-depth: sanitize user input before passing to engine
	processedText = sanitizeUserInput(processedText)
	if _, converted := conversionMetadata["converted_from_hash"]; converted {
		a.Logger().Debug("Converted # prefix to / prefix", "original", msgEvent.Text, "converted", processedText)

		if a.processHashCommand(processedText, msgEvent.User, msgEvent.Channel, threadID) {
			return
		}
	}

	sessionID := a.GetOrCreateSession(msgEvent.User, msgEvent.BotUserID, msgEvent.Channel, threadID)

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
			"message_ts":   msgEvent.TS,
		},
	}

	if msgEvent.ThreadTS != "" {
		msg.Metadata["thread_ts"] = msgEvent.ThreadTS
	} else {
		// Slack Assistant API strictly requires a thread_ts for its endpoints
		msg.Metadata["thread_ts"] = msgEvent.TS
	}

	if msgEvent.SubType != "" {
		msg.Metadata["subtype"] = msgEvent.SubType
	}

	for k, v := range conversionMetadata {
		msg.Metadata[k] = v
	}

	// Store user message if storage is enabled
	a.storeUserMessage(ctx, msg)

	a.webhook.Run(ctx, a.Handler(), msg)
}

// handleAppHomeOpened handles the app_home_opened event
func (a *Adapter) handleAppHomeOpened(ctx context.Context, eventData json.RawMessage) {
	if a.apphomeHandler == nil {
		a.Logger().Debug("AppHome handler not configured, skipping app_home_opened event")
		return
	}

	var event struct {
		Type    string `json:"type"`
		User    string `json:"user"`
		Channel string `json:"channel"`
		Tab     string `json:"tab"`
	}
	if err := json.Unmarshal(eventData, &event); err != nil {
		a.Logger().Error("Parse app_home_opened event failed", "error", err)
		return
	}

	a.Logger().Debug("Handling app_home_opened event",
		"user", event.User,
		"channel", event.Channel,
		"tab", event.Tab)

	homeEvent := &apphome.HomeOpenedEvent{
		User:    event.User,
		Channel: event.Channel,
		Tab:     event.Tab,
	}

	if err := a.apphomeHandler.HandleHomeOpened(ctx, homeEvent); err != nil {
		a.Logger().Error("Failed to handle app_home_opened",
			"user", event.User,
			"error", err)
	}
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
