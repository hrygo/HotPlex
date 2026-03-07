package slack

import (
	"testing"

	"github.com/hrygo/hotplex/chatapps/base"
	"github.com/slack-go/slack"
	"github.com/stretchr/testify/assert"
)

func TestBuildSessionStatsMessage_Int32Types(t *testing.T) {
	// This test verifies that BuildSessionStatsMessage correctly handles
	// int32 types from SessionStatsData (the actual types used in production)
	builder := NewMessageBuilder()

	msg := &base.ChatMessage{
		Type:    base.MessageTypeSessionStats,
		Content: "",
		Metadata: map[string]any{
			"event_type":        "session_stats",
			"session_id":        "sess_123",
			"total_duration_ms": int64(12500),
			"input_tokens":      int32(1200), // int32 as from SessionStatsData
			"output_tokens":     int32(350),  // int32 as from SessionStatsData
			"tool_call_count":   int32(3),    // int32 as from SessionStatsData
			"files_modified":    int32(2),    // int32 as from SessionStatsData
		},
	}

	blocks := builder.BuildSessionStatsMessage(msg)

	assert.NotNil(t, blocks)
	assert.Len(t, blocks, 1) // Single context block with stats

	// Verify context block contains stats
	contextBlock, ok := blocks[0].(*slack.ContextBlock)
	assert.True(t, ok)
	assert.NotNil(t, contextBlock)
}

func TestBuildSessionStatsMessage_Int64Types(t *testing.T) {
	// This test verifies backward compatibility with int64 types
	builder := NewMessageBuilder()

	msg := &base.ChatMessage{
		Type:    base.MessageTypeSessionStats,
		Content: "",
		Metadata: map[string]any{
			"event_type":        "session_stats",
			"total_duration_ms": int64(12500),
			"input_tokens":      int64(1200),
			"output_tokens":     int64(350),
			"tool_call_count":   int64(3),
			"files_modified":    int64(2),
		},
	}

	blocks := builder.BuildSessionStatsMessage(msg)

	assert.NotNil(t, blocks)
	assert.Len(t, blocks, 1) // Single context block with stats
}

func TestBuildSessionStatsMessage_Empty(t *testing.T) {
	// When no stats are available, should return empty/nil blocks
	builder := NewMessageBuilder()

	msg := &base.ChatMessage{
		Type:    base.MessageTypeSessionStats,
		Content: "",
		Metadata: map[string]any{
			"event_type": "session_stats",
		},
	}

	blocks := builder.BuildSessionStatsMessage(msg)

	// No stats means no blocks (only raw stats are displayed)
	assert.Len(t, blocks, 0)
}

func TestBuildSessionStatsMessage_WithAllFields(t *testing.T) {
	// Full test with all stats fields populated
	builder := NewMessageBuilder()

	msg := &base.ChatMessage{
		Type:    base.MessageTypeSessionStats,
		Content: "",
		Metadata: map[string]any{
			"event_type":        "session_stats",
			"session_id":        "sess_123",
			"total_duration_ms": int64(12500),
			"input_tokens":      int32(1200),
			"output_tokens":     int32(350),
			"tool_call_count":   int32(3),
			"files_modified":    int32(2),
		},
	}

	blocks := builder.BuildSessionStatsMessage(msg)

	assert.NotNil(t, blocks)
	assert.Len(t, blocks, 1)

	// Verify the stats line contains expected emojis and values
	contextBlock, ok := blocks[0].(*slack.ContextBlock)
	assert.True(t, ok)
	assert.NotNil(t, contextBlock)

	// Verify context block contains single text element with all stats joined by " • "
	assert.Len(t, contextBlock.ContextElements.Elements, 1)

	textElem, ok := contextBlock.ContextElements.Elements[0].(*slack.TextBlockObject)
	assert.True(t, ok)

	// Check all expected stats are present in the text
	assert.Contains(t, textElem.Text, "⏱️")
	assert.Contains(t, textElem.Text, "12s")

	assert.Contains(t, textElem.Text, "⚡")
	assert.Contains(t, textElem.Text, "1.2K/350")

	assert.Contains(t, textElem.Text, "📝")
	assert.Contains(t, textElem.Text, "2 files")

	assert.Contains(t, textElem.Text, "🔧")
	assert.Contains(t, textElem.Text, "3 tools")
}

func TestExtractInt64(t *testing.T) {
	tests := []struct {
		name     string
		metadata map[string]any
		key      string
		expected int64
	}{
		{
			name: "int64 value",
			metadata: map[string]any{
				"key": int64(100),
			},
			key:      "key",
			expected: 100,
		},
		{
			name: "int32 value",
			metadata: map[string]any{
				"key": int32(100),
			},
			key:      "key",
			expected: 100,
		},
		{
			name: "missing key",
			metadata: map[string]any{
				"other": int64(50),
			},
			key:      "key",
			expected: 0,
		},
		{
			name:     "nil metadata",
			metadata: nil,
			key:      "key",
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractInt64(tt.metadata, tt.key)
			assert.Equal(t, tt.expected, result)
		})
	}
}
