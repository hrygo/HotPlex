package slack

import (
	"testing"
	"time"

	"github.com/hrygo/hotplex/chatapps/base"
	"github.com/slack-go/slack"
	"github.com/stretchr/testify/assert"
)

// =============================================================================
// ToolMessageBuilder Tests
// =============================================================================

func TestToolMessageBuilder_BuildToolUseMessage(t *testing.T) {
	builder := NewMessageBuilder(&Config{})

	msg := &base.ChatMessage{
		Type:    base.MessageTypeToolUse,
		Content: "Creating a new file",
		Metadata: map[string]any{
			"tool_name": "Write",
			"tool_input": map[string]any{
				"file_path": "/test/main.go",
				"content":   "package main",
			},
		},
	}

	blocks := builder.Build(msg)

	assert.NotNil(t, blocks)
	assert.GreaterOrEqual(t, len(blocks), 1)
}

func TestToolMessageBuilder_BuildToolResultMessage(t *testing.T) {
	builder := NewMessageBuilder(&Config{})

	msg := &base.ChatMessage{
		Type:    base.MessageTypeToolResult,
		Content: "File created successfully",
		Metadata: map[string]any{
			"tool_name":   "Write",
			"is_error":    false,
			"tool_output": "Created /test/main.go",
		},
	}

	blocks := builder.Build(msg)

	assert.NotNil(t, blocks)
	assert.GreaterOrEqual(t, len(blocks), 1)
}

// =============================================================================
// AnswerMessageBuilder Tests
// =============================================================================

func TestAnswerMessageBuilder_BuildAnswerMessage(t *testing.T) {
	builder := NewMessageBuilder(&Config{})

	msg := &base.ChatMessage{
		Type:    base.MessageTypeAnswer,
		Content: "This is the AI's answer to the user.",
		Metadata: map[string]any{
			"message_ts": "1234567890.123456",
		},
	}

	blocks := builder.Build(msg)

	assert.NotNil(t, blocks)
	assert.GreaterOrEqual(t, len(blocks), 1)
}

func TestAnswerMessageBuilder_BuildErrorMessage(t *testing.T) {
	builder := NewMessageBuilder(&Config{})

	msg := &base.ChatMessage{
		Type:    base.MessageTypeError,
		Content: "Something went wrong: connection timeout",
		Metadata: map[string]any{
			"error_code": "TIMEOUT",
		},
	}

	blocks := builder.Build(msg)

	assert.NotNil(t, blocks)
	assert.GreaterOrEqual(t, len(blocks), 1)
}

// =============================================================================
// PlanMessageBuilder Tests
// =============================================================================

func TestPlanMessageBuilder_BuildPlanModeMessage(t *testing.T) {
	builder := NewMessageBuilder(&Config{})

	msg := &base.ChatMessage{
		Type:    base.MessageTypePlanMode,
		Content: "I have created a plan for you to review.",
		Metadata: map[string]any{
			"plan_steps": []map[string]any{
				{"id": "1", "description": "Step 1: Analyze requirements"},
				{"id": "2", "description": "Step 2: Implement solution"},
			},
		},
	}

	blocks := builder.Build(msg)

	assert.NotNil(t, blocks)
}

func TestPlanMessageBuilder_BuildExitPlanModeMessage(t *testing.T) {
	builder := NewMessageBuilder(&Config{})

	msg := &base.ChatMessage{
		Type:     base.MessageTypeExitPlanMode,
		Content:  "Exiting plan mode",
		Metadata: map[string]any{},
	}

	blocks := builder.Build(msg)

	assert.NotNil(t, blocks)
}

func TestPlanMessageBuilder_BuildAskUserQuestionMessage(t *testing.T) {
	builder := NewMessageBuilder(&Config{})

	msg := &base.ChatMessage{
		Type:    base.MessageTypeAskUserQuestion,
		Content: "Would you like to proceed with this approach?",
		Metadata: map[string]any{
			"options": []string{"Yes, proceed", "No, stop", "Show alternatives"},
		},
	}

	blocks := builder.Build(msg)

	assert.NotNil(t, blocks)
}

// =============================================================================
// InteractiveMessageBuilder Tests
// =============================================================================

func TestInteractiveMessageBuilder_BuildDangerBlockMessage(t *testing.T) {
	builder := NewMessageBuilder(&Config{})

	msg := &base.ChatMessage{
		Type:    base.MessageTypeDangerBlock,
		Content: "Warning: This action cannot be undone. Are you sure?",
		Metadata: map[string]any{
			"confirm_text":   "Yes, delete",
			"cancel_text":    "Cancel",
			"confirm_action": "danger_confirm",
			"cancel_action":  "danger_cancel",
		},
	}

	blocks := builder.Build(msg)

	assert.NotNil(t, blocks)
}

// =============================================================================
// StatsMessageBuilder Tests
// =============================================================================

func TestStatsMessageBuilder_BuildSessionStatsMessage(t *testing.T) {
	builder := NewMessageBuilder(&Config{})

	msg := &base.ChatMessage{
		Type:    base.MessageTypeSessionStats,
		Content: "",
		Metadata: map[string]any{
			"session_id":        "sess_abc123",
			"total_duration_ms": int64(30000),
			"input_tokens":      int32(1500),
			"output_tokens":     int32(800),
			"tool_call_count":   int32(5),
			"files_modified":    int32(3),
		},
	}

	blocks := builder.BuildSessionStatsMessage(msg)

	assert.NotNil(t, blocks)
	assert.GreaterOrEqual(t, len(blocks), 1)

	// Verify it's a context block with stats
	if len(blocks) > 0 {
		_, ok := blocks[0].(*slack.ContextBlock)
		assert.True(t, ok, "Expected ContextBlock for stats")
	}
}

func TestStatsMessageBuilder_BuildCommandProgressMessage(t *testing.T) {
	builder := NewMessageBuilder(&Config{})

	msg := &base.ChatMessage{
		Type:    base.MessageTypeCommandProgress,
		Content: "Processing files...",
		Metadata: map[string]any{
			"command":  "git commit",
			"progress": 50,
			"total":    100,
		},
	}

	blocks := builder.Build(msg)

	assert.NotNil(t, blocks)
}

func TestStatsMessageBuilder_BuildCommandCompleteMessage(t *testing.T) {
	builder := NewMessageBuilder(&Config{})

	msg := &base.ChatMessage{
		Type:    base.MessageTypeCommandComplete,
		Content: "Command completed successfully",
		Metadata: map[string]any{
			"command":    "git push",
			"exit_code":  0,
			"duration_s": 2.5,
		},
	}

	blocks := builder.Build(msg)

	assert.NotNil(t, blocks)
}

// =============================================================================
// SystemMessageBuilder Tests
// =============================================================================

func TestSystemMessageBuilder_BuildSystemMessage(t *testing.T) {
	builder := NewMessageBuilder(&Config{})

	msg := &base.ChatMessage{
		Type:    base.MessageTypeSystem,
		Content: "System initialized",
		Metadata: map[string]any{
			"timestamp": time.Now().Unix(),
		},
	}

	blocks := builder.Build(msg)

	assert.NotNil(t, blocks)
}

func TestSystemMessageBuilder_BuildUserMessage(t *testing.T) {
	builder := NewMessageBuilder(&Config{})

	msg := &base.ChatMessage{
		Type:    base.MessageTypeUser,
		Content: "User input message",
		Metadata: map[string]any{
			"user_id": "U123456",
		},
	}

	blocks := builder.Build(msg)

	assert.NotNil(t, blocks)
}

func TestSystemMessageBuilder_BuildStepStartMessage(t *testing.T) {
	builder := NewMessageBuilder(&Config{})

	msg := &base.ChatMessage{
		Type:    base.MessageTypeStepStart,
		Content: "Starting step: Code Review",
		Metadata: map[string]any{
			"step_id":   "step_1",
			"step_name": "Code Review",
		},
	}

	blocks := builder.Build(msg)

	assert.NotNil(t, blocks)
}

func TestSystemMessageBuilder_BuildStepFinishMessage(t *testing.T) {
	builder := NewMessageBuilder(&Config{})

	msg := &base.ChatMessage{
		Type:    base.MessageTypeStepFinish,
		Content: "Step completed: Code Review",
		Metadata: map[string]any{
			"step_id":     "step_1",
			"step_name":   "Code Review",
			"duration_ms": int64(5000),
		},
	}

	blocks := builder.Build(msg)

	assert.NotNil(t, blocks)
}

func TestSystemMessageBuilder_BuildRawMessage(t *testing.T) {
	builder := NewMessageBuilder(&Config{})

	msg := &base.ChatMessage{
		Type:    base.MessageTypeRaw,
		Content: "Raw message content",
		Metadata: map[string]any{
			"raw_blocks": []slack.Block{
				&slack.SectionBlock{Text: &slack.TextBlockObject{Type: "mrkdwn", Text: "Raw"}},
			},
		},
	}

	blocks := builder.Build(msg)

	assert.NotNil(t, blocks)
}

func TestSystemMessageBuilder_BuildUserMessageReceivedMessage(t *testing.T) {
	builder := NewMessageBuilder(&Config{})

	msg := &base.ChatMessage{
		Type:    base.MessageTypeUserMessageReceived,
		Content: "Message received from user",
		Metadata: map[string]any{
			"message_ts": "1234567890.123456",
		},
	}

	blocks := builder.Build(msg)

	assert.NotNil(t, blocks)
}

// =============================================================================
// Integration Tests - Build Routing
// =============================================================================

func TestBuild_RoutesToCorrectSubBuilder(t *testing.T) {
	builder := NewMessageBuilder(&Config{})

	testCases := []struct {
		msgType    base.MessageType
		msgContent string
		expectNil  bool // true if message type is handled by StatusManager (no blocks)
	}{
		{base.MessageTypeToolUse, "tool use", false},
		{base.MessageTypeToolResult, "tool result", false},
		{base.MessageTypeAnswer, "answer", false},
		{base.MessageTypeError, "error", false},
		{base.MessageTypePlanMode, "plan mode", false},
		{base.MessageTypeSystem, "system", false},
		// SessionStats is handled by StatusManager, returns nil blocks
		{base.MessageTypeSessionStats, "stats", true},
	}

	for _, tc := range testCases {
		t.Run(string(tc.msgType), func(t *testing.T) {
			msg := &base.ChatMessage{
				Type:    tc.msgType,
				Content: tc.msgContent,
			}

			blocks := builder.Build(msg)
			if tc.expectNil {
				// SessionStats and similar types are handled by StatusManager
				assert.Nil(t, blocks, "Expected nil blocks for %s (handled by StatusManager)", tc.msgType)
			} else {
				assert.NotNil(t, blocks, "Expected non-nil blocks for %s", tc.msgType)
			}
		})
	}
}

func TestBuild_DefaultToAnswerForUnknownType(t *testing.T) {
	builder := NewMessageBuilder(&Config{})

	msg := &base.ChatMessage{
		Type:    base.MessageType("unknown_type"),
		Content: "Unknown message",
	}

	blocks := builder.Build(msg)

	// Should fallback to answer message
	assert.NotNil(t, blocks)
}

// =============================================================================
// FormatDuration Tests
// =============================================================================

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		input    int64
		expected string
	}{
		// Milliseconds
		{0, "0ms"},
		{100, "100ms"},
		{500, "500ms"},
		{999, "999ms"},
		// Seconds
		{1000, "1s"},
		{5000, "5s"},
		{30000, "30s"},
		{59000, "59s"},
		// Minutes
		{60000, "1m"},
		{90000, "1m 30s"},
		{120000, "2m"},
		{1800000, "30m"},
		{3540000, "59m"},
		// Hours
		{3600000, "1h"},
		{4500000, "1h 15m"},
		{5400000, "1h 30m"},
		{7200000, "2h"},
		{18000000, "5h"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := FormatDuration(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
