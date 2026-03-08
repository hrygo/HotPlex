package base

import (
	"context"

	"github.com/hrygo/hotplex/plugins/storage"
	"github.com/hrygo/hotplex/types"
)

// =============================================================================
// 职责链处理器接口 (M1 重构)
// =============================================================================

// StorageHandler 存储处理接口 (职责链基础)
type StorageHandler interface {
	Handle(ctx context.Context, msgCtx *MessageContext) error
	Name() string
}

// StorageHandlerChain 职责链
type StorageHandlerChain struct {
	handlers []StorageHandler
}

// Add 添加处理器
func (c *StorageHandlerChain) Add(h StorageHandler) {
	c.handlers = append(c.handlers, h)
}

// Handle 执行职责链
func (c *StorageHandlerChain) Handle(ctx context.Context, msgCtx *MessageContext) error {
	for _, h := range c.handlers {
		if err := h.Handle(ctx, msgCtx); err != nil {
			return err
		}
	}
	return nil
}

// =============================================================================
// 1. Filter - 消息类型过滤
// =============================================================================

// MessageFilter 消息过滤器
type MessageFilter struct {
	messageTypes []types.MessageType
}

func NewMessageFilter(msgTypes ...types.MessageType) *MessageFilter {
	return &MessageFilter{messageTypes: msgTypes}
}

func (f *MessageFilter) Name() string { return "MessageFilter" }

func (f *MessageFilter) Handle(ctx context.Context, msgCtx *MessageContext) error {
	for _, t := range f.messageTypes {
		if msgCtx.MessageType == t {
			return nil
		}
	}
	return ErrMessageTypeFiltered
}

// ErrMessageTypeFiltered 消息类型被过滤
var ErrMessageTypeFiltered = &MessageFilterError{"message type not allowed"}

type MessageFilterError struct {
	msg string
}

func (e *MessageFilterError) Error() string { return e.msg }

// =============================================================================
// 2. Transformer - 数据转换
// =============================================================================

// MessageTransformer 消息转换器
type MessageTransformer struct{}

func NewMessageTransformer() *MessageTransformer {
	return &MessageTransformer{}
}

func (t *MessageTransformer) Name() string { return "MessageTransformer" }

func (t *MessageTransformer) Handle(ctx context.Context, msgCtx *MessageContext) error {
	return msgCtx.Validate()
}

// ToStorageMessage 转换为存储实体
func (t *MessageTransformer) ToStorageMessage(msgCtx *MessageContext, isUserMessage bool) *storage.ChatAppMessage {
	fromUserID := msgCtx.ChatBotUserID
	if isUserMessage {
		fromUserID = msgCtx.ChatUserID
	}

	return &storage.ChatAppMessage{
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
		FromUserID:        fromUserID,
		Content:           msgCtx.Content,
		Metadata:          msgCtx.Metadata,
	}
}

// =============================================================================
// 3. Policy - 存储策略检查
// =============================================================================

// StoragePolicy 存储策略处理器
type StoragePolicy struct {
	strategy storage.StorageStrategy
}

func NewStoragePolicy(strategy storage.StorageStrategy) *StoragePolicy {
	return &StoragePolicy{strategy: strategy}
}

func (p *StoragePolicy) Name() string { return "StoragePolicy" }

func (p *StoragePolicy) Handle(ctx context.Context, msgCtx *MessageContext) error {
	return nil
}

// ShouldStore 策略检查方法
func (p *StoragePolicy) ShouldStore(msg *storage.ChatAppMessage) bool {
	if p.strategy == nil {
		return true
	}
	return p.strategy.ShouldStore(msg)
}

// =============================================================================
// 4. Repository - 存储仓库 (封装持久化逻辑)
// =============================================================================

// StorageRepository 存储仓库实现
type StorageRepository struct {
	store  storage.WriteOnlyStore
	logger Logger
	retry  bool
}

// Logger 接口 (简化 slog.Logger)
type Logger interface {
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

// NewStorageRepository 创建存储仓库
func NewStorageRepository(store storage.WriteOnlyStore, logger Logger, retry bool) *StorageRepository {
	return &StorageRepository{
		store:  store,
		logger: logger,
		retry:  retry,
	}
}

func (r *StorageRepository) Name() string { return "StorageRepository" }

func (r *StorageRepository) Handle(ctx context.Context, msgCtx *MessageContext) error {
	return nil
}

// StoreUserMessage 存储用户消息
func (r *StorageRepository) StoreUserMessage(ctx context.Context, msg *storage.ChatAppMessage) error {
	if r.retry {
		return withRetry(ctx, r.logger, "StoreUserMessage", func() error {
			return r.store.StoreUserMessage(ctx, msg)
		})
	}
	return r.store.StoreUserMessage(ctx, msg)
}

// StoreBotResponse 存储机器人响应
func (r *StorageRepository) StoreBotResponse(ctx context.Context, msg *storage.ChatAppMessage) error {
	if r.retry {
		return withRetry(ctx, r.logger, "StoreBotResponse", func() error {
			return r.store.StoreBotResponse(ctx, msg)
		})
	}
	return r.store.StoreBotResponse(ctx, msg)
}

func (r *StorageRepository) Close() error {
	if closer, ok := r.store.(interface{ Close() error }); ok {
		return closer.Close()
	}
	return nil
}
