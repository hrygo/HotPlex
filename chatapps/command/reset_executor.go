package command

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hrygo/hotplex/engine"
	"github.com/hrygo/hotplex/event"
)

// ResetExecutor implements the /reset command
type ResetExecutor struct {
	engine     *engine.Engine
	workDir    string
	adapters   interface{} // Will be cast to *chatapps.AdapterManager
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

// SetAdapterManager sets the adapter manager for session cleanup
func (e *ResetExecutor) SetAdapterManager(adapters interface{}) {
	e.adapters = adapters
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
		{Name: "delete_claude", Message: "Deleting session file...", Status: "pending"},
		{Name: "delete_marker", Message: "Deleting marker...", Status: "pending"},
		{Name: "terminate", Message: "Terminating process...", Status: "pending"},
	}

	emitter := NewProgressEmitter(e.Command(), callback, steps)

	// Step 1: Find session (10%)
	_ = emitter.Running(0)
	_ = emitter.Emit("Resetting Context")

	sessionID := req.SessionID
	var providerSessionID string

	sess, exists := e.engine.GetSession(sessionID)
	if !exists {
		_ = emitter.Error(0, "No active session found")
		_ = emitter.Emit("Reset Failed")
		return &Result{
			Success: false,
			Message: "No active session found",
		}, nil
	}
	providerSessionID = sess.ProviderSessionID

	_ = emitter.Success(0, "Session located")
	_ = emitter.Emit("Resetting Context")

	// Step 2: Delete Claude Code session file (40%)
	_ = emitter.Running(1)
	_ = emitter.Emit("Resetting Context")

	deletedCount := e.deleteClaudeCodeSessionFile(providerSessionID)
	_ = emitter.Success(1, fmt.Sprintf("Deleted %d file(s)", deletedCount))
	_ = emitter.Emit("Resetting Context")

	// Step 3: Delete HotPlex marker (60%)
	_ = emitter.Running(2)
	_ = emitter.Emit("Resetting Context")

	markerDeleted := e.deleteHotPlexMarker(providerSessionID)
	if markerDeleted {
		_ = emitter.Success(2, "Marker deleted")
	} else {
		_ = emitter.Success(2, "Marker cleanup done")
	}
	_ = emitter.Emit("Resetting Context")

	// Step 4: Terminate session (80%)
	_ = emitter.Running(3)
	_ = emitter.Emit("Resetting Context")

	// Note: Adapter cleanup is handled by engine.StopSession callback
	// Adapters will clean up their own state (aggregator buffers, etc.)

	if err := e.engine.StopSession(sessionID, "user_requested_reset"); err != nil {
		_ = emitter.Error(3, fmt.Sprintf("Failed: %v", err))
		_ = emitter.Emit("Reset Failed")
		return &Result{
			Success: false,
			Message: fmt.Sprintf("Failed to terminate session: %v", err),
		}, nil
	}

	_ = emitter.Success(3, "Process terminated")
	_ = emitter.Emit("Resetting Context")

	// Complete
	_ = emitter.Complete("Context reset. Ready for fresh start!")

	return &Result{
		Success: true,
		Message: "Context reset. Ready for fresh start!",
		Metadata: map[string]any{
			"files_deleted":       deletedCount,
			"marker_deleted":      markerDeleted,
			"provider_session_id": providerSessionID,
		},
	}, nil
}

// deleteClaudeCodeSessionFile renames the Claude Code session file
func (e *ResetExecutor) deleteClaudeCodeSessionFile(providerSessionID string) int {
	if providerSessionID == "" {
		return 0
	}

	projectsDir := filepath.Join(os.Getenv("HOME"), ".claude", "projects")

	// Derive workspace directory from workDir
	cwd := e.workDir
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			cwd = os.TempDir() // Fallback to temp directory
		}
	}
	workspaceKey := strings.ReplaceAll(cwd, "/", "-")
	workspaceDir := filepath.Join(projectsDir, workspaceKey)

	sessionPath := filepath.Join(workspaceDir, providerSessionID+".jsonl")
	deletedPath := sessionPath + ".deleted"

	if _, err := os.Stat(sessionPath); err == nil {
		if err := os.Rename(sessionPath, deletedPath); err == nil {
			return 1
		}
	}
	return 0
}

// deleteHotPlexMarker deletes the HotPlex session marker file
func (e *ResetExecutor) deleteHotPlexMarker(providerSessionID string) bool {
	if providerSessionID == "" {
		return false
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return false
	}

	markerPath := filepath.Join(homeDir, ".hotplex", "sessions", providerSessionID+".lock")
	deletedPath := markerPath + ".deleted"

	if _, err := os.Stat(markerPath); err == nil {
		if err := os.Rename(markerPath, deletedPath); err == nil {
			return true
		}
	}
	return false
}
