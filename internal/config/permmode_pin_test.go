package config_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/worker"
)

// TestConfigAgreesWithWorkerPermissionModes pins config.Validate's valid-set to the worker.PermissionMode* constants (config can't import worker; drift would silently fail-open).
func TestConfigAgreesWithWorkerPermissionModes(t *testing.T) {
	t.Parallel()
	for _, tier := range []string{
		worker.PermissionModeReadOnly,
		worker.PermissionModeWorkspace,
		worker.PermissionModeAutoEdit,
		worker.PermissionModeBypass,
	} {
		c := *config.Default()
		c.Worker.DefaultPermissionMode = tier
		require.Empty(t, c.Validate(), "worker tier %q should be accepted by config.Validate", tier)
	}
}
