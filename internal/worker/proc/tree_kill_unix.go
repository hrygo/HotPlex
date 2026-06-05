//go:build darwin || linux

package proc

import (
	"log/slog"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// ForceKillTree sends SIGKILL to the entire process group identified by pgid,
// then recursively discovers and kills any orphaned descendant processes that
// escaped the PGID (e.g., because the child called process_group(0) or setsid).
//
// This is necessary because some worker runtimes (notably Codex) spawn MCP
// servers with their own process group, making them invisible to the standard
// kill(-pgid, SIGKILL) approach.
func ForceKillTree(pgid int, log *slog.Logger) {
	if pgid <= 0 {
		return
	}

	// Phase 1: Collect orphaned descendants BEFORE killing the root,
	// because getPGID(rootPID) returns -1 once the root is dead.
	descendants := findDescendants(pgid, log)

	// Phase 2: Kill the main process group.
	_ = ForceKill(pgid)

	// Phase 3: Kill orphaned descendants that escaped the PGID.
	if len(descendants) == 0 {
		return
	}

	log.Warn("proc: found orphaned descendants outside PGID, killing",
		"root_pid", pgid,
		"count", len(descendants),
	)

	for _, pid := range descendants {
		if err := syscall.Kill(pid, syscall.SIGKILL); err != nil && !IsProcessNotExist(err) {
			log.Debug("proc: failed to kill orphan", "pid", pid, "err", err)
		}
	}
}

// GracefulTerminateTree sends SIGTERM to the entire process group identified by pgid,
// then recursively discovers and terminates any orphaned descendant processes.
// After the grace period, it escalates to SIGKILL via ForceKillTree.
func GracefulTerminateTree(pgid int, gracePeriod time.Duration, log *slog.Logger) {
	if pgid <= 0 {
		return
	}

	// Collect orphans before sending any signals.
	descendants := findDescendants(pgid, log)

	// SIGTERM the main process group.
	_ = GracefulTerminate(pgid)

	// SIGTERM any orphaned descendants.
	for _, pid := range descendants {
		_ = syscall.Kill(pid, syscall.SIGTERM)
	}

	if len(descendants) > 0 {
		log.Info("proc: sent SIGTERM to orphaned descendants",
			"root_pid", pgid,
			"count", len(descendants),
		)
	}

	// Escalate to SIGKILL after grace period.
	time.Sleep(gracePeriod)

	// Re-collect orphans (some may have spawned between first scan and now).
	_ = ForceKill(pgid)
	newOrphans := findDescendants(pgid, log)
	merged := make([]int, 0, len(descendants)+len(newOrphans))
	merged = append(merged, descendants...)
	merged = append(merged, newOrphans...)
	seen := make(map[int]bool)
	for _, pid := range merged {
		if seen[pid] {
			continue
		}
		seen[pid] = true
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
}

// findDescendants discovers all descendant processes of the given root PID
// by recursively walking the process tree via `pgrep -P`.
// It returns PIDs of processes that are NOT in the same process group as root
// (i.e., orphans that escaped the PGID).
//
// The walk is BFS: it first collects all children, then recursively collects
// their children. Only processes in a different PGID than the root are returned,
// since same-PGID processes are already handled by kill(-pgid, ...).
func findDescendants(rootPID int, log *slog.Logger) []int {
	// First, find the root's PGID to compare against.
	rootPGID := getPGID(rootPID, log)
	if rootPGID <= 0 {
		// Cannot determine root PGID; fall back to killing all descendants.
		rootPGID = -1
	}

	visited := make(map[int]bool)
	var orphans []int

	// BFS queue: start with direct children of root.
	queue := getChildren(rootPID, log)

	for len(queue) > 0 {
		pid := queue[0]
		queue = queue[1:]

		if visited[pid] {
			continue
		}
		visited[pid] = true

		// Check if this process is in a different PGID than root.
		pgid := getPGID(pid, log)
		if pgid != rootPGID {
			orphans = append(orphans, pid)
		}

		// Enqueue children of this process.
		children := getChildren(pid, log)
		queue = append(queue, children...)
	}

	return orphans
}

// getChildren returns the PIDs of direct child processes of the given PID.
// Uses `pgrep -P <pid>` which is available on both macOS and Linux.
func getChildren(pid int, log *slog.Logger) []int {
	out, err := exec.Command("pgrep", "-P", strconv.Itoa(pid)).Output()
	if err != nil {
		// pgrep exits with 1 when no children found — not an error.
		return nil
	}

	var children []int
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		childPID, err := strconv.Atoi(line)
		if err != nil {
			log.Debug("proc: failed to parse child PID", "raw", line, "err", err)
			continue
		}
		children = append(children, childPID)
	}
	return children
}

// getPGID returns the process group ID of the given PID.
// Returns -1 if the process does not exist or PGID cannot be determined.
func getPGID(pid int, log *slog.Logger) int {
	// On macOS and Linux, the PGID can be read via `ps -o pgid= -p <pid>`.
	out, err := exec.Command("ps", "-o", "pgid=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return -1
	}

	line := strings.TrimSpace(string(out))
	if line == "" {
		return -1
	}

	pgid, err := strconv.Atoi(line)
	if err != nil {
		log.Debug("proc: failed to parse PGID", "pid", pid, "raw", line, "err", err)
		return -1
	}
	return pgid
}
