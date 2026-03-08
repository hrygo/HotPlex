package chatapps

import (
	"testing"

	"github.com/hrygo/hotplex/chatapps/base"
	slackadapter "github.com/hrygo/hotplex/chatapps/slack"
	"github.com/hrygo/hotplex/event"
	"github.com/hrygo/hotplex/provider"
	"github.com/slack-go/slack"
	"github.com/stretchr/testify/assert"
)

// TestProviderToSlackTokenDisplay verifies the complete data flow:
// Provider Event -> SessionStatsData -> ChatMessage -> Slack Block
func TestProviderToSlackTokenDisplay(t *testing.T) {
	p, _ := provider.NewClaudeCodeProvider(provider.ProviderConfig{}, nil)

	// Step 1: Parse Claude Code result event with usage data
	line := `{"type": "result", "result": "Task completed", "duration_ms": 2000, "usage": {"input_tokens": 1200, "output_tokens": 350, "cache_creation_input_tokens": 100, "cache_read_input_tokens": 50}, "total_cost_usd": 0.05}`

	events, err := p.ParseEvent(line)
	assert.NoError(t, err)
	assert.Len(t, events, 1)

	evt := events[0]
	assert.Equal(t, provider.EventTypeResult, evt.Type)
	assert.NotNil(t, evt.Metadata)

	// Step 2: Verify provider extracted tokens correctly
	assert.Equal(t, int32(1200), evt.Metadata.InputTokens)
	assert.Equal(t, int32(350), evt.Metadata.OutputTokens)
	assert.Equal(t, int32(100), evt.Metadata.CacheWriteTokens)
	assert.Equal(t, int32(50), evt.Metadata.CacheReadTokens)

	// Step 3: Simulate engine accumulating stats (like runner.go does)
	stats := &event.SessionStatsData{
		SessionID:       "test-session",
		TotalDurationMs: evt.Metadata.TotalDurationMs,
		InputTokens:     evt.Metadata.InputTokens,
		OutputTokens:    evt.Metadata.OutputTokens,
		ToolCallCount:   3,
		FilesModified:   2,
	}

	// Step 4: Create ChatMessage like engine_handler.go does
	chatMsg := &base.ChatMessage{
		Type:    base.MessageTypeSessionStats,
		Content: "",
		Metadata: map[string]any{
			"event_type":        "session_stats",
			"session_id":        stats.SessionID,
			"total_duration_ms": stats.TotalDurationMs,
			"input_tokens":      int64(stats.InputTokens),
			"output_tokens":     int64(stats.OutputTokens),
			"tool_call_count":   int64(stats.ToolCallCount),
			"files_modified":    int64(stats.FilesModified),
		},
	}

	// Step 5: Build Slack message
	builder := slackadapter.NewMessageBuilder(&slackadapter.Config{})
	blocks := builder.BuildSessionStatsMessage(chatMsg)

	// Step 6: Verify Slack message contains tokens
	assert.NotNil(t, blocks)
	assert.Len(t, blocks, 1)

	contextBlock, ok := blocks[0].(*slack.ContextBlock)
	assert.True(t, ok)
	assert.Len(t, contextBlock.ContextElements.Elements, 1)

	textElem, ok := contextBlock.ContextElements.Elements[0].(*slack.TextBlockObject)
	assert.True(t, ok)

	// Verify all expected stats are present
	assert.Contains(t, textElem.Text, "⏱️")
	assert.Contains(t, textElem.Text, "2s")
	assert.Contains(t, textElem.Text, "⚡")
	assert.Contains(t, textElem.Text, "1.2K/350")
	assert.Contains(t, textElem.Text, "📝")
	assert.Contains(t, textElem.Text, "2 files")
	assert.Contains(t, textElem.Text, "🔧")
	assert.Contains(t, textElem.Text, "3 tools")

	t.Logf("Generated Slack message: %s", textElem.Text)
}

// TestProviderToSlackTokenDisplay_NoUsage verifies behavior when usage is missing
func TestProviderToSlackTokenDisplay_NoUsage(t *testing.T) {
	p, _ := provider.NewClaudeCodeProvider(provider.ProviderConfig{}, nil)

	// Parse result event WITHOUT usage data
	line := `{"type": "result", "result": "Done", "duration_ms": 1000}`

	events, err := p.ParseEvent(line)
	assert.NoError(t, err)
	assert.Len(t, events, 1)

	evt := events[0]
	assert.NotNil(t, evt.Metadata)
	assert.Equal(t, int32(0), evt.Metadata.InputTokens)
	assert.Equal(t, int32(0), evt.Metadata.OutputTokens)

	// Simulate stats with zero tokens
	stats := &event.SessionStatsData{
		TotalDurationMs: 1000,
		InputTokens:     0,
		OutputTokens:    0,
	}

	chatMsg := &base.ChatMessage{
		Type:    base.MessageTypeSessionStats,
		Content: "",
		Metadata: map[string]any{
			"event_type":        "session_stats",
			"total_duration_ms": stats.TotalDurationMs,
			"input_tokens":      int64(stats.InputTokens),
			"output_tokens":     int64(stats.OutputTokens),
		},
	}

	builder := slackadapter.NewMessageBuilder(&slackadapter.Config{})
	blocks := builder.BuildSessionStatsMessage(chatMsg)

	// When no tokens/files/tools, only duration is shown (1 block with just duration)
	assert.Len(t, blocks, 1)

	contextBlock, ok := blocks[0].(*slack.ContextBlock)
	assert.True(t, ok)

	textElem, ok := contextBlock.ContextElements.Elements[0].(*slack.TextBlockObject)
	assert.True(t, ok)

	// Should contain duration but NOT tokens
	assert.Contains(t, textElem.Text, "⏱️")
	assert.Contains(t, textElem.Text, "1s")
	// Should NOT contain tokens emoji since both are 0
	assert.NotContains(t, textElem.Text, "⚡")
}
