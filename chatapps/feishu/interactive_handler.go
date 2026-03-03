package feishu

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/hrygo/hotplex/chatapps/base"
)

// InteractiveEvent represents a Feishu interactive event
type InteractiveEvent struct {
	Header *InteractiveHeader    `json:"header"`
	Event  *InteractiveEventData `json:"event"`
	Token  string                `json:"token"`
}

// InteractiveHeader represents the event header
type InteractiveHeader struct {
	EventID    string `json:"event_id"`
	EventType  string `json:"event_type"`
	CreateTime string `json:"create_time"`
	Token      string `json:"token"`
	AppID      string `json:"app_id"`
	TenantKey  string `json:"tenant_key"`
}

// InteractiveEventData represents the event data
type InteractiveEventData struct {
	Message *InteractiveMessage `json:"message"`
	User    *InteractiveUser    `json:"user"`
	Action  *InteractiveAction  `json:"action"`
}

// InteractiveMessage represents the message in the event
type InteractiveMessage struct {
	MessageID   string `json:"message_id"`
	ChatID      string `json:"chat_id"`
	MessageType string `json:"message_type"`
}

// InteractiveUser represents the user who triggered the event
type InteractiveUser struct {
	UserID string `json:"user_id"`
}

// InteractiveAction represents the button action
type InteractiveAction struct {
	Value string `json:"value"`
	Tag   string `json:"tag"`
}

// ActionValue represents the decoded action value from button click
type ActionValue struct {
	Action    string `json:"action"`
	SessionID string `json:"session_id"`
	MessageID string `json:"message_id,omitempty"`
}

// InteractiveHandler handles Feishu interactive card callbacks
type InteractiveHandler struct {
	eventHandler *EventHandler
	adapter      *Adapter
	logger       *slog.Logger
}

// NewInteractiveHandler creates a new interactive handler
func NewInteractiveHandler(adapter *Adapter) *InteractiveHandler {
	eh := NewEventHandler(adapter)
	return &InteractiveHandler{
		eventHandler: eh,
		adapter:      adapter,
		logger:       eh.logger,
	}
}

// HandleInteractive handles incoming interactive events
func (h *InteractiveHandler) HandleInteractive(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := base.ReadBody(r)
	if err != nil {
		h.logger.Error("Read body failed", "error", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// Verify signature
	if err := h.adapter.verifySignature(r, body); err != nil {
		h.logger.Warn("Invalid signature", "error", err)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse event
	var event InteractiveEvent
	if err := json.Unmarshal(body, &event); err != nil {
		h.logger.Error("Parse event failed", "error", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// Handle URL verification
	if event.Header.EventType == "url_verification" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"challenge":"` + event.Token + `"}`))
		return
	}

	// Handle interactive message reply
	if event.Header.EventType == "im.message.reply" {
		h.handleButtonCallbackInternal(&event)
		h.eventHandler.WriteOKResponse(w)
		return
	}

	// Unknown event type
	h.logger.Debug("Ignoring unknown event type", "type", event.Header.EventType)
	h.eventHandler.WriteOKResponse(w)
}

// handleButtonCallbackInternal handles button callback without HTTP response
func (h *InteractiveHandler) handleButtonCallbackInternal(event *InteractiveEvent) {
	if event.Event.Action == nil || event.Event.Action.Value == "" {
		h.logger.Warn("Missing action value")
		return
	}

	var actionValue ActionValue
	if err := json.Unmarshal([]byte(event.Event.Action.Value), &actionValue); err != nil {
		h.logger.Error("Decode action value failed", "error", err)
		return
	}

	h.logger.Info("Button callback received",
		"action", actionValue.Action,
		"session_id", actionValue.SessionID,
		"user_id", event.Event.User.UserID,
	)

	// Route to appropriate handler based on action type
	switch actionValue.Action {
	case "permission_request":
		h.handlePermissionCallbackInternal(event, &actionValue)
	default:
		h.logger.Warn("Unknown action type", "action", actionValue.Action)
	}
}

// handlePermissionCallbackInternal handles permission approval/denial
func (h *InteractiveHandler) handlePermissionCallbackInternal(event *InteractiveEvent, actionValue *ActionValue) {
	chatID := event.Event.Message.ChatID
	if chatID == "" {
		h.logger.Error("Missing chat_id in event")
		return
	}

	resultText := "✅ 已允许执行"

	ctx := context.Background()
	token, err := h.adapter.GetAppTokenWithContext(ctx)
	if err != nil {
		h.logger.Error("Get token failed", "error", err)
		return
	}

	_, err = h.adapter.client.SendTextMessage(ctx, token, chatID, resultText)
	if err != nil {
		h.logger.Error("Send confirmation failed", "error", err)
	}

	// Update permission card async
	go func() {
		if err := h.UpdatePermissionCard(ctx, event.Event.Message.MessageID, chatID, "approved"); err != nil {
			h.logger.Error("Update permission card failed", "error", err)
		}
	}()
}

// UpdatePermissionCard updates a permission card with the result
func (h *InteractiveHandler) UpdatePermissionCard(ctx context.Context, messageID, chatID, result string) error {
	token, err := h.adapter.GetAppTokenWithContext(ctx)
	if err != nil {
		return err
	}

	var cardTemplate, title, description string
	switch strings.ToLower(result) {
	case "approved", "allow":
		cardTemplate = CardTemplateGreen
		title = "✅ 已允许"
		description = "权限已批准，操作将继续执行"
	case "denied", "deny":
		cardTemplate = CardTemplateRed
		title = "❌ 已拒绝"
		description = "权限已拒绝，操作已取消"
	default:
		cardTemplate = Grey
		title = "⏸️ 已取消"
		description = "操作已取消"
	}

	resultCard := &CardTemplate{
		Config: &CardConfig{
			WideScreenMode: false,
			EnableForward:  true,
		},
		Header: &CardHeader{
			Template: cardTemplate,
			Title: &Text{
				Content: title,
				Tag:     TextTypePlainText,
			},
		},
		Elements: []CardElement{
			{
				Type: ElementDiv,
				Text: &Text{
					Content: description,
					Tag:     TextTypeLarkMD,
				},
			},
			{
				Type: ElementNote,
				Elements: []CardElement{
					{
						Type: ElementMarkdown,
						Text: &Text{
							Content: "操作时间：" + time.Now().Format("2006-01-02 15:04:05"),
							Tag:     TextTypeLarkMD,
						},
					},
				},
			},
		},
	}

	cardJSON, err := json.Marshal(resultCard)
	if err != nil {
		return err
	}

	_, err = h.adapter.client.SendMessage(ctx, token, chatID, "interactive", map[string]string{
		"config": string(cardJSON),
	})
	if err != nil {
		return err
	}

	h.logger.Info("Permission card updated",
		"message_id", messageID,
		"chat_id", chatID,
		"result", result,
	)

	return nil
}

// EncodeActionValue encodes an action value for button callback
func EncodeActionValue(action, sessionID, messageID string) (string, error) {
	value := ActionValue{
		Action:    action,
		SessionID: sessionID,
		MessageID: messageID,
	}

	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}

	return string(data), nil
}

// DecodeActionValue decodes an action value from button callback
func DecodeActionValue(value string) (*ActionValue, error) {
	var actionValue ActionValue
	if err := json.Unmarshal([]byte(value), &actionValue); err != nil {
		return nil, err
	}

	return &actionValue, nil
}
