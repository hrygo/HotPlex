package gateway

import (
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/pkg/events"
)

// tryReadEnvelope reads one envelope from the WS conn with a short deadline.
// Returns nil if nothing arrived (used to assert "no message was sent").
func tryReadEnvelope(t *testing.T, server interface {
	ReadMessage() (int, []byte, error)
	SetReadDeadline(time.Time) error
}) *events.Envelope {
	t.Helper()
	require.NoError(t, server.SetReadDeadline(time.Now().Add(300*time.Millisecond)))
	defer func() { _ = server.SetReadDeadline(time.Time{}) }()
	_, data, err := server.ReadMessage()
	if err != nil {
		return nil
	}
	var env events.Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return &env
}

// TestMaybeSendDoneFallback verifies a synthetic Message is injected when a
// turn did tool work but produced no assistant text, so feishu/slack don't
// show an empty reply. Mirrors maybeTransitionIdleAfterDone test pattern.
func TestMaybeSendDoneFallback(t *testing.T) {
	t.Parallel()

	setup := func(t *testing.T, platform string) (*Bridge, *sessionAccumulator, *forwardContext, interface {
		ReadMessage() (int, []byte, error)
		SetReadDeadline(time.Time) error
	}) {
		t.Helper()
		h := newTestHub(t)
		conn, server := newTestWSConnPair(t)
		t.Cleanup(func() { _ = conn.Close(); _ = server.Close() })
		c := newConn(h, conn, "s1", nil)
		h.JoinSession("s1", c)
		b := NewBridge(BridgeDeps{Log: slog.Default(), Hub: h})
		acc := &sessionAccumulator{}
		fc := &forwardContext{
			sessionID:     "s1",
			sessPlatform:  platform,
			turnStartTime: time.Now().Add(-5 * time.Second),
		}
		acc.TurnDurationMs = 5000
		return b, acc, fc, server
	}

	t.Run("triggers_when_no_text_with_tools", func(t *testing.T) {
		t.Parallel()
		b, acc, fc, server := setup(t, "feishu")
		acc.ToolCallCount.Store(3)
		acc.ToolNames = map[string]int{"Bash": 2, "Read": 1}

		b.maybeSendDoneFallback("s1", acc, fc)

		env := tryReadEnvelope(t, server)
		require.NotNil(t, env, "expected a fallback Message envelope")
		require.Equal(t, events.Message, env.Event.Type)
		data, ok := env.Event.Data.(map[string]any)
		require.True(t, ok, "event data should deserialize to map")
		content, _ := data["content"].(string)
		require.Contains(t, content, "3")
		require.Contains(t, content, "Bash×2")
		require.Contains(t, content, "Read×1")
	})

	t.Run("skipped_when_text_present", func(t *testing.T) {
		t.Parallel()
		b, acc, fc, server := setup(t, "feishu")
		acc.ToolCallCount.Store(3)
		fc.turnText.WriteString("real reply text")

		b.maybeSendDoneFallback("s1", acc, fc)

		require.Nil(t, tryReadEnvelope(t, server), "no fallback expected when turn has text")
	})

	t.Run("skipped_when_no_tools", func(t *testing.T) {
		t.Parallel()
		b, acc, fc, server := setup(t, "feishu")
		// ToolCallCount stays 0

		b.maybeSendDoneFallback("s1", acc, fc)

		require.Nil(t, tryReadEnvelope(t, server), "no fallback expected when no tool calls")
	})

	t.Run("skipped_for_webchat", func(t *testing.T) {
		t.Parallel()
		b, acc, fc, server := setup(t, platformWebChat)
		acc.ToolCallCount.Store(3)

		b.maybeSendDoneFallback("s1", acc, fc)

		require.Nil(t, tryReadEnvelope(t, server), "no fallback expected for webchat")
	})
}
