package onboard

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"github.com/hrygo/hotplex/internal/cli"
	"github.com/hrygo/hotplex/internal/cli/checkers"
	"github.com/hrygo/hotplex/internal/config"
)

type messagingPlatformConfig struct {
	enabled        bool
	dmPolicy       string
	groupPolicy    string
	requireMention bool
	allowFrom      []string
	credentials    map[string]string
}

type WizardOptions struct {
	ConfigPath        string
	NonInteractive    bool
	Force             bool
	EnableSlack       bool
	EnableFeishu      bool
	SlackAllowFrom    []string
	SlackDMPolicy     string
	SlackGroupPolicy  string
	FeishuAllowFrom   []string
	FeishuDMPolicy    string
	FeishuGroupPolicy string
}

// ExistingConfig holds detected existing configuration state.
type ExistingConfig struct {
	ConfigExists  bool
	EnvExists     bool
	SlackEnabled  bool
	FeishuEnabled bool
	SlackCreds    bool
	FeishuCreds   bool
	ConfigPath    string
	EnvPath       string
}

func (ec *ExistingConfig) HasAny() bool      { return ec.ConfigExists || ec.EnvExists }
func (ec *ExistingConfig) SlackReady() bool  { return ec.SlackEnabled && ec.SlackCreds }
func (ec *ExistingConfig) FeishuReady() bool { return ec.FeishuEnabled && ec.FeishuCreds }

func detectExistingConfig(configPath, envPath string) *ExistingConfig {
	ec := &ExistingConfig{ConfigPath: configPath, EnvPath: envPath}

	if data, err := os.ReadFile(configPath); err == nil {
		ec.ConfigExists = true
		content := string(data)
		ec.SlackEnabled = isPlatformEnabled(content, "slack")
		ec.FeishuEnabled = isPlatformEnabled(content, "feishu")
	}

	if data, err := os.ReadFile(envPath); err == nil {
		ec.EnvExists = true
		content := string(data)
		ec.SlackCreds = hasEnvValue(content, "HOTPLEX_MESSAGING_SLACK_BOT_TOKEN")
		ec.FeishuCreds = hasEnvValue(content, "HOTPLEX_MESSAGING_FEISHU_APP_ID")
	}

	return ec
}

func isPlatformEnabled(yamlContent, platform string) bool {
	markers := []string{
		platform + ":\n  enabled: true",
		platform + ":\n    enabled: true",
	}
	for _, m := range markers {
		if strings.Contains(yamlContent, m) {
			return true
		}
	}
	return false
}

func hasEnvValue(content, key string) bool {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		prefix := key + "="
		if strings.HasPrefix(line, prefix) && len(line) > len(prefix) {
			return true
		}
	}
	return false
}

type WizardResult struct {
	ConfigPath string
	EnvPath    string
	Steps      []StepResult
	Action     string // "keep" or "reconfigure"
}

type StepResult struct {
	Name   string
	Status string
	Detail string
}

func Run(ctx context.Context, opts WizardOptions) (*WizardResult, error) {
	result := &WizardResult{
		ConfigPath: opts.ConfigPath,
		EnvPath:    filepath.Join(filepath.Dir(opts.ConfigPath), ".env"),
	}

	var jwtSecret, adminToken, workerType string
	var slackCfg, feishuCfg messagingPlatformConfig
	var configCreated bool

	displayBanner()

	result.add(stepEnvPreCheck())
	if result.hasFail() {
		return result, fmt.Errorf("environment pre-check failed, resolve errors above before continuing")
	}

	existing := detectExistingConfig(opts.ConfigPath, result.EnvPath)
	if !opts.Force && existing.HasAny() {
		if opts.NonInteractive {
			result.Action = "keep"
			result.add(StepResult{Name: "onboard", Status: "pass", Detail: "kept existing configuration (non-interactive)"})
			result.add(stepVerify(opts.ConfigPath))
			return result, nil
		}
		displayExistingConfig(existing)
		if promptKeepOrReconfigure() {
			result.Action = "keep"
			result.add(StepResult{Name: "onboard", Status: "pass", Detail: "kept existing configuration"})
			result.add(stepVerify(opts.ConfigPath))
			return result, nil
		}
		result.Action = "reconfigure"
		opts.Force = true // overwrite existing config on reconfigure
		fmt.Fprintln(os.Stderr, "  → Reconfiguring...")
	}

	if opts.NonInteractive {
		jwtSecret = GenerateSecret()
		adminToken = GenerateSecret()
		workerType = "claude_code"
		result.add(StepResult{Name: "required_config", Status: "pass", Detail: "auto-generated secrets, worker=claude_code"})
	} else {
		reader := bufio.NewReader(os.Stdin)
		jwtSecret, adminToken, workerType, _ = stepRequiredConfig(reader)
	}

	result.add(stepWorkerDep(workerType))

	if opts.NonInteractive {
		slackCfg = buildPlatformNonInteractive(opts.EnableSlack, opts.SlackDMPolicy, opts.SlackGroupPolicy, opts.SlackAllowFrom)
		feishuCfg = buildPlatformNonInteractive(opts.EnableFeishu, opts.FeishuDMPolicy, opts.FeishuGroupPolicy, opts.FeishuAllowFrom)
		result.add(StepResult{Name: "messaging", Status: "pass", Detail: messagingDetail(slackCfg.enabled, feishuCfg.enabled)})
	} else {
		reader := bufio.NewReader(os.Stdin)
		slackCfg, feishuCfg, _ = stepMessaging(reader, opts)
	}

	tplOpts := ConfigTemplateOptions{
		WorkerType:           workerType,
		SlackEnabled:         slackCfg.enabled,
		SlackDMPolicy:        slackCfg.dmPolicy,
		SlackGroupPolicy:     slackCfg.groupPolicy,
		SlackRequireMention:  toPtr(slackCfg.requireMention),
		SlackAllowFrom:       slackCfg.allowFrom,
		FeishuEnabled:        feishuCfg.enabled,
		FeishuDMPolicy:       feishuCfg.dmPolicy,
		FeishuGroupPolicy:    feishuCfg.groupPolicy,
		FeishuRequireMention: toPtr(feishuCfg.requireMention),
		FeishuAllowFrom:      feishuCfg.allowFrom,
	}

	s5, configCreated := stepConfigGen(opts, tplOpts)
	result.add(s5)
	if s5.Status == "fail" {
		return result, fmt.Errorf("config generation failed: %s", s5.Detail)
	}

	s6 := stepWriteConfig(result.EnvPath, jwtSecret, adminToken, slackCfg, feishuCfg, configCreated, opts)
	result.add(s6)
	if s6.Status == "fail" {
		return result, fmt.Errorf("config write failed: %s", s6.Detail)
	}

	result.add(stepVerify(opts.ConfigPath))
	result.Action = "reconfigure"
	return result, nil
}

func toPtr[T any](v T) *T { return &v }

func (r *WizardResult) add(s StepResult) { r.Steps = append(r.Steps, s) }

func (r *WizardResult) hasFail() bool {
	for _, s := range r.Steps {
		if s.Status == "fail" {
			return true
		}
	}
	return false
}

func messagingDetail(slack, feishu bool) string {
	switch {
	case slack && feishu:
		return "slack+feishu"
	case slack:
		return "slack"
	case feishu:
		return "feishu"
	default:
		return "non-interactive"
	}
}

func buildPlatformNonInteractive(enabled bool, dmPolicy, groupPolicy string, allowFrom []string) messagingPlatformConfig {
	return messagingPlatformConfig{
		enabled:        enabled,
		dmPolicy:       defaultStr(dmPolicy, "allowlist"),
		groupPolicy:    defaultStr(groupPolicy, "allowlist"),
		requireMention: true,
		allowFrom:      allowFrom,
		credentials:    map[string]string{},
	}
}

// ─── Display helpers ─────────────────────────────────────────────────────────

func displayBanner() {
	fmt.Fprintf(os.Stderr, "\n  \033[1mHotPlex Worker Gateway\033[0m — Setup Wizard\n")
	fmt.Fprintln(os.Stderr, "  "+strings.Repeat("─", 45))
	fmt.Fprintln(os.Stderr, "")
}

func displayExistingConfig(ec *ExistingConfig) {
	fmt.Fprintln(os.Stderr, "  \033[1mExisting Configuration Detected\033[0m")
	fmt.Fprintln(os.Stderr, "  "+strings.Repeat("─", 45))
	if ec.ConfigExists {
		fmt.Fprintf(os.Stderr, "    Config: \033[32m%s\033[0m\n", ec.ConfigPath)
		if ec.SlackEnabled {
			s := "\033[32m✓ configured\033[0m"
			if !ec.SlackCreds {
				s = "\033[33m⚠ missing token in .env\033[0m"
			}
			fmt.Fprintf(os.Stderr, "    Slack:  enabled (%s)\n", s)
		}
		if ec.FeishuEnabled {
			s := "\033[32m✓ configured\033[0m"
			if !ec.FeishuCreds {
				s = "\033[33m⚠ missing credentials in .env\033[0m"
			}
			fmt.Fprintf(os.Stderr, "    Feishu: enabled (%s)\n", s)
		}
		if !ec.SlackEnabled && !ec.FeishuEnabled {
			fmt.Fprintln(os.Stderr, "    Platforms: none enabled")
		}
	}
	if ec.EnvExists && !ec.ConfigExists {
		fmt.Fprintf(os.Stderr, "    Env file: \033[33m%s (config file missing)\033[0m\n", ec.EnvPath)
	}
	fmt.Fprintln(os.Stderr, "")
}

// ─── Step 1: Environment pre-check ──────────────────────────────────────────

func stepEnvPreCheck() StepResult {
	ver := runtime.Version()
	goOK := false
	if s, ok := strings.CutPrefix(ver, "go"); ok {
		parts := strings.Split(s, ".")
		if len(parts) >= 2 {
			if minor, err := strconv.Atoi(parts[1]); err == nil {
				goOK = minor >= 26
			}
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

// ─── Step 2: Required config items ──────────────────────────────────────────

func stepRequiredConfig(reader *bufio.Reader) (jwtSecret, adminToken, workerType string, result StepResult) {
	fmt.Fprintln(os.Stderr, "\n── Required Configuration ──")

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

// ─── Step 3: Worker dependency check ────────────────────────────────────────

func stepWorkerDep(workerType string) StepResult {
	binaries := map[string]string{
		"claude_code":     "claude",
		"opencode_server": "opencode",
	}
	if bin, ok := binaries[workerType]; ok {
		if p, err := exec.LookPath(bin); err == nil {
			return StepResult{Name: "worker_dep", Status: "pass", Detail: bin + " binary found: " + p}
		}
		return StepResult{Name: "worker_dep", Status: "pass", Detail: bin + " binary not found in PATH — install before running serve"}
	}
	return StepResult{Name: "worker_dep", Status: "skip", Detail: "worker type " + workerType + " has no binary dependency"}
}

// ─── Step 4: Messaging platform ─────────────────────────────────────────────

func stepMessaging(reader *bufio.Reader, _ WizardOptions) (slackCfg, feishuCfg messagingPlatformConfig, result StepResult) {
	fmt.Fprintln(os.Stderr, "\n── Messaging Platform (optional) ──")

	slackCfg = collectPlatformConfig(reader, "Slack", map[string]string{
		"HOTPLEX_MESSAGING_SLACK_BOT_TOKEN": "Slack Bot Token (xoxb-...)",
		"HOTPLEX_MESSAGING_SLACK_APP_TOKEN": "Slack App Token (xapp-...)",
	})

	feishuCfg = collectPlatformConfig(reader, "Feishu", map[string]string{
		"HOTPLEX_MESSAGING_FEISHU_APP_ID":     "Feishu App ID",
		"HOTPLEX_MESSAGING_FEISHU_APP_SECRET": "Feishu App Secret",
	})

	return slackCfg, feishuCfg, StepResult{Name: "messaging", Status: "pass", Detail: messagingDetail(slackCfg.enabled, feishuCfg.enabled)}
}

func collectPlatformConfig(reader *bufio.Reader, platform string, credPrompts map[string]string) messagingPlatformConfig {
	if !promptYesNo(reader, fmt.Sprintf("Configure %s?", platform)) {
		return messagingPlatformConfig{credentials: map[string]string{}}
	}

	cfg := messagingPlatformConfig{
		enabled:     true,
		credentials: map[string]string{},
	}

	for envKey, promptText := range credPrompts {
		if val := prompt(reader, "  "+promptText); val != "" {
			cfg.credentials[envKey] = val
		}
	}

	fmt.Fprintln(os.Stderr, "\n  ── Access Policy [Enter = accept defaults] ──")
	cfg.dmPolicy = promptWithDefault(reader, "  DM policy", "allowlist")
	cfg.groupPolicy = promptWithDefault(reader, "  Group policy", "allowlist")
	cfg.requireMention = promptYesNo(reader, "  Require @mention in groups?")
	cfg.allowFrom = promptCommaList(reader, fmt.Sprintf("  Allowed users for %s", platform))

	fmt.Fprintf(os.Stderr, "  → %s: dm=%s group=%s mention=%t\n", platform, cfg.dmPolicy, cfg.groupPolicy, cfg.requireMention)
	return cfg
}

// ─── Step 5: Config file generation ─────────────────────────────────────────

func stepConfigGen(opts WizardOptions, tplOpts ConfigTemplateOptions) (StepResult, bool) {
	if _, err := os.Stat(opts.ConfigPath); err == nil && !opts.Force {
		return StepResult{Name: "config_gen", Status: "skip", Detail: "config file already exists (use --force to overwrite)"}, false
	}

	dir := filepath.Dir(opts.ConfigPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return StepResult{Name: "config_gen", Status: "fail", Detail: "create config dir: " + err.Error()}, false
	}

	if err := os.WriteFile(opts.ConfigPath, []byte(BuildConfigYAML(tplOpts)), 0o600); err != nil {
		return StepResult{Name: "config_gen", Status: "fail", Detail: "write config: " + err.Error()}, false
	}
	return StepResult{Name: "config_gen", Status: "pass", Detail: opts.ConfigPath}, true
}

// ─── Step 6: Write config ───────────────────────────────────────────────────

func stepWriteConfig(envPath, jwtSecret, adminToken string, slackCfg, feishuCfg messagingPlatformConfig, configCreated bool, opts WizardOptions) StepResult {
	if err := os.WriteFile(envPath, []byte(buildEnvContent(jwtSecret, adminToken, slackCfg, feishuCfg)), 0o600); err != nil {
		return StepResult{Name: "write_config", Status: "fail", Detail: "write .env: " + err.Error()}
	}

	if configCreated {
		if _, err := config.Load(opts.ConfigPath, config.LoadOptions{}); err != nil {
			return StepResult{Name: "write_config", Status: "fail", Detail: "config parse error: " + err.Error()}
		}
	}

	return StepResult{Name: "write_config", Status: "pass", Detail: envPath}
}

func buildEnvContent(jwtSecret, adminToken string, slackCfg, feishuCfg messagingPlatformConfig) string {
	var b strings.Builder
	b.WriteString("# HotPlex Worker Gateway - Environment Configuration\n# Generated by onboard wizard\n\n")
	b.WriteString("# ── Security ──\n")
	b.WriteString("HOTPLEX_JWT_SECRET=" + jwtSecret + "\n")
	b.WriteString("HOTPLEX_ADMIN_TOKEN_1=" + adminToken + "\n")

	writePlatformEnv := func(name, enabledEnv string, cfg messagingPlatformConfig) {
		if !cfg.enabled {
			return
		}
		fmt.Fprintf(&b, "\n# ── %s ──\n%s=true\n", name, enabledEnv)
		for _, key := range sortedKeys(cfg.credentials) {
			fmt.Fprintf(&b, "%s=%s\n", key, cfg.credentials[key])
		}
	}
	writePlatformEnv("Slack", "HOTPLEX_MESSAGING_SLACK_ENABLED", slackCfg)
	writePlatformEnv("Feishu", "HOTPLEX_MESSAGING_FEISHU_ENABLED", feishuCfg)

	b.WriteByte('\n')
	return b.String()
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// ─── Step 7: Verify ─────────────────────────────────────────────────────────

func stepVerify(configPath string) StepResult {
	checkers.SetConfigPath(configPath)

	var allCheckers []cli.Checker
	allCheckers = append(allCheckers, cli.DefaultRegistry.ByCategory("environment")...)
	allCheckers = append(allCheckers, cli.DefaultRegistry.ByCategory("config")...)

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

func promptKeepOrReconfigure() bool {
	fmt.Fprintf(os.Stderr, "? Keep existing configuration? \033[1m[Y/n]\033[0m: ")
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	line = strings.TrimSpace(strings.ToLower(line))
	return line != "n" && line != "no"
}

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

func promptWithDefault(reader *bufio.Reader, question, def string) string { //nolint:unparam // def default varies by caller context
	fmt.Fprintf(os.Stderr, "? %s [%s]: ", question, def)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return def
	}
	return line
}

func promptCommaList(reader *bufio.Reader, question string) []string {
	fmt.Fprintf(os.Stderr, "? %s (comma-separated, Enter to skip): ", question)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}
	parts := strings.Split(line, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			result = append(result, p)
		}
	}
	return result
}
