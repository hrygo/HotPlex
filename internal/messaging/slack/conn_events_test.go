package slack

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/slack-go/slack"
	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/pkg/events"
)

// fakeStreamWriter is a controllable streamContentCloser fake: it records the
// contexts passed to CloseContext so tests can assert deadline sharing between
// the close and the terminal fallback.
type fakeStreamWriter struct {
	mu             sync.Mutex
	content        string
	closeErr       error
	closeCalls     int
	closeContexts  []context.Context
	closeDeadlines []time.Time
}

func (f *fakeStreamWriter) Content() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.content
}

func (f *fakeStreamWriter) Write(p []byte) (int, error) {
	return len(p), nil
}

func (f *fakeStreamWriter) Close() error {
	return f.CloseContext(context.Background())
}

func (f *fakeStreamWriter) CloseContext(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closeCalls++
	f.closeContexts = append(f.closeContexts, ctx)
	if d, ok := ctx.Deadline(); ok {
		f.closeDeadlines = append(f.closeDeadlines, d)
	}
	return f.closeErr
}

// slackTerminalFake is a RoundTripper backing the adapter's real slack client.
// It records chat.postMessage request bodies and their context deadlines, and
// can be configured to fail posts. It answers every endpoint with a complete
// success structure so the slack-go SDK parses it without error.
type slackTerminalFake struct {
	mu        sync.Mutex
	bodies    []string
	deadlines []time.Time
	postFail  error
}

func (f *slackTerminalFake) RoundTrip(req *http.Request) (*http.Response, error) {
	if strings.Contains(req.URL.Path, "chat.postMessage") {
		if f.postFail != nil {
			return nil, f.postFail
		}
		body, _ := io.ReadAll(req.Body)
		f.mu.Lock()
		f.bodies = append(f.bodies, string(body))
		if d, ok := req.Context().Deadline(); ok {
			f.deadlines = append(f.deadlines, d)
		}
		f.mu.Unlock()
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"ok":true,"channel":"C_TEST","ts":"1600000000.000001"}`)),
			Request:    req,
		}, nil
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
		Request:    req,
	}, nil
}

// texts returns the decoded chat.postMessage texts in order.
func (f *slackTerminalFake) texts() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.bodies))
	for _, body := range f.bodies {
		decoded, err := url.QueryUnescape(body)
		if err != nil {
			decoded = body
		}
		out = append(out, decoded)
	}
	return out
}

// newConnWithTerminalFake creates a SlackConn whose adapter posts through a
// real slack client backed by the given RoundTripper. Transport-level failures
// (postFail) surface as *url.Error preserving the sentinel identity via
// Unwrap, so errors.Is can reach them.
func newConnWithTerminalFake(t *testing.T, fake *slackTerminalFake) *SlackConn {
	t.Helper()
	adapter := newTestAdapter(t)
	adapter.client = slack.New("x-test-token",
		slack.OptionAPIURL("http://terminal-fake.invalid/"),
		slack.OptionHTTPClient(&http.Client{Transport: fake}))
	return NewSlackConn(adapter, "C_TEST", "", "")
}

func doneEnvelope() *events.Envelope {
	return &events.Envelope{
		Version:   events.Version,
		SessionID: "session-1",
		Event:     events.Event{Type: events.Done, Data: events.DoneData{Success: true}},
	}
}

func errorEnvelope() *events.Envelope {
	return &events.Envelope{
		Version:   events.Version,
		SessionID: "session-1",
		Event:     events.Event{Type: events.Error, Data: events.ErrorData{Code: "MODEL_ERROR", Message: "model failed"}},
	}
}

func TestSlackConn_HandleDone_CloseOKNoFallback(t *testing.T) {
	t.Parallel()

	fake := &slackTerminalFake{}
	conn := newConnWithTerminalFake(t, fake)
	conn.streamWriter = &fakeStreamWriter{content: "full response"}

	err := conn.handleDone(context.Background(), doneEnvelope())

	require.NoError(t, err)
	require.Empty(t, fake.texts(), "close success must not trigger a fallback")
}

func TestSlackConn_HandleDone_FallbackSentOnceWhenBodyNotPresented(t *testing.T) {
	t.Parallel()

	closeErr := errors.New("stop stream failed")
	fake := &slackTerminalFake{}
	conn := newConnWithTerminalFake(t, fake)
	conn.streamWriter = &fakeStreamWriter{
		content:  "full response",
		closeErr: &StreamTerminalError{Cause: closeErr, ContentPresented: false},
	}

	err := conn.handleDone(context.Background(), doneEnvelope())

	require.ErrorIs(t, err, closeErr, "the original close error must be preserved")
	texts := fake.texts()
	require.Len(t, texts, 1, "exactly one fixed short fallback text")
	require.Contains(t, texts[0], "Reply delivery failed")
	require.NotContains(t, texts[0], "full response", "never resend the full answer")
}

func TestSlackConn_HandleDone_NoFallbackWhenBodyPresented(t *testing.T) {
	t.Parallel()

	closeErr := errors.New("stop stream failed")
	fake := &slackTerminalFake{}
	conn := newConnWithTerminalFake(t, fake)
	conn.streamWriter = &fakeStreamWriter{
		content:  "full response",
		closeErr: &StreamTerminalError{Cause: closeErr, ContentPresented: true},
	}

	err := conn.handleDone(context.Background(), doneEnvelope())

	require.ErrorIs(t, err, closeErr)
	require.Empty(t, fake.texts(), "decoration-only failure must not duplicate content")
}

func TestSlackConn_HandleDone_JoinsCloseAndFallbackFailures(t *testing.T) {
	t.Parallel()

	stopErr := errors.New("stop stream failed")
	fallbackErr := errors.New("post message failed")
	fake := &slackTerminalFake{postFail: fallbackErr}
	conn := newConnWithTerminalFake(t, fake)
	conn.streamWriter = &fakeStreamWriter{
		content:  "full response",
		closeErr: &StreamTerminalError{Cause: stopErr, ContentPresented: false},
	}

	err := conn.handleDone(context.Background(), doneEnvelope())

	require.ErrorIs(t, err, stopErr, "close failure must be visible")
	require.ErrorIs(t, err, fallbackErr, "fallback failure must be visible")
}

func TestSlackConn_HandleError_PostMessageFailureReturnsSynchronously(t *testing.T) {
	t.Parallel()

	postErr := errors.New("post message failed")
	fake := &slackTerminalFake{postFail: postErr}
	conn := newConnWithTerminalFake(t, fake)
	// No active stream writer: only the controlled error text is sent.

	err := conn.handleError(context.Background(), errorEnvelope())

	require.ErrorIs(t, err, postErr, "error-event send failure must surface synchronously, not via goroutine")
}

func TestSlackConn_HandleError_JoinsCloseAndSendFailures(t *testing.T) {
	t.Parallel()

	stopErr := errors.New("stop stream failed")
	postErr := errors.New("post message failed")
	fake := &slackTerminalFake{postFail: postErr}
	conn := newConnWithTerminalFake(t, fake)
	conn.streamWriter = &fakeStreamWriter{
		content:  "partial response",
		closeErr: &StreamTerminalError{Cause: stopErr, ContentPresented: false},
	}

	err := conn.handleError(context.Background(), errorEnvelope())

	require.ErrorIs(t, err, stopErr, "close failure must be visible")
	require.ErrorIs(t, err, postErr, "error text send failure must be visible")
}

func TestSlackConn_closeStreamWriter_NilWriterReturnsNil(t *testing.T) {
	t.Parallel()

	conn := newConnWithTerminalFake(t, &slackTerminalFake{})
	require.NoError(t, conn.closeStreamWriter(context.Background()))
}

func TestSlackConn_closeStreamWriter_PropagatesCloseError(t *testing.T) {
	t.Parallel()

	closeErr := errors.New("close failed")
	conn := newConnWithTerminalFake(t, &slackTerminalFake{})
	conn.streamWriter = &fakeStreamWriter{closeErr: closeErr}

	require.ErrorIs(t, conn.closeStreamWriter(context.Background()), closeErr)
}

func TestSlackConn_closeStreamWriter_IdempotentAfterClose(t *testing.T) {
	t.Parallel()

	closeErr := errors.New("close failed")
	conn := newConnWithTerminalFake(t, &slackTerminalFake{})
	conn.streamWriter = &fakeStreamWriter{closeErr: closeErr}

	require.ErrorIs(t, conn.closeStreamWriter(context.Background()), closeErr)
	require.NoError(t, conn.closeStreamWriter(context.Background()),
		"second close must be a nil no-op (writer already cleared)")
}

func TestSlackConn_TerminalHandlersShareCallerDeadlineAcrossCloseAndFallback(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		env  *events.Envelope
	}{
		{name: "done", env: doneEnvelope()},
		{name: "error", env: errorEnvelope()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			closeErr := errors.New("stop stream failed")
			fake := &slackTerminalFake{}
			conn := newConnWithTerminalFake(t, fake)
			writer := &fakeStreamWriter{
				content:  "terminal body",
				closeErr: &StreamTerminalError{Cause: closeErr, ContentPresented: false},
			}
			conn.streamWriter = writer

			callerTimeout := 250 * time.Millisecond
			if tt.env.Event.Type == events.Error {
				callerTimeout = 6 * time.Second
			}
			ctx, cancel := context.WithTimeout(context.Background(), callerTimeout)
			defer cancel()
			callerDeadline, ok := ctx.Deadline()
			require.True(t, ok)

			err := conn.handleDoneOrError(t, tt.env, ctx)

			require.ErrorIs(t, err, closeErr)
			require.NotEmpty(t, writer.closeDeadlines)
			require.NotEmpty(t, fake.deadlines, "fallback send must carry a deadline")
			for _, deadline := range append(writer.closeDeadlines, fake.deadlines...) {
				require.WithinDuration(t, callerDeadline, deadline, 10*time.Millisecond,
					"close and fallback must share the caller's terminal budget, never extend it")
			}
		})
	}
}

func TestSlackConn_TerminalHandlersDefaultDeadlineIsSharedWithFallback(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		env  *events.Envelope
	}{
		{name: "done", env: doneEnvelope()},
		{name: "error", env: errorEnvelope()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			closeErr := errors.New("stop stream failed")
			fake := &slackTerminalFake{}
			conn := newConnWithTerminalFake(t, fake)
			writer := &fakeStreamWriter{
				content:  "terminal body",
				closeErr: &StreamTerminalError{Cause: closeErr, ContentPresented: false},
			}
			conn.streamWriter = writer

			started := time.Now()
			err := conn.handleDoneOrError(t, tt.env, context.Background())

			require.ErrorIs(t, err, closeErr)
			require.NotEmpty(t, writer.closeDeadlines)
			require.NotEmpty(t, fake.deadlines, "fallback send must carry a deadline")
			sharedDeadline := writer.closeDeadlines[0]
			require.WithinDuration(t, started.Add(defaultTerminalDeliveryTimeout), sharedDeadline, 50*time.Millisecond,
				"no caller deadline must yield one default end-to-end 5s budget")
			for _, deadline := range append(writer.closeDeadlines, fake.deadlines...) {
				require.Equal(t, sharedDeadline, deadline,
					"close and fallback must use one default end-to-end deadline")
			}
		})
	}
}

// handleDoneOrError dispatches to handleDone or handleError based on the
// envelope kind (shared by the deadline tests).
func (c *SlackConn) handleDoneOrError(t *testing.T, env *events.Envelope, ctx context.Context) error {
	t.Helper()
	switch env.Event.Type {
	case events.Done:
		return c.handleDone(ctx, env)
	case events.Error:
		return c.handleError(ctx, env)
	}
	t.Fatalf("unexpected kind %s", env.Event.Type)
	return nil
}
