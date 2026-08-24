package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveGatewayRestartAllowFrom(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		platform []string
		bot      []string
		want     []string
		wantNil  bool
	}{
		{name: "platform default deny", wantNil: true},
		{name: "platform allowlist", platform: []string{"ou_platform"}, want: []string{"ou_platform"}},
		{name: "bot inherits when nil", platform: []string{"ou_platform"}, want: []string{"ou_platform"}},
		{name: "bot overrides platform", platform: []string{"ou_platform"}, bot: []string{"ou_bot"}, want: []string{"ou_bot"}},
		{name: "explicit empty bot list disables", platform: []string{"ou_platform"}, bot: []string{}, want: []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveGatewayRestartAllowFrom(tt.platform, tt.bot)
			if tt.wantNil {
				require.Nil(t, got)
				return
			}
			require.Equal(t, tt.want, got)
		})
	}
}

func TestApplyMessagingEnv_FeishuGatewayRestartAllowFrom(t *testing.T) {
	t.Setenv("HOTPLEX_MESSAGING_FEISHU_GATEWAY_RESTART_ALLOW_FROM", "ou_one, ou_two")

	cfg := Default()
	applyMessagingEnv(cfg)

	require.Equal(t, []string{"ou_one", "ou_two"}, cfg.Messaging.Feishu.GatewayRestartAllowFrom)
}
