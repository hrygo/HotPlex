package brain

import (
	"context"

	"github.com/hrygo/hotplex/brain/llm"
)

// Brain represents the core "System 1" intelligence for HotPlex.
// It provides fast, structured, and low-cost reasoning capabilities.
type Brain interface {
	// Chat generates a plain text response for a given prompt.
	// Best used for simple questions, greetings, or summarization.
	Chat(ctx context.Context, prompt string) (string, error)

	// Analyze performs structured analysis and returns the result in the target struct.
	// The target must be a pointer to a struct that can be unmarshaled from JSON.
	// Useful for intent routing, safety checks, and complex data extraction.
	Analyze(ctx context.Context, prompt string, target any) error
}

// StreamingBrain extends Brain with streaming capabilities.
// It provides token-by-token streaming for real-time responses.
type StreamingBrain interface {
	Brain

	// ChatStream returns a channel that streams tokens as they are generated.
	// The channel is closed when the stream completes or an error occurs.
	// Best used for long responses, real-time UI updates, or progressive rendering.
	ChatStream(ctx context.Context, prompt string) (<-chan string, error)
}

// HealthStatus represents the health status of the Brain service.
// Re-exported from llm package for convenience.
type HealthStatus = llm.HealthStatus

var (
	globalBrain Brain
)

// Global returns the globally configured Brain instance.
// If no brain is configured, it returns nil.
func Global() Brain {
	return globalBrain
}

// SetGlobal sets the global Brain instance.
func SetGlobal(b Brain) {
	globalBrain = b
}
