package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/hrygo/hotplex/chatapps/base"
)

type Adapter struct {
	*base.Adapter
	config      Config
	rateLimiter *RateLimiter
	webhookPath string
	sender      func(ctx context.Context, sessionID string, msg *base.ChatMessage) error
}

type RateLimiter struct {
	tokens     float64
	maxTokens  float64
	refillRate float64
	lastRefill time.Time
}

func NewRateLimiter(maxTokens, refillRate float64) *RateLimiter {
	return &RateLimiter{
		tokens:     maxTokens,
		maxTokens:  maxTokens,
		refillRate: refillRate,
		lastRefill: time.Now(),
	}
}

func (r *RateLimiter) Allow() bool {
	now := time.Now()
	elapsed := now.Sub(r.lastRefill).Seconds()
	r.tokens += elapsed * r.refillRate
	if r.tokens > r.maxTokens {
		r.tokens = r.maxTokens
	}
	r.lastRefill = now

	if r.tokens >= 1 {
		r.tokens--
		return true
	}
	return false
}

func (r *RateLimiter) Wait(ctx context.Context) error {
	for {
		if r.Allow() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func NewAdapter(config Config, logger *slog.Logger) *Adapter {
	a := &Adapter{
		config:      config,
		rateLimiter: NewRateLimiter(30, 10),
		webhookPath: "/webhook",
	}

	a.Adapter = base.NewAdapter("telegram", base.Config{
		ServerAddr:   config.ServerAddr,
		SystemPrompt: config.SystemPrompt,
	}, logger,
		base.WithHTTPHandler(a.webhookPath, a.handleWebhook),
	)

	return a
}

func (a *Adapter) SendMessage(ctx context.Context, sessionID string, msg *base.ChatMessage) error {
	if err := a.rateLimiter.Wait(ctx); err != nil {
		return fmt.Errorf("rate limited: %w", err)
	}
	return a.sender(ctx, sessionID, msg)
}

func (a *Adapter) SetSender(fn func(ctx context.Context, sessionID string, msg *base.ChatMessage) error) {
	a.sender = fn
}

type Update struct {
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

type MessageResponse struct {
	OK     bool `json:"ok"`
	Result *Msg `json:"result"`
}

type Msg struct {
	MessageID int64 `json:"message_id"`
	Chat      struct {
		ID int64 `json:"id"`
	} `json:"chat"`
	Date int64  `json:"date"`
	Text string `json:"text"`
}

func (a *Adapter) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		a.Adapter.Logger().Error("Read body failed", "error", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	if a.config.SecretToken != "" {
		token := r.Header.Get("X-Telegram-Bot-Api-Secret-Token")
		if token != a.config.SecretToken {
			a.Adapter.Logger().Warn("Invalid secret token")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
	}

	var update Update
	if err := json.Unmarshal(body, &update); err != nil {
		a.Adapter.Logger().Error("Parse update failed", "error", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	if update.CallbackQuery != nil {
		a.Adapter.Logger().Debug("Callback query received", "id", update.CallbackQuery.ID)
		w.WriteHeader(http.StatusOK)
		return
	}

	if update.Message.Text == "" {
		a.Adapter.Logger().Debug("Ignoring non-text message")
		w.WriteHeader(http.StatusOK)
		return
	}

	for _, entity := range update.Message.Entities {
		if entity.Type == "bot_command" {
			a.Adapter.Logger().Debug("Ignoring bot command", "text", update.Message.Text)
			w.WriteHeader(http.StatusOK)
			return
		}
	}

	sessionID := a.GetOrCreateSession(
		fmt.Sprintf("%d:%d", update.Message.Chat.ID, update.Message.From.ID),
		fmt.Sprintf("%d", update.Message.From.ID),
	)

	msg := &base.ChatMessage{
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

	if a.Handler() != nil {
		reqCtx := r.Context()
		go func() {
			if err := a.Handler()(reqCtx, msg); err != nil {
				a.Logger().Error("Handle message failed", "error", err)
			}
		}()
	}

	w.WriteHeader(http.StatusOK)
}

func (a *Adapter) SetWebhook(ctx context.Context) error {
	if a.config.WebhookURL == "" {
		return nil
	}

	webhookURL := a.config.WebhookURL + a.webhookPath
	payload := map[string]string{"url": webhookURL}

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

	var result struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode response failed: %w", err)
	}

	if !result.OK {
		return fmt.Errorf("set webhook failed: %s", result.Description)
	}

	a.Adapter.Logger().Info("Webhook set successfully", "url", webhookURL)
	return nil
}

func (a *Adapter) DeleteWebhook(ctx context.Context) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/deleteWebhook", a.config.BotToken)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	return nil
}

func (a *Adapter) Start(ctx context.Context) error {
	if err := a.Adapter.Start(ctx); err != nil {
		return err
	}
	return a.SetWebhook(ctx)
}

func (a *Adapter) Stop() error {
	if a.config.WebhookURL != "" {
		if err := a.DeleteWebhook(context.Background()); err != nil {
			a.Logger().Warn("Delete webhook failed", "error", err)
		}
	}
	return a.Adapter.Stop()
}

func (a *Adapter) Logger() *slog.Logger {
	return a.Adapter.Logger()
}

func (a *Adapter) SetLogger(logger *slog.Logger) {
	a.Adapter.SetLogger(logger)
}
