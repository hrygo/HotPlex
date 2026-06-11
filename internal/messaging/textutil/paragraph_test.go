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
