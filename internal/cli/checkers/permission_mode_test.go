package checkers

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/cli"
)

func TestClaudeHelpSupportsAuto(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		help string
		want bool
	}{
		{
			name: "modern CLI with auto choice",
			help: `  --permission-mode <mode>  Permission mode [choices: "plan", "acceptEdits", "auto", "bypassPermissions"]`,
			want: true,
		},
		{
			name: "flag present but no auto value",
			help: `  --permission-mode <mode>  Permission mode [choices: "plan", "acceptEdits", "bypassPermissions"]`,
			want: false,
		},
		{
			name: "auto word without permission-mode flag",
			help: `  --auto-compact  Automatically compact context`,
			want: false,
		},
		{
			name: "empty help",
			help: ``,
			want: false,
		},
		{
			name: "legacy CLI lacking the flag entirely",
			help: `  --dangerously-skip-permissions  Bypass permission prompts`,
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, claudeHelpSupportsAuto(tc.help))
		})
	}
}

func TestClaudeAutoModeCheckerMetadata(t *testing.T) {
	t.Parallel()
	c := claudeAutoModeChecker{}
	require.Equal(t, "worker.claude_auto_mode", c.Name())
	require.Equal(t, "worker", c.Category())
}

func TestClaudeBypassModeChecker(t *testing.T) {
	// This test changes package-global configPath and process environment, so it
	// must remain serial (see withConfigPath in config_test.go).
	checker := registeredChecker("worker.claude_bypass_mode")
	require.NotNil(t, checker, "doctor must register the Claude bypass risk checker")

	tests := []struct {
		name       string
		yaml       string
		configPath string
		noConfig   bool
		env        map[string]string
		wantStatus cli.Status
		wantHint   bool
	}{
		{
			name:       "no configuration skips the risk check",
			noConfig:   true,
			wantStatus: cli.StatusPass,
		},
		{
			name: "YAML workspace defaults pass",
			yaml: `worker:
  default_permission_mode: workspace
  claude_code:
    permission_mode: workspace
messaging:
  feishu:
    enabled: true
    worker_type: claude_code
`,
			wantStatus: cli.StatusPass,
		},
		{
			name: "YAML workspace default bypass is clamped by the operator ceiling",
			yaml: `worker:
  default_permission_mode: bypass
  claude_code:
    permission_mode: workspace
messaging:
  feishu:
    enabled: true
    worker_type: claude_code
`,
			wantStatus: cli.StatusPass,
		},
		{
			name: "YAML bypass defaults warn when the operator does not tighten them",
			yaml: `worker:
  default_permission_mode: bypass
  claude_code:
    permission_mode: bypass
messaging:
  feishu:
    enabled: true
    worker_type: claude_code
`,
			wantStatus: cli.StatusWarn,
			wantHint:   true,
		},
		{
			name: "YAML Claude Code platform default bypass warns",
			yaml: `worker:
  default_permission_mode: workspace
  claude_code:
    permission_mode: bypass
messaging:
  feishu:
    enabled: true
    worker_type: claude_code
`,
			wantStatus: cli.StatusWarn,
			wantHint:   true,
		},
		{
			name: "default permission environment variable is not a runtime override",
			yaml: `worker:
  default_permission_mode: workspace
  claude_code:
    permission_mode: workspace
messaging:
  feishu:
    enabled: true
    worker_type: claude_code
`,
			env: map[string]string{
				"HOTPLEX_WORKER_DEFAULT_PERMISSION_MODE": "bypass",
			},
			wantStatus: cli.StatusPass,
		},
		{
			name: "Claude Code permission environment override warns",
			yaml: `worker:
  default_permission_mode: workspace
  claude_code:
    permission_mode: workspace
messaging:
  feishu:
    enabled: true
    worker_type: claude_code
`,
			env: map[string]string{
				"HOTPLEX_WORKER_CLAUDE_CODE_PERMISSION_MODE": "bypass",
			},
			wantStatus: cli.StatusWarn,
			wantHint:   true,
		},
		{
			name: "non Claude Code Feishu platform does not warn",
			yaml: `worker:
  default_permission_mode: bypass
  claude_code:
    permission_mode: bypass
messaging:
  feishu:
    enabled: true
    worker_type: codex_cli
    bots:
      - name: operations
        app_id: cli_test
        app_secret: secret
        worker_type: acp
`,
			wantStatus: cli.StatusPass,
		},
		{
			name:       "unreadable configuration warns with remediation",
			configPath: filepath.Join(t.TempDir(), "missing.yaml"),
			wantStatus: cli.StatusWarn,
			wantHint:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HOTPLEX_WORKER_DEFAULT_PERMISSION_MODE", "")
			t.Setenv("HOTPLEX_WORKER_CLAUDE_CODE_PERMISSION_MODE", "")
			for key, value := range tt.env {
				t.Setenv(key, value)
			}

			path := tt.configPath
			if path == "" && !tt.noConfig {
				path = filepath.Join(t.TempDir(), "config.yaml")
				require.NoError(t, os.WriteFile(path, []byte(tt.yaml), 0o600))
			}
			withConfigPath(t, path)

			diagnostic := checker.Check(context.Background())
			require.Equal(t, tt.wantStatus, diagnostic.Status, diagnostic.Detail)
			require.Equal(t, "worker.claude_bypass_mode", diagnostic.Name)
			require.Equal(t, "worker", diagnostic.Category)
			require.Equal(t, tt.wantHint, diagnostic.FixHint != "")
			require.Nil(t, diagnostic.FixFunc, "this checker must never modify production configuration")
		})
	}
}

func registeredChecker(name string) cli.Checker {
	for _, checker := range cli.DefaultRegistry.All() {
		if checker.Name() == name {
			return checker
		}
	}
	return nil
}
