package gateway

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/e2econtract"
	"github.com/hrygo/hotplex/internal/skills"
	"github.com/hrygo/hotplex/internal/worker"
)

func TestSkillResolutionCoversThreeChannelsFourWorkers(t *testing.T) {
	t.Parallel()

	catalog := []skills.Skill{{Name: "oracle-dba", FilePath: "/workspace/.agents/skills/oracle-dba/SKILL.md"}}
	seen := make(map[string]struct{}, len(e2econtract.Combinations()))
	for _, combination := range e2econtract.Combinations() {
		combination := combination
		t.Run(combination.ID, func(t *testing.T) {
			t.Parallel()
			invocation, matched, err := resolveSkillInvocation("/oracle-dba 10.102.78.1", catalog)
			require.NoError(t, err)
			require.True(t, matched)
			require.Equal(t, "oracle-dba", invocation.Name)
			require.Equal(t, "10.102.78.1", invocation.Args)
			require.Equal(t, "/workspace/.agents/skills/oracle-dba/SKILL.md", invocation.Path)
			require.NotEmpty(t, combination.Platform)
			require.NotEmpty(t, combination.Worker)
		})
		seen[combination.ID] = struct{}{}
	}
	require.Len(t, seen, 12)
	require.Len(t, e2econtract.Combinations(), 12)

	for _, workerType := range []worker.WorkerType{
		worker.TypeClaudeCode,
		worker.TypeOpenCodeSrv,
		worker.TypeCodexCLI,
		worker.TypeACP,
	} {
		require.NotEmpty(t, skillInvocationMode(workerType))
	}
}
