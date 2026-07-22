package agentspec

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/worker"
)

func fullBaseStartParams() worker.SessionStartParams {
	return worker.SessionStartParams{
		ID:            "sess-1",
		UserID:        "u1",
		BotID:         "bot-1",
		BotName:       "my-bot",
		WorkerType:    worker.TypeClaudeCode,
		AllowedTools:  []string{"Read"},
		WorkDir:       "/work",
		Platform:      "webchat",
		PlatformKey:   map[string]string{"k": "v"},
		Title:         "title",
		ClientKey:     "ck",
		WorkspaceID:   "ws-1",
		InjectExclude: []string{"MEMORY.md"},
	}
}

func TestMapToStartParams_Overlay(t *testing.T) {
	t.Parallel()
	base := fullBaseStartParams()
	spec := AgentSpec{
		Worker: WorkerSpec{Type: "codex_cli"},
		Policy: PolicySpec{AllowedTools: []string{"Bash", "Grep"}},
	}
	out := MapToStartParams(spec, base)

	// Owned fields overlaid.
	require.Equal(t, worker.WorkerType("codex_cli"), out.WorkerType)
	require.Equal(t, []string{"Bash", "Grep"}, out.AllowedTools)

	// Passthrough fields untouched.
	require.Equal(t, base.ID, out.ID)
	require.Equal(t, base.UserID, out.UserID)
	require.Equal(t, base.BotID, out.BotID)
	require.Equal(t, base.BotName, out.BotName)
	require.Equal(t, base.WorkDir, out.WorkDir)
	require.Equal(t, base.Platform, out.Platform)
	require.Equal(t, base.PlatformKey, out.PlatformKey)
	require.Equal(t, base.Title, out.Title)
	require.Equal(t, base.ClientKey, out.ClientKey)
	require.Equal(t, base.WorkspaceID, out.WorkspaceID)
	require.Equal(t, base.InjectExclude, out.InjectExclude)
}

func TestMapToStartParams_NilAllowedToolsPreservesBase(t *testing.T) {
	t.Parallel()
	base := fullBaseStartParams()
	spec := AgentSpec{Worker: WorkerSpec{Type: "acp"}} // AllowedTools nil
	out := MapToStartParams(spec, base)
	require.Equal(t, base.AllowedTools, out.AllowedTools, "nil AllowedTools must preserve base")
	require.Equal(t, worker.WorkerType("acp"), out.WorkerType)
}

func TestMapToStartParams_Idempotent(t *testing.T) {
	t.Parallel()
	base := fullBaseStartParams()
	spec := AgentSpec{Worker: WorkerSpec{Type: "codex_cli"}, Policy: PolicySpec{AllowedTools: []string{"Bash"}}}
	once := MapToStartParams(spec, base)
	twice := MapToStartParams(spec, once)
	require.Equal(t, once, twice)
}

func fullBaseSessionInfo() worker.SessionInfo {
	return worker.SessionInfo{
		SessionID:       "sess-1",
		UserID:          "u1",
		ProjectDir:      "/work",
		Env:             map[string]string{"E": "v"},
		Args:            []string{"--x"},
		AllowedTools:    []string{"Read"},
		WorkerSessionID: "wsi-1",
		AllowedModels:   []string{"m-old"},
		MCPConfig:       `{"mcpServers":{}}`,
		StrictMCPConfig: true,
		DisallowedTools: []string{"WebSearch"},
		SystemPrompt:    "sys-prompt",
		ConfigEnv:       []string{"CE=v"},
		ConfigBlocklist: []string{"BLOCKED"},
		PermissionMode:  "workspace",
		Sandbox:         "read-only",
		ACPCommand:      "acp-bin",
		// resume family (preserved path).
		ContinueSession: true,
		ForkSession:     true,
		ResumeSessionAt: "msg-9",
		ResumeSessionID: "rs-9",
		MaxTurns:        7,
		MaxBudgetUSD:    1.5,
		AllowedDirs:     []string{"/extra"},
	}
}

func TestMapToSessionInfo_OwnedFieldsOverlay(t *testing.T) {
	t.Parallel()
	base := fullBaseSessionInfo()
	spec := AgentSpec{
		Worker:  WorkerSpec{AllowedModels: []string{"m-new"}},
		Policy:  PolicySpec{PermissionMode: "bypass", AllowedTools: []string{"Bash"}, DisallowedTools: []string{"Edit"}},
		Sandbox: SandboxSpec{Mode: "danger-full-access", AllowedDirs: []string{"/d"}},
		Budget:  BudgetSpec{MaxTurns: 20, MaxBudgetUSD: 9.9},
	}
	out := MapToSessionInfo(spec, base)

	require.Equal(t, "bypass", out.PermissionMode)
	require.Equal(t, []string{"Bash"}, out.AllowedTools)
	require.Equal(t, []string{"Edit"}, out.DisallowedTools)
	require.Equal(t, []string{"m-new"}, out.AllowedModels)
	require.Equal(t, "danger-full-access", out.Sandbox)
	require.Equal(t, []string{"/d"}, out.AllowedDirs)
	require.Equal(t, 20, out.MaxTurns)
	require.Equal(t, 9.9, out.MaxBudgetUSD)
}

// TestMapToSessionInfo_ZeroSpecNoOverride: a zero AgentSpec must leave base
// entirely unchanged (the "zero = do not override" rule).
func TestMapToSessionInfo_ZeroSpecNoOverride(t *testing.T) {
	t.Parallel()
	base := fullBaseSessionInfo()
	out := MapToSessionInfo(AgentSpec{}, base)
	require.Equal(t, base, out)
}

// TestMapToSessionInfo_PreservedFieldsUntouched: reserved-path fields (resume
// family, MCPConfig, SystemPrompt, ConfigEnv, ConfigBlocklist, ACPCommand,
// WorkerSessionID, identity) are never modified by the mapper.
func TestMapToSessionInfo_PreservedFieldsUntouched(t *testing.T) {
	t.Parallel()
	base := fullBaseSessionInfo()
	spec := AgentSpec{
		Worker:  WorkerSpec{AllowedModels: []string{"m-new"}},
		Policy:  PolicySpec{PermissionMode: "bypass", SkipPermissions: true},
		Sandbox: SandboxSpec{Mode: "workspace-write"},
		Budget:  BudgetSpec{MaxTurns: 3},
	}
	out := MapToSessionInfo(spec, base)

	require.Equal(t, base.ContinueSession, out.ContinueSession)
	require.Equal(t, base.ForkSession, out.ForkSession)
	require.Equal(t, base.ResumeSessionAt, out.ResumeSessionAt)
	require.Equal(t, base.ResumeSessionID, out.ResumeSessionID)
	require.Equal(t, base.MCPConfig, out.MCPConfig)
	require.Equal(t, base.StrictMCPConfig, out.StrictMCPConfig)
	require.Equal(t, base.SystemPrompt, out.SystemPrompt)
	require.Equal(t, base.ConfigEnv, out.ConfigEnv)
	require.Equal(t, base.ConfigBlocklist, out.ConfigBlocklist)
	require.Equal(t, base.ACPCommand, out.ACPCommand)
	require.Equal(t, base.WorkerSessionID, out.WorkerSessionID)
	require.Equal(t, base.SessionID, out.SessionID)
	require.Equal(t, base.UserID, out.UserID)
	require.Equal(t, base.ProjectDir, out.ProjectDir)
	require.Equal(t, base.Env, out.Env)
	require.Equal(t, base.Args, out.Args)
}

func TestMapToSessionInfo_AllowedModelsNilPreservesBase(t *testing.T) {
	t.Parallel()
	base := fullBaseSessionInfo()
	spec := AgentSpec{Policy: PolicySpec{PermissionMode: "auto-edit"}} // AllowedModels nil (F1)
	out := MapToSessionInfo(spec, base)
	require.Equal(t, base.AllowedModels, out.AllowedModels, "nil AllowedModels must preserve base (F1)")
	require.Equal(t, "auto-edit", out.PermissionMode)
}

func TestMapToSessionInfo_Idempotent(t *testing.T) {
	t.Parallel()
	base := fullBaseSessionInfo()
	spec := AgentSpec{
		Worker:  WorkerSpec{AllowedModels: []string{"m-new"}},
		Policy:  PolicySpec{PermissionMode: "bypass", AllowedTools: []string{"Bash"}},
		Sandbox: SandboxSpec{Mode: "danger-full-access"},
		Budget:  BudgetSpec{MaxTurns: 20},
	}
	once := MapToSessionInfo(spec, base)
	twice := MapToSessionInfo(spec, once)
	require.Equal(t, once, twice)
}
