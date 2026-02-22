package telemetry

import (
	"log/slog"
	"sync"
)

type HealthStatus string

const (
	StatusHealthy   HealthStatus = "healthy"
	StatusDegraded  HealthStatus = "degraded"
	StatusUnhealthy HealthStatus = "unhealthy"
)

type HealthChecker struct {
	logger *slog.Logger
	checks map[string]func() bool
	mu     sync.RWMutex
}

func NewHealthChecker(logger *slog.Logger) *HealthChecker {
	if logger == nil {
		logger = slog.Default()
	}
	return &HealthChecker{
		logger: logger,
		checks: make(map[string]func() bool),
	}
}

func (h *HealthChecker) RegisterCheck(name string, check func() bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.checks[name] = check
}

func (h *HealthChecker) Check() (HealthStatus, map[string]bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	results := make(map[string]bool)
	allHealthy := true
	anyHealthy := false

	for name, check := range h.checks {
		ok := check()
		results[name] = ok
		if !ok {
			allHealthy = false
		}
		if ok {
			anyHealthy = true
		}
	}

	if allHealthy {
		return StatusHealthy, results
	}
	if anyHealthy {
		return StatusDegraded, results
	}
	return StatusUnhealthy, results
}

var (
	globalHealthChecker   *HealthChecker
	globalHealthCheckerMu sync.Once
)

func GetHealthChecker() *HealthChecker {
	globalHealthCheckerMu.Do(func() {
		if globalHealthChecker == nil {
			globalHealthChecker = NewHealthChecker(nil)
		}
	})
	return globalHealthChecker
}
