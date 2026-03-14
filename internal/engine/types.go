package engine

import (
	"context"
	"log/slog"
	"time"

	"github.com/hrygo/hotplex/provider"
)

// SessionStatus defines the current state of a session.
type SessionStatus string

const (
	SessionStatusStarting SessionStatus = "starting"
	SessionStatusReady    SessionStatus = "ready"
	SessionStatusBusy     SessionStatus = "busy"
	SessionStatusDead     SessionStatus = "dead"
)

// Scanner buffer sizes for CLI output parsing.
const (
	ScannerInitialBufSize = 4 * 1024   // 4 KB - start small
	ScannerMaxBufSize    = 512 * 1024 // 512 KB - sufficient for most CLI outputs
)

// Session lifecycle constants.
const (
	DefaultReadyTimeout = 10 * time.Second // Maximum time to wait for session to be ready
)

// SessionConfig contains the minimal configuration needed for session management.
// This is a subset of the root Config to avoid circular dependencies.
type SessionConfig struct {
	WorkDir          string // Absolute path to the isolated sandbox directory
	TaskInstructions string // Persistent instructions for the session
	BaseSystemPrompt string // Session-level system prompt override (takes precedence over Engine-level)
}

// Callback handles streaming events from the CLI.
// Events are dispatched as they occur, allowing real-time UI updates.
type Callback func(eventType string, data any) error

// EngineOptions defines the configuration parameters for initializing a new Engine.
// It allows customization of timeouts, logging, and foundational security boundaries
// that apply to all sessions managed by this engine instance.
type EngineOptions struct {
	Timeout     time.Duration // Maximum time to wait for a single execution turn to complete
	IdleTimeout time.Duration // Time after which an idle session is eligible for termination
	Logger      *slog.Logger  // Optional logger instance; defaults to slog.Default()

	// Namespace is used to generate isolated, deterministic UUID v5 Session IDs.
	// This ensures that the same Conversation ID creates an isolated but persistent sandbox,
	// preventing cross-application or cross-user session leaks.
	Namespace string

	// Foundational Security & Context (Engine-level boundaries)
	PermissionMode             string   // Controls CLI permissions (e.g., "bypass-permissions"). Defaults to strict mode.
	DangerouslySkipPermissions bool     // Bypasses all permission checks. Equivalent to --dangerously-skip-permissions.
	BaseSystemPrompt           string   // Foundational instructions injected at CLI startup for all sessions.
	AllowedTools               []string // Explicit list of tools allowed (whitelist). If empty, all tools are allowed.
	DisallowedTools            []string // Explicit list of tools forbidden (blacklist).

	// AdminToken is the secret required to toggle security bypass mode.
	// If empty, bypass will be disabled for security.
	AdminToken string

	// Provider is the AI CLI provider (e.g., Claude Code, OpenCode).
	// If nil, defaults to ClaudeCodeProvider.
	Provider provider.Provider
}

// SessionManager defines the behavioral interface for managing a process pool.
type SessionManager interface {
	// GetOrCreateSession retrieves an active session or performs a Cold Start if none exists.
	// Returns the session, a boolean indicating if it was a Cold Start, and an error if any.
	GetOrCreateSession(ctx context.Context, sessionID string, cfg SessionConfig, prompt string) (*Session, bool, error)
	// GetSession performs a non-side-effect lookup of an active session.
	GetSession(sessionID string) (*Session, bool)
	// TerminateSession kills the OS process group and removes the session from the pool.
	TerminateSession(sessionID string) error
	// ListActiveSessions provides a snapshot of all tracked sessions.
	ListActiveSessions() []*Session
	// Shutdown performing a total cleanup of the pool and its background workers.
	Shutdown()
}
