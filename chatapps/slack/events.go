package slack

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/hrygo/hotplex/chatapps/base"
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

	if !a.config.ShouldProcessChannel(msgEvent.ChannelType, msgEvent.Channel) {
		if msgEvent.ChannelType == "dm" {
			telemetry.GetMetrics().IncSlackPermissionBlockedDM()
		}
		a.Logger().Debug("Channel blocked by policy", "channel_type", msgEvent.ChannelType, "channel_id", msgEvent.Channel)
		return
	}

	if msgEvent.ChannelType == "channel" || msgEvent.ChannelType == "group" {
		if a.config.GroupPolicy == "mention" && !a.config.ContainsBotMention(msgEvent.Text) {
			telemetry.GetMetrics().IncSlackPermissionBlockedMention()
			a.Logger().Debug("Message ignored - bot not mentioned", "channel_type", msgEvent.ChannelType, "policy", "mention")
			return
		}
	}

	threadID := msgEvent.ThreadTS

	processedText, conversionMetadata := preprocessMessageText(msgEvent.Text)
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
	}

	if msgEvent.SubType != "" {
		msg.Metadata["subtype"] = msgEvent.SubType
	}

	for k, v := range conversionMetadata {
		msg.Metadata[k] = v
	}

	a.webhook.Run(ctx, a.Handler(), msg)
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
