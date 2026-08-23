package onboard

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultTemplatesContainOnlyScopedOperationalGuidance(t *testing.T) {
	t.Parallel()

	byName := make(map[string]string)
	for _, template := range DefaultTemplates() {
		byName[template.Name] = template.Content
	}
	require.Len(t, byName, 5)

	joined := strings.Join([]string{
		byName["SOUL.md"],
		byName["AGENTS.md"],
		byName["TOOLS.md"],
		byName["USER.md"],
		byName["MEMORY.md"],
	}, "\n")
	for _, forbidden := range []string{
		"非交互式后台运行",
		"权限请求 5 分钟",
		"最多 3 次",
		"Git commit / branch / merge",
		"安装任务所需依赖",
		"auto-managed by the agent",
		"真实 Agent Skills",
	} {
		require.NotContains(t, joined, forbidden)
	}

	require.Contains(t, byName["SOUL.md"], "以当前运行时")
	require.Contains(t, byName["AGENTS.md"], "当前请求")
	require.Contains(t, byName["AGENTS.md"], "验证")

	tools := byName["TOOLS.md"]
	for _, anchor := range []string{
		"/help", "/stop", "/reset", "/new", "/gc", "/park", "/cd", "/skills", "/mcp", "/worker",
		"hotplex-cli", "lark-cli", "Cron", "cron get", "STT", "TTS", "新 Session",
	} {
		require.Contains(t, tools, anchor, "TOOLS.md missing operational anchor %q", anchor)
	}
	require.NotContains(t, tools, "完整命令清单")
	require.NotContains(t, tools, "声明某个工具一定存在")

	require.NotContains(t, byName["USER.md"], "hotplex-setup")
	require.NotContains(t, byName["USER.md"], "配置层级")
	require.Contains(t, byName["MEMORY.md"], "只读")
	require.Contains(t, byName["MEMORY.md"], "授权")
}
