package base

import (
	"fmt"

	"github.com/google/uuid"
)

// SessionIDGenerator generates deterministic session IDs
type SessionIDGenerator interface {
	// Generate creates a deterministic session ID based on:
	// - platform: the platform name (e.g., "slack", "telegram")
	// - userID: the user's ID on the platform
	// - botUserID: the bot's user ID (for multi-bot scenarios)
	// - channelID: the channel/room ID (empty for DM)
	// - threadID: the thread/topic ID (empty if not applicable)
	Generate(platform, userID, botUserID, channelID, threadID string) string
}

// UUID5Generator generates session IDs using UUID5 (SHA1 hash)
// This ensures the same inputs always produce the same session ID
type UUID5Generator struct {
	namespace string
}

// Compile-time interface compliance check
var _ SessionIDGenerator = (*UUID5Generator)(nil)

// NewUUID5Generator creates a new UUID5 generator with the given namespace
func NewUUID5Generator(namespace string) *UUID5Generator {
	return &UUID5Generator{
		namespace: namespace,
	}
}

// Generate creates a deterministic session ID
// Format: UUID5(namespace + ":session:" + platform + ":" + userID + ":" + botUserID + ":" + channelID + ":" + threadID)
func (g *UUID5Generator) Generate(platform, userID, botUserID, channelID, threadID string) string {
	// Build the key from all components
	key := fmt.Sprintf("%s:%s:%s:%s:%s", platform, userID, botUserID, channelID, threadID)

	// Create unique string for hashing
	input := g.namespace + ":session:" + key

	// Generate UUID5 (deterministic)
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(input)).String()
}

// SimpleKeyGenerator generates session IDs using a simple concatenated key
// This is useful for debugging or when you don't need UUID format
type SimpleKeyGenerator struct{}

// Compile-time interface compliance check
var _ SessionIDGenerator = (*SimpleKeyGenerator)(nil)

// NewSimpleKeyGenerator creates a new simple key generator
func NewSimpleKeyGenerator() *SimpleKeyGenerator {
	return &SimpleKeyGenerator{}
}

// Generate creates a session ID by concatenating all components
// Format: platform:userID:botUserID:channelID:threadID
func (g *SimpleKeyGenerator) Generate(platform, userID, botUserID, channelID, threadID string) string {
	return fmt.Sprintf("%s:%s:%s:%s:%s", platform, userID, botUserID, channelID, threadID)
}

// WithSessionIDGenerator sets the session ID generator
// Use this to customize session ID generation per platform
func WithSessionIDGenerator(generator SessionIDGenerator) AdapterOption {
	return func(a *Adapter) {
		a.sessionIDGenerator = generator
	}
}
