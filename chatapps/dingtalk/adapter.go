package dingtalk

import (
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
	sender      func(ctx context.Context, sessionID string, msg *base.ChatMessage) error
	token       string
	tokenExpire time.Time
	tokenMu     sync.Mutex
}

func NewAdapter(config Config, logger *slog.Logger) *Adapter {
	a := &Adapter{
		config:      config,
		webhookPath: "/webhook",
	}

	a.Adapter = base.NewAdapter("dingtalk", base.Config{
		ServerAddr:   config.ServerAddr,
		SystemPrompt: config.SystemPrompt,
	}, logger,
		base.WithHTTPHandler(a.webhookPath, a.handleCallback),
	)

	return a
}

func (a *Adapter) SendMessage(ctx context.Context, sessionID string, msg *base.ChatMessage) error {
	return a.sender(ctx, sessionID, msg)
}

func (a *Adapter) SetSender(fn func(ctx context.Context, sessionID string, msg *base.ChatMessage) error) {
	a.sender = fn
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

	sessionID := a.GetOrCreateSession(callback.ConversationID+":"+callback.SenderID, callback.SenderID)

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
		go func() {
			if err := a.Handler()(r.Context(), msg); err != nil {
				a.Logger().Error("Handle message failed", "error", err)
			}
		}()
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

	a.tokenMu.Lock()
	if a.token != "" && time.Now().Add(5*time.Minute).Before(a.tokenExpire) {
		token := a.token
		a.tokenMu.Unlock()
		return token, nil
	}
	a.tokenMu.Unlock()

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
	a.tokenMu.Unlock()

	return result.AccessToken, nil
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
