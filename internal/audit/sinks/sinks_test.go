package sinks

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNoopSink_NoError(t *testing.T) {
	t.Parallel()
	s := NewNoopSink()
	err := s.OnAlertEvent(context.Background(), AlertEvent{
		EventID: "e1", UserID: "u1", Action: "test", Outcome: "success",
	})
	require.NoError(t, err)
}

func TestLogSink_WritesToSlog(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	s := NewLogSink(log)
	err := s.OnAlertEvent(context.Background(), AlertEvent{
		EventID: "e1", Ts: time.Unix(1700000000, 0), UserID: "u1", UserIDType: "platform",
		Platform: "feishu", Action: "auth.login", Outcome: "success",
	})
	require.NoError(t, err)
	// Parse the JSON line and assert fields
	var line map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &line))
	require.Equal(t, "audit_event", line["msg"])
	require.Equal(t, "e1", line["event_id"])
	require.Equal(t, "u1", line["user_id"])
}

func TestLogSink_NilLoggerDefaults(t *testing.T) {
	t.Parallel()
	s := NewLogSink(nil)
	require.NotNil(t, s)
	err := s.OnAlertEvent(context.Background(), AlertEvent{EventID: "e1"})
	require.NoError(t, err)
}

func TestRegisterAndBuild(t *testing.T) {
	t.Parallel()
	sink, err := Build("noop", nil, nil)
	require.NoError(t, err)
	require.NotNil(t, sink)
}

func TestBuildUnknownReturnsError(t *testing.T) {
	t.Parallel()
	_, err := Build("unknown_sink", nil, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown sink name")
}

func TestRegisterCustomSink(t *testing.T) {
	t.Parallel()
	called := false
	Register("custom_test", func(_ map[string]any, _ *slog.Logger) (Sink, error) {
		called = true
		return NewNoopSink(), nil
	})
	sink, err := Build("custom_test", nil, nil)
	require.NoError(t, err)
	require.NotNil(t, sink)
	require.True(t, called)
}
