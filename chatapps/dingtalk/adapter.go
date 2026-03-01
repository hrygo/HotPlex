package dingtalk

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/hrygo/hotplex/chatapps/base"
)

type Adapter struct {
	*base.Adapter
	config      Config
	webhookPath string
	sender      *base.SenderWithMutex
	webhook     *base.WebhookRunner
	token       string
	tokenExpire time.Time
	tokenMu     sync.Mutex
}

func NewAdapter(config Config, logger *slog.Logger, opts ...base.AdapterOption) *Adapter {
	a := &Adapter{
		config:      config,
		webhookPath: "/webhook",
		sender:      base.NewSenderWithMutex(),
		webhook:     base.NewWebhookRunner(logger),
	}

	a.Adapter = base.NewAdapter("dingtalk", base.Config{
		ServerAddr:   config.ServerAddr,
		SystemPrompt: config.SystemPrompt,
	}, logger,
		base.WithHTTPHandler(a.webhookPath, a.handleCallback),
	)

	for _, opt := range opts {
		opt(a.Adapter)
	}

	// Set default sender if AppID and AppSecret are configured
	if config.AppID != "" && config.AppSecret != "" {
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

type CallbackRequest struct {
	MsgType        string `json:"msgtype"`
	ConversationID string `json:"conversationId"`
	SenderID       string `json:"senderId"`
	SenderNick     string `json:"senderNick"`
	IsAdmin        bool   `json:"isAdmin"`
	RobotCode      string `json:"robotCode"`
	Text           struct {
		Content string `json:"content"`
	} `json:"text"`
	EventType string `json:"eventType"`
}

func (a *Adapter) handleCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		a.handleCallbackVerify(w, r)
		return
	}

	if r.Method == "POST" {
		a.handleCallbackMessage(w, r)
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func (a *Adapter) handleCallbackVerify(w http.ResponseWriter, r *http.Request) {
	signature := r.URL.Query().Get("signature")
	timestamp := r.URL.Query().Get("timestamp")
	nonce := r.URL.Query().Get("nonce")

	if a.config.CallbackToken != "" && !a.verifySignature(signature, timestamp, nonce, a.config.CallbackToken) {
		a.Logger().Warn("Invalid callback signature")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, timestamp)
}

func (a *Adapter) handleCallbackMessage(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		a.Logger().Error("Read body failed", "error", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	var callback CallbackRequest
	if err := json.Unmarshal(body, &callback); err != nil {
		a.Logger().Error("Parse callback failed", "error", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	if callback.MsgType != "text" {
		a.Logger().Debug("Ignoring non-text message", "type", callback.MsgType)
		w.WriteHeader(http.StatusOK)
		return
	}

	sessionID := a.GetOrCreateSession(callback.SenderID, "", callback.ConversationID)

	msg := &base.ChatMessage{
		Platform:  "dingtalk",
		SessionID: sessionID,
		UserID:    callback.SenderID,
		Content:   callback.Text.Content,
		MessageID: callback.ConversationID + ":" + callback.SenderID,
		Timestamp: time.Now(),
		Metadata: map[string]any{
			"conversation_id": callback.ConversationID,
			"sender_nick":     callback.SenderNick,
			"robot_code":      callback.RobotCode,
		},
	}

	if a.Handler() != nil {
		a.webhook.Run(r.Context(), a.Handler(), msg)
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"msgtype":"text","text":{"content":"收到消息，正在处理..."}}`))
}

func (a *Adapter) verifySignature(signature, timestamp, nonce, token string) bool {
	stringToSign := timestamp + token + nonce
	mac := hmac.New(sha256.New, []byte(token))
	mac.Write([]byte(stringToSign))
	sign := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return sign == signature
}

func (a *Adapter) GetAccessToken() (string, error) {
	if a.config.AppID == "" || a.config.AppSecret == "" {
		return "", nil
	}

	// Fast path: check if we have a valid token (with lock)
	token, isValid := func() (string, bool) {
		a.tokenMu.Lock()
		defer a.tokenMu.Unlock()
		if a.token != "" && time.Now().Add(5*time.Minute).Before(a.tokenExpire) {
			return a.token, true
		}
		return "", false
	}()
	if isValid {
		return token, nil
	}

	// Slow path: fetch new token
	url := fmt.Sprintf("https://api.dingtalk.com/v1.0/oauth2/oAuth2/accessToken?appKey=%s&appSecret=%s",
		a.config.AppID, a.config.AppSecret)
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	var result struct {
		AccessToken string `json:"accessToken"`
		ExpireIn    int    `json:"expireIn"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	a.tokenMu.Lock()
	a.token = result.AccessToken
	if result.ExpireIn > 300 {
		a.tokenExpire = time.Now().Add(time.Duration(result.ExpireIn-300) * time.Second)
	} else {
		a.tokenExpire = time.Now().Add(time.Duration(result.ExpireIn) * time.Second)
	}
	token = a.token
	a.tokenMu.Unlock()

	return token, nil
}

func (a *Adapter) ChunkMessage(content string) []string {
	maxLen := a.config.MaxMessageLen
	if maxLen <= 0 {
		maxLen = 5000
	}

	if len(content) <= maxLen {
		return []string{content}
	}

	var chunks []string
	lines := strings.Split(content, "\n")
	var currentChunk strings.Builder

	for _, line := range lines {
		if len(line) > maxLen {
			if currentChunk.Len() > 0 {
				chunks = append(chunks, currentChunk.String())
				currentChunk.Reset()
			}
			for len(line) > maxLen {
				chunks = append(chunks, line[:maxLen])
				line = line[maxLen:]
			}
			if len(line) > 0 {
				currentChunk.WriteString(line)
				currentChunk.WriteString("\n")
			}
			continue
		}

		if currentChunk.Len()+len(line)+1 > maxLen {
			if currentChunk.Len() > 0 {
				chunks = append(chunks, currentChunk.String())
				currentChunk.Reset()
			}
		}

		currentChunk.WriteString(line)
		currentChunk.WriteString("\n")
	}

	if currentChunk.Len() > 0 {
		chunks = append(chunks, currentChunk.String())
	}

	return chunks
}

// Stop stops the adapter and waits for pending webhook goroutines
func (a *Adapter) Stop() error {
	a.webhook.Stop()
	return a.Adapter.Stop()
}

// defaultSender sends message via DingTalk Robot API
func (a *Adapter) defaultSender(ctx context.Context, sessionID string, msg *base.ChatMessage) error {
	if a.config.AppID == "" || a.config.AppSecret == "" {
		return fmt.Errorf("dingtalk app credentials not configured")
	}

	// Extract webhook URL from session metadata or use robot code
	webhookURL := a.extractWebhookURL(sessionID, msg)
	if webhookURL == "" {
		return fmt.Errorf("webhook url not found in session")
	}

	// Get access token
	token, err := a.GetAccessToken()
	if err != nil {
		return fmt.Errorf("get access token: %w", err)
	}
	if token == "" {
		return fmt.Errorf("access token is empty")
	}

	// Chunk message if needed
	chunks := a.ChunkMessage(msg.Content)

	for _, chunk := range chunks {
		payload := map[string]any{
			"msgtype": "text",
			"text": map[string]any{
				"content": chunk,
			},
		}

		body, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal payload: %w", err)
		}

		url := fmt.Sprintf("%s?access_token=%s", webhookURL, token)
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

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("dingtalk API error: %d", resp.StatusCode)
		}
	}

	return nil
}

// extractWebhookURL extracts webhook URL from session or message metadata
func (a *Adapter) extractWebhookURL(sessionID string, msg *base.ChatMessage) string {
	if msg.Metadata == nil {
		return ""
	}
	if webhookURL, ok := msg.Metadata["webhook_url"].(string); ok && webhookURL != "" {
		return webhookURL
	}
	// Fallback: construct webhook URL from robot code
	if robotCode, ok := msg.Metadata["robot_code"].(string); ok && robotCode != "" {
		return "https://api.dingtalk.com/v1.0/robot/message/sendToConversation"
	}
	return ""
}

// Compile-time interface compliance check
var _ base.ChatAdapter = (*Adapter)(nil)

// =============================================================================
// MessageOperations interface implementation (graceful fallback for unsupported ops)
// =============================================================================

// DeleteMessage is not supported in DingTalk
func (a *Adapter) DeleteMessage(ctx context.Context, channelID, messageTS string) error {
	a.Logger().Debug("DeleteMessage not supported on DingTalk", "channel_id", channelID, "message_ts", messageTS)
	return nil // Graceful fallback: no-op
}

// AddReaction is not supported in DingTalk
func (a *Adapter) AddReaction(ctx context.Context, reaction base.Reaction) error {
	a.Logger().Debug("AddReaction not supported on DingTalk", "reaction", reaction.Name)
	return nil // Graceful fallback: no-op
}

// RemoveReaction is not supported in DingTalk
func (a *Adapter) RemoveReaction(ctx context.Context, reaction base.Reaction) error {
	a.Logger().Debug("RemoveReaction not supported on DingTalk", "reaction", reaction.Name)
	return nil // Graceful fallback: no-op
}

// UpdateMessage is not supported in DingTalk
func (a *Adapter) UpdateMessage(ctx context.Context, channelID, messageTS string, msg *base.ChatMessage) error {
	a.Logger().Debug("UpdateMessage not supported on DingTalk", "channel_id", channelID, "message_ts", messageTS)
	return nil // Graceful fallback: no-op
}
