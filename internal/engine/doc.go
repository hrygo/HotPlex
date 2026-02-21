// Package engine provides the core session management and process pool for HotPlex.
//
// The engine package implements the Hot-Multiplexing pattern, maintaining persistent
// CLI agent processes that can be reused across multiple execution turns. This eliminates
// the cold-start latency of spawning heavy Node.js processes for each request.
//
// Key components:
//   - SessionPool: Thread-safe process pool with idle GC
//   - Session: Individual CLI process wrapper with full-duplex I/O
//   - SessionManager: Interface for process lifecycle management
package engine
