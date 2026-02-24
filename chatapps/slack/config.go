package slack

import (
	"fmt"
	"regexp"
	"strings"
)

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
	// "block" - Block all group messages
	GroupPolicy string

	// AllowedUsers: List of user IDs who can interact with the bot (whitelist)
	AllowedUsers []string
	// BlockedUsers: List of user IDs who cannot interact with the bot (blacklist)
	BlockedUsers []string
}

// Token format patterns
var (
	botTokenRegex      = regexp.MustCompile(`^xoxb-[0-9]+-[0-9]+-[a-zA-Z0-9]+$`)
	appTokenRegex      = regexp.MustCompile(`^xapp-[0-9]+-[0-9]+-[a-zA-Z0-9]+$`)
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
// channelType: "dm" or "channel" or "group"
func (c *Config) ShouldProcessChannel(channelType, channelID string) bool {
	switch channelType {
	case "dm":
		switch c.DMPolicy {
		case "block":
			return false
		case "pairing":
			// TODO: Check if user is paired
			return true
		default: // "allow"
			return true
		}
	case "channel", "group":
		switch c.GroupPolicy {
		case "block":
			return false
		case "mention":
			// TODO: Check if bot was mentioned
			return true
		default: // "allow"
			return true
		}
	}
	return true
}
