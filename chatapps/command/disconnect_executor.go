package command

import (
	"context"
	"fmt"

	"github.com/hrygo/hotplex/engine"
	"github.com/hrygo/hotplex/event"
)

// DisconnectExecutor implements the /dc command
type DisconnectExecutor struct {
	engine *engine.Engine
}

// Verify DisconnectExecutor implements Executor at compile time
var _ Executor = (*DisconnectExecutor)(nil)

// NewDisconnectExecutor creates a new disconnect command executor
func NewDisconnectExecutor(eng *engine.Engine) *DisconnectExecutor {
	return &DisconnectExecutor{
		engine: eng,
	}
}

// Command returns the command name
func (e *DisconnectExecutor) Command() string {
	return "/dc"
}

// Description returns the command description
func (e *DisconnectExecutor) Description() string {
	return "Disconnect and preserve context for resume"
}

// Execute runs the disconnect command
func (e *DisconnectExecutor) Execute(ctx context.Context, req *Request, callback event.Callback) (*Result, error) {
	// Define progress steps
	steps := []ProgressStep{
		{Name: "find_session", Message: "Finding session...", Status: "pending"},
		{Name: "terminate", Message: "Terminating process...", Status: "pending"},
	}

	emitter := NewProgressEmitter(e.Command(), callback, steps)

	// Step 1: Find session
	_ = emitter.Running(0)

	sessionID := req.SessionID

	sess, exists := e.engine.GetSession(sessionID)
	if !exists {
		_ = emitter.Error(0, "No conversation to disconnect")
		return &Result{
			Success: true, // Not an error - already disconnected
			Message: "No active conversation. Already idle.",
		}, nil
	}

	_ = emitter.Success(0, "Session located")

	// Step 2: Terminate session (context is preserved)
	_ = emitter.Running(1)

	// StopSession preserves the marker file, allowing resume on next message
	if err := e.engine.StopSession(sessionID, "user_requested_disconnect"); err != nil {
		_ = emitter.Error(1, fmt.Sprintf("Failed: %v", err))
		return &Result{
			Success: false,
			Message: fmt.Sprintf("Failed to disconnect: %v", err),
		}, nil
	}

	_ = emitter.Success(1, "Process terminated")

	// Complete
	_ = emitter.Complete("Disconnected from CLI. Context preserved. Next message will resume.")

	return &Result{
		Success: true,
		Message: "Disconnected from CLI. Context preserved. Next message will resume.",
		Metadata: map[string]any{
			"provider_session_id": sess.ProviderSessionID,
		},
	}, nil
}
