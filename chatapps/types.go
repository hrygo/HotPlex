package chatapps

import (
	"context"
	"time"
)

// ParseMode defines message formatting mode
type ParseMode string

const (
	ParseModeNone     ParseMode = ""
	ParseModeMarkdown ParseMode = "markdown" // Telegram: Markdown, Slack: mrkdwn
	ParseModeHTML     ParseMode = "html"     // Telegram: HTML
)

// InlineKeyboardButton represents a button in inline keyboard
type InlineKeyboardButton struct {
	Text         string `json:"text"`
	URL          string `json:"url,omitempty"`
	CallbackData string `json:"callback_data,omitempty"`
}

// InlineKeyboard represents inline keyboard markup
type InlineKeyboardMarkup struct {
	InlineKeyboard [][]InlineKeyboardButton `json:"inline_keyboard"`
}

// Attachment represents rich content attachments
type Attachment struct {
	Type     string `json:"type"`                // image, file, audio
	URL      string `json:"url"`                 // URL to the content
	Title    string `json:"title"`               // Optional title
	Text     string `json:"text"`                // Optional description
	ThumbURL string `json:"thumb_url,omitempty"` // Thumbnail for images
}

// SlackBlock represents a Slack Block Kit block
type SlackBlock map[string]any

// DiscordEmbed represents a Discord embed
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

// RichContent holds rich message content for advanced platforms
type RichContent struct {
	// Formatting
	ParseMode ParseMode `json:"parse_mode,omitempty"`

	// Telegram specific
	InlineKeyboard *InlineKeyboardMarkup `json:"inline_keyboard,omitempty"`

	// Slack specific
	Blocks []SlackBlock `json:"blocks,omitempty"`

	// Discord specific
	Embeds []DiscordEmbed `json:"embeds,omitempty"`

	// Common attachments
	Attachments []Attachment `json:"attachments,omitempty"`
}

type ChatMessage struct {
	Platform    string
	SessionID   string
	UserID      string
	Content     string
	MessageID   string
	Timestamp   time.Time
	Metadata    map[string]any
	RichContent *RichContent `json:"rich_content,omitempty"` // Optional rich content
}

type ChatAdapter interface {
	Platform() string
	Start(ctx context.Context) error
	Stop() error
	SendMessage(ctx context.Context, sessionID string, msg *ChatMessage) error
	HandleMessage(ctx context.Context, msg *ChatMessage) error
}

type MessageHandler func(ctx context.Context, msg *ChatMessage) error

// StreamHandler handles streaming message responses
type StreamHandler func(ctx context.Context, sessionID string, chunk string, isFinal bool) error

// StreamAdapter extends ChatAdapter for streaming message support
type StreamAdapter interface {
	ChatAdapter
	// SendStreamMessage sends a message and returns a stream handler for updates
	SendStreamMessage(ctx context.Context, sessionID string, msg *ChatMessage) (StreamHandler, error)
	// UpdateMessage updates an existing message (for edit support)
	UpdateMessage(ctx context.Context, sessionID, messageID string, msg *ChatMessage) error
}
