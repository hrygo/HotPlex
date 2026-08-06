package claudecode

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/worker"
)

func TestSkillCommandTextUsesCanonicalSlashSyntax(t *testing.T) {
	t.Parallel()

	require.Equal(t, "/oracle-dba", skillCommandText(worker.SkillInvocation{Name: "oracle-dba"}))
	require.Equal(t, "/oracle-dba 10.102.78.1", skillCommandText(worker.SkillInvocation{
		Name: "oracle-dba",
		Args: "10.102.78.1",
	}))
}
