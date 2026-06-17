package gateway

import (
	"context"
	"log/slog"
	"time"

	"github.com/hrygo/hotplex/internal/eventstore"
	"github.com/hrygo/hotplex/internal/security"
	"github.com/hrygo/hotplex/internal/session"
)

// HandlerDeps groups all dependencies for Handler construction.
type HandlerDeps struct {
	Log           *slog.Logger
	Hub           *Hub
	SM            SessionManager
	Auth          *security.Authenticator
	Bridge        *Bridge
	SkillsLocator SkillsLocator
}

// WorkspaceOverridesReader is the narrow workspace-store subset Bridge needs to
// resolve per-workspace agent-config overrides (spec ② §7.3). Kept separate from
// session.UserWorkspaceStore so tests mock a single method.
type WorkspaceOverridesReader interface {
	GetWorkspaceByID(ctx context.Context, id string) (*session.Workspace, error)
}

// BridgeDeps groups all dependencies for Bridge construction.
type BridgeDeps struct {
	Log                *slog.Logger
	Hub                *Hub
	SM                 bridgeSM
	EventCollector     *eventstore.Collector  // optional; nil means event storage disabled
	TurnsQuerier       eventstore.TurnQuerier // optional; for LatestGeneration on startup
	RetryCtrl          *LLMRetryController
	AgentConfigDir     string
	TurnTimeout        time.Duration
	WorkerEnv          []string                 // extra env vars from worker.environment config
	WorkerEnvBlocklist []string                 // extra blocklist entries from worker.env_blocklist config
	CronEnv            []string                 // env vars injected only into cron platform sessions (e.g. admin API creds)
	MCPConfigJSON      string                   // pre-serialized MCP config JSON; "" = not configured → Claude Code default discovery
	AgentConfigExclude map[string][]string      // platform → inject_exclude (global default at "" key)
	WSStore            WorkspaceOverridesReader // WebChat per-workspace agent-config overrides (spec ②); nil = disabled
}
