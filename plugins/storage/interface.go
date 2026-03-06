package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/hrygo/hotplex/types"
)

// ChatAppMessage 存储层消息实体
type ChatAppMessage struct {
	ID                string
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
	FromUserID        string
	FromUserName      string
	ToUserID          string
	Content           string
	Metadata          map[string]any
	CreatedAt         time.Time
	UpdatedAt         time.Time
	Deleted           bool
	DeletedAt         *time.Time
}

// MessageQuery 消息查询条件
type MessageQuery struct {
	ChatSessionID     string
	EngineSessionID   uuid.UUID
	ProviderType      string
	ProviderSessionID string
	StartTime         *time.Time
	EndTime           *time.Time
	MessageTypes      []types.MessageType
	Limit             int
	Offset            int
	Ascending         bool
	IncludeDeleted    bool
}

// SessionMeta 会话元数据
type SessionMeta struct {
	ChatSessionID string
	ChatPlatform  string
	ChatUserID    string
	LastMessageID string
	LastMessageAt time.Time
	MessageCount  int64
	UpdatedAt     time.Time
}

// ReadOnlyStore 只读接口 (ISP)
type ReadOnlyStore interface {
	Get(ctx context.Context, messageID string) (*ChatAppMessage, error)
	List(ctx context.Context, query *MessageQuery) ([]*ChatAppMessage, error)
	Count(ctx context.Context, query *MessageQuery) (int64, error)
}

// WriteOnlyStore 只写接口 (ISP)
type WriteOnlyStore interface {
	StoreUserMessage(ctx context.Context, msg *ChatAppMessage) error
	StoreBotResponse(ctx context.Context, msg *ChatAppMessage) error
}

// SessionStore 会话管理接口
type SessionStore interface {
	GetSessionMeta(ctx context.Context, chatSessionID string) (*SessionMeta, error)
	ListUserSessions(ctx context.Context, platform, userID string) ([]string, error)
	DeleteSession(ctx context.Context, chatSessionID string) error
}

// ChatAppMessageStore 完整接口
type ChatAppMessageStore interface {
	ReadOnlyStore
	WriteOnlyStore
	SessionStore
	Initialize(ctx context.Context) error
	Close() error
	Name() string
	Version() string
}

// StorageStrategy 存储策略接口 (OCP)
type StorageStrategy interface {
	ShouldStore(msg *ChatAppMessage) bool
	BeforeStore(ctx context.Context, msg *ChatAppMessage) error
	AfterStore(ctx context.Context, msg *ChatAppMessage) error
}

// DefaultStrategy 默认策略
type DefaultStrategy struct{}

// Compile-time interface compliance check
var _ StorageStrategy = (*DefaultStrategy)(nil)

func NewDefaultStrategy() *DefaultStrategy { return &DefaultStrategy{} }
func (s *DefaultStrategy) ShouldStore(msg *ChatAppMessage) bool {
	return msg.MessageType.IsStorable()
}
func (s *DefaultStrategy) BeforeStore(ctx context.Context, msg *ChatAppMessage) error { return nil }
func (s *DefaultStrategy) AfterStore(ctx context.Context, msg *ChatAppMessage) error  { return nil }
