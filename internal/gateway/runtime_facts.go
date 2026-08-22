package gateway

import (
	"sort"

	"github.com/hrygo/hotplex/internal/agentconfig"
	"github.com/hrygo/hotplex/internal/worker"
)

var gatewayRuntimeEnvKeys = [...]string{
	"GATEWAY_PLATFORM",
	"GATEWAY_BOT_ID",
	"GATEWAY_BOT_NAME",
	"GATEWAY_USER_ID",
	"GATEWAY_SESSION_ID",
	"GATEWAY_WORK_DIR",
	"GATEWAY_CHANNEL_ID",
	"GATEWAY_THREAD_ID",
	"GATEWAY_TEAM_ID",
}

// buildRuntimeFacts translates the selected Worker and already-resolved
// SessionInfo into the worker-independent prompt contract. The caller derives
// scope from session metadata; this function never copies any identity value.
func buildRuntimeFacts(w worker.Worker, info worker.SessionInfo, platform string, scope agentconfig.RuntimeScopeKind) agentconfig.RuntimeFacts {
	facts := agentconfig.RuntimeFacts{
		SchemaVersion:             agentconfig.RuntimeFactsSchemaVersion,
		Platform:                  platform,
		WorkerType:                runtimeWorkerType(w),
		ScopeKind:                 runtimeScopeKind(scope),
		DeclaredPermissionMode:    info.PermissionMode,
		DeclaredQuerySurfaces:     []agentconfig.RuntimeQuerySurface{agentconfig.QuerySkills},
		DeclaredSkillCatalogOwner: agentconfig.SkillCatalogOwnerNone,
		PresentGatewayEnvKeys:     presentGatewayEnvKeys(info.Env),
	}

	if w == nil {
		facts.DeclaredCapabilities = nil
		return facts
	}
	if w.SupportsResume() {
		facts.DeclaredCapabilities = append(facts.DeclaredCapabilities, agentconfig.CapabilityResume)
	}
	if w.SupportsStreaming() {
		facts.DeclaredCapabilities = append(facts.DeclaredCapabilities, agentconfig.CapabilityStreaming)
	}
	if w.SupportsTools() {
		facts.DeclaredCapabilities = append(facts.DeclaredCapabilities, agentconfig.CapabilityTools)
	}
	if facts.WorkerType != agentconfig.RuntimeWorkerUnknown {
		facts.DeclaredSkillCatalogOwner = agentconfig.SkillCatalogOwnerWorker
	}
	if _, ok := w.(worker.ControlRequester); ok {
		facts.DeclaredQuerySurfaces = append(facts.DeclaredQuerySurfaces, agentconfig.QueryMCP)
	}
	if _, ok := w.(worker.WorkerCommander); ok {
		facts.DeclaredQuerySurfaces = append(facts.DeclaredQuerySurfaces, agentconfig.QueryWorker)
	}
	sort.Slice(facts.DeclaredCapabilities, func(i, j int) bool {
		return facts.DeclaredCapabilities[i] < facts.DeclaredCapabilities[j]
	})
	sort.Slice(facts.DeclaredQuerySurfaces, func(i, j int) bool {
		return facts.DeclaredQuerySurfaces[i] < facts.DeclaredQuerySurfaces[j]
	})
	return facts
}

func runtimeWorkerType(w worker.Worker) agentconfig.RuntimeWorkerType {
	if w == nil {
		return agentconfig.RuntimeWorkerUnknown
	}
	switch w.Type() {
	case worker.TypeClaudeCode:
		return agentconfig.RuntimeWorkerClaudeCode
	case worker.TypeOpenCodeSrv:
		return agentconfig.RuntimeWorkerOpenCodeSrv
	case worker.TypeCodexCLI:
		return agentconfig.RuntimeWorkerCodexCLI
	case worker.TypeACP:
		return agentconfig.RuntimeWorkerACP
	default:
		return agentconfig.RuntimeWorkerUnknown
	}
}

func runtimeScopeKind(scope agentconfig.RuntimeScopeKind) agentconfig.RuntimeScopeKind {
	switch scope {
	case agentconfig.RuntimeScopeBot, agentconfig.RuntimeScopeWorkspace:
		return scope
	default:
		return agentconfig.RuntimeScopeUnbound
	}
}

func runtimeScopeForSession(workspaceID, botID, botName string) agentconfig.RuntimeScopeKind {
	if workspaceID != "" {
		return agentconfig.RuntimeScopeWorkspace
	}
	if botID != "" || botName != "" {
		return agentconfig.RuntimeScopeBot
	}
	return agentconfig.RuntimeScopeUnbound
}

func presentGatewayEnvKeys(env map[string]string) []string {
	keys := make([]string, 0, len(gatewayRuntimeEnvKeys))
	for _, key := range gatewayRuntimeEnvKeys {
		if env[key] != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}
