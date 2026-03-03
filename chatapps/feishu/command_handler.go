package feishu

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/hrygo/hotplex/chatapps/base"
	"github.com/hrygo/hotplex/chatapps/command"
	"github.com/hrygo/hotplex/event"
)

// CommandHandler handles Feishu bot commands (/reset, /dc)
type CommandHandler struct {
	adapter  *Adapter
	registry *command.Registry
	logger   *slog.Logger
	rateLimiter *RateLimiter
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
	Header *CommandHeader `json:"header"`
	Event  *CommandEventData `json:"event"`
	Token  string `json:"token"`
}

// CommandHeader represents the command event header
type CommandHeader struct {
	EventID   string `json:"event_id"`
	EventType string `json:"event_type"`
	CreateTime string `json:"create_time"`
	AppID     string `json:"app_id"`
	TenantKey string `json:"tenant_key"`
}

// CommandEventData represents the command event data
type CommandEventData struct {
	AppID      string `json:"app_id"`
	TenantKey  string `json:"tenant_key"`
	OperatorID *UserID `json:"operator_id"`
	Name       string `json:"name"`
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
	return &CommandHandler{
		adapter:     adapter,
		registry:    registry,
		logger:      adapter.Logger(),
		rateLimiter: NewRateLimiter(5 * time.Second), // 5 second cooldown
	}
}

// HandleCommand handles incoming command events
func (h *CommandHandler) HandleCommand(w http.ResponseWriter, r *http.Request) {
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
	var cmdEvent CommandEvent
	if err := json.Unmarshal(body, &cmdEvent); err != nil {
		h.logger.Error("Parse command event failed", "error", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// Handle URL verification
	if cmdEvent.Header.EventType == "url_verification" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"challenge":"` + cmdEvent.Token + `"}`))
		return
	}

	// Handle application.open_event_v6 (command invocation)
	if cmdEvent.Header.EventType == "application.open_event_v6" {
		h.handleCommandInvocation(w, r, &cmdEvent)
		return
	}

	// Unknown event type
	h.logger.Debug("Ignoring unknown command event type", "type", cmdEvent.Header.EventType)
	w.WriteHeader(http.StatusOK)
}

// handleCommandInvocation handles command invocation events
func (h *CommandHandler) handleCommandInvocation(w http.ResponseWriter, r *http.Request, event *CommandEvent) {
	// Extract command name
	cmdName := event.Event.Name
	if cmdName == "" {
		h.logger.Warn("Missing command name")
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// Extract user ID
	userID := event.Event.OperatorID.UserID
	if userID == "" {
		h.logger.Warn("Missing operator user ID")
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// Rate limiting
	if !h.rateLimiter.Allow(userID) {
		h.logger.Warn("Rate limit exceeded", "user_id", userID)
		http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
		return
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
		http.Error(w, "Unknown command", http.StatusBadRequest)
		return
	}

	// Build command request
	req := &command.Request{
		Command: internalCmd,
		Text:    "",
		UserID:  userID,
		SessionID: userID, // Use user ID as session ID for now
		Metadata: map[string]any{
			"app_id":     event.Event.AppID,
			"tenant_key": event.Event.TenantKey,
		},
	}

	// Create callback for progress updates
	callback := h.createCommandCallback(r.Context(), userID)

	// Execute command
	result, err := h.registry.Execute(r.Context(), req, callback)
	if err != nil {
		h.logger.Error("Command execution failed", "error", err)
		h.sendCommandResult(r.Context(), userID, false, "命令执行失败："+err.Error())
		http.Error(w, "Command execution failed", http.StatusInternalServerError)
		return
	}

	// Send result
	h.sendCommandResult(r.Context(), userID, result.Success, result.Message)

	// Acknowledge
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{}`))
}

// mapCommand maps Feishu command names to internal commands
func (h *CommandHandler) mapCommand(feishuCmd string) string {
	// Feishu commands are configured without leading /
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
		// Progress updates will be sent via interactive cards in future iterations
		return nil
	}
}

// sendCommandResult sends a command result message
func (h *CommandHandler) sendCommandResult(ctx context.Context, userID string, success bool, message string) {
	// Get token
	token, err := h.adapter.GetAppTokenWithContext(ctx)
	if err != nil {
		h.logger.Error("Get token failed", "error", err)
		return
	}

	// For now, use user's DM chat ID
	// In production, this should be resolved from user ID
	chatID := userID

	// Send result message
	_, err = h.adapter.client.SendTextMessage(ctx, token, chatID, message)
	if err != nil {
		h.logger.Error("Send command result failed", "error", err)
	}
}
