package gateway

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/worker"
)

func TestTurnTTFTTracker_RecordsEarlyFirstOutputOnceAndFirstTextSeparately(t *testing.T) {
	t.Parallel()
	tracker := newTurnTTFTTracker()
	start := time.Unix(100, 0)

	tracker.begin("session-1", "execution-1", worker.TypeCodexCLI, start)
	admission, ok := tracker.durableAccepted("session-1", "execution-1", start.Add(100*time.Millisecond))
	require.True(t, ok)
	require.Equal(t, 100*time.Millisecond, admission.duration)

	outputs := tracker.firstOutput("session-1", false, start.Add(time.Second))
	require.Nil(t, outputs.ttft, "output may arrive before Worker.Input returns")

	dispatch, outputs, ok := tracker.workerAccepted("session-1", "execution-1", start.Add(250*time.Millisecond))
	require.True(t, ok)
	require.Equal(t, 150*time.Millisecond, dispatch.duration)
	require.NotNil(t, outputs.ttft)
	require.Equal(t, time.Second, outputs.ttft.duration)
	require.NotNil(t, outputs.firstOutputStage)
	require.Equal(t, 750*time.Millisecond, outputs.firstOutputStage.duration)
	require.Nil(t, outputs.firstText, "reasoning must not count as first text")

	outputs = tracker.firstOutput("session-1", true, start.Add(1500*time.Millisecond))
	require.Nil(t, outputs.ttft, "multiple worker events must not create a second TTFT sample")
	require.NotNil(t, outputs.firstText)
	require.Equal(t, 1500*time.Millisecond, outputs.firstText.duration)

	outputs = tracker.firstOutput("session-1", true, start.Add(2*time.Second))
	require.Nil(t, outputs.ttft)
	require.Nil(t, outputs.firstText, "multiple text deltas must not create a second first-text sample")

	workerType, withoutOutput := tracker.finish("session-1")
	require.Equal(t, worker.TypeCodexCLI, workerType)
	require.False(t, withoutOutput)
}

func TestTurnTTFTTracker_ClassifiesTerminalTurnWithoutOutput(t *testing.T) {
	t.Parallel()
	tracker := newTurnTTFTTracker()
	tracker.begin("session-1", "execution-1", worker.TypeCodexCLI, time.Now())

	workerType, withoutOutput := tracker.finish("session-1")
	require.Equal(t, worker.TypeCodexCLI, workerType)
	require.True(t, withoutOutput)

	_, withoutOutput = tracker.finish("session-1")
	require.False(t, withoutOutput, "terminal handling must be idempotent")
}
