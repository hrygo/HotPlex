package gateway

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hrygo/hotplex/internal/brain"
	"github.com/hrygo/hotplex/internal/brain/llm"
	"github.com/hrygo/hotplex/internal/eventstore"
	"github.com/hrygo/hotplex/internal/worker"
)

// ─── Constants ────────────────────────────────────────────────────────

const (
	maxHistoryBytes        = 50000  // total byte budget for injected history (~12.5k tokens)
	keepRecentN            = 4      // number of recent turns to keep uncompressed
	brainInputCap          = 100000 // Brain LLM input limit in bytes
	compressThresholdRatio = 1.2    // only compress when total > budget × ratio (adaptive)
	compressTimeout        = 45 * time.Second
)

// ─── Types ────────────────────────────────────────────────────────────

// HistoryCompressor uses Brain to intelligently compress conversation history
// when it exceeds the character budget, preserving recent turns verbatim.
type HistoryCompressor struct {
	log *slog.Logger
	hub *Hub // for progress notifications; nil = silent
}

// CompressResult holds the outcome of a compression attempt.
type CompressResult struct {
	Turns         []worker.ConversationTurn
	Compressed    bool
	OriginalChars int
	FinalChars    int
}

// compressorBrain is a local interface for Brain dependency injection.
// Production code injects brain.Global(); tests inject a mock.
type compressorBrain interface {
	ChatWithOptions(ctx context.Context, prompt string, opts brain.ChatOptions) (string, error)
}

// ─── Constructor ──────────────────────────────────────────────────────

func NewHistoryCompressor(log *slog.Logger, hub *Hub) *HistoryCompressor {
	return &HistoryCompressor{log: log, hub: hub}
}

// ─── Core Algorithm ──────────────────────────────────────────────────

// CompressHistory compresses older conversation turns using Brain when
// total content exceeds the character budget. Recent turns are preserved
// verbatim for fresh context. Falls back to truncation on any failure.
func (c *HistoryCompressor) CompressHistory(
	ctx context.Context,
	turns []*eventstore.TurnRecord,
	sessionID string,
	brainFn func() compressorBrain,
) CompressResult {
	// 1. Filter empty-content turns and build preliminary list.
	filtered := make([]turnWithChars, 0, len(turns))
	totalChars := 0
	for _, t := range turns {
		if t.Content == "" {
			continue
		}
		filtered = append(filtered, turnWithChars{
			role:      t.Role,
			content:   t.Content,
			createdAt: t.CreatedAt,
		})
		totalChars += len(t.Content)
	}

	if len(filtered) == 0 {
		return CompressResult{}
	}

	// 2. Self-adaptive: skip compression for moderate overruns.
	threshold := int(float64(maxHistoryBytes) * compressThresholdRatio)
	if totalChars <= threshold {
		return c.truncateResult(filtered)
	}

	// 3. Partition into compress group (older) and keep group (recent).
	splitIdx := max(len(filtered)-keepRecentN, 0)
	compressGroup := filtered[:splitIdx]
	keepGroup := filtered[splitIdx:]

	// If no older turns to compress, just truncate the keep group.
	if len(compressGroup) == 0 {
		return c.truncateResult(filtered)
	}

	// 4. Calculate budgets.
	recentChars := sumChars(keepGroup)
	compressBudget := maxHistoryBytes - recentChars
	if compressBudget <= 0 {
		// Recent turns alone exceed budget — hard truncate recent group.
		c.log.Warn("history: recent turns exceed budget, truncating",
			"session_id", sessionID,
			"recent_chars", recentChars,
			"budget", maxHistoryBytes)
		return c.truncateResult(filtered)
	}

	// 5. Send progress notification.
	c.notifyProgress(sessionID, len(compressGroup), totalChars)

	// 6. Format compress group into text block.
	compressText := formatTurns(compressGroup)
	if compressText == "" {
		// All turns had non-user/assistant roles; nothing compressible.
		return c.truncateResult(filtered)
	}

	// Pre-truncate if exceeds brain input cap (drop oldest first).
	if len(compressText) > brainInputCap {
		compressText = truncateHead(compressText, brainInputCap)
		c.log.Debug("history: pre-truncated compress input to brain cap",
			"session_id", sessionID,
			"capped", len(compressText))
	}
	compressChars := len(compressText) // recalculate after potential truncation

	// 7. Call Brain for compression.
	result, ok := c.callBrain(ctx, sessionID, compressText, len(compressGroup), compressChars, compressBudget, brainFn)
	if !ok {
		// Brain failed — fall back to truncation.
		return c.truncateResult(filtered)
	}

	// 8. Build final history: [summary turn] + [recent turns].
	final := make([]worker.ConversationTurn, 0, 1+len(keepGroup))
	final = append(final, worker.ConversationTurn{
		Role:    "assistant",
		Content: result,
	})
	for _, t := range keepGroup {
		final = append(final, worker.ConversationTurn{
			Role:    t.role,
			Content: t.content,
		})
	}

	finalChars := len(result) + recentChars
	compressRatio := "N/A"
	if compressChars > 0 {
		compressRatio = fmt.Sprintf("%.0f%%", (1.0-float64(len(result))/float64(compressChars))*100)
	}
	c.log.Info("history: compressed conversation history",
		"session_id", sessionID,
		"original_chars", totalChars,
		"final_chars", finalChars,
		"compress_ratio", compressRatio,
		"turns_compressed", len(compressGroup),
		"turns_kept", len(keepGroup))

	return CompressResult{
		Turns:         final,
		Compressed:    true,
		OriginalChars: totalChars,
		FinalChars:    finalChars,
	}
}

// ─── Brain Interaction ────────────────────────────────────────────────

func (c *HistoryCompressor) callBrain(
	ctx context.Context,
	sessionID, compressText string,
	turnCount, compressChars, compressBudget int,
	brainFn func() compressorBrain,
) (string, bool) {
	b := brainFn()
	if b == nil {
		c.log.Warn("history: brain not configured, falling back to truncation",
			"session_id", sessionID)
		return "", false
	}

	systemPrompt := fmt.Sprintf(historyCompressSystemPrompt, compressBudget, compressBudget/4)
	userPrompt := fmt.Sprintf(historyCompressUserTemplate,
		turnCount, compressChars, compressText, compressBudget)

	compressCtx, cancel := context.WithTimeout(ctx, compressTimeout)
	defer cancel()

	opts := brain.ChatOptions{
		MaxTokens:    4096,
		Temperature:  llm.FloatPtr(0.3),
		SystemPrompt: systemPrompt,
	}

	result, err := b.ChatWithOptions(compressCtx, userPrompt, opts)
	if err != nil {
		c.log.Warn("history: brain compression failed, falling back to truncation",
			"session_id", sessionID, "error", err)
		return "", false
	}

	result = strings.TrimSpace(result)
	if result == "" {
		c.log.Warn("history: brain returned empty summary, falling back to truncation",
			"session_id", sessionID)
		return "", false
	}

	// Hard-truncate summary if still over budget (no double-compression).
	if len(result) > compressBudget {
		result = truncateAtBoundary(result, compressBudget)
		c.log.Warn("history: summary exceeded budget, truncated",
			"session_id", sessionID,
			"summary_len", len(result),
			"budget", compressBudget)
	}

	return result, true
}

// ─── Progress Notification ────────────────────────────────────────────

func (c *HistoryCompressor) notifyProgress(sessionID string, compressCount, totalChars int) {
	if c.hub == nil {
		return
	}
	msg := fmt.Sprintf("正在压缩对话历史（%d 条 → 摘要，共 %d 字符）...", compressCount, totalChars)
	seq := c.hub.NextSeq(sessionID)
	env := buildNotifyEnvelope(sessionID, msg, seq)
	// Best-effort send; failure is non-critical.
	_ = c.hub.SendToSession(context.Background(), env)
}

// ─── Helpers ──────────────────────────────────────────────────────────

type turnWithChars struct {
	role      string
	content   string
	createdAt int64 // unix millis from TurnRecord.CreatedAt
}

func sumChars(turns []turnWithChars) int {
	total := 0
	for _, t := range turns {
		total += len(t.content)
	}
	return total
}

func formatTurns(turns []turnWithChars) string {
	var sb strings.Builder
	for _, t := range turns {
		switch t.role {
		case "user":
			sb.WriteString("[User")
		case "assistant":
			sb.WriteString("[Assistant")
		default:
			continue
		}
		if t.createdAt > 0 {
			sb.WriteString(" ")
			sb.WriteString(time.UnixMilli(t.createdAt).Format("2006-01-02 15:04"))
		}
		sb.WriteString("]: ")
		sb.WriteString(t.content)
		sb.WriteString("\n\n")
	}
	return sb.String()
}

// truncateResult builds a CompressResult by truncating turns to fit within budget.
// Iterates from newest to oldest so that when the first (oldest) turn alone
// exceeds the budget, we still return the most recent turns that fit.
func (c *HistoryCompressor) truncateResult(turns []turnWithChars) CompressResult {
	history := make([]worker.ConversationTurn, 0, len(turns))
	bytesUsed := 0
	for i := len(turns) - 1; i >= 0; i-- {
		t := turns[i]
		if bytesUsed+len(t.content) > maxHistoryBytes {
			break
		}
		bytesUsed += len(t.content)
		history = append(history, worker.ConversationTurn{
			Role:    t.role,
			Content: t.content,
		})
	}
	// Reverse to restore chronological order.
	for i, j := 0, len(history)-1; i < j; i, j = i+1, j-1 {
		history[i], history[j] = history[j], history[i]
	}
	if len(history) == 0 && len(turns) > 0 {
		c.log.Warn("history: all turns exceed byte budget, history empty",
			"turn_count", len(turns),
			"budget", maxHistoryBytes,
			"largest_turn_bytes", len(turns[len(turns)-1].content))
	}
	return CompressResult{
		Turns:         history,
		Compressed:    false,
		OriginalChars: sumChars(turns),
		FinalChars:    bytesUsed,
	}
}

// truncateAtBoundary truncates s to at most maxLen bytes, breaking at
// the last newline within the limit for cleaner output.
// maxLen is a byte budget (not rune count); we back up to a valid UTF-8
// boundary to avoid splitting multi-byte characters.
func truncateAtBoundary(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	// Scan backward for a newline within the byte budget.
	for i := maxLen - 1; i >= 0; i-- {
		if s[i] == '\n' {
			return s[:i]
		}
	}
	// No newline found; back up to a valid UTF-8 boundary.
	for maxLen > 0 && !utf8.RuneStart(s[maxLen]) {
		maxLen--
	}
	return s[:maxLen]
}

// truncateHead truncates the text from the head (dropping oldest content)
// to fit within maxLen bytes. Scans forward to find the first newline after
// maxLen to avoid splitting mid-turn.
func truncateHead(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	// Advance to valid UTF-8 boundary first.
	for maxLen < len(s) && !utf8.RuneStart(s[maxLen]) {
		maxLen++
	}
	// Scan forward for a clean break point.
	for i := maxLen; i < len(s) && i < maxLen+1024; i++ {
		if s[i] == '\n' {
			return s[i+1:]
		}
	}
	return s[maxLen:]
}

// ─── Prompt Templates ─────────────────────────────────────────────────

const historyCompressSystemPrompt = `你是一位对话历史压缩助手。你的任务是将多轮对话历史压缩为简洁的摘要。

输出规范：
1. 纯文本，保留关键技术术语、文件名、决策点和结论
2. 控制在 %d 字节以内（约 %d tokens）
3. 使用 [User] 和 [Assistant] 标记保持对话结构
4. 保留所有代码变更描述和文件路径
5. 保留错误信息和解决方案
6. 压缩率目标：将原始内容压缩到 30-40%%（去除 60-70%% 的冗余内容）

压缩策略：
- 合并相似主题的多轮对话
- 省略重复的确认/否定回复和中间调试过程
- 保留所有工具调用结果的关键信息
- 保留用户明确要求/决策的完整表述
- 时间线标记：[较早] ... [中间] ... [较近]`

const historyCompressUserTemplate = `请将以下 %d 轮对话历史（共 %d 字符）压缩为简洁摘要：

%s

要求：压缩后控制在 %d 字节以内，保留关键上下文、技术细节和决策点。输出压缩率 60-70%%。`
