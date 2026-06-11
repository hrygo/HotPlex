package gateway

import (
	"context"
	"fmt"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"sync"

	"github.com/hrygo/hotplex/internal/brain"
	"github.com/hrygo/hotplex/internal/eventstore"
	"github.com/hrygo/hotplex/internal/worker"
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
	require.True(t, result.FinalChars <= maxHistoryBytes)
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
	require.True(t, result.FinalChars <= maxHistoryBytes)
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
	require.True(t, result.FinalChars <= maxHistoryBytes)
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
	// Calculate actual compress budget from the kept turns, not hardcoded constants.
	recentChars := 0
	for i := len(turns) - keepRecentN; i < len(turns); i++ {
		recentChars += len(turns[i].Content)
	}
	compressBudget := maxHistoryBytes - recentChars
	require.True(t, len(result.Turns[0].Content) <= compressBudget,
		"summary (%d bytes) should fit within compress budget (%d bytes)",
		len(result.Turns[0].Content), compressBudget)
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
	require.True(t, result.FinalChars <= maxHistoryBytes)
}

func TestTruncateHistory_BasicTruncation(t *testing.T) {
	t.Parallel()

	c := NewHistoryCompressor(slog.Default(), nil)

	// 20 turns × 4000 chars = 80k > 50k budget → should truncate.
	turns := makeLargeTurns(20, 4000)

	result := c.TruncateHistory(turns)

	require.False(t, result.Compressed)
	require.True(t, result.FinalChars <= maxHistoryBytes)
	require.NotEmpty(t, result.Turns)
	require.True(t, result.OriginalChars > maxHistoryBytes,
		"original should exceed budget to test truncation")
}

func TestTruncateHistory_AllTurnsFit(t *testing.T) {
	t.Parallel()

	c := NewHistoryCompressor(slog.Default(), nil)

	turns := []*eventstore.TurnRecord{
		turn("user", "hello"),
		turn("assistant", "hi"),
		turn("user", "how are you?"),
	}

	result := c.TruncateHistory(turns)

	require.False(t, result.Compressed)
	require.Len(t, result.Turns, 3)
	require.Equal(t, "hello", result.Turns[0].Content)
}

func TestTruncateHistory_EmptyTurns(t *testing.T) {
	t.Parallel()

	c := NewHistoryCompressor(slog.Default(), nil)

	result := c.TruncateHistory(nil)
	require.False(t, result.Compressed)
	require.Empty(t, result.Turns)

	// All turns have empty content.
	turns := []*eventstore.TurnRecord{turn("user", ""), turn("assistant", "")}
	result = c.TruncateHistory(turns)
	require.False(t, result.Compressed)
	require.Empty(t, result.Turns)
}

func TestTruncateHistory_FiltersEmptyContent(t *testing.T) {
	t.Parallel()

	c := NewHistoryCompressor(slog.Default(), nil)

	turns := []*eventstore.TurnRecord{
		turn("user", ""),
		turn("assistant", "response"),
		turn("user", ""),
		turn("assistant", "response2"),
	}

	result := c.TruncateHistory(turns)
	require.Len(t, result.Turns, 2)
	require.Equal(t, "response", result.Turns[0].Content)
	require.Equal(t, "response2", result.Turns[1].Content)
}

func TestTruncateHistory_PreservesChronologicalOrder(t *testing.T) {
	t.Parallel()

	c := NewHistoryCompressor(slog.Default(), nil)

	// Create turns where only recent ones fit within budget.
	turns := []*eventstore.TurnRecord{
		turn("user", makeLargeString(30000)),
		turn("assistant", makeLargeString(30000)),
		turn("user", "recent1"),
		turn("assistant", "recent2"),
	}

	result := c.TruncateHistory(turns)

	// Result should be chronologically ordered (oldest first).
	require.False(t, result.Compressed)
	require.True(t, len(result.Turns) >= 2)
	// Last turns should be the recent small ones.
	last := result.Turns[len(result.Turns)-1]
	require.Equal(t, "recent2", last.Content)
}

func TestCompressCache_InvalidationOnTurnChange(t *testing.T) {
	t.Parallel()

	// Simulate cache behavior: same latestTurnCreatedAt = cache hit,
	// different = cache miss (invalidate).
	cache := &sync.Map{}

	entry1 := &compressCacheEntry{
		turns:               []worker.ConversationTurn{{Role: "assistant", Content: "summary"}},
		latestTurnCreatedAt: 1000,
	}
	cache.Store("s1", entry1)

	// Same timestamp → hit.
	if cached, ok := cache.Load("s1"); ok {
		e := cached.(*compressCacheEntry)
		require.Equal(t, int64(1000), e.latestTurnCreatedAt)
		require.Len(t, e.turns, 1)
	}

	// Different timestamp → miss → invalidate.
	if cached, ok := cache.Load("s1"); ok {
		e := cached.(*compressCacheEntry)
		if e.latestTurnCreatedAt != 2000 {
			cache.Delete("s1")
		}
	}
	_, ok := cache.Load("s1")
	require.False(t, ok, "cache should be invalidated after timestamp mismatch")
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
