package internal

import (
	"context"
	"log/slog"
	"sync"

	"github.com/hrygo/hotplex/chatapps/base"
)

// StatusManager统一管理AI状态通知
// 职责: 状态去重、节流、线程安全
type StatusManager struct {
	provider base.StatusProvider
	logger   *slog.Logger
	mu       sync.Mutex
	current  base.StatusType
}

// NewStatusManager 创建StatusManager
func NewStatusManager(provider base.StatusProvider, logger *slog.Logger) *StatusManager {
	return &StatusManager{
		provider: provider,
		logger:   logger,
	}
}

// Notify 通知状态变化
// 如果状态未变则跳过，避免重复通知
// channelID 和 threadTS 指定状态显示位置
func (m *StatusManager) Notify(ctx context.Context, channelID, threadTS string, status base.StatusType, text string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.current == status {
		return nil // 避免重复
	}
	m.current = status

	return m.provider.SetStatus(ctx, channelID, threadTS, status, text)
}

// Clear 清除状态
func (m *StatusManager) Clear(ctx context.Context, channelID, threadTS string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.current = base.StatusIdle
	return m.provider.ClearStatus(ctx, channelID, threadTS)
}

// Current 获取当前状态
func (m *StatusManager) Current() base.StatusType {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.current
}
