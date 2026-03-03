package feishu

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/hrygo/hotplex/chatapps/command"
	"github.com/hrygo/hotplex/event"
)

// CommandHandler handles Feishu bot commands (/reset, /dc)
type CommandHandler struct {
	eventHandler *EventHandler
	adapter      *Adapter
	registry     *command.Registry
	logger       *slog.Logger
	rateLimiter  *RateLimiter
}

// RateLimiter implements simple rate limiting for commands
type RateLimiter struct {
	mu       map[string]time.Time
	duration time.Duration
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(duration time.Duration) *RateLimiter {
	return &RateLimiter{
		mu:       make(map[string]time.Time),
		duration: duration,
	}
}

// Allow checks if a request is allowed
func (r *RateLimiter) Allow(key string) bool {
	now := time.Now()
	if last, exists := r.mu[key]; exists {
		if now.Sub(last) < r.duration {
			return false
		}
	}
	r.mu[key] = now
	return true
}

// CommandEvent represents a Feishu command event
type CommandEvent struct {
	Header *CommandHeader    `json:"header"`
	Event  *CommandEventData `json:"event"`
	Token  string            `json:"token"`
}

// CommandHeader represents the command event header
type CommandHeader struct {
	EventID    string `json:"event_id"`
	EventType  string `json:"event_type"`
	CreateTime string `json:"create_time"`
	AppID      string `json:"app_id"`
	TenantKey  string `json:"tenant_key"`
}

// CommandEventData represents the command event data
type CommandEventData struct {
	AppID      string          `json:"app_id"`
	TenantKey  string          `json:"tenant_key"`
	OperatorID *UserID         `json:"operator_id"`
	Name       string          `json:"name"`
	Content    *CommandContent `json:"content"`
}

// UserID represents a user identifier
type UserID struct {
	UserID string `json:"user_id"`
}

// CommandContent represents the command content
type CommandContent struct {
	Text string `json:"text"`
}

// NewCommandHandler creates a new command handler
func NewCommandHandler(adapter *Adapter, registry *command.Registry) *CommandHandler {
	eh := NewEventHandler(adapter)
	return &CommandHandler{
		eventHandler: eh,
		adapter:      adapter,
		registry:     registry,
		logger:       eh.logger,
		rateLimiter:  NewRateLimiter(5 * time.Second),
	}
}

// HandleCommand handles incoming command events
func (h *CommandHandler) HandleCommand(w http.ResponseWriter, r *http.Request) {
	h.eventHandler.ProcessEvent(w, r, h.parseCommandEvent, h.handleCommandEvent)
}

// parseCommandEvent parses command event JSON
func (h *CommandHandler) parseCommandEvent(body []byte) (interface{}, error) {
	var cmdEvent CommandEvent
	if err := json.Unmarshal(body, &cmdEvent); err != nil {
		return nil, err
	}
	return &cmdEvent, nil
}

// handleCommandEvent handles the parsed command event
func (h *CommandHandler) handleCommandEvent(event interface{}) error {
	ce := event.(*CommandEvent)

	// Handle URL verification
	if ce.Header.EventType == "url_verification" {
		return nil
	}

	// Handle command invocation
	if ce.Header.EventType == "application.open_event_v6" {
		return h.handleCommandInvocationInternal(ce)
	}

	// Unknown event type
	h.logger.Debug("Ignoring unknown command event type", "type", ce.Header.EventType)
	return nil
}

// handleCommandInvocationInternal handles command invocation without HTTP response
func (h *CommandHandler) handleCommandInvocationInternal(event *CommandEvent) error {
	cmdName := event.Event.Name
	if cmdName == "" {
		h.logger.Warn("Missing command name")
		return nil
	}

	userID := event.Event.OperatorID.UserID
	if userID == "" {
		h.logger.Warn("Missing operator user ID")
		return nil
	}

	// Rate limiting
	if !h.rateLimiter.Allow(userID) {
		h.logger.Warn("Rate limit exceeded", "user_id", userID)
		return nil
	}

	h.logger.Info("Command invoked",
		"command", cmdName,
		"user_id", userID,
		"app_id", event.Event.AppID,
	)

	// Map Feishu command name to internal command
	internalCmd := h.mapCommand(cmdName)
	if internalCmd == "" {
		h.logger.Warn("Unknown command", "command", cmdName)
		return nil
	}

	// Build command request
	req := &command.Request{
		Command:   internalCmd,
		Text:      "",
		UserID:    userID,
		SessionID: userID,
		Metadata: map[string]any{
			"app_id":     event.Event.AppID,
			"tenant_key": event.Event.TenantKey,
		},
	}

	// Create callback for progress updates
	callback := h.createCommandCallback(context.Background(), userID)

	// Execute command
	_, err := h.registry.Execute(context.Background(), req, callback)
	if err != nil {
		h.logger.Error("Command execution failed", "error", err)
		h.sendCommandResultInternal(userID, false, "命令执行失败："+err.Error())
		return err
	}

	return nil
}

// mapCommand maps Feishu command names to internal commands
func (h *CommandHandler) mapCommand(feishuCmd string) string {
	switch strings.ToLower(feishuCmd) {
	case "reset":
		return command.CommandReset
	case "dc":
		return command.CommandDisconnect
	default:
		return ""
	}
}

// createCommandCallback creates a callback for command progress events
func (h *CommandHandler) createCommandCallback(ctx context.Context, userID string) event.Callback {
	return func(eventType string, data any) error {
		h.logger.Debug("Command callback", "type", eventType, "data", data)
		return nil
	}
}

// sendCommandResult sends a command result message
func (h *CommandHandler) sendCommandResult(ctx context.Context, userID string, success bool, message string) {
	token, err := h.adapter.GetAppTokenWithContext(ctx)
	if err != nil {
		h.logger.Error("Get token failed", "error", err)
		return
	}

	chatID := userID
	_, err = h.adapter.client.SendTextMessage(ctx, token, chatID, message)
	if err != nil {
		h.logger.Error("Send command result failed", "error", err)
	}
}

// sendCommandResultInternal sends a command result message without context
func (h *CommandHandler) sendCommandResultInternal(userID string, success bool, message string) {
	ctx := context.Background()
	h.sendCommandResult(ctx, userID, success, message)
}
