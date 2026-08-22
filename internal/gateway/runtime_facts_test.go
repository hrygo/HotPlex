package gateway

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/agentconfig"
	"github.com/hrygo/hotplex/internal/worker"
)

type runtimeFactsCapabilityWorker struct {
	*mockBridgeWorker
}

func (w *runtimeFactsCapabilityWorker) ListNativeCommands(context.Context, string) ([]worker.NativeCommandDescriptor, error) {
	return nil, nil
}

func (w *runtimeFactsCapabilityWorker) InvokeNativeCommand(context.Context, worker.NativeCommandInvocation) error {
	return nil
}

func (w *runtimeFactsCapabilityWorker) UpdateSystemPrompt(string) {}

func (w *runtimeFactsCapabilityWorker) SendControlRequest(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, nil
}

func (w *runtimeFactsCapabilityWorker) Compact(context.Context, map[string]any) error { return nil }
func (w *runtimeFactsCapabilityWorker) Clear(context.Context) error                   { return nil }
func (w *runtimeFactsCapabilityWorker) Rewind(context.Context, string) error          { return nil }

func TestBuildRuntimeFacts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		worker     worker.Worker
		platform   string
		scope      agentconfig.RuntimeScopeKind
		wantWorker agentconfig.RuntimeWorkerType
		wantCaps   []agentconfig.RuntimeCapability
		wantQuery  []agentconfig.RuntimeQuerySurface
		wantOwner  agentconfig.SkillCatalogOwner
	}{
		{
			name:       "claude bot without native catalog",
			worker:     &mockBridgeWorker{workerType: worker.TypeClaudeCode},
			platform:   "slack",
			scope:      agentconfig.RuntimeScopeBot,
			wantWorker: agentconfig.RuntimeWorkerClaudeCode,
			wantCaps: []agentconfig.RuntimeCapability{
				agentconfig.CapabilityResume,
				agentconfig.CapabilityStreaming,
				agentconfig.CapabilityTools,
			},
			wantQuery: []agentconfig.RuntimeQuerySurface{agentconfig.QuerySkills},
			wantOwner: agentconfig.SkillCatalogOwnerNone,
		},
		{
			name: "full worker workspace",
			worker: &runtimeFactsCapabilityWorker{
				mockBridgeWorker: &mockBridgeWorker{workerType: worker.TypeCodexCLI},
			},
			platform:   "webchat",
			scope:      agentconfig.RuntimeScopeWorkspace,
			wantWorker: agentconfig.RuntimeWorkerCodexCLI,
			wantCaps: []agentconfig.RuntimeCapability{
				agentconfig.CapabilityResume,
				agentconfig.CapabilityStreaming,
				agentconfig.CapabilityTools,
			},
			wantQuery: []agentconfig.RuntimeQuerySurface{
				agentconfig.QueryMCP,
				agentconfig.QuerySkills,
				agentconfig.QueryWorker,
			},
			wantOwner: agentconfig.SkillCatalogOwnerWorker,
		},
		{
			name:       "opencode platform",
			worker:     &mockBridgeWorker{workerType: worker.TypeOpenCodeSrv},
			platform:   "feishu",
			scope:      agentconfig.RuntimeScopeBot,
			wantWorker: agentconfig.RuntimeWorkerOpenCodeSrv,
			wantCaps: []agentconfig.RuntimeCapability{
				agentconfig.CapabilityResume,
				agentconfig.CapabilityStreaming,
				agentconfig.CapabilityTools,
			},
			wantQuery: []agentconfig.RuntimeQuerySurface{agentconfig.QuerySkills},
			wantOwner: agentconfig.SkillCatalogOwnerNone,
		},
		{
			name:       "acp platform",
			worker:     &mockBridgeWorker{workerType: worker.TypeACP},
			platform:   "slack",
			scope:      agentconfig.RuntimeScopeBot,
			wantWorker: agentconfig.RuntimeWorkerACP,
			wantCaps: []agentconfig.RuntimeCapability{
				agentconfig.CapabilityResume,
				agentconfig.CapabilityStreaming,
				agentconfig.CapabilityTools,
			},
			wantQuery: []agentconfig.RuntimeQuerySurface{agentconfig.QuerySkills},
			wantOwner: agentconfig.SkillCatalogOwnerNone,
		},
		{
			name:       "unknown unbound worker",
			worker:     &mockBridgeWorker{workerType: worker.TypeUnknown},
			platform:   "cron",
			scope:      agentconfig.RuntimeScopeUnbound,
			wantWorker: agentconfig.RuntimeWorkerUnknown,
			wantCaps: []agentconfig.RuntimeCapability{
				agentconfig.CapabilityResume,
				agentconfig.CapabilityStreaming,
				agentconfig.CapabilityTools,
			},
			wantQuery: []agentconfig.RuntimeQuerySurface{agentconfig.QuerySkills},
			wantOwner: agentconfig.SkillCatalogOwnerNone,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			facts := buildRuntimeFacts(tt.worker, worker.SessionInfo{
				PermissionMode: "workspace",
				Env: map[string]string{
					"GATEWAY_PLATFORM":   tt.platform,
					"GATEWAY_SESSION_ID": "private-session-value",
					"OPENAI_API_KEY":     "must-not-appear",
				},
			}, tt.platform, tt.scope)

			require.Equal(t, agentconfig.RuntimeFactsSchemaVersion, facts.SchemaVersion)
			require.Equal(t, tt.platform, facts.Platform)
			require.Equal(t, tt.wantWorker, facts.WorkerType)
			require.Equal(t, tt.scope, facts.ScopeKind)
			require.Equal(t, "workspace", facts.DeclaredPermissionMode)
			require.Equal(t, tt.wantCaps, facts.DeclaredCapabilities)
			require.Equal(t, tt.wantQuery, facts.DeclaredQuerySurfaces)
			require.Equal(t, tt.wantOwner, facts.DeclaredSkillCatalogOwner)
			require.Equal(t, []string{"GATEWAY_PLATFORM", "GATEWAY_SESSION_ID"}, facts.PresentGatewayEnvKeys)
			require.NotContains(t, facts.PresentGatewayEnvKeys, "OPENAI_API_KEY")

			payload, err := facts.CanonicalJSON()
			require.NoError(t, err)
			require.NotContains(t, string(payload), "private-session-value")
			require.NotContains(t, string(payload), "must-not-appear")
		})
	}
}

func TestBuildRuntimeFactsUsesDeclaredOptionalInterfaces(t *testing.T) {
	t.Parallel()

	base := &runtimeFactsCapabilityWorker{
		mockBridgeWorker: &mockBridgeWorker{workerType: worker.TypeOpenCodeSrv},
	}
	facts := buildRuntimeFacts(base, worker.SessionInfo{}, "feishu", agentconfig.RuntimeScopeBot)
	require.Equal(t, []agentconfig.RuntimeCapability{
		agentconfig.CapabilityResume,
		agentconfig.CapabilityStreaming,
		agentconfig.CapabilityTools,
	}, facts.DeclaredCapabilities)
	require.Equal(t, []agentconfig.RuntimeQuerySurface{
		agentconfig.QueryMCP,
		agentconfig.QuerySkills,
		agentconfig.QueryWorker,
	}, facts.DeclaredQuerySurfaces)
}

func TestRuntimeScopeForSessionPrecedence(t *testing.T) {
	t.Parallel()

	require.Equal(t, agentconfig.RuntimeScopeWorkspace, runtimeScopeForSession("workspace-1", "bot-1", "primary"))
	require.Equal(t, agentconfig.RuntimeScopeBot, runtimeScopeForSession("", "bot-1", ""))
	require.Equal(t, agentconfig.RuntimeScopeBot, runtimeScopeForSession("", "", "primary"))
	require.Equal(t, agentconfig.RuntimeScopeUnbound, runtimeScopeForSession("", "", ""))
}
