package checkers

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/agentspec"
	"github.com/hrygo/hotplex/internal/cli"
	"github.com/hrygo/hotplex/internal/config"
)

var effectivePlanHashRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

func TestEffectivePlanChecker_Identity(t *testing.T) {
	t.Parallel()
	c := effectivePlanChecker{}
	require.Equal(t, "runtime.effective_plan", c.Name())
	require.Equal(t, "runtime", c.Category())
}

// Tests that touch the package-level configPath must NOT use t.Parallel
// (see withConfigPath in config_test.go).

func TestEffectivePlanChecker_NoConfig(t *testing.T) {
	withConfigPath(t, "")

	c := effectivePlanChecker{}
	d := c.Check(context.Background())

	require.Equal(t, cli.StatusPass, d.Status)
	require.Contains(t, d.Message, "No config loaded")
}

func TestEffectivePlanChecker_LoadError(t *testing.T) {
	withConfigPath(t, "/nonexistent/config.yaml")

	c := effectivePlanChecker{}
	d := c.Check(context.Background())

	require.Equal(t, cli.StatusWarn, d.Status)
	require.Contains(t, d.Message, "Cannot load config")
	require.NotEmpty(t, d.FixHint)
}

// TestEffectivePlanChecker_BlockedPlans_Warn: the test binary's worker
// registry is empty, so every config-driven messaging probe is blocked
// fail-closed (unknown_worker_type). Doctor must surface that as a warning —
// a blocked plan is never a silent pass (#946 spec §6.6).
func TestEffectivePlanChecker_BlockedPlans_Warn(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `gateway:
  addr: ":8888"
admin:
  addr: ":9999"
  enabled: true
db:
  path: "data/test.db"
`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	withConfigPath(t, path)

	c := effectivePlanChecker{}
	d := c.Check(context.Background())

	require.Equal(t, cli.StatusWarn, d.Status)
	require.Contains(t, d.Detail, "BLOCKED="+agentspec.BlockUnknownWorkerType)
	require.Contains(t, d.Message, "blocked")
	require.NotEmpty(t, d.FixHint)
}

// probeEffectivePlans is pure given cfg — the tests below are parallel-safe.

func TestProbeEffectivePlans_NilConfig_Unresolved(t *testing.T) {
	t.Parallel()

	results := probeEffectivePlans(context.Background(), nil)

	require.Len(t, results, len(messagingProbePlatforms))
	for _, res := range results {
		require.Regexp(t, effectivePlanHashRe, res.hash)
		require.Equal(t, "unresolved", res.worker)
		require.Empty(t, res.blocked)
		require.Contains(t, res.warnings, agentspec.WarnWorkerTypeUnresolved)
	}
}

// TestProbeEffectivePlans_DefaultConfig_BlockedByEmptyRegistry documents the
// registry boundary: with nothing registered (the test binary), the shared
// resolver rejects the compile-default worker type and every probe is blocked
// with no plan hash.
func TestProbeEffectivePlans_DefaultConfig_BlockedByEmptyRegistry(t *testing.T) {
	t.Parallel()

	results := probeEffectivePlans(context.Background(), config.Default())

	require.Len(t, results, len(messagingProbePlatforms))
	for _, res := range results {
		require.Empty(t, res.hash, "a blocked plan carries no hash")
		require.Equal(t, []string{agentspec.BlockUnknownWorkerType}, res.blocked)
	}
}

func TestProbeEffectivePlans_CancelledContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	results := probeEffectivePlans(ctx, config.Default())

	require.Empty(t, results)
}

func TestDisplayOr(t *testing.T) {
	t.Parallel()
	require.Equal(t, "value", displayOr("value", "fallback"))
	require.Equal(t, "fallback", displayOr("", "fallback"))
}
