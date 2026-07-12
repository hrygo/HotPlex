package gateway

import (
	"context"
	"log/slog"
	"time"

	"github.com/hrygo/hotplex/internal/eventstore"
	"github.com/hrygo/hotplex/internal/execution"
	"github.com/hrygo/hotplex/internal/security"
	"github.com/hrygo/hotplex/internal/session"
)

// HandlerDeps groups all dependencies for Handler construction.
type HandlerDeps struct {
	Log            *slog.Logger
	Hub            *Hub
	SM             SessionManager
	Auth           *security.Authenticator
	Bridge         *Bridge
	SkillsLocator  SkillsLocator
	ExecutionStore execution.Store
}

// WorkspaceOverridesReader is the narrow workspace-store subset Bridge needs to
// resolve per-workspace agent-config overrides (spec ② §7.3), plus the startup
// scan to detect stale/invalid overrides (#749). Kept separate from
// session.UserWorkspaceStore so tests mock a minimal surface.
type WorkspaceOverridesReader interface {
	GetWorkspaceByID(ctx context.Context, id string) (*session.Workspace, error)
	ListAllWorkspaces(ctx context.Context) ([]*session.Workspace, error)
}

// BridgeDeps groups all dependencies for Bridge construction.
type BridgeDeps struct {
	Log                    *slog.Logger
	Hub                    *Hub
	SM                     bridgeSM
	EventCollector         *eventstore.Collector  // optional; nil means event storage disabled
	TurnsQuerier           eventstore.TurnQuerier // optional; for LatestGeneration on startup
	RetryCtrl              *LLMRetryController
	AgentConfigDir         string
	TurnTimeout            time.Duration
	WorkerEnv              []string                 // extra env vars from worker.environment config
	WorkerEnvBlocklist     []string                 // extra blocklist entries from worker.env_blocklist config
	CronEnv                []string                 // env vars injected only into cron platform sessions (e.g. admin API creds)
	MCPConfigJSON          string                   // pre-serialized MCP config JSON; "" = not configured → Claude Code default discovery
	DefaultPermissionMode  string                   // worker.PermissionMode* tier; consumed by resolveWorkspacePermissionMode for workspaces with no explicit override (r3 #804). Seeded "workspace" by Default(); hot-reloadable via UpdateDefaultPermissionMode.
	AgentConfigExclude     map[string][]string      // platform → inject_exclude (global default at "" key)
	WSStore                WorkspaceOverridesReader // WebChat per-workspace agent-config overrides (spec ②); nil = disabled
	PermissionDedupEnabled bool                     // suppress repeated permission cards after a user denial (Permission-Deny-Dedup-Spec)
	PermissionDedupWindow  time.Duration            // denial cache TTL; only honored when PermissionDedupEnabled
}
