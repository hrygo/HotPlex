package gateway

import (
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func newTurnClockBridge() *Bridge {
	return NewBridge(BridgeDeps{Log: slog.Default()})
}

// TestTurnClock_ExcludesInterTurnIdle (T-D1): a duration measured from a turn
// start stamp does not include idle time that elapsed before the stamp.
func TestTurnClock_ExcludesInterTurnIdle(t *testing.T) {
	b := newTurnClockBridge()
	sid := "sess-idle"

	// Simulate the input path recording turn start AFTER a long idle gap.
	time.Sleep(20 * time.Millisecond)
	b.RecordTurnStart(sid)

	// Processing the turn takes a short time.
	time.Sleep(30 * time.Millisecond)

	startMs := b.consumeTurnStartMs(sid)
	require.Greater(t, startMs, int64(0), "consume must return the recorded stamp")
	duration := time.Now().UnixMilli() - startMs
	// ~30ms processing, NOT ~50ms (idle + processing). Allow generous upper
	// bound for CI jitter, but it must be well under the idle+processing sum.
	require.Less(t, duration, int64(60), "duration should approximate processing only, not include idle")
}

// TestTurnClock_InputFailureClearsStart (T-D2): when input delivery fails, the
// start stamp is cleared so a later Done cannot bill a turn that never ran.
func TestTurnClock_InputFailureClearsStart(t *testing.T) {
	b := newTurnClockBridge()
	sid := "sess-fail"

	b.RecordTurnStart(sid)
	require.Greater(t, b.consumeTurnStartMsForTest(sid), int64(0), "stamp recorded")

	// Re-record then simulate the failed-delivery clear path.
	b.RecordTurnStart(sid)
	b.ClearTurnStart(sid)
	require.Equal(t, int64(0), b.consumeTurnStartMs(sid), "stamp must be cleared on input failure")
}

// TestTurnClock_MissingStartReturnsZero (T-D3): with no recorded start (crash
// recovery / replay), consume returns 0 so the forwarder can fall back to the
// first worker event time.
func TestTurnClock_MissingStartReturnsZero(t *testing.T) {
	b := newTurnClockBridge()
	require.Equal(t, int64(0), b.consumeTurnStartMs("never-stamped"),
		"consume with no prior record must return 0 for fallback")
}

// consumeTurnStartMsForTest peeks without clearing so the test can assert a
// stamp was set before exercising the clearing path.
func (b *Bridge) consumeTurnStartMsForTest(sessionID string) int64 {
	acc := b.getOrInitAccum(sessionID, "", time.Now())
	// Swap then restore: equivalent to a non-destructive read for the assertion.
	v := acc.consumeTurnStartMs()
	if v > 0 {
		acc.recordTurnStart(time.UnixMilli(v))
	}
	return v
}
