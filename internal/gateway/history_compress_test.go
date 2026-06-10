package gateway

import (
	"context"
	"fmt"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/brain"
	"github.com/hrygo/hotplex/internal/eventstore"
)

// mockBrain implements compressorBrain for testing.
type mockBrain struct {
	chatFn func(ctx context.Context, prompt string, opts brain.ChatOptions) (string, error)
}

func (m *mockBrain) ChatWithOptions(ctx context.Context, prompt string, opts brain.ChatOptions) (string, error) {
	if m.chatFn == nil {
		return "", fmt.Errorf("mock brain: no chatFn configured")
	}
	return m.chatFn(ctx, prompt, opts)
}

func mockBrainFn(b compressorBrain) func() compressorBrain {
	return func() compressorBrain { return b }
}

func nilBrainFn() compressorBrain { return nil }

// helper to create TurnRecord pointers.
func turn(role, content string) *eventstore.TurnRecord {
	return &eventstore.TurnRecord{Role: role, Content: content}
}

func TestCompressHistory_NoCompressionNeeded(t *testing.T) {
	t.Parallel()

	c := NewHistoryCompressor(slog.Default(), nil)
	turns := []*eventstore.TurnRecord{
		turn("user", "hello"),
		turn("assistant", "hi there"),
	}

	result := c.CompressHistory(context.Background(), turns, "s1", nilBrainFn)

	require.False(t, result.Compressed)
	require.Len(t, result.Turns, 2)
	require.Equal(t, "hello", result.Turns[0].Content)
}

func TestCompressHistory_EmptyTurns(t *testing.T) {
	t.Parallel()

	c := NewHistoryCompressor(slog.Default(), nil)

	result := c.CompressHistory(context.Background(), nil, "s1", nilBrainFn)
	require.False(t, result.Compressed)
	require.Empty(t, result.Turns)

	// All turns have empty content.
	turns := []*eventstore.TurnRecord{
		turn("user", ""),
		turn("assistant", ""),
	}
	result = c.CompressHistory(context.Background(), turns, "s1", nilBrainFn)
	require.False(t, result.Compressed)
	require.Empty(t, result.Turns)
}

func TestCompressHistory_BrainNotConfigured(t *testing.T) {
	t.Parallel()

	c := NewHistoryCompressor(slog.Default(), nil)

	// Create turns that exceed the threshold (60k+ chars).
	turns := makeLargeTurns(20, 4000) // 80k total chars

	result := c.CompressHistory(context.Background(), turns, "s1", nilBrainFn)

	// Should fall back to truncation.
	require.False(t, result.Compressed)
	require.True(t, result.FinalChars <= maxHistoryChars)
	require.NotEmpty(t, result.Turns)
}

func TestCompressHistory_BrainCallFails(t *testing.T) {
	t.Parallel()

	c := NewHistoryCompressor(slog.Default(), nil)
	turns := makeLargeTurns(20, 4000) // 80k total

	mock := &mockBrain{
		chatFn: func(ctx context.Context, prompt string, opts brain.ChatOptions) (string, error) {
			return "", fmt.Errorf("LLM unavailable")
		},
	}

	result := c.CompressHistory(context.Background(), turns, "s1", mockBrainFn(mock))

	require.False(t, result.Compressed)
	require.True(t, result.FinalChars <= maxHistoryChars)
}

func TestCompressHistory_SuccessfulCompression(t *testing.T) {
	t.Parallel()

	c := NewHistoryCompressor(slog.Default(), nil)
	turns := makeLargeTurns(20, 4000) // 80k total, compress group = 16 turns, keep = 4

	summary := "[摘要] 用户讨论了多个技术问题和解决方案..."
	mock := &mockBrain{
		chatFn: func(ctx context.Context, prompt string, opts brain.ChatOptions) (string, error) {
			return summary, nil
		},
	}

	result := c.CompressHistory(context.Background(), turns, "s1", mockBrainFn(mock))

	require.True(t, result.Compressed)
	// 1 summary + 4 recent turns = 5
	require.Len(t, result.Turns, 5)
	require.Equal(t, "assistant", result.Turns[0].Role)
	require.Equal(t, summary, result.Turns[0].Content)

	// Recent turns preserved verbatim.
	require.Equal(t, "user", result.Turns[1].Role)
	require.Equal(t, turns[len(turns)-4].Content, result.Turns[1].Content)

	require.True(t, result.FinalChars < result.OriginalChars)
	require.True(t, result.FinalChars <= maxHistoryChars)
}

func TestCompressHistory_SummaryExceedsBudget(t *testing.T) {
	t.Parallel()

	c := NewHistoryCompressor(slog.Default(), nil)
	turns := makeLargeTurns(17, 4000) // 68k total > 60k threshold

	// Brain returns a summary that's way too long.
	longSummary := makeLargeString(80000)
	mock := &mockBrain{
		chatFn: func(ctx context.Context, prompt string, opts brain.ChatOptions) (string, error) {
			return longSummary, nil
		},
	}

	result := c.CompressHistory(context.Background(), turns, "s1", mockBrainFn(mock))

	require.True(t, result.Compressed)
	require.True(t, len(result.Turns[0].Content) <= maxHistoryChars)
}

func TestCompressHistory_BrainReturnsEmpty(t *testing.T) {
	t.Parallel()

	c := NewHistoryCompressor(slog.Default(), nil)
	turns := makeLargeTurns(20, 4000)

	mock := &mockBrain{
		chatFn: func(ctx context.Context, prompt string, opts brain.ChatOptions) (string, error) {
			return "", nil
		},
	}

	result := c.CompressHistory(context.Background(), turns, "s1", mockBrainFn(mock))
	require.False(t, result.Compressed)
}

func TestCompressHistory_AllTurnsFitInRecentGroup(t *testing.T) {
	t.Parallel()

	c := NewHistoryCompressor(slog.Default(), nil)

	// Only 3 turns, all fit in keepRecentN=4 — no compression even though
	// they might exceed the threshold.
	turns := []*eventstore.TurnRecord{
		turn("user", makeLargeString(30000)),
		turn("assistant", makeLargeString(30000)),
		turn("user", makeLargeString(30000)),
	}
	// total = 90k > 60k threshold, but splitIdx = max(0, 3-4) = 0
	// compressGroup = turns[:0] = empty → truncateResult

	result := c.CompressHistory(context.Background(), turns, "s1", nilBrainFn)
	require.False(t, result.Compressed)
}

func TestCompressHistory_RecentTurnsExceedBudget(t *testing.T) {
	t.Parallel()

	c := NewHistoryCompressor(slog.Default(), nil)

	// 6 turns, each 20k chars. Recent 4 = 80k > 50k budget.
	turns := makeLargeTurns(6, 20000)

	result := c.CompressHistory(context.Background(), turns, "s1", nilBrainFn)

	require.False(t, result.Compressed)
	require.True(t, result.FinalChars <= maxHistoryChars)
}

// ─── Helpers ──────────────────────────────────────────────────────────

func makeLargeTurns(count, charsPerTurn int) []*eventstore.TurnRecord {
	turns := make([]*eventstore.TurnRecord, count)
	for i := range count {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		turns[i] = &eventstore.TurnRecord{
			Role:    role,
			Content: makeLargeString(charsPerTurn),
		}
	}
	return turns
}

func makeLargeString(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'A' + byte(i%26)
	}
	return string(b)
}
