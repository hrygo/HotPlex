package hotplex

import (
	"encoding/json"
	"testing"
)

func TestStreamMessage_JSON_Unmarshal(t *testing.T) {
	// Test unmarshaling a result message
	jsonStr := `{
		"type": "result",
		"duration_ms": 1000,
		"is_error": false,
		"total_cost_usd": 0.05,
		"usage": {
			"input_tokens": 100,
			"output_tokens": 50
		}
	}`

	var msg StreamMessage
	err := json.Unmarshal([]byte(jsonStr), &msg)
	if err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if msg.Type != "result" {
		t.Errorf("Type = %q, want 'result'", msg.Type)
	}
	if msg.Duration != 1000 {
		t.Errorf("Duration = %d, want 1000", msg.Duration)
	}
	if msg.Usage == nil {
		t.Fatal("Usage should not be nil")
	}
	if msg.Usage.InputTokens != 100 {
		t.Errorf("InputTokens = %d, want 100", msg.Usage.InputTokens)
	}
}

func TestStreamMessage_JSON_ToolUse(t *testing.T) {
	jsonStr := `{
		"type": "tool_use",
		"name": "bash",
		"content": [
			{"type": "tool_use", "id": "tool-123", "input": {"command": "ls"}}
		]
	}`

	var msg StreamMessage
	err := json.Unmarshal([]byte(jsonStr), &msg)
	if err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if msg.Type != "tool_use" {
		t.Errorf("Type = %q", msg.Type)
	}
	if msg.Name != "bash" {
		t.Errorf("Name = %q, want 'bash'", msg.Name)
	}
}

func TestStreamMessage_JSON_Assistant(t *testing.T) {
	jsonStr := `{
		"type": "assistant",
		"message": {
			"id": "msg-123",
			"role": "assistant",
			"content": [
				{"type": "text", "text": "Hello!"}
			]
		}
	}`

	var msg StreamMessage
	err := json.Unmarshal([]byte(jsonStr), &msg)
	if err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if msg.Message == nil {
		t.Fatal("Message should not be nil")
	}
	if len(msg.Message.Content) != 1 {
		t.Errorf("len(Content) = %d, want 1", len(msg.Message.Content))
	}
	if msg.Message.Content[0].Text != "Hello!" {
		t.Errorf("Text = %q", msg.Message.Content[0].Text)
	}
}

func TestContentBlock_JSON_ToolResult(t *testing.T) {
	jsonStr := `{
		"type": "tool_result",
		"tool_use_id": "tool-456",
		"content": "command output",
		"is_error": false
	}`

	var block ContentBlock
	err := json.Unmarshal([]byte(jsonStr), &block)
	if err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if block.Type != "tool_result" {
		t.Errorf("Type = %q", block.Type)
	}
	if block.ToolUseID != "tool-456" {
		t.Errorf("ToolUseID = %q", block.ToolUseID)
	}
	if block.Content != "command output" {
		t.Errorf("Content = %q", block.Content)
	}
}

func TestSessionStatsData_JSON_Marshal(t *testing.T) {
	data := &SessionStatsData{
		SessionID:       "test-123",
		TotalDurationMs: 1000,
		ToolCallCount:   5,
		ToolsUsed:       []string{"bash", "edit"},
		FilesModified:   2,
	}

	jsonBytes, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	// Verify JSON contains expected fields
	jsonStr := string(jsonBytes)
	if !contains(jsonStr, "session_id") {
		t.Error("JSON should contain 'session_id'")
	}
	if !contains(jsonStr, "test-123") {
		t.Error("JSON should contain 'test-123'")
	}
}

func TestDangerBlockEvent_JSON_Marshal(t *testing.T) {
	event := &DangerBlockEvent{
		Operation:     "rm -rf /",
		Reason:        "Delete root",
		Level:         DangerLevelCritical,
		Category:      "file_delete",
		BypassAllowed: false,
		Suggestions:   []string{"Use rm -i"},
	}

	jsonBytes, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	jsonStr := string(jsonBytes)
	if !contains(jsonStr, "operation") {
		t.Error("JSON should contain 'operation'")
	}
}

func TestUsageStats_JSON(t *testing.T) {
	stats := UsageStats{
		InputTokens:           100,
		OutputTokens:          50,
		CacheWriteInputTokens: 20,
		CacheReadInputTokens:  10,
	}

	jsonBytes, err := json.Marshal(stats)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded UsageStats
	err = json.Unmarshal(jsonBytes, &decoded)
	if err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if decoded.InputTokens != 100 {
		t.Errorf("InputTokens = %d, want 100", decoded.InputTokens)
	}
}

func TestEventMeta_JSON(t *testing.T) {
	meta := &EventMeta{
		DurationMs:      1000,
		TotalDurationMs: 5000,
		ToolName:        "bash",
		ToolID:          "tool-123",
		Status:          "success",
	}

	jsonBytes, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	jsonStr := string(jsonBytes)
	if !contains(jsonStr, "tool_name") {
		t.Error("JSON should contain 'tool_name'")
	}
}
