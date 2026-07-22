package observability

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The cardinality contract: no high-cardinality correlation key may be a metric
// label. A per-session/per-request id as a Prometheus label explodes the label
// set and starves the scraper. This guards the allowlist against accidental
// addition.
func TestHighCardinalityKeysNotMetricSafe(t *testing.T) {
	t.Parallel()
	high := []string{
		KeyAgentID, KeyUserID, KeyWorkspaceID, KeyExecutionID, KeySessionID,
	}
	for _, k := range high {
		require.False(t, IsMetricSafe(k), "high-cardinality key %q must NOT be a metric label", k)
	}
}

// Bounded enum keys — the intended metric label population — must be metric-safe.
func TestBoundedKeysAreMetricSafe(t *testing.T) {
	t.Parallel()
	bounded := []string{
		KeyWorkerType, KeyPlatform, KeyEventType, KeyDirection,
		KeyReason, KeyStatus, KeyErrorType, KeyExitCode,
	}
	for _, k := range bounded {
		require.True(t, IsMetricSafe(k), "bounded key %q should be metric-safe", k)
	}
}

// trace_id/span_id are low-cardinality within a sampling window but carry no
// useful distributional meaning as labels, so they are deliberately excluded to
// keep metric label sets bounded.
func TestTraceKeysNotMetricSafe(t *testing.T) {
	t.Parallel()
	require.False(t, IsMetricSafe(KeyTraceID))
	require.False(t, IsMetricSafe(KeySpanID))
}

// IsMetricSafe must reject unknown keys (default-deny).
func TestUnknownKeyNotMetricSafe(t *testing.T) {
	t.Parallel()
	require.False(t, IsMetricSafe("definitely_not_a_key"))
}
