package codexcli

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/internal/worker/base"
)

func TestTurnInputItemSkillMarshalsNativeFields(t *testing.T) {
	t.Parallel()

	data, err := json.Marshal([]TurnInputItem{
		{Type: "skill", Name: "oracle-dba", Path: "/workspace/.agents/skills/oracle-dba/SKILL.md"},
		{Type: "text", Text: "$oracle-dba 10.102.78.1"},
	})
	require.NoError(t, err)
	require.JSONEq(t, `[{"type":"skill","name":"oracle-dba","path":"/workspace/.agents/skills/oracle-dba/SKILL.md"},{"type":"text","text":"$oracle-dba 10.102.78.1"}]`, string(data))
}

func TestAppServerWorkerInvokeSkillUsesStructuredTurnInput(t *testing.T) {
	t.Parallel()

	mgr := NewCodexAppServerManager(slog.Default(), config.CodexCLIConfig{
		IdleDrainPeriod: time.Minute,
		CallTimeout:     20 * time.Millisecond,
	})
	var buf strings.Builder
	mgr.stdin = struct {
		io.Writer
		io.Closer
	}{
		Writer: &buf,
		Closer: io.NopCloser(nil),
	}
	mgr.mu.Lock()
	mgr.refs = 1
	mgr.state = stateRunning
	mgr.mu.Unlock()

	w := &AppServerWorker{
		BaseWorker: base.NewBaseWorker(slog.Default(), nil),
		manager:    mgr,
		threadID:   "thr-skill",
	}
	err := w.InvokeSkill(context.Background(), worker.SkillInvocation{
		Name: "oracle-dba",
		Args: "10.102.78.1",
		Path: "/workspace/.agents/skills/oracle-dba/SKILL.md",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "codexcli: turn/start:")
	require.Contains(t, buf.String(), `"type":"skill"`)
	require.Contains(t, buf.String(), `"name":"oracle-dba"`)
	require.Contains(t, buf.String(), `"path":"/workspace/.agents/skills/oracle-dba/SKILL.md"`)
	require.Contains(t, buf.String(), `"text":"$oracle-dba 10.102.78.1"`)
}

func TestAppConnSkillReplayRetainsNativeInvocation(t *testing.T) {
	t.Parallel()

	conn := &appConn{}
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
