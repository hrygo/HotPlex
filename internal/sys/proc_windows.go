//go:build windows

package sys

import (
	"fmt"
	"os"
	"os/exec"
)

// SetupCmdSysProcAttr configures the command for Windows (No PGID support).
func SetupCmdSysProcAttr(cmd *exec.Cmd) {
	// Windows does not use Setpgid or process groups in the same way as Unix.
	// For deeper isolation on Windows, Job Objects would be required.
}

// KillProcessGroup terminates the process and its children on Windows (#10).
// Uses taskkill /F /T /PID to kill the entire process tree.
func KillProcessGroup(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}

	// Use taskkill to terminate the entire process tree
	// /F = force, /T = terminate all child processes, /PID = process ID
	killCmd := exec.Command("taskkill", "/F", "/T", "/PID", fmt.Sprintf("%d", cmd.Process.Pid))
	// Ignore errors - process may already be dead
	_ = killCmd.Run()

	// Fallback: try direct Kill() in case taskkill failed
	_ = cmd.Process.Kill()
}

// IsProcessAlive checks if the process is still running (Windows).
func IsProcessAlive(process *os.Process) bool {
	if process == nil {
		return false
	}
	// On Windows, if we have the process handle and haven't Wait()ed,
	// we assume it is alive. The goroutine in SessionManager handles dead state.
	return true
}
