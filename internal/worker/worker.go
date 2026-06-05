// Package worker defines the interfaces that all worker adapters must implement.
package worker

import (
	"context"
	"errors"
	"time"

	"github.com/hrygo/hotplex/pkg/events"
)

// ErrNotImplemented is returned for unimplemented worker methods.
var ErrNotImplemented = errors.New("worker: not implemented")

// ─── SessionConn ─────────────────────────────────────────────────────────────

// SessionConn represents the bidirectional communication channel between
// the gateway and a worker's runtime process. It is the data plane interface.
type SessionConn interface {
	// Send delivers a message to the worker runtime.
	Send(ctx context.Context, msg *events.Envelope) error

	// Recv returns a channel that yields messages from the worker runtime.
	// The channel is closed when the connection is closed.
	Recv() <-chan *events.Envelope

	// Close terminates the connection and releases resources.
	Close() error

	// UserID returns the user who owns this session.
	UserID() string

	// SessionID returns the session identifier.
	SessionID() string
}

// ─── Capabilities ───────────────────────────────────────────────────────────

// Capabilities describes what a worker adapter supports.
type Capabilities interface {
	// Type returns the worker type identifier (e.g. "claude_code", "opencode_server", "codex_cli", "acp").
	Type() WorkerType

	// SupportsResume returns true if the worker can resume a previous session.
	SupportsResume() bool

	// SupportsStreaming returns true if the worker emits streaming (delta) events.
	SupportsStreaming() bool

	// SupportsTools returns true if the worker exposes tool call capabilities.
	SupportsTools() bool

	// EnvBlocklist returns the set of environment variable names this worker
	// must NOT receive (empty = all allowed).
	EnvBlocklist() []string

	// SessionStoreDir returns the directory where the worker stores session state,
	// or empty string if the worker does not persist sessions.
	SessionStoreDir() string

	// MaxTurns returns the maximum number of turns (input/output cycles) allowed
	// per session, or 0 if unlimited.
	MaxTurns() int

	// Modalities returns the supported content modalities (e.g. "text", "code", "image").
	Modalities() []string
}

// WorkerType is the string identifier for a worker implementation.
type WorkerType string

const (
	TypeClaudeCode  WorkerType = "claude_code"
	TypeOpenCodeSrv WorkerType = "opencode_server"
	TypeCodexCLI    WorkerType = "codex_cli"
	TypeACP         WorkerType = "acp"
	TypeUnknown     WorkerType = "unknown"
)

// ─── Worker ─────────────────────────────────────────────────────────────────

// Worker is the main interface that all worker adapters must implement.
// The gateway communicates with a worker exclusively through this interface.
type Worker interface {
	Capabilities

	// Start launches the worker runtime for the given session.
	// It blocks until the runtime is ready to receive input or an error occurs.
	Start(ctx context.Context, session SessionInfo) error

	// Input delivers a user message to the worker runtime.
	Input(ctx context.Context, content string, metadata map[string]any) error

	// Resume reattaches to an existing session using the sessionID from session.Info.
	// Returns ErrSessionNotFound if the session cannot be located.
	Resume(ctx context.Context, session SessionInfo) error

	// Terminate gracefully stops the worker runtime.
	// It sends SIGTERM first, then SIGKILL after grace period.
	Terminate(ctx context.Context) error

	// Kill immediately terminates the worker runtime with SIGKILL.
	Kill() error

	// Wait blocks until the worker runtime exits, returning the exit code.
	Wait() (int, error)

	// Conn returns the SessionConn for this worker, or nil if not started.
	Conn() SessionConn

	// Health returns a snapshot of the worker's runtime health.
	Health() WorkerHealth

	// LastIO returns the time of the last I/O activity (input sent or output received).
	// Used by GC zombie detection to identify stuck workers.
	// Implementations that don't track I/O should return the zero time.Time.
	LastIO() time.Time

	// ResetContext clears the worker's runtime context.
	// The worker decides the implementation:
	//   - Per-process workers (Claude Code, Codex): terminate + restart process, return ConnReplaced=true.
	//   - In-place workers (OCS, ACP): reset via API/RPC, emit internal_reset event, return ConnReplaced=false.
	// Gateway reads ResetResult.ConnReplaced to decide whether to spawn a new forwardEvents goroutine.
	// Note: Gateway layer has already called sm.ClearContext() to clear SessionInfo.Context.
	ResetContext(ctx context.Context) (ResetResult, error)
}

// ResetResult describes the outcome of a ResetContext call.
// Gateway reads this to decide orchestration without knowing Worker internals.
type ResetResult struct {
	// ConnReplaced indicates whether the worker replaced its underlying connection.
	// true  = Worker restarted the process or rebuilt the connection (e.g. Claude Code, Codex Exec).
	//         Gateway must spawn a new forwardEvents goroutine.
	// false = Worker reset in-place without replacing the connection (e.g. OCS HTTP reset, ACP new session).
	//         The existing forwardEvents goroutine continues running.
	ConnReplaced bool
}

// WorkerHealth reports the runtime health of a worker process.
type WorkerHealth struct {
	Type      WorkerType `json:"type"`
	SessionID string     `json:"session_id"`
	PID       int        `json:"pid"`
	Running   bool       `json:"running"`
	Healthy   bool       `json:"healthy"`
	Uptime    string     `json:"uptime"`
	Error     string     `json:"error,omitempty"`

	// Agent discovery fields (populated by ACP and similar workers).
	AgentName       string `json:"agent_name,omitempty"`
	AgentVersion    string `json:"agent_version,omitempty"`
	ProtocolVersion int    `json:"protocol_version,omitempty"`
}

// InputRecoverer is an optional interface for session connections that cache
// the last user input for crash recovery re-delivery. Bridge detects
// implementations via type assertion to recover input after resume failure.
type InputRecoverer interface {
	LastInput() string
}

// EventInjector is an optional interface for SessionConn implementations
// that support injecting synthetic events into the Recv() stream.
// In-place-reset workers use this to emit internal_reset events that
// forwardEvents processes without client forwarding.
type EventInjector interface {
	Inject(env *events.Envelope)
}

// WorkerSessionIDHandler is an optional interface for workers that manage
// their own internal session IDs separate from the Gateway session ID.
// Bridge detects implementations via type assertion and uses them to
// persist/restore worker-internal session IDs for resume support.
type WorkerSessionIDHandler interface {
	SetWorkerSessionID(id string)
	GetWorkerSessionID() string
}

// SessionInfo contains metadata about a session needed by the worker to start/resume.
type SessionInfo struct {
	SessionID    string
	UserID       string
	ProjectDir   string
	Env          map[string]string
	Args         []string
	AllowedTools []string // tools allowed for this session (from InitConfig.AllowedTools)

	// WorkerSessionID is the internal session ID used by the worker runtime.
	// For workers that manage their own session state (OpenCode Server),
	// this field carries the worker-internal session ID for persistence and resume.
	// Empty for workers that use Gateway SessionID directly (Claude Code).
	WorkerSessionID string
	AllowedModels   []string // models allowed for this session

	// MCPConfig holds MCP server configuration as JSON content ({"mcpServers":{...}}).
	// Written to a temp file by buildCLIArgs and passed via --mcp-config.
	MCPConfig string
	// StrictMCPConfig restricts MCP servers to only those specified in MCPConfig (--strict-mcp-config).
	StrictMCPConfig bool
	// DisallowedTools lists tools that the worker should NOT use.
	DisallowedTools []string
	// SystemPrompt is appended to the worker's default system prompt (--append-system-prompt).
	SystemPrompt string
	// SystemPromptReplace, if non-empty, replaces the default system prompt entirely (--system-prompt).
	// Takes precedence over SystemPrompt when set.
	SystemPromptReplace string
	// ConfigEnv holds extra env vars from worker.environment config. These always
	// override values from os.Environ().
	ConfigEnv []string
	// ConfigBlocklist holds additional env var names from worker.env_blocklist config.
	// These are merged with the hardcoded per-worker blocklist in BuildEnv.
	ConfigBlocklist []string
	// PermissionMode controls how the worker handles permission requests.
	// Valid values: "default", "plan", "auto-accept".
	PermissionMode string
	// SkipPermissions bypasses all permission checks (equivalent to --dangerously-skip-permissions).
	SkipPermissions bool
	// Sandbox controls the codex CLI sandbox mode ("read-only", "workspace-write", "danger-full-access").
	// Empty = use config default. Per-bot override propagated from messaging config.
	Sandbox string
	// ACPCommand overrides the global ACP agent binary for this session.
	// Empty = use worker.acp.command config. Per-bot override propagated via platformKey.
	ACPCommand string
	// ContinueSession resumes the latest session in the current directory without a session ID.
	ContinueSession bool
	// ForkSession, when resuming, creates a new session ID instead of reusing the existing one.
	ForkSession bool
	// ResumeSessionAt restores the session up to and including the specified
	// assistant message ID, discarding later history (--resume-session-at).
	ResumeSessionAt string
	// ResumeSessionID is the worker-internal session ID for resuming a previous session.
	// For one-shot workers (e.g. Codex CLI), this carries the thread ID for resume --last.
	ResumeSessionID string
	// MaxTurns limits the number of agentic turns in non-interactive mode.
	MaxTurns int
	// Bare runs Claude Code in minimal mode, skipping hooks, LSP, and plugin sync.
	Bare bool
	// AllowedDirs lists additional directories the worker can access (--add-dir).
	AllowedDirs []string
	// MaxBudgetUSD caps API spending per session (--max-budget-usd).
	MaxBudgetUSD float64
	// JSONSchema validates structured output against a JSON Schema (--json-schema).
	JSONSchema string
	// IncludeHookEvents exposes all hook lifecycle events in the output stream
	// (--include-hook-events).
	IncludeHookEvents bool
	// IncludePartialMessages exposes partial message blocks as they arrive
	// (--include-partial-messages).
	IncludePartialMessages bool
	// Images carries image file paths for codex exec --image flags.
	// NOTE: Currently populated only in exec-mode buildArgs() which reads
	// from SessionInfo directly. Per-session injection through gateway/bridge
	// is not yet wired. Reserved for future per-session image support.
	Images []string
}

// SandboxPlatformKey is the platformKey map key used to propagate sandbox config
// from bridge/executor through session persistence to the worker.
const SandboxPlatformKey = "_sandbox"

// ACPCommandPlatformKey is the platformKey map key used to propagate per-bot
// ACP command override from messaging bridge through session persistence to the ACP worker.
const ACPCommandPlatformKey = "_acp_command"

// ForkSessionPlatformKey is the platformKey map key used to signal that a session
// should be forked from an existing session instead of resumed or created fresh.
// Expected value: "true" (string, checked via == "true" in bridge).
const ForkSessionPlatformKey = "_fork_session"

// JSONSchemaPlatformKey is the platformKey map key used to inject a JSON Schema
// for structured output into the first prompt of a session.
// Expected value: raw JSON Schema string (e.g. '{"type":"object","properties":{...}}').
const JSONSchemaPlatformKey = "_json_schema"
