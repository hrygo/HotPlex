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
	sender      func(ctx context.Context, sessionID string, msg *base.ChatMessage) error
}

func NewAdapter(config Config, logger *slog.Logger) *Adapter {
	a := &Adapter{
		config:      config,
		webhookPath: "/webhook/interactions",
	}

	a.Adapter = base.NewAdapter("discord", base.Config{
		ServerAddr:   config.ServerAddr,
		SystemPrompt: config.SystemPrompt,
	}, logger,
		base.WithHTTPHandler(a.webhookPath, a.handleInteraction),
	)

	return a
}

func (a *Adapter) SendMessage(ctx context.Context, sessionID string, msg *base.ChatMessage) error {
	return a.sender(ctx, sessionID, msg)
}

func (a *Adapter) SetSender(fn func(ctx context.Context, sessionID string, msg *base.ChatMessage) error) {
	a.sender = fn
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

	sessionID := a.GetOrCreateSession(interaction.Channel+":"+interaction.Member.User.ID, interaction.Member.User.ID)

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

	if a.Handler() != nil {
		go func() {
			if err := a.Handler()(ctx, msg); err != nil {
				a.Logger().Error("Handle message failed", "error", err)
			}
		}()
	}
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
