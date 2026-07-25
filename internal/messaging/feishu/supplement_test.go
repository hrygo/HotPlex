package feishu

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/pkg/events"
)

// TestFeishuSupplementText verifies the i18n text mapping per supplement mode.
// injected: supplement was merged into the running turn.
// buffered: supplement was queued for the next turn.
// Any other mode falls back to the buffered text (safe default — promises
// future handling rather than implying immediate processing).
func TestFeishuSupplementText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		mode string
		want string
	}{
		{"injected merged into current turn", "injected", "⏳ 已收到，正在当前任务中一并处理"},
		{"buffered for next turn", "buffered", "⏳ 已收到，当前任务完成后会自动处理"},
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

// TestWriteCtx_SupplementMode_RendersI18nText verifies that a `message` envelope
// carrying supplement_mode metadata triggers the i18n supplement text send
// instead of the normal display path (which would silently drop empty Content).
//
// Detection is verified by the sendTextMessage error ("lark client not
// initialized" in the test harness). Without the early return, handleMessageEvent
// would see empty Content and return nil silently.
func TestWriteCtx_SupplementMode_RendersI18nText(t *testing.T) {
	t.Parallel()

	adapter := newTestAdapter(t)
	conn := newTestConn(adapter, "") // no replyToMsgID → sendTextMessage path

	for _, mode := range []string{"injected", "buffered"} {
		env := &events.Envelope{
			SessionID: "s-test",
			Event:     events.Event{Type: events.Message},
			Metadata:  map[string]any{"supplement_mode": mode},
		}
		err := conn.WriteCtx(context.Background(), env)
		// The supplement path reaches sendTextMessage, which fails predictably
		// with "lark client not initialized" in the test harness. Without the
		// supplement check, WriteCtx would return nil (empty Content dropped).
		require.Error(t, err, "mode=%s should reach sendTextMessage", mode)
		require.Contains(t, err.Error(), "lark client not initialized",
			"mode=%s should trigger sendTextMessage via supplement path", mode)
	}
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
