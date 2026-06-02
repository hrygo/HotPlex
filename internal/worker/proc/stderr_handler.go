package proc

import (
	"log/slog"
	"strings"
)

// StderrHandler defines how a worker type processes the subprocess's stderr output.
//
// Each handler is responsible for:
//  1. Identifying the log level from the agent's raw stderr line
//  2. Optionally suppressing or folding low-signal content (e.g. system prompt echo)
//  3. Returning a cleaned message (empty string = suppress this line entirely)
//
// Implementations must be concurrency-safe: drainStderr calls Handle from a single
// goroutine per session, but different sessions may share the same handler instance.
//
// For per-session state (e.g. multi-line folding), use a StderrHandlerFactory to
// return a new handler instance for each session.
type StderrHandler interface {
	// Handle processes one line of stderr output.
	// Returns the slog.Level to log at and the processed message.
	// If msg is empty, the line is completely discarded.
	Handle(line string) (level slog.Level, msg string)
}

// StderrHandlerFactory is a factory function per worker type.
// It is called on every Start() invocation, allowing handlers with
// session-specific state (like multi-line folding buffers) to be created.
type StderrHandlerFactory func(sessionID string) StderrHandler

// DefaultStderrHandler provides safe fallback behavior for workers that don't
// provide a custom stderr handler.
//
// Behavior:
//   - Parses [LEVEL] prefixes → maps to slog.Level
//   - [ERROR] → Error, [WARN]/[WARNING] → Warn, [DEBUG] → Debug, [INFO] → Debug
//   - Unmarked lines default to Debug
//   - No content suppression (no folding of system prompts, tracebacks, etc.)
//
// This preserves the existing behavior of all non-ACP workers — stderr is still
// fully logged, just at more appropriate levels instead of always-Info.
type DefaultStderrHandler struct{}

// Handle implements StderrHandler.
func (h *DefaultStderrHandler) Handle(line string) (slog.Level, string) {
	return ParseLevelPrefix(line)
}

// ParseLevelPrefix extracts the log level from a stderr line and returns the
// appropriate slog.Level with the timestamp and level marker stripped.
//
// Recognized patterns:
//
//	[ERROR] ...  → slog.LevelError,  "..."
//	[WARN] ...   → slog.LevelWarn,   "..."
//	[WARNING] ... → slog.LevelWarn,  "..."
//	[DEBUG] ...  → slog.LevelDebug,  "..."
//	[INFO] ...   → slog.LevelDebug,  "..." (agent INFO = noisy, demote to Debug)
//	(unmarked)   → slog.LevelDebug, line (unchanged)
//
// The returned message has the leading timestamp and level marker removed so the
// outer log entry (which already carries its own timestamp and the mapped level)
// does not contain redundant information.
func ParseLevelPrefix(line string) (slog.Level, string) {
	level := slog.LevelDebug
	marker := ""
	switch {
	case containsLevelMarker(line, "[ERROR]"):
		level, marker = slog.LevelError, "[ERROR]"
	case containsLevelMarker(line, "[WARN]"), containsLevelMarker(line, "[WARNING]"):
		level, marker = slog.LevelWarn, "[WARN]"
	case containsLevelMarker(line, "[DEBUG]"):
		level, marker = slog.LevelDebug, "[DEBUG]"
	case containsLevelMarker(line, "[INFO]"):
		level, marker = slog.LevelDebug, "[INFO]"
	}

	if marker == "" {
		return slog.LevelDebug, line
	}

	msg := stripLevelPrefix(line, marker)
	return level, msg
}

// stripLevelPrefix removes the timestamp and level marker from a log line.
// Handles patterns like:
//
//	2026-06-02 09:21:24 [INFO] actual message
//	2026/06/02 09:21:24 [INFO] actual message
func stripLevelPrefix(line, marker string) string {
	idx := strings.Index(line, marker)
	if idx < 0 {
		return line
	}
	// Skip past the marker and any trailing space.
	rest := line[idx+len(marker):]
	rest = strings.TrimLeft(rest, " \t")
	return rest
}

// containsLevelMarker checks if a line contains a bracketed log level marker
// at a position that looks like a real log prefix (start of line or after timestamp).
func containsLevelMarker(line, marker string) bool {
	idx := strings.Index(line, marker)
	if idx < 0 {
		return false
	}
	// Allow white space before the marker (e.g. "2026-06-02 08:45:20 [INFO] ...")
	prefix := line[:idx]
	if prefix == "" {
		return true
	}
	// Only match if the prefix consists of whitespace or timestamp-like characters
	// (digits, hyphens, colons, spaces, dots). This avoids false matches where
	// [ERROR] appears inside a message body.
	for _, c := range prefix {
		if c != ' ' && c != '\t' && c != '-' && c != ':' && c != '.' &&
			(c < '0' || c > '9') {
			return false
		}
	}
	return true
}
