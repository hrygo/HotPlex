package chatapps

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)

type AdapterManager struct {
	adapters map[string]ChatAdapter
	mu       sync.RWMutex
	logger   *slog.Logger
}

func NewAdapterManager(logger *slog.Logger) *AdapterManager {
	return &AdapterManager{
		adapters: make(map[string]ChatAdapter),
		logger:   logger,
	}
}

func (m *AdapterManager) Register(adapter ChatAdapter) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	platform := adapter.Platform()
	if _, exists := m.adapters[platform]; exists {
		return nil
	}

	m.adapters[platform] = adapter
	m.logger.Info("Adapter registered", "platform", platform)
	return nil
}

func (m *AdapterManager) Unregister(platform string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if adapter, ok := m.adapters[platform]; ok {
		_ = adapter.Stop()
		delete(m.adapters, platform)
		m.logger.Info("Adapter unregistered", "platform", platform)
	}
	return nil
}

func (m *AdapterManager) GetAdapter(platform string) (ChatAdapter, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	adapter, ok := m.adapters[platform]
	return adapter, ok
}

func (m *AdapterManager) StartAll(ctx context.Context) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for platform, adapter := range m.adapters {
		if err := adapter.Start(ctx); err != nil {
			return err
		}
		m.logger.Info("Adapter started", "platform", platform)
	}
	return nil
}

func (m *AdapterManager) StopAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for platform, adapter := range m.adapters {
		if err := adapter.Stop(); err != nil {
			m.logger.Error("Stop adapter failed", "platform", platform, "error", err)
			continue
		}
		m.logger.Info("Adapter stopped", "platform", platform)
	}
	return nil
}

func (m *AdapterManager) ListPlatforms() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	platforms := make([]string, 0, len(m.adapters))
	for platform := range m.adapters {
		platforms = append(platforms, platform)
	}
	return platforms
}

// SendMessage sends a message to a specific platform
func (m *AdapterManager) SendMessage(ctx context.Context, platform, sessionID string, msg *ChatMessage) error {
	adapter, ok := m.GetAdapter(platform)
	if !ok {
		return fmt.Errorf("adapter not found for platform: %s", platform)
	}
	return adapter.SendMessage(ctx, sessionID, msg)
}
