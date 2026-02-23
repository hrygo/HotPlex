package chatapps

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// TelegramConfig Telegram Bot Configuration
type TelegramConfig struct {
	BotToken    string // Bot Token from @BotFather
	WebhookURL  string // Public URL for webhook
	ServerAddr  string // Local server address
	SecretToken string // Secret token for webhook verification (required for security)
}

// TelegramUpdate represents incoming Telegram update
type TelegramUpdate struct {
	UpdateID int64 `json:"update_id"`
	Message  struct {
		MessageID int64 `json:"message_id"`
		From      struct {
			ID        int64  `json:"id"`
			FirstName string `json:"first_name"`
			LastName  string `json:"last_name"`
			Username  string `json:"username"`
		} `json:"from"`
		Chat struct {
			ID    int64  `json:"id"`
			Type  string `json:"type"`
			Title string `json:"title,omitempty"`
		} `json:"chat"`
		Date     int64  `json:"date"`
		Text     string `json:"text"`
		Entities []struct {
			Type   string `json:"type"`
			Offset int    `json:"offset"`
			Length int    `json:"length"`
		} `json:"entities,omitempty"`
	} `json:"message"`
	CallbackQuery *struct {
		ID   string `json:"id"`
		From struct {
			ID        int64  `json:"id"`
			FirstName string `json:"first_name"`
			LastName  string `json:"last_name"`
			Username  string `json:"username"`
		} `json:"from"`
		Message struct {
			Chat struct {
				ID int64 `json:"id"`
			} `json:"chat"`
			MessageID int64  `json:"message_id"`
			Data      string `json:"data"`
		} `json:"message,omitempty"`
	} `json:"callback_query,omitempty"`
}

// TelegramMessageResponse Telegram API response for sending messages
type TelegramMessageResponse struct {
	OK     bool         `json:"ok"`
	Result *TelegramMsg `json:"result"`
}

// TelegramMsg represents sent message
type TelegramMsg struct {
	MessageID int64 `json:"message_id"`
	Chat      struct {
		ID int64 `json:"id"`
	} `json:"chat"`
	Date int64  `json:"date"`
	Text string `json:"text"`
}

// TelegramWebhookResponse Response for Telegram webhook verification
type TelegramWebhookResponse struct {
	OK          bool   `json:"ok"`
	Description string `json:"description"`
}

// TelegramAdapter Telegram Bot Adapter
type TelegramAdapter struct {
	config   TelegramConfig
	logger   *slog.Logger
	server   *http.Server
	sessions map[string]*TelegramSession
	mu       sync.RWMutex
	handler  MessageHandler
	running  bool
}

// TelegramSession Session for Telegram user
type TelegramSession struct {
	SessionID  string
	UserID     string
	ChatID     int64
	Platform   string
	LastActive time.Time
}

// NewTelegramAdapter Create new Telegram adapter
func NewTelegramAdapter(config TelegramConfig, logger *slog.Logger) *TelegramAdapter {
	if config.ServerAddr == "" {
		config.ServerAddr = ":8080"
	}
	return &TelegramAdapter{
		config:   config,
		logger:   logger,
		sessions: make(map[string]*TelegramSession),
	}
}

// Platform Returns platform name
func (a *TelegramAdapter) Platform() string {
	return "telegram"
}

// SetHandler Set message handler
func (a *TelegramAdapter) SetHandler(handler MessageHandler) {
	a.handler = handler
}

// Start Start Telegram adapter
func (a *TelegramAdapter) Start(ctx context.Context) error {
	if a.running {
		return nil
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/webhook", a.handleWebhook)
	mux.HandleFunc("/health", a.handleHealth)

	a.server = &http.Server{
		Addr:    a.config.ServerAddr,
		Handler: mux,
	}

	go func() {
		a.logger.Info("Starting Telegram adapter", "addr", a.config.ServerAddr)
		if err := a.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			a.logger.Error("Telegram server error", "error", err)
		}
	}()

	// Set webhook if URL provided
	if a.config.WebhookURL != "" {
		if err := a.setWebhook(ctx); err != nil {
			a.logger.Warn("Failed to set webhook", "error", err)
		}
	}

	a.running = true
	return nil
}

// Stop Stop Telegram adapter
func (a *TelegramAdapter) Stop() error {
	if !a.running {
		return nil
	}

	// Remove webhook
	if a.config.WebhookURL != "" {
		_ = a.removeWebhook(context.Background())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := a.server.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown server: %w", err)
	}

	a.running = false
	a.logger.Info("Telegram adapter stopped")
	return nil
}

// SendMessage Send message to Telegram user
func (a *TelegramAdapter) SendMessage(ctx context.Context, sessionID string, msg *ChatMessage) error {
	chatID, ok := msg.Metadata["chat_id"].(int64)
	if !ok || chatID == 0 {
		return fmt.Errorf("chat_id not found in metadata")
	}

	payload := map[string]any{
		"chat_id": chatID,
		"text":    msg.Content,
	}

	// Apply rich content if present
	if msg.RichContent != nil {
		// Parse mode (Markdown or HTML)
		switch msg.RichContent.ParseMode {
		case ParseModeMarkdown:
			payload["parse_mode"] = "MarkdownV2"
		case ParseModeHTML:
			payload["parse_mode"] = "HTML"
		}

		// Inline keyboard
		if msg.RichContent.InlineKeyboard != nil {
			payload["reply_markup"] = msg.RichContent.InlineKeyboard
		}

		// Handle attachments (send as media group if multiple, or individual)
		if len(msg.RichContent.Attachments) > 0 {
			return a.sendMessageWithAttachments(ctx, sessionID, chatID, msg)
		}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", a.config.BotToken)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("send message: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("send failed: %d %s", resp.StatusCode, string(respBody))
	}

	a.logger.Debug("Message sent", "session", sessionID, "chat_id", chatID)
	return nil
}

// sendMessageWithAttachments sends message with media attachments
func (a *TelegramAdapter) sendMessageWithAttachments(ctx context.Context, sessionID string, chatID int64, msg *ChatMessage) error {
	// For now, send text first then handle attachments
	// Full media group support would require additional implementation
	if err := a.sendSimpleMessage(ctx, chatID, msg.Content, msg.RichContent.ParseMode); err != nil {
		return err
	}

	// Send each attachment as a separate message
	for _, att := range msg.RichContent.Attachments {
		var method string
		payload := map[string]any{"chat_id": chatID}

		switch att.Type {
		case "photo":
			method = "sendPhoto"
			payload["photo"] = att.URL
			if att.Text != "" {
				payload["caption"] = att.Text
			}
		case "video":
			method = "sendVideo"
			payload["video"] = att.URL
			if att.Text != "" {
				payload["caption"] = att.Text
			}
		case "document":
			method = "sendDocument"
			payload["document"] = att.URL
			if att.Title != "" {
				payload["title"] = att.Title
			}
		case "audio":
			method = "sendAudio"
			payload["audio"] = att.URL
		default:
			continue
		}

		switch msg.RichContent.ParseMode {
		case ParseModeMarkdown:
			payload["parse_mode"] = "MarkdownV2"
		case ParseModeHTML:
			payload["parse_mode"] = "HTML"
		}

		body, err := json.Marshal(payload)
		if err != nil {
			a.logger.Error("Marshal attachment payload failed", "error", err)
			continue
		}
		url := fmt.Sprintf("https://api.telegram.org/bot%s/%s", a.config.BotToken, method)
		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
		if err != nil {
			a.logger.Error("Create request failed", "error", err)
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			a.logger.Error("Send attachment failed", "error", err)
			continue
		}
		defer func() { _ = resp.Body.Close() }()
	}

	return nil
}

// sendSimpleMessage sends a simple text message
func (a *TelegramAdapter) sendSimpleMessage(ctx context.Context, chatID int64, text string, parseMode ParseMode) error {
	payload := map[string]any{
		"chat_id": chatID,
		"text":    text,
	}

	switch parseMode {
	case ParseModeMarkdown:
		payload["parse_mode"] = "MarkdownV2"
	case ParseModeHTML:
		payload["parse_mode"] = "HTML"
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", a.config.BotToken)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("send failed: %d %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// HandleMessage Handle incoming message (not used for webhook mode)
func (a *TelegramAdapter) HandleMessage(ctx context.Context, msg *ChatMessage) error {
	return nil
}

func (a *TelegramAdapter) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		a.logger.Error("Read body failed", "error", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// Verify secret token for security (CVE-2026-25474)
	if a.config.SecretToken != "" {
		token := r.Header.Get("X-Telegram-Bot-Api-Secret-Token")
		if token != a.config.SecretToken {
			a.logger.Warn("Invalid secret token")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
	}

	var update TelegramUpdate
	if err := json.Unmarshal(body, &update); err != nil {
		a.logger.Error("Parse update failed", "error", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// Handle callback query
	if update.CallbackQuery != nil {
		a.handleCallbackQuery(update.CallbackQuery)
		w.WriteHeader(http.StatusOK)
		return
	}

	// Skip if no message
	if update.Message.Text == "" {
		a.logger.Debug("Ignoring non-text message")
		w.WriteHeader(http.StatusOK)
		return
	}

	// Skip bot commands (/start, /help, etc.)
	for _, entity := range update.Message.Entities {
		if entity.Type == "bot_command" {
			a.logger.Debug("Ignoring bot command", "text", update.Message.Text)
			w.WriteHeader(http.StatusOK)
			return
		}
	}

	sessionID := a.getOrCreateSession(update.Message.From.ID, update.Message.Chat.ID)

	msg := &ChatMessage{
		Platform:  "telegram",
		SessionID: sessionID,
		UserID:    fmt.Sprintf("%d", update.Message.From.ID),
		Content:   update.Message.Text,
		MessageID: fmt.Sprintf("%d", update.Message.MessageID),
		Timestamp: time.Unix(update.Message.Date, 0),
		Metadata: map[string]any{
			"chat_id":    update.Message.Chat.ID,
			"chat_type":  update.Message.Chat.Type,
			"first_name": update.Message.From.FirstName,
			"last_name":  update.Message.From.LastName,
			"username":   update.Message.From.Username,
		},
	}

	if a.handler != nil {
		go func() {
			if err := a.handler(context.Background(), msg); err != nil {
				a.logger.Error("Handle message failed", "error", err)
			}
		}()
	}

	w.WriteHeader(http.StatusOK)
}

func (a *TelegramAdapter) handleCallbackQuery(query *struct {
	ID   string `json:"id"`
	From struct {
		ID        int64  `json:"id"`
		FirstName string `json:"first_name"`
		LastName  string `json:"last_name"`
		Username  string `json:"username"`
	} `json:"from"`
	Message struct {
		Chat struct {
			ID int64 `json:"id"`
		} `json:"chat"`
		MessageID int64  `json:"message_id"`
		Data      string `json:"data"`
	} `json:"message,omitempty"`
}) {
	a.logger.Debug("Callback query received", "id", query.ID, "data", query.Message.Data)
	// Handle inline keyboard callbacks if needed
}

func (a *TelegramAdapter) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, "OK")
}

func (a *TelegramAdapter) getOrCreateSession(userID int64, chatID int64) string {
	a.mu.Lock()
	defer a.mu.Unlock()

	key := fmt.Sprintf("%d:%d", chatID, userID)
	if session, ok := a.sessions[key]; ok {
		session.LastActive = time.Now()
		return session.SessionID
	}

	session := &TelegramSession{
		SessionID:  fmt.Sprintf("tg-%d", time.Now().UnixNano()),
		UserID:     fmt.Sprintf("%d", userID),
		ChatID:     chatID,
		Platform:   "telegram",
		LastActive: time.Now(),
	}
	a.sessions[key] = session

	a.logger.Info("New session created", "session", session.SessionID, "user", userID, "chat", chatID)
	return session.SessionID
}

func (a *TelegramAdapter) setWebhook(ctx context.Context) error {
	webhookURL := a.config.WebhookURL + "/webhook"
	payload := map[string]string{
		"url": webhookURL,
	}

	// Add secret token if configured
	if a.config.SecretToken != "" {
		payload["secret_token"] = a.config.SecretToken
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/setWebhook", a.config.BotToken)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	var result TelegramWebhookResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}

	if !result.OK {
		return fmt.Errorf("set webhook failed: %s", result.Description)
	}

	a.logger.Info("Webhook set successfully", "url", webhookURL)
	return nil
}

func (a *TelegramAdapter) removeWebhook(ctx context.Context) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/deleteWebhook", a.config.BotToken)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	a.logger.Info("Webhook removed")
	return nil
}
