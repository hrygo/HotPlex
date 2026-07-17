package checkers

import (
	"context"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/hrygo/hotplex/internal/cli"
	"github.com/hrygo/hotplex/internal/config"
)

// classifyExecutable reports whether resolved is a native binary or a wrapper
// script. A Windows .cmd/.bat shim (or POSIX .sh) can remap the child
// process's native exit status to a generic code, masking the original
// 0xC0000142-class failure — flagging wrappers is central to issue #900.
func classifyExecutable(resolved string) string {
	ext := strings.ToLower(filepath.Ext(resolved))
	switch ext {
	case "", ".exe":
		return "native"
	case ".cmd", ".bat", ".ps1", ".sh":
		return "wrapper"
	default:
		return "unknown"
	}
}

// opencodeResolveChecker inspects the OpenCode Server worker command beyond a
// bare PATH lookup. It reports the resolved executable, whether it is a native
// binary or a wrapper script, and whether `opencode --version` succeeds within
// a bounded timeout. On failure it emits platform-aware evidence-collection
// hints so 0xC0000142-class startup failures become diagnosable. See #900.
type opencodeResolveChecker struct{}

func (c opencodeResolveChecker) Name() string     { return "dependencies.opencode_server_resolve" }
func (c opencodeResolveChecker) Category() string { return "dependencies" }

func (c opencodeResolveChecker) Check(ctx context.Context) cli.Diagnostic {
	cfg, err := loadConfig()
	if err != nil {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusFail,
			Message:  "Failed to load config for OCS resolve check",
			Detail:   err.Error(),
		}
	}
	if cfg == nil {
		cfg = config.Default()
	}

	cmdStr := cfg.Worker.OpenCodeServer.Command
	if cmdStr == "" {
		cmdStr = "opencode"
	}
	parts := strings.Fields(cmdStr)
	binary := "opencode"
	if len(parts) > 0 {
		binary = parts[0]
	}

	resolved, lookErr := exec.LookPath(binary)
	if lookErr != nil {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusFail,
			Message:  "OpenCode Server command not found in PATH: " + binary,
			Detail:   lookErr.Error(),
			FixHint:  ocsResolveFixHint(runtime.GOOS == "windows"),
		}
	}

	kind := classifyExecutable(resolved)
	versionOK, versionOut := probeVersion(ctx, binary)

	var sb strings.Builder
	sb.WriteString("resolved=" + resolved + " (" + kind + ")")
	if versionOK {
		sb.WriteString("; version OK")
	} else {
		sb.WriteString("; version probe FAILED")
	}

	diag := cli.Diagnostic{
		Name:     c.Name(),
		Category: c.Category(),
		Status:   cli.StatusPass,
		Message:  sb.String(),
	}
	if !versionOK {
		diag.Status = cli.StatusWarn
		out := versionOut
		if out == "" {
			out = "(no output)"
		}
		diag.Detail = "version probe output: " + out
		diag.FixHint = ocsResolveFixHint(runtime.GOOS == "windows")
	}
	if kind == "wrapper" {
		// Wrappers are permitted but can mask the child's native exit code.
		diag.Status = cli.StatusWarn
		diag.Detail = "OCS command resolves to a wrapper script (" + resolved +
			"). Wrappers can remap the child process's native exit code (e.g. 0xC0000142) to a generic value."
		if diag.FixHint == "" {
			diag.FixHint = "If startup fails, run `opencode serve --port <unused>` directly under the service account to capture the raw exit code."
		}
	}
	return diag
}

// probeVersion runs `<binary> --version` with a 5s timeout and reports success
// plus a trimmed snippet of combined output. This mirrors the real spawn path:
// if the binary DLL-initializes a child that exits with 0xC0000142, the probe
// fails the same way.
func probeVersion(ctx context.Context, binary string) (bool, string) {
	vctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(vctx, binary, "--version").CombinedOutput()
	if vctx.Err() == context.DeadlineExceeded {
		return false, "timeout after 5s"
	}
	if err != nil {
		return false, strings.TrimSpace(string(out))
	}
	return true, strings.TrimSpace(string(out))
}

// ocsResolveFixHint returns platform-aware evidence-collection guidance for an
// OCS startup failure. On Windows it points to Event Viewer's faulting module,
// ProcMon, and service-account reproduction — the only sources that can name
// the DLL behind a 0xC0000142.
func ocsResolveFixHint(windows bool) string {
	if windows {
		return "Collect: Event Viewer (Application Error / WER faulting module), " +
			"ProcMon DLL-load trace for opencode.exe, and run " +
			"`opencode serve --port <unused>` under the HotPlex service account to " +
			"read $LASTEXITCODE. Do NOT disable EDR to verify — use a controlled allowlist."
	}
	return "Run `opencode serve --port <unused>` under the HotPlex service account " +
		"to capture stdout, stderr and the exit code."
}

func init() {
	cli.DefaultRegistry.Register(opencodeResolveChecker{})
}
