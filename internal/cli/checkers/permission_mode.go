package checkers

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/hrygo/hotplex/internal/cli"
)

// claudeAutoModeChecker verifies the installed Claude Code CLI advertises the
// "auto" value for --permission-mode, which backs the auto-edit workspace
// permission tier (issue #789). Older CLIs lack it and will reject sessions
// started with that tier. This is a capability probe, not a hard gate: missing
// support downgrades to StatusWarn so `doctor` stays non-blocking — other tiers
// (read-only/workspace/bypass) work regardless.
type claudeAutoModeChecker struct{}

func (c claudeAutoModeChecker) Name() string     { return "worker.claude_auto_mode" }
func (c claudeAutoModeChecker) Category() string { return "worker" }

func (c claudeAutoModeChecker) Check(ctx context.Context) cli.Diagnostic {
	claude, err := exec.LookPath("claude")
	if err != nil {
		// workerBinaryChecker already flags a missing binary as Fail; stay Warn here
		// so this checker never hard-blocks `doctor` on its own.
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusWarn,
			Message:  "Claude Code not found on PATH; cannot probe --permission-mode auto support",
			FixHint:  "Install Claude Code (npm install -g @anthropic-ai/claude-code)",
		}
	}

	helpOut, err := probeClaudeHelp(ctx, claude)
	if err != nil {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusWarn,
			Message:  "Failed to probe Claude Code capabilities: " + err.Error(),
			FixHint:  "Ensure 'claude --help' runs; upgrade with npm install -g @anthropic-ai/claude-code@latest",
		}
	}
	if claudeHelpSupportsAuto(helpOut) {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusPass,
			Message:  "Claude Code supports --permission-mode auto (workspace auto-edit tier ready)",
		}
	}
	return cli.Diagnostic{
		Name:     c.Name(),
		Category: c.Category(),
		Status:   cli.StatusWarn,
		Message:  "Claude Code does not advertise --permission-mode auto; the auto-edit tier will fail to start sessions",
		Detail:   "Other tiers (read-only/workspace/bypass) are unaffected. Upgrade to enable auto-edit (issue #789).",
		FixHint:  "npm install -g @anthropic-ai/claude-code@latest",
	}
}

// probeClaudeHelp runs `claude --help` with a bounded timeout and returns its
// combined stdout/stderr. The help text lists supported --permission-mode values.
func probeClaudeHelp(ctx context.Context, claude string) (string, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, claude, "--help")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// claudeHelpSupportsAuto reports whether Claude Code --help output advertises
// the "auto" value for --permission-mode. Detection is a two-substring heuristic
// (capability probe beats brittle version parsing): the --permission-mode flag
// must be present AND "auto" must appear alongside it. Warn-level tolerance makes
// a rare false positive (e.g. "automatically" with no auto mode) non-fatal.
func claudeHelpSupportsAuto(helpText string) bool {
	return strings.Contains(helpText, "permission-mode") && strings.Contains(helpText, "auto")
}

func init() {
	cli.DefaultRegistry.Register(claudeAutoModeChecker{})
}
