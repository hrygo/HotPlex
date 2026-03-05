package session

import (
	"fmt"

	"github.com/google/uuid"
)

// SessionManager 统一管理三层 SessionID (DRY 原则)
type SessionManager interface {
	GetChatSessionID(platform, userID, botUserID, channelID, threadID string) string
	GenerateEngineSessionID() uuid.UUID
	GenerateProviderSessionID(engineSessionID uuid.UUID, providerType string) string
	CreateSessionContext(platform, userID, botUserID, channelID, threadID, providerType string) *SessionContext
}

// SessionContext 完整会话上下文
type SessionContext struct {
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
}

// DefaultSessionManager 默认实现
type DefaultSessionManager struct {
	namespace string
}

func NewSessionManager(namespace string) *DefaultSessionManager {
	return &DefaultSessionManager{namespace: namespace}
}

func (m *DefaultSessionManager) GetChatSessionID(platform, userID, botUserID, channelID, threadID string) string {
	key := fmt.Sprintf("%s:%s:%s:%s:%s", platform, userID, botUserID, channelID, threadID)
	input := m.namespace + ":session:" + key
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(input)).String()
}

func (m *DefaultSessionManager) GenerateEngineSessionID() uuid.UUID {
	return uuid.New()
}

func (m *DefaultSessionManager) GenerateProviderSessionID(engineSessionID uuid.UUID, providerType string) string {
	input := m.namespace + ":provider:" + providerType + ":" + engineSessionID.String()
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(input)).String()
}

func (m *DefaultSessionManager) CreateSessionContext(platform, userID, botUserID, channelID, threadID, providerType string) *SessionContext {
	chatSessionID := m.GetChatSessionID(platform, userID, botUserID, channelID, threadID)
	engineSessionID := m.GenerateEngineSessionID()
	providerSessionID := m.GenerateProviderSessionID(engineSessionID, providerType)

	return &SessionContext{
		ChatSessionID:     chatSessionID,
		ChatPlatform:      platform,
		ChatUserID:        userID,
		ChatBotUserID:     botUserID,
		ChatChannelID:     channelID,
		ChatThreadID:      threadID,
		EngineSessionID:   engineSessionID,
		EngineNamespace:   m.namespace,
		ProviderSessionID: providerSessionID,
		ProviderType:      providerType,
	}
}
