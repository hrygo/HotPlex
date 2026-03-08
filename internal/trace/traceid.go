package trace

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"
)

// Generator generates trace IDs for distributed tracing.
type Generator struct {
	prefix string
}

// New creates a new trace ID generator with optional prefix.
func New(prefix string) *Generator {
	if prefix == "" {
		prefix = "hotplex"
	}
	return &Generator{prefix: prefix}
}

// Generate generates a new trace ID.
// Format: {prefix}-{timestamp}-{random}
// Example: hotplex-6076b8cb-8a2f4d3e
func (g *Generator) Generate() string {
	// Get current timestamp in hex (8 chars)
	timestamp := time.Now().UnixNano() >> 20 // Shift to get ~8 hex chars

	// Generate 8 random bytes (16 hex chars)
	randomBytes := make([]byte, 8)
	rand.Read(randomBytes)
	randomHex := hex.EncodeToString(randomBytes)[:8]

	ts := strings.TrimLeft(hex.EncodeToString([]byte{byte(timestamp >> 24), byte(timestamp >> 16), byte(timestamp >> 8), byte(timestamp)}), "0")
	return strings.Join([]string{g.prefix, ts, randomHex}, "-")
}

// GenerateSimple generates a simple 16-character hex trace ID.
func GenerateSimple() string {
	bytes := make([]byte, 8)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)[:16]
}
