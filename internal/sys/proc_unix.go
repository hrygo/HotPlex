//go:build !windows

package sys

import (
	"os"
	"os/exec"
	"syscall"
)

// SetupCmdSysProcAttr configures the command to run in its own process group (Unix).
func SetupCmdSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// KillProcessGroup terminates the entire process tree using the negative PID (Unix).
func KillProcessGroup(cmd *exec.Cmd) {
	if cmd != nil && cmd.Process != nil {
		// We set Setpgid = true in SetupCmdSysProcAttr, so negate the PID to kill the group.
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) //nolint:errcheck
	}
}

// IsProcessAlive checks if the process is still running using Signal(0) (Unix).
func IsProcessAlive(process *os.Process) bool {
	if process == nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}
