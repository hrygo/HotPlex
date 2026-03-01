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

	// Slack permission metrics
	slackPermissionAllowed        int64
	slackPermissionBlockedUser    int64
	slackPermissionBlockedDM      int64
	slackPermissionBlockedMention int64
	mu                            sync.RWMutex
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

	// Slack permission metrics
	SlackPermissionAllowed        int64
	SlackPermissionBlockedUser    int64
	SlackPermissionBlockedDM      int64
	SlackPermissionBlockedMention int64
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

		// Slack permission metrics
		SlackPermissionAllowed:        m.slackPermissionAllowed,
		SlackPermissionBlockedUser:    m.slackPermissionBlockedUser,
		SlackPermissionBlockedDM:      m.slackPermissionBlockedDM,
		SlackPermissionBlockedMention: m.slackPermissionBlockedMention,
	}
}

// Slack Permission Metrics

func (m *Metrics) IncSlackPermissionAllowed() {
	m.mu.Lock()
	m.slackPermissionAllowed++
	m.mu.Unlock()
}

func (m *Metrics) IncSlackPermissionBlockedUser() {
	m.mu.Lock()
	m.slackPermissionBlockedUser++
	m.mu.Unlock()
}

func (m *Metrics) IncSlackPermissionBlockedDM() {
	m.mu.Lock()
	m.slackPermissionBlockedDM++
	m.mu.Unlock()
}

func (m *Metrics) IncSlackPermissionBlockedMention() {
	m.mu.Lock()
	m.slackPermissionBlockedMention++
	m.mu.Unlock()
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
