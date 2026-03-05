// Package slack provides the Slack adapter implementation for the hotplex engine.
package slack

import (
	"testing"

	"github.com/hrygo/hotplex/chatapps/base"
	"github.com/slack-go/slack"
	"github.com/stretchr/testify/assert"
)

// =============================================================================
// ToolMessageBuilder Tests
// =============================================================================

func TestToolMessageBuilder_BuildToolUseMessage(t *testing.T) {
	formatter := NewMrkdwnFormatter()
	builder := NewToolMessageBuilder(formatter)

	tests := []struct {
		name     string
		msg      *base.ChatMessage
		wantLen  int
		contains string
	}{
		{
			name: "basic tool use",
			msg: &base.ChatMessage{
				Type:    base.MessageTypeToolUse,
				Content: "Bash",
				Metadata: map[string]any{
					"input": "ls -la",
				},
			},
			wantLen:  1,
			contains: "Bash",
		},
		{
			name: "tool with input summary",
			msg: &base.ChatMessage{
				Type:    base.MessageTypeToolUse,
				Content: "Read",
				Metadata: map[string]any{
					"input_summary": "README.md",
				},
			},
			wantLen:  1,
			contains: "Read",
		},
		{
			name: "empty content defaults to Unknown Tool",
			msg: &base.ChatMessage{
				Type:     base.MessageTypeToolUse,
				Content:  "",
				Metadata: map[string]any{},
			},
			wantLen:  1,
			contains: "Unknown Tool",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blocks := builder.BuildToolUseMessage(tt.msg)
			assert.Len(t, blocks, tt.wantLen)
			assertBlockContains(t, blocks, tt.contains)
		})
	}
}

func TestToolMessageBuilder_BuildToolResultMessage(t *testing.T) {
	formatter := NewMrkdwnFormatter()
	builder := NewToolMessageBuilder(formatter)

	tests := []struct {
		name    string
		msg     *base.ChatMessage
		wantLen int
	}{
		{
			name: "successful result",
			msg: &base.ChatMessage{
				Type:    base.MessageTypeToolResult,
				Content: "file contents",
				Metadata: map[string]any{
					"success":     true,
					"tool_name":   "Read",
					"duration_ms": int64(1500),
				},
			},
			wantLen: 1,
		},
		{
			name: "failed result with error preview",
			msg: &base.ChatMessage{
				Type:    base.MessageTypeToolResult,
				Content: "error: file not found",
				Metadata: map[string]any{
					"success":   false,
					"tool_name": "Read",
				},
			},
			wantLen: 2, // status + error preview
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blocks := builder.BuildToolResultMessage(tt.msg)
			assert.Len(t, blocks, tt.wantLen)
		})
	}
}

// =============================================================================
// AnswerMessageBuilder Tests
// =============================================================================

func TestAnswerMessageBuilder_BuildAnswerMessage(t *testing.T) {
	formatter := NewMrkdwnFormatter()
	builder := NewAnswerMessageBuilder(formatter)

	tests := []struct {
		name    string
		msg     *base.ChatMessage
		wantNil bool
	}{
		{
			name: "simple answer",
			msg: &base.ChatMessage{
				Type:    base.MessageTypeAnswer,
				Content: "Hello, world!",
			},
			wantNil: false,
		},
		{
			name: "empty content returns nil",
			msg: &base.ChatMessage{
				Type:    base.MessageTypeAnswer,
				Content: "",
			},
			wantNil: true,
		},
		{
			name: "long content gets chunked",
			msg: &base.ChatMessage{
				Type:    base.MessageTypeAnswer,
				Content: string(make([]byte, 5000)), // long content
			},
			wantNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blocks := builder.BuildAnswerMessage(tt.msg)
			if tt.wantNil {
				assert.Nil(t, blocks)
			} else {
				assert.NotNil(t, blocks)
			}
		})
	}
}

func TestAnswerMessageBuilder_BuildErrorMessage(t *testing.T) {
	formatter := NewMrkdwnFormatter()
	builder := NewAnswerMessageBuilder(formatter)

	msg := &base.ChatMessage{
		Type:    base.MessageTypeError,
		Content: "Something went wrong",
	}

	blocks := builder.BuildErrorMessage(msg)
	assert.Len(t, blocks, 1)
	assertBlockContains(t, blocks, "Error")
}

// =============================================================================
// PlanMessageBuilder Tests
// =============================================================================

func TestPlanMessageBuilder_BuildPlanModeMessage(t *testing.T) {
	builder := NewPlanMessageBuilder()

	msg := &base.ChatMessage{
		Type:    base.MessageTypePlanMode,
		Content: "1. Read file\n2. Modify code\n3. Test",
	}

	blocks := builder.BuildPlanModeMessage(msg)
	assert.NotEmpty(t, blocks)
}

func TestPlanMessageBuilder_BuildExitPlanModeMessage(t *testing.T) {
	builder := NewPlanMessageBuilder()

	msg := &base.ChatMessage{
		Type:    base.MessageTypeExitPlanMode,
		Content: "Plan ready for approval",
		Metadata: map[string]any{
			"session_id": "test-session-123",
		},
	}

	blocks := builder.BuildExitPlanModeMessage(msg)
	assert.Len(t, blocks, 4) // header + section + divider + actions
}

func TestPlanMessageBuilder_BuildAskUserQuestionMessage(t *testing.T) {
	builder := NewPlanMessageBuilder()

	t.Run("question without options", func(t *testing.T) {
		msg := &base.ChatMessage{
			Type:    base.MessageTypeAskUserQuestion,
			Content: "What would you like to do?",
		}
		blocks := builder.BuildAskUserQuestionMessage(msg)
		assert.Len(t, blocks, 1)
	})

	t.Run("question with options", func(t *testing.T) {
		msg := &base.ChatMessage{
			Type:    base.MessageTypeAskUserQuestion,
			Content: "Choose an option",
			Metadata: map[string]any{
				"options":    []string{"Option A", "Option B"},
				"session_id": "test-session",
			},
		}
		blocks := builder.BuildAskUserQuestionMessage(msg)
		assert.Len(t, blocks, 2) // section + actions
	})
}

// =============================================================================
// InteractiveMessageBuilder Tests
// =============================================================================

func TestInteractiveMessageBuilder_BuildDangerBlockMessage(t *testing.T) {
	builder := NewInteractiveMessageBuilder()

	msg := &base.ChatMessage{
		Type:    base.MessageTypeDangerBlock,
		Content: "This action will delete files",
		Metadata: map[string]any{
			"session_id": "danger-session",
		},
	}

	blocks := builder.BuildDangerBlockMessage(msg)
	assert.Len(t, blocks, 4) // header + section + divider + actions
}

// =============================================================================
// StatsMessageBuilder Tests
// =============================================================================

func TestStatsMessageBuilder_BuildSessionStatsMessage(t *testing.T) {
	builder := NewStatsMessageBuilder()

	msg := &base.ChatMessage{
		Type: base.MessageTypeSessionStats,
		Metadata: map[string]any{
			"total_duration_ms": int64(5000),
			"input_tokens":      int64(1000),
			"output_tokens":     int64(500),
			"files_modified":    int64(3),
			"tool_call_count":   int64(5),
		},
	}

	blocks := builder.BuildSessionStatsMessage(msg)
	assert.NotEmpty(t, blocks)
}

// =============================================================================
// SystemMessageBuilder Tests
// =============================================================================

func TestSystemMessageBuilder_BuildSystemMessage(t *testing.T) {
	builder := NewSystemMessageBuilder()

	t.Run("with content", func(t *testing.T) {
		msg := &base.ChatMessage{
			Type:    base.MessageTypeSystem,
			Content: "System notification",
		}
		blocks := builder.BuildSystemMessage(msg)
		assert.Len(t, blocks, 1)
	})

	t.Run("empty content returns nil", func(t *testing.T) {
		msg := &base.ChatMessage{
			Type:    base.MessageTypeSystem,
			Content: "",
		}
		blocks := builder.BuildSystemMessage(msg)
		assert.Nil(t, blocks)
	})
}

func TestSystemMessageBuilder_BuildStepMessages(t *testing.T) {
	builder := NewSystemMessageBuilder()

	t.Run("step start", func(t *testing.T) {
		msg := &base.ChatMessage{
			Type:    base.MessageTypeStepStart,
			Content: "Reading files",
			Metadata: map[string]any{
				"step":  1,
				"total": 3,
			},
		}
		blocks := builder.BuildStepStartMessage(msg)
		assert.Len(t, blocks, 1)
		assertBlockContains(t, blocks, "Step 1/3")
	})

	t.Run("step finish", func(t *testing.T) {
		msg := &base.ChatMessage{
			Type:    base.MessageTypeStepFinish,
			Content: "Completed",
			Metadata: map[string]any{
				"step":        1,
				"duration_ms": int64(500),
			},
		}
		blocks := builder.BuildStepFinishMessage(msg)
		assert.Len(t, blocks, 1)
		assertBlockContains(t, blocks, "Step 1")
	})
}

// =============================================================================
// Helper Functions
// =============================================================================

// assertBlockContains checks if any block contains the expected text
func assertBlockContains(t *testing.T, blocks []slack.Block, expected string) {
	t.Helper()
	found := false
	for _, block := range blocks {
		switch b := block.(type) {
		case *slack.SectionBlock:
			if b.Text != nil && containsStr(b.Text.Text, expected) {
				found = true
			}
		case *slack.ContextBlock:
			for _, el := range b.ContextElements.Elements {
				if textEl, ok := el.(*slack.TextBlockObject); ok {
					if containsStr(textEl.Text, expected) {
						found = true
					}
				}
			}
		case *slack.HeaderBlock:
			if b.Text != nil && containsStr(b.Text.Text, expected) {
				found = true
			}
		}
	}
	assert.True(t, found, "expected to find %q in blocks", expected)
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStrHelper(s, substr))
}

func containsStrHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
