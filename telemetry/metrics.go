package telemetry

import (
	"log/slog"
	"sync"
	"time"
)

type Metrics struct {
	logger          *slog.Logger
	sessionsActive  int64
	sessionsTotal   int64
	sessionsErrors  int64
	toolsInvoked    int64
	dangersBlocked  int64
	requestDuration time.Duration
	mu              sync.RWMutex
}

var (
	globalMetrics   *Metrics
	globalMetricsMu sync.Once
)

func NewMetrics(logger *slog.Logger) *Metrics {
	if logger == nil {
		logger = slog.Default()
	}
	return &Metrics{logger: logger}
}

func (m *Metrics) IncSessionsActive() {
	m.mu.Lock()
	m.sessionsActive++
	m.sessionsTotal++
	m.mu.Unlock()
}

func (m *Metrics) DecSessionsActive() {
	m.mu.Lock()
	if m.sessionsActive > 0 {
		m.sessionsActive--
	}
	m.mu.Unlock()
}

func (m *Metrics) IncSessionsErrors() {
	m.mu.Lock()
	m.sessionsErrors++
	m.mu.Unlock()
}

func (m *Metrics) IncToolsInvoked() {
	m.mu.Lock()
	m.toolsInvoked++
	m.mu.Unlock()
}

func (m *Metrics) IncDangersBlocked() {
	m.mu.Lock()
	m.dangersBlocked++
	m.mu.Unlock()
}

func (m *Metrics) RecordDuration(d time.Duration) {
	m.mu.Lock()
	m.requestDuration = d
	m.mu.Unlock()
}

type MetricsSnapshot struct {
	SessionsActive  int64
	SessionsTotal   int64
	SessionsErrors  int64
	ToolsInvoked    int64
	DangersBlocked  int64
	RequestDuration time.Duration
}

func (m *Metrics) Snapshot() MetricsSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return MetricsSnapshot{
		SessionsActive:  m.sessionsActive,
		SessionsTotal:   m.sessionsTotal,
		SessionsErrors:  m.sessionsErrors,
		ToolsInvoked:    m.toolsInvoked,
		DangersBlocked:  m.dangersBlocked,
		RequestDuration: m.requestDuration,
	}
}

func InitMetrics(logger *slog.Logger) {
	globalMetrics = NewMetrics(logger)
}

func GetMetrics() *Metrics {
	globalMetricsMu.Do(func() {
		if globalMetrics == nil {
			globalMetrics = NewMetrics(nil)
		}
	})
	return globalMetrics
}
