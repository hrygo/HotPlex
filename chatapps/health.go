package chatapps

import (
	"context"
	"github.com/hrygo/hotplex/internal/panicx"
	"sync"
	"sync/atomic"
	"time"
)

type AdapterMetrics struct {
	MessagesReceived atomic.Int64
	MessagesSent     atomic.Int64
	MessagesFailed   atomic.Int64
	LastMessageAt    atomic.Int64
	Uptime           atomic.Int64
}

func NewAdapterMetrics() *AdapterMetrics {
	m := &AdapterMetrics{}
	m.Uptime.Store(time.Now().Unix())
	return m
}

func (m *AdapterMetrics) RecordReceive() {
	m.MessagesReceived.Add(1)
	m.LastMessageAt.Store(time.Now().Unix())
}

func (m *AdapterMetrics) RecordSend() {
	m.MessagesSent.Add(1)
}

func (m *AdapterMetrics) RecordFailure() {
	m.MessagesFailed.Add(1)
}

func (m *AdapterMetrics) GetStats() map[string]int64 {
	return map[string]int64{
		"messages_received": m.MessagesReceived.Load(),
		"messages_sent":     m.MessagesSent.Load(),
		"messages_failed":   m.MessagesFailed.Load(),
		"last_message_at":   m.LastMessageAt.Load(),
		"uptime_seconds":    time.Now().Unix() - m.Uptime.Load(),
	}
}

type HealthChecker struct {
	mu       sync.RWMutex
	checks   map[string]HealthCheck
	interval time.Duration
	ctx      context.Context
	cancel   context.CancelFunc
}

type HealthCheck struct {
	Name      string
	Status    string
	LastCheck time.Time
	LastError string
	Interval  time.Duration
	CheckFunc func() error
}

func NewHealthChecker(interval time.Duration) *HealthChecker {
	ctx, cancel := context.WithCancel(context.Background())
	return &HealthChecker{
		checks:   make(map[string]HealthCheck),
		interval: interval,
		ctx:      ctx,
		cancel:   cancel,
	}
}

func (h *HealthChecker) Register(check HealthCheck) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.checks[check.Name] = check
}

func (h *HealthChecker) Start() {
	panicx.SafeGo(h.logger, func() {
		ticker := time.NewTicker(h.interval)
		defer ticker.Stop()
		for {
			select {
			case <-h.ctx.Done():
				return
			case <-ticker.C:
				h.runChecks()
			}
		}
	})
}

func (h *HealthChecker) Stop() {
	h.cancel()
}

func (h *HealthChecker) runChecks() {
	h.mu.Lock()
	defer h.mu.Unlock()

	for name, check := range h.checks {
		if err := check.CheckFunc(); err != nil {
			check.Status = "unhealthy"
			check.LastError = err.Error()
		} else {
			check.Status = "healthy"
			check.LastError = ""
		}
		check.LastCheck = time.Now()
		h.checks[name] = check
	}
}

func (h *HealthChecker) GetStatus() map[string]HealthCheck {
	h.mu.RLock()
	defer h.mu.RUnlock()

	result := make(map[string]HealthCheck)
	for k, v := range h.checks {
		result[k] = v
	}
	return result
}

type LifecycleManager struct {
	mu           sync.RWMutex
	adapters     map[string]ChatAdapter
	startOrder   []string
	stopOrder    []string
	onStartHooks []func(ChatAdapter) error
	onStopHooks  []func(ChatAdapter) error
}

func NewLifecycleManager() *LifecycleManager {
	return &LifecycleManager{
		adapters:   make(map[string]ChatAdapter),
		startOrder: []string{},
		stopOrder:  []string{},
	}
}

func (m *LifecycleManager) RegisterAdapter(adapter ChatAdapter, startPriority int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	platform := adapter.Platform()
	m.adapters[platform] = adapter

	m.startOrder = append(m.startOrder, platform)
	m.stopOrder = append([]string{platform}, m.stopOrder...)

	for i := range m.startOrder {
		if startPriority < i {
			copy(m.startOrder[i+1:], m.startOrder[i:])
			m.startOrder[i] = platform
			break
		}
	}
}

func (m *LifecycleManager) OnStart(hook func(ChatAdapter) error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onStartHooks = append(m.onStartHooks, hook)
}

func (m *LifecycleManager) OnStop(hook func(ChatAdapter) error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onStopHooks = append(m.onStopHooks, hook)
}

func (m *LifecycleManager) StartAll(ctx context.Context) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, platform := range m.startOrder {
		adapter := m.adapters[platform]
		if err := adapter.Start(ctx); err != nil {
			return err
		}
		for _, hook := range m.onStartHooks {
			if err := hook(adapter); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *LifecycleManager) StopAll() error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, platform := range m.stopOrder {
		adapter := m.adapters[platform]
		for _, hook := range m.onStopHooks {
			if err := hook(adapter); err != nil {
				return err
			}
		}
		if err := adapter.Stop(); err != nil {
			return err
		}
	}
	return nil
}
