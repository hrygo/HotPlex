package feishu

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hrygo/hotplex/chatapps/base"
)

func TestBuildThinkingCard(t *testing.T) {
	builder := NewCardBuilder("test-session-123")

	cardJSON, err := builder.BuildThinkingCard("正在分析用户请求...")
	if err != nil {
		t.Fatalf("BuildThinkingCard failed: %v", err)
	}

	// Validate JSON structure
	var card CardTemplate
	if err := json.Unmarshal([]byte(cardJSON), &card); err != nil {
		t.Fatalf("Invalid JSON: %v", err)
	}

	// Verify header
	if card.Header == nil {
		t.Fatal("Header is nil")
	}
	if card.Header.Template != CardTemplateBlue {
		t.Errorf("Expected template %s, got %s", CardTemplateBlue, card.Header.Template)
	}
	if card.Header.Title.Content != "🤔 正在思考" {
		t.Errorf("Expected title '🤔 正在思考', got %s", card.Header.Title.Content)
	}

	// Verify elements
	if len(card.Elements) != 1 {
		t.Fatalf("Expected 1 element, got %d", len(card.Elements))
	}
	if card.Elements[0].Type != ElementMarkdown {
		t.Errorf("Expected element type %s, got %s", ElementMarkdown, card.Elements[0].Type)
	}
	if !strings.Contains(card.Elements[0].Text.Content, "正在分析用户请求") {
		t.Errorf("Expected message content, got %s", card.Elements[0].Text.Content)
	}
}

func TestBuildToolUseCard(t *testing.T) {
	builder := NewCardBuilder("test-session-456")

	cardJSON, err := builder.BuildToolUseCard("Bash", "ls -la /tmp")
	if err != nil {
		t.Fatalf("BuildToolUseCard failed: %v", err)
	}

	var card CardTemplate
	if err := json.Unmarshal([]byte(cardJSON), &card); err != nil {
		t.Fatalf("Invalid JSON: %v", err)
	}

	// Verify header
	if card.Header == nil {
		t.Fatal("Header is nil")
	}
	if card.Header.Template != CardTemplateWathet {
		t.Errorf("Expected template %s, got %s", CardTemplateWathet, card.Header.Template)
	}

	// Verify elements
	if len(card.Elements) != 2 {
		t.Fatalf("Expected 2 elements, got %d", len(card.Elements))
	}

	// First element: tool name
	if card.Elements[0].Type != ElementDiv {
		t.Errorf("Expected element type %s, got %s", ElementDiv, card.Elements[0].Type)
	}
	if !strings.Contains(card.Elements[0].Text.Content, "Bash") {
		t.Errorf("Expected tool name, got %s", card.Elements[0].Text.Content)
	}

	// Second element: tool input
	if card.Elements[1].Type != ElementNote {
		t.Errorf("Expected element type %s, got %s", ElementNote, card.Elements[1].Type)
	}
}

func TestBuildPermissionCard(t *testing.T) {
	builder := NewCardBuilder("test-session-789")

	tests := []struct {
		name         string
		riskLevel    string
		wantTemplate string
		wantBtnType  string
	}{
		{"Low risk", "low", CardTemplateWathet, ButtonTypeDefault},
		{"Medium risk", "medium", CardTemplateOrange, ButtonTypeDefault},
		{"High risk", "high", CardTemplateRed, ButtonTypeDanger},
		{"Default", "", CardTemplateYellow, ButtonTypeDefault},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cardJSON, err := builder.BuildPermissionCard(
				"执行危险命令",
				"是否允许执行 rm -rf /tmp/* ?",
				tt.riskLevel,
			)
			if err != nil {
				t.Fatalf("BuildPermissionCard failed: %v", err)
			}

			var card CardTemplate
			if err := json.Unmarshal([]byte(cardJSON), &card); err != nil {
				t.Fatalf("Invalid JSON: %v", err)
			}

			// Verify template
			if card.Header.Template != tt.wantTemplate {
				t.Errorf("Expected template %s, got %s", tt.wantTemplate, card.Header.Template)
			}

			// Verify action buttons exist
			hasAction := false
			for _, elem := range card.Elements {
				if elem.Type == ElementAction {
					hasAction = true
					if len(elem.Actions) != 2 {
						t.Errorf("Expected 2 actions, got %d", len(elem.Actions))
					}
				}
			}
			if !hasAction {
				t.Error("Expected action element with buttons")
			}
		})
	}
}

func TestBuildPermissionCardActionValue(t *testing.T) {
	builder := NewCardBuilder("test-session-abc")

	cardJSON, err := builder.BuildPermissionCard("Test", "Test description", "medium")
	if err != nil {
		t.Fatalf("BuildPermissionCard failed: %v", err)
	}

	// Parse and verify action value contains session_id
	var card CardTemplate
	if err := json.Unmarshal([]byte(cardJSON), &card); err != nil {
		t.Fatalf("Invalid JSON: %v", err)
	}

	// Find action element
	for _, elem := range card.Elements {
		if elem.Type == ElementAction {
			for _, action := range elem.Actions {
				var value map[string]string
				if err := json.Unmarshal([]byte(action.Value.(string)), &value); err != nil {
					t.Fatalf("Invalid action value JSON: %v", err)
				}
				if value["session_id"] != "test-session-abc" {
					t.Errorf("Expected session_id 'test-session-abc', got %s", value["session_id"])
				}
				if value["action"] != "permission_request" {
					t.Errorf("Expected action 'permission_request', got %s", value["action"])
				}
			}
		}
	}
}

func TestBuildAnswerCard(t *testing.T) {
	builder := NewCardBuilder("test-session-def")

	content := "## 分析结果\n\n这是 **Markdown** 格式的回答。"
	cardJSON, err := builder.BuildAnswerCard(content)
	if err != nil {
		t.Fatalf("BuildAnswerCard failed: %v", err)
	}

	var card CardTemplate
	if err := json.Unmarshal([]byte(cardJSON), &card); err != nil {
		t.Fatalf("Invalid JSON: %v", err)
	}

	// Verify header
	if card.Header.Template != CardTemplateGreen {
		t.Errorf("Expected template %s, got %s", CardTemplateGreen, card.Header.Template)
	}

	// Verify content
	if len(card.Elements) != 1 {
		t.Fatalf("Expected 1 element, got %d", len(card.Elements))
	}
	if card.Elements[0].Type != ElementMarkdown {
		t.Errorf("Expected element type %s, got %s", ElementMarkdown, card.Elements[0].Type)
	}
	if card.Elements[0].Text.Content != content {
		t.Errorf("Expected content %s, got %s", content, card.Elements[0].Text.Content)
	}
}

func TestBuildErrorCard(t *testing.T) {
	builder := NewCardBuilder("test-session-ghi")

	errorMsg := "命令执行失败：permission denied"
	cardJSON, err := builder.BuildErrorCard(errorMsg)
	if err != nil {
		t.Fatalf("BuildErrorCard failed: %v", err)
	}

	var card CardTemplate
	if err := json.Unmarshal([]byte(cardJSON), &card); err != nil {
		t.Fatalf("Invalid JSON: %v", err)
	}

	// Verify header
	if card.Header.Template != CardTemplateRed {
		t.Errorf("Expected template %s, got %s", CardTemplateRed, card.Header.Template)
	}
	if card.Header.Title.Content != "❌ 错误" {
		t.Errorf("Expected title '❌ 错误', got %s", card.Header.Title.Content)
	}

	// Verify alert element
	if len(card.Elements) != 1 {
		t.Fatalf("Expected 1 element, got %d", len(card.Elements))
	}
	if card.Elements[0].Type != ElementAlert {
		t.Errorf("Expected element type %s, got %s", ElementAlert, card.Elements[0].Type)
	}
}

func TestBuildSessionStatsCard(t *testing.T) {
	builder := NewCardBuilder("test-session-jkl")

	otherStats := map[string]string{
		"步骤": "3/5",
		"内存": "128MB",
	}

	cardJSON, err := builder.BuildSessionStatsCard("2.3s", 1200, otherStats)
	if err != nil {
		t.Fatalf("BuildSessionStatsCard failed: %v", err)
	}

	var card CardTemplate
	if err := json.Unmarshal([]byte(cardJSON), &card); err != nil {
		t.Fatalf("Invalid JSON: %v", err)
	}

	// Verify elements
	if len(card.Elements) != 1 {
		t.Fatalf("Expected 1 element, got %d", len(card.Elements))
	}
	if card.Elements[0].Type != ElementNote {
		t.Errorf("Expected element type %s, got %s", ElementNote, card.Elements[0].Type)
	}

	// Verify stats content
	statsContent := card.Elements[0].Elements[0].Text.Content
	if !strings.Contains(statsContent, "⏱️ 2.3s") {
		t.Errorf("Expected duration in stats, got %s", statsContent)
	}
	if !strings.Contains(statsContent, "⚡ 1200 tokens") {
		t.Errorf("Expected token usage in stats, got %s", statsContent)
	}
	if !strings.Contains(statsContent, "步骤") || !strings.Contains(statsContent, "3/5") {
		t.Errorf("Expected custom stats, got %s", statsContent)
	}
}

func TestTruncateString(t *testing.T) {
	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"short", 10, "short"},
		{"exactly ten", 10, "exactly..."},
		{"this is a very long string that should be truncated", 20, "this is a very lo..."},
		{"", 5, ""},
	}

	for _, tt := range tests {
		got := base.TruncateWithEllipsis(tt.input, tt.maxLen)
		if got != tt.want {
			t.Errorf("TruncateWithEllipsis(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
		}
	}
}

func TestCardBuilderConfig(t *testing.T) {
	builder := NewCardBuilder("test-session-mno")

	cardJSON, err := builder.BuildThinkingCard("test")
	if err != nil {
		t.Fatalf("BuildThinkingCard failed: %v", err)
	}

	var card CardTemplate
	if err := json.Unmarshal([]byte(cardJSON), &card); err != nil {
		t.Fatalf("Invalid JSON: %v", err)
	}

	// Verify config
	if card.Config == nil {
		t.Fatal("Config is nil")
	}
	if !card.Config.EnableForward {
		t.Error("Expected EnableForward to be true")
	}
}
