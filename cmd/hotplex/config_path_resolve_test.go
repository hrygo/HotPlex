package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/config"
)

func TestResolveRestartConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		configPath     string
		changed        bool
		instConfigPath string
		want           string
	}{
		{"explicit path wins", "/x/explicit.yaml", true, "/x/running.yaml", "/x/explicit.yaml"},
		{"explicit default absolute wins", config.DefaultConfigPath(), true, "/x/running.yaml", config.DefaultConfigPath()},
		{"unset preserves instance path", "/x/default.yaml", false, "/x/running.yaml", "/x/running.yaml"},
		{"unset without instance keeps passed path", "/x/default.yaml", false, "", "/x/default.yaml"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, resolveRestartConfig(tt.configPath, tt.changed, tt.instConfigPath))
		})
	}
}

func TestTriggerConfigPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		configPath string
		changed    bool
		want       string
	}{
		{"unset maps to empty for callee resolution", "/x/config.yaml", false, ""},
		{"explicit path preserved", "/x/config.yaml", true, "/x/config.yaml"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, triggerConfigPath(tt.configPath, tt.changed))
		})
	}
}
