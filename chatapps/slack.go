package chatapps

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// SlackConfig Slack Bot Configuration
type SlackConfig struct {
	BotToken      string // xoxb- token
	AppToken      string // xapp- token for Socket Mode
	SigningSecret string
	ServerAddr    string
}

// SlackEvent Slack Event API payload
type SlackEvent struct {
	Token     string          `json:"token"`
	TeamID    string          `json:"team_id"`
	APIAppID  string          `json:"api_app_id"`
	Type      string          `json:"type"`
	EventID   string          `json:"event_id"`
	EventTime int64           `json:"event_time"`
	Event     json.RawMessage `json:"event"`
	Challenge string          `json:"challenge"`
}

// SlackMessageEvent Message event from Slack
type SlackMessageEvent struct {
	Type        string `json:"type"`
	Channel     string `json:"channel"`
	ChannelType string `json:"channel_type"`
	User        string `json:"user"`
	Text        string `json:"text"`
	TS          string `json:"ts"`
	EventTS     string `json:"event_ts"`
	BotID       string `json:"bot_id,omitempty"`
	SubType     string `json:"subtype,omitempty"`
}

// SlackInteractivePayload Interactive payload from Slack
type SlackInteractivePayload struct {
	Type           string          `json:"type"`
	Challenge      string          `json:"challenge,omitempty"`
	Token          string          `json:"token"`
	TeamID         string          `json:"team_id"`
	TeamDomain     string          `json:"team_domain"`
	EnterpriseID   string          `json:"enterprise_id,omitempty"`
	EnterpriseName string          `json:"enterprise_name,omitempty"`
	ChannelID      string          `json:"channel_id"`
	ChannelName    string          `json:"channel_name"`
	UserID         string          `json:"user_id"`
	UserName       string          `json:"user_name"`
	Command        string          `json:"command,omitempty"`
	Text           string          `json:"text"`
	ResponseURL    string          `json:"response_url"`
	TriggerID      string          `json:"trigger_id"`
	View           json.RawMessage `json:"view,omitempty"`
}

// SlackEventResponse Response to Slack URL verification
type SlackEventResponse struct {
	Challenge string `json:"challenge"`
}

// SlackAdapter Slack Bot Adapter
type SlackAdapter struct {
	config        SlackConfig
	logger        *slog.Logger
	server        *http.Server
	sessions      map[string]*SlackSession
	mu            sync.RWMutex
	handler       MessageHandler
	running       bool
	cleanupCtx    context.Context
	cleanupCancel context.CancelFunc
}

// SlackSession Session for Slack user
type SlackSession struct {
	SessionID  string
	UserID     string
	ChannelID  string
	Platform   string
	LastActive time.Time
}

// NewSlackAdapter Create new Slack adapter
func NewSlackAdapter(config SlackConfig, logger *slog.Logger) *SlackAdapter {
	if config.ServerAddr == "" {
		config.ServerAddr = ":8080"
	}
	return &SlackAdapter{
		config:   config,
		logger:   logger,
		sessions: make(map[string]*SlackSession),
	}
}

// Platform Returns platform name
func (a *SlackAdapter) Platform() string {
	return "slack"
}

// SetHandler Set message handler
func (a *SlackAdapter) SetHandler(handler MessageHandler) {
	a.handler = handler
}

// Start Start Slack adapter
func (a *SlackAdapter) Start(ctx context.Context) error {
	if a.running {
		return nil
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/webhook/events", a.handleEvent)
	mux.HandleFunc("/webhook/interactive", a.handleInteractive)
	mux.HandleFunc("/health", a.handleHealth)

	a.server = &http.Server{
		Addr:    a.config.ServerAddr,
		Handler: mux,
	}

	go func() {
		a.logger.Info("Starting Slack adapter", "addr", a.config.ServerAddr)
		if err := a.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			a.logger.Error("Slack server error", "error", err)
		}
	}()

	a.running = true
	// Start session cleanup goroutine
	a.cleanupCtx, a.cleanupCancel = context.WithCancel(context.Background())
	go a.cleanupSessions()
	return nil
}

// Stop Stop Slack adapter
func (a *SlackAdapter) Stop() error {
	if !a.running {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := a.server.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown server: %w", err)
	}
	// Stop session cleanup
	if a.cleanupCancel != nil {
		a.cleanupCancel()
	}
	a.running = false
	a.logger.Info("Slack adapter stopped")
	return nil
}

// SendMessage Send message to Slack channel/user
func (a *SlackAdapter) SendMessage(ctx context.Context, sessionID string, msg *ChatMessage) error {
	channelID, ok := msg.Metadata["channel_id"].(string)
	if !ok || channelID == "" {
		return fmt.Errorf("channel_id not found in metadata")
	}

	payload := map[string]any{
		"channel": channelID,
		"text":    msg.Content,
	}

	// Apply rich content if present
	if msg.RichContent != nil {
		// Parse mode (mrkdwn for Slack)
		if msg.RichContent.ParseMode == ParseModeMarkdown {
			payload["mrkdwn"] = true
		}

		// Block Kit blocks
		if len(msg.RichContent.Blocks) > 0 {
			payload["blocks"] = msg.RichContent.Blocks
		}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://slack.com/api/chat.postMessage", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.config.BotToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("send message: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("send failed: %d %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}

	if !result.OK {
		return fmt.Errorf("slack error: %s", result.Error)
	}

	a.logger.Debug("Message sent", "session", sessionID, "channel", channelID)
	return nil
}

// SendBlockMessage sends a message with Block Kit blocks
func (a *SlackAdapter) SendBlockMessage(ctx context.Context, sessionID, channelID string, blocks []SlackBlock, text string) error {
	payload := map[string]any{
		"channel": channelID,
		"blocks":  blocks,
	}

	if text != "" {
		payload["text"] = text // Fallback text for notifications
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://slack.com/api/chat.postMessage", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.config.BotToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("send message: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("send failed: %d %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}

	if !result.OK {
		return fmt.Errorf("slack error: %s", result.Error)
	}

	a.logger.Debug("Block message sent", "session", sessionID, "channel", channelID)
	return nil
}

// HandleMessage Handle incoming message
func (a *SlackAdapter) HandleMessage(ctx context.Context, msg *ChatMessage) error {
	return nil
}

func (a *SlackAdapter) handleEvent(w http.ResponseWriter, r *http.Request) {
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
	// Verify signature if SigningSecret is configured
	if a.config.SigningSecret != "" {
		signature := r.Header.Get("X-Slack-Signature")
		timestamp := r.Header.Get("X-Slack-Request-Timestamp")
		if signature == "" || timestamp == "" {
			a.logger.Warn("Missing signature headers")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		if !a.verifySignature(body, timestamp, signature) {
			a.logger.Warn("Invalid signature")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
	}

	var event SlackEvent
	if err := json.Unmarshal(body, &event); err != nil {
		a.logger.Error("Parse event failed", "error", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// URL verification challenge
	if event.Challenge != "" {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(event.Challenge))
		return
	}

	// Verify token
	if event.Token != a.config.BotToken && event.Token != a.config.AppToken {
		a.logger.Warn("Invalid token", "token", event.Token)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Handle event
	if event.Type == "event_callback" {
		a.handleEventCallback(event.Event)
	}

	w.WriteHeader(http.StatusOK)
}

func (a *SlackAdapter) handleEventCallback(eventData json.RawMessage) {
	var msgEvent SlackMessageEvent
	if err := json.Unmarshal(eventData, &msgEvent); err != nil {
		a.logger.Error("Parse message event failed", "error", err)
		return
	}

	// Skip bot messages and subtypes
	if msgEvent.BotID != "" || msgEvent.SubType != "" && msgEvent.SubType != "message_changed" {
		a.logger.Debug("Skipping bot message", "subtype", msgEvent.SubType)
		return
	}

	// Skip empty text
	if msgEvent.Text == "" {
		return
	}

	sessionID := a.getOrCreateSession(msgEvent.User, msgEvent.Channel)

	msg := &ChatMessage{
		Platform:  "slack",
		SessionID: sessionID,
		UserID:    msgEvent.User,
		Content:   msgEvent.Text,
		MessageID: msgEvent.TS,
		Timestamp: time.Now(),
		Metadata: map[string]any{
			"channel_id":   msgEvent.Channel,
			"channel_type": msgEvent.ChannelType,
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

func (a *SlackAdapter) handleInteractive(w http.ResponseWriter, r *http.Request) {
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

	var payload SlackInteractivePayload
	if err := json.Unmarshal(body, &payload); err != nil {
		a.logger.Error("Parse interactive payload failed", "error", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	a.logger.Debug("Interactive payload received", "type", payload.Type, "user", payload.UserID)

	w.WriteHeader(http.StatusOK)
}

func (a *SlackAdapter) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, "OK")
}

func (a *SlackAdapter) getOrCreateSession(userID, channelID string) string {
	a.mu.Lock()
	defer a.mu.Unlock()

	key := channelID + ":" + userID
	if session, ok := a.sessions[key]; ok {
		session.LastActive = time.Now()
		return session.SessionID
	}

	session := &SlackSession{
		SessionID:  fmt.Sprintf("slack-%d", time.Now().UnixNano()),
		UserID:     userID,
		ChannelID:  channelID,
		Platform:   "slack",
		LastActive: time.Now(),
	}
	a.sessions[key] = session

	a.logger.Info("New session created", "session", session.SessionID, "user", userID, "channel", channelID)
	return session.SessionID
}

// verifySignature verifies Slack webhook signature using HMAC-SHA256
func (a *SlackAdapter) verifySignature(body []byte, timestamp, signature string) bool {
	// Check timestamp to prevent replay attacks (within 5 minutes)
	parsedTS := strings.TrimPrefix(timestamp, "v0=")
	var ts int64
	if _, err := fmt.Sscanf(parsedTS, "%d", &ts); err != nil {
		a.logger.Warn("Failed to parse timestamp", "timestamp", parsedTS)
		return false
	}
	now := time.Now().Unix()
	if abs(now-ts) > 60*5 {
		a.logger.Warn("Timestamp too old", "timestamp", ts, "now", now)
		return false
	}

	// Create signature base string: v0:timestamp:body
	baseString := fmt.Sprintf("v0:%s:%s", parsedTS, string(body))

	// Compute HMAC-SHA256
	h := hmac.New(sha256.New, []byte(a.config.SigningSecret))
	h.Write([]byte(baseString))
	signatureComputed := "v0=" + hex.EncodeToString(h.Sum(nil))

	return hmac.Equal([]byte(signatureComputed), []byte(signature))
}

// abs returns absolute value of int64
func abs(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

// cleanupSessions periodically removes stale sessions
func (a *SlackAdapter) cleanupSessions() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-a.cleanupCtx.Done():
			a.logger.Info("Session cleanup stopped")
			return
		case <-ticker.C:
			a.mu.Lock()
			now := time.Now()
			for key, session := range a.sessions {
				if now.Sub(session.LastActive) > 10*time.Minute {
					delete(a.sessions, key)
					a.logger.Debug("Session removed", "session", session.SessionID, "inactive", now.Sub(session.LastActive))
				}
			}
			a.mu.Unlock()
		}
	}
}
