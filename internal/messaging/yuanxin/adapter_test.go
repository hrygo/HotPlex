package yuanxin

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/messaging"
	"github.com/hrygo/hotplex/pkg/events"
)

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestAdapter() *Adapter {
	log := newTestLogger()
	return &Adapter{
		BaseAdapter: messaging.BaseAdapter[*YuanxinConn]{
			PlatformAdapter: messaging.PlatformAdapter{
				Log:          log,
				Interactions: messaging.NewInteractionManager(log),
			},
		},
	}
}

func TestAdapter_ConfigureWith(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		extras     map[string]any
		wantAppID  string
		wantURL    string
		wantTenant string
		wantNS     string
		wantTopic  string
		wantErr    bool
	}{
		{
			name:       "missing app_id",
			extras:     map[string]any{},
			wantAppID:  "",
			wantURL:    "pulsar://localhost:6650",
			wantTenant: "public",
			wantNS:     "default",
			wantTopic:  "global-open-claw-response-topic",
			wantErr:    true,
		},
		{
			name: "custom values",
			extras: map[string]any{
				"app_id":         "my-app",
				"pulsar_url":     "pulsar://custom:6650",
				"tenant":         "custom-tenant",
				"namespace":      "custom-ns",
				"producer_topic": "my-response-topic",
			},
			wantAppID:  "my-app",
			wantURL:    "pulsar://custom:6650",
			wantTenant: "custom-tenant",
			wantNS:     "custom-ns",
			wantTopic:  "my-response-topic",
		},
		{
			name: "full topic URL passthrough",
			extras: map[string]any{
				"app_id":         "app-123",
				"producer_topic": "persistent://tenant/ns/topic",
			},
			wantAppID:  "app-123",
			wantURL:    "pulsar://localhost:6650",
			wantTenant: "public",
			wantNS:     "default",
			wantTopic:  "persistent://tenant/ns/topic",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			a := &Adapter{}
			err := a.ConfigureWith(messaging.AdapterConfig{Extras: tt.extras})
			if tt.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), "app_id is required")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantAppID, a.appID)
			require.Equal(t, tt.wantURL, a.pulsarURL)
			require.Equal(t, tt.wantTenant, a.tenant)
			require.Equal(t, tt.wantNS, a.ns)
			require.Equal(t, tt.wantTopic, a.producerTopic)
		})
	}
}

func TestAdapter_Platform(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		adapter  *Adapter
		wantType messaging.PlatformType
		wantID   string
	}{
		{
			name:     "platform type",
			adapter:  &Adapter{},
			wantType: messaging.PlatformYuanxin,
		},
		{
			name:    "bot ID from appID",
			adapter: &Adapter{appID: "test-bot"},
			wantID:  "test-bot",
		},
		{
			name:    "empty bot ID",
			adapter: &Adapter{},
			wantID:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.wantType != "" {
				require.Equal(t, tt.wantType, tt.adapter.Platform())
			}
			if tt.name != "platform type" {
				require.Equal(t, tt.wantID, tt.adapter.GetBotID())
			}
		})
	}
}

func TestAdapter_HandleTextMessage(t *testing.T) {
	t.Parallel()
	a := &Adapter{}
	err := a.HandleTextMessage(context.Background(), "msg", "ch", "team", "thread", "user", "text")
	require.NoError(t, err)
}

func TestNewYuanxinConn(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		channel string
		thread  string
		workDir string
	}{
		{"full params", "channel1", "thread1", "/tmp/work"},
		{"empty thread", "channel2", "", "/data"},
		{"empty all", "", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			adapter := &Adapter{}
			conn := NewYuanxinConn(adapter, tt.channel, tt.thread, tt.workDir)
			require.Equal(t, adapter, conn.adapter)
			require.Equal(t, tt.channel, conn.channelID)
			require.Equal(t, tt.thread, conn.threadKey)
			require.Equal(t, tt.workDir, conn.workDir)
			require.NotNil(t, conn.metadata)
		})
	}
}

func TestYuanxinConn_WriteCtx_NilEnvelope(t *testing.T) {
	t.Parallel()
	conn := &YuanxinConn{}
	err := conn.WriteCtx(context.Background(), nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "nil envelope")
}

func TestYuanxinConn_WriteCtx_EventTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		env     *events.Envelope
		wantErr bool
		errMsg  string
	}{
		{
			name: "done cancels interactions",
			env: &events.Envelope{
				SessionID: "sess-1",
				Event:     events.Event{Type: events.Done},
			},
		},
		{
			name: "error with empty message",
			env: &events.Envelope{
				SessionID: "sess-2",
				Event: events.Event{
					Type: events.Error,
					Data: events.ErrorData{Code: events.ErrCodeInternalError},
				},
			},
		},
		{
			name: "error with message but no producer",
			env: &events.Envelope{
				SessionID: "sess-3",
				Event: events.Event{
					Type: events.Error,
					Data: events.ErrorData{Message: "something broke"},
				},
			},
			wantErr: true,
			errMsg:  "producer not initialized",
		},
		{
			name: "unsupported event type no text",
			env: &events.Envelope{
				Event: events.Event{Type: "unknown_type"},
			},
		},
		{
			name: "message.delta with content but no producer",
			env: &events.Envelope{
				Event: events.Event{
					Type: events.MessageDelta,
					Data: events.MessageDeltaData{Content: "hello"},
				},
			},
		},
		{
			name: "message.delta with empty content",
			env: &events.Envelope{
				Event: events.Event{
					Type: events.MessageDelta,
					Data: events.MessageDeltaData{Content: ""},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			a := newTestAdapter()
			conn := NewYuanxinConn(a, "ch", "thread", "/tmp")

			err := conn.WriteCtx(context.Background(), tt.env)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					require.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestYuanxinConn_Close(t *testing.T) {
	t.Parallel()
	adapter := &Adapter{}
	conn := NewYuanxinConn(adapter, "test-channel", "test-thread", "/tmp")
	err := conn.Close()
	require.NoError(t, err)
}

func TestYuanxinConn_SetGetMetadata(t *testing.T) {
	t.Parallel()

	conn := &YuanxinConn{
		metadata: make(map[string]any),
	}

	metadata := map[string]any{"trace_id": "abc123", "span_id": "def456"}
	conn.SetMetadata(metadata)

	result := conn.GetMetadata()
	require.Equal(t, "abc123", result["trace_id"])
	require.Equal(t, "def456", result["span_id"])

	original := conn.GetMetadata()
	metadata["new_key"] = "new_value"
	require.NotEqual(t, original["new_key"], "new_value")
}

func TestYuanxinMessage_Fields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		msg      YuanxinMessage
		wantMsg  string
		wantMeta map[string]any
	}{
		{
			name: "with metadata and msg",
			msg: YuanxinMessage{
				Metadata: map[string]any{"key": "value", "id": 123},
				Msg:      "Hello, World!",
			},
			wantMsg:  "Hello, World!",
			wantMeta: map[string]any{"key": "value", "id": 123},
		},
		{
			name:     "empty message",
			msg:      YuanxinMessage{},
			wantMsg:  "",
			wantMeta: nil,
		},
		{
			name:     "msg only",
			msg:      YuanxinMessage{Msg: "just text"},
			wantMsg:  "just text",
			wantMeta: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.wantMsg, tt.msg.Msg)
			if tt.wantMeta != nil {
				require.Equal(t, tt.wantMeta["key"], tt.msg.Metadata["key"])
			} else {
				require.Nil(t, tt.msg.Metadata)
			}
		})
	}
}

func TestAdapter_SendResponse_NoProducer(t *testing.T) {
	t.Parallel()
	a := newTestAdapter()
	conn := NewYuanxinConn(a, "ch", "th", "/tmp")

	err := a.SendResponse(context.Background(), conn, "hello")
	require.Error(t, err)
	require.Contains(t, err.Error(), "producer not initialized")
}

func TestYuanxinConn_ImplementsPlatformConn(t *testing.T) {
	t.Parallel()
	var conn messaging.PlatformConn = &YuanxinConn{}
	require.NotNil(t, conn)
}

func TestMetadataString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		md   map[string]any
		key  string
		want string
	}{
		{"nil map", nil, "key", ""},
		{"empty map", map[string]any{}, "key", ""},
		{"key missing", map[string]any{"other": "val"}, "key", ""},
		{"string value", map[string]any{"replyUserCodes": "u-123"}, "replyUserCodes", "u-123"},
		{"non-string value", map[string]any{"count": 42}, "count", ""},
		{"bool value", map[string]any{"flag": true}, "flag", ""},
		{"empty string", map[string]any{"name": ""}, "name", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, metadataString(tt.md, tt.key))
		})
	}
}

func TestAdapter_Close_WithoutStart(t *testing.T) {
	t.Parallel()
	a := newTestAdapter()

	require.False(t, a.IsClosed())
	err := a.Close(context.Background())
	require.NoError(t, err)
	require.True(t, a.IsClosed())
}

func TestAdapter_Close_IdempotentMarkClosed(t *testing.T) {
	t.Parallel()
	a := newTestAdapter()

	err := a.Close(context.Background())
	require.NoError(t, err)
	require.True(t, a.IsClosed())

	err = a.Close(context.Background())
	require.NoError(t, err)
	require.True(t, a.IsClosed())
}

func TestAdapter_GetConnResources_NilFields(t *testing.T) {
	t.Parallel()
	a := &Adapter{}

	consumer, producer := a.getConnResources()
	require.Nil(t, consumer)
	require.Nil(t, producer)
}

func TestYuanxinConn_DeltaAccumulation(t *testing.T) {
	t.Parallel()
	a := newTestAdapter()
	conn := NewYuanxinConn(a, "ch", "thread", "/tmp")

	err := conn.WriteCtx(context.Background(), &events.Envelope{
		Event: events.Event{
			Type: events.MessageDelta,
			Data: events.MessageDeltaData{Content: "Hello"},
		},
	})
	require.NoError(t, err)

	err = conn.WriteCtx(context.Background(), &events.Envelope{
		Event: events.Event{
			Type: events.MessageDelta,
			Data: events.MessageDeltaData{Content: " World"},
		},
	})
	require.NoError(t, err)

	conn.mu.RLock()
	got := conn.textBuilder.String()
	conn.mu.RUnlock()
	require.Equal(t, "Hello World", got)
}

func TestYuanxinConn_DoneSendsAccumulatedText(t *testing.T) {
	t.Parallel()
	a := newTestAdapter()
	conn := NewYuanxinConn(a, "ch", "thread", "/tmp")

	err := conn.WriteCtx(context.Background(), &events.Envelope{
		SessionID: "sess-1",
		Event: events.Event{
			Type: events.MessageDelta,
			Data: events.MessageDeltaData{Content: "Hello "},
		},
	})
	require.NoError(t, err)

	err = conn.WriteCtx(context.Background(), &events.Envelope{
		SessionID: "sess-1",
		Event: events.Event{
			Type: events.MessageDelta,
			Data: events.MessageDeltaData{Content: "World"},
		},
	})
	require.NoError(t, err)

	err = conn.WriteCtx(context.Background(), &events.Envelope{
		SessionID: "sess-1",
		Event:     events.Event{Type: events.Done},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "producer not initialized")

	conn.mu.RLock()
	got := conn.textBuilder.String()
	conn.mu.RUnlock()
	require.Equal(t, "", got)
}

func TestAdapter_SendCronResult_MissingMessageId(t *testing.T) {
	t.Parallel()
	a := newTestAdapter()

	err := a.SendCronResult(context.Background(), "hello", map[string]string{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing message_id")
}

func TestAdapter_SendCronResult_NoProducer(t *testing.T) {
	t.Parallel()
	a := newTestAdapter()

	err := a.SendCronResult(context.Background(), "hello", map[string]string{
		"message_id": "msg-123",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "producer not initialized")
}

func TestYuanxinConn_DoneWithNoAccumulatedText(t *testing.T) {
	t.Parallel()
	a := newTestAdapter()
	conn := NewYuanxinConn(a, "ch", "thread", "/tmp")

	err := conn.WriteCtx(context.Background(), &events.Envelope{
		SessionID: "sess-1",
		Event:     events.Event{Type: events.Done},
	})
	require.NoError(t, err)
}

func TestYuanxinConn_ErrorClearsAccumulatedText(t *testing.T) {
	t.Parallel()
	a := newTestAdapter()
	conn := NewYuanxinConn(a, "ch", "thread", "/tmp")

	err := conn.WriteCtx(context.Background(), &events.Envelope{
		SessionID: "sess-1",
		Event: events.Event{
			Type: events.MessageDelta,
			Data: events.MessageDeltaData{Content: "some text"},
		},
	})
	require.NoError(t, err)

	err = conn.WriteCtx(context.Background(), &events.Envelope{
		SessionID: "sess-1",
		Event: events.Event{
			Type: events.Error,
			Data: events.ErrorData{Message: "boom"},
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "producer not initialized")

	conn.mu.RLock()
	got := conn.textBuilder.String()
	conn.mu.RUnlock()
	require.Equal(t, "", got)
}
