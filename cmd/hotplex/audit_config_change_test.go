package main

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/audit"
	"github.com/hrygo/hotplex/internal/config"
)

// TestAuditConfigDiff_NoChange covers the common path: identical configs yield
// no diff entries, so emitAuditConfigChange must not fire.
func TestAuditConfigDiff_NoChange(t *testing.T) {
	t.Parallel()
	base := config.Default().Audit
	changes := auditConfigDiff(base, base)
	require.Empty(t, changes)
}

// TestAuditConfigDiff_DetectsFields verifies every tracked field surfaces in
// the diff when mutated.
func TestAuditConfigDiff_DetectsFields(t *testing.T) {
	t.Parallel()
	prev := config.Default().Audit
	next := config.Default().Audit
	next.Retention = 48 * time.Hour
	next.FullContentRetention = 96 * time.Hour
	next.Collector.BatchInterval = 2 * time.Second
	next.Collector.BatchSize = 200
	next.Collector.ChannelCap = 8192
	next.Collector.SpillDir = "/tmp/other"
	next.Enabled = false
	next.Sinks = []config.AuditSinkConfig{{Name: "log", Type: "log", Config: nil}}

	changes := auditConfigDiff(prev, next)
	require.Len(t, changes, 8, "all tracked fields should appear in diff")

	byField := map[string]map[string]string{}
	for _, c := range changes {
		byField[c["field"]] = c
	}
	require.Contains(t, byField, "audit.enabled")
	require.Contains(t, byField, "audit.retention")
	require.Contains(t, byField, "audit.full_content_retention")
	require.Contains(t, byField, "audit.collector.batch_interval")
	require.Contains(t, byField, "audit.collector.batch_size")
	require.Contains(t, byField, "audit.collector.channel_cap")
	require.Contains(t, byField, "audit.collector.spill_dir")
	require.Contains(t, byField, "audit.sinks")
	// Spot-check old/new values.
	require.Equal(t, prev.Retention.String(), byField["audit.retention"]["old"])
	require.Equal(t, next.Retention.String(), byField["audit.retention"]["new"])
}

func TestAuditConfigDiff_RedactsSinkSecrets(t *testing.T) {
	t.Parallel()
	prev := config.Default().Audit
	next := config.Default().Audit
	prev.Sinks = []config.AuditSinkConfig{{
		Name: "siem", Type: "webhook",
		Config: map[string]any{"url": "https://example.test/audit", "secret": "OLD_FAKE_SECRET"},
	}}
	next.Sinks = []config.AuditSinkConfig{{
		Name: "siem", Type: "webhook",
		Config: map[string]any{"url": "https://example.test/audit", "secret": "NEW_FAKE_SECRET"},
	}}

	changes := auditConfigDiff(prev, next)
	require.Len(t, changes, 1)
	require.Equal(t, "audit.sinks", changes[0]["field"])
	require.NotContains(t, changes[0]["old"], "OLD_FAKE_SECRET")
	require.NotContains(t, changes[0]["new"], "NEW_FAKE_SECRET")
	require.Contains(t, changes[0]["old"], "[REDACTED]")
}

// TestEmitAuditConfigChange_Enqueues end-to-end: the callback produced by
// emitAuditConfigChange must enqueue exactly one system.audit_config_changed
// row into the collector when the audit config changes.
func TestEmitAuditConfigChange_Enqueues(t *testing.T) {
	t.Parallel()

	store := newAuditTestStore(t)
	spill, err := audit.OpenSpill(filepath.Join(t.TempDir(), "spill.wal"))
	require.NoError(t, err)
	collector := audit.NewCollector(store, spill, nil, slog.Default(), audit.CollectorConfig{
		ChannelCap: 64, BatchSize: 10, BatchInterval: 10 * time.Millisecond,
	})
	collector.Start(context.Background())
	t.Cleanup(func() { _ = collector.Close(context.Background()) })

	cb := emitAuditConfigChange(collector)
	prev := config.Default()
	next := config.Default()
	next.Audit.Retention = 10000 * time.Hour
	next.Audit.Collector.BatchSize = 250

	cb(prev, next)

	// Wait for async flush.
	require.Eventually(t, func() bool {
		return store.RowCount(t) >= 1
	}, 2*time.Second, 5*time.Millisecond, "config-change audit row should be persisted")

	rows := store.AllRows(t)
	require.Len(t, rows, 1, "exactly one meta-audit row expected per reload")
	r := rows[0]
	require.Equal(t, audit.ActionSystemAuditConfigChanged, r.Action)
	require.Equal(t, audit.OutcomeSuccess, r.Outcome)
	require.Equal(t, audit.UserIDTypeSystem, r.UserIDType)
	require.Equal(t, "system", r.UserID)
	require.Equal(t, audit.PlatformAdmin, r.Platform)
	require.NotEmpty(t, r.DetailJSON)
	require.Contains(t, r.DetailJSON, "audit.retention")
	require.Contains(t, r.DetailJSON, "audit.collector.batch_size")
}

// TestEmitAuditConfigChange_NoChangeNoop verifies the callback is a no-op when
// the audit config did not change (avoids spurious audit noise on unrelated
// config reloads).
func TestEmitAuditConfigChange_NoChangeNoop(t *testing.T) {
	t.Parallel()

	store := newAuditTestStore(t)
	spill, err := audit.OpenSpill(filepath.Join(t.TempDir(), "spill.wal"))
	require.NoError(t, err)
	collector := audit.NewCollector(store, spill, nil, slog.Default(), audit.CollectorConfig{
		ChannelCap: 64, BatchSize: 10, BatchInterval: 10 * time.Millisecond,
	})
	collector.Start(context.Background())
	t.Cleanup(func() { _ = collector.Close(context.Background()) })

	cb := emitAuditConfigChange(collector)
	base := config.Default()
	cb(base, base) // identical audit section

	// Give the collector a moment, then assert nothing was enqueued.
	time.Sleep(50 * time.Millisecond)
	require.Equal(t, int64(0), collector.Enqueued(), "no row should be enqueued when audit config is unchanged")
}

// TestEmitAuditConfigChange_PartialChange verifies that when only one audit
// field changes, the detail_json records exactly that field (no noise from
// unchanged fields).
func TestEmitAuditConfigChange_PartialChange(t *testing.T) {
	t.Parallel()
	store := newAuditTestStore(t)
	spill, err := audit.OpenSpill(filepath.Join(t.TempDir(), "spill.wal"))
	require.NoError(t, err)
	collector := audit.NewCollector(store, spill, nil, slog.Default(), audit.CollectorConfig{
		ChannelCap: 64, BatchSize: 10, BatchInterval: 10 * time.Millisecond,
	})
	collector.Start(context.Background())
	t.Cleanup(func() { _ = collector.Close(context.Background()) })

	cb := emitAuditConfigChange(collector)
	prev, next := config.Default(), config.Default()
	// Only one field changes.
	next.Audit.Collector.BatchSize = 42

	cb(prev, next)

	require.Eventually(t, func() bool { return store.RowCount(t) >= 1 },
		2*time.Second, 5*time.Millisecond)
	rows := store.AllRows(t)
	require.Len(t, rows, 1)
	// detail_json must mention the changed field and not the unchanged ones.
	require.Contains(t, rows[0].DetailJSON, "audit.collector.batch_size")
	require.NotContains(t, rows[0].DetailJSON, "audit.retention")
	require.NotContains(t, rows[0].DetailJSON, "audit.enabled")
}
