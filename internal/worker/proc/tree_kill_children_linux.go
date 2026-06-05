//go:build linux

package proc

import (
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// getChildren returns the PIDs of direct child processes of the given PID.
// Reads /proc/<pid>/task/<tid>/children (kernel 3.5+) or falls back to
// scanning /proc/*/stat for matching PPID.
func getChildren(pid int, _ *slog.Logger) []int {
	// Fast path: /proc/<pid>/task/<pid>/children (single file, space-separated PIDs).
	if children := readChildrenFile(pid); len(children) > 0 {
		return children
	}

	// Slow path: scan /proc/*/stat for matching ppid.
	return scanChildrenFromProcStat(pid)
}

func readChildrenFile(pid int) []int {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "task", strconv.Itoa(pid), "children"))
	if err != nil {
		return nil
	}
	var children []int
	for _, field := range strings.Fields(string(data)) {
		if childPID, err := strconv.Atoi(field); err == nil {
			children = append(children, childPID)
		}
	}
	return children
}

func scanChildrenFromProcStat(pid int) []int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	ppidStr := strconv.Itoa(pid)
	var children []int
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		childPID, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		statBytes, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "stat"))
		if err != nil {
			continue
		}
		// Format: pid (comm) state ppid ...
		// Find the closing ')' to skip over comm (may contain spaces/parens).
		stat := string(statBytes)
		idx := strings.LastIndex(stat, ")")
		if idx < 0 || idx+2 >= len(stat) {
			continue
		}
		fields := strings.Fields(stat[idx+2:])
		if len(fields) < 2 {
			continue
		}
		if fields[1] == ppidStr {
			children = append(children, childPID)
		}
	}
	return children
}
