package command

import (
	"context"

	"github.com/hrygo/hotplex/event"
)

// Executor defines the interface for slash command executors.
// Each command (/reset, /dc, etc.) implements this interface.
type Executor interface {
	// Command returns the command name (e.g., "/reset")
	Command() string

	// Description returns a human-readable description
	Description() string

	// Execute runs the command, sending progress updates via callback
	Execute(ctx context.Context, req *Request, callback event.Callback) (*Result, error)
}

// Request encapsulates a slash command request
type Request struct {
	Command           string         // Original command (e.g., "/reset")
	Text              string         // Command arguments
	UserID            string         // User who invoked the command
	ChannelID         string         // Channel where command was invoked
	SessionID         string         // Associated session ID
	ProviderSessionID string         // Provider session ID (if available)
	ResponseURL       string         // Slack response_url (optional)
	Metadata          map[string]any // Platform-specific metadata
}

// Result encapsulates command execution result
type Result struct {
	Success  bool
	Message  string         // Final message to display
	Metadata map[string]any // Result metadata
}

// ProgressStep represents a single step in command execution
type ProgressStep struct {
	Name    string // Step identifier (e.g., "find_session")
	Message string // Human-readable message
	Status  string // "pending", "running", "success", "error"
}
