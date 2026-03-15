package diag

import (
	"sync"
	"sync/atomic"
	"time"
)

// Metrics collects diagnostic system metrics.
type Metrics struct {
	// Counters
	diagnosesTotal     atomic.Int64
	diagnosesSuccess   atomic.Int64
	diagnosesFailed    atomic.Int64
	issuesCreated      atomic.Int64
	issuesIgnored      atomic.Int64
	autoTriggers       atomic.Int64
	commandTriggers    atomic.Int64
	cacheHits          atomic.Int64
	cacheMisses        atomic.Int64

	// Gauges (use mutex for these)
	mu                  sync.RWMutex
	pendingDiagnoses    int64
	activeDiagnoses     int64

	// Histograms (simplified as averages)
	diagnosisDurationMs atomic.Int64
	diagnosisCount      atomic.Int64

	// Start time
	startTime time.Time
}

// NewMetrics creates a new Metrics instance.
func NewMetrics() *Metrics {
	return &Metrics{
		startTime: time.Now(),
	}
}

// RecordDiagnosisStart records the start of a diagnosis.
func (m *Metrics) RecordDiagnosisStart(trigger DiagTrigger) {
	m.diagnosesTotal.Add(1)

	m.mu.Lock()
	m.activeDiagnoses++
	m.mu.Unlock()

	if trigger == TriggerAuto {
		m.autoTriggers.Add(1)
	} else {
		m.commandTriggers.Add(1)
	}
}

// RecordDiagnosisComplete records a completed diagnosis.
func (m *Metrics) RecordDiagnosisComplete(success bool, duration time.Duration) {
	if success {
		m.diagnosesSuccess.Add(1)
	} else {
		m.diagnosesFailed.Add(1)
	}

	m.mu.Lock()
	m.activeDiagnoses--
	m.mu.Unlock()

	// Update average duration (simplified)
	ms := duration.Milliseconds()
	total := m.diagnosisDurationMs.Load() + ms
	count := m.diagnosisCount.Add(1)
	m.diagnosisDurationMs.Store(total / count)
}

// RecordIssueCreated records a created issue.
func (m *Metrics) RecordIssueCreated() {
	m.issuesCreated.Add(1)

	m.mu.Lock()
	if m.pendingDiagnoses > 0 {
		m.pendingDiagnoses--
	}
	m.mu.Unlock()
}

// RecordIssueIgnored records an ignored issue.
func (m *Metrics) RecordIssueIgnored() {
	m.issuesIgnored.Add(1)

	m.mu.Lock()
	if m.pendingDiagnoses > 0 {
		m.pendingDiagnoses--
	}
	m.mu.Unlock()
}

// RecordPendingAdded records a new pending diagnosis.
func (m *Metrics) RecordPendingAdded() {
	m.mu.Lock()
	m.pendingDiagnoses++
	m.mu.Unlock()
}

// RecordCacheHit records a cache hit.
func (m *Metrics) RecordCacheHit() {
	m.cacheHits.Add(1)
}

// RecordCacheMiss records a cache miss.
func (m *Metrics) RecordCacheMiss() {
	m.cacheMisses.Add(1)
}

// MetricsSnapshot is a snapshot of metrics.
type MetricsSnapshot struct {
	// Counters
	DiagnosesTotal   int64
	DiagnosesSuccess int64
	DiagnosesFailed  int64
	IssuesCreated    int64
	IssuesIgnored    int64
	AutoTriggers     int64
	CommandTriggers  int64
	CacheHits        int64
	CacheMisses      int64

	// Gauges
	PendingDiagnoses int64
	ActiveDiagnoses  int64

	// Averages
	AvgDiagnosisMs int64

	// Derived
	SuccessRate    float64
	CacheHitRate   float64
	Uptime         time.Duration
	AutoRate       float64
}

// Snapshot returns a snapshot of current metrics.
func (m *Metrics) Snapshot() MetricsSnapshot {
	m.mu.RLock()
	pending := m.pendingDiagnoses
	active := m.activeDiagnoses
	m.mu.RUnlock()

	total := m.diagnosesTotal.Load()
	success := m.diagnosesSuccess.Load()
	failed := m.diagnosesFailed.Load()

	var successRate float64
	if total > 0 {
		successRate = float64(success) / float64(total)
	}

	cacheHits := m.cacheHits.Load()
	cacheTotal := cacheHits + m.cacheMisses.Load()
	var cacheHitRate float64
	if cacheTotal > 0 {
		cacheHitRate = float64(cacheHits) / float64(cacheTotal)
	}

	auto := m.autoTriggers.Load()
	var autoRate float64
	if total > 0 {
		autoRate = float64(auto) / float64(total)
	}

	return MetricsSnapshot{
		DiagnosesTotal:   total,
		DiagnosesSuccess: success,
		DiagnosesFailed:  failed,
		IssuesCreated:    m.issuesCreated.Load(),
		IssuesIgnored:    m.issuesIgnored.Load(),
		AutoTriggers:     auto,
		CommandTriggers:  m.commandTriggers.Load(),
		CacheHits:        cacheHits,
		CacheMisses:      m.cacheMisses.Load(),
		PendingDiagnoses: pending,
		ActiveDiagnoses:  active,
		AvgDiagnosisMs:   m.diagnosisDurationMs.Load(),
		SuccessRate:      successRate,
		CacheHitRate:     cacheHitRate,
		Uptime:           time.Since(m.startTime),
		AutoRate:         autoRate,
	}
}

// Reset resets all metrics.
func (m *Metrics) Reset() {
	m.diagnosesTotal.Store(0)
	m.diagnosesSuccess.Store(0)
	m.diagnosesFailed.Store(0)
	m.issuesCreated.Store(0)
	m.issuesIgnored.Store(0)
	m.autoTriggers.Store(0)
	m.commandTriggers.Store(0)
	m.cacheHits.Store(0)
	m.cacheMisses.Store(0)
	m.diagnosisDurationMs.Store(0)
	m.diagnosisCount.Store(0)

	m.mu.Lock()
	m.pendingDiagnoses = 0
	m.activeDiagnoses = 0
	m.startTime = time.Now()
	m.mu.Unlock()
}

// Global metrics instance
var globalMetrics = NewMetrics()

// GetGlobalMetrics returns the global metrics instance.
func GetGlobalMetrics() *Metrics {
	return globalMetrics
}
