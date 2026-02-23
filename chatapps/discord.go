package chatapps

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
	"sync"
	"time"
)

// DiscordConfig Discord Bot Configuration
type DiscordConfig struct {
	BotToken   string // Discord Bot Token
	ServerAddr string
	PublicKey  string // For interaction verification
}

// DiscordInteraction Discord interaction payload
type DiscordInteraction struct {
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

// DiscordMessage Discord message structure
type DiscordMessage struct {
	Content string `json:"content"`
}

// DiscordMessageResponse Response from Discord API
type DiscordMessageResponse struct {
	ID        string `json:"id"`
	ChannelID string `json:"channel_id"`
	Content   string `json:"content"`
	Timestamp string `json:"timestamp"`
}

// DiscordGuildMember Guild member info
type DiscordGuildMember struct {
	User struct {
		ID       string `json:"id"`
		Username string `json:"username"`
		Bot      bool   `json:"bot"`
	} `json:"user"`
	Nick     string   `json:"nick"`
	Roles    []string `json:"roles"`
	JoinedAt string   `json:"joined_at"`
}

// DiscordAdapter Discord Bot Adapter
type DiscordAdapter struct {
	config   DiscordConfig
	logger   *slog.Logger
	server   *http.Server
	sessions map[string]*DiscordSession
	mu       sync.RWMutex
	handler  MessageHandler
	running  bool
}

// DiscordSession Session for Discord user
type DiscordSession struct {
	SessionID  string
	UserID     string
	ChannelID  string
	GuildID    string
	Platform   string
	LastActive time.Time
}

// NewDiscordAdapter Create new Discord adapter
func NewDiscordAdapter(config DiscordConfig, logger *slog.Logger) *DiscordAdapter {
	if config.ServerAddr == "" {
		config.ServerAddr = ":8080"
	}
	return &DiscordAdapter{
		config:   config,
		logger:   logger,
		sessions: make(map[string]*DiscordSession),
	}
}

// Platform Returns platform name
func (a *DiscordAdapter) Platform() string {
	return "discord"
}

// SetHandler Set message handler
func (a *DiscordAdapter) SetHandler(handler MessageHandler) {
	a.handler = handler
}

// Start Start Discord adapter
func (a *DiscordAdapter) Start(ctx context.Context) error {
	if a.running {
		return nil
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/webhook/interactions", a.handleInteraction)
	mux.HandleFunc("/health", a.handleHealth)

	a.server = &http.Server{
		Addr:    a.config.ServerAddr,
		Handler: mux,
	}

	go func() {
		a.logger.Info("Starting Discord adapter", "addr", a.config.ServerAddr)
		if err := a.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			a.logger.Error("Discord server error", "error", err)
		}
	}()

	a.running = true
	return nil
}

// Stop Stop Discord adapter
func (a *DiscordAdapter) Stop() error {
	if !a.running {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := a.server.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown server: %w", err)
	}

	a.running = false
	a.logger.Info("Discord adapter stopped")
	return nil
}

// SendMessage Send message to Discord channel
func (a *DiscordAdapter) SendMessage(ctx context.Context, sessionID string, msg *ChatMessage) error {
	channelID, ok := msg.Metadata["channel_id"].(string)
	if !ok || channelID == "" {
		return fmt.Errorf("channel_id not found in metadata")
	}

	// Build payload
	payload := map[string]any{
		"content": msg.Content,
	}

	// Apply rich content if present
	if msg.RichContent != nil {
		// Discord embeds
		if len(msg.RichContent.Embeds) > 0 {
			payload["embeds"] = msg.RichContent.Embeds
		}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	url := fmt.Sprintf("https://discord.com/api/v10/channels/%s/messages", channelID)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bot "+a.config.BotToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("send message: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("send failed: %d %s", resp.StatusCode, string(respBody))
	}

	a.logger.Debug("Message sent", "session", sessionID, "channel", channelID)
	return nil
}

// SendEmbedMessage sends a message with embeds to Discord channel
func (a *DiscordAdapter) SendEmbedMessage(ctx context.Context, sessionID, channelID string, embeds []DiscordEmbed) error {
	payload := map[string]any{
		"embeds": embeds,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	url := fmt.Sprintf("https://discord.com/api/v10/channels/%s/messages", channelID)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bot "+a.config.BotToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("send message: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("send failed: %d %s", resp.StatusCode, string(respBody))
	}

	a.logger.Debug("Embed message sent", "session", sessionID, "channel", channelID)
	return nil
}

// HandleMessage Handle incoming message
func (a *DiscordAdapter) HandleMessage(ctx context.Context, msg *ChatMessage) error {
	return nil
}

func (a *DiscordAdapter) handleInteraction(w http.ResponseWriter, r *http.Request) {
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

	// Verify Ed25519 signature if PublicKey is configured
	if a.config.PublicKey != "" {
		if !a.verifySignature(r, body) {
			a.logger.Warn("Invalid interaction signature")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
	}

	var interaction DiscordInteraction
	if err := json.Unmarshal(body, &interaction); err != nil {
		a.logger.Error("Parse interaction failed", "error", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// Ping interaction for verification
	if interaction.Type == 1 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"type":1}`))
		return
	}

	// Handle message command
	if interaction.Type == 2 || interaction.Type == 3 {
		a.handleMessageCommand(interaction)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"type":5}`))
}

// verifySignature verifies Discord's Ed25519 signature
func (a *DiscordAdapter) verifySignature(r *http.Request, body []byte) bool {
	signature := r.Header.Get("X-Ed25519-Signature")
	timestamp := r.Header.Get("X-Signature-Timestamp")

	if signature == "" || timestamp == "" {
		return false
	}

	// Decode the public key
	publicKeyBytes, err := base64.StdEncoding.DecodeString(a.config.PublicKey)
	if err != nil {
		a.logger.Error("Failed to decode public key", "error", err)
		return false
	}

	if len(publicKeyBytes) != ed25519.PublicKeySize {
		a.logger.Error("Invalid public key length")
		return false
	}

	// Construct the message to verify: timestamp + body
	message := timestamp + string(body)

	// Decode the signature
	signatureBytes, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		a.logger.Error("Failed to decode signature", "error", err)
		return false
	}

	if len(signatureBytes) != ed25519.SignatureSize {
		a.logger.Error("Invalid signature length")
		return false
	}

	// Verify the signature
	publicKey := ed25519.PublicKey(publicKeyBytes)
	return ed25519.Verify(publicKey, []byte(message), signatureBytes)
}

func (a *DiscordAdapter) handleMessageCommand(interaction DiscordInteraction) {
	var data struct {
		Options []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"options"`
	}
	if err := json.Unmarshal(interaction.Data, &data); err != nil {
		a.logger.Error("Failed to unmarshal interaction data", "error", err)
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
			a.logger.Error("Failed to unmarshal interaction message", "error", err)
		}
		messageContent = msg.Content
	}

	if messageContent == "" {
		return
	}

	sessionID := a.getOrCreateSession(interaction.Member.User.ID, interaction.Channel, interaction.GuildID)

	msg := &ChatMessage{
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

	if a.handler != nil {
		go func() {
			if err := a.handler(context.Background(), msg); err != nil {
				a.logger.Error("Handle message failed", "error", err)
			}
		}()
	}
}

func (a *DiscordAdapter) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, "OK")
}

func (a *DiscordAdapter) getOrCreateSession(userID, channelID, guildID string) string {
	a.mu.Lock()
	defer a.mu.Unlock()

	key := channelID + ":" + userID
	if session, ok := a.sessions[key]; ok {
		session.LastActive = time.Now()
		return session.SessionID
	}

	session := &DiscordSession{
		SessionID:  fmt.Sprintf("discord-%d", time.Now().UnixNano()),
		UserID:     userID,
		ChannelID:  channelID,
		GuildID:    guildID,
		Platform:   "discord",
		LastActive: time.Now(),
	}
	a.sessions[key] = session

	a.logger.Info("New session created", "session", session.SessionID, "user", userID, "channel", channelID)
	return session.SessionID
}
