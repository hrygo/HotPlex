//go:build darwin || linux

package proc

import (
	"log/slog"
	"syscall"
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

	descendants := findDescendants(pgid, log)
	_ = ForceKill(pgid)

	if len(descendants) == 0 {
		return
	}

	log.Warn("proc: found orphaned descendants outside PGID, killing",
		"root_pid", pgid, "count", len(descendants))

	for _, pid := range descendants {
		if err := syscall.Kill(pid, syscall.SIGKILL); err != nil && !IsProcessNotExist(err) {
			log.Debug("proc: failed to kill orphan", "pid", pid, "err", err)
		}
	}
}

// findDescendants discovers all descendant processes of the given root PID
// by recursively walking the process tree via platform-native child enumeration.
// It returns PIDs of processes that are NOT in the same process group as root
// (i.e., orphans that escaped the PGID).
func findDescendants(rootPID int, log *slog.Logger) []int {
	rootPGID := getPGID(rootPID)
	if rootPGID <= 0 {
		rootPGID = -1
	}

	visited := make(map[int]bool)
	var orphans []int
	queue := getChildren(rootPID, log)

	for len(queue) > 0 {
		pid := queue[0]
		queue = queue[1:]

		if visited[pid] {
			continue
		}
		visited[pid] = true

		if pgid := getPGID(pid); pgid != rootPGID {
			orphans = append(orphans, pid)
		}

		queue = append(queue, getChildren(pid, log)...)
	}

	return orphans
}

// getPGID returns the process group ID of the given PID using syscall.Getpgid.
func getPGID(pid int) int {
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		return -1
	}
	return pgid
}
