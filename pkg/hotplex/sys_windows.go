//go:build windows

package hotplex

import (
	"os"
	"os/exec"
)

// setupCmdSysProcAttr configures the command for Windows (No PGID support).
func setupCmdSysProcAttr(cmd *exec.Cmd) {
	// Windows does not use Setpgid or process groups in the same way as Unix.
	// For deeper isolation on Windows, Job Objects would be required.
}

// killProcessGroup terminates the process (Windows).
func killProcessGroup(cmd *exec.Cmd) {
	if cmd != nil && cmd.Process != nil {
		// On Windows, Kill() terminates the process.
		// Tree-kill would require additional logic (e.g. taskkill /F /T /PID).
		_ = cmd.Process.Kill()
	}
}

// isProcessAlive checks if the process is still running (Windows).
func isProcessAlive(process *os.Process) bool {
	if process == nil {
		return false
	}
	// On Windows, if we have the process handle and haven't Wait()ed,
	// we assume it is alive. The goroutine in SessionManager handles dead state.
	return true
}
