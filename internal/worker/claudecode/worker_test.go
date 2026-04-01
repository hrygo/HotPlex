package claudecode

import (
	"context"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"

	"hotplex-worker/internal/worker"
)

func hasClaudeBinary() bool {
	_, err := exec.LookPath("claude")
	return err == nil
}

func TestClaudeCodeWorker_Capabilities(t *testing.T) {
	t.Parallel()
	w := New()

	require.Equal(t, worker.TypeClaudeCode, w.Type())
	require.True(t, w.SupportsResume())
	require.True(t, w.SupportsStreaming())
	require.True(t, w.SupportsTools())
	require.NotNil(t, w.EnvWhitelist())
	require.Equal(t, ".claude/projects", w.SessionStoreDir())
	require.Zero(t, w.MaxTurns())
	require.Equal(t, []string{"text", "code", "image"}, w.Modalities())
}

func TestClaudeCodeWorker_EnvWhitelist(t *testing.T) {
	t.Parallel()
	w := New()

	wl := w.EnvWhitelist()
	require.Contains(t, wl, "CLAUDE_API_KEY")
	require.Contains(t, wl, "CLAUDE_MODEL")
	require.Contains(t, wl, "CLAUDE_BASE_URL")
	require.Contains(t, wl, "HOME")
	require.Contains(t, wl, "PATH")
}

func TestClaudeCodeWorker_ConnBeforeStart(t *testing.T) {
	t.Parallel()
	w := New()
	require.Nil(t, w.Conn())
}

func TestClaudeCodeWorker_HealthBeforeStart(t *testing.T) {
	t.Parallel()
	w := New()

	h := w.Health()
	require.Equal(t, worker.TypeClaudeCode, h.Type)
	require.False(t, h.Running)
	require.True(t, h.Healthy)
	require.Empty(t, h.SessionID)
}

func TestClaudeCodeWorker_LastIOBeforeStart(t *testing.T) {
	t.Parallel()
	w := New()
	require.True(t, w.LastIO().IsZero())
}

func TestClaudeCodeWorker_TerminateWithoutStart(t *testing.T) {
	t.Parallel()

	w := New()
	ctx := context.Background()

	err := w.Terminate(ctx)
	require.NoError(t, err)
}

func TestClaudeCodeWorker_KillWithoutStart(t *testing.T) {
	t.Parallel()

	w := New()
	err := w.Kill()
	require.NoError(t, err)
}

func TestClaudeCodeWorker_WaitWithoutStart(t *testing.T) {
	t.Parallel()

	w := New()
	_, err := w.Wait()
	require.Error(t, err)
	require.Contains(t, err.Error(), "not started")
}

func TestClaudeCodeWorker_Input_WithoutStart(t *testing.T) {
	t.Parallel()

	w := New()
	ctx := context.Background()
	err := w.Input(ctx, "hello", nil)
	require.Error(t, err)
}

func TestClaudeCodeWorker_Start_WithBinary(t *testing.T) {
	if !hasClaudeBinary() {
		t.Skip("claude binary not found, skipping integration test")
	}

	w := New()
	ctx := context.Background()
	session := worker.SessionInfo{
		SessionID:  "test-session",
		UserID:     "test-user",
		ProjectDir: "/tmp",
	}

	err := w.Start(ctx, session)
	require.NoError(t, err)

	conn := w.Conn()
	require.NotNil(t, conn)
	require.Equal(t, "test-session", conn.SessionID())
	require.Equal(t, "test-user", conn.UserID())

	h := w.Health()
	require.Equal(t, worker.TypeClaudeCode, h.Type)
	require.True(t, h.Running)

	_ = w.Kill()
}

func TestClaudeCodeWorker_Resume_WithBinary(t *testing.T) {
	if !hasClaudeBinary() {
		t.Skip("claude binary not found, skipping integration test")
	}

	w := New()
	ctx := context.Background()
	session := worker.SessionInfo{
		SessionID:  "test-session",
		UserID:     "test-user",
		ProjectDir: "/tmp",
	}

	err := w.Resume(ctx, session)
	require.NoError(t, err)

	conn := w.Conn()
	require.NotNil(t, conn)

	_ = w.Kill()
}

func TestClaudeCodeWorker_DoubleStart(t *testing.T) {
	if !hasClaudeBinary() {
		t.Skip("claude binary not found, skipping integration test")
	}

	w := New()
	ctx := context.Background()
	session := worker.SessionInfo{
		SessionID:  "test-session",
		UserID:     "test-user",
		ProjectDir: "/tmp",
	}

	_ = w.Start(ctx, session)
	err := w.Start(ctx, session)
	require.Error(t, err)
	require.Contains(t, err.Error(), "already started")

	_ = w.Kill()
}
