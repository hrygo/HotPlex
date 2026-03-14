package feishu

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hrygo/hotplex/chatapps/base"
)

// CardBuilder builds Feishu interactive cards for HotPlex events
type CardBuilder struct {
	sessionID string
}

// NewCardBuilder creates a new card builder
func NewCardBuilder(sessionID string) *CardBuilder {
	return &CardBuilder{
		sessionID: sessionID,
	}
}

// BuildThinkingCard builds a thinking state card
// Event: thinking - Shows "🤔 Thinking..." with loading animation
func (b *CardBuilder) BuildThinkingCard(message string) (string, error) {
	card := &CardTemplate{
		Config: &CardConfig{
			WideScreenMode: false,
			EnableForward:  true,
		},
		Header: &CardHeader{
			Template: CardTemplateBlue,
			Title: &Text{
				Content: "🤔 正在思考",
				Tag:     TextTypePlainText,
			},
		},
		Elements: []CardElement{
			{
				Type: ElementMarkdown,
				Text: &Text{
					Content: message,
					Tag:     TextTypeLarkMD,
				},
			},
		},
	}

	return b.marshalCard(card)
}

// BuildToolUseCard builds a tool execution card
// Event: tool_use - Shows "🛠️ Executing: Bash"
func (b *CardBuilder) BuildToolUseCard(toolName, toolInput string) (string, error) {
	card := &CardTemplate{
		Config: &CardConfig{
			WideScreenMode: false,
			EnableForward:  true,
		},
		Header: &CardHeader{
			Template: CardTemplateWathet,
			Title: &Text{
				Content: "🛠️ 工具调用",
				Tag:     TextTypePlainText,
			},
		},
		Elements: []CardElement{
			{
				Type: ElementDiv,
				Text: &Text{
					Content: fmt.Sprintf("**工具**: %s", toolName),
					Tag:     TextTypeLarkMD,
				},
			},
			{
				Type: ElementNote,
				Elements: []CardElement{
					{
						Type: ElementMarkdown,
						Text: &Text{
							Content: fmt.Sprintf("输入：%s", base.TruncateWithEllipsis(toolInput, 200)),
							Tag:     TextTypeLarkMD,
						},
					},
				},
			},
		},
	}

	return b.marshalCard(card)
}

// BuildPermissionCard builds a permission request card with Allow/Deny buttons
// Event: permission_request - Interactive card with buttons
func (b *CardBuilder) BuildPermissionCard(title, description, riskLevel string) (string, error) {
	// Determine button style based on risk level
	template := CardTemplateYellow
	btnType := ButtonTypeDefault

	switch strings.ToLower(riskLevel) {
	case "high":
		template = CardTemplateRed
		btnType = ButtonTypeDanger
	case "medium":
		template = CardTemplateOrange
	case "low":
		template = CardTemplateWathet
	}

	// Build action value for callback
	actionValue := map[string]string{
		"action":     "permission_request",
		"session_id": b.sessionID,
	}

	actionValueJSON, err := json.Marshal(actionValue)
	if err != nil {
		return "", err
	}

	card := &CardTemplate{
		Config: &CardConfig{
			WideScreenMode: false,
			EnableForward:  true,
		},
		Header: &CardHeader{
			Template: template,
			Title: &Text{
				Content: "⚠️ 权限请求",
				Tag:     TextTypePlainText,
			},
		},
		Elements: []CardElement{
			{
				Type: ElementDiv,
				Text: &Text{
					Content: fmt.Sprintf("**%s**", title),
					Tag:     TextTypeLarkMD,
				},
			},
			{
				Type: ElementDiv,
				Text: &Text{
					Content: description,
					Tag:     TextTypeLarkMD,
				},
			},
			{
				Type: ElementNote,
				Elements: []CardElement{
					{
						Type: ElementMarkdown,
						Text: &Text{
							Content: fmt.Sprintf("风险等级：%s", riskLevel),
							Tag:     TextTypeLarkMD,
						},
					},
				},
			},
			{
				Type: ElementAction,
				Actions: []CardAction{
					{
						Type: ButtonTypeDefault,
						Text: &Text{
							Content: "✅ 允许",
							Tag:     TextTypePlainText,
						},
						Value: string(actionValueJSON),
					},
					{
						Type: btnType,
						Text: &Text{
							Content: "❌ 拒绝",
							Tag:     TextTypePlainText,
						},
						Value: string(actionValueJSON),
					},
				},
			},
		},
	}

	return b.marshalCard(card)
}

// BuildAnswerCard builds an answer card with Markdown support
// Event: answer - Final answer with Markdown
func (b *CardBuilder) BuildAnswerCard(content string) (string, error) {
	card := &CardTemplate{
		Config: &CardConfig{
			WideScreenMode: false,
			EnableForward:  true,
		},
		Header: &CardHeader{
			Template: CardTemplateGreen,
			Title: &Text{
				Content: "✅ 回答",
				Tag:     TextTypePlainText,
			},
		},
		Elements: []CardElement{
			{
				Type: ElementMarkdown,
				Text: &Text{
					Content: content,
					Tag:     TextTypeLarkMD,
				},
			},
		},
	}

	return b.marshalCard(card)
}

// BuildErrorCard builds an error/warning card
// Event: error - Red alert box
func (b *CardBuilder) BuildErrorCard(errorMsg string) (string, error) {
	card := &CardTemplate{
		Config: &CardConfig{
			WideScreenMode: false,
			EnableForward:  true,
		},
		Header: &CardHeader{
			Template: CardTemplateRed,
			Title: &Text{
				Content: "❌ 错误",
				Tag:     TextTypePlainText,
			},
		},
		Elements: []CardElement{
			{
				Type: ElementAlert,
				Text: &Text{
					Content: errorMsg,
					Tag:     TextTypeLarkMD,
				},
			},
		},
	}

	return b.marshalCard(card)
}

// formatTokenCount formats token count in compact form (1.2K, 1.00M)
// Uses proper threshold: K for < 1M, M for >= 1M
func formatTokenCount(count int) string {
	if count >= 1000000 {
		return fmt.Sprintf("%.2fM", float64(count)/1000000)
	}
	if count >= 1000 {
		kValue := float64(count) / 1000
		// If k value >= 999.5, show M to avoid rounding issues
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

// BuildSessionStatsCard builds a session statistics card
// Event: session_stats - Gray note with stats
func (b *CardBuilder) BuildSessionStatsCard(duration string, tokenUsage int, otherStats map[string]string) (string, error) {
	// Build stats text with formatted token count
	var statsBuilder strings.Builder
	_, _ = fmt.Fprintf(&statsBuilder, "⏱️ %s • ⚡ %s tokens", duration, formatTokenCount(tokenUsage))

	// Add additional stats if provided
	for key, value := range otherStats {
		_, _ = fmt.Fprintf(&statsBuilder, " • %s: %s", key, value)
	}

	card := &CardTemplate{
		Config: &CardConfig{
			WideScreenMode: false,
			EnableForward:  true,
		},
		Elements: []CardElement{
			{
				Type: ElementNote,
				Elements: []CardElement{
					{
						Type: ElementMarkdown,
						Text: &Text{
							Content: statsBuilder.String(),
							Tag:     TextTypeLarkMD,
						},
					},
				},
			},
		},
	}

	return b.marshalCard(card)
}

// marshalCard converts a CardTemplate to JSON string
func (b *CardBuilder) marshalCard(card *CardTemplate) (string, error) {
	data, err := json.Marshal(card)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
