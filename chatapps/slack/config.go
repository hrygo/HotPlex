// Package slack provides the Slack adapter implementation for the hotplex engine.
// Configuration structures and validation logic for the Slack adapter.
package slack

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

// pairingState holds runtime pairing state with thread-safe access
type pairingState struct {
	mu    sync.RWMutex
	once  sync.Once
	users map[string]bool
}

type Config struct {
	BotToken      string
	AppToken      string
	SigningSecret string
	SystemPrompt  string
	// Mode: "http" (default) or "socket" for WebSocket connection
	Mode string
	// ServerAddr: HTTP server address (e.g., ":8080")
	ServerAddr string

	// Permission Policy for Direct Messages
	// "allow" - Allow all DMs (default)
	// "pairing" - Only allow when user is paired
	// "block" - Block all DMs
	DMPolicy string

	// Permission Policy for Group Messages
	// "allow" - Allow all group messages (default)
	// "mention" - Only allow when bot is mentioned
	// "multibot" - Multi-bot routing: broadcast if no @, respond only if @self
	// "block" - Block all group messages
	GroupPolicy string

	// AllowedUsers: List of user IDs who can interact with the bot (whitelist)
	AllowedUsers []string
	// BlockedUsers: List of user IDs who cannot interact with the bot (blacklist)
	BlockedUsers []string
	// BotUserID: Bot's user ID (e.g., "U1234567890") - used for mention detection
	BotUserID string

	// SlashCommandRateLimit: Maximum requests per second per user for slash commands
	// Default: 10.0 requests/second
	SlashCommandRateLimit float64

	// pairing holds runtime pairing state (pointer for thread safety)
	pairing *pairingState

	// BroadcastResponder generates responses for broadcast messages (no @ mention).
	// If nil, uses DefaultBroadcastResponse.
	BroadcastResponder BroadcastResponder

	// Storage configuration for message persistence (optional)
	// When enabled, stores user messages and bot responses for history retrieval
	Storage *StorageConfig
}

// StorageConfig holds message storage configuration for Slack adapter.
//
// Storage Architecture:
//   - Session ID is derived from (platform, botUserID, channelID, threadTS)
//   - Messages are grouped by thread, enabling conversation history retrieval
//   - ChatUserID field distinguishes message sender for user-filtered queries
//
// Example Usage:
//
//	&StorageConfig{
//	    Enabled:   true,
//	    Type:      "sqlite",
//	    SQLitePath: "data/slack_messages.db",
//	}
type StorageConfig struct {
	// Enabled enables message storage
	Enabled bool
	// Type: "memory" (default), "sqlite", "postgresql"
	Type string
	// SQLitePath: Path to SQLite database file (only for type="sqlite")
	SQLitePath string
	// PostgreSQLURL: Connection URL for PostgreSQL (only for type="postgresql")
	// Format: "postgres://user:pass@host:port/dbname"
	PostgreSQLURL string
	// StreamEnabled enables streaming message buffering
	StreamEnabled bool
	// StreamTimeout is the timeout for streaming buffer (default 5min)
	StreamTimeout time.Duration
}

// Token format patterns - supports both legacy 3-part and new 4-part Slack token formats
var (
	botTokenRegex = regexp.MustCompile(`^xoxb-[0-9]+-[0-9]+-[a-zA-Z0-9]+$`)
	// Legacy: xapp-{num}-{num}-{alnum}
	// New:      xapp-{num}-{alnum}-{num}-{alnum} (Slack updated format in 2025+)
	appTokenRegex      = regexp.MustCompile(`^xapp-[0-9]+-[a-zA-Z0-9]+-[0-9]+-[a-zA-Z0-9]+$|^xapp-[0-9]+-[0-9]+-[a-zA-Z0-9]+$`)
	signingSecretRegex = regexp.MustCompile(`^[a-zA-Z0-9]+$`)
)

// Validate checks the configuration based on the selected mode
func (c *Config) Validate() error {
	// Bot token is always required
	if c.BotToken == "" {
		return fmt.Errorf("bot token is required")
	}
	if !botTokenRegex.MatchString(c.BotToken) {
		return fmt.Errorf("invalid bot token format: expected xoxb-*-*-*")
	}

	switch c.Mode {
	case "", "http":
		// HTTP Mode requires SigningSecret
		if c.SigningSecret == "" {
			return fmt.Errorf("signing secret is required for HTTP mode")
		}
		if len(c.SigningSecret) < 32 {
			return fmt.Errorf("signing secret too short: minimum 32 characters")
		}
		if !signingSecretRegex.MatchString(c.SigningSecret) {
			return fmt.Errorf("invalid signing secret format: must be alphanumeric")
		}
	case "socket":
		// Socket Mode requires AppToken
		if c.AppToken == "" {
			return fmt.Errorf("app token is required for Socket mode")
		}
		if !appTokenRegex.MatchString(c.AppToken) {
			return fmt.Errorf("invalid app token format: expected xapp-*-*-*")
		}
	default:
		return fmt.Errorf("invalid mode: %s (use 'http' or 'socket')", c.Mode)
	}

	// Validate ServerAddr if provided
	if c.ServerAddr != "" {
		if !strings.HasPrefix(c.ServerAddr, ":") && !strings.Contains(c.ServerAddr, ":") {
			return fmt.Errorf("invalid server address format: use :8080 or host:port")
		}
	}

	return nil
}

// IsSocketMode returns true if Socket Mode is enabled
func (c *Config) IsSocketMode() bool {
	return c.Mode == "socket"
}

// IsUserAllowed checks if a user is allowed to interact with the bot
func (c *Config) IsUserAllowed(userID string) bool {
	// Check blocked list first
	for _, blocked := range c.BlockedUsers {
		if blocked == userID {
			return false
		}
	}

	// If allowlist is set, check it
	if len(c.AllowedUsers) > 0 {
		for _, allowed := range c.AllowedUsers {
			if allowed == userID {
				return true
			}
		}
		return false
	}

	// No allowlist, user is allowed
	return true
}

// ShouldProcessChannel checks if messages from a channel should be processed
// channelType: "dm", "im" (direct message), or "channel" or "group"
func (c *Config) ShouldProcessChannel(channelType, channelID string) bool {
	switch channelType {
	case "dm", "im":
		switch c.DMPolicy {
		case "block":
			return false
		case "pairing":
			// Check if user has active DM pairing with bot
			return c.isPaired(channelID)
		default: // "allow"
			return true
		}
	case "channel", "group":
		switch c.GroupPolicy {
		case "block":
			return false
		case "mention":
			// Mention check is done at message level, not channel level
			// Return true here and check message text in adapter
			return true
		default: // "allow"
			return true
		}
	}
	return true
}

// isPaired checks if a user has an active DM conversation with the bot
// Returns true only if the user has been explicitly marked as paired
func (c *Config) isPaired(userID string) bool {
	if c.pairing == nil {
		return false
	}
	c.pairing.mu.RLock()
	defer c.pairing.mu.RUnlock()
	if c.pairing.users == nil {
		return false
	}
	return c.pairing.users[userID]
}

// MarkPaired marks a user as having an active DM with the bot
func (c *Config) MarkPaired(userID string) {
	// Initialize pairing state once (thread-safe)
	if c.pairing == nil {
		c.pairing = &pairingState{}
	}
	c.pairing.once.Do(func() {
		c.pairing.users = make(map[string]bool)
	})
	c.pairing.mu.Lock()
	defer c.pairing.mu.Unlock()
	c.pairing.users[userID] = true
}

// ContainsBotMention checks if message text contains a bot mention
// Slack mention format: <@U1234567890> or <!here> or <!channel>
// Uses regex for exact matching to prevent false positives
func (c *Config) ContainsBotMention(text string) bool {
	if c.BotUserID == "" {
		return false
	}
	// Exact match for bot user mention: <@BOT_USER_ID>
	// Pattern matches <@USERID> or <!@USERID> format
	mentionPattern := "<@!?" + regexp.QuoteMeta(c.BotUserID) + ">"
	matched, _ := regexp.MatchString(mentionPattern, text)
	return matched
}

// mentionUserRegex matches <@USERID> or <@!USERID> format
var mentionUserRegex = regexp.MustCompile(`<@!?([A-Z][A-Z0-9]+)>`)

// ExtractMentionedUsers extracts all mentioned user IDs from message text.
// Slack mention format: <@U1234567890> or <@!U1234567890>
func ExtractMentionedUsers(text string) []string {
	matches := mentionUserRegex.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil
	}
	users := make([]string, 0, len(matches))
	for _, m := range matches {
		if len(m) > 1 {
			users = append(users, m[1])
		}
	}
	return users
}

// ShouldRespondInMultibotMode determines if this bot should respond in multibot mode.
// Returns true if:
// - No mentions in message (broadcast mode - all bots respond)
// - Bot is explicitly mentioned
// Returns false if:
// - Other bots are mentioned but not this one
func (c *Config) ShouldRespondInMultibotMode(text string) bool {
	mentioned := ExtractMentionedUsers(text)
	if len(mentioned) == 0 {
		return true // Broadcast: no @ means all bots respond
	}
	// Check if we are in the mention list
	return slices.Contains(mentioned, c.BotUserID)
}

// IsBroadcastMessage returns true if this is a broadcast message (no @ mentions).
// Only meaningful in multibot mode.
func (c *Config) IsBroadcastMessage(text string) bool {
	return len(ExtractMentionedUsers(text)) == 0
}

// SetBroadcastResponse sets a static response for broadcast messages.
// Creates a StaticBroadcastResponder internally.
func (c *Config) SetBroadcastResponse(text string) {
	c.BroadcastResponder = NewStaticBroadcastResponder(text)
}

// GetBroadcastResponse returns the response for broadcast messages.
// Uses configured BroadcastResponder or DefaultBroadcastResponse if not set.
func (c *Config) GetBroadcastResponse(ctx context.Context, userMessage string) string {
	if c.BroadcastResponder == nil {
		return DefaultBroadcastResponse
	}
	resp, err := c.BroadcastResponder.Respond(ctx, userMessage)
	if err != nil {
		return DefaultBroadcastResponse
	}
	return resp
}
