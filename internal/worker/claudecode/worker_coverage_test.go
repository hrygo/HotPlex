package claudecode

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/internal/worker/base"
	"github.com/hrygo/hotplex/internal/worker/proc"
	"github.com/hrygo/hotplex/pkg/events"
)

// helperReadOutput runs readOutput with the given lines and returns sent envelopes.
func helperReadOutput(t *testing.T, w *Worker, mc *mockConn, lines []string) []*events.Envelope {
	t.Helper()

	var idx atomic.Int64
	w.readLineFn = func() (string, error) {
		i := int(idx.Add(1) - 1)
		if i >= len(lines) {
			return "", io.EOF
		}
		return lines[i], nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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
		_ = mc.Recv() // satisfy defer Close guard
		w.readOutput(ctx)
	}()

	readWg.Wait()
	cancel()
	recverWg.Wait()

	return mc.sentEnvelopes()
}

// ─── readOutput: control events ──────────────────────────────────────────────

func TestReadOutput_ControlEvent_PermissionRequest(t *testing.T) {
	t.Parallel()

	w := NewWithMocks()
	require.NoError(t, w.permissionCeiling.Capture(worker.PermissionModeBypass))
	mc := newMockConn("user1", "session1")
	w.testConn = mc

	line := `{"type":"control_request","request_id":"perm_123","response":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"ls"}}}`

	sent := helperReadOutput(t, w, mc, []string{line})
	require.Len(t, sent, 1)
	require.Equal(t, events.PermissionRequest, sent[0].Event.Type)
}

func TestReadOutput_ControlEvent_AskUserQuestion(t *testing.T) {
	t.Parallel()

	w := NewWithMocks()
	mc := newMockConn("user1", "session1")
	w.testConn = mc

	line := `{"type":"control_request","request_id":"q_456","response":{"subtype":"can_use_tool","tool_name":"AskUserQuestion","input":{"questions":[{"id":"q1","question":"Deploy?"}]}}}`

	sent := helperReadOutput(t, w, mc, []string{line})
	require.Len(t, sent, 1)
	require.Equal(t, events.QuestionRequest, sent[0].Event.Type)
}

func TestReadOutput_ControlEvent_AskUserQuestion_NoInput(t *testing.T) {
	t.Parallel()

	w := NewWithMocks()
	mc := newMockConn("user1", "session1")
	w.testConn = mc

	line := `{"type":"control_request","request_id":"q_789","response":{"subtype":"can_use_tool","tool_name":"AskUserQuestion"}}`

	sent := helperReadOutput(t, w, mc, []string{line})
	require.Len(t, sent, 1)
	require.Equal(t, events.QuestionRequest, sent[0].Event.Type)
}

func TestReadOutput_ControlEvent_Elicitation(t *testing.T) {
	t.Parallel()

	w := NewWithMocks()
	mc := newMockConn("user1", "session1")
	w.testConn = mc

	line := `{"type":"control_request","request_id":"elic_789","response":{"subtype":"elicitation","mcp_server_name":"test-server","message":"Proceed?","mode":"confirm"}}`

	sent := helperReadOutput(t, w, mc, []string{line})
	require.Len(t, sent, 1)
	require.Equal(t, events.ElicitationRequest, sent[0].Event.Type)
}

func TestReadOutput_ControlResponse_DeliversToPending(t *testing.T) {
	t.Parallel()

	w := NewWithMocks()
	mc := newMockConn("user1", "session1")
	w.testConn = mc

	line := `{"type":"control_response","response":{"request_id":"ctx_abc","response":{"status":"ok"}}}`

	sent := helperReadOutput(t, w, mc, []string{line})
	require.Len(t, sent, 0) // consumed internally
}

func TestReadOutput_InterruptEvent(t *testing.T) {
	t.Parallel()

	w := NewWithMocks()
	mc := newMockConn("user1", "session1")
	w.testConn = mc

	line := `{"type":"control_request","request_id":"int_1","response":{"subtype":"interrupt"}}`

	sent := helperReadOutput(t, w, mc, []string{line})
	require.Len(t, sent, 0) // interrupt terminates without forwarding
}

func TestReadOutput_EmptyLines_Skipped(t *testing.T) {
	t.Parallel()

	w := NewWithMocks()
	mc := newMockConn("user1", "session1")
	w.testConn = mc

	lines := []string{"", `{"type":"result","is_error":false,"result":"ok"}`}
	sent := helperReadOutput(t, w, mc, lines)
	require.Len(t, sent, 1)
	require.Equal(t, events.Done, sent[0].Event.Type)
}

func TestReadOutput_ContextCancelled(t *testing.T) {
	t.Parallel()

	w := NewWithMocks()
	mc := newMockConn("user1", "session1")
	w.testConn = mc

	w.readLineFn = func() (string, error) {
		time.Sleep(10 * time.Millisecond)
		return "", io.EOF
	}

	ctx, cancel := context.WithCancel(context.Background())

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
		_ = mc.Recv()
		w.readOutput(ctx)
	}()

	time.AfterFunc(50*time.Millisecond, cancel)
	readWg.Wait()
	recverWg.Wait()
}

func TestReadOutput_ControlAutoSuccess(t *testing.T) {
	t.Parallel()

	w := NewWithMocks()
	mc := newMockConn("user1", "session1")
	w.testConn = mc

	line := `{"type":"control_request","request_id":"auto_123","response":{"subtype":"set_permission_mode","permission_mode":"auto-accept"}}`

	sent := helperReadOutput(t, w, mc, []string{line})
	require.Len(t, sent, 0) // auto-success handled internally
}

func TestReadOutput_InboundPermissionEscalationIsDenied(t *testing.T) {
	t.Parallel()

	w := NewWithMocks()
	require.NoError(t, w.permissionCeiling.Capture(worker.PermissionModeWorkspace))
	mc := newMockConn("user1", "session1")
	w.testConn = mc
	var response bytes.Buffer
	w.control = NewControlHandler(slog.Default(), &response)

	line := `{"type":"control_request","request_id":"deny_123","response":{"subtype":"set_permission_mode","permission_mode":"bypassPermissions"}}`
	sent := helperReadOutput(t, w, mc, []string{line})

	require.Empty(t, sent)
	require.Contains(t, response.String(), `"subtype":"error"`)
	require.Contains(t, response.String(), `"error":"ceiling_exceeded"`)
	require.NotContains(t, response.String(), `"subtype":"success"`)
	require.NotContains(t, response.String(), "bypassPermissions")
}

func TestAllowInboundPermissionMode_AllowsTightening(t *testing.T) {
	t.Parallel()

	w := NewWithMocks()
	require.NoError(t, w.permissionCeiling.Capture(worker.PermissionModeWorkspace))
	var response bytes.Buffer
	w.control = NewControlHandler(slog.Default(), &response)

	require.True(t, w.allowInboundPermissionMode(&ControlRequestPayload{
		RequestID:      "tighten_123",
		Subtype:        string(ControlSetPermissionMode),
		PermissionMode: "plan",
	}))
	require.Empty(t, response.String(), "legal requests proceed to the normal auto-success path")
}

func TestReadOutput_StreamEvent(t *testing.T) {
	t.Parallel()

	w := NewWithMocks()
	mc := newMockConn("user1", "session1")
	w.testConn = mc

	line := `{"type":"stream_event","event":{"type":"text","message":{"id":"msg_1","content":"hello"}}}`

	sent := helperReadOutput(t, w, mc, []string{line})
	require.Len(t, sent, 1)
	require.Equal(t, events.MessageDelta, sent[0].Event.Type)
}

func TestInput_QuestionResponse(t *testing.T) {
	t.Parallel()

	w := NewWithMocks()
	mc := newMockConn("user1", "session1")
	w.testConn = mc

	qResp := map[string]any{
		"question_response": map[string]any{
			"id":      "q_123",
			"answers": map[string]string{"q1": "yes"},
		},
	}

	err := w.Input(context.Background(), "", qResp)
	require.NoError(t, err)
}

func TestInput_ElicitationResponse(t *testing.T) {
	t.Parallel()

	w := NewWithMocks()
	mc := newMockConn("user1", "session1")
	w.testConn = mc

	eResp := map[string]any{
		"elicitation_response": map[string]any{
			"id":      "e_456",
			"action":  "accept",
			"content": map[string]any{"key": "value"},
		},
	}

	err := w.Input(context.Background(), "", eResp)
	require.NoError(t, err)
}

func TestInput_NormalText_MockConnFallback(t *testing.T) {
	t.Parallel()

	w := NewWithMocks()
	mc := newMockConn("user1", "session1")
	w.testConn = mc

	err := w.Input(context.Background(), "hello world", nil)
	require.NoError(t, err)

	sent := mc.sentEnvelopes()
	require.Len(t, sent, 1)
	require.Equal(t, events.Input, sent[0].Event.Type)
}

func TestInput_PermissionResponse_WriteError(t *testing.T) {
	t.Parallel()

	w := NewWithMocks()
	w.control = NewControlHandler(slog.Default(), &closedBuffer{})

	permResp := map[string]any{
		"permission_response": map[string]any{
			"request_id": "req_closed",
			"allowed":    true,
			"reason":     "",
		},
	}

	err := w.Input(context.Background(), "", permResp)
	require.Error(t, err)
}

// ─── SendControlRequest when not running ─────────────────────────────────────

func TestSendControlRequest_NotRunning(t *testing.T) {
	t.Parallel()

	w := New()
	_, err := w.SendControlRequest(context.Background(), "get_context_usage", nil)
	require.Error(t, err)
	var we *worker.WorkerError
	require.ErrorAs(t, err, &we)
	require.Equal(t, worker.ErrKindUnavailable, we.Kind)
}

// ─── nextSeq ─────────────────────────────────────────────────────────────────

func TestNextSeq_Monotonic(t *testing.T) {
	t.Parallel()

	w := New()
	// nextSeq now returns 0 to delegate seq assignment to the bridge's Hub
	// SeqGen. A local counter restarts from 1 on every Worker launch and
	// collides with persisted events on resume (issue #879).
	for i := 0; i < 5; i++ {
		require.Equal(t, int64(0), w.nextSeq(),
			"nextSeq must return 0 so bridge assigns Hub SeqGen")
	}
}

// ─── trySend edge cases ──────────────────────────────────────────────────────

func TestTrySend_NilConn(t *testing.T) {
	t.Parallel()

	w := New()
	w.trySend(events.NewEnvelope("id1", "sess1", 1, events.Done, nil))
}

func TestTrySend_ConnWithoutTrySend(t *testing.T) {
	t.Parallel()

	w := New()
	w.testConn = &noTrySendConn{}
	w.trySend(events.NewEnvelope("id1", "sess1", 1, events.Done, nil))
}

type noTrySendConn struct{}

func (c *noTrySendConn) Send(_ context.Context, _ *events.Envelope) error { return nil }
func (c *noTrySendConn) Recv() <-chan *events.Envelope {
	ch := make(chan *events.Envelope)
	close(ch)
	return ch
}
func (c *noTrySendConn) Close() error           { return nil }
func (c *noTrySendConn) UserID() string         { return "user" }
func (c *noTrySendConn) SessionID() string      { return "session" }
func (c *noTrySendConn) StdinWriter() io.Writer { return io.Discard }

// ─── deleteSessionFiles ──────────────────────────────────────────────────────

func TestDeleteSessionFiles_NoFiles(t *testing.T) {
	t.Parallel()

	w := New()
	w.sessionID = "nonexistent-session-id"
	err := w.deleteSessionFiles()
	require.NoError(t, err)
}

// ─── ResetContext ─────────────────────────────────────────────────────────────

func TestResetContext_CleansUp(t *testing.T) {
	t.Parallel()

	w := NewWithMocks()
	w.sessionID = "reset-test-session"
	w.origSession = worker.SessionInfo{
		SessionID:  "reset-test-session",
		UserID:     "user1",
		ProjectDir: "/tmp",
	}

	w.readLineFn = func() (string, error) { return "", io.EOF }

	err := w.deleteSessionFiles()
	require.NoError(t, err)
}

// ─── joinTools ────────────────────────────────────────────────────────────────

func TestJoinTools(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		tools []string
		want  string
	}{
		{"empty", nil, ""},
		{"single", []string{"Read"}, "Read"},
		{"multiple", []string{"Read", "Write", "Bash"}, "Read,Write,Bash"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, joinTools(tt.tools))
		})
	}
}

// ─── ControlHandler: DeliverResponse no pending request ──────────────────────

func TestControlHandler_DeliverResponse_NoPending(t *testing.T) {
	t.Parallel()

	ch := NewControlHandler(slog.Default(), io.Discard)
	ch.DeliverResponse("nonexistent_req_id", map[string]any{"status": "ok"})
}

// ─── ControlHandler: SendControlRequest with context cancellation ────────────

func TestControlHandler_SendControlRequest_ContextCancelled(t *testing.T) {
	t.Parallel()

	ch := NewControlHandler(slog.Default(), io.Discard)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := ch.SendControlRequest(ctx, "set_model", map[string]any{"model": "test"})
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
}

type closedBuffer struct{}

func (c *closedBuffer) Write(_ []byte) (int, error) { return 0, io.ErrClosedPipe }

// ─── Compact ──────────────────────────────────────────────────────────────────

func TestCompact_NotStarted(t *testing.T) {
	t.Parallel()

	w := New()
	err := w.Compact(context.Background(), nil)
	require.Error(t, err)
	var we *worker.WorkerError
	require.ErrorAs(t, err, &we)
	require.Equal(t, worker.ErrKindUnavailable, we.Kind)
}

func TestCompact_ProcessNotRunning(t *testing.T) {
	t.Parallel()

	r, w, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close() })

	ww := New()
	conn := base.NewConn(slog.Default(), w, "user1", "sess1")
	ww.testConn = conn
	ww.Proc = proc.New(proc.Opts{Logger: slog.Default()})
	ww.Proc.SetPIDKey("test")

	require.NoError(t, w.Close())

	err = ww.Compact(context.Background(), nil)
	require.Error(t, err)
	var we *worker.WorkerError
	require.ErrorAs(t, err, &we)
	require.Equal(t, worker.ErrKindUnavailable, we.Kind)
}

func TestClear_NotImplemented(t *testing.T) {
	t.Parallel()

	w := New()
	err := w.Clear(context.Background())
	require.Error(t, err)
	require.Equal(t, worker.ErrNotImplemented, err)
}

// ─── Rewind ──────────────────────────────────────────────────────────────────

func TestRewind_NotRunning(t *testing.T) {
	t.Parallel()

	w := New()
	err := w.Rewind(context.Background(), "msg-123")
	require.Error(t, err)
	var we *worker.WorkerError
	require.ErrorAs(t, err, &we)
	require.Equal(t, worker.ErrKindUnavailable, we.Kind)
}

func TestRewind_ControlHandlerNotInitialized(t *testing.T) {
	t.Parallel()

	w := New()
	// Proc is nil → SendControlRequest returns ErrKindUnavailable before reaching control handler
	err := w.Rewind(context.Background(), "msg-123")
	require.Error(t, err)
}
