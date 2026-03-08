package command

import (
	"context"
	"fmt"

	"github.com/hrygo/hotplex/engine"
	"github.com/hrygo/hotplex/event"
)

// ResetExecutor implements the /reset command
type ResetExecutor struct {
	engine  *engine.Engine
	workDir string
}

// Verify ResetExecutor implements Executor at compile time
var _ Executor = (*ResetExecutor)(nil)

// NewResetExecutor creates a new reset command executor
func NewResetExecutor(eng *engine.Engine, workDir string) *ResetExecutor {
	return &ResetExecutor{
		engine:  eng,
		workDir: workDir,
	}
}

// Command returns the command name
func (e *ResetExecutor) Command() string {
	return CommandReset
}

// Description returns the command description
func (e *ResetExecutor) Description() string {
	return "Reset conversation context and start fresh"
}

// Execute runs the reset command
func (e *ResetExecutor) Execute(ctx context.Context, req *Request, callback event.Callback) (*Result, error) {
	// Define progress steps
	steps := []ProgressStep{
		{Name: "find_session", Message: "Finding session...", Status: "pending"},
		{Name: "terminate", Message: "Terminating process...", Status: "pending"},
		{Name: "delete_marker", Message: "Deleting marker...", Status: "pending"},
		{Name: "delete_claude", Message: "Deleting session file...", Status: "pending"},
	}

	emitter := NewProgressEmitter(e.Command(), callback, steps)

	// Step 1: Find session (10%)
	_ = emitter.Running(0)

	sessionID := req.SessionID
	var providerSessionID string

	sess, exists := e.engine.GetSession(sessionID)
	if !exists {
		_ = emitter.Error(0, "No conversation to reset")
		_ = emitter.Emit("Nothing to Reset")
		_ = emitter.Complete("No active conversation found.")
		return &Result{
			Success: true, // Not an error - just nothing to do
			Message: "No active conversation found. Start chatting first!",
		}, nil
	}
	providerSessionID = sess.ProviderSessionID

	_ = emitter.Success(0, "Session located")

	// Step 2: Terminate session FIRST (40%)
	// This prevents race conditions where new messages could recreate the marker
	_ = emitter.Running(1)

	// Note: Adapter cleanup is handled by engine.StopSession callback
	// Adapters will clean up their own state (aggregator buffers, etc.)

	if err := e.engine.StopSession(sessionID, "user_requested_reset"); err != nil {
		_ = emitter.Error(1, fmt.Sprintf("Termination error: %v", err))
		_ = emitter.Emit("Reset Incomplete")
		_ = emitter.Complete(fmt.Sprintf("Could not fully reset. (%v)", err))
		return &Result{
			Success: false,
			Message: fmt.Sprintf("Could not fully reset. Please try again or use /disconnect first. (%v)", err),
		}, nil
	}

	_ = emitter.Success(1, "Process terminated")

	// Step 3 & 4: Delete session context via unified Engine API (60%-80%)
	_ = emitter.Running(2)

	// Calls down to Provider interface to scrape `.jsonl` and HotPlex to drop the `.lock` marker
	cleanupErr := e.engine.CleanupSessionFiles(sessionID)
	if cleanupErr != nil {
		_ = emitter.Error(2, fmt.Sprintf("Cleanup incomplete: %v", cleanupErr))
		_ = emitter.Error(3, fmt.Sprintf("Cleanup incomplete: %v", cleanupErr))
		_ = emitter.Emit("Reset Incomplete")
		_ = emitter.Complete(fmt.Sprintf("Process terminated, but cleanup failed. Try /dc then /reset. (%v)", cleanupErr))
		return &Result{
			Success: false,
			Message: fmt.Sprintf("Process terminated, but context cleanup failed. Try /dc then /reset. (%v)", cleanupErr),
		}, nil
	}

	_ = emitter.Success(2, "Marker deleted")
	_ = emitter.Running(3)
	_ = emitter.Success(3, "Session file deleted")

	// Complete
	_ = emitter.Emit("Resetting Context")
	_ = emitter.Complete("Context reset. Ready for fresh start!")

	return &Result{
		Success: true,
		Message: "Context reset. Ready for fresh start!",
		Metadata: map[string]any{
			"provider_session_id": providerSessionID,
		},
	}, nil
}
