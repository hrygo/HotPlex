package telemetry

import (
	"testing"
	"time"
)

func TestMetricsIncrement(t *testing.T) {
	m := NewMetrics(nil)

	// Test session active increment
	m.IncSessionsActive()
	snap := m.Snapshot()
	if snap.SessionsActive != 1 || snap.SessionsTotal != 1 {
		t.Errorf("expected SessionsActive=1, SessionsTotal=1, got %d, %d", snap.SessionsActive, snap.SessionsTotal)
	}

	// Test session decrement
	m.DecSessionsActive()
	snap = m.Snapshot()
	if snap.SessionsActive != 0 {
		t.Errorf("expected SessionsActive=0, got %d", snap.SessionsActive)
	}

	// Test tools invoked
	m.IncToolsInvoked()
	snap = m.Snapshot()
	if snap.ToolsInvoked != 1 {
		t.Errorf("expected ToolsInvoked=1, got %d", snap.ToolsInvoked)
	}

	// Test dangers blocked
	m.IncDangersBlocked()
	snap = m.Snapshot()
	if snap.DangersBlocked != 1 {
		t.Errorf("expected DangersBlocked=1, got %d", snap.DangersBlocked)
	}

	// Test errors
	m.IncSessionsErrors()
	snap = m.Snapshot()
	if snap.SessionsErrors != 1 {
		t.Errorf("expected SessionsErrors=1, got %d", snap.SessionsErrors)
	}

	// Test duration
	m.RecordDuration(5 * time.Second)
	snap = m.Snapshot()
	if snap.RequestDuration != 5*time.Second {
		t.Errorf("expected RequestDuration=5s, got %v", snap.RequestDuration)
	}
}

func TestMetricsDecrementFloor(t *testing.T) {
	m := NewMetrics(nil)

	// Decrementing below 0 should stay at 0
	m.DecSessionsActive()
	snap := m.Snapshot()
	if snap.SessionsActive != 0 {
		t.Errorf("expected SessionsActive=0, got %d", snap.SessionsActive)
	}
}

func TestHealthChecker(t *testing.T) {
	h := NewHealthChecker(nil)

	// Initially no checks registered, should be healthy
	status, checks := h.Check()
	if status != StatusHealthy {
		t.Errorf("expected StatusHealthy, got %v", status)
	}
	if len(checks) != 0 {
		t.Errorf("expected 0 checks, got %d", len(checks))
	}

	// Register a passing check
	h.RegisterCheck("database", func() bool { return true })
	status, checks = h.Check()
	if status != StatusHealthy {
		t.Errorf("expected StatusHealthy, got %v", status)
	}
	if !checks["database"] {
		t.Error("expected database check to pass")
	}

	// Register a failing check
	h.RegisterCheck("cache", func() bool { return false })
	status, checks = h.Check()
	if status != StatusDegraded {
		t.Errorf("expected StatusDegraded, got %v", status)
	}
	if checks["cache"] {
		t.Error("expected cache check to fail")
	}

	// Register another failing check - all fail
	h.RegisterCheck("database", func() bool { return false })
	status, _ = h.Check()
	if status != StatusUnhealthy {
		t.Errorf("expected StatusUnhealthy, got %v", status)
	}
}

func TestGlobalMetrics(t *testing.T) {
	InitMetrics(nil)
	m := GetMetrics()
	if m == nil {
		t.Error("expected non-nil metrics")
	}
}

func TestGlobalHealthChecker(t *testing.T) {
	h := GetHealthChecker()
	if h == nil {
		t.Error("expected non-nil health checker")
	}
}
