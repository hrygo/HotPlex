package feishu

import (
	"context"
	"strconv"
	"strings"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	"github.com/hrygo/hotplex/internal/messaging"
	"github.com/hrygo/hotplex/internal/messaging/phrases"
)

// handleChatEntered processes the bot_p2p_chat_entered_v1 event.
// It sends a welcome card to new/returning users and records analytics.
func (a *Adapter) handleChatEntered(ctx context.Context, event *larkim.P2ChatAccessEventBotP2pChatEnteredV1) {
	if event.Event == nil {
		return
	}

	chatID := ptrStr(event.Event.ChatId)
	if chatID == "" {
		return
	}
	openID := ptrStr(event.Event.OperatorId.OpenId)
	if openID == "" {
		return
	}
	eventID := ""
	if event.EventV2Base != nil && event.EventV2Base.Header != nil {
		eventID = event.EventV2Base.Header.EventID
	}
	if eventID == "" {
		return
	}

	var lastMsgMs int64
	if event.Event.LastMessageCreateTime != nil && *event.Event.LastMessageCreateTime != "" {
		if v, err := strconv.ParseInt(*event.Event.LastMessageCreateTime, 10, 64); err == nil {
			lastMsgMs = v
		}
	}

	store := a.chatAccessStore()
	if store == nil {
		return
	}

	accessType := store.Classify(ctx, string(messaging.PlatformFeishu), chatID, a.botOpenID, openID, lastMsgMs)

	welcomeSent := false
	if accessType == messaging.ChatAccessNew || accessType == messaging.ChatAccessReturning {
		if err := a.sendWelcomeCard(ctx, chatID, accessType); err != nil {
			a.Log.Warn("feishu: welcome card send failed", "chat", chatID, "err", err)
		} else {
			welcomeSent = true
		}
	}

	inserted, err := store.Record(ctx, messaging.ChatAccessRecord{
		EventID:       eventID,
		Platform:      string(messaging.PlatformFeishu),
		ChatID:        chatID,
		UserID:        openID,
		BotID:         a.botOpenID,
		LastMessageAt: lastMsgMs,
		WelcomeSent:   welcomeSent,
	})
	if err != nil {
		a.Log.Warn("feishu: chat access record failed", "err", err)
	}
	if !inserted {
		a.Log.Debug("feishu: duplicate chat_entered event", "event_id", eventID)
	}
}

// sendWelcomeCard builds and sends a welcome card to the chat.
func (a *Adapter) sendWelcomeCard(ctx context.Context, chatID string, accessType messaging.ChatAccessType) error {
	category := "welcome"
	if accessType == messaging.ChatAccessReturning {
		category = "welcome_back"
	}

	text := a.phrases.Random(category)
	if text == "" {
		text = "Hi，我是 {bot_name}，你的 AI 编程助手！"
	}
	text = strings.ReplaceAll(text, "{bot_name}", a.resolveBotName())
	body := buildWelcomeBody(text, a.phrases)

	cardJSON := buildCard(
		cardHeader{Title: a.resolveBotName(), Template: headerBlue},
		map[string]any{"wide_screen_mode": true},
		[]map[string]any{{"tag": "markdown", "content": body}},
	)

	_, err := larkCreateMessage(ctx, a.larkClient, chatID, cardJSON)
	return err
}

// buildWelcomeBody assembles the welcome card body from phrases.
func buildWelcomeBody(greeting string, p *phrases.Phrases) string {
	body := greeting
	if caps := p.All("capabilities"); len(caps) > 0 {
		header := phraseOr(p, "capabilities_header", "我可以帮你：")
		body += "\n\n" + header
		for _, c := range caps {
			body += "\n• " + c
		}
	}
	if cmds := p.All("quick_commands"); len(cmds) > 0 {
		header := phraseOr(p, "commands_header", "快捷命令：")
		body += "\n\n" + header
		for i, c := range cmds {
			if i > 0 {
				body += " "
			}
			body += c
		}
	}
	if cl := p.Random("closing_line"); cl != "" {
		body += "\n" + cl
	}
	return body
}

// phraseOr returns the first entry of a category, or fallback if empty/nil.
func phraseOr(p *phrases.Phrases, category, fallback string) string {
	if s := p.Random(category); s != "" {
		return s
	}
	return fallback
}

// chatAccessStore extracts the ChatAccessStore from the adapter extras.
func (a *Adapter) chatAccessStore() messaging.ChatAccessStorer {
	if a.Extras == nil {
		return nil
	}
	s, _ := a.Extras["chat_access_store"].(messaging.ChatAccessStorer)
	return s
}
