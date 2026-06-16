package gateway

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/agentconfig"
	"github.com/hrygo/hotplex/internal/session"
)

// TestBridge_TwoWorkspaces_DifferentOverrides verifies the core spec ② invariant:
// two workspaces with different overrides produce different system prompts, and
// neither pollutes the other nor the team default.
func TestBridge_TwoWorkspaces_DifferentOverrides(t *testing.T) {
	t.Parallel()

	// Team defaults on disk (global level).
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("team-rules"), 0o644))

	wsA := &session.Workspace{ID: "ws-a", AgentConfigOverrides: `{"SOUL.md":"persona-A"}`}
	wsB := &session.Workspace{ID: "ws-b", AgentConfigOverrides: `{"SOUL.md":"persona-B"}`}
	storeA := &stubWSStore{ws: wsA}
	storeB := &stubWSStore{ws: wsB}

	promptA := buildPromptFor(t, dir, storeA, "ws-a")
	promptB := buildPromptFor(t, dir, storeB, "ws-b")

	require.Contains(t, promptA, "persona-A")
	require.NotContains(t, promptA, "persona-B")
	require.Contains(t, promptB, "persona-B")
	require.NotContains(t, promptB, "persona-A")
	// Both inherit team default AGENTS.md
	require.Contains(t, promptA, "team-rules")
	require.Contains(t, promptB, "team-rules")
}

// TestBridge_WorkspaceWithoutOverrides_InheritsTeamDefault verifies a workspace
// with empty overrides falls fully back to team defaults.
func TestBridge_WorkspaceWithoutOverrides_InheritsTeamDefault(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SOUL.md"), []byte("team-soul"), 0o644))

	ws := &session.Workspace{ID: "ws-x", AgentConfigOverrides: ""}
	store := &stubWSStore{ws: ws}

	prompt := buildPromptFor(t, dir, store, "ws-x")
	require.Contains(t, prompt, "team-soul")
}

// buildPromptFor exercises the same data path as injectAgentConfig:
// resolveWorkspaceOverrides → LoadForWorkspace → BuildSystemPrompt.
//
// resolveWorkspaceOverrides returns nil for empty/invalid overrides (graceful
// degradation), and LoadForWorkspace accepts a nil overrides map (falls back to
// team defaults), so no NotNil assertion is applied here.
func buildPromptFor(t *testing.T, dir string, store *stubWSStore, workspaceID string) string {
	t.Helper()
	b := &Bridge{log: testLogger(t), wsStore: store, agentConfigDir: dir}
	overrides := b.resolveWorkspaceOverrides(workspaceID)
	cfg, err := agentconfig.LoadForWorkspace(dir, "webchat", overrides)
	require.NoError(t, err)
	return agentconfig.BuildSystemPrompt(cfg)
}
