package base

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// MessageHandlerFunc defines the function signature for message handlers
type MessageHandlerFunc func(ctx context.Context, msg *ChatMessage) (*ChatMessage, error)

// MessageHandlerChain processes messages through a chain of handlers
type MessageHandlerChain struct {
	handlers []MessageHandlerFunc
	logger   *slog.Logger
}

// NewMessageHandlerChain creates a new MessageHandlerChain
func NewMessageHandlerChain(logger *slog.Logger) *MessageHandlerChain {
	if logger == nil {
		logger = slog.Default()
	}
	return &MessageHandlerChain{
		handlers: make([]MessageHandlerFunc, 0),
		logger:   logger,
	}
}

// AddHandler adds a handler to the chain
func (c *MessageHandlerChain) AddHandler(fn MessageHandlerFunc) *MessageHandlerChain {
	c.handlers = append(c.handlers, fn)
	return c
}

// Process processes a message through all handlers in sequence
func (c *MessageHandlerChain) Process(ctx context.Context, msg *ChatMessage) (*ChatMessage, error) {
	result := msg
	var err error

	for i, handler := range c.handlers {
		result, err = handler(ctx, result)
		if err != nil {
			c.logger.Error("Handler chain error",
				"handler_index", i,
				"error", err)
			return result, err
		}
		if result == nil {
			c.logger.Debug("Handler returned nil, skipping remaining handlers")
			return nil, nil
		}
	}

	return result, nil
}

// HandlerCount returns the number of handlers in the chain
func (c *MessageHandlerChain) HandlerCount() int {
	return len(c.handlers)
}

// =============================================================================
// Built-in Handlers
// =============================================================================

// RichContentHandler processes RichContent (reactions, attachments, blocks)
func RichContentHandler(ctx context.Context, msg *ChatMessage) (*ChatMessage, error) {
	// RichContent processing is handled by the adapter's sender
	// This handler mainly ensures RichContent is properly initialized
	if msg.RichContent == nil {
		msg.RichContent = &RichContent{}
	}
	return msg, nil
}

// MessageAggregatorHandler aggregates multiple messages into one
type MessageAggregatorHandler struct {
	mu          sync.Mutex
	messages    map[string][]*ChatMessage
	aggregateFn func(messages []*ChatMessage) string
	timeout     time.Duration
}

type MessageAggregatorOption func(*MessageAggregatorHandler)

func WithAggregateFn(fn func(messages []*ChatMessage) string) MessageAggregatorOption {
	return func(h *MessageAggregatorHandler) {
		h.aggregateFn = fn
	}
}

func WithAggregateTimeout(timeout time.Duration) MessageAggregatorOption {
	return func(h *MessageAggregatorHandler) {
		h.timeout = timeout
	}
}

func NewMessageAggregatorHandler(opts ...MessageAggregatorOption) *MessageAggregatorHandler {
	h := &MessageAggregatorHandler{
		messages: make(map[string][]*ChatMessage),
		timeout:  2 * time.Second,
	}

	for _, opt := range opts {
		opt(h)
	}

	return h
}

func (h *MessageAggregatorHandler) Handle(ctx context.Context, msg *ChatMessage) (*ChatMessage, error) {
	if h.aggregateFn == nil {
		return msg, nil
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	sessionKey := msg.SessionID
	messages := h.messages[sessionKey]

	// Add current message
	messages = append(messages, msg)
	h.messages[sessionKey] = messages

	_ = len(messages) // Used for future aggregation logic

	return msg, nil
}

// Flush flushes all pending messages for a session
func (h *MessageAggregatorHandler) Flush(ctx context.Context, sessionID string) ([]*ChatMessage, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	messages := h.messages[sessionID]
	if len(messages) == 0 {
		return nil, nil
	}

	delete(h.messages, sessionID)
	return messages, nil
}

// RateLimitHandler controls message sending frequency
type RateLimitHandler struct {
	mu           sync.Mutex
	lastSendTime map[string]time.Time
	minInterval  time.Duration
}

func NewRateLimitHandler(minInterval time.Duration) *RateLimitHandler {
	return &RateLimitHandler{
		lastSendTime: make(map[string]time.Time),
		minInterval:  minInterval,
	}
}

func (h *RateLimitHandler) Handle(ctx context.Context, msg *ChatMessage) (*ChatMessage, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// For now, just record the send time
	// Actual rate limiting would be done at the sender level
	h.lastSendTime[msg.SessionID] = time.Now()

	return msg, nil
}

// GetLastSendTime returns the last send time for a session
func (h *RateLimitHandler) GetLastSendTime(sessionID string) time.Time {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.lastSendTime[sessionID]
}
