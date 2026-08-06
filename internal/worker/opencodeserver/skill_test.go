package opencodeserver

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/worker"
)

func TestConnSkillReplayRetainsNativeInvocation(t *testing.T) {
	t.Parallel()

	conn := &conn{}
	want := worker.SkillInvocation{
		Name: "oracle-dba",
		Args: "10.102.78.1",
		Path: "/workspace/.agents/skills/oracle-dba/SKILL.md",
	}
	conn.setSkillReplay(want)

	got := conn.LastInputReplay()
	require.Equal(t, "/oracle-dba 10.102.78.1", got.Content)
	require.Equal(t, &want, got.Skill)
}
