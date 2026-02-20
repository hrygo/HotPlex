//go:build !windows

package hotplex

import (
	"os"
	"os/exec"
	"syscall"
)

// setupCmdSysProcAttr configures the command to run in its own process group (Unix).
func setupCmdSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessGroup terminates the entire process tree using the negative PID (Unix).
func killProcessGroup(cmd *exec.Cmd) {
	if cmd != nil && cmd.Process != nil {
		// We set Setpgid = true in setupCmdSysProcAttr, so negate the PID to kill the group.
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) //nolint:errcheck
	}
}

// isProcessAlive checks if the process is still running using Signal(0) (Unix).
func isProcessAlive(process *os.Process) bool {
	if process == nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}
