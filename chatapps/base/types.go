package base

import (
	"context"
	"net/http"
	"time"
)

type ChatMessage struct {
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
	Reactions      []Reaction
}

// Reaction represents a reaction to add to a message
type Reaction struct {
	Name      string // emoji name (e.g., "thumbsup", "+1")
	Channel   string
	Timestamp string // message timestamp to react to
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
