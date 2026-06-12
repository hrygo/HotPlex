package gateway

import (
	"context"
	"fmt"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

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

func TestTruncateHistory_OversizedTurnTruncated(t *testing.T) {
	t.Parallel()

	c := NewHistoryCompressor(slog.Default(), nil)

	// Single turn that exceeds maxHistoryBytes — must be truncated, not discarded.
	turns := []*eventstore.TurnRecord{
		turn("assistant", makeLargeString(80000)),
	}

	result := c.TruncateHistory(turns)

	require.False(t, result.Compressed)
	require.Len(t, result.Turns, 1, "oversized turn should be truncated, not discarded")
	require.True(t, len(result.Turns[0].Content) <= maxHistoryBytes)
	require.True(t, len(result.Turns[0].Content) > 0, "truncated content must not be empty")
	require.True(t, result.FinalChars <= maxHistoryBytes)
}

func TestTruncateHistory_NewestTurnPreservedWhenOversized(t *testing.T) {
	t.Parallel()

	c := NewHistoryCompressor(slog.Default(), nil)

	// Two oversized turns — newest should always get at least partial content.
	turns := []*eventstore.TurnRecord{
		turn("user", makeLargeString(60000)),
		turn("assistant", makeLargeString(60000)),
	}

	result := c.TruncateHistory(turns)

	require.False(t, result.Compressed)
	require.NotEmpty(t, result.Turns, "at least the newest turn must be preserved")
	// Newest turn (chronologically last) should have partial content.
	last := result.Turns[len(result.Turns)-1]
	require.True(t, len(last.Content) > 0, "newest turn must have content")
	require.True(t, result.FinalChars <= maxHistoryBytes)
}

func TestResolveCachedHistory_AllBranches(t *testing.T) {
	t.Parallel()

	t.Run("cache miss", func(t *testing.T) {
		b := &Bridge{}
		turns, hit := b.resolveCachedHistory("miss-session", 1000)
		require.False(t, hit)
		require.Nil(t, turns)
	})

	t.Run("cache hit", func(t *testing.T) {
		b := &Bridge{}
		expected := []worker.ConversationTurn{{Role: "assistant", Content: "summary"}}
		b.compressCache.Store("hit-session", &compressCacheEntry{
			turns:               expected,
			latestTurnCreatedAt: 1000,
		})
		turns, hit := b.resolveCachedHistory("hit-session", 1000)
		require.True(t, hit)
		require.Equal(t, expected, turns)
	})

	t.Run("stale cache invalidated", func(t *testing.T) {
		b := &Bridge{}
		b.compressCache.Store("stale-session", &compressCacheEntry{
			turns:               []worker.ConversationTurn{{Role: "assistant", Content: "old"}},
			latestTurnCreatedAt: 1000,
		})
		turns, hit := b.resolveCachedHistory("stale-session", 2000)
		require.False(t, hit)
		require.Nil(t, turns)
		_, exists := b.compressCache.Load("stale-session")
		require.False(t, exists, "stale entry should be deleted")
	})

	t.Run("invalid type deleted", func(t *testing.T) {
		b := &Bridge{}
		b.compressCache.Store("bad-type-session", "not-a-cache-entry")
		turns, hit := b.resolveCachedHistory("bad-type-session", 1000)
		require.False(t, hit)
		require.Nil(t, turns)
		_, exists := b.compressCache.Load("bad-type-session")
		require.False(t, exists, "invalid entry should be deleted")
	})
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
