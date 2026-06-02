package acp

import (
	"log/slog"
	"strings"

	"github.com/hrygo/hotplex/internal/worker/proc"
)

// ─── ACP Stderr Handler —────────────────────────────────────────────────────

// acpStderrHandler implements proc.StderrHandler for ACP-compatible agents.
//
// Optimizations over DefaultStderrHandler:
//   - Folds system prompt echo (12KB+) into a single Debug summary line
//   - Folds XML agent configuration blocks
//   - Folds Python tracebacks into a single Error summary with the exception line
//   - Demotes MCP registration messages to Debug
//   - Demotes stream reconnection messages to Debug
//
// The folding logic uses per-handler mutable state (foldBuf, foldKind).
// Each session gets its own handler instance via ACPStderrHandlerFactory.
type acpStderrHandler struct {
	// Stateful folding — per-handler instance (one per session).
	folding  bool
	foldBuf  []string
	foldKind foldKind
	foldLen  int
}

type foldKind int

const (
	foldNone         foldKind = iota
	foldSystemPrompt          // [SYSTEM INSTRUCTIONS] … [/SYSTEM INSTRUCTIONS]
	foldTraceback             // Traceback (most recent call last): … exception line
	foldXMLConfig             // <agent-configuration> … </agent-configuration>
)

// Safety valve: force emit if a folded block exceeds these limits.
const (
	maxFoldLines = 256
	maxFoldBytes = 32 * 1024
)

// Handle implements proc.StderrHandler.
func (h *acpStderrHandler) Handle(line string) (slog.Level, string) {
	if h.folding {
		if h.isFoldEnd(line) {
			return h.emitFoldSummary()
		}
		h.foldBuf = append(h.foldBuf, line)
		h.foldLen += len(line)
		if len(h.foldBuf) > maxFoldLines || h.foldLen > maxFoldBytes {
			return h.emitFoldSummary()
		}
		return slog.LevelDebug, ""
	}

	if h.isFoldStart(line) {
		h.startFold(line)
		return slog.LevelDebug, ""
	}

	level, msg := proc.ParseLevelPrefix(line)
	return h.adjustLevel(level, msg), msg
}

func (h *acpStderrHandler) isFoldStart(line string) bool {
	return strings.Contains(line, "[SYSTEM INSTRUCTIONS]") ||
		strings.Contains(line, "<agent-configuration>") ||
		strings.HasPrefix(strings.TrimSpace(line), "Traceback (most recent call last):")
}

func (h *acpStderrHandler) isFoldEnd(line string) bool {
	switch h.foldKind {
	case foldSystemPrompt:
		return strings.Contains(line, "[/SYSTEM INSTRUCTIONS]")
	case foldXMLConfig:
		return strings.Contains(line, "</agent-configuration>")
	case foldTraceback:
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			return false // blank lines inside traceback
		}
		isFrame := strings.HasPrefix(trimmed, "File ") || strings.HasPrefix(trimmed, "  ")
		if isFrame || trimmed == "^" || strings.HasPrefix(trimmed, "~") {
			return false
		}
		return true
	default:
		return false
	}
}

func (h *acpStderrHandler) startFold(line string) {
	h.folding = true
	h.foldBuf = []string{line}
	h.foldLen = len(line)
	switch {
	case strings.Contains(line, "[SYSTEM INSTRUCTIONS]"):
		h.foldKind = foldSystemPrompt
	case strings.Contains(line, "<agent-configuration>"):
		h.foldKind = foldXMLConfig
	default:
		h.foldKind = foldTraceback
	}
}

func (h *acpStderrHandler) emitFoldSummary() (slog.Level, string) {
	level := slog.LevelDebug
	var summary string

	switch h.foldKind {
	case foldSystemPrompt:
		summary = "acp: system prompt echoed (suppressed)"
	case foldXMLConfig:
		summary = "acp: agent config echoed (suppressed)"
	case foldTraceback:
		last := h.foldBuf[len(h.foldBuf)-1]
		summary = "acp: traceback: " + strings.TrimSpace(last)
		level = slog.LevelError
	}

	h.folding = false
	h.foldBuf = nil
	h.foldLen = 0
	h.foldKind = foldNone

	return level, summary
}

// adjustLevel applies ACP-specific post-processing to the agent log level.
func (h *acpStderrHandler) adjustLevel(level slog.Level, msg string) slog.Level {
	if strings.Contains(msg, "mcp_tool: MCP server") || strings.Contains(msg, "MCP: registered") {
		return slog.LevelDebug
	}
	if strings.Contains(msg, "reconnecting in") {
		return slog.LevelDebug
	}
	return level
}

// ACPStderrHandlerFactory returns a factory function for acpStderrHandler.
// Each call creates a new handler with fresh folding state, suitable for one session.
func ACPStderrHandlerFactory(_ string) proc.StderrHandler {
	return &acpStderrHandler{}
}
