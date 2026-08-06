package gateway

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/skills"
	"github.com/hrygo/hotplex/internal/worker"
)

func TestResolveSkillInvocationUsesCatalogPath(t *testing.T) {
	t.Parallel()

	invocation, matched, err := resolveSkillInvocation("/oracle-dba 10.102.78.1", []skills.Skill{
		{Name: "oracle-dba", FilePath: "/workspace/.agents/skills/oracle-dba/SKILL.md"},
	})
	require.NoError(t, err)
	require.True(t, matched)
	require.Equal(t, worker.SkillInvocation{
		Name: "oracle-dba",
		Args: "10.102.78.1",
		Path: "/workspace/.agents/skills/oracle-dba/SKILL.md",
	}, invocation)
}

func TestResolveSkillInvocationLeavesUnknownTextOrdinary(t *testing.T) {
	t.Parallel()

	invocation, matched, err := resolveSkillInvocation("/unknown-do-not-run", []skills.Skill{{Name: "oracle-dba"}})
	require.NoError(t, err)
	require.False(t, matched)
	require.Equal(t, worker.SkillInvocation{}, invocation)
}
