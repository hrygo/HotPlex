package slack

import (
	"context"
)

// BroadcastResponder generates a polite response for broadcast messages
// (messages without explicit @ mentions in multibot mode).
// This allows for future integration with native brain for intelligent responses.
type BroadcastResponder interface {
	// Respond generates a response for the given user message.
	// In broadcast mode, this is called when no bot is explicitly mentioned.
	Respond(ctx context.Context, userMessage string) (string, error)
}

// StaticBroadcastResponder is a simple implementation that returns a fixed response.
// Use this for basic deployments or as a fallback.
type StaticBroadcastResponder struct {
	response string
}

// NewStaticBroadcastResponder creates a responder with a fixed response.
func NewStaticBroadcastResponder(response string) *StaticBroadcastResponder {
	return &StaticBroadcastResponder{response: response}
}

// Respond returns the configured static response.
func (r *StaticBroadcastResponder) Respond(_ context.Context, _ string) (string, error) {
	return r.response, nil
}

// DefaultBroadcastResponse is the default polite response for broadcast messages.
const DefaultBroadcastResponse = "Hello! I'm ready to help. Please @mention me if you'd like me to respond specifically to you."
