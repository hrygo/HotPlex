package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestResolveWorkerType exercises the documented 5-level worker_type fallback
// (#847 / F3): per-bot > platform(YAML/env) > messaging shared default > compile
// default (claude_code).
func TestResolveWorkerType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		platform string
		botName  string
		mutate   func(*Config)
		want     string
	}{
		{
			name:     "level5 compile default when nothing set",
			platform: "slack",
			mutate:   func(c *Config) { c.Messaging.WorkerType = "" },
			want:     "claude_code",
		},
		{
			name:     "level4 messaging shared default",
			platform: "slack",
			mutate:   func(c *Config) { c.Messaging.WorkerType = "codex_cli" },
			want:     "codex_cli",
		},
		{
			name:     "level2 platform overrides messaging",
			platform: "slack",
			mutate: func(c *Config) {
				c.Messaging.WorkerType = "codex_cli"
				c.Messaging.Slack.WorkerType = "acp"
			},
			want: "acp",
		},
		{
			name:     "level1 per-bot overrides platform",
			platform: "slack",
			botName:  "b1",
			mutate: func(c *Config) {
				c.Messaging.WorkerType = "codex_cli"
				c.Messaging.Slack.WorkerType = "acp"
				c.Messaging.Slack.Bots = []SlackBotConfig{{Name: "b1", WorkerType: "opencode_server"}}
			},
			want: "opencode_server",
		},
		{
			name:     "feishu per-bot override",
			platform: "feishu",
			botName:  "fb",
			mutate: func(c *Config) {
				c.Messaging.Feishu.Bots = []FeishuBotConfig{{Name: "fb", WorkerType: "codex_cli"}}
			},
			want: "codex_cli",
		},
		{
			name:     "feishu platform level",
			platform: "feishu",
			mutate:   func(c *Config) { c.Messaging.Feishu.WorkerType = "acp" },
			want:     "acp",
		},
		{
			name:     "yuanxin platform level",
			platform: "yuanxin",
			mutate:   func(c *Config) { c.Messaging.Yuanxin.WorkerType = "opencode_server" },
			want:     "opencode_server",
		},
		{
			name:     "unknown bot falls through to platform",
			platform: "slack",
			botName:  "ghost",
			mutate: func(c *Config) {
				c.Messaging.Slack.WorkerType = "acp"
				c.Messaging.Slack.Bots = []SlackBotConfig{{Name: "b1", WorkerType: "opencode_server"}}
			},
			want: "acp",
		},
		{
			name:     "per-bot empty worker_type falls through",
			platform: "slack",
			botName:  "b1",
			mutate: func(c *Config) {
				c.Messaging.Slack.WorkerType = "acp"
				c.Messaging.Slack.Bots = []SlackBotConfig{{Name: "b1"}} // no worker_type
			},
			want: "acp",
		},
		{
			name:     "unknown platform uses messaging then compile default",
			platform: "webchat",
			mutate:   func(c *Config) { c.Messaging.WorkerType = "" },
			want:     "claude_code",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := Default()
			tc.mutate(cfg)
			require.Equal(t, tc.want, cfg.ResolveWorkerType(tc.platform, tc.botName))
		})
	}
}
