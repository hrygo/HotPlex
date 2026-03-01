package discord

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
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
	webhookPath string
	sender      *base.SenderWithMutex
	webhook     *base.WebhookRunner
}

func NewAdapter(config Config, logger *slog.Logger, opts ...base.AdapterOption) *Adapter {
	a := &Adapter{
		config:      config,
		webhookPath: "/webhook/interactions",
		sender:      base.NewSenderWithMutex(),
		webhook:     base.NewWebhookRunner(logger),
	}

	a.Adapter = base.NewAdapter("discord", base.Config{
		ServerAddr:   config.ServerAddr,
		SystemPrompt: config.SystemPrompt,
	}, logger,
		base.WithHTTPHandler(a.webhookPath, a.handleInteraction),
	)

	for _, opt := range opts {
		opt(a.Adapter)
	}

	// Set default sender if BotToken is configured
	if config.BotToken != "" {
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

// defaultSender sends message via Discord Bot API
func (a *Adapter) defaultSender(ctx context.Context, sessionID string, msg *base.ChatMessage) error {
	if a.config.BotToken == "" {
		return fmt.Errorf("discord bot token not configured")
	}

	// Extract channel_id from session or message metadata
	channelID := a.extractChannelID(sessionID, msg)
	if channelID == "" {
		return fmt.Errorf("channel_id not found in session")
	}

	return a.SendToChannel(ctx, channelID, msg.Content)
}

// extractChannelID extracts channel_id from session or message metadata
func (a *Adapter) extractChannelID(sessionID string, msg *base.ChatMessage) string {
	if msg.Metadata == nil {
		return ""
	}
	if channelID, ok := msg.Metadata["channel_id"].(string); ok {
		return channelID
	}
	return ""
}

type Interaction struct {
	Type    int             `json:"type"`
	Data    json.RawMessage `json:"data"`
	GuildID string          `json:"guild_id"`
	Channel string          `json:"channel_id"`
	Member  struct {
		User struct {
			ID       string `json:"id"`
			Username string `json:"username"`
		} `json:"user"`
	} `json:"member"`
	Message json.RawMessage `json:"message"`
}

func (a *Adapter) handleInteraction(w http.ResponseWriter, r *http.Request) {
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

	if a.config.PublicKey != "" {
		if !a.verifySignature(r, body) {
			a.Logger().Warn("Invalid interaction signature")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
	}

	var interaction Interaction
	if err := json.Unmarshal(body, &interaction); err != nil {
		a.Logger().Error("Parse interaction failed", "error", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	if interaction.Type == 1 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"type":1}`))
		return
	}

	if interaction.Type == 2 || interaction.Type == 3 {
		a.handleMessageCommand(r.Context(), interaction)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"type":5}`))
}

func (a *Adapter) verifySignature(r *http.Request, body []byte) bool {
	signature := r.Header.Get("X-Ed25519-Signature")
	timestamp := r.Header.Get("X-Signature-Timestamp")

	if signature == "" || timestamp == "" {
		return false
	}

	publicKeyBytes, err := base64.StdEncoding.DecodeString(a.config.PublicKey)
	if err != nil {
		a.Logger().Error("Failed to decode public key", "error", err)
		return false
	}

	if len(publicKeyBytes) != ed25519.PublicKeySize {
		a.Logger().Error("Invalid public key length")
		return false
	}

	message := timestamp + string(body)

	signatureBytes, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		a.Logger().Error("Failed to decode signature", "error", err)
		return false
	}

	if len(signatureBytes) != ed25519.SignatureSize {
		a.Logger().Error("Invalid signature length")
		return false
	}

	publicKey := ed25519.PublicKey(publicKeyBytes)
	return ed25519.Verify(publicKey, []byte(message), signatureBytes)
}

func (a *Adapter) handleMessageCommand(ctx context.Context, interaction Interaction) {
	var data struct {
		Options []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"options"`
	}
	if err := json.Unmarshal(interaction.Data, &data); err != nil {
		a.Logger().Error("Failed to unmarshal interaction data", "error", err)
	}

	var messageContent string
	for _, opt := range data.Options {
		if opt.Name == "message" || opt.Name == "content" {
			messageContent = opt.Value
			break
		}
	}

	if messageContent == "" && interaction.Message != nil {
		var msg struct {
			Content string `json:"content"`
		}
		if err := json.Unmarshal(interaction.Message, &msg); err != nil {
			a.Logger().Error("Failed to unmarshal interaction message", "error", err)
		}
		messageContent = msg.Content
	}

	if messageContent == "" {
		return
	}

	// Note: GuildID is used as botUserID parameter to scope sessions per Discord server
	// This ensures users have different sessions in different Guilds
	sessionID := a.GetOrCreateSession(interaction.Member.User.ID, interaction.GuildID, interaction.Channel)

	msg := &base.ChatMessage{
		Platform:  "discord",
		SessionID: sessionID,
		UserID:    interaction.Member.User.ID,
		Content:   messageContent,
		Timestamp: time.Now(),
		Metadata: map[string]any{
			"channel_id": interaction.Channel,
			"guild_id":   interaction.GuildID,
			"username":   interaction.Member.User.Username,
		},
	}

	a.webhook.Run(ctx, a.Handler(), msg)
}

// Stop waits for pending webhook goroutines to complete
func (a *Adapter) Stop() error {
	a.webhook.Stop()
	return a.Adapter.Stop()
}

func (a *Adapter) SendToChannel(ctx context.Context, channelID, content string) error {
	payload := map[string]any{"content": content}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("https://discord.com/api/v10/channels/%s/messages", channelID)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bot "+a.config.BotToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("send failed with status %d (failed to read body: %v)", resp.StatusCode, err)
		}
		return fmt.Errorf("send failed: %d %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// Compile-time interface compliance check
var _ base.ChatAdapter = (*Adapter)(nil)

// =============================================================================
// MessageOperations interface implementation (graceful fallback for unsupported ops)
// =============================================================================

// DeleteMessage is not fully supported in Discord (can only delete own messages within 15 min)
func (a *Adapter) DeleteMessage(ctx context.Context, channelID, messageTS string) error {
	a.Logger().Debug("DeleteMessage not fully supported on Discord", "channel_id", channelID, "message_ts", messageTS)
	return nil // Graceful fallback: no-op
}

// AddReaction is not supported in Discord
func (a *Adapter) AddReaction(ctx context.Context, reaction base.Reaction) error {
	a.Logger().Debug("AddReaction not supported on Discord", "reaction", reaction.Name)
	return nil // Graceful fallback: no-op
}

// RemoveReaction is not supported in Discord
func (a *Adapter) RemoveReaction(ctx context.Context, reaction base.Reaction) error {
	a.Logger().Debug("RemoveReaction not supported on Discord", "reaction", reaction.Name)
	return nil // Graceful fallback: no-op
}

// UpdateMessage is supported in Discord
func (a *Adapter) UpdateMessage(ctx context.Context, channelID, messageTS string, msg *base.ChatMessage) error {
	// Discord supports message editing - implement if needed
	// For now, return nil as graceful fallback
	a.Logger().Debug("UpdateMessage not implemented for Discord", "channel_id", channelID, "message_ts", messageTS)
	return nil
}
