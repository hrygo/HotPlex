package slack

import (
	"context"
	"testing"
)

func TestStaticBroadcastResponder(t *testing.T) {
	responder := NewStaticBroadcastResponder("Hello! How can I help you?")

	tests := []struct {
		name     string
		ctx      context.Context
		userMsg  string
		expected string
	}{
		{
			name:     "basic response",
			ctx:      context.Background(),
			userMsg:  "hello",
			expected: "Hello! How can I help you?",
		},
		{
			name:     "empty message",
			ctx:      context.Background(),
			userMsg:  "",
			expected: "Hello! How can I help you?",
		},
		{
			name:     "complex message",
			ctx:      context.Background(),
			userMsg:  "I need help with my code",
			expected: "Hello! How can I help you?",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := responder.Respond(tt.ctx, tt.userMsg)
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
			if result != tt.expected {
				t.Errorf("Respond() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestDefaultBroadcastResponse(t *testing.T) {
	if DefaultBroadcastResponse == "" {
		t.Error("DefaultBroadcastResponse should not be empty")
	}
}
