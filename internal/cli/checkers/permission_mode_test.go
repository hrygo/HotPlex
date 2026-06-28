package checkers

import (
	"testing"

	"github.com/stretchr/testify/require"
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
