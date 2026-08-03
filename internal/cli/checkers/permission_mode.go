package checkers

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/hrygo/hotplex/internal/cli"
	"github.com/hrygo/hotplex/internal/config"
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

// claudeBypassModeChecker reports the risk of a configured Feishu Claude Code
// worker running with bypass permissions. It is intentionally read-only: a
// permission change needs an operator decision and a restart.
type claudeBypassModeChecker struct{}

func (c claudeBypassModeChecker) Name() string     { return "worker.claude_bypass_mode" }
func (c claudeBypassModeChecker) Category() string { return "worker" }

func (c claudeBypassModeChecker) Check(ctx context.Context) cli.Diagnostic {
	cfg, err := loadConfig()
	if cfg == nil && err == nil {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusPass,
			Message:  "Config path not set; skipping Claude Code bypass permission check",
		}
	}
	if err != nil {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusWarn,
			Message:  "Cannot load config for Claude Code bypass permission check",
			Detail:   err.Error(),
			FixHint:  "Fix the configuration file, then run hotplex doctor again",
		}
	}

	if !hasFeishuClaudeCodeWorker(cfg) {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusPass,
			Message:  "No Claude Code Feishu worker configured",
		}
	}

	workspaceMode := resolveClaudePermissionMode(cfg.Worker.DefaultPermissionMode, cfg.Worker.ClaudeCode.PermissionMode)
	platformMode := resolveClaudePermissionMode("", cfg.Worker.ClaudeCode.PermissionMode)
	if workspaceMode == permissionModeBypass || platformMode == permissionModeBypass {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusWarn,
			Message:  "Claude Code bypass permission mode is active for Feishu",
			Detail:   "Bypass skips normal Claude Code permission prompts for at least one effective Feishu session mode.",
			FixHint:  "Set worker.claude_code.permission_mode and worker.default_permission_mode to workspace or read-only, then restart HotPlex.",
		}
	}

	return cli.Diagnostic{
		Name:     c.Name(),
		Category: c.Category(),
		Status:   cli.StatusPass,
		Message:  "Configured Feishu Claude Code workers use restricted permission modes",
	}
}

const (
	permissionModeReadOnly  = "read-only"
	permissionModeWorkspace = "workspace"
	permissionModeAutoEdit  = "auto-edit"
	permissionModeBypass    = "bypass"
)

// resolveClaudePermissionMode mirrors Claude Code's runtime resolution: a
// platform session has no session mode and therefore uses the operator mode;
// an explicit workspace default is clamped to that operator ceiling. Empty
// operator mode preserves the legacy Claude Code bypass default.
func resolveClaudePermissionMode(sessionMode, operatorMode string) string {
	effectiveMode := sessionMode
	if effectiveMode == "" {
		effectiveMode = operatorMode
	}
	if operatorMode != "" && permissionModeRank(effectiveMode) > permissionModeRank(operatorMode) {
		effectiveMode = operatorMode
	}
	if effectiveMode == "" {
		return permissionModeBypass
	}
	return effectiveMode
}

func permissionModeRank(mode string) int {
	switch mode {
	case permissionModeReadOnly:
		return 0
	case permissionModeWorkspace:
		return 1
	case permissionModeAutoEdit:
		return 2
	case permissionModeBypass:
		return 3
	default:
		return 0
	}
}

func hasFeishuClaudeCodeWorker(cfg *config.Config) bool {
	if !cfg.Messaging.Feishu.Enabled {
		return false
	}
	if len(cfg.Messaging.Feishu.Bots) == 0 {
		return cfg.ResolveWorkerType("feishu", "") == config.DefaultWorkerType
	}
	for _, bot := range cfg.Messaging.Feishu.Bots {
		if cfg.ResolveWorkerType("feishu", bot.Name) == config.DefaultWorkerType {
			return true
		}
	}
	return false
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
	cli.DefaultRegistry.Register(claudeBypassModeChecker{})
}
