package feishu

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

// ─── redactURL ──────────────────────────────────────────────────────────────

func TestRedactURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "non-URL unchanged",
			input: "plain text message",
			want:  "plain text message",
		},
		{
			name:  "ws URL without params unchanged",
			input: "ws://localhost:8080/path",
			want:  "ws://localhost:8080/path",
		},
		{
			name:  "wss URL without params unchanged",
			input: "wss://api.feishu.cn/ws",
			want:  "wss://api.feishu.cn/ws",
		},
		{
			name:  "http URL without params unchanged",
			input: "http://localhost:8888/health",
			want:  "http://localhost:8888/health",
		},
		{
			name:  "https URL without sensitive params unchanged",
			input: "https://api.feishu.cn/v1/messages?token=abc123",
			want:  "https://api.feishu.cn/v1/messages?token=abc123",
		},
		{
			name:  "access_key redacted",
			input: "https://api.feishu.cn/v1/messages?access_key=mykey&foo=bar",
			want:  "https://api.feishu.cn/v1/messages?access_key=***&foo=bar",
		},
		{
			name:  "ticket redacted",
			input: "wss://api.feishu.cn/ws?ticket=secret&channel=main",
			want:  "wss://api.feishu.cn/ws?ticket=***&channel=main",
		},
		{
			name:  "both redacted order preserved",
			input: "https://api.feishu.cn/v1/messages?access_key=key&ticket=tkt&other=val",
			want:  "https://api.feishu.cn/v1/messages?access_key=***&ticket=***&other=val",
		},
		{
			name:  "empty string unchanged",
			input: "",
			want:  "",
		},
		{
			name:  "partial URL prefix not matched",
			input: "wss://example.com",
			want:  "wss://example.com",
		},
		{
			name:  "embedded URL with prefix",
			input: "connected to wss://msg-frontier.feishu.cn/ws/v2?fpid=493&aid=552564&device_id=7631867954884938706&access_key=da66fd33a4640d3451f410be09f00066&service_id=33554678&ticket=8710bbb0-94e7-487e-80d5-393e88505c44",
			want:  "connected to wss://msg-frontier.feishu.cn/ws/v2?fpid=***&aid=552564&device_id=***&access_key=***&service_id=***&ticket=***",
		},
		{
			name:  "conn_id in brackets redacted",
			input: "connected to wss://host/ws?ticket=abc123[conn_id=7631867954884938706]",
			want:  "connected to wss://host/ws?ticket=***[conn_id=***]",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := redactURL(tt.input)
			require.Equal(t, tt.want, got)
		})
	}
}

// ─── SlogLogger Debug ─────────────────────────────────────────────────────────

func TestSlogLogger_Debug(t *testing.T) {
	t.Parallel()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	l := SlogLogger{Logger: log}

	// Silent debug messages produce no output (discarded).
	l.Debug(context.Background(), "ping success from server")
	l.Debug(context.Background(), "receive pong from endpoint")

	// Non-silent debug messages are logged (need LevelDebug handler).
	var got string
	h := slog.NewTextHandler(&captureWriter{&got}, &slog.HandlerOptions{Level: slog.LevelDebug})
	l2 := SlogLogger{Logger: slog.New(h)}
	l2.Debug(context.Background(), "some important debug message")
	require.Contains(t, got, "some important debug message")
}

// ─── SlogLogger Info ──────────────────────────────────────────────────────────

func TestSlogLogger_Info(t *testing.T) {
	t.Parallel()
	var got string
	h := slog.NewTextHandler(&captureWriter{&got}, nil)
	l := SlogLogger{Logger: slog.New(h)}

	l.Info(context.Background(), "info message", "key", "value")
	require.Contains(t, got, "info message")
	require.Contains(t, got, "key")
	require.Contains(t, got, "value")
}

// ─── SlogLogger Warn ──────────────────────────────────────────────────────────

func TestSlogLogger_Warn(t *testing.T) {
	t.Parallel()
	var got string
	h := slog.NewTextHandler(&captureWriter{&got}, nil)
	l := SlogLogger{Logger: slog.New(h)}

	l.Warn(context.Background(), "warning message")
	require.Contains(t, got, "warning message")
}

// ─── SlogLogger Error ─────────────────────────────────────────────────────────

func TestSlogLogger_Error(t *testing.T) {
	t.Parallel()
	var got string
	h := slog.NewTextHandler(&captureWriter{&got}, nil)
	l := SlogLogger{Logger: slog.New(h)}

	l.Error(context.Background(), "error message")
	require.Contains(t, got, "error message")
}

// ─── Level-specific filtering ──────────────────────────────────────────────────
//
// sdkLogFilter silences routine heartbeat/reconnect messages at Debug/Info level.
// sdkWarnFilter does NOT silence them at Warn/Error level — failures must surface.

func TestSlogLogger_DebugSilencesHeartbeat(t *testing.T) {
	t.Parallel()
	var got string
	h := slog.NewTextHandler(&captureWriter{&got}, &slog.HandlerOptions{Level: slog.LevelDebug})
	l := SlogLogger{Logger: slog.New(h)}

	l.Debug(context.Background(), "ping success from server")
	require.Empty(t, got, "Debug should silence 'ping success'")

	got = ""
	l.Debug(context.Background(), "receive pong from endpoint")
	require.Empty(t, got, "Debug should silence 'receive pong'")

	got = ""
	l.Debug(context.Background(), "disconnected to wss://example.com/ws")
	require.Empty(t, got, "Debug should silence 'disconnected to wss://'")

	got = ""
	l.Debug(context.Background(), "trying to reconnect: attempt 1")
	require.Empty(t, got, "Debug should silence 'trying to reconnect:'")
}

func TestSlogLogger_InfoSilencesHeartbeat(t *testing.T) {
	t.Parallel()
	var got string
	h := slog.NewTextHandler(&captureWriter{&got}, &slog.HandlerOptions{Level: slog.LevelInfo})
	l := SlogLogger{Logger: slog.New(h)}

	l.Info(context.Background(), "ping success from server")
	require.Empty(t, got, "Info should silence 'ping success'")

	got = ""
	l.Info(context.Background(), "disconnected to wss://example.com/ws")
	require.Empty(t, got, "Info should silence 'disconnected to wss://'")
}

// No-op event payloads (reaction, read) are silenced at Debug/Info level —
// these events have empty handlers in newEventHandler and their inbound DEBUG
// traffic buries real message events. Warn/Error must still surface them.

func TestSlogLogger_DebugSilencesNoopEventPayloads(t *testing.T) {
	t.Parallel()
	var got string
	h := slog.NewTextHandler(&captureWriter{&got}, &slog.HandlerOptions{Level: slog.LevelDebug})
	l := SlogLogger{Logger: slog.New(h)}

	l.Debug(context.Background(), `receive message, message_type: event, payload: {"header":{"event_type":"im.message.reaction.created_v1"}}`)
	require.Empty(t, got, "Debug should silence reaction.created no-op event")

	got = ""
	l.Debug(context.Background(), `receive message, payload: {"event_type":"im.message.reaction.deleted_v1"}`)
	require.Empty(t, got, "Debug should silence reaction.deleted no-op event")

	got = ""
	l.Debug(context.Background(), `receive message, payload: {"event_type":"im.message.read_v1"}`)
	require.Empty(t, got, "Debug should silence message.read no-op event")
}

func TestSlogLogger_DebugKeepsRealMessageEvents(t *testing.T) {
	t.Parallel()
	var got string
	h := slog.NewTextHandler(&captureWriter{&got}, &slog.HandlerOptions{Level: slog.LevelDebug})
	l := SlogLogger{Logger: slog.New(h)}

	l.Debug(context.Background(), `receive message, payload: {"event_type":"im.message.receive_v1"}`)
	require.Contains(t, got, "im.message.receive_v1",
		"Debug must NOT silence real inbound message events")
}

func TestSlogLogger_WarnDoesNotSilenceNoopEventPayloads(t *testing.T) {
	t.Parallel()
	var got string
	h := slog.NewTextHandler(&captureWriter{&got}, nil)
	l := SlogLogger{Logger: slog.New(h)}

	l.Warn(context.Background(), `dispatch failed, payload: {"event_type":"im.message.reaction.created_v1"}`)
	require.Contains(t, got, "im.message.reaction.created_v1",
		"Warn must NOT silence no-op event payloads — failures must surface")
}

func TestSlogLogger_WarnDoesNotSilenceHeartbeat(t *testing.T) {
	t.Parallel()
	var got1, got2 string
	h1 := slog.NewTextHandler(&captureWriter{&got1}, nil)
	h2 := slog.NewTextHandler(&captureWriter{&got2}, nil)
	l1 := SlogLogger{Logger: slog.New(h1)}
	l2 := SlogLogger{Logger: slog.New(h2)}

	l1.Warn(context.Background(), "ping success with latency 30ms")
	require.Contains(t, got1, "ping success",
		"Warn should NOT silence routine heartbeat messages")

	l2.Warn(context.Background(), "disconnected to wss://example.com/ws")
	require.Contains(t, got2, "disconnected to wss://",
		"Warn should NOT silence reconnection messages")
}

func TestSlogLogger_ErrorDoesNotSilenceHeartbeat(t *testing.T) {
	t.Parallel()
	var got1, got2 string
	h1 := slog.NewTextHandler(&captureWriter{&got1}, nil)
	h2 := slog.NewTextHandler(&captureWriter{&got2}, nil)
	l1 := SlogLogger{Logger: slog.New(h1)}
	l2 := SlogLogger{Logger: slog.New(h2)}

	l1.Error(context.Background(), "ping success after timeout")
	require.Contains(t, got1, "ping success",
		"Error should NOT silence routine heartbeat messages")

	l2.Error(context.Background(), "trying to reconnect: attempt 3")
	require.Contains(t, got2, "trying to reconnect:",
		"Error should NOT silence reconnection messages")
}

func TestSlogLogger_WarnAndErrorCleanReceiveMsgFailed(t *testing.T) {
	t.Parallel()
	var got1, got2 string
	h1 := slog.NewTextHandler(&captureWriter{&got1}, nil)
	h2 := slog.NewTextHandler(&captureWriter{&got2}, nil)
	l1 := SlogLogger{Logger: slog.New(h1)}
	l2 := SlogLogger{Logger: slog.New(h2)}

	l1.Warn(context.Background(), "receive message failed, err: something broke")
	require.Contains(t, got1, "receive message failed")
	require.Contains(t, got1, "(connection reset by peer)")
	require.NotContains(t, got1, "something broke",
		"Warn should clean 'receive message failed'")

	l2.Error(context.Background(), "receive message failed, err: timeout")
	require.Contains(t, got2, "receive message failed")
	require.Contains(t, got2, "(connection reset by peer)")
	require.NotContains(t, got2, "timeout",
		"Error should clean 'receive message failed'")
}

// TestSlogLoggerNilLoggerNoPanic guards against nil *slog.Logger deref panic
// (PR #828 review P2). SlogLogger is a public type with new call sites; a nil
// embedded logger must not panic.
func TestSlogLoggerNilLoggerNoPanic(t *testing.T) {
	t.Parallel()
	s := SlogLogger{}
	require.NotPanics(t, func() {
		s.Debug(context.Background(), "debug")
		s.Info(context.Background(), "info")
		s.Warn(context.Background(), "warn")
		s.Error(context.Background(), "error")
	})
}

// captureWriter implements io.Writer for slog handler.
type captureWriter struct {
	s *string
}

func (w *captureWriter) Write(p []byte) (int, error) {
	*w.s += string(p)
	return len(p), nil
}
