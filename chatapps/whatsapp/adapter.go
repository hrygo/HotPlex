package whatsapp

import (
	"bytes"

	"context"
	"encoding/json"
	"fmt"
	"github.com/hrygo/hotplex/internal/panicx"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/hrygo/hotplex/chatapps/base"
)

type Adapter struct {
	*base.Adapter
	config      Config
	webhookPath string
	sender      *base.SenderWithMutex
	webhook     *base.WebhookRunner
}

func NewAdapter(config Config, logger *slog.Logger, opts ...base.AdapterOption) *Adapter {
	a := &Adapter{
		config:      config,
		webhookPath: "/webhook",
		sender:      base.NewSenderWithMutex(),
		webhook:     base.NewWebhookRunner(logger),
	}

	if config.APIVersion == "" {
		config.APIVersion = "v21.0"
	}

	a.Adapter = base.NewAdapter("whatsapp", base.Config{
		ServerAddr:   config.ServerAddr,
		SystemPrompt: config.SystemPrompt,
	}, logger,
		base.WithHTTPHandler(a.webhookPath, a.handleWebhook),
	)

	for _, opt := range opts {
		opt(a.Adapter)
	}

	// Set default sender if credentials are configured
	if config.AccessToken != "" && config.PhoneNumberID != "" {
		a.sender.SetSender(a.defaultSender)
	}

	return a
}

func (a *Adapter) SendMessage(ctx context.Context, sessionID string, msg *base.ChatMessage) error {
	return a.sender.SendMessage(ctx, sessionID, msg)
}

func (a *Adapter) SetSender(fn func(ctx context.Context, sessionID string, msg *base.ChatMessage) error) {
	a.sender.SetSender(fn)
}

// defaultSender sends message via WhatsApp Cloud API
func (a *Adapter) defaultSender(ctx context.Context, sessionID string, msg *base.ChatMessage) error {
	if a.config.AccessToken == "" || a.config.PhoneNumberID == "" {
		return fmt.Errorf("whatsapp credentials not configured")
	}

	// Extract recipient phone number from session metadata
	recipient := a.extractRecipient(sessionID, msg)
	if recipient == "" {
		return fmt.Errorf("recipient phone number not found in session")
	}

	// Use configured PhoneNumberID or extract from metadata
	phoneNumberID := a.config.PhoneNumberID
	if pnid, ok := msg.Metadata["phone_number_id"].(string); ok && pnid != "" {
		phoneNumberID = pnid
	}

	payload := map[string]any{
		"messaging_product": "whatsapp",
		"to":                recipient,
		"type":              "text",
		"text": map[string]any{
			"body": msg.Content,
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	url := fmt.Sprintf("https://graph.facebook.com/%s/%s/messages", a.config.APIVersion, phoneNumberID)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+a.config.AccessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("send message: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("whatsapp API error: %d %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// extractRecipient extracts recipient phone number from session or message metadata
func (a *Adapter) extractRecipient(sessionID string, msg *base.ChatMessage) string {
	if msg.Metadata == nil {
		return ""
	}
	if recipient, ok := msg.Metadata["recipient"].(string); ok && recipient != "" {
		return recipient
	}
	// Fallback: use UserID as recipient (phone number)
	return msg.UserID
}

// Stop waits for pending webhook goroutines to complete
func (a *Adapter) Stop() error {
	a.webhook.Stop()
	return a.Adapter.Stop()
}

type IncomingMessage struct {
	Object string `json:"object"`
	Entry  []struct {
		ID      string `json:"id"`
		Changes []struct {
			Value struct {
				MessagingProduct string `json:"messaging_product"`
				Metadata         struct {
					DisplayPhoneNumber string `json:"display_phone_number"`
					PhoneNumberID      string `json:"phone_number_id"`
				} `json:"metadata"`
				Messages []struct {
					From string `json:"from"`
					ID   string `json:"id"`
					Text struct {
						Body string `json:"body"`
					} `json:"text"`
					Type string `json:"type"`
				} `json:"messages"`
			} `json:"value"`
		} `json:"changes"`
	} `json:"entry"`
}

func (a *Adapter) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		a.handleVerify(w, r)
		return
	}

	if r.Method == "POST" {
		a.handleMessage(w, r)
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func (a *Adapter) handleVerify(w http.ResponseWriter, r *http.Request) {
	mode := r.URL.Query().Get("hub.mode")
	token := r.URL.Query().Get("hub.verify_token")
	challenge := r.URL.Query().Get("hub.challenge")

	if mode == "subscribe" && token == a.config.VerifyToken {
		a.Logger().Info("WhatsApp webhook verified")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, challenge)
		return
	}

	a.Logger().Warn("WhatsApp verification failed")
	http.Error(w, "Unauthorized", http.StatusUnauthorized)
}

func (a *Adapter) handleMessage(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		a.Logger().Error("Read body failed", "error", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	var incoming IncomingMessage
	if err := json.Unmarshal(body, &incoming); err != nil {
		a.Logger().Error("Parse message failed", "error", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	for _, entry := range incoming.Entry {
		for _, change := range entry.Changes {
			for _, msg := range change.Value.Messages {
				if msg.Type != "text" {
					continue
				}

				sessionID := a.GetOrCreateSession(msg.From, "", msg.From)

				chatMsg := &base.ChatMessage{
					Platform:  "whatsapp",
					SessionID: sessionID,
					UserID:    msg.From,
					Content:   msg.Text.Body,
					MessageID: msg.ID,
					Timestamp: time.Now(),
					Metadata: map[string]any{
						"phone_number_id": change.Value.Metadata.PhoneNumberID,
					},
				}

				if a.Handler() != nil {
					panicx.SafeGo(a.Logger(), func() {
						if err := a.Handler()(r.Context(), chatMsg); err != nil {
							a.Logger().Error("Handle message failed", "error", err)
						}
					})
				}
			}
		}
	}

	w.WriteHeader(http.StatusOK)
}

// Compile-time interface compliance check
var _ base.ChatAdapter = (*Adapter)(nil)
