package textutil

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParagraphBreaker_BelowThreshold(t *testing.T) {
	t.Parallel()
	var pb ParagraphBreaker
	text := strings.Repeat("a", 149) + "。"
	require.False(t, pb.Add(text))
}

func TestParagraphBreaker_TriggersAtThreshold(t *testing.T) {
	t.Parallel()
	var pb ParagraphBreaker
	// Accumulate to exactly 150 — not > 150, no break.
	require.False(t, pb.Add(strings.Repeat("a", 150)))
	// One more character with terminator triggers break.
	require.True(t, pb.Add("。"))
}

func TestParagraphBreaker_NoTerminatorNoBreak(t *testing.T) {
	t.Parallel()
	var pb ParagraphBreaker
	require.False(t, pb.Add(strings.Repeat("a", 500)))
}

func TestParagraphBreaker_CJKTerminator(t *testing.T) {
	t.Parallel()
	var pb ParagraphBreaker
	require.False(t, pb.Add(strings.Repeat("a", 150)))
	require.True(t, pb.Add("！"))
}

func TestParagraphBreaker_ResetAfterBreak(t *testing.T) {
	t.Parallel()
	var pb ParagraphBreaker
	require.False(t, pb.Add(strings.Repeat("a", 150)))
	require.True(t, pb.Add("。"))
	// Counter reset — fresh accumulation.
	require.False(t, pb.Add("hello"))
}

func TestParagraphBreaker_Reset(t *testing.T) {
	t.Parallel()
	var pb ParagraphBreaker
	require.False(t, pb.Add(strings.Repeat("a", 100)))
	pb.Reset()
	// After reset, need full threshold again.
	require.False(t, pb.Add(strings.Repeat("a", 50)+"。"))
}

// TestParagraphBreaker_Contract verifies the full caller contract:
// when Add returns true, the caller appends "\n\n" to produce
// paragraph separation in the output stream.
func TestParagraphBreaker_Contract(t *testing.T) {
	t.Parallel()
	var pb ParagraphBreaker

	var output strings.Builder
	deltas := []string{
		strings.Repeat("这是一段中文文本", 10) + "。", // 71 chars
		strings.Repeat("继续输出更多内容", 10) + "。", // 71 chars (total 142)
		"最后一句话。", // 6 chars (total 148)
		"新段落开始。", // 6 chars (total 154 > 150, triggers break)
	}
	for _, d := range deltas {
		if pb.Add(d) {
			output.WriteString(d + "\n\n")
		} else {
			output.WriteString(d)
		}
	}

	require.Contains(t, output.String(), "。\n\n", "break should insert \\n\\n after sentence terminator")
	// After break, counter reset — next accumulation starts fresh.
	require.False(t, pb.Add("short"), "counter should be reset after break")
}

// TestParagraphBreaker_DeltaDoneDeltaSequence verifies the cross-event
// sequence: streaming deltas → Done (reset) → new turn deltas.
// The Done event resets the counter so the next turn starts fresh.
func TestParagraphBreaker_DeltaDoneDeltaSequence(t *testing.T) {
	t.Parallel()
	var pb ParagraphBreaker

	// Turn 1: accumulate to near-threshold.
	require.False(t, pb.Add(strings.Repeat("a", 140)+"。"))
	// Done event resets.
	pb.Reset()
	// Turn 2: fresh counter — 50 chars should not trigger break.
	require.False(t, pb.Add(strings.Repeat("b", 50)+"。"),
		"after Done reset, 50 chars should not exceed threshold")
	// Continue turn 2 to trigger break.
	require.True(t, pb.Add(strings.Repeat("c", 101)+"。"),
		"50+102=152 > 150 with terminator should break")
}

// TestParagraphBreaker_MultipleBreaks verifies that paragraph breaks
// fire repeatedly in a long output (not just once).
func TestParagraphBreaker_MultipleBreaks(t *testing.T) {
	t.Parallel()
	var pb ParagraphBreaker

	breakCount := 0
	chunk := strings.Repeat("x", 151) + "。" // 152 chars, always triggers
	for i := 0; i < 5; i++ {
		if pb.Add(chunk) {
			breakCount++
		}
	}
	require.Equal(t, 5, breakCount, "should break on every chunk")
}

// TestParagraphBreaker_NeverBreaksWithoutTerminator verifies that
// the counter grows unbounded without a sentence terminator.
func TestParagraphBreaker_NeverBreaksWithoutTerminator(t *testing.T) {
	t.Parallel()
	var pb ParagraphBreaker

	for i := 0; i < 20; i++ {
		require.False(t, pb.Add(strings.Repeat("a", 100)),
			"iteration %d: no terminator should never break", i)
	}
}

// TestParagraphBreaker_ResetOnError verifies reset during error events.
func TestParagraphBreaker_ResetOnError(t *testing.T) {
	t.Parallel()
	var pb ParagraphBreaker

	// Accumulate heavily.
	require.False(t, pb.Add(strings.Repeat("a", 300)))
	// Error event resets.
	pb.Reset()
	// Fresh start — 50 chars should not break.
	require.False(t, pb.Add(strings.Repeat("a", 50)+"。"))
}
