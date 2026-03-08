package diag

import (
	"bytes"
	"context"
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"github.com/hrygo/hotplex"
	"github.com/hrygo/hotplex/brain"
	"github.com/hrygo/hotplex/internal/persistence"
	"github.com/hrygo/hotplex/plugins/storage"
)

// Collector collects diagnostic context from various sources.
type Collector struct {
	config        *Config
	historyStore  persistence.MessageHistoryStore
	redactor      *Redactor
	brain         brain.Brain
	version       string
	startTime     time.Time
}

// NewCollector creates a new diagnostic context collector.
func NewCollector(config *Config, historyStore persistence.MessageHistoryStore, br brain.Brain) *Collector {
	if config == nil {
		config = DefaultConfig()
	}
	return &Collector{
		config:       config,
		historyStore: historyStore,
		redactor:     NewRedactor(RedactStandard),
		brain:        br,
		version:      hotplex.Version,
		startTime:    time.Now(),
	}
}

// Collect gathers all diagnostic context for a given trigger.
func (c *Collector) Collect(ctx context.Context, trigger Trigger) (*DiagContext, error) {
	now := time.Now()

	diagCtx := &DiagContext{
		OriginalSessionID: trigger.SessionID(),
		Platform:          trigger.Platform(),
		UserID:            trigger.UserID(),
		ChannelID:         trigger.ChannelID(),
		ThreadID:          trigger.ThreadID(),
		Trigger:           trigger.Type(),
		Error:             trigger.Error(),
		Timestamp:         now,
	}

	// Collect environment info
	diagCtx.Environment = c.collectEnvInfo()

	// Collect conversation history
	conv, err := c.collectConversation(ctx, trigger.SessionID())
	if err != nil {
		// Log but don't fail - conversation data is optional
		conv = &ConversationData{
			Processed:    fmt.Sprintf("Failed to collect conversation: %v", err),
			MessageCount: 0,
		}
	}
	diagCtx.Conversation = conv

	// Collect recent logs (placeholder - would need log buffer integration)
	diagCtx.Logs = c.collectLogs()

	return diagCtx, nil
}

// collectEnvInfo gathers environment information.
func (c *Collector) collectEnvInfo() *EnvInfo {
	buildInfo, _ := debug.ReadBuildInfo()

	var goVersion string
	if buildInfo != nil {
		goVersion = buildInfo.GoVersion
	}

	return &EnvInfo{
		HotPlexVersion: c.version,
		GoVersion:      goVersion,
		OS:             runtime.GOOS,
		Arch:           runtime.GOARCH,
		Uptime:         time.Since(c.startTime),
	}
}

// collectConversation gathers and processes conversation history.
func (c *Collector) collectConversation(ctx context.Context, sessionID string) (*ConversationData, error) {
	if c.historyStore == nil {
		return &ConversationData{
			Processed:    "History store not available",
			MessageCount: 0,
		}, nil
	}

	// Get recent messages
	messages, err := c.historyStore.GetRecentMessages(ctx, sessionID, 100)
	if err != nil {
		return nil, fmt.Errorf("get recent messages: %w", err)
	}

	if len(messages) == 0 {
		return &ConversationData{
			Processed:    "No messages in session",
			MessageCount: 0,
		}, nil
	}

	// Format conversation
	var buf bytes.Buffer
	for _, msg := range messages {
		role := "User"
		if msg.MessageType == "bot" || msg.MessageType == "assistant" {
			role = "Assistant"
		}
		// Redact sensitive content
		content := c.redactor.Redact(msg.Content)
		buf.WriteString(fmt.Sprintf("[%s] %s: %s\n", msg.CreatedAt.Format(time.RFC3339), role, content))
	}

	rawContent := buf.String()
	rawSize := len(rawContent)

	// Check if summarization is needed
	limit := c.config.ConversationSizeLimit
	if limit <= 0 {
		limit = 20 * 1024 // Default 20KB
	}

	result := &ConversationData{
		RawSize:      rawSize,
		Processed:    rawContent,
		IsSummarized: false,
		MessageCount: len(messages),
	}

	// Summarize if too large
	if rawSize > limit && c.brain != nil {
		summary, err := c.summarizeConversation(ctx, rawContent, limit)
		if err == nil {
			result.Processed = summary
			result.IsSummarized = true
		}
		// If summarization fails, keep original (truncated if needed)
	}

	// Final truncation if still too large
	if len(result.Processed) > limit*2 {
		result.Processed = result.Processed[:limit*2] + "\n... [truncated]"
	}

	return result, nil
}

// summarizeConversation uses Brain to summarize conversation content.
func (c *Collector) summarizeConversation(ctx context.Context, content string, targetSize int) (string, error) {
	if c.brain == nil {
		return "", fmt.Errorf("brain not available")
	}

	prompt := fmt.Sprintf(SummarizeConversationPrompt, targetSize, content)

	summary, err := c.brain.Chat(ctx, prompt)
	if err != nil {
		return "", fmt.Errorf("brain chat: %w", err)
	}

	return summary, nil
}

// collectLogs gathers recent log entries.
// Note: This is a placeholder. Real implementation would integrate with log buffer.
func (c *Collector) collectLogs() []byte {
	// TODO: Integrate with log buffer/circular buffer
	// For now, return empty
	return []byte{}
}

// FormatConversationForIssue formats conversation data for GitHub issue body.
func FormatConversationForIssue(conv *ConversationData) string {
	var buf bytes.Buffer

	buf.WriteString(fmt.Sprintf("### Conversation History\n"))
	buf.WriteString(fmt.Sprintf("- Messages: %d\n", conv.MessageCount))
	buf.WriteString(fmt.Sprintf("- Raw Size: %d bytes\n", conv.RawSize))
	if conv.IsSummarized {
		buf.WriteString("- *Summarized due to size*\n")
	}
	buf.WriteString("\n```\n")
	buf.WriteString(conv.Processed)
	buf.WriteString("\n```\n")

	return buf.String()
}

// FormatErrorForIssue formats error info for GitHub issue body.
func FormatErrorForIssue(err *ErrorInfo) string {
	if err == nil {
		return "No error information available"
	}

	var buf strings.Builder

	buf.WriteString(fmt.Sprintf("### Error Details\n"))
	buf.WriteString(fmt.Sprintf("- **Type**: %s\n", err.Type))
	buf.WriteString(fmt.Sprintf("- **Message**: %s\n", err.Message))
	buf.WriteString(fmt.Sprintf("- **Time**: %s\n", err.Timestamp.Format(time.RFC3339)))

	if err.ExitCode != 0 {
		buf.WriteString(fmt.Sprintf("- **Exit Code**: %d\n", err.ExitCode))
	}

	if err.StackTrace != "" {
		buf.WriteString("\n**Stack Trace:**\n```\n")
		buf.WriteString(err.StackTrace)
		buf.WriteString("\n```\n")
	}

	if len(err.Context) > 0 {
		buf.WriteString("\n**Context:**\n")
		for k, v := range err.Context {
			buf.WriteString(fmt.Sprintf("- %s: %v\n", k, v))
		}
	}

	return buf.String()
}

// FormatEnvForIssue formats environment info for GitHub issue body.
func FormatEnvForIssue(env *EnvInfo) string {
	var buf strings.Builder

	buf.WriteString("### Environment\n")
	buf.WriteString(fmt.Sprintf("| Field | Value |\n"))
	buf.WriteString(fmt.Sprintf("|-------|-------|\n"))
	buf.WriteString(fmt.Sprintf("| HotPlex Version | %s |\n", env.HotPlexVersion))
	buf.WriteString(fmt.Sprintf("| Go Version | %s |\n", env.GoVersion))
	buf.WriteString(fmt.Sprintf("| OS | %s |\n", env.OS))
	buf.WriteString(fmt.Sprintf("| Arch | %s |\n", env.Arch))
	if env.CLIVersion != "" {
		buf.WriteString(fmt.Sprintf("| CLI Version | %s |\n", env.CLIVersion))
	}
	buf.WriteString(fmt.Sprintf("| Uptime | %s |\n", env.Uptime.Round(time.Second)))

	return buf.String()
}

// ConvertStorageMessages converts storage messages to a simpler format.
func ConvertStorageMessages(msgs []*storage.ChatAppMessage) []map[string]string {
	result := make([]map[string]string, len(msgs))
	for i, msg := range msgs {
		result[i] = map[string]string{
			"id":        msg.ID,
			"role":      string(msg.MessageType),
			"content":   msg.Content,
			"timestamp": msg.CreatedAt.Format(time.RFC3339),
		}
	}
	return result
}
