package slack

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/pkg/events"
)

// TestSlackSupplementText verifies the English i18n text mapping per supplement
// mode. injected: merged into the running turn → "" (silent — the result
// appears as part of the current reply). buffered: queued for the next turn →
// user-facing ack. Any unrecognized mode falls back to the buffered text as a
// safe default.
func TestSlackSupplementText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		mode string
		want string
	}{
		{"injected merged into current turn is silent", "injected", ""},
		{"buffered for next turn acks", "buffered", "⏳ Got it — will process automatically once the current task finishes."},
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

// TestWriteCtx_SupplementMode_BufferedReachesSend verifies that a `message`
// envelope carrying supplement_mode=buffered triggers the i18n supplement text
// send (synchronously) instead of the normal display path.
//
// Detection is verified by the writeWithPostMessage error ("slack: client not
// initialized" in the test harness).
func TestWriteCtx_SupplementMode_BufferedReachesSend(t *testing.T) {
	t.Parallel()

	conn := &SlackConn{
		adapter:   &Adapter{},
		channelID: "C123",
		threadTS:  "123.456",
	}

	env := &events.Envelope{
		SessionID: "s-test",
		Event:     events.Event{Type: events.Message},
		Metadata:  map[string]any{"supplement_mode": "buffered"},
	}
	err := conn.WriteCtx(context.Background(), env)
	require.Error(t, err)
	require.Contains(t, err.Error(), "client not initialized")
}

// TestWriteCtx_SupplementMode_InjectedIsSilent verifies that an injected
// supplement does NOT send any ack text — returns nil before reaching
// writeWithPostMessage.
func TestWriteCtx_SupplementMode_InjectedIsSilent(t *testing.T) {
	t.Parallel()

	conn := &SlackConn{
		adapter:   &Adapter{},
		channelID: "C123",
		threadTS:  "123.456",
	}

	env := &events.Envelope{
		SessionID: "s-test",
		Event:     events.Event{Type: events.Message},
		Metadata:  map[string]any{"supplement_mode": "injected"},
	}
	err := conn.WriteCtx(context.Background(), env)
	require.NoError(t, err)
}

// TestWriteCtx_SupplementMode_NoMetadataFallsThrough confirms that a Message
// envelope WITHOUT supplement_mode metadata is handled by the normal display
// path (returns nil for empty Content).
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
