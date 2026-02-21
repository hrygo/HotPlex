// Package sys provides cross-platform process management utilities.
//
// This package handles low-level OS-specific operations for process group
// management, signal handling, and process lifecycle control. It abstracts
// the differences between Unix (PGID-based) and Windows (taskkill-based)
// process termination strategies.
//
// The primary purpose is to ensure proper cleanup of CLI processes and their
// children, preventing zombie processes and resource leaks.
package sys
