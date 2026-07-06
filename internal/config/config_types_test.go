package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDefaultAuditConfig(t *testing.T) {
	t.Parallel()

	cfg := Default()

	// Audit block must be present with expected defaults.
	require.True(t, cfg.Audit.Enabled, "Audit.Enabled should default to true")
	require.Equal(t, 26280*time.Hour, cfg.Audit.Retention, "Audit.Retention should default to 26280h (3 years)")
	require.Equal(t, 4096, cfg.Audit.Collector.ChannelCap, "Audit.Collector.ChannelCap should default to 4096")
	require.Equal(t, 1*time.Second, cfg.Audit.Collector.BatchInterval, "Audit.Collector.BatchInterval should default to 1s")
	require.Equal(t, 100, cfg.Audit.Collector.BatchSize, "Audit.Collector.BatchSize should default to 100")
	require.Contains(t, cfg.Audit.Collector.SpillDir, "audit-spill", "Audit.Collector.SpillDir should contain audit-spill")

	// Default sink must be the noop sink.
	require.Len(t, cfg.Audit.Sinks, 1, "should have exactly one default sink")
	require.Equal(t, "noop", cfg.Audit.Sinks[0].Name, "default sink name should be noop")
	require.Equal(t, "noop", cfg.Audit.Sinks[0].Type, "default sink type should be noop")
}
