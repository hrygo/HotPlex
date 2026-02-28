package chatapps

import (
	"context"
	"time"

	"github.com/hrygo/hotplex/chatapps/base"
	"github.com/hrygo/hotplex/event"
	"github.com/hrygo/hotplex/types"
)

// Engine abstracts the engine functionality for dependency inversion
// This interface allows chatapps to work with any engine implementation
type Engine interface {
	// Execute runs a command with the given config and prompt
	Execute(ctx context.Context, cfg *types.Config, prompt string, callback event.Callback) error
	// GetSession retrieves a session by ID
	GetSession(sessionID string) (Session, bool)
	// Close shuts down the engine and releases resources
	Close() error
	// GetSessionStats returns statistics for a session
	GetSessionStats(sessionID string) *SessionStats
	// ValidateConfig validates a configuration
	ValidateConfig(cfg *types.Config) error
	// StopSession stops a running session
	StopSession(sessionID string, reason string) error
	// ResetSessionProvider resets the provider for a session
	ResetSessionProvider(sessionID string)
	// SetDangerAllowPaths sets allowed paths for dangerous operations
	SetDangerAllowPaths(paths []string)
	// SetDangerBypassEnabled enables/disables danger bypass
	SetDangerBypassEnabled(token string, enabled bool) error
	// SetAllowedTools sets the list of allowed tools
	SetAllowedTools(tools []string)
	// SetDisallowedTools sets the list of disallowed tools
	SetDisallowedTools(tools []string)
	// GetAllowedTools returns the list of allowed tools
	GetAllowedTools() []string
	// GetDisallowedTools returns the list of disallowed tools
	GetDisallowedTools() []string
}

// Session abstracts session state and operations
type Session interface {
	// ID returns the session identifier
	ID() string
	// Status returns the current session status
	Status() string
	// CreatedAt returns the session creation time
	CreatedAt() time.Time
}

// SessionStats holds session statistics
type SessionStats struct {
	SessionID     string
	Status        string
	TotalTokens   int64
	InputTokens   int64
	OutputTokens  int64
	CacheRead     int64
	CacheWrite    int64
	TotalCost     float64
	Duration      time.Duration
	ToolCallCount int
	ErrorCount    int
}

// Re-export interfaces from base for convenience
type (
	MessageOperations = base.MessageOperations
	SessionOperations = base.SessionOperations
)

type ParseMode = base.ParseMode

const (
	ParseModeNone     = base.ParseModeNone
	ParseModeMarkdown = base.ParseModeMarkdown
	ParseModeHTML     = base.ParseModeHTML
)

type ChatMessage = base.ChatMessage
type RichContent = base.RichContent
type Attachment = base.Attachment
type ChatAdapter = base.ChatAdapter
type MessageHandler = base.MessageHandler

type InlineKeyboardButton struct {
	Text         string `json:"text"`
	URL          string `json:"url,omitempty"`
	CallbackData string `json:"callback_data,omitempty"`
}

type InlineKeyboardMarkup struct {
	InlineKeyboard [][]InlineKeyboardButton `json:"inline_keyboard"`
}

type SlackBlock map[string]any

type DiscordEmbed struct {
	Title       string                 `json:"title,omitempty"`
	Description string                 `json:"description,omitempty"`
	URL         string                 `json:"url,omitempty"`
	Color       int                    `json:"color,omitempty"`
	Fields      []DiscordEmbedField    `json:"fields,omitempty"`
	Footer      *DiscordEmbedFooter    `json:"footer,omitempty"`
	Thumbnail   *DiscordEmbedThumbnail `json:"thumbnail,omitempty"`
	Image       *DiscordEmbedImage     `json:"image,omitempty"`
	Timestamp   string                 `json:"timestamp,omitempty"`
}

type DiscordEmbedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline,omitempty"`
}

type DiscordEmbedFooter struct {
	Text    string `json:"text"`
	IconURL string `json:"icon_url,omitempty"`
}

type DiscordEmbedThumbnail struct {
	URL string `json:"url"`
}

type DiscordEmbedImage struct {
	URL string `json:"url"`
}

type StreamHandler func(ctx context.Context, sessionID string, chunk string, isFinal bool) error

type StreamAdapter interface {
	ChatAdapter
	SendStreamMessage(ctx context.Context, sessionID string, msg *ChatMessage) (StreamHandler, error)
	UpdateMessage(ctx context.Context, sessionID, messageID string, msg *ChatMessage) error
}

func NewChatMessage(platform, sessionID, userID, content string) *ChatMessage {
	return &ChatMessage{
		Platform:  platform,
		SessionID: sessionID,
		UserID:    userID,
		Content:   content,
		Timestamp: time.Now(),
		Metadata:  make(map[string]any),
	}
}
