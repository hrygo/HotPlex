package agentspec

import (
	"encoding/json"
	"errors"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/worker"
)

var planHashRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

func TestResolvePlan_Success(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Messaging.Feishu.WorkerType = "codex_cli"
	cfg.Worker.CodexCLI.Sandbox = "workspace-write"

	plan, err := testResolver().ResolvePlan(Input{
		Cfg:      cfg,
		Platform: "feishu",
		BotName:  "b1",
		UserID:   "u1",
	})

	require.NoError(t, err)
	require.Equal(t, PlanVersion, plan.Version)
	require.Equal(t, PlanResolverID, plan.Resolver)
	require.Regexp(t, planHashRe, plan.PlanHash)
	require.Equal(t, "codex_cli", plan.AgentSpec.Worker.Type)
	require.Empty(t, plan.Blocked)
	require.Empty(t, plan.Warnings)
	require.Contains(t, plan.SourceRefs, PlanSourceRef{Field: "worker_type", Source: PlanSourcePlatformConfig})
	require.Contains(t, plan.SourceRefs, PlanSourceRef{Field: "sandbox_mode", Source: PlanSourceBaseConfig})
}

func TestResolvePlan_UsesConfiguredWorkspacePermission(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Messaging.Feishu.WorkerType = "claude_code"
	cfg.Worker.DefaultPermissionMode = worker.PermissionModeReadOnly

	plan, err := testResolver().ResolvePlan(Input{
		Cfg:         cfg,
		Platform:    "feishu",
		WorkspaceID: "workspace-1",
	})

	require.NoError(t, err)
	require.Equal(t, worker.PermissionModeReadOnly, plan.AgentSpec.Policy.PermissionMode)
	require.Contains(t, plan.SourceRefs, PlanSourceRef{
		Field: "permission_mode", Source: PlanSourceBaseConfig,
	})
}

func TestResolvePlan_UsesConfiguredOperatorPermissionWithoutWorkspace(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Messaging.Feishu.WorkerType = "claude_code"
	cfg.Worker.ClaudeCode.PermissionMode = worker.PermissionModeReadOnly

	plan, err := testResolver().ResolvePlan(Input{Cfg: cfg, Platform: "feishu"})

	require.NoError(t, err)
	require.Equal(t, worker.PermissionModeReadOnly, plan.AgentSpec.Policy.PermissionMode)
	require.Contains(t, plan.SourceRefs, PlanSourceRef{
		Field: "permission_mode", Source: PlanSourceBaseConfig,
	})
}

// TestResolvePlan_Determinism: same Input → same plan hash; a different
// effective field → different hash. The hash is a pure function of the
// redacted desired state.
func TestResolvePlan_Determinism(t *testing.T) {
	t.Parallel()
	in := Input{
		InitMeta: InitMetadata{WorkerType: "claude_code"},
		Platform: "webchat",
		UserID:   "u1",
	}
	a, err := testResolver().ResolvePlan(in)
	require.NoError(t, err)
	b, err := testResolver().ResolvePlan(in)
	require.NoError(t, err)
	require.Equal(t, a.PlanHash, b.PlanHash)

	other, err := testResolver().ResolvePlan(Input{
		InitMeta: InitMetadata{WorkerType: "opencode_server"},
		Platform: "webchat",
		UserID:   "u1",
	})
	require.NoError(t, err)
	require.NotEqual(t, a.PlanHash, other.PlanHash)
}

// TestResolvePlan_WSRESTEquivalence: two structurally equal Inputs (the WS
// init and REST create construction shapes) must produce the same plan hash —
// the shadow-mode divergence alarm of #946 spec §6.4. Any future hash
// divergence between equivalent entries fails here.
func TestResolvePlan_WSRESTEquivalence(t *testing.T) {
	t.Parallel()
	base := Input{
		InitMeta:    InitMetadata{WorkerType: "claude_code"},
		Platform:    "webchat",
		UserID:      "u1",
		WorkspaceID: "ws1",
	}
	wsLike := base
	restLike := base

	wsPlan, err := testResolver().ResolvePlan(wsLike)
	require.NoError(t, err)
	restPlan, err := testResolver().ResolvePlan(restLike)
	require.NoError(t, err)
	require.Equal(t, wsPlan.PlanHash, restPlan.PlanHash,
		"equivalent WS/REST inputs must resolve to the same plan identity")

	// And the documented F4 divergence (AllowedTools present on WS, absent on
	// REST) DOES change the plan identity — it is visible, never silent.
	wsTools := base
	wsTools.InitMeta.AllowedTools = []string{"Bash"}
	toolsPlan, err := testResolver().ResolvePlan(wsTools)
	require.NoError(t, err)
	require.NotEqual(t, restPlan.PlanHash, toolsPlan.PlanHash)
}

func TestResolvePlan_Blocked(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		in       Input
		wantCode string
	}{
		{
			name:     "unknown worker type",
			in:       Input{InitMeta: InitMetadata{WorkerType: "bogus_worker"}, Platform: "webchat"},
			wantCode: BlockUnknownWorkerType,
		},
		{
			name:     "invalid permission mode",
			in:       Input{InitMeta: InitMetadata{WorkerType: "claude_code", PermissionMode: "god-mode"}, Platform: "webchat"},
			wantCode: BlockInvalidPermissionMode,
		},
		{
			name: "unverifiable sandbox mode",
			in: Input{
				InitMeta:    InitMetadata{WorkerType: "codex_cli"},
				Platform:    "webchat",
				PlatformKey: map[string]string{worker.SandboxPlatformKey: "yolo-unlocked"},
			},
			wantCode: BlockInvalidSandboxMode,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			plan, err := testResolver().ResolvePlan(tc.in)
			require.Error(t, err)
			require.ErrorIs(t, err, ErrPlanBlocked, "blocked plans must wrap ErrPlanBlocked")
			require.Len(t, plan.Blocked, 1)
			require.Equal(t, tc.wantCode, plan.Blocked[0].Code)
			require.NotEmpty(t, plan.Blocked[0].Message)
			require.Empty(t, plan.PlanHash, "a blocked plan has no executable identity")
		})
	}
}

func TestResolvePlan_Warnings(t *testing.T) {
	t.Parallel()

	t.Run("unresolved worker type warns", func(t *testing.T) {
		t.Parallel()
		plan, err := testResolver().ResolvePlan(Input{Platform: "webchat"})
		require.NoError(t, err, "empty worker type stays compat-compatible")
		require.Empty(t, plan.Blocked)
		require.Len(t, plan.Warnings, 1)
		require.Equal(t, WarnWorkerTypeUnresolved, plan.Warnings[0].Code)
	})

	t.Run("init vs workspace permission divergence warns", func(t *testing.T) {
		t.Parallel()
		plan, err := testResolver().ResolvePlan(Input{
			InitMeta:      InitMetadata{WorkerType: "claude_code", PermissionMode: "workspace"},
			WorkspacePerm: "bypass",
			Platform:      "webchat",
		})
		require.NoError(t, err)
		require.Empty(t, plan.Blocked)
		var codes []string
		for _, w := range plan.Warnings {
			codes = append(codes, w.Code)
		}
		require.Contains(t, codes, WarnPermissionSourceConflict)
		require.Equal(t, "workspace", plan.AgentSpec.Policy.PermissionMode,
			"documented precedence still applies")
	})
}

// TestRedacted_ExcludesInternalContract: launch command, model, tool lists,
// budget, identity refs and sandbox directories never reach the public view.
func TestRedacted_ExcludesInternalContract(t *testing.T) {
	t.Parallel()
	plan := EffectiveRuntimePlan{
		Version:  PlanVersion,
		Resolver: PlanResolverID,
		AgentSpec: AgentSpec{
			Worker: WorkerSpec{
				Type:    "claude_code",
				Command: "launch-cmd-SENTINEL --api-key=SECRETKEYVAL",
				Model:   "model-SENTINEL",
			},
			Policy: PolicySpec{
				PermissionMode:  "workspace",
				AllowedTools:    []string{"tool-SENTINEL"},
				DisallowedTools: []string{"blocked-SENTINEL"},
			},
			Sandbox: SandboxSpec{
				Mode:        "read-only",
				AllowedDirs: []string{"/path-SENTINEL/project"},
			},
			Budget:   BudgetSpec{MaxTurns: 7},
			Identity: IdentityRefs{UserID: "user-SENTINEL", WorkspaceID: "ws-SENTINEL"},
		},
	}

	view := plan.Redacted()
	require.Equal(t, "claude_code", view.WorkerType)
	require.Equal(t, "workspace", view.PermissionMode)
	require.Equal(t, "read-only", view.SandboxMode)

	raw, err := json.Marshal(view)
	require.NoError(t, err)
	s := string(raw)
	for _, sentinel := range []string{
		"launch-cmd-SENTINEL", "SECRETKEYVAL", "model-SENTINEL",
		"tool-SENTINEL", "blocked-SENTINEL", "/path-SENTINEL",
		"user-SENTINEL", "ws-SENTINEL",
	} {
		require.NotContains(t, s, sentinel, "public view leaked an internal contract value")
	}
}

// TestRedacted_SecretShapedEnvKeys: bare key names pass; anything carrying a
// value is dropped and reported as a bounded blocked reason, never echoed.
func TestRedacted_SecretShapedEnvKeys(t *testing.T) {
	t.Parallel()
	plan := EffectiveRuntimePlan{
		Version:  PlanVersion,
		Resolver: PlanResolverID,
		EnvKeys:  []string{"GOOD_KEY", "BAD=hunter2", "ALSO BAD", "TOKEN \"quoted\""},
	}

	view := plan.Redacted()
	require.Equal(t, []string{"GOOD_KEY"}, view.EnvKeys)
	require.Len(t, view.Blocked, 3)
	for _, b := range view.Blocked {
		require.Equal(t, BlockSecretShapedValue, b.Code)
		require.NotContains(t, b.Message, "hunter2", "the offending value must never be echoed")
	}

	raw, err := json.Marshal(view)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "hunter2")
}

// TestCanonicalPlanHash_NilEmptyNormalization: nil vs empty slices (rule 2)
// hash identically.
func TestCanonicalPlanHash_NilEmptyNormalization(t *testing.T) {
	t.Parallel()
	withNil := EffectiveRuntimePlanView{Version: PlanVersion, Resolver: PlanResolverID, WorkerType: "claude_code"}
	withEmpty := withNil
	withEmpty.EnvKeys = []string{}
	withEmpty.CapabilityIDs = []string{}
	withEmpty.SourceRefs = []PlanSourceRef{}
	withEmpty.Warnings = []PlanWarning{}
	withEmpty.Blocked = []PlanBlockReason{}

	require.Equal(t, CanonicalPlanHash(withNil), CanonicalPlanHash(withEmpty))
}

// TestCanonicalPlanHash_UnorderedListsSorted: semantically unordered ID/key
// lists (rule 3) hash identically in any input order.
func TestCanonicalPlanHash_UnorderedListsSorted(t *testing.T) {
	t.Parallel()
	a := EffectiveRuntimePlanView{
		Version:       PlanVersion,
		Resolver:      PlanResolverID,
		EnvKeys:       []string{"ALPHA", "BETA"},
		CapabilityIDs: []string{"cap-2", "cap-1"},
	}
	b := a
	b.EnvKeys = []string{"BETA", "ALPHA"}
	b.CapabilityIDs = []string{"cap-1", "cap-2"}

	require.Equal(t, CanonicalPlanHash(a), CanonicalPlanHash(b))
}

// TestCanonicalPlanHash_SourceOrderPreserved: SourceRefs order is semantic
// precedence (rule 4) — reordering changes the identity.
func TestCanonicalPlanHash_SourceOrderPreserved(t *testing.T) {
	t.Parallel()
	a := EffectiveRuntimePlanView{
		Version:  PlanVersion,
		Resolver: PlanResolverID,
		SourceRefs: []PlanSourceRef{
			{Field: "worker_type", Source: PlanSourceInitMetadata},
			{Field: "permission_mode", Source: PlanSourceWorkspaceOverride},
		},
	}
	b := a
	b.SourceRefs = []PlanSourceRef{
		{Field: "permission_mode", Source: PlanSourceWorkspaceOverride},
		{Field: "worker_type", Source: PlanSourceInitMetadata},
	}

	require.NotEqual(t, CanonicalPlanHash(a), CanonicalPlanHash(b),
		"source-ref order carries precedence semantics and must affect the hash")
}

// TestCanonicalPlanHash_ExcludesHashField: the hash is over the body, not
// itself (rule 6) — a pre-stamped PlanHash must not feed back in.
func TestCanonicalPlanHash_ExcludesHashField(t *testing.T) {
	t.Parallel()
	a := EffectiveRuntimePlanView{Version: PlanVersion, Resolver: PlanResolverID, WorkerType: "acp"}
	b := a
	b.PlanHash = "deadbeef"

	require.Equal(t, CanonicalPlanHash(a), CanonicalPlanHash(b))
	require.Regexp(t, planHashRe, CanonicalPlanHash(a))
}

// TestResolvePlan_BlockedIsTyped ensures callers can branch on the sentinel.
func TestResolvePlan_BlockedIsTyped(t *testing.T) {
	t.Parallel()
	_, err := testResolver().ResolvePlan(Input{
		InitMeta: InitMetadata{WorkerType: "bogus"},
		Platform: "webchat",
	})
	require.True(t, errors.Is(err, ErrPlanBlocked))
}
