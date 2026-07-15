package gateway

import (
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/aep"
	"github.com/hrygo/hotplex/pkg/events"
)

func TestProcessForwardedEvent_RetryPreservesTTFTUntilFinalDone(t *testing.T) {
	t.Parallel()

	const (
		sessionID   = "session-1"
		executionID = "execution-1"
	)
	start := time.Unix(100, 0)
	retryCtrl := NewLLMRetryController(config.AutoRetryConfig{
		Enabled:    true,
		MaxRetries: 1,
		BaseDelay:  time.Hour,
		MaxDelay:   time.Hour,
		RetryInput: "continue",
	}, slog.Default())
	b := NewBridge(BridgeDeps{Log: slog.Default(), Hub: newTestHub(t), RetryCtrl: retryCtrl})
	b.shutdownCancel()

	b.turnTTFT.begin(sessionID, executionID, worker.TypeCodexCLI, start)
	_, ok := b.turnTTFT.durableAccepted(sessionID, executionID, start.Add(100*time.Millisecond))
	require.True(t, ok)
	_, _, ok = b.turnTTFT.workerAccepted(sessionID, executionID, start.Add(200*time.Millisecond))
	require.True(t, ok)

	fc := &forwardContext{sessionID: sessionID, workerType: worker.TypeCodexCLI}
	fw := &mockBridgeWorker{workerType: worker.TypeCodexCLI}
	b.processForwardedEvent(events.NewEnvelope(aep.NewID(), "", 0, events.Error,
		events.ErrorData{Message: "rate limit exceeded"}), fw, forwardOpts{}, fc)
	b.processForwardedEvent(events.NewEnvelope(aep.NewID(), "", 0, events.Done,
		events.DoneData{Success: false}), fw, forwardOpts{}, fc)

	outputs := b.turnTTFT.firstOutput(sessionID, true, start.Add(time.Second))
	require.NotNil(t, outputs.ttft, "retry must preserve the TTFT record for the next worker output")
	require.NotNil(t, outputs.firstText)
}
