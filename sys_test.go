//go:build unix

package hotplex

import (
	"os"
	"os/exec"
	"syscall"
	"testing"
)

func TestSetupCmdSysProcAttr(t *testing.T) {
	cmd := exec.Command("echo", "test")
	setupCmdSysProcAttr(cmd)

	// Verify SysProcAttr is set
	if cmd.SysProcAttr == nil {
		t.Fatal("SysProcAttr should not be nil")
	}

	// Verify PGID is set
	if !cmd.SysProcAttr.Setpgid {
		t.Error("Setpgid should be true")
	}
}

func TestKillProcessGroup(t *testing.T) {
	// Start a simple process
	cmd := exec.Command("sleep", "10")
	setupCmdSysProcAttr(cmd)

	err := cmd.Start()
	if err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}

	pid := cmd.Process.Pid

	// Kill the process group
	killProcessGroup(cmd)

	// Wait for process to finish
	_ = cmd.Wait()

	// Verify process is gone
	process, err := os.FindProcess(pid)
	if err != nil {
		t.Fatalf("FindProcess error: %v", err)
	}

	// Sending signal 0 checks if process exists
	err = process.Signal(syscall.Signal(0))
	if err == nil {
		t.Error("Process should be terminated")
	}
}

func TestKillProcessGroup_NilCmd(t *testing.T) {
	// Should not panic with nil cmd
	killProcessGroup(nil)
}

func TestKillProcessGroup_NilProcess(t *testing.T) {
	// Should not panic with nil Process
	cmd := &exec.Cmd{}
	killProcessGroup(cmd)
}

func TestIsProcessAlive(t *testing.T) {
	// Start a process
	cmd := exec.Command("sleep", "1")
	setupCmdSysProcAttr(cmd)

	err := cmd.Start()
	if err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}

	// Process should be alive
	if !isProcessAlive(cmd.Process) {
		t.Error("Process should be alive")
	}

	// Kill process
	_ = cmd.Process.Kill()
	_ = cmd.Wait()

	// Process should not be alive
	if isProcessAlive(cmd.Process) {
		t.Error("Process should not be alive after kill")
	}
}

func TestIsProcessAlive_NilProcess(t *testing.T) {
	if isProcessAlive(nil) {
		t.Error("nil process should not be alive")
	}
}
