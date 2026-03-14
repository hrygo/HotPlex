// Package slack provides the Slack adapter implementation for the hotplex engine.
// Stats message builders for Slack Block Kit.
package slack

import (
	"fmt"
	"strings"

	"github.com/hrygo/hotplex/chatapps/base"
	"github.com/slack-go/slack"
)

// StatsMessageBuilder builds stats-related Slack messages (SessionStats, CommandProgress, CommandComplete)
type StatsMessageBuilder struct{}

// NewStatsMessageBuilder creates a new StatsMessageBuilder
func NewStatsMessageBuilder() *StatsMessageBuilder {
	return &StatsMessageBuilder{}
}

// BuildSessionStatsMessage builds a message for session statistics
// Implements EventTypeResult (Turn Complete) per spec - compact single-line format
func (b *StatsMessageBuilder) BuildSessionStatsMessage(msg *base.ChatMessage) []slack.Block {
	var blocks []slack.Block

	// Build compact stats line: ⏱️ duration • ⚡ tokens in/out • 📝 files • 🔧 tools
	if msg.Metadata != nil {
		var stats []string

		// Total Duration (from total_duration_ms in SessionStats.ToSummary)
		if duration := extractInt64(msg.Metadata, "total_duration_ms"); duration > 0 {
			stats = append(stats, "⏱️ "+FormatDuration(duration))
		}

		// Tokens (show in/out separately with cache info)
		// input_tokens/output_tokens already include cache tokens from API
		tokensIn := extractInt64(msg.Metadata, "input_tokens")
		tokensOut := extractInt64(msg.Metadata, "output_tokens")
		cacheRead := extractInt64(msg.Metadata, "cache_read_tokens")
		cacheWrite := extractInt64(msg.Metadata, "cache_write_tokens")
		if tokensIn > 0 || tokensOut > 0 {
			// Show cache info if available: "⚡ 100K/50K (cache: 10K/5K)"
			if cacheRead > 0 || cacheWrite > 0 {
				stats = append(stats, fmt.Sprintf("⚡ %s/%s (cache: %s/%s)",
					formatTokenCount(tokensIn), formatTokenCount(tokensOut),
					formatTokenCount(cacheRead), formatTokenCount(cacheWrite)))
			} else {
				stats = append(stats, fmt.Sprintf("⚡ %s/%s", formatTokenCount(tokensIn), formatTokenCount(tokensOut)))
			}
		}

		// Files modified
		if files := extractInt64(msg.Metadata, "files_modified"); files > 0 {
			stats = append(stats, fmt.Sprintf("📝 %d files", files))
		}

		// Tool calls (from tool_call_count in SessionStats.ToSummary)
		if tools := extractInt64(msg.Metadata, "tool_call_count"); tools > 0 {
			stats = append(stats, fmt.Sprintf("🔧 %d tools", tools))
		}

		if len(stats) > 0 {
			statsText := slack.NewTextBlockObject("mrkdwn", strings.Join(stats, " • "), false, false)
			blocks = append(blocks, slack.NewContextBlock("", statsText))
		}
	}

	return blocks
}

// extractInt64 extracts int64 value from metadata, supporting both int32 and int64 types
func extractInt64(metadata map[string]any, key string) int64 {
	if v, ok := metadata[key].(int64); ok {
		return v
	}
	if v, ok := metadata[key].(int32); ok {
		return int64(v)
	}
	return 0
}

// formatTokenCount formats token count in compact form (1.2K)
// formatTokenCount formats token count in compact form (1.2K, 1.00M)
// Uses proper threshold: K for < 1M, M for >= 1M
func formatTokenCount(count int64) string {
	if count >= 1000000 {
		return fmt.Sprintf("%.2fM", float64(count)/1000000)
	}
	if count >= 1000 {
		kValue := float64(count) / 1000
		// If k value >= 999.5, show M to avoid rounding issues (999.9K -> 1000.0K)
		if kValue >= 999.5 {
			return fmt.Sprintf("%.2fM", float64(count)/1000000)
		}
		// Use integer for >= 100K
		if kValue >= 100 {
			return fmt.Sprintf("%.0fK", kValue)
		}
		return fmt.Sprintf("%.1fK", kValue)
	}
	return fmt.Sprintf("%d", count)
}

// BuildCommandProgressMessage builds a message for command progress updates
// Implements EventTypeCommandProgress per spec (17)
// Block type: section + context + actions
func (b *StatsMessageBuilder) BuildCommandProgressMessage(msg *base.ChatMessage) []slack.Block {
	title := msg.Content
	if title == "" {
		title = "Executing command..."
	}

	// Get command name from metadata
	commandName := ""
	if msg.Metadata != nil {
		if cmd, ok := msg.Metadata["command"].(string); ok {
			commandName = cmd
		}
	}

	headerText := "⚙️ " + commandName
	if commandName == "" {
		headerText = "⚙️ Executing"
	}

	mrkdwn := slack.NewTextBlockObject("mrkdwn", headerText+"\n"+title, false, false)

	var blocks []slack.Block
	blocks = append(blocks, slack.NewSectionBlock(mrkdwn, nil, nil))

	// Add progress steps from metadata if available
	if msg.Metadata != nil {
		if steps, ok := msg.Metadata["steps"].([]string); ok && len(steps) > 0 {
			var stepTexts []string
			for i, step := range steps {
				stepTexts = append(stepTexts, fmt.Sprintf("○ Step %d: %s", i+1, step))
			}
			stepsText := strings.Join(stepTexts, "\n")
			stepsObj := slack.NewTextBlockObject("mrkdwn", stepsText, false, false)
			blocks = append(blocks, slack.NewSectionBlock(stepsObj, nil, nil))

			// Per spec: context block with progress indicator
			progressText := slack.NewTextBlockObject("mrkdwn", fmt.Sprintf("Progress: %d steps", len(steps)), false, false)
			blocks = append(blocks, slack.NewContextBlock("", progressText))
		}
	}

	// Per spec: do not add cancel button for command progress messages
	// Command execution cannot be cancelled by user
	return blocks
}

// BuildCommandCompleteMessage builds a single-line compact Context Block for command completion
// Format: ⚡ {cmd} 执行完成 ({completed}/{total} | 耗时: {dur})
func (b *StatsMessageBuilder) BuildCommandCompleteMessage(msg *base.ChatMessage) []slack.Block {
	title := msg.Content
	if title == "" {
		title = "Command completed"
	}

	commandName := ""
	var durationMs int64
	var completedSteps, totalSteps int
	if msg.Metadata != nil {
		if cmd, ok := msg.Metadata["command"].(string); ok {
			commandName = cmd
		}
		if dur, ok := msg.Metadata["duration_ms"].(int64); ok {
			durationMs = dur
		}
		if completed, ok := msg.Metadata["completed_steps"].(int); ok {
			completedSteps = completed
		}
		if total, ok := msg.Metadata["total_steps"].(int); ok {
			totalSteps = total
		}
	}

	line := "⚡ "
	if commandName != "" {
		line += "`" + commandName + "` "
	}
	line += title

	var extras []string
	if totalSteps > 0 {
		extras = append(extras, fmt.Sprintf("%d/%d steps", completedSteps, totalSteps))
	}
	if durationMs > 0 {
		extras = append(extras, "⏱️ "+FormatDuration(durationMs))
	}
	if len(extras) > 0 {
		line += "  |  " + strings.Join(extras, "  |  ")
	}

	text := slack.NewTextBlockObject("mrkdwn", line, false, false)
	return []slack.Block{slack.NewContextBlock("", text)}
}
