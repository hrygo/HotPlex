package groupchat

import (
	"fmt"
	"time"
)

// Config holds the group chat configuration from config.yaml.
type Config struct {
	Enabled               bool    `mapstructure:"enabled"`
	MaxTurns              int     `mapstructure:"max_turns"`
	TurnTimeoutSec        int     `mapstructure:"turn_timeout_sec"`
	CooldownMS            int     `mapstructure:"cooldown_ms"`
	MaxGroupSessions      int     `mapstructure:"max_group_sessions"`
	MaxSessionsPerUser    int     `mapstructure:"max_sessions_per_user"`
	MaxTurnContentLength  int     `mapstructure:"max_turn_content_length"`
	MaxTotalContextLength int     `mapstructure:"max_total_context_length"`
	CostLimitUSD          float64 `mapstructure:"cost_limit_usd"`
	MaxTopicLength        int     `mapstructure:"max_topic_length"`
	PoolReservation       int     `mapstructure:"pool_reservation"`
}

// DefaultConfig returns sensible defaults matching the spec.
func DefaultConfig() Config {
	return Config{
		Enabled:               false,
		MaxTurns:              15,
		TurnTimeoutSec:        120,
		CooldownMS:            5000,
		MaxGroupSessions:      20,
		MaxSessionsPerUser:    2,
		MaxTurnContentLength:  50000,
		MaxTotalContextLength: 80000,
		CostLimitUSD:          1.00,
		MaxTopicLength:        500,
		PoolReservation:       10,
	}
}

// Validate checks config consistency. Returns error on invalid values.
func (c Config) Validate() error {
	if !c.Enabled {
		return nil
	}
	if c.MaxTurns <= 0 {
		return fmt.Errorf("groupchat: max_turns must be > 0, got %d", c.MaxTurns)
	}
	if c.TurnTimeoutSec <= 0 {
		return fmt.Errorf("groupchat: turn_timeout_sec must be > 0, got %d", c.TurnTimeoutSec)
	}
	if c.MaxGroupSessions <= 0 {
		return fmt.Errorf("groupchat: max_group_sessions must be > 0, got %d", c.MaxGroupSessions)
	}
	if c.MaxSessionsPerUser <= 0 {
		return fmt.Errorf("groupchat: max_sessions_per_user must be > 0, got %d", c.MaxSessionsPerUser)
	}
	if c.CostLimitUSD < 0 {
		return fmt.Errorf("groupchat: cost_limit_usd must be >= 0, got %.2f", c.CostLimitUSD)
	}
	return nil
}

// TurnTimeout returns the per-turn timeout as a time.Duration.
func (c Config) TurnTimeout() time.Duration {
	return time.Duration(c.TurnTimeoutSec) * time.Second
}

// Cooldown returns the inter-turn cooldown as a time.Duration.
func (c Config) Cooldown() time.Duration {
	return time.Duration(c.CooldownMS) * time.Millisecond
}
