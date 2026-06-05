//go:build darwin

package proc

import (
	"log/slog"
	"syscall"
	"unsafe"
)

// getChildren returns the PIDs of direct child processes of the given PID.
// Uses sysctl KERN_PROC_PID to enumerate processes, then filters by PPID match.
func getChildren(pid int, _ *slog.Logger) []int {
	// Use kern.proc.pid.* to get process info, then filter by ppid.
	// On macOS, we use syscall.Sysctl to read the process list.
	// KERN_PROC_PID = 1 (see sysctl.h)
	var children []int

	// Try sysctl approach: iterate potential PIDs and check ppid.
	// Since there's no direct "children of PID" syscall on macOS,
	// we use kern.proc.pid to check each candidate.
	// For efficiency with small trees, we limit the scan range.
	const maxScanPID = 65536
	for candidate := 1; candidate < maxScanPID; candidate++ {
		if ppid := getParentPID(candidate); ppid == pid {
			children = append(children, candidate)
		}
	}
	return children
}

// getParentPID returns the parent PID of the given process using sysctl.
// Returns 0 if the process doesn't exist or info can't be retrieved.
func getParentPID(pid int) int {
	// kern.proc.pid.<pid> returns a []C.kinfo_proc (externKinfoProc)
	// The kinfo_proc structure has ki_ppid at a known offset.
	// On macOS arm64/x86_64, kinfo_proc.kp_proc.p_ppid is at offset 24.
	name := []int32{1, 14, 1, int32(pid)} // CTL_KERN, KERN_PROC, KERN_PROC_PID, pid

	var buf []byte
	bufLen := uintptr(0)

	// First call: get required buffer size.
	_, _, errno := syscall.Syscall6(
		syscall.SYS___SYSCTL,
		uintptr(unsafe.Pointer(&name[0])),
		4,
		0,
		uintptr(unsafe.Pointer(&bufLen)),
		0,
		0,
	)
	if errno != 0 || bufLen == 0 {
		return 0
	}

	buf = make([]byte, bufLen)
	_, _, errno = syscall.Syscall6(
		syscall.SYS___SYSCTL,
		uintptr(unsafe.Pointer(&name[0])),
		4,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&bufLen)),
		0,
		0,
	)
	if errno != 0 {
		return 0
	}

	// kinfo_proc layout on macOS:
	// kp_proc (extern_proc) starts at offset 0
	//   extern_proc.p_ppid is at offset 24 (after p_stat, p_pid, etc.)
	if len(buf) < 32 {
		return 0
	}
	return int(*(*int32)(unsafe.Pointer(&buf[24])))
}
