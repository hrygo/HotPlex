package slack

import (
	"context"
	"strings"
	"time"

	"github.com/hrygo/hotplex/chatapps/base"
	"github.com/hrygo/hotplex/chatapps/command"
	"github.com/hrygo/hotplex/internal/panicx"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

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

	a.socketModeClient.Ack(*evt.Request)

	teamID := eventsAPIEvent.TeamID

	switch eventsAPIEvent.Type {
	case slackevents.CallbackEvent:
		innerEvent := eventsAPIEvent.InnerEvent
		switch ev := innerEvent.Data.(type) {
		case *slackevents.AppMentionEvent:
			a.handleAppMentionEvent(teamID, ev)
		case *slackevents.MessageEvent:
			a.handleSocketModeMessageEvent(teamID, ev)
		}
	default:
		a.Logger().Debug("Unhandled EventsAPI type", "type", eventsAPIEvent.Type)
	}
}

// handleAppMentionEvent handles app_mention events
func (a *Adapter) handleAppMentionEvent(teamID string, ev *slackevents.AppMentionEvent) {
	a.Logger().Debug("App mention received", "user", ev.User, "channel", ev.Channel)

	if !a.config.IsUserAllowed(ev.User) {
		a.Logger().Debug("User blocked", "user_id", ev.User)
		return
	}

	// Owner policy check (Phase 1: Bot Behavior Spec)
	if !a.config.CanRespond(ev.User) {
		a.Logger().Debug("User not allowed by owner policy",
			"user_id", ev.User,
			"policy", a.config.GetOwnerPolicy())
		return
	}

	threadID := ev.ThreadTimeStamp
	if threadID == "" {
		threadID = ev.TimeStamp
	}

	// Claim thread ownership (Phase 1: Bot Behavior Spec)
	// Note: ownershipTracker may be nil if initialization failed
	if a.config.IsThreadOwnershipEnabled() && a.ownershipTracker != nil {
		threadKey := NewThreadKey(ev.Channel, threadID)
		// R3/R4: This bot claims ownership when @mentioned
		// Each bot in multi-bot scenarios independently tracks its own owned threads
		a.ownershipTracker.Claim(threadKey)
		a.Logger().Debug("Thread ownership claimed",
			"channel", ev.Channel,
			"thread_ts", threadID)
	}

	sessionID := a.GetOrCreateSession(ev.User, a.config.BotUserID, ev.Channel, threadID)
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
			"channel_type": "channel",
			"message_ts":   ev.TimeStamp,
			"thread_ts":    threadID, // Fallbacks to TimeStamp if ThreadTimeStamp is empty
			"event_type":   "app_mention",
			"event_ts":     ev.TimeStamp,
		},
	}

	if ev.Channel != "" {
		if teamID != "" {
			a.channelToTeam.Store(ev.Channel, teamID)
		}
		if ev.User != "" {
			a.channelToUser.Store(ev.Channel, ev.User)
		}
	}

	// Store user message if storage is enabled
	a.storeUserMessage(a.socketModeCtx, msg)

	a.webhook.Run(a.socketModeCtx, a.Handler(), msg)
}

// handleSocketModeMessageEvent handles message events via Socket Mode
func (a *Adapter) handleSocketModeMessageEvent(teamID string, ev *slackevents.MessageEvent) {

	if ev.BotID != "" || ev.User == a.config.BotUserID {
		return
	}

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

	// Channel/DM policy check (must happen before GroupPolicy check)
	if !a.config.ShouldProcessChannel(ev.ChannelType, ev.Channel) {
		a.Logger().Debug("Channel blocked by policy", "channel_type", ev.ChannelType)
		return
	}

	// =========================================================================
	// Strict Collision Avoidance (Airtight Filter)
	// =========================================================================
	// If message contains bot mention in a non-DM channel, ALWAYS skip it here.
	// It is guaranteed to be handled by handleAppMentionEvent.
	// This is critical for multibot mode to avoid dual-processing same user request.
	if ev.ChannelType != "dm" && a.config.ContainsBotMention(ev.Text) {
		a.Logger().Debug("Skipping 'message' event with mention (delegating to 'app_mention')", "ts", ev.TimeStamp)
		return
	}

	// Group policy check: if GroupPolicy is "mention", only process messages that mention the bot
	// Note: app_mention events are handled separately by handleAppMentionEvent
	if (ev.ChannelType == "channel" || ev.ChannelType == "group") && a.config.GroupPolicy == "mention" {
		// Already checked for lack of mention above? No, the block above only skips if mention IS PRESENT.
		// So if we reach here, there is no mention.
		a.Logger().Debug("Message ignored - bot not mentioned", "channel_type", ev.ChannelType, "policy", "mention")
		return
	}

	// Multibot mode: respond if no mentions (broadcast) or mentioned self
	if (ev.ChannelType == "channel" || ev.ChannelType == "group") && a.config.GroupPolicy == "multibot" {
		a.Logger().Info("Multibot mode processing",
			"channel_type", ev.ChannelType,
			"bot_user_id", a.config.BotUserID,
			"message_text", ev.Text,
			"mentioned_users", ExtractMentionedUsers(ev.Text))

		if !a.config.ShouldRespondInMultibotMode(ev.Text) {
			a.Logger().Info("Message ignored - other bot mentioned", "channel_type", ev.ChannelType, "policy", "multibot")
			return
		}
		// Broadcast response only for top-level channel messages (not threads)
		// Thread messages are targeted conversations, not broadcasts
		if a.config.IsBroadcastMessage(ev.Text) && ev.ThreadTimeStamp == "" {
			threadID := ev.ThreadTimeStamp
			if threadID == "" {
				threadID = ev.TimeStamp
			}
			a.Logger().Info("Broadcast message - sending polite response", "channel", ev.Channel, "bot_user_id", a.config.BotUserID)
			response := a.config.GetBroadcastResponse(a.socketModeCtx, ev.Text)
			_ = a.SendToChannel(a.socketModeCtx, ev.Channel, response, threadID)
			return
		}
		// Thread message without @mention - process normally (it's a targeted conversation)
		if ev.ThreadTimeStamp != "" {
			a.Logger().Info("Multibot mode - thread message without @mention, processing", "bot_user_id", a.config.BotUserID, "thread_ts", ev.ThreadTimeStamp)
		} else {
			a.Logger().Info("Multibot mode - bot self-mention (or broadcast), processing message", "bot_user_id", a.config.BotUserID)
		}
	}

	// Thread ownership decision (Phase 1: Bot Behavior Spec)
	// This replaces and enhances the previous GroupPolicy checks when ThreadOwnership is enabled
	if a.config.IsThreadOwnershipEnabled() {
		shouldRespond, reason := a.ShouldRespondToMessage(
			ev.ChannelType,
			ev.Channel,
			ev.ThreadTimeStamp,
			ev.Text,
			ev.User,
		)
		if !shouldRespond {
			a.Logger().Debug("Message ignored by thread ownership policy",
				"reason", reason,
				"channel", ev.Channel,
				"thread_ts", ev.ThreadTimeStamp,
				"user", ev.User)
			return
		}
		a.Logger().Debug("Message accepted by thread ownership policy",
			"reason", reason,
			"channel", ev.Channel,
			"thread_ts", ev.ThreadTimeStamp)
	}

	threadID := ev.ThreadTimeStamp
	if threadID == "" {
		threadID = ev.TimeStamp
	}

	processedText, conversionMetadata := preprocessMessageText(ev.Text)
	if _, converted := conversionMetadata["converted_from_hash"]; converted {
		a.Logger().Debug("Converted # prefix to / prefix (Socket Mode)",
			"original", ev.Text, "converted", processedText)

		if a.processHashCommand(processedText, ev.User, ev.Channel, threadID) {
			return
		}
	}

	sessionID := a.GetOrCreateSession(ev.User, a.config.BotUserID, ev.Channel, threadID)
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
			"message_ts":   ev.TimeStamp,
			"event_type":   "message",
			"event_ts":     ev.TimeStamp,
		},
	}

	if ev.ThreadTimeStamp != "" {
		msg.Metadata["thread_ts"] = ev.ThreadTimeStamp
	} else {
		// Slack Assistant API strictly requires a thread_ts for its endpoints
		msg.Metadata["thread_ts"] = ev.TimeStamp
	}

	for k, v := range conversionMetadata {
		msg.Metadata[k] = v
	}

	if ev.Channel != "" {
		if teamID != "" {
			a.channelToTeam.Store(ev.Channel, teamID)
		}
		if ev.User != "" {
			a.channelToUser.Store(ev.Channel, ev.User)
		}
	}

	// Store user message if storage is enabled
	a.storeUserMessage(a.socketModeCtx, msg)

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

	if !a.rateLimiter.Allow(cmd.UserID) {
		a.Logger().Warn("Rate limit exceeded", "user_id", cmd.UserID)
		a.socketModeClient.Ack(*evt.Request, map[string]interface{}{
			"text": "⚠️ Rate limit exceeded. Please wait a moment.",
		})
		return
	}

	a.socketModeClient.Ack(*evt.Request, map[string]interface{}{
		"text": "Processing command...",
	})

	baseSession := a.FindSessionByUserAndChannel(cmd.UserID, cmd.ChannelID)
	var sessionID string
	if baseSession != nil {
		sessionID = baseSession.SessionID
	}

	req := &command.Request{
		Command:     cmd.Command,
		Text:        cmd.Text,
		UserID:      cmd.UserID,
		ChannelID:   cmd.ChannelID,
		ThreadTS:    "",
		SessionID:   sessionID,
		ResponseURL: cmd.ResponseURL,
	}

	// Create callback for progress events
	var progressTS string
	callback := func(eventType string, data any) error {
		return a.handleCommandProgress(cmd.ChannelID, "", &progressTS, eventType, data)
	}

	_, err := a.cmdRegistry.Execute(context.Background(), req, callback)
	if err != nil {
		a.Logger().Error("Command execution failed", "command", cmd.Command, "error", err)
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

	a.socketModeClient.Ack(*evt.Request)

	switch callback.Type {
	case slack.InteractionTypeBlockActions:

		for _, action := range callback.ActionCallback.BlockActions {
			actionID := action.ActionID

			// Permission buttons
			if actionID == "perm_allow" || actionID == "perm_deny" {
				slackCallback := &SlackInteractionCallback{
					Type:    "block_actions",
					User:    CallbackUser{ID: callback.User.ID},
					Channel: CallbackChannel{ID: callback.Channel.ID},
					Message: CallbackMessage{Ts: callback.Message.Timestamp},
					Actions: []SlackAction{
						{
							ActionID: actionID,
							BlockID:  action.BlockID,
							Value:    action.Value,
						},
					},
				}
				a.handlePermissionCallback(slackCallback, slackCallback.Actions[0], nil)
				return
			}

			// Danger block buttons (WAF approval)
			if strings.HasPrefix(actionID, "danger_confirm") || strings.HasPrefix(actionID, "danger_cancel") {
				slackCallback := &SlackInteractionCallback{
					Type:    "block_actions",
					User:    CallbackUser{ID: callback.User.ID},
					Channel: CallbackChannel{ID: callback.Channel.ID},
					Message: CallbackMessage{Ts: callback.Message.Timestamp},
					Actions: []SlackAction{
						{
							ActionID: actionID,
							BlockID:  action.BlockID,
							Value:    action.Value,
						},
					},
				}
				a.handleDangerBlockCallback(slackCallback, slackCallback.Actions[0], nil)
				return
			}

			// Plan mode buttons
			if actionID == "plan_approve" || actionID == "plan_modify" || actionID == "plan_cancel" {
				slackCallback := &SlackInteractionCallback{
					Type:    "block_actions",
					User:    CallbackUser{ID: callback.User.ID},
					Channel: CallbackChannel{ID: callback.Channel.ID},
					Message: CallbackMessage{Ts: callback.Message.Timestamp},
					Actions: []SlackAction{
						{
							ActionID: actionID,
							BlockID:  action.BlockID,
							Value:    action.Value,
						},
					},
				}
				a.handlePlanModeCallback(slackCallback, slackCallback.Actions[0], nil)
				return
			}

			// User question options
			if strings.HasPrefix(actionID, "question_option_") {
				slackCallback := &SlackInteractionCallback{
					Type:    "block_actions",
					User:    CallbackUser{ID: callback.User.ID},
					Channel: CallbackChannel{ID: callback.Channel.ID},
					Message: CallbackMessage{Ts: callback.Message.Timestamp},
					Actions: []SlackAction{
						{
							ActionID: actionID,
							BlockID:  action.BlockID,
							Value:    action.Value,
						},
					},
				}
				a.handleAskUserQuestionCallback(slackCallback, slackCallback.Actions[0], nil)
				return
			}
		}
	default:
		a.Logger().Debug("Unhandled interaction type", "type", callback.Type)
	}
}
