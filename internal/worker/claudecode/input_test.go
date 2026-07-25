package claudecode

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/worker/base"
)

func TestWriteStreamInput_WritesClaudeFormat(t *testing.T) {
	t.Parallel()

	r, w, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close() })
	t.Cleanup(func() { _ = w.Close() })

	conn := base.NewConn(slog.Default(), w, "user1", "sess1")

	err = writeStreamInput(w, conn.WriteMu(), "what is 1+1?")
	require.NoError(t, err)

	require.NoError(t, w.Close())

	data := make([]byte, 4096)
	n, readErr := r.Read(data)
	require.NoError(t, readErr)
	require.Greater(t, n, 0)

	// Verify JSON structure matches streamUserMessage.
	var msg streamUserMessage
	require.NoError(t, json.Unmarshal(data[:n], &msg))
	require.Equal(t, "user", msg.Type)
	require.Equal(t, "user", msg.Message.Role)
	require.Len(t, msg.Message.Content, 1)
	require.Equal(t, "text", msg.Message.Content[0].Type)
	require.Equal(t, "what is 1+1?", msg.Message.Content[0].Text)
}

func TestWriteStreamInput_CapturesLastInput(t *testing.T) {
	t.Parallel()

	r, w, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close() })
	t.Cleanup(func() { _ = w.Close() })

	conn := base.NewConn(slog.Default(), w, "user1", "sess1")

	// Initially empty.
	require.Equal(t, "", conn.LastInput())

	require.NoError(t, writeStreamInput(w, conn.WriteMu(), "first message"))
	conn.SetLastInput("first message")
	require.Equal(t, "first message", conn.LastInput())

	require.NoError(t, writeStreamInput(w, conn.WriteMu(), "second message"))
	conn.SetLastInput("second message")
	require.Equal(t, "second message", conn.LastInput())

	// Drain pipe so Close doesn't block.
	_, _ = r.Read(make([]byte, 4096))
}

func TestWriteStreamInput_ClosedStdin(t *testing.T) {
	t.Parallel()

	r, w, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close() })

	require.NoError(t, w.Close())

	var mu sync.Mutex
	err = writeStreamInput(w, &mu, "should fail")
	require.Error(t, err)
}

func TestConn_Stdin_SetLastInput_Integration(t *testing.T) {
	t.Parallel()

	// Verify Stdin(), SetLastInput(), WriteMu() work together.
	r, w, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close() })
	t.Cleanup(func() { _ = w.Close() })

	conn := base.NewConn(slog.Default(), w, "user1", "sess1")

	stdin, mu := conn.StdinLocked()
	require.NotNil(t, stdin)

	require.NoError(t, writeStreamInputLocked(stdin, "hello"))
	mu.Unlock()
	conn.SetLastInput("hello")
	require.Equal(t, "hello", conn.LastInput())

	// Drain pipe.
	_, _ = r.Read(make([]byte, 4096))
}

// TestInjectMidTurn_WritesStdinWithoutSetLastInput verifies the two defining
// properties of (*Worker).InjectMidTurn:
//
//  1. It writes the same stream-json user frame to stdin that Input would
//     (so Claude Code headless --print --input-format stream-json absorbs it
//     into the running turn rather than queuing a new one).
//  2. It does NOT update crash-recovery lastInput — bridge_worker.go replays
//     lastInput on resume failure, and a mid-turn supplement must never be
//     re-delivered as the turn's primary input.
//
// The pipe-backed *base.Conn triggers the production stdin-write path (the
// mock-conn fallback in Input is for non-base connections). Pattern mirrors
// TestCompact_ProcessNotRunning in worker_coverage_test.go.
func TestInjectMidTurn_WritesStdinWithoutSetLastInput(t *testing.T) {
	t.Parallel()

	r, w, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close() })

	baseConn := base.NewConn(slog.Default(), w, "user1", "sess1")
	ww := New()
	ww.testConn = baseConn

	content := "BONUS: tell me 7+8"
	require.NoError(t, ww.InjectMidTurn(t.Context(), content, nil))

	// Close the write end so io.ReadAll returns; the conn's stdin field is
	// left pointing at the closed fd, which is fine — the test is done.
	require.NoError(t, w.Close())

	frame, err := io.ReadAll(r)
	require.NoError(t, err)
	require.NotEmpty(t, frame)

	require.Contains(t, string(frame), content)
	require.Contains(t, string(frame), `"type":"user"`)

	// Critical correctness invariant: InjectMidTurn must NOT update lastInput.
	require.Empty(t, baseConn.LastInput(), "InjectMidTurn must not SetLastInput")
}
