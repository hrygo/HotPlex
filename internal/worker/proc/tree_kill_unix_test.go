//go:build darwin || linux

package proc

import (
	"log/slog"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFindDescendants_NoOrphans(t *testing.T) {
	t.Parallel()
	log := slog.Default()

	cmd := exec.Command("sleep", "30")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	require.NoError(t, cmd.Start(), "start sleep")
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	require.Eventually(t, func() bool {
		return syscall.Kill(cmd.Process.Pid, 0) == nil
	}, 2*time.Second, 50*time.Millisecond)

	orphans := findDescendants(cmd.Process.Pid, log)
	require.Empty(t, orphans)
}

func TestForceKillTree_KillsRoot(t *testing.T) {
	t.Parallel()
	log := slog.Default()

	cmd := exec.Command("sleep", "30")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	require.NoError(t, cmd.Start())
	pid := cmd.Process.Pid
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	require.Eventually(t, func() bool {
		return syscall.Kill(pid, 0) == nil
	}, 2*time.Second, 50*time.Millisecond)

	ForceKillTree(pid, log)

	// Wait reaps the zombie so Kill(0) can detect the exit.
	_ = cmd.Wait()

	require.Eventually(t, func() bool {
		return syscall.Kill(pid, 0) != nil
	}, 2*time.Second, 50*time.Millisecond)
}

func TestGetChildren_Empty(t *testing.T) {
	t.Parallel()
	log := slog.Default()

	cmd := exec.Command("sleep", "5")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	children := getChildren(cmd.Process.Pid, log)
	require.Empty(t, children)
}

func TestGetPGID_ValidProcess(t *testing.T) {
	t.Parallel()

	cmd := exec.Command("sleep", "5")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	pgid := getPGID(cmd.Process.Pid)
	require.Greater(t, pgid, 0)
}

func TestGetPGID_NonexistentProcess(t *testing.T) {
	t.Parallel()

	pgid := getPGID(9999999)
	require.Equal(t, -1, pgid)
}
