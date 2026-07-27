package feishu

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/pkg/events"
)

// TestFeishuSupplementText verifies the i18n text mapping per supplement mode.
// injected: merged into the running turn → "" (silent — the result appears as
// part of the current reply, so a confirmation would be redundant noise).
// buffered: queued for the next turn → user-facing ack so the user knows their
// message wasn't lost. Any unrecognized mode falls back to the buffered text
// as a safe default (promise future handling, never imply immediate processing).
func TestFeishuSupplementText(t *testing.T) {
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
			require.Equal(t, tt.want, feishuSupplementText(tt.mode))
		})
	}
}

// TestWriteCtx_SupplementMode_BufferedReachesSend verifies that a `message`
// envelope carrying supplement_mode=buffered triggers the i18n supplement text
// send instead of the normal display path (which would silently drop empty
// Content).
//
// Detection is verified by the sendTextMessage error ("lark client not
// initialized" in the test harness). Without the supplement branch, the empty
// Content would be dropped silently (return nil).
func TestWriteCtx_SupplementMode_BufferedReachesSend(t *testing.T) {
	t.Parallel()

	adapter := newTestAdapter(t)
	conn := newTestConn(adapter, "") // no replyToMsgID → sendTextMessage path

	env := &events.Envelope{
		SessionID: "s-test",
		Event:     events.Event{Type: events.Message},
		Metadata:  map[string]any{"supplement_mode": "buffered"},
	}
	err := conn.WriteCtx(context.Background(), env)
	// buffered reaches sendTextMessage, which fails predictably with "lark
	// client not initialized" in the test harness.
	require.Error(t, err)
	require.Contains(t, err.Error(), "lark client not initialized")
}

// TestWriteCtx_SupplementMode_InjectedIsSilent verifies that an injected
// supplement (merged into the running turn) does NOT send any ack text — it
// returns nil before reaching sendTextMessage, since the result will appear as
// part of the current reply and a confirmation would be redundant noise.
func TestWriteCtx_SupplementMode_InjectedIsSilent(t *testing.T) {
	t.Parallel()

	adapter := newTestAdapter(t)
	conn := newTestConn(adapter, "")

	env := &events.Envelope{
		SessionID: "s-test",
		Event:     events.Event{Type: events.Message},
		Metadata:  map[string]any{"supplement_mode": "injected"},
	}
	err := conn.WriteCtx(context.Background(), env)
	// injected is silent: no sendTextMessage call, no error.
	require.NoError(t, err)
}

// TestWriteCtx_SupplementMode_NoMetadataFallsThrough confirms that a Message
// envelope WITHOUT supplement_mode metadata is handled by the normal display
// path (returns nil for empty Content, does not call sendTextMessage).
func TestWriteCtx_SupplementMode_NoMetadataFallsThrough(t *testing.T) {
	t.Parallel()

	adapter := newTestAdapter(t)
	conn := newTestConn(adapter, "")

	// Empty Content, no supplement metadata → silent no-op (existing behavior).
	env := &events.Envelope{
		SessionID: "s-test",
		Event:     events.Event{Type: events.Message},
	}
	err := conn.WriteCtx(context.Background(), env)
	require.NoError(t, err, "Message without supplement_mode and empty Content returns nil")
}
