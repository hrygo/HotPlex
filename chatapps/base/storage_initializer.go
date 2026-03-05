package base

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/hrygo/hotplex/chatapps/session"
	"github.com/hrygo/hotplex/plugins/storage"
)

// MessageStoreInitializer 消息存储插件初始化器
type MessageStoreInitializer struct {
	logger *slog.Logger
}

// NewMessageStoreInitializer 创建初始化器
func NewMessageStoreInitializer(logger *slog.Logger) *MessageStoreInitializer {
	return &MessageStoreInitializer{logger: logger}
}

// InitializeFromConfig 从配置初始化消息存储插件
func (i *MessageStoreInitializer) InitializeFromConfig(ctx context.Context, cfg MessageStorePluginConfig) (*MessageStorePlugin, error) {
	if cfg.Store == nil {
		return nil, nil // 未配置存储，返回 nil
	}

	plugin, err := NewMessageStorePlugin(cfg)
	if err != nil {
		return nil, fmt.Errorf("create message store plugin: %w", err)
	}

	if err := plugin.Initialize(ctx); err != nil {
		return nil, fmt.Errorf("initialize message store: %w", err)
	}

	i.logger.Info("Message store plugin initialized",
		"type", cfg.Store.Name(),
		"version", cfg.Store.Version(),
		"streaming_enabled", cfg.StreamEnabled)

	return plugin, nil
}

// CreateStorageFromType 根据类型创建存储后端
func CreateStorageFromType(storageType string, config map[string]any) (storage.ChatAppMessageStore, error) {
	registry := storage.GlobalRegistry()
	if registry == nil {
		return nil, fmt.Errorf("storage registry not initialized")
	}

	store, err := registry.Get(storageType, config)
	if err != nil {
		return nil, fmt.Errorf("get storage plugin: %w", err)
	}
	if store == nil {
		return nil, fmt.Errorf("unknown storage type: %s", storageType)
	}

	return store, nil
}

// CreateSessionManager 创建 SessionManager
func CreateSessionManager(namespace string) session.SessionManager {
	return session.NewSessionManager(namespace)
}

// CreateDefaultStrategy 创建默认存储策略
func CreateDefaultStrategy() storage.StorageStrategy {
	return storage.NewDefaultStrategy()
}

// BuildMessageStorePlugin 从配置构建完整的消息存储插件
func BuildMessageStorePlugin(
	storageType string,
	storageConfig map[string]any,
	namespace string,
	providerType string,
	streamEnabled bool,
	streamTimeout time.Duration,
	streamMaxBuffers int,
) (*MessageStorePlugin, error) {
	// 1. 创建存储后端
	store, err := CreateStorageFromType(storageType, storageConfig)
	if err != nil {
		return nil, err
	}

	// 2. 创建 SessionManager
	sessionMgr := CreateSessionManager(namespace)

	// 3. 创建默认策略
	strategy := CreateDefaultStrategy()

	// 4. 创建插件
	plugin, err := NewMessageStorePlugin(MessageStorePluginConfig{
		Store:            store,
		SessionManager:   sessionMgr,
		Strategy:         strategy,
		StreamEnabled:    streamEnabled,
		StreamTimeout:    streamTimeout,
		StreamMaxBuffers: streamMaxBuffers,
	})
	if err != nil {
		return nil, fmt.Errorf("create message store plugin: %w", err)
	}

	return plugin, nil
}
