package claudecode

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/events"
)

func TestSkillCommandTextUsesCanonicalSlashSyntax(t *testing.T) {
	t.Parallel()

	require.Equal(t, "/oracle-dba", skillCommandText(worker.SkillInvocation{Name: "oracle-dba"}))
	require.Equal(t, "/oracle-dba 10.102.78.1", skillCommandText(worker.SkillInvocation{
		Name: "oracle-dba",
		Args: "10.102.78.1",
	}))
}

// newSkillTestWorker wires a StreamFake into a Worker so InvokeSkill exercises
// the real writeStreamInputLocked path (NDJSON user frame to the fake's stdin)
// and readOutput can consume canned assistant frames through fake.ReadLine.
func newSkillTestWorker(t *testing.T) (*Worker, *StreamFake) {
	t.Helper()
	w := NewWithMocks()
	fake, err := NewStreamFake()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, fake.Close()) })
	fake.Attach(w, "user1", "session1")
	return w, fake
}

// TestInvokeSkillWritesCanonicalSlashFrame drives the native dispatch path
// (NativeCommandInvocation → AsNativeInvoker → InvokeSkill) through the
// protocol fake and asserts the captured stream-json user frame carries the
// exact canonical slash form `/oracle-dba 10.0.0.1`.
func TestInvokeSkillWritesCanonicalSlashFrame(t *testing.T) {
	t.Parallel()

	w, fake := newSkillTestWorker(t)

	invoker, ok := worker.AsNativeInvoker(w)
	require.True(t, ok, "claudecode worker must expose a native invoker via AsNativeInvoker")

	err := invoker.InvokeNativeCommand(t.Context(), worker.NativeCommandInvocation{
		Name: "oracle-dba",
		Args: "10.0.0.1",
		Mode: worker.SkillModeTextCommand,
	})
	require.NoError(t, err)

	// The user frame must be the canonical slash form, not an AEP envelope.
	fake.AssertUserFrame(t, "oracle-dba", "10.0.0.1")
}

// TestInvokeSkillNoArgsWritesBareSlashFrame covers the no-args branch of
// skillCommandText: `/name` with no trailing space or args.
func TestInvokeSkillNoArgsWritesBareSlashFrame(t *testing.T) {
	t.Parallel()

	w, fake := newSkillTestWorker(t)

	err := w.InvokeSkill(t.Context(), worker.SkillInvocation{
		Name: "oracle-dba",
		Mode: worker.SkillModeTextCommand,
	})
	require.NoError(t, err)

	fake.AssertUserFrame(t, "oracle-dba", "")
}

// TestInvokeSkillEmptyNameRejected verifies the existing guard in
// InvokeSkill (worker.go): empty/whitespace names are rejected before any
// frame is written, on both the legacy and the native dispatch surface.
func TestInvokeSkillEmptyNameRejected(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		inv  worker.SkillInvocation
	}{
		{name: "empty name", inv: worker.SkillInvocation{}},
		{name: "whitespace name", inv: worker.SkillInvocation{Name: "   ", Args: "10.0.0.1"}},
		{name: "empty name with args", inv: worker.SkillInvocation{Args: "10.0.0.1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			w, fake := newSkillTestWorker(t)

			err := w.InvokeSkill(t.Context(), tt.inv)
			require.Error(t, err)
			require.Contains(t, err.Error(), "skill name required")
			require.Empty(t, fake.Frames(), "no user frame may be written for a rejected skill name")
		})
	}

	t.Run("native invocation surface", func(t *testing.T) {
		t.Parallel()
		w, fake := newSkillTestWorker(t)

		invoker, ok := worker.AsNativeInvoker(w)
		require.True(t, ok)
		err := invoker.InvokeNativeCommand(t.Context(), worker.NativeCommandInvocation{
			Name: "  ",
			Args: "10.0.0.1",
			Mode: worker.SkillModeTextCommand,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "skill name required")
		require.Empty(t, fake.Frames(), "no user frame may be written for a rejected native command")
	})
}

// TestInvokeSkillReplayStoresCanonicalSlashText verifies the crash-replay
// contract for claudecode: a native skill invocation is dispatched through the
// ordinary text path, so the durable replay surface (base.Conn.LastInput →
// InputReplay.Content) carries the canonical slash form verbatim. Replaying
// that content re-dispatches the same native command.
func TestInvokeSkillReplayStoresCanonicalSlashText(t *testing.T) {
	t.Parallel()

	w, fake := newSkillTestWorker(t)
	bc := fake.Conn()
	require.NotNil(t, bc, "Attach must expose the underlying base.Conn for replay inspection")

	err := w.InvokeSkill(t.Context(), worker.SkillInvocation{
		Name: "oracle-dba",
		Args: "10.0.0.1",
		Mode: worker.SkillModeTextCommand,
	})
	require.NoError(t, err)

	// SetLastInput runs synchronously inside Input after the frame write, so a
	// direct assertion is safe once InvokeSkill returned nil.
	require.Equal(t, "/oracle-dba 10.0.0.1", bc.LastInput(),
		"crash replay must re-deliver the exact canonical slash form")

	// And it must be a proper native invocation, not an arbitrary prompt: the
	// slash text re-enters the same skill path when replayed as text.
	frame, err := fake.WaitForUserFrame(t.Context())
	require.NoError(t, err)
	require.Equal(t, "/oracle-dba 10.0.0.1", frame.Text())
}

// TestFakeEmitsTerminalResult feeds a canned assistant turn ending in a result
// frame through the fake and asserts the adapter surfaces the response through
// the ordinary readOutput → parser → mapper → trySend pipeline (assistant text
// becomes message.delta, result becomes done).
func TestFakeEmitsTerminalResult(t *testing.T) {
	t.Parallel()

	w := NewWithMocks()
	mc := newMockConn("user1", "session1")
	w.testConn = mc

	fake, err := NewStreamFake()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, fake.Close()) })
	w.readLineFn = fake.ReadLine

	fake.EmitAssistantTurn("oracle-dba: all good", false)

	sent := drainReadOutput(t, w, mc)
	require.Len(t, sent, 2)

	require.Equal(t, events.MessageDelta, sent[0].Event.Type)
	delta, ok := sent[0].Event.Data.(events.MessageDeltaData)
	require.True(t, ok)
	require.Equal(t, "oracle-dba: all good", delta.Content)

	require.Equal(t, events.Done, sent[1].Event.Type)
	done, ok := sent[1].Event.Data.(events.DoneData)
	require.True(t, ok)
	require.True(t, done.Success)
}

// TestStreamFakeSdkFramingFramesTolerated locks a protocol-fidelity property of
// the fake: the Anthropic SDK framing events (message_start, content_block_*,
// message_delta, message_stop) wrapped in stream_event envelopes are tolerated
// by the parser (no error, no event) and a subsequent result frame still
// terminates the turn. This mirrors what the real CLI emits between text
// chunks (see scripts/test_cc_command.py stdout_reader).
func TestStreamFakeSdkFramingFramesTolerated(t *testing.T) {
	t.Parallel()

	w := NewWithMocks()
	mc := newMockConn("user1", "session1")
	w.testConn = mc

	fake, err := NewStreamFake()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, fake.Close()) })
	w.readLineFn = fake.ReadLine

	fake.EmitMessageStart("msg_01", "claude-test-model")
	fake.EmitContentBlockStart(0)
	fake.EmitContentBlockDelta(0, "hello")
	fake.EmitContentBlockStop(0)
	fake.EmitMessageDelta("")
	fake.EmitMessageStop()
	fake.EmitResult("done", false)

	sent := drainReadOutput(t, w, mc)
	require.Len(t, sent, 1, "SDK framing frames are protocol noise; only the result surfaces")
	require.Equal(t, events.Done, sent[0].Event.Type)
}

// drainReadOutput runs readOutput to completion (fake.ReadLine returns io.EOF
// once the queued frames are consumed) and returns the envelopes the worker
// forwarded through the mock connection.
func drainReadOutput(t *testing.T, w *Worker, mc *mockConn) []*events.Envelope {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	var recverWg sync.WaitGroup
	recverWg.Add(1)
	go func() {
		defer recverWg.Done()
		for range mc.Recv() {
		}
	}()

	var readWg sync.WaitGroup
	readWg.Add(1)
	go func() {
		defer readWg.Done()
		_ = mc.Recv() // match the existing readOutput test harness pattern
		w.readOutput(ctx)
	}()
	readWg.Wait()
	cancel()
	recverWg.Wait()
	return mc.sentEnvelopes()
}
