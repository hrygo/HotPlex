package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/admin"
	"github.com/hrygo/hotplex/internal/agentconfig"
	"github.com/hrygo/hotplex/internal/config"
)

// newTestBotConfigAdapter builds a botConfigAdapter over a temp agent-config
// directory backed by an empty config store. Used to exercise platform-level
// (channel team-default) agent config read/write without the messaging
// registry — the webchat path has no bot instance.
func newTestBotConfigAdapter(t *testing.T) (*botConfigAdapter, string) {
	t.Helper()
	dir := t.TempDir()
	cfgStore := config.NewConfigStore(&config.Config{}, slog.Default())
	return newBotConfigAdapter(cfgStore, dir, filepath.Join(dir, "config.yaml")), dir
}

// Write then read a webchat platform-level file; verify it lands at
// dir/webchat/<file> and reports source "platform".
func TestBotConfigAdapter_PlatformAgentConfigFile_WriteRead(t *testing.T) {
	t.Parallel()
	a, dir := newTestBotConfigAdapter(t)

	content := "# WebChat Soul\nYou are a webchat channel agent.\n"
	require.NoError(t, a.WritePlatformAgentConfigFile(
		context.Background(), "webchat", admin.AgentConfigSoul, content))

	got, err := a.GetPlatformAgentConfigFile(context.Background(), "webchat", admin.AgentConfigSoul)
	require.NoError(t, err)
	require.Equal(t, content, got.Content)
	require.Equal(t, "platform", got.Source) // platform-level team default
	require.Equal(t, "SOUL.md", got.File)
	require.Equal(t, len(content), got.Size)

	// Verify on-disk location: dir/webchat/SOUL.md.
	data, err := os.ReadFile(filepath.Join(dir, "webchat", "SOUL.md"))
	require.NoError(t, err)
	require.Contains(t, string(data), "WebChat Soul")
}

func TestBotConfigAdapter_PlatformToolsConfig_WriteRead(t *testing.T) {
	t.Parallel()
	a, dir := newTestBotConfigAdapter(t)

	content := "# Tool guidance\nUse the runtime tool catalog as authority.\n"
	require.NoError(t, a.WritePlatformAgentConfigFile(
		context.Background(), "webchat", admin.AgentConfigTools, content))

	got, err := a.GetPlatformAgentConfigFile(context.Background(), "webchat", admin.AgentConfigTools)
	require.NoError(t, err)
	require.Equal(t, content, got.Content)
	require.Equal(t, "platform", got.Source)
	require.Equal(t, "TOOLS.md", got.File)

	data, err := os.ReadFile(filepath.Join(dir, "webchat", "TOOLS.md"))
	require.NoError(t, err)
	require.Equal(t, content, string(data))
	_, err = os.Stat(filepath.Join(dir, "webchat", "SKILLS.md"))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestBotConfigAdapter_PlatformToolsConfig_IgnoresLegacyFile(t *testing.T) {
	t.Parallel()
	a, dir := newTestBotConfigAdapter(t)

	legacyContent := "legacy tool guidance"
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "webchat"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "webchat", "SKILLS.md"), []byte(legacyContent), 0o644))

	got, err := a.GetPlatformAgentConfigFile(context.Background(), "webchat", admin.AgentConfigTools)
	require.NoError(t, err)
	require.Empty(t, got.Content)
	require.Empty(t, got.Source)
	require.Equal(t, "TOOLS.md", got.File)

	summary := getAgentConfigSummary("webchat", "", dir)
	require.NotNil(t, summary)
	require.Nil(t, summary.Tools)
}

func TestBotConfigAdapter_PlatformToolsConfig_CanonicalSummary(t *testing.T) {
	t.Parallel()
	a, dir := newTestBotConfigAdapter(t)

	content := "canonical tool guidance"
	require.NoError(t, a.WritePlatformAgentConfigFile(
		context.Background(), "webchat", admin.AgentConfigTools, content))

	summary := getAgentConfigSummary("webchat", "", dir)
	require.NotNil(t, summary)
	require.Equal(t, &admin.AgentConfigMeta{Source: "platform", Size: len(content)}, summary.Tools)
}

func TestBotConfigAdapter_PlatformToolsConfig_PresentEmptySummary(t *testing.T) {
	t.Parallel()
	_, dir := newTestBotConfigAdapter(t)

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "webchat"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "webchat", "TOOLS.md"), nil, 0o644))

	summary := getAgentConfigSummary("webchat", "", dir)
	require.NotNil(t, summary)
	require.Equal(t, &admin.AgentConfigMeta{Source: "platform", Size: 0}, summary.Tools)
}

// The written platform-level file must serve as the webchat channel team
// default via LoadForWorkspace, and be overridden per-workspace following the
// existing precedence (issue #796 acceptance ②).
func TestBotConfigAdapter_PlatformAgentConfigFile_TeamDefaultEffective(t *testing.T) {
	t.Parallel()
	a, dir := newTestBotConfigAdapter(t)

	content := "# WebChat Rules\n- Be concise.\n"
	require.NoError(t, a.WritePlatformAgentConfigFile(
		context.Background(), "webchat", admin.AgentConfigAgents, content))

	// No override → platform-level team default resolves.
	cfg, err := agentconfig.LoadForWorkspace(dir, "webchat", nil)
	require.NoError(t, err)
	require.Equal(t, content, cfg.Agents)

	// Non-empty override replaces the team default.
	override := map[string]string{"AGENTS.md": "# Override\nWorkspace-specific rules."}
	cfg2, err := agentconfig.LoadForWorkspace(dir, "webchat", override)
	require.NoError(t, err)
	require.Equal(t, "# Override\nWorkspace-specific rules.", cfg2.Agents)

	// Empty override value explicitly clears the team default.
	cfg3, err := agentconfig.LoadForWorkspace(dir, "webchat", map[string]string{"AGENTS.md": ""})
	require.NoError(t, err)
	require.Empty(t, cfg3.Agents)
}

// Unknown platforms are rejected before any path resolution (defense-in-depth
// against arbitrary directory creation).
func TestBotConfigAdapter_PlatformAgentConfigFile_UnknownPlatformRejected(t *testing.T) {
	t.Parallel()
	a, dir := newTestBotConfigAdapter(t)

	_, err := a.GetPlatformAgentConfigFile(context.Background(), "telegram", admin.AgentConfigSoul)
	require.Error(t, err)

	err = a.WritePlatformAgentConfigFile(context.Background(), "telegram", admin.AgentConfigSoul, "x")
	require.Error(t, err)

	// No directory created for the rejected platform.
	_, statErr := os.Stat(filepath.Join(dir, "telegram"))
	require.Error(t, statErr) // not exist
	require.True(t, os.IsNotExist(statErr))
}

// Without a platform-level file, reads fall through to the global layer.
func TestBotConfigAdapter_PlatformAgentConfigFile_GlobalFallback(t *testing.T) {
	t.Parallel()
	a, dir := newTestBotConfigAdapter(t)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte("global memory"), 0o644))
	got, err := a.GetPlatformAgentConfigFile(context.Background(), "webchat", admin.AgentConfigMemory)
	require.NoError(t, err)
	require.Equal(t, "global memory", got.Content)
	require.Equal(t, "global", got.Source)
}

// Each recognized platform gets its own team-default namespace.
func TestBotConfigAdapter_PlatformAgentConfigFile_PerPlatformIsolation(t *testing.T) {
	t.Parallel()
	a, _ := newTestBotConfigAdapter(t)

	require.NoError(t, a.WritePlatformAgentConfigFile(
		context.Background(), "webchat", admin.AgentConfigUser, "webchat user"))
	require.NoError(t, a.WritePlatformAgentConfigFile(
		context.Background(), "feishu", admin.AgentConfigUser, "feishu user"))

	wc, err := a.GetPlatformAgentConfigFile(context.Background(), "webchat", admin.AgentConfigUser)
	require.NoError(t, err)
	require.Equal(t, "webchat user", wc.Content)

	fc, err := a.GetPlatformAgentConfigFile(context.Background(), "feishu", admin.AgentConfigUser)
	require.NoError(t, err)
	require.Equal(t, "feishu user", fc.Content)
}
