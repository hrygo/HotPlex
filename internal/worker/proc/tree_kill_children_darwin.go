//go:build darwin

package proc

import (
	"log/slog"
	"unsafe"

	"golang.org/x/sys/unix"
)

// getChildren returns the PIDs of direct child processes of the given PID.
// Uses sysctl KERN_PROC_PID to enumerate processes, then filters by PPID match.
func getChildren(pid int, _ *slog.Logger) []int {
	var children []int

	// sysctl KERN_PROC_PID per-candidate is O(N) syscalls.
	// Most systems have <4096 processes; cap the scan to avoid 131K syscalls
	// on the worker shutdown path.
	const maxScanPID = 4096
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
	name := []int32{1, 14, 1, int32(pid)} // CTL_KERN, KERN_PROC, KERN_PROC_PID, pid

	var bufLen uintptr
	// First call: get required buffer size.
	_, _, errno := syscall6(
		unsafe.Pointer(&name[0]),
		4,
		0,
		uintptr(unsafe.Pointer(&bufLen)),
	)
	if errno != 0 || bufLen == 0 {
		return 0
	}

	buf := make([]byte, bufLen)
	_, _, errno = syscall6(
		unsafe.Pointer(&name[0]),
		4,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&bufLen)),
	)
	if errno != 0 {
		return 0
	}

	// Parse using the typed KinfoProc struct to avoid fragile hardcoded offsets.
	// Eproc.Ppid is the parent PID provided by the kernel.
	var kp unix.KinfoProc
	if uintptr(len(buf)) < unsafe.Sizeof(kp) {
		return 0
	}
	kp = *(*unix.KinfoProc)(unsafe.Pointer(&buf[0]))
	return int(kp.Eproc.Ppid)
}

// syscall6 wraps syscall.Syscall6 for sysctl calls.
func syscall6(name unsafe.Pointer, namelen, oldp, oldlenp uintptr) (uintptr, uintptr, unix.Errno) {
	return unix.Syscall6(
		unix.SYS___SYSCTL,
		uintptr(name), namelen, oldp, oldlenp, 0, 0,
	)
}
