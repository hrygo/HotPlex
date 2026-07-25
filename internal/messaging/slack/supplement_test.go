package slack

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/pkg/events"
)

// TestSlackSupplementText verifies the English i18n text mapping per supplement
// mode. injected: merged into the running turn. buffered: queued for next turn.
// Any other mode falls back to the buffered text (safe default).
func TestSlackSupplementText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		mode string
		want string
	}{
		{"injected merged into current turn", "injected", "⏳ Got it — processing within the current task."},
		{"buffered for next turn", "buffered", "⏳ Got it — will process automatically once the current task finishes."},
		{"unknown mode falls back to buffered text", "unknown", "⏳ Got it — will process automatically once the current task finishes."},
		{"empty mode falls back to buffered text", "", "⏳ Got it — will process automatically once the current task finishes."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, slackSupplementText(tt.mode))
		})
	}
}

// TestWriteCtx_SupplementMode_RendersI18nText verifies that a `message` envelope
// carrying supplement_mode metadata triggers the i18n supplement text send
// (synchronously) instead of the normal display path (which would silently drop
// empty Content).
//
// Detection is verified by the writeWithPostMessage error ("slack: client not
// initialized" in the test harness). Without the early return, the message
// path returns nil silently for empty Content.
func TestWriteCtx_SupplementMode_RendersI18nText(t *testing.T) {
	t.Parallel()

	conn := &SlackConn{
		adapter:   &Adapter{},
		channelID: "C123",
		threadTS:  "123.456",
	}

	for _, mode := range []string{"injected", "buffered"} {
		env := &events.Envelope{
			SessionID: "s-test",
			Event:     events.Event{Type: events.Message},
			Metadata:  map[string]any{"supplement_mode": mode},
		}
		err := conn.WriteCtx(context.Background(), env)
		// The supplement path reaches writeWithPostMessage, which fails
		// predictably with "slack: client not initialized" in the test harness.
		// Without the supplement check, WriteCtx would return nil (empty
		// Content dropped by handleStandaloneMessage).
		require.Error(t, err, "mode=%s should reach writeWithPostMessage", mode)
		require.Contains(t, err.Error(), "client not initialized",
			"mode=%s should trigger writeWithPostMessage via supplement path", mode)
	}
}

// TestWriteCtx_SupplementMode_NoMetadataFallsThrough confirms that a Message
// envelope WITHOUT supplement_mode metadata is handled by the normal display
// path (returns nil for empty Content, does not call writeWithPostMessage).
func TestWriteCtx_SupplementMode_NoMetadataFallsThrough(t *testing.T) {
	t.Parallel()

	conn := &SlackConn{
		adapter:   &Adapter{},
		channelID: "C123",
		threadTS:  "123.456",
	}

	env := &events.Envelope{
		SessionID: "s-test",
		Event:     events.Event{Type: events.Message},
	}
	err := conn.WriteCtx(context.Background(), env)
	require.NoError(t, err, "Message without supplement_mode and empty Content returns nil")
}
