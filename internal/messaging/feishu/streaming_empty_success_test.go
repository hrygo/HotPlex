package feishu

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/messaging"
)

// TestStreamingCardController_Close_PlaceholderEmptySuccess (RC-3/Fix C, T-C2):
// when Close runs with an empty buffer and the placeholder still active (no
// real assistant content ever arrived), the card must replace the placeholder
// with a retryable empty-success terminal instead of keeping the placeholder
// or treating lastFlushed==placeholder as "already completed content".
func TestStreamingCardController_Close_PlaceholderEmptySuccess(t *testing.T) {
	t.Parallel()
	c := newTestStreamingCtrl()

	require.True(t, c.transition(PhaseCreating))
	require.True(t, c.transition(PhaseStreaming))

	// Reproduce the SendPlaceholder state: buf empty, lastFlushed == placeholder,
	// placeholder active. This is exactly the state RC-3 misclassified.
	c.mu.Lock()
	c.placeholder = ":Get: hello\n:StatusFlashOfInspiration: tip"
	c.lastFlushed = c.placeholder
	c.cardKitOK = false // degraded → no flush side effects, focus on the decision
	c.cardID = ""
	c.msgID = ""
	c.streamingActive = false
	c.mu.Unlock()

	err := c.Close(context.Background())
	require.NoError(t, err)

	c.mu.Lock()
	defer c.mu.Unlock()
	require.Empty(t, c.placeholder, "placeholder must be cleared on empty-success close")
	require.Contains(t, c.lastFlushed, "未收到可展示",
		"lastFlushed must hold the empty-success terminal, not the placeholder text")
}

// TestStreamingCardController_Close_RealPartialKept (T-C4): when real partial
// content was flushed (placeholder already cleared) and the final buffer is
// empty, the existing partial must be retained — NOT replaced by a terminal.
func TestStreamingCardController_Close_RealPartialKept(t *testing.T) {
	t.Parallel()
	c := newTestStreamingCtrl()

	require.True(t, c.transition(PhaseCreating))
	require.True(t, c.transition(PhaseStreaming))

	c.mu.Lock()
	c.placeholder = "" // already cleared by a prior real flush
	c.lastFlushed = "real partial reply that streamed earlier"
	c.buf.Reset()
	c.cardKitOK = false
	c.cardID = ""
	c.msgID = ""
	c.streamingActive = false
	c.mu.Unlock()

	err := c.Close(context.Background())
	require.NoError(t, err)

	c.mu.Lock()
	defer c.mu.Unlock()
	require.NotContains(t, c.lastFlushed, "未收到可展示",
		"real partial content must NOT be replaced by an empty-success terminal")
}

// TestFormatEmptySuccess_NonEmpty ensures the terminal text is non-empty so a
// card can never be finalized with blank content (CardKit rejects empty).
func TestFormatEmptySuccess_NonEmpty(t *testing.T) {
	text := messaging.FormatEmptySuccess()
	require.NotEmpty(t, text)
}
