package base

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/hrygo/hotplex/chatapps/session"
	"github.com/hrygo/hotplex/plugins/storage"
	"github.com/hrygo/hotplex/types"
)

// RetryConfig 重试配置
type RetryConfig struct {
	MaxAttempts  int
	InitialDelay time.Duration
	MaxDelay     time.Duration
	Multiplier   float64
}

// DefaultRetryConfig 默认重试配置
var DefaultRetryConfig = RetryConfig{
	MaxAttempts:  3,
	InitialDelay: 100 * time.Millisecond,
	MaxDelay:     2 * time.Second,
	Multiplier:   2.0,
}

// ErrStorageRetryExhausted 存储重试次数耗尽
var ErrStorageRetryExhausted = errors.New("storage retry exhausted")

// withRetry 带重试的存储操作 (接受 Logger 接口)
func withRetry(ctx context.Context, logger Logger, op string, fn func() error) error {
	cfg := DefaultRetryConfig
	delay := cfg.InitialDelay

	var lastErr error
	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		err := fn()
		if err == nil {
			return nil
		}

		lastErr = err
		logger.Warn("storage operation failed, will retry",
			"op", op,
			"attempt", attempt,
			"max_attempts", cfg.MaxAttempts,
			"error", err.Error(),
			"next_delay", delay.String())

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}

		delay = time.Duration(float64(delay) * cfg.Multiplier)
		if delay > cfg.MaxDelay {
			delay = cfg.MaxDelay
		}
	}

	logger.Error("storage retry exhausted",
		"op", op,
		"attempts", cfg.MaxAttempts,
		"last_error", lastErr.Error())

	return errors.Join(ErrStorageRetryExhausted, lastErr)
}

// slogLogger 实现 Logger 接口
type slogLogger struct {
	logger *slog.Logger
}

func (l *slogLogger) Warn(msg string, args ...any) {
	l.logger.Warn(msg, args...)
}

func (l *slogLogger) Error(msg string, args ...any) {
	l.logger.Error(msg, args...)
}

// MessageDirection 消息方向
type MessageDirection string

const (
	DirectionUserToBot MessageDirection = "user_to_bot"
	DirectionBotToUser MessageDirection = "bot_to_user"
)

// MessageContext 消息存储上下文
type MessageContext struct {
	ChatSessionID     string
	ChatPlatform      string
	ChatUserID        string
	ChatBotUserID     string
	ChatChannelID     string
	ChatThreadID      string
	EngineSessionID   uuid.UUID
	EngineNamespace   string
	ProviderSessionID string
	ProviderType      string
	MessageType       types.MessageType
	Direction         MessageDirection
	Content           string
	Metadata          map[string]any
	RequestID         string
	TraceID           string
}

// MessageContextBuilder 构建器模式
type MessageContextBuilder struct {
	ctx *MessageContext
}

func NewMessageContextBuilder() *MessageContextBuilder {
	return &MessageContextBuilder{
		ctx: &MessageContext{Metadata: make(map[string]any)},
	}
}

func (b *MessageContextBuilder) WithChatSession(sessionID, platform, userID, botUserID, channelID, threadID string) *MessageContextBuilder {
	b.ctx.ChatSessionID = sessionID
	b.ctx.ChatPlatform = platform
	b.ctx.ChatUserID = userID
	b.ctx.ChatBotUserID = botUserID
	b.ctx.ChatChannelID = channelID
	b.ctx.ChatThreadID = threadID
	return b
}

func (b *MessageContextBuilder) WithEngineSession(sessionID uuid.UUID, namespace string) *MessageContextBuilder {
	b.ctx.EngineSessionID = sessionID
	b.ctx.EngineNamespace = namespace
	return b
}

func (b *MessageContextBuilder) WithProviderSession(sessionID, providerType string) *MessageContextBuilder {
	b.ctx.ProviderSessionID = sessionID
	b.ctx.ProviderType = providerType
	return b
}

func (b *MessageContextBuilder) WithMessage(msgType types.MessageType, direction MessageDirection, content string) *MessageContextBuilder {
	b.ctx.MessageType = msgType
	b.ctx.Direction = direction
	b.ctx.Content = content
	return b
}

func (b *MessageContextBuilder) WithMetadata(key string, value any) *MessageContextBuilder {
	b.ctx.Metadata[key] = value
	return b
}

func (b *MessageContextBuilder) Build() (*MessageContext, error) {
	if err := b.ctx.Validate(); err != nil {
		return nil, err
	}
	return b.ctx, nil
}

func (mc *MessageContext) Validate() error {
	if mc.ChatSessionID == "" {
		return ErrMissingChatSessionID
	}
	if mc.EngineSessionID == uuid.Nil {
		return ErrMissingEngineSessionID
	}
	if mc.ProviderSessionID == "" {
		return ErrMissingProviderSessionID
	}
	if mc.Content == "" {
		return ErrMissingContent
	}
	return nil
}

// MessageStorePlugin 消息存储插件 (协调器，SRP)
type MessageStorePlugin struct {
	store       storage.ChatAppMessageStore
	sessionMgr  session.SessionManager
	strategy    storage.StorageStrategy
	streamStore *StreamMessageStore
	logger      Logger
}

// MessageStorePluginConfig 配置
type MessageStorePluginConfig struct {
	Store            storage.ChatAppMessageStore
	SessionManager   session.SessionManager
	Strategy         storage.StorageStrategy
	StreamEnabled    bool
	StreamTimeout    time.Duration
	StreamMaxBuffers int
	Logger           *slog.Logger
}

func NewMessageStorePlugin(cfg MessageStorePluginConfig) (*MessageStorePlugin, error) {
	if cfg.Store == nil {
		return nil, ErrNilStore
	}
	if cfg.SessionManager == nil {
		return nil, ErrNilSessionManager
	}

	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	plugin := &MessageStorePlugin{
		store:      cfg.Store,
		sessionMgr: cfg.SessionManager,
		strategy:   cfg.Strategy,
		logger:     &slogLogger{logger: logger},
	}

	if cfg.StreamEnabled {
		timeout := cfg.StreamTimeout
		if timeout == 0 {
			timeout = 5 * time.Minute
		}
		maxBuffers := cfg.StreamMaxBuffers
		if maxBuffers == 0 {
			maxBuffers = 1000
		}
		plugin.streamStore = NewStreamMessageStore(cfg.Store, timeout, maxBuffers, logger)
	}

	return plugin, nil
}

func (p *MessageStorePlugin) OnUserMessage(ctx context.Context, msgCtx *MessageContext) error {
	if msgCtx.MessageType != types.MessageTypeUserInput {
		return nil
	}

	transformer := NewMessageTransformer()
	if err := transformer.Handle(ctx, msgCtx); err != nil {
		return err
	}

	chatMsg := transformer.ToStorageMessage(msgCtx, true)

	if p.strategy != nil && !p.strategy.ShouldStore(chatMsg) {
		return nil
	}

	return withRetry(ctx, p.logger, "StoreUserMessage", func() error {
		return p.store.StoreUserMessage(ctx, chatMsg)
	})
}

func (p *MessageStorePlugin) OnBotResponse(ctx context.Context, msgCtx *MessageContext) error {
	if msgCtx.MessageType != types.MessageTypeFinalResponse {
		return nil
	}

	transformer := NewMessageTransformer()
	if err := transformer.Handle(ctx, msgCtx); err != nil {
		return err
	}

	chatMsg := transformer.ToStorageMessage(msgCtx, false)

	if p.strategy != nil && !p.strategy.ShouldStore(chatMsg) {
		return nil
	}

	if p.streamStore != nil {
		return p.streamStore.OnStreamChunk(ctx, msgCtx.ChatSessionID, msgCtx.Content)
	}

	return withRetry(ctx, p.logger, "StoreBotResponse", func() error {
		return p.store.StoreBotResponse(ctx, chatMsg)
	})
}

func (p *MessageStorePlugin) OnStreamComplete(ctx context.Context, sessionID string, msgCtx *MessageContext) error {
	if p.streamStore == nil {
		return nil
	}

	chatMsg := &storage.ChatAppMessage{
		ChatSessionID:     msgCtx.ChatSessionID,
		ChatPlatform:      msgCtx.ChatPlatform,
		ChatUserID:        msgCtx.ChatUserID,
		ChatBotUserID:     msgCtx.ChatBotUserID,
		ChatChannelID:     msgCtx.ChatChannelID,
		ChatThreadID:      msgCtx.ChatThreadID,
		EngineSessionID:   msgCtx.EngineSessionID,
		EngineNamespace:   msgCtx.EngineNamespace,
		ProviderSessionID: msgCtx.ProviderSessionID,
		ProviderType:      msgCtx.ProviderType,
		MessageType:       msgCtx.MessageType,
		FromUserID:        msgCtx.ChatBotUserID,
		Metadata:          msgCtx.Metadata,
	}

	return p.streamStore.OnStreamComplete(ctx, sessionID, chatMsg)
}

func (p *MessageStorePlugin) GetSessionMeta(ctx context.Context, chatSessionID string) (*storage.SessionMeta, error) {
	return p.store.GetSessionMeta(ctx, chatSessionID)
}

func (p *MessageStorePlugin) ListUserSessions(ctx context.Context, platform, userID string) ([]string, error) {
	return p.store.ListUserSessions(ctx, platform, userID)
}

func (p *MessageStorePlugin) ListMessages(ctx context.Context, query *storage.MessageQuery) ([]*storage.ChatAppMessage, error) {
	return p.store.List(ctx, query)
}

func (p *MessageStorePlugin) Close() error {
	if p.streamStore != nil {
		p.streamStore.Close()
	}
	return p.store.Close()
}

func (p *MessageStorePlugin) Initialize(ctx context.Context) error {
	return p.store.Initialize(ctx)
}
