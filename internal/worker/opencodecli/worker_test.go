package opencodecli

import (
	"context"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hotplex/hotplex-worker/internal/worker"
)

func hasOpenCodeBinary() bool {
	_, err := exec.LookPath("opencode")
	return err == nil
}

func TestOpenCodeCLIWorker_Capabilities(t *testing.T) {
	t.Parallel()
	w := New()

	require.Equal(t, worker.TypeOpenCodeCLI, w.Type())
	require.False(t, w.SupportsResume())
	require.True(t, w.SupportsStreaming())
	require.True(t, w.SupportsTools())
	require.NotNil(t, w.EnvWhitelist())
	require.Empty(t, w.SessionStoreDir())
	require.Zero(t, w.MaxTurns())
	require.Equal(t, []string{"text", "code"}, w.Modalities())
}

func TestOpenCodeCLIWorker_EnvWhitelist(t *testing.T) {
	t.Parallel()
	w := New()

	wl := w.EnvWhitelist()
	require.Contains(t, wl, "OPENAI_API_KEY")
	require.Contains(t, wl, "OPENAI_BASE_URL")
	require.Contains(t, wl, "OPENCODE_API_KEY")
	require.Contains(t, wl, "OPENCODE_BASE_URL")
	require.Contains(t, wl, "HOME")
	require.Contains(t, wl, "PATH")
}

func TestOpenCodeCLIWorker_ConnBeforeStart(t *testing.T) {
	t.Parallel()
	w := New()
	require.Nil(t, w.Conn())
}

func TestOpenCodeCLIWorker_HealthBeforeStart(t *testing.T) {
	t.Parallel()
	w := New()

	h := w.Health()
	require.Equal(t, worker.TypeOpenCodeCLI, h.Type)
	require.False(t, h.Running)
	require.True(t, h.Healthy)
	require.Empty(t, h.SessionID)
}

func TestOpenCodeCLIWorker_LastIOBeforeStart(t *testing.T) {
	t.Parallel()
	w := New()
	require.True(t, w.LastIO().IsZero())
}

func TestOpenCodeCLIWorker_TerminateWithoutStart(t *testing.T) {
	t.Parallel()

	w := New()
	ctx := context.Background()

	err := w.Terminate(ctx)
	require.NoError(t, err)
}

func TestOpenCodeCLIWorker_KillWithoutStart(t *testing.T) {
	t.Parallel()

	w := New()
	err := w.Kill()
	require.NoError(t, err)
}

func TestOpenCodeCLIWorker_WaitWithoutStart(t *testing.T) {
	t.Parallel()

	w := New()
	_, err := w.Wait()
	require.Error(t, err)
	require.Contains(t, err.Error(), "not started")
}

func TestOpenCodeCLIWorker_Resume_NotSupported(t *testing.T) {
	t.Parallel()

	w := New()
	ctx := context.Background()
	session := worker.SessionInfo{
		SessionID:  "test-session",
		UserID:     "test-user",
		ProjectDir: "/tmp",
	}

	err := w.Resume(ctx, session)
	require.Error(t, err)
	require.Contains(t, err.Error(), "resume not supported")
}

func TestOpenCodeCLIWorker_Input_WithoutStart(t *testing.T) {
	t.Parallel()

	w := New()
	ctx := context.Background()
	err := w.Input(ctx, "hello", nil)
	require.Error(t, err)
}

func TestOpenCodeCLIWorker_Start_WithBinary(t *testing.T) {
	if !hasOpenCodeBinary() {
		t.Skip("opencode binary not found, skipping integration test")
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

	_ = w.Kill()
}

func TestOpenCodeCLIWorker_DoubleStart(t *testing.T) {
	if !hasOpenCodeBinary() {
		t.Skip("opencode binary not found, skipping integration test")
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
