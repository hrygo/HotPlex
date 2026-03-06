package feishu

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/hrygo/hotplex/chatapps/base"
	"github.com/hrygo/hotplex/chatapps/command"
)

// Adapter implements the Feishu (Lark) chat adapter
type Adapter struct {
	*base.Adapter
	config      *Config
	webhookPath string
	sender      *base.SenderWithMutex
	webhook     *base.WebhookRunner

	// Feishu API client (interface for testability - SOLID: Dependency Inversion)
	client FeishuAPIClient

	// Token cache
	appToken    string
	tokenExpire time.Time
	tokenMu     sync.RWMutex

	// Command handler (Phase 2.3)
	commandHandler *CommandHandler
	// Interactive handler (Phase 2.2)
	interactiveHandler *InteractiveHandler
	// Command registry
	commandRegistry *command.Registry
}

// Compile-time interface compliance checks
var (
	_ base.ChatAdapter       = (*Adapter)(nil)
	_ base.MessageOperations = (*Adapter)(nil)
)

// NewAdapter creates a new Feishu adapter
func NewAdapter(config *Config, logger *slog.Logger, opts ...base.AdapterOption) (*Adapter, error) {
	// Validate config
	if err := config.Validate(); err != nil {
		return nil, err
	}

	a := &Adapter{
		config:      config,
		webhookPath: "/feishu/events",
		sender:      base.NewSenderWithMutex(),
		webhook:     base.NewWebhookRunner(logger),
	}

	// Initialize API client (concrete implementation of FeishuAPIClient)
	a.client = NewClient(config.AppID, config.AppSecret, logger)

	// Initialize command registry
	a.commandRegistry = command.NewRegistry()

	// Prepare HTTP handlers
	httpOpts := []base.AdapterOption{
		base.WithHTTPHandler(a.webhookPath, a.handleEvent),
	}

	// Combine options
	allOpts := append(opts, httpOpts...)

	// Create base adapter
	a.Adapter = base.NewAdapter("feishu", base.Config{
		ServerAddr:   config.ServerAddr,
		SystemPrompt: config.SystemPrompt,
	}, logger, allOpts...)

	// Initialize interactive handler (Phase 2.2) after base adapter is created
	a.interactiveHandler = NewInteractiveHandler(a)

	// Initialize command handler (Phase 2.3) after base adapter is created
	a.commandHandler = NewCommandHandler(a, a.commandRegistry)

	// Set default sender
	a.sender.SetSender(a.defaultSender)

	return a, nil
}

// SendMessage sends a message to Feishu
func (a *Adapter) SendMessage(ctx context.Context, sessionID string, msg *base.ChatMessage) error {
	return a.sender.SendMessage(ctx, sessionID, msg)
}

// SetSender sets the message sender function
func (a *Adapter) SetSender(fn func(ctx context.Context, sessionID string, msg *base.ChatMessage) error) {
	a.sender.SetSender(fn)
}

// defaultSender sends message via Feishu API
func (a *Adapter) defaultSender(ctx context.Context, sessionID string, msg *base.ChatMessage) error {
	if a.client == nil {
		return ErrMessageSendFailed
	}

	// Get access token with context
	token, err := a.GetAppTokenWithContext(ctx)
	if err != nil {
		return err
	}

	// Get chat_id from metadata
	chatID, ok := msg.Metadata["chat_id"].(string)
	if !ok || chatID == "" {
		return ErrMessageSendFailed
	}

	// Send message via client
	_, err = a.client.SendTextMessage(ctx, token, chatID, msg.Content)
	return err
}

// handleEvent handles incoming Feishu webhook events
func (a *Adapter) handleEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		a.handleEventMessage(w, r)
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

// handleEventMessage handles Feishu event subscription messages
func (a *Adapter) handleEventMessage(w http.ResponseWriter, r *http.Request) {
	body, err := base.ReadBody(r)
	if err != nil {
		a.Logger().Error("Read body failed", "error", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// Verify signature
	if err := a.verifySignature(r, body); err != nil {
		a.Logger().Warn("Invalid signature", "error", err)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse event
	event, err := a.parseEvent(body)
	if err != nil {
		a.Logger().Error("Parse event failed", "error", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// Handle challenge (URL verification)
	if event.Type == "url_verification" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"challenge":"` + event.Challenge + `"}`))
		return
	}

	// Ignore non-message events
	if event.Header == nil || event.Header.EventType != "im.message.receive_v1" {
		a.Logger().Debug("Ignoring non-message event", "type", event.Header.EventType)
		w.WriteHeader(http.StatusOK)
		return
	}

	// Extract message content
	if event.Event == nil || event.Event.Message == nil {
		w.WriteHeader(http.StatusOK)
		return
	}

	msgContent := event.Event.Message.Content
	if msgContent == nil || msgContent.Text == "" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Create or get session
	sessionID := a.GetOrCreateSession(
		event.Event.Message.SenderID,
		"", // botUserID (not needed for Feishu)
		event.Event.Message.ChatID,
		"", // threadID (not needed for Feishu)
	)

	// Build ChatMessage
	chatMsg := &base.ChatMessage{
		Platform:  "feishu",
		SessionID: sessionID,
		UserID:    event.Event.Message.SenderID,
		Content:   msgContent.Text,
		MessageID: event.Event.Message.MessageID,
		Timestamp: time.Unix(event.Event.Message.CreateTime/1000, 0),
		Metadata: map[string]any{
			"chat_id":    event.Event.Message.ChatID,
			"tenant_key": event.Event.Message.TenantKey,
			"msg_type":   msgContent.Type,
		},
	}

	// Process message through handler
	if a.Handler() != nil {
		a.webhook.Run(r.Context(), a.Handler(), chatMsg)
	}

	// Acknowledge immediately (async processing)
	w.WriteHeader(http.StatusOK)
}

// verifySignature verifies the Feishu request signature
func (a *Adapter) verifySignature(r *http.Request, body []byte) error {
	// Get headers
	timestamp := r.Header.Get("X-Timestamp")
	signature := r.Header.Get("X-Signature")

	if timestamp == "" || signature == "" {
		return ErrInvalidSignature
	}

	// Build string to sign
	stringToSign := timestamp + a.config.EncryptKey + string(body)

	// Calculate expected signature
	expectedSignature := calculateHMACSHA256(stringToSign, a.config.EncryptKey)

	// Compare signatures
	if !secureCompare(signature, expectedSignature) {
		return ErrInvalidSignature
	}

	return nil
}

// GetAppToken gets or caches the app access token
func (a *Adapter) GetAppToken() (string, error) {
	return a.GetAppTokenWithContext(context.Background())
}

// GetAppTokenWithContext gets or caches the app access token with context
func (a *Adapter) GetAppTokenWithContext(ctx context.Context) (string, error) {
	a.tokenMu.RLock()
	if a.appToken != "" && time.Now().Add(5*time.Minute).Before(a.tokenExpire) {
		token := a.appToken
		a.tokenMu.RUnlock()
		return token, nil
	}
	a.tokenMu.RUnlock()

	// Fast path: check if we have a valid token (with lock)
	a.tokenMu.Lock()
	defer a.tokenMu.Unlock()

	// Double-check after acquiring write lock
	if a.appToken != "" && time.Now().Add(5*time.Minute).Before(a.tokenExpire) {
		return a.appToken, nil
	}

	// Fetch new token with context
	token, expireIn, err := a.client.GetAppTokenWithContext(ctx)
	if err != nil {
		return "", err
	}

	a.appToken = token
	a.tokenExpire = time.Now().Add(time.Duration(expireIn-300) * time.Second)

	a.Logger().Info("Feishu app token refreshed", "expire_in", expireIn)

	return token, nil
}
