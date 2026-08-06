package codexcli

import (
	"context"
	"encoding/json"
	"fmt"
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
	want := worker.NativeCommandInvocation{
		Name: "oracle-dba",
		Args: "10.102.78.1",
		Path: "/workspace/.agents/skills/oracle-dba/SKILL.md",
	}
	conn.setSkillReplay(want)

	got := conn.LastInputReplay()
	require.Equal(t, "/oracle-dba 10.102.78.1", got.Content)
	require.Equal(t, &want, got.Skill)
}

func TestAppConnTextReplayReplacesSkillReplay(t *testing.T) {
	t.Parallel()

	conn := &appConn{}
	conn.setSkillReplay(worker.NativeCommandInvocation{
		Name: "oracle-dba",
		Args: "10.102.78.1",
		Path: "/workspace/.agents/skills/oracle-dba/SKILL.md",
	})
	conn.setTextReplay("thanks")

	got := conn.LastInputReplay()
	require.Equal(t, "thanks", got.Content)
	require.Nil(t, got.Skill, "ordinary text input must clear the native Skill replay")
}

func TestAppServerWorkerInputRefreshesReplayAfterSkill(t *testing.T) {
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

	conn := &appConn{}
	w := &AppServerWorker{
		BaseWorker: base.NewBaseWorker(slog.Default(), nil),
		manager:    mgr,
		threadID:   "thr-replay",
		conn:       conn,
	}

	// A Skill invocation registers the native replay...
	err := w.InvokeSkill(t.Context(), worker.SkillInvocation{
		Name: "oracle-dba",
		Args: "10.102.78.1",
		Path: "/workspace/.agents/skills/oracle-dba/SKILL.md",
	})
	require.Error(t, err) // turn/start times out against the fake stdin
	require.NotNil(t, conn.LastInputReplay().Skill)

	// ...and an ordinary text input must replace it, so crash recovery never
	// re-invokes the stale Skill after the user moved on to plain text.
	err = w.Input(t.Context(), "thanks", nil)
	require.Error(t, err) // turn/start times out against the fake stdin
	got := conn.LastInputReplay()
	require.Equal(t, "thanks", got.Content)
	require.Nil(t, got.Skill)
}

func TestAppServerWorkerListInvokableSkillsParsesNativeResponse(t *testing.T) {
	t.Parallel()

	mgr := NewCodexAppServerManager(slog.Default(), config.CodexCLIConfig{
		IdleDrainPeriod: time.Minute,
		CallTimeout:     500 * time.Millisecond,
	})
	incoming := make(chan string, 4)
	writer := channelWriter{ch: incoming}
	mgr.stdin = struct {
		io.Writer
		io.Closer
	}{
		Writer: writer,
		Closer: io.NopCloser(nil),
	}
	mgr.mu.Lock()
	mgr.refs = 1
	mgr.state = stateRunning
	mgr.mu.Unlock()

	w := &AppServerWorker{
		BaseWorker: base.NewBaseWorker(slog.Default(), nil),
		manager:    mgr,
	}

	type result struct {
		descriptors []worker.SkillDescriptor
		err         error
	}
	done := make(chan result, 1)
	go func() {
		d, err := w.ListInvokableSkills(context.Background(), "/workspace")
		done <- result{descriptors: d, err: err}
	}()

	// Wait for the skills/list request and extract its id so dispatchFrame can
	// correlate the response.
	var req struct {
		ID int64 `json:"id"`
	}
	require.Eventually(t, func() bool {
		select {
		case line := <-incoming:
			dec := json.NewDecoder(strings.NewReader(line))
			if err := dec.Decode(&req); err != nil {
				return false
			}
			return req.ID != 0
		default:
			return false
		}
	}, time.Second, 10*time.Millisecond, "skills/list request never written")

	frame := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":{"data":[{"cwd":"/workspace","errors":[],"skills":[{"name":"oracle-dba","description":"DBA helper","enabled":true,"path":"/workspace/.agents/skills/oracle-dba/SKILL.md","scope":"project"},{"name":"disabled-skill","description":"off","enabled":false,"path":"/x/SKILL.md","scope":"project"}]}]}}`, req.ID)
	mgr.dispatchFrame([]byte(frame))

	select {
	case r := <-done:
		require.NoError(t, r.err)
		require.Len(t, r.descriptors, 1)
		require.Equal(t, worker.SkillDescriptor{
			Name:        "oracle-dba",
			Description: "DBA helper",
			Path:        "/workspace/.agents/skills/oracle-dba/SKILL.md",
		}, r.descriptors[0])
	case <-time.After(2 * time.Second):
		t.Fatal("ListInvokableSkills never returned")
	}
}

func TestAppServerWorkerListInvokableSkillsRequiresRunningManager(t *testing.T) {
	t.Parallel()

	mgr := NewCodexAppServerManager(slog.Default(), config.CodexCLIConfig{
		IdleDrainPeriod: time.Minute,
		CallTimeout:     20 * time.Millisecond,
	})
	w := &AppServerWorker{
		BaseWorker: base.NewBaseWorker(slog.Default(), nil),
		manager:    mgr,
	}
	// manager.state is stateStopped by default.
	_, err := w.ListInvokableSkills(context.Background(), "/workspace")
	require.ErrorContains(t, err, "app-server not running")
}

// channelWriter forwards each Write to a channel so tests can observe the
// app-server request stream without racing a strings.Builder.
type channelWriter struct {
	ch chan string
}

func (w channelWriter) Write(p []byte) (int, error) {
	w.ch <- string(p)
	return len(p), nil
}
