package claudecode

// stream_fake.go is a protocol-level fake for the Claude Code binary invoked
// as `claude --print --output-format stream-json --input-format stream-json`.
//
// It closes the in-repo test-infrastructure gap (issue #958 T12a): the only
// stream-json probe used to live in scripts/test_cc_command.py as a live
// Python subprocess. This fake drives the same wire protocol in-process so
// worker tests — and later gateway contract tests — can exercise the full
// stdin (user frames) and stdout (assistant frames) surface without a real
// `claude` binary.
//
// Inbound (Worker → fake): user frames written by the Worker are captured on
// the write end of an os.Pipe (StdinFile). The Worker's base.Conn must be
// constructed over that file so the real writeStreamInputLocked path is
// exercised; captured lines are decoded into UserFrame values.
//
// Outbound (fake → Worker): canned assistant frames queued with the Emit*
// helpers are served one NDJSON line at a time through ReadLine, which is a
// drop-in for worker.readLineFn. ReadLine returns io.EOF once the queue is
// drained, terminating readOutput exactly like a closed stdout pipe would.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hrygo/hotplex/internal/worker/base"
)

// streamFakeFrameWait is the default wait window for AssertUserFrame. User
// frames arrive asynchronously (the capture goroutine drains the pipe), so the
// assertion helper polls with a bounded timeout instead of racing.
const streamFakeFrameWait = 5 * time.Second

// UserFrameBlock is a single content block of a captured user frame.
type UserFrameBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// UserFrameMessage is the message body of a captured user frame.
type UserFrameMessage struct {
	Role    string           `json:"role"`
	Content []UserFrameBlock `json:"content"`
}

// UserFrame is a decoded stream-json user frame written by the Worker to
// "claude stdin" (the shape produced by writeStreamInputLocked).
type UserFrame struct {
	Type    string           `json:"type"`
	Message UserFrameMessage `json:"message"`
}

// Text returns the concatenated text content of the frame. For a skill
// invocation this is the canonical slash form `/name args`.
func (f UserFrame) Text() string {
	var b strings.Builder
	for _, block := range f.Message.Content {
		if block.Type == "text" {
			b.WriteString(block.Text)
		}
	}
	return b.String()
}

// StreamFake emulates the Claude Code CLI's stream-json stdio protocol. It is
// intentionally Worker-agnostic on the read side (Emit*, ReadLine) and the
// stdin side (StdinFile), so gateway contract tests can drive the same fake
// through plain file descriptors.
type StreamFake struct {
	mu sync.Mutex

	stdinR *os.File // read end (fake side; drained by the capture goroutine)
	stdinW *os.File // write end (handed to the Worker as its stdin)

	frames  []UserFrame
	frameCh chan struct{} // buffered 1; non-blocking signal when a frame arrives

	out    []string // queued outbound NDJSON lines
	outIdx int

	conn *base.Conn // set by Attach; exposed for LastInput/replay inspection

	captureDone chan struct{} // closed when captureLoop exits; stdinR is owned by Close
	closed      bool
}

// NewStreamFake creates a fake and starts the inbound capture goroutine.
// Call Close to release the pipe file descriptors.
func NewStreamFake() (*StreamFake, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("claudecode: stream fake pipe: %w", err)
	}
	f := &StreamFake{
		stdinR:      r,
		stdinW:      w,
		frameCh:     make(chan struct{}, 1),
		captureDone: make(chan struct{}),
	}
	go f.captureLoop()
	return f, nil
}

// captureLoop drains the inbound pipe, decoding NDJSON user frames into
// UserFrame values. Non-user protocol lines (e.g. control_request frames a
// test may inject) are ignored, matching the parser's tolerance. The read end
// is closed only by Close (after captureDone) to avoid a double-close race.
func (f *StreamFake) captureLoop() {
	defer close(f.captureDone)
	scanner := bufio.NewScanner(f.stdinR)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	for scanner.Scan() {
		var frame UserFrame
		if err := json.Unmarshal(scanner.Bytes(), &frame); err != nil {
			continue
		}
		f.mu.Lock()
		f.frames = append(f.frames, frame)
		f.mu.Unlock()
		select {
		case f.frameCh <- struct{}{}:
		default:
		}
	}
}

// StdinFile returns the write end of the inbound pipe. Pass it to
// base.NewConn so the Worker's stream-json input path writes into the fake.
func (f *StreamFake) StdinFile() *os.File { return f.stdinW }

// Frames returns a copy of the captured user frames.
func (f *StreamFake) Frames() []UserFrame {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]UserFrame(nil), f.frames...)
}

// WaitForUserFrame blocks until the first captured user frame is available or
// ctx is done.
func (f *StreamFake) WaitForUserFrame(ctx context.Context) (UserFrame, error) {
	for {
		f.mu.Lock()
		if len(f.frames) > 0 {
			frame := f.frames[0]
			f.mu.Unlock()
			return frame, nil
		}
		f.mu.Unlock()
		select {
		case <-f.frameCh:
		case <-ctx.Done():
			return UserFrame{}, ctx.Err()
		}
	}
}

// AssertUserFrame asserts that a user frame was captured and its text equals
// the canonical slash form `/name args` (produced by skillCommandText /
// InvokeSkill). It waits up to streamFakeFrameWait for the frame to arrive.
func (f *StreamFake) AssertUserFrame(t testing.TB, name, args string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), streamFakeFrameWait)
	defer cancel()
	frame, err := f.WaitForUserFrame(ctx)
	if err != nil {
		t.Fatalf("claudecode: stream fake: waiting for user frame: %v", err)
	}
	want := "/" + strings.TrimSpace(name)
	if trimmed := strings.TrimSpace(args); trimmed != "" {
		want += " " + trimmed
	}
	if got := frame.Text(); got != want {
		t.Fatalf("claudecode: stream fake: user frame text = %q, want canonical slash form %q", got, want)
	}
}

// Attach wires the fake into a Worker under test: the fake's stdin pipe
// becomes the worker's SessionConn (exercising the real writeStreamInputLocked
// path for Input/InvokeSkill) and ReadLine feeds readOutput. The created
// *base.Conn is retained and returned via Conn for LastInput/replay inspection.
func (f *StreamFake) Attach(w *Worker, userID, sessionID string) *base.Conn {
	bc := base.NewConn(w.Log, f.StdinFile(), userID, sessionID)
	f.mu.Lock()
	f.conn = bc
	f.mu.Unlock()
	w.testConn = bc
	w.readLineFn = f.ReadLine
	return bc
}

// Conn returns the base.Conn created by Attach, or nil if Attach was not called.
func (f *StreamFake) Conn() *base.Conn {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.conn
}

// ReadLine is a drop-in for worker.readLineFn: it serves the queued outbound
// frames one NDJSON line at a time and returns io.EOF once the queue is empty.
func (f *StreamFake) ReadLine() (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.outIdx >= len(f.out) {
		return "", io.EOF
	}
	line := f.out[f.outIdx]
	f.outIdx++
	return line, nil
}

// Close releases the pipe file descriptors. Safe to call multiple times.
// Closing the write end makes the capture scanner hit EOF; the read end is
// closed only after captureLoop has exited, so stdinR is never double-closed.
func (f *StreamFake) Close() error {
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		return nil
	}
	f.closed = true
	f.mu.Unlock()

	errW := f.stdinW.Close()
	<-f.captureDone
	errR := f.stdinR.Close()
	if errW != nil {
		return errW
	}
	return errR
}

// ─── Outbound frame emitters ───────────────────────────────────────────────

// queue appends one NDJSON line to the outbound queue. The shapes are fixed by
// the call sites, so json.Marshal cannot fail; a failure is intentionally
// ignored rather than recovered in a method without an error return.
func (f *StreamFake) queue(obj any) {
	data, err := json.Marshal(obj)
	if err != nil {
		return
	}
	f.mu.Lock()
	f.out = append(f.out, string(data))
	f.mu.Unlock()
}

// queueStreamEvent wraps an Anthropic SDK event in the Claude Code
// stream_event envelope, matching what the real CLI emits on stdout.
func (f *StreamFake) queueStreamEvent(event map[string]any) {
	f.queue(map[string]any{"type": "stream_event", "event": event})
}

// EmitMessageStart queues a stream_event/message_start frame.
func (f *StreamFake) EmitMessageStart(msgID, model string) {
	if msgID == "" {
		msgID = "msg_test"
	}
	if model == "" {
		model = "claude-test-model"
	}
	f.queueStreamEvent(map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id":      msgID,
			"type":    "message",
			"role":    "assistant",
			"content": []any{},
			"model":   model,
			"usage":   map[string]any{"input_tokens": 0, "output_tokens": 0},
		},
	})
}

// EmitContentBlockStart queues a stream_event/content_block_start frame.
func (f *StreamFake) EmitContentBlockStart(index int) {
	f.queueStreamEvent(map[string]any{
		"type":          "content_block_start",
		"index":         index,
		"content_block": map[string]any{"type": "text", "text": ""},
	})
}

// EmitContentBlockDelta queues a stream_event/content_block_delta frame.
func (f *StreamFake) EmitContentBlockDelta(index int, text string) {
	f.queueStreamEvent(map[string]any{
		"type":  "content_block_delta",
		"index": index,
		"delta": map[string]any{"type": "text_delta", "text": text},
	})
}

// EmitContentBlockStop queues a stream_event/content_block_stop frame.
func (f *StreamFake) EmitContentBlockStop(index int) {
	f.queueStreamEvent(map[string]any{"type": "content_block_stop", "index": index})
}

// EmitMessageDelta queues a stream_event/message_delta frame. An empty
// stopReason defaults to "end_turn".
func (f *StreamFake) EmitMessageDelta(stopReason string) {
	if stopReason == "" {
		stopReason = "end_turn"
	}
	f.queueStreamEvent(map[string]any{
		"type":  "message_delta",
		"delta": map[string]any{"stop_reason": stopReason, "stop_sequence": nil},
		"usage": map[string]any{"output_tokens": 0},
	})
}

// EmitMessageStop queues a stream_event/message_stop frame.
func (f *StreamFake) EmitMessageStop() {
	f.queueStreamEvent(map[string]any{"type": "message_stop"})
}

// EmitAssistantText queues an `assistant` frame whose message carries a single
// text block. This is the shape the parser's parseAssistant consumes, so it is
// the primary user-visible text delivery for a fake turn.
func (f *StreamFake) EmitAssistantText(msgID, text string) {
	message := map[string]any{
		"role":    "assistant",
		"content": []map[string]any{{"type": "text", "text": text}},
	}
	if msgID != "" {
		message["id"] = msgID
	}
	f.queue(map[string]any{"type": "assistant", "message": message})
}

// EmitTextDelta queues a stream_event/text frame with the text in
// event.message.content — the exact shape parser_test uses for streamed text.
func (f *StreamFake) EmitTextDelta(msgID, text string) {
	message := map[string]any{"content": text, "role": "assistant"}
	if msgID != "" {
		message["id"] = msgID
	}
	f.queueStreamEvent(map[string]any{"type": "text", "message": message})
}

// EmitResult queues a `result` frame carrying the final assistant text and the
// turn outcome. isError=false surfaces as done(success), isError=true as
// error + done(failure).
func (f *StreamFake) EmitResult(text string, isError bool) {
	f.queue(map[string]any{
		"type":           "result",
		"is_error":       isError,
		"result":         text,
		"duration_ms":    42,
		"num_turns":      1,
		"total_cost_usd": 0.0,
	})
}

// EmitAssistantTurn queues a complete, parser-realistic assistant turn:
// the Anthropic SDK framing events (message_start → content_block_start →
// content_block_delta → content_block_stop → message_delta → message_stop),
// an assistant frame with the final text, and the terminating result frame.
// The framing events are protocol noise for the parser (ignored without
// error); the assistant frame delivers the text and the result closes the turn.
func (f *StreamFake) EmitAssistantTurn(text string, isError bool) {
	f.EmitMessageStart("", "")
	f.EmitContentBlockStart(0)
	f.EmitContentBlockDelta(0, text)
	f.EmitContentBlockStop(0)
	f.EmitMessageDelta("")
	f.EmitMessageStop()
	f.EmitAssistantText("", text)
	f.EmitResult(text, isError)
}
