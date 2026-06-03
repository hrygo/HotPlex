package groupchat

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	require.False(t, cfg.Enabled)
	require.Equal(t, 15, cfg.MaxTurns)
	require.Equal(t, 120, cfg.TurnTimeoutSec)
	require.Equal(t, 5000, cfg.CooldownMS)
	require.Equal(t, 20, cfg.MaxGroupSessions)
	require.Equal(t, 2, cfg.MaxSessionsPerUser)
	require.Equal(t, 50000, cfg.MaxTurnContentLength)
	require.Equal(t, 80000, cfg.MaxTotalContextLength)
	require.Equal(t, 1.00, cfg.CostLimitUSD)
	require.Equal(t, 500, cfg.MaxTopicLength)
	require.Equal(t, 10, cfg.PoolReservation)
}

func TestConfig_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		modify  func(*Config)
		wantErr bool
		errMsg  string
	}{
		{
			name:    "disabled is always valid",
			modify:  func(c *Config) { c.Enabled = false },
			wantErr: false,
		},
		{
			name: "valid enabled config",
			modify: func(c *Config) {
				c.Enabled = true
			},
			wantErr: false,
		},
		{
			name: "max_turns zero",
			modify: func(c *Config) {
				c.Enabled = true
				c.MaxTurns = 0
			},
			wantErr: true,
			errMsg:  "max_turns must be > 0",
		},
		{
			name: "max_turns negative",
			modify: func(c *Config) {
				c.Enabled = true
				c.MaxTurns = -5
			},
			wantErr: true,
			errMsg:  "max_turns must be > 0",
		},
		{
			name: "turn_timeout_sec zero",
			modify: func(c *Config) {
				c.Enabled = true
				c.TurnTimeoutSec = 0
			},
			wantErr: true,
			errMsg:  "turn_timeout_sec must be > 0",
		},
		{
			name: "max_group_sessions zero",
			modify: func(c *Config) {
				c.Enabled = true
				c.MaxGroupSessions = 0
			},
			wantErr: true,
			errMsg:  "max_group_sessions must be > 0",
		},
		{
			name: "max_sessions_per_user zero",
			modify: func(c *Config) {
				c.Enabled = true
				c.MaxSessionsPerUser = 0
			},
			wantErr: true,
			errMsg:  "max_sessions_per_user must be > 0",
		},
		{
			name: "cost_limit_usd negative",
			modify: func(c *Config) {
				c.Enabled = true
				c.CostLimitUSD = -0.01
			},
			wantErr: true,
			errMsg:  "cost_limit_usd must be >= 0",
		},
		{
			name: "cost_limit_usd zero is valid",
			modify: func(c *Config) {
				c.Enabled = true
				c.CostLimitUSD = 0
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := DefaultConfig()
			tt.modify(&cfg)
			err := cfg.Validate()
			if tt.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestConfig_TurnTimeout(t *testing.T) {
	t.Parallel()

	cfg := Config{TurnTimeoutSec: 90}
	require.Equal(t, 90*time.Second, cfg.TurnTimeout())
}

func TestConfig_Cooldown(t *testing.T) {
	t.Parallel()

	cfg := Config{CooldownMS: 3000}
	require.Equal(t, 3*time.Second, cfg.Cooldown())
}
