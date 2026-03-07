package base

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/hrygo/hotplex/chatapps/session"
	"github.com/hrygo/hotplex/plugins/storage"
	"github.com/hrygo/hotplex/types"
)

// MessageDirection 消息方向
type MessageDirection string

const (
	DirectionUserToBot MessageDirection = "user_to_bot"
	DirectionBotToUser MessageDirection = "bot_to_user"
)

// MessageContext 消息存储上下文
type MessageContext struct {
	// ChatApp 层信息
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
	// 消息信息
	MessageType types.MessageType
	Direction   MessageDirection
	Content     string
	Metadata    map[string]any
	// 可选追踪信息
	RequestID string
	TraceID   string
}

// MessageContextBuilder 构建器模式
type MessageContextBuilder struct {
	ctx *MessageContext
}

// NewMessageContextBuilder 创建构建器
func NewMessageContextBuilder() *MessageContextBuilder {
	return &MessageContextBuilder{
		ctx: &MessageContext{Metadata: make(map[string]any)},
	}
}

// WithChatSession 设置 ChatApp 层 Session 信息
func (b *MessageContextBuilder) WithChatSession(sessionID, platform, userID, botUserID, channelID, threadID string) *MessageContextBuilder {
	b.ctx.ChatSessionID = sessionID
	b.ctx.ChatPlatform = platform
	b.ctx.ChatUserID = userID
	b.ctx.ChatBotUserID = botUserID
	b.ctx.ChatChannelID = channelID
	b.ctx.ChatThreadID = threadID
	return b
}

// WithEngineSession 设置 Engine 层 Session 信息
func (b *MessageContextBuilder) WithEngineSession(sessionID uuid.UUID, namespace string) *MessageContextBuilder {
	b.ctx.EngineSessionID = sessionID
	b.ctx.EngineNamespace = namespace
	return b
}

// WithProviderSession 设置 Provider 层 Session 信息
func (b *MessageContextBuilder) WithProviderSession(sessionID, providerType string) *MessageContextBuilder {
	b.ctx.ProviderSessionID = sessionID
	b.ctx.ProviderType = providerType
	return b
}

// WithMessage 设置消息信息
func (b *MessageContextBuilder) WithMessage(msgType types.MessageType, direction MessageDirection, content string) *MessageContextBuilder {
	b.ctx.MessageType = msgType
	b.ctx.Direction = direction
	b.ctx.Content = content
	return b
}

// WithMetadata 添加元数据
func (b *MessageContextBuilder) WithMetadata(key string, value any) *MessageContextBuilder {
	b.ctx.Metadata[key] = value
	return b
}

// Build 构建并验证
func (b *MessageContextBuilder) Build() (*MessageContext, error) {
	if err := b.ctx.Validate(); err != nil {
		return nil, err
	}
	return b.ctx, nil
}

// Validate 验证必填字段
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
	logger      *slog.Logger
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

// NewMessageStorePlugin 创建消息存储插件
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
		logger:     logger,
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

// OnUserMessage 处理用户消息存储
func (p *MessageStorePlugin) OnUserMessage(ctx context.Context, msgCtx *MessageContext) error {
	if msgCtx.MessageType != types.MessageTypeUserInput {
		return nil // 只存储用户输入
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
		FromUserID:        msgCtx.ChatUserID,
		Content:           msgCtx.Content,
		Metadata:          msgCtx.Metadata,
	}

	if p.strategy != nil && !p.strategy.ShouldStore(chatMsg) {
		return nil
	}

	return p.store.StoreUserMessage(ctx, chatMsg)
}

// OnBotResponse 处理机器人响应存储
func (p *MessageStorePlugin) OnBotResponse(ctx context.Context, msgCtx *MessageContext) error {
	if msgCtx.MessageType != types.MessageTypeFinalResponse {
		return nil // 只存储最终响应
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
		Content:           msgCtx.Content,
		Metadata:          msgCtx.Metadata,
	}

	if p.strategy != nil && !p.strategy.ShouldStore(chatMsg) {
		return nil
	}

	if p.streamStore != nil {
		// 流式模式：先缓存 chunk，等待完成信号
		return p.streamStore.OnStreamChunk(ctx, msgCtx.ChatSessionID, msgCtx.Content)
	}

	return p.store.StoreBotResponse(ctx, chatMsg)
}

// OnStreamComplete 标记流式消息完成并存储
func (p *MessageStorePlugin) OnStreamComplete(ctx context.Context, sessionID string, msgCtx *MessageContext) error {
	if p.streamStore == nil {
		return nil // 流式未启用
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

// GetSessionMeta 获取会话元数据
func (p *MessageStorePlugin) GetSessionMeta(ctx context.Context, chatSessionID string) (*storage.SessionMeta, error) {
	return p.store.GetSessionMeta(ctx, chatSessionID)
}

// ListUserSessions 列出用户的所有会话
func (p *MessageStorePlugin) ListUserSessions(ctx context.Context, platform, userID string) ([]string, error) {
	return p.store.ListUserSessions(ctx, platform, userID)
}

// ListMessages 列出会话的消息列表
func (p *MessageStorePlugin) ListMessages(ctx context.Context, query *storage.MessageQuery) ([]*storage.ChatAppMessage, error) {
	return p.store.List(ctx, query)
}

// Close 关闭插件
func (p *MessageStorePlugin) Close() error {
	if p.streamStore != nil {
		p.streamStore.Close()
	}
	return p.store.Close()
}

// Initialize 初始化插件
func (p *MessageStorePlugin) Initialize(ctx context.Context) error {
	return p.store.Initialize(ctx)
}
