package config

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestConfig_Warnings_GatewayIdleTimeoutDeadConfig verifies Q3 (#817):
// gateway.idle_timeout is a dead config — session IDLE GC uses
// worker.idle_timeout. Warn when an operator changed it from the default,
// since the change silently has no effect.
func TestConfig_Warnings_GatewayIdleTimeoutDeadConfig(t *testing.T) {
	t.Parallel()

	t.Run("non-default value warns", func(t *testing.T) {
		t.Parallel()
		c := *Default()
		c.Gateway.IdleTimeout = 10 * time.Minute // operator changed from 5m default

		warns := c.Warnings()
		found := false
		for _, w := range warns {
			if strings.Contains(w, "gateway.idle_timeout is unused") {
				found = true
			}
		}
		require.True(t, found, "expected dead-config warning, got %v", warns)
	})

	t.Run("default value silent", func(t *testing.T) {
		t.Parallel()
		c := *Default()
		for _, w := range c.Warnings() {
			require.NotContains(t, w, "gateway.idle_timeout is unused",
				"default config should not trigger dead-config warning")
		}
	})
}
