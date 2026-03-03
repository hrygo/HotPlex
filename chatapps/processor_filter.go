package chatapps

import (
	"context"
	"log/slog"

	"github.com/hrygo/hotplex/chatapps/base"
)

// hiddenEvents lists event types that should be filtered out (noise for the user).
// Per design: https://docs/chatapps/slack-message-grouping-design.md
var hiddenEvents = map[string]bool{
	"system": true, // System-level info
	"user":   true, // User message reflection (redundant)
	"raw":    true, // Unparsed content
}

// MessageFilterProcessor drops noise events before they enter the rest of the chain.
type MessageFilterProcessor struct {
	logger *slog.Logger
}

// NewMessageFilterProcessor creates a new MessageFilterProcessor.
func NewMessageFilterProcessor(logger *slog.Logger) *MessageFilterProcessor {
	if logger == nil {
		logger = slog.Default()
	}
	return &MessageFilterProcessor{logger: logger}
}

// Name returns the processor name.
func (p *MessageFilterProcessor) Name() string {
	return "MessageFilterProcessor"
}

// Order returns the processor order – must run before all others.
func (p *MessageFilterProcessor) Order() int {
	return int(OrderFilter)
}

// Process drops hidden events by returning nil.
func (p *MessageFilterProcessor) Process(_ context.Context, msg *base.ChatMessage) (*base.ChatMessage, error) {
	if msg == nil {
		return nil, nil
	}

	eventType, _ := msg.Metadata["event_type"].(string)
	if hiddenEvents[eventType] {
		p.logger.Debug("Filtered hidden event",
			"event_type", eventType,
			"platform", msg.Platform,
			"session_id", msg.SessionID)
		return nil, nil // drop
	}

	return msg, nil
}

// Verify MessageFilterProcessor implements MessageProcessor at compile time
var _ MessageProcessor = (*MessageFilterProcessor)(nil)
