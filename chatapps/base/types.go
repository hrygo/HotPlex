package base

import (
	"context"
	"io"
	"net/http"
	"time"
)

// MessageType defines the normalized message types across all chat platforms
type MessageType string

const (
	// MessageTypeThinking indicates the AI is reasoning or thinking
	MessageTypeThinking MessageType = "thinking"
	// MessageTypeAnswer indicates text output from the AI
	MessageTypeAnswer MessageType = "answer"
	// MessageTypeToolUse indicates a tool invocation is starting
	MessageTypeToolUse MessageType = "tool_use"
	// MessageTypeToolResult indicates a tool execution result
	MessageTypeToolResult MessageType = "tool_result"
	// MessageTypeError indicates an error occurred
	MessageTypeError MessageType = "error"
	// MessageTypePlanMode indicates AI is in plan mode and generating a plan
	MessageTypePlanMode MessageType = "plan_mode"
	// MessageTypeExitPlanMode indicates AI completed planning and requests user approval
	MessageTypeExitPlanMode MessageType = "exit_plan_mode"
	// MessageTypeAskUserQuestion indicates AI is asking a clarifying question
	MessageTypeAskUserQuestion MessageType = "ask_user_question"
	// MessageTypeDangerBlock indicates a dangerous operation confirmation block
	MessageTypeDangerBlock MessageType = "danger_block"
	// MessageTypeSessionStats indicates session statistics
	MessageTypeSessionStats MessageType = "session_stats"
	// MessageTypeCommandProgress indicates a slash command is executing with progress updates
	MessageTypeCommandProgress MessageType = "command_progress"
	// MessageTypeCommandComplete indicates a slash command has completed
	MessageTypeCommandComplete MessageType = "command_complete"
	// MessageTypeSystem indicates a system-level message
	MessageTypeSystem MessageType = "system"
	// MessageTypeUser indicates a user message reflection
	MessageTypeUser MessageType = "user"
	// MessageTypeStepStart indicates a new step/milestone (OpenCode specific)
	MessageTypeStepStart MessageType = "step_start"
	// MessageTypeStepFinish indicates a step/milestone completed (OpenCode specific)
	MessageTypeStepFinish MessageType = "step_finish"
	// MessageTypeRaw indicates unparsed raw output (fallback)
	MessageTypeRaw MessageType = "raw"
	// MessageTypeSessionStart indicates a new session is starting (cold start)
	MessageTypeSessionStart MessageType = "session_start"
	// MessageTypeEngineStarting indicates the engine is starting up
	MessageTypeEngineStarting MessageType = "engine_starting"
	// MessageTypeUserMessageReceived indicates user message has been received
	MessageTypeUserMessageReceived MessageType = "user_message_received"
	// MessageTypePermissionRequest indicates a permission request from Claude Code
	MessageTypePermissionRequest MessageType = "permission_request"
)

type ChatMessage struct {
	Type        MessageType // Message type for rendering decisions
	Platform    string
	SessionID   string
	UserID      string
	Content     string
	MessageID   string
	Timestamp   time.Time
	Metadata    map[string]any
	RichContent *RichContent
}

type RichContent struct {
	ParseMode      ParseMode
	InlineKeyboard any
	Blocks         []any
	Embeds         []any
	Attachments    []Attachment
}

type Attachment struct {
	Type     string `json:"type"`
	URL      string `json:"url"`
	Title    string `json:"title"`
	Text     string `json:"text"`
	ThumbURL string `json:"thumb_url,omitempty"`
}

type ParseMode string

const (
	ParseModeNone     ParseMode = ""
	ParseModeMarkdown ParseMode = "markdown"
	ParseModeHTML     ParseMode = "html"
)

type ChatAdapter interface {
	Platform() string
	SystemPrompt() string
	Start(ctx context.Context) error
	Stop() error
	SendMessage(ctx context.Context, sessionID string, msg *ChatMessage) error
	HandleMessage(ctx context.Context, msg *ChatMessage) error
	SetHandler(MessageHandler)
}

type MessageHandler func(ctx context.Context, msg *ChatMessage) error

// WebhookProvider exposes HTTP handlers for unified server integration
type WebhookProvider interface {
	WebhookPath() string
	WebhookHandler() http.Handler
}

// MessageOperations defines platform-specific message operations
type MessageOperations interface {
	DeleteMessage(ctx context.Context, channelID, messageTS string) error
	UpdateMessage(ctx context.Context, channelID, messageTS string, msg *ChatMessage) error
	// SetAssistantStatus sets the native assistant status text at the bottom of the thread
	// Used to drive dynamic status hints (e.g., "Thinking...", "Searching code...")
	SetAssistantStatus(ctx context.Context, channelID, threadTS, status string) error
	// StartStream starts a native streaming message, returns message_ts as anchor for subsequent updates
	StartStream(ctx context.Context, channelID, threadTS string) (string, error)
	// AppendStream incrementally pushes content to an existing stream
	AppendStream(ctx context.Context, channelID, messageTS, content string) error
	// StopStream ends the stream and finalizes the message
	StopStream(ctx context.Context, channelID, messageTS string) error
}

// SessionOperations defines platform-specific session operations
// Note: Session is defined in base/adapter.go to avoid circular dependencies
type SessionOperations interface {
	GetSession(key string) (*Session, bool)
	FindSessionByUserAndChannel(userID, channelID string) *Session
}

// StreamWriter defines the interface for streaming message writes
// Platform-agnostic abstraction for native streaming support
type StreamWriter interface {
	io.Writer
	io.Closer
	// MessageTS returns the message timestamp after stream starts
	MessageTS() string
}

// StatusType defines AI working states
type StatusType string

const (
	StatusThinking   StatusType = "thinking"
	StatusToolUse    StatusType = "tool_use"
	StatusToolResult StatusType = "tool_result"
	StatusAnswering  StatusType = "answering"
	StatusIdle       StatusType = "idle"
)

// StatusProvider defines the abstraction for status notification
// Follows Dependency Inversion Principle - adapters decide the concrete implementation
type StatusProvider interface {
	// SetStatus sets current status, adapter converts to native API or bubble message
	// channelID and threadTS specify where to display the status
	SetStatus(ctx context.Context, channelID, threadTS string, status StatusType, text string) error

	// ClearStatus clears status indicator
	ClearStatus(ctx context.Context, channelID, threadTS string) error
}

// MessageTypeToStatusType converts MessageType to StatusType for status notification
// Returns StatusIdle for unrecognized types
func MessageTypeToStatusType(msgType MessageType) StatusType {
	switch msgType {
	case MessageTypeThinking:
		return StatusThinking
	case MessageTypeToolUse:
		return StatusToolUse
	case MessageTypeToolResult:
		return StatusToolResult
	case MessageTypeAnswer, MessageTypeExitPlanMode:
		return StatusAnswering
	default:
		return StatusIdle
	}
}
