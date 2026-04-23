package onboard

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"github.com/hotplex/hotplex-worker/internal/cli"
	"github.com/hotplex/hotplex-worker/internal/cli/checkers"
	"github.com/hotplex/hotplex-worker/internal/config"
)

type WizardOptions struct {
	ConfigPath     string
	NonInteractive bool
	Force          bool
}

type WizardResult struct {
	ConfigPath string
	EnvPath    string
	Steps      []StepResult
}

type StepResult struct {
	Name   string
	Status string // "pass", "skip", "fail"
	Detail string
}

func Run(ctx context.Context, opts WizardOptions) (*WizardResult, error) {
	result := &WizardResult{
		ConfigPath: opts.ConfigPath,
		EnvPath:    ".env",
	}

	var jwtSecret, adminToken, workerType string

	// Step 1: Environment pre-check
	result.add(stepEnvPreCheck())

	if result.hasFail() {
		return result, fmt.Errorf("environment pre-check failed, resolve errors above before continuing")
	}

	// Step 2: Config file generation
	s2, configCreated := stepConfigGen(opts)
	result.add(s2)
	if s2.Status == "fail" {
		return result, fmt.Errorf("config generation failed: %s", s2.Detail)
	}

	// Step 3: Required config items
	if opts.NonInteractive {
		jwtSecret = GenerateSecret()
		adminToken = GenerateSecret()
		workerType = "claude_code"
		result.add(StepResult{Name: "required_config", Status: "pass", Detail: "auto-generated secrets, worker=claude_code"})
	} else {
		reader := bufio.NewReader(os.Stdin)
		var s3 StepResult
		jwtSecret, adminToken, workerType, s3 = stepRequiredConfig(reader)
		result.add(s3)
	}

	// Step 4: Worker dependency check
	result.add(stepWorkerDep(workerType))

	// Step 5: Messaging platform (optional)
	var slackVars, feishuVars map[string]string
	if opts.NonInteractive {
		result.add(StepResult{Name: "messaging", Status: "skip", Detail: "non-interactive mode"})
	} else {
		reader := bufio.NewReader(os.Stdin)
		var s5 StepResult
		slackVars, feishuVars, s5 = stepMessaging(reader)
		result.add(s5)
	}

	// Step 6: Write config
	s6 := stepWriteConfig(result.EnvPath, jwtSecret, adminToken, workerType, slackVars, feishuVars, configCreated, opts)
	result.add(s6)
	if s6.Status == "fail" {
		return result, fmt.Errorf("config write failed: %s", s6.Detail)
	}

	// Step 7: Verify
	result.add(stepVerify(opts.ConfigPath))

	return result, nil
}

func (r *WizardResult) add(s StepResult) {
	r.Steps = append(r.Steps, s)
}

func (r *WizardResult) hasFail() bool {
	for _, s := range r.Steps {
		if s.Status == "fail" {
			return true
		}
	}
	return false
}

// ─── Step 1: Environment pre-check ──────────────────────────────────────────

func stepEnvPreCheck() StepResult {
	ver := runtime.Version()
	goOK := false
	if strings.HasPrefix(ver, "go") {
		numStr := strings.TrimPrefix(ver, "go")
		if dotIdx := strings.Index(numStr, "."); dotIdx > 0 {
			numStr = numStr[:dotIdx]
		}
		if n, err := strconv.Atoi(numStr); err == nil {
			goOK = n >= 26
		}
	}
	if !goOK {
		return StepResult{Name: "env_precheck", Status: "fail", Detail: fmt.Sprintf("Go version %s does not meet requirement (>= go1.26)", ver)}
	}

	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		return StepResult{Name: "env_precheck", Status: "fail", Detail: fmt.Sprintf("OS %s is not supported (need darwin or linux)", runtime.GOOS)}
	}

	var stat syscall.Statfs_t
	if err := syscall.Statfs(".", &stat); err != nil {
		return StepResult{Name: "env_precheck", Status: "fail", Detail: "cannot check disk space: " + err.Error()}
	}
	freeMB := stat.Bavail * uint64(stat.Bsize) / 1024 / 1024
	if freeMB < 100 {
		return StepResult{Name: "env_precheck", Status: "fail", Detail: fmt.Sprintf("insufficient disk space: %d MB (need >= 100 MB)", freeMB)}
	}

	return StepResult{Name: "env_precheck", Status: "pass", Detail: fmt.Sprintf("Go %s, %s/%s, %d MB free", ver, runtime.GOOS, runtime.GOARCH, freeMB)}
}

// ─── Step 2: Config file generation ─────────────────────────────────────────

func stepConfigGen(opts WizardOptions) (StepResult, bool) {
	created := false
	_, err := os.Stat(opts.ConfigPath)
	if err == nil {
		if !opts.Force {
			return StepResult{Name: "config_gen", Status: "skip", Detail: "config file already exists (use --force to overwrite)"}, false
		}
		// Force: overwrite
	}

	dir := filepath.Dir(opts.ConfigPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return StepResult{Name: "config_gen", Status: "fail", Detail: "create config dir: " + err.Error()}, false
	}

	if err := os.WriteFile(opts.ConfigPath, []byte(DefaultConfigYAML()), 0o600); err != nil {
		return StepResult{Name: "config_gen", Status: "fail", Detail: "write config: " + err.Error()}, false
	}
	created = true
	return StepResult{Name: "config_gen", Status: "pass", Detail: opts.ConfigPath}, created
}

// ─── Step 3: Required config items ──────────────────────────────────────────

func stepRequiredConfig(reader *bufio.Reader) (jwtSecret, adminToken, workerType string, result StepResult) {
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "── Required Configuration ──")

	jwtSecret = prompt(reader, "JWT secret (enter to auto-generate)")
	if jwtSecret == "" {
		jwtSecret = GenerateSecret()
		fmt.Fprintln(os.Stderr, "  → Generated JWT secret")
	}

	adminToken = prompt(reader, "Admin token (enter to auto-generate)")
	if adminToken == "" {
		adminToken = GenerateSecret()
		fmt.Fprintln(os.Stderr, "  → Generated admin token")
	}

	workerType = promptChoice(reader, "Worker type", []string{"claude_code", "opencode_server", "pi"})

	return jwtSecret, adminToken, workerType, StepResult{Name: "required_config", Status: "pass", Detail: "worker=" + workerType}
}

// ─── Step 4: Worker dependency check ────────────────────────────────────────

func stepWorkerDep(workerType string) StepResult {
	switch workerType {
	case "claude_code":
		if p, err := exec.LookPath("claude"); err == nil {
			return StepResult{Name: "worker_dep", Status: "pass", Detail: "claude binary found: " + p}
		}
		return StepResult{Name: "worker_dep", Status: "pass", Detail: "claude binary not found in PATH — install before running serve"}
	case "opencode_server":
		if p, err := exec.LookPath("opencode"); err == nil {
			return StepResult{Name: "worker_dep", Status: "pass", Detail: "opencode binary found: " + p}
		}
		return StepResult{Name: "worker_dep", Status: "pass", Detail: "opencode binary not found in PATH — install before running serve"}
	default:
		return StepResult{Name: "worker_dep", Status: "skip", Detail: "worker type " + workerType + " has no binary dependency"}
	}
}

// ─── Step 5: Messaging platform ─────────────────────────────────────────────

func stepMessaging(reader *bufio.Reader) (slackVars, feishuVars map[string]string, result StepResult) {
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "── Messaging Platform (optional) ──")

	if promptYesNo(reader, "Configure Slack?") {
		slackVars = map[string]string{
			"SLACK_BOT_TOKEN": prompt(reader, "  Slack Bot Token (xoxb-...)"),
			"SLACK_APP_TOKEN": prompt(reader, "  Slack App Token (xapp-...)"),
		}
	}

	if promptYesNo(reader, "Configure Feishu?") {
		feishuVars = map[string]string{
			"FEISHU_APP_ID":     prompt(reader, "  Feishu App ID"),
			"FEISHU_APP_SECRET": prompt(reader, "  Feishu App Secret"),
		}
	}

	detail := "none"
	if len(slackVars) > 0 && len(feishuVars) > 0 {
		detail = "slack+feishu"
	} else if len(slackVars) > 0 {
		detail = "slack"
	} else if len(feishuVars) > 0 {
		detail = "feishu"
	}
	return slackVars, feishuVars, StepResult{Name: "messaging", Status: "pass", Detail: detail}
}

// ─── Step 6: Write config ───────────────────────────────────────────────────

func stepWriteConfig(envPath, jwtSecret, adminToken, workerType string, slackVars, feishuVars map[string]string, configCreated bool, opts WizardOptions) StepResult {
	// When config wasn't generated/overwritten, skip config write but still write .env.

	envContent := buildEnvContent(jwtSecret, adminToken, workerType, slackVars, feishuVars)
	if err := os.WriteFile(envPath, []byte(envContent), 0o600); err != nil {
		return StepResult{Name: "write_config", Status: "fail", Detail: "write .env: " + err.Error()}
	}

	// If config was generated (step 2), verify it parses.
	if configCreated {
		if _, err := config.Load(opts.ConfigPath, config.LoadOptions{}); err != nil {
			return StepResult{Name: "write_config", Status: "fail", Detail: "config parse error: " + err.Error()}
		}
	}

	return StepResult{Name: "write_config", Status: "pass", Detail: envPath}
}

func buildEnvContent(jwtSecret, adminToken, workerType string, slackVars, feishuVars map[string]string) string {
	var b strings.Builder
	b.WriteString("# HotPlex Worker Gateway - Environment Configuration\n")
	b.WriteString("# Generated by onboard wizard\n\n")
	b.WriteString("HOTPLEX_JWT_SECRET=" + jwtSecret + "\n")
	b.WriteString("HOTPLEX_ADMIN_TOKEN_1=" + adminToken + "\n")

	if workerType != "" {
		b.WriteString("\n# Worker type\n")
		b.WriteString("HOTPLEX_WORKER_TYPE=" + workerType + "\n")
	}

	if len(slackVars) > 0 {
		b.WriteString("\n# Slack\n")
		for k, v := range slackVars {
			b.WriteString(k + "=" + v + "\n")
		}
	}

	if len(feishuVars) > 0 {
		b.WriteString("\n# Feishu\n")
		for k, v := range feishuVars {
			b.WriteString(k + "=" + v + "\n")
		}
	}

	b.WriteString("\n")
	return b.String()
}

// ─── Step 7: Verify ─────────────────────────────────────────────────────────

func stepVerify(configPath string) StepResult {
	checkers.SetConfigPath(configPath)

	envCheckers := cli.DefaultRegistry.ByCategory("environment")
	configCheckers := cli.DefaultRegistry.ByCategory("config")

	var allCheckers []cli.Checker
	allCheckers = append(allCheckers, envCheckers...)
	allCheckers = append(allCheckers, configCheckers...)

	var passCount, failCount int
	var details []string
	for _, c := range allCheckers {
		d := c.Check(context.Background())
		switch d.Status {
		case cli.StatusPass:
			passCount++
		case cli.StatusFail:
			failCount++
			details = append(details, d.Name+": "+d.Message)
		}
	}

	if failCount > 0 {
		return StepResult{Name: "verify", Status: "fail", Detail: strings.Join(details, "; ")}
	}
	return StepResult{Name: "verify", Status: "pass", Detail: fmt.Sprintf("%d checks passed", passCount)}
}

// ─── Prompt helpers ─────────────────────────────────────────────────────────

func prompt(reader *bufio.Reader, question string) string {
	fmt.Fprintf(os.Stderr, "? %s: ", question)
	line, _ := reader.ReadString('\n')
	return strings.TrimSpace(line)
}

func promptChoice(reader *bufio.Reader, question string, choices []string) string {
	fmt.Fprintf(os.Stderr, "? %s:\n", question)
	for i, c := range choices {
		fmt.Fprintf(os.Stderr, "  %d) %s\n", i+1, c)
	}
	fmt.Fprintf(os.Stderr, "  Select [1]: ")
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return choices[0]
	}
	idx, err := strconv.Atoi(line)
	if err != nil || idx < 1 || idx > len(choices) {
		return choices[0]
	}
	return choices[idx-1]
}

func promptYesNo(reader *bufio.Reader, question string) bool {
	fmt.Fprintf(os.Stderr, "? %s [y/N]: ", question)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "y" || line == "yes"
}
