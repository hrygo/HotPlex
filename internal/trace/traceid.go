package trace

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
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
	if _, err := rand.Read(randomBytes); err != nil {
		// Fallback to time-based random on error
		slog.Warn("trace: rand.Read failed, using time-based fallback", "error", err)
		randomBytes[0] = byte(timestamp >> 24)
		randomBytes[1] = byte(timestamp >> 16)
		randomBytes[2] = byte(timestamp >> 8)
		randomBytes[3] = byte(timestamp)
		randomBytes[4] = byte(timestamp >> 20)
		randomBytes[5] = byte(timestamp >> 12)
		randomBytes[6] = byte(timestamp >> 4)
		randomBytes[7] = byte(timestamp)
	}
	randomHex := hex.EncodeToString(randomBytes)[:8]

	ts := strings.TrimLeft(hex.EncodeToString([]byte{byte(timestamp >> 24), byte(timestamp >> 16), byte(timestamp >> 8), byte(timestamp)}), "0")
	return strings.Join([]string{g.prefix, ts, randomHex}, "-")
}

// GenerateSimple generates a simple 16-character hex trace ID.
func GenerateSimple() string {
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to time-based random on error
		slog.Warn("trace: rand.Read failed in GenerateSimple, using time-based fallback", "error", err)
		timestamp := time.Now().UnixNano()
		for i := range bytes {
			bytes[i] = byte(timestamp >> (i * 8))
		}
	}
	return hex.EncodeToString(bytes)[:16]
}
