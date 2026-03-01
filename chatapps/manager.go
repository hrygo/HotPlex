package chatapps

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/gorilla/mux"

	"github.com/hrygo/hotplex/chatapps/base"
	"github.com/hrygo/hotplex/engine"
)

type AdapterManager struct {
	adapters map[string]ChatAdapter
	engines  []*engine.Engine
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
	// Copy adapters to local slice to avoid holding lock during blocking I/O
	m.mu.RLock()
	adapters := make([]ChatAdapter, 0, len(m.adapters))
	for _, adapter := range m.adapters {
		adapters = append(adapters, adapter)
	}
	m.mu.RUnlock()

	// Start adapters without holding lock
	for _, adapter := range adapters {
		if err := adapter.Start(ctx); err != nil {
			return err
		}
		m.logger.Info("Adapter started", "platform", adapter.Platform())
	}
	return nil
}

func (m *AdapterManager) StopAll() error {
	m.mu.Lock()
	adapters := make([]ChatAdapter, 0, len(m.adapters))
	for _, adapter := range m.adapters {
		adapters = append(adapters, adapter)
	}
	engines := m.engines
	m.engines = nil
	m.mu.Unlock()

	var firstErr error

	// 1. Stop adapters (webhooks, etc.)
	for _, adapter := range adapters {
		if err := adapter.Stop(); err != nil {
			m.logger.Error("Stop adapter failed", "platform", adapter.Platform(), "error", err)
			if firstErr == nil {
				firstErr = err
			}
		} else {
			m.logger.Info("Adapter stopped", "platform", adapter.Platform())
		}
	}

	// 2. Close all platform engines in parallel (kills CLI child processes)
	var wg sync.WaitGroup
	var errMu sync.Mutex

	for _, eng := range engines {
		if eng == nil {
			continue
		}
		wg.Add(1)
		go func(e *engine.Engine) {
			defer wg.Done()
			if err := e.Close(); err != nil {
				m.logger.Error("Close engine failed", "error", err)
				errMu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				errMu.Unlock()
			}
		}(eng)
	}

	wg.Wait()
	m.logger.Info("Cleanup completed", "engines_count", len(engines))
	return firstErr
}

func (m *AdapterManager) RegisterEngine(eng *engine.Engine) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.engines = append(m.engines, eng)
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

// RegisterRoutes registers all adapter webhooks to a unified router
// Path format: /webhook/{platform} (e.g., /webhook/telegram, /webhook/discord)
func (m *AdapterManager) RegisterRoutes(router *mux.Router) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for platform, adapter := range m.adapters {
		// Check if adapter implements WebhookProvider
		if provider, ok := adapter.(base.WebhookProvider); ok {
			handler := provider.WebhookHandler()

			// Register under /webhook/{platform} prefix
			// We use PathPrefix and StripPrefix to allow the adapter's internal mux to handle multiple sub-paths
			prefix := fmt.Sprintf("/webhook/%s/", platform)
			router.PathPrefix(prefix).Handler(http.StripPrefix(strings.TrimSuffix(prefix, "/"), handler))

			m.logger.Info("Registered webhooks", "platform", platform, "prefix", prefix)
		} else {
			m.logger.Warn("Adapter does not support webhooks", "platform", platform)
		}
	}
}

// Handler returns an http.Handler with all adapter webhooks mounted
// This is a convenience method when you don't need gorilla/mux
func (m *AdapterManager) Handler() http.Handler {
	mux := mux.NewRouter()
	m.RegisterRoutes(mux)
	return mux
}

// GetMessageOperations returns platform-specific message operations interface
// Returns nil if the platform doesn't support message operations
func (m *AdapterManager) GetMessageOperations(platform string) MessageOperations {
	m.mu.RLock()
	defer m.mu.RUnlock()

	adapter, ok := m.adapters[platform]
	if !ok {
		return nil
	}

	// Safe type assertion - only place where this is allowed
	if ops, ok := adapter.(MessageOperations); ok {
		return ops
	}
	return nil
}

// GetSessionOperations returns platform-specific session operations interface
// Returns nil if the platform doesn't support session operations
func (m *AdapterManager) GetSessionOperations(platform string) SessionOperations {
	m.mu.RLock()
	defer m.mu.RUnlock()

	adapter, ok := m.adapters[platform]
	if !ok {
		return nil
	}

	// Safe type assertion - only place where this is allowed
	if ops, ok := adapter.(SessionOperations); ok {
		return ops
	}
	return nil
}
