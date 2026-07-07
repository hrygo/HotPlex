package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestEffectiveEventsRetention_Defaults verifies the default config yields the
// audit full-content retention (90d > 30d events default).
func TestEffectiveEventsRetention_Defaults(t *testing.T) {
	t.Parallel()
	cfg := Default()
	// Sanity: defaults are as documented.
	require.Equal(t, 720*time.Hour, cfg.Events.Retention)
	require.True(t, cfg.Audit.Enabled)
	require.Equal(t, 2160*time.Hour, cfg.Audit.FullContentRetention)
	// Effective = max(720h, 2160h) = 2160h.
	require.Equal(t, 2160*time.Hour, EffectiveEventsRetention(cfg))
}

// TestEffectiveEventsRetention_AuditDisabledNoOverride verifies that when audit
// is disabled, the events retention is returned UNCHANGED even if
// full_content_retention is longer (audit must not silently lengthen TTL when
// the operator turned audit off).
func TestEffectiveEventsRetention_AuditDisabledNoOverride(t *testing.T) {
	t.Parallel()
	cfg := Default()
	cfg.Audit.Enabled = false
	cfg.Audit.FullContentRetention = 2160 * time.Hour // longer than events
	cfg.Events.Retention = 720 * time.Hour
	// Audit off → no override; events retention returned as-is.
	require.Equal(t, 720*time.Hour, EffectiveEventsRetention(cfg))
}

// TestEffectiveEventsRetention_EventsLongerThanFull verifies the max picks
// events.retention when it exceeds full_content_retention.
func TestEffectiveEventsRetention_EventsLongerThanFull(t *testing.T) {
	t.Parallel()
	cfg := Default()
	cfg.Audit.Enabled = true
	cfg.Events.Retention = 5000 * time.Hour
	cfg.Audit.FullContentRetention = 2160 * time.Hour
	// max(5000h, 2160h) = 5000h.
	require.Equal(t, 5000*time.Hour, EffectiveEventsRetention(cfg))
}

// TestEffectiveEventsRetention_EqualValues is the boundary case.
func TestEffectiveEventsRetention_EqualValues(t *testing.T) {
	t.Parallel()
	cfg := Default()
	cfg.Audit.Enabled = true
	cfg.Events.Retention = 1000 * time.Hour
	cfg.Audit.FullContentRetention = 1000 * time.Hour
	require.Equal(t, 1000*time.Hour, EffectiveEventsRetention(cfg))
}

// TestEffectiveEventsRetention_ZeroEventsFallback verifies a zero/empty events
// retention falls back to the 720h default before the max is taken.
func TestEffectiveEventsRetention_ZeroEventsFallback(t *testing.T) {
	t.Parallel()
	cfg := Default()
	cfg.Audit.Enabled = true
	cfg.Events.Retention = 0 // unset
	cfg.Audit.FullContentRetention = 100 * time.Hour
	// max(720h fallback, 100h) = 720h.
	require.Equal(t, 720*time.Hour, EffectiveEventsRetention(cfg))
}

// TestEffectiveEventsRetention_NilConfig guards against nil dereference.
func TestEffectiveEventsRetention_NilConfig(t *testing.T) {
	t.Parallel()
	require.Equal(t, 720*time.Hour, EffectiveEventsRetention(nil))
}

// TestEffectiveEventsRetention_ZeroFullContent verifies a zero
// full_content_retention does not shrink events retention (only extends).
func TestEffectiveEventsRetention_ZeroFullContent(t *testing.T) {
	t.Parallel()
	cfg := Default()
	cfg.Audit.Enabled = true
	cfg.Events.Retention = 720 * time.Hour
	cfg.Audit.FullContentRetention = 0 // unset
	// max(720h, 0h) = 720h — no change.
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
