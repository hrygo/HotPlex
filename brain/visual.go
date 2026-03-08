package brain

import (
	"fmt"

	"github.com/hrygo/hotplex/provider"
)

// Visualizer translates provider events into human-readable messages for UI display.
type Visualizer struct{}

// NewVisualizer creates a new Visualizer.
func NewVisualizer() *Visualizer {
	return &Visualizer{}
}

// TranslateEvent translates a provider event into a human-readable message.
// It uses session context to provide more accurate translations.
func (v *Visualizer) TranslateEvent(evt *provider.ProviderEvent, platform, taskType string) string {
	switch evt.Type {
	case provider.EventTypeToolUse:
		return v.translateToolUse(evt, platform)
	case provider.EventTypeThinking:
		return v.translateThinking(evt, platform)
	case provider.EventTypeResult:
		return v.translateResult(evt)
	case provider.EventTypeError:
		return v.translateError(evt)
	default:
		return ""
	}
}

// translateToolUse translates a tool use event.
func (v *Visualizer) translateToolUse(evt *provider.ProviderEvent, platform string) string {
	toolName := evt.ToolName
	if toolName == "" {
		toolName = "unknown tool"
	}

	switch platform {
	case "slack":
		return fmt.Sprintf("🔧 Executing %s...", toolName)
	case "feishu":
		return fmt.Sprintf("🔧 正在执行 %s...", toolName)
	default:
		return fmt.Sprintf("Executing %s...", toolName)
	}
}

// translateThinking translates a thinking event.
func (v *Visualizer) translateThinking(evt *provider.ProviderEvent, platform string) string {
	switch platform {
	case "slack":
		return "🤔 Thinking..."
	case "feishu":
		return "🤔 思考中..."
	default:
		return "Thinking..."
	}
}

// translateResult translates a result event.
func (v *Visualizer) translateResult(evt *provider.ProviderEvent) string {
	if evt.Metadata != nil && evt.Metadata.TotalDurationMs > 0 {
		return fmt.Sprintf("✓ Completed in %dms", evt.Metadata.TotalDurationMs)
	}
	return "✓ Completed"
}

// translateError translates an error event.
func (v *Visualizer) translateError(evt *provider.ProviderEvent) string {
	errMsg := evt.Error
	if errMsg == "" {
		return "❌ An error occurred"
	}
	if len(errMsg) > 100 {
		errMsg = errMsg[:100] + "..."
	}
	return fmt.Sprintf("❌ Error: %s", errMsg)
}

// GetTaskTypeSummary returns a brief summary of the task type for display.
func (v *Visualizer) GetTaskTypeSummary(taskType string) string {
	switch taskType {
	case "code":
		return "💻 Code"
	case "chat":
		return "💬 Chat"
	case "analysis":
		return "📊 Analysis"
	case "debug":
		return "🐛 Debug"
	case "git":
		return "🔀 Git"
	default:
		return "❓ Unknown"
	}
}

// Global visualizer instance
var globalVisualizer *Visualizer

// GlobalVisualizer returns the global Visualizer instance.
func GlobalVisualizer() *Visualizer {
	if globalVisualizer == nil {
		globalVisualizer = NewVisualizer()
	}
	return globalVisualizer
}

// InitVisualizer initializes the global Visualizer.
func InitVisualizer() {
	globalVisualizer = NewVisualizer()
}
