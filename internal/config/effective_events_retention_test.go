package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestEffectiveEventsRetention_Defaults verifies that event and audit retention
// are independent, even when the audit full-content window is longer.
func TestEffectiveEventsRetention_Defaults(t *testing.T) {
	t.Parallel()
	cfg := Default()
	// Sanity: defaults are as documented.
	require.Equal(t, 720*time.Hour, cfg.Events.Retention)
	require.True(t, cfg.Audit.Enabled)
	require.Equal(t, 2160*time.Hour, cfg.Audit.FullContentRetention)
	// Event storage retains its own 30-day window; audit retains plaintext
	// independently for its configured 90-day window.
	require.Equal(t, 720*time.Hour, EffectiveEventsRetention(cfg))
}

// TestEffectiveEventsRetention_AuditDisabledNoOverride verifies that disabling
// audit leaves the independent event retention unchanged.
func TestEffectiveEventsRetention_AuditDisabledNoOverride(t *testing.T) {
	t.Parallel()
	cfg := Default()
	cfg.Audit.Enabled = false
	cfg.Audit.FullContentRetention = 2160 * time.Hour // longer than events
	cfg.Events.Retention = 720 * time.Hour
	// Audit off → no override; events retention returned as-is.
	require.Equal(t, 720*time.Hour, EffectiveEventsRetention(cfg))
}

// TestEffectiveEventsRetention_EventsLongerThanFull verifies that audit
// full-content retention never changes events.retention.
func TestEffectiveEventsRetention_EventsLongerThanFull(t *testing.T) {
	t.Parallel()
	cfg := Default()
	cfg.Audit.Enabled = true
	cfg.Events.Retention = 5000 * time.Hour
	cfg.Audit.FullContentRetention = 2160 * time.Hour
	// Event retention remains operator-configured at 5000h.
	require.Equal(t, 5000*time.Hour, EffectiveEventsRetention(cfg))
}

// TestEffectiveEventsRetention_EqualValues is the equal-window boundary case.
func TestEffectiveEventsRetention_EqualValues(t *testing.T) {
	t.Parallel()
	cfg := Default()
	cfg.Audit.Enabled = true
	cfg.Events.Retention = 1000 * time.Hour
	cfg.Audit.FullContentRetention = 1000 * time.Hour
	require.Equal(t, 1000*time.Hour, EffectiveEventsRetention(cfg))
}

// TestEffectiveEventsRetention_ZeroEventsFallback verifies a zero/empty events
// retention falls back to the independent 720h default.
func TestEffectiveEventsRetention_ZeroEventsFallback(t *testing.T) {
	t.Parallel()
	cfg := Default()
	cfg.Audit.Enabled = true
	cfg.Events.Retention = 0 // unset
	cfg.Audit.FullContentRetention = 100 * time.Hour
	// The 720h event fallback is not affected by audit retention.
	require.Equal(t, 720*time.Hour, EffectiveEventsRetention(cfg))
}

// TestEffectiveEventsRetention_NilConfig guards against nil dereference.
func TestEffectiveEventsRetention_NilConfig(t *testing.T) {
	t.Parallel()
	require.Equal(t, 720*time.Hour, EffectiveEventsRetention(nil))
}

// TestEffectiveEventsRetention_ZeroFullContent verifies a zero
// full_content_retention does not affect events retention.
func TestEffectiveEventsRetention_ZeroFullContent(t *testing.T) {
	t.Parallel()
	cfg := Default()
	cfg.Audit.Enabled = true
	cfg.Events.Retention = 720 * time.Hour
	cfg.Audit.FullContentRetention = 0 // unset
	// Event retention remains 720h.
	require.Equal(t, 720*time.Hour, EffectiveEventsRetention(cfg))
}

// TestAuditFullContentRetention_BindEnv is a smoke test that the env var name
// matches the documented HOTPLEX_AUDIT_FULL_CONTENT_RETENTION binding. We
// don't fully exercise viper here (config_loader_test covers env binding);
// this just locks the field's mapstructure tag to the watched key.
func TestAuditFullContentRetention_MapstructureTag(t *testing.T) {
	t.Parallel()
	cfg := Default()
	// The watcher tracks "audit.full_content_retention" which must match the
	// mapstructure tag on AuditConfig.FullContentRetention. Resolve it via the
	// same reflection the watcher uses.
	got := resolveField(cfg, "audit.full_content_retention")
	require.Equal(t, (2160 * time.Hour).String(), got,
		"audit.full_content_retention must resolve to the default 2160h via the mapstructure tag")
}
