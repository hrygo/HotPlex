package diag

import (
	"testing"
	"time"
)

func TestMetrics_RecordDiagnosisStart(t *testing.T) {
	m := NewMetrics()

	// Record auto trigger
	m.RecordDiagnosisStart(TriggerAuto)

	if m.diagnosesTotal.Load() != 1 {
		t.Errorf("diagnosesTotal = %d, want 1", m.diagnosesTotal.Load())
	}
	if m.autoTriggers.Load() != 1 {
		t.Errorf("autoTriggers = %d, want 1", m.autoTriggers.Load())
	}
	if m.commandTriggers.Load() != 0 {
		t.Errorf("commandTriggers = %d, want 0", m.commandTriggers.Load())
	}

	// Record command trigger
	m.RecordDiagnosisStart(TriggerCommand)

	if m.diagnosesTotal.Load() != 2 {
		t.Errorf("diagnosesTotal = %d, want 2", m.diagnosesTotal.Load())
	}
	if m.commandTriggers.Load() != 1 {
		t.Errorf("commandTriggers = %d, want 1", m.commandTriggers.Load())
	}
}

func TestMetrics_RecordDiagnosisComplete(t *testing.T) {
	m := NewMetrics()

	// Start and complete a successful diagnosis
	m.RecordDiagnosisStart(TriggerAuto)
	m.RecordDiagnosisComplete(true, 100*time.Millisecond)

	if m.diagnosesSuccess.Load() != 1 {
		t.Errorf("diagnosesSuccess = %d, want 1", m.diagnosesSuccess.Load())
	}
	if m.diagnosesFailed.Load() != 0 {
		t.Errorf("diagnosesFailed = %d, want 0", m.diagnosesFailed.Load())
	}

	// Complete a failed diagnosis
	m.RecordDiagnosisStart(TriggerCommand)
	m.RecordDiagnosisComplete(false, 50*time.Millisecond)

	if m.diagnosesFailed.Load() != 1 {
		t.Errorf("diagnosesFailed = %d, want 1", m.diagnosesFailed.Load())
	}
}

func TestMetrics_RecordIssueCreated(t *testing.T) {
	m := NewMetrics()

	m.RecordPendingAdded()
	m.RecordIssueCreated()

	if m.issuesCreated.Load() != 1 {
		t.Errorf("issuesCreated = %d, want 1", m.issuesCreated.Load())
	}
}

func TestMetrics_RecordIssueIgnored(t *testing.T) {
	m := NewMetrics()

	m.RecordPendingAdded()
	m.RecordIssueIgnored()

	if m.issuesIgnored.Load() != 1 {
		t.Errorf("issuesIgnored = %d, want 1", m.issuesIgnored.Load())
	}
}

func TestMetrics_RecordCacheHit(t *testing.T) {
	m := NewMetrics()

	m.RecordCacheHit()
	m.RecordCacheHit()
	m.RecordCacheMiss()

	if m.cacheHits.Load() != 2 {
		t.Errorf("cacheHits = %d, want 2", m.cacheHits.Load())
	}
	if m.cacheMisses.Load() != 1 {
		t.Errorf("cacheMisses = %d, want 1", m.cacheMisses.Load())
	}
}

func TestMetrics_Snapshot(t *testing.T) {
	m := NewMetrics()

	// Record some data
	m.RecordDiagnosisStart(TriggerAuto)
	m.RecordDiagnosisComplete(true, 100*time.Millisecond)
	m.RecordCacheHit()
	m.RecordPendingAdded()
	m.RecordIssueCreated()

	snap := m.Snapshot()

	if snap.DiagnosesTotal != 1 {
		t.Errorf("Snapshot.DiagnosesTotal = %d, want 1", snap.DiagnosesTotal)
	}
	if snap.IssuesCreated != 1 {
		t.Errorf("Snapshot.IssuesCreated = %d, want 1", snap.IssuesCreated)
	}
	if snap.CacheHitRate != 1.0 {
		t.Errorf("Snapshot.CacheHitRate = %v, want 1.0", snap.CacheHitRate)
	}
}

func TestMetrics_Reset(t *testing.T) {
	m := NewMetrics()

	// Record some data
	m.RecordDiagnosisStart(TriggerAuto)
	m.RecordDiagnosisComplete(true, 100*time.Millisecond)
	m.RecordCacheHit()

	m.Reset()

	snap := m.Snapshot()
	if snap.DiagnosesTotal != 0 {
		t.Errorf("After Reset, DiagnosesTotal = %d, want 0", snap.DiagnosesTotal)
	}
	if snap.CacheHits != 0 {
		t.Errorf("After Reset, CacheHits = %d, want 0", snap.CacheHits)
	}
}

func TestMetrics_PendingDiagnoses(t *testing.T) {
	m := NewMetrics()

	m.RecordPendingAdded()
	m.RecordPendingAdded()

	snap := m.Snapshot()
	if snap.PendingDiagnoses != 2 {
		t.Errorf("PendingDiagnoses = %d, want 2", snap.PendingDiagnoses)
	}

	// Complete one
	m.RecordIssueCreated()

	snap = m.Snapshot()
	if snap.PendingDiagnoses != 1 {
		t.Errorf("PendingDiagnoses after issue created = %d, want 1", snap.PendingDiagnoses)
	}
}
