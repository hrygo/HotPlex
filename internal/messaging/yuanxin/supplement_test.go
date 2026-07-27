package yuanxin

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/pkg/events"
)

// TestYuanxinSupplementText verifies the Chinese i18n text mapping per supplement
// mode. injected: merged into the running turn → "" (silent — the result
// appears as part of the current reply). buffered: queued for the next turn →
// user-facing ack. Any unrecognized mode falls back to the buffered text as a
// safe default.
func TestYuanxinSupplementText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		mode string
		want string
	}{
		{"injected merged into current turn is silent", "injected", ""},
		{"buffered for next turn acks", "buffered", "⏳ 已收到，当前任务完成后会自动处理"},
		{"unknown mode falls back to buffered text", "unknown", "⏳ 已收到，当前任务完成后会自动处理"},
		{"empty mode falls back to buffered text", "", "⏳ 已收到，当前任务完成后会自动处理"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, yuanxinSupplementText(tt.mode))
		})
	}
}

// TestWriteCtx_SupplementMode_BufferedReachesSend verifies that an envelope
// carrying supplement_mode=buffered triggers the i18n supplement text send.
//
// Detection is verified by the SendResponse error ("producer not initialized"
// in the test harness).
func TestWriteCtx_SupplementMode_BufferedReachesSend(t *testing.T) {
	t.Parallel()

	a := newTestAdapter()
	conn := NewYuanxinConn(a, "ch-test", "th-test", "/tmp")

	env := &events.Envelope{
		SessionID: "s-test",
		Event:     events.Event{Type: events.Message},
		Metadata:  map[string]any{"supplement_mode": "buffered"},
	}
	err := conn.WriteCtx(context.Background(), env)
	require.Error(t, err)
	require.Contains(t, err.Error(), "producer not initialized")
}

// TestWriteCtx_SupplementMode_InjectedIsSilent verifies that an injected
// supplement does NOT send any ack text — returns nil before reaching
// SendResponse.
func TestWriteCtx_SupplementMode_InjectedIsSilent(t *testing.T) {
	t.Parallel()

	a := newTestAdapter()
	conn := NewYuanxinConn(a, "ch-test", "th-test", "/tmp")

	env := &events.Envelope{
		SessionID: "s-test",
		Event:     events.Event{Type: events.Message},
		Metadata:  map[string]any{"supplement_mode": "injected"},
	}
	err := conn.WriteCtx(context.Background(), env)
	require.NoError(t, err)
}

// TestWriteCtx_SupplementMode_NoMetadataFallsThrough confirms that an envelope
// WITHOUT supplement_mode metadata is handled by the normal display path.
func TestWriteCtx_SupplementMode_NoMetadataFallsThrough(t *testing.T) {
	t.Parallel()

	a := newTestAdapter()
	conn := NewYuanxinConn(a, "ch-test", "th-test", "/tmp")

	env := &events.Envelope{
		SessionID: "s-test",
		Event:     events.Event{Type: events.Message},
	}
	err := conn.WriteCtx(context.Background(), env)
	require.NoError(t, err, "Message without supplement_mode and empty Content returns nil")
}
