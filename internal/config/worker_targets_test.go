package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnabledWorkerTypesUsesResolvedPlatformAndBotValues(t *testing.T) {
	cfg := Config{
		Messaging: MessagingConfig{
			Slack: SlackConfig{
				MessagingPlatformConfig: MessagingPlatformConfig{
					Enabled: true, WorkerType: "claude_code",
				},
				Bots: []SlackBotConfig{
					{Name: "z", WorkerType: "codex_cli"},
					{Name: "a", WorkerType: "claude_code"},
				},
			},
			Feishu: FeishuConfig{
				MessagingPlatformConfig: MessagingPlatformConfig{
					Enabled: true, WorkerType: "codex_cli",
				},
			},
		},
	}
	require.Equal(t, []string{"claude_code", "codex_cli"}, cfg.EnabledWorkerTypes())
}

func TestEnabledWorkerTypesExcludesDisabledPlatformsAndDoesNotUseRegistry(t *testing.T) {
	cfg := Config{
		Messaging: MessagingConfig{
			Slack: SlackConfig{
				MessagingPlatformConfig: MessagingPlatformConfig{Enabled: false},
				Bots:                    []SlackBotConfig{{Name: "disabled", WorkerType: "acp"}},
			},
		},
	}
	require.Empty(t, cfg.EnabledWorkerTypes())
}

func TestEnabledWorkerTypesIncludesYuanxinAndPlatformFallback(t *testing.T) {
	cfg := Config{
		Messaging: MessagingConfig{
			Yuanxin: YuanxinConfig{MessagingPlatformConfig: MessagingPlatformConfig{
				Enabled: true, WorkerType: "opencode_server",
			}},
		},
	}
	require.Equal(t, []string{"opencode_server"}, cfg.EnabledWorkerTypes())
}

func TestEnabledWorkerTypesResolvesEmptyBotWorkerType(t *testing.T) {
	cfg := Config{
		Messaging: MessagingConfig{
			Slack: SlackConfig{
				MessagingPlatformConfig: MessagingPlatformConfig{Enabled: true, WorkerType: "codex_cli"},
				Bots: []SlackBotConfig{
					{Name: "inherits-platform"},
					{Name: "overrides-platform", WorkerType: "opencode_server"},
				},
			},
		},
	}
	require.Equal(t, []string{"codex_cli", "opencode_server"}, cfg.EnabledWorkerTypes())
}

func TestEnabledWorkerTypesDeduplicatesDefaultWorkerAndReturnsNonNilEmpty(t *testing.T) {
	cfg := Config{
		Messaging: MessagingConfig{
			Slack:  SlackConfig{MessagingPlatformConfig: MessagingPlatformConfig{Enabled: true}},
			Feishu: FeishuConfig{MessagingPlatformConfig: MessagingPlatformConfig{Enabled: true}},
		},
	}
	require.Equal(t, []string{DefaultWorkerType}, cfg.EnabledWorkerTypes())

	var nilConfig *Config
	got := nilConfig.EnabledWorkerTypes()
	require.NotNil(t, got)
	require.Empty(t, got)

	emptyConfig := Config{}
	require.NotNil(t, emptyConfig.EnabledWorkerTypes())
	require.Empty(t, emptyConfig.EnabledWorkerTypes())
}
