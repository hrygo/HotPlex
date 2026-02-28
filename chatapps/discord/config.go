package discord

import "fmt"

type Config struct {
	BotToken     string
	ServerAddr   string
	PublicKey    string
	SystemPrompt string
}

// Validate validates the Discord configuration
func (c *Config) Validate() error {
	// Bot token is always required
	if c.BotToken == "" {
		return fmt.Errorf("bot token is required")
	}
	
	// Discord bot tokens typically start with "Bot " or are just long strings
	// Basic validation: must be at least 50 characters (typical Discord token length)
	if len(c.BotToken) < 50 {
		return fmt.Errorf("invalid bot token format: token too short")
	}
	
	// ServerAddr is optional for some deployments
	// PublicKey is required for interaction handling
	if c.PublicKey == "" {
		return fmt.Errorf("public key is required for interaction handling")
	}
	
	return nil
}
