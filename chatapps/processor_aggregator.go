package chatapps

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/hrygo/hotplex/chatapps/base"
)

// Buffer safety limits to prevent OOM
const (
	maxBufferMsgs  = 50   // Maximum messages in buffer
	maxBufferBytes = 4000 // Maximum total content bytes (Slack single message limit)
)

// EventConfig defines aggregation behavior for specific event types
type EventConfig struct {
	Aggregate    bool // Whether to aggregate messages of this type
	SameTypeOnly bool // Only aggregate with same event type
	Immediate    bool // Send immediately, skip aggregation
	UseUpdate    bool // Use chat.update for streaming updates
	MinContent   int  // Minimum content length to skip aggregation (0 = use global default)
}

// defaultEventConfig defines default aggregation behavior for each event type
// Per spec: https://docs/chatapps/engine-events-slack-ux-spec.md
var defaultEventConfig = map[string]EventConfig{
	// Session lifecycle events (0.4, 0.5, 0.6)
	"session_start":         {Aggregate: false, Immediate: true},   // Show immediately - first message/cold start
	"engine_starting":       {Aggregate: true, SameTypeOnly: true}, // Can aggregate - during engine init
	"user_message_received": {Aggregate: false, Immediate: true},   // Show immediately - acknowledgment

	// Core events
	"thinking":    {Aggregate: false, Immediate: true},                                     // Show immediately, 500ms dedup window in handler
	"tool_use":    {Aggregate: true, SameTypeOnly: true, Immediate: false, MinContent: 50}, // 500ms aggregation
	"tool_result": {Aggregate: false, Immediate: true},                                     // Per spec: 不聚合 - 立即发送
	"answer":      {Aggregate: true, UseUpdate: true, Immediate: false},                    // Stream with chat.update (1/sec)

	// Status events
	"error":         {Aggregate: false, Immediate: true}, // Show immediately - errors need instant feedback
	"result":        {Aggregate: false, Immediate: true}, // Show at end - final stats
	"session_stats": {Aggregate: false, Immediate: true}, // Show at end - session complete

	// Interactive events
	"permission_request": {Aggregate: false, Immediate: true}, // Need immediate user decision
	"danger_block":       {Aggregate: false, Immediate: true}, // Need immediate user decision

	// Plan mode events
	"plan_mode":      {Aggregate: true, UseUpdate: true},  // Stream with chat.update
	"exit_plan_mode": {Aggregate: false, Immediate: true}, // Need immediate user decision

	// Question events
	"ask_user_question": {Aggregate: false, Immediate: true}, // Need immediate user response

	// Step events (OpenCode)
	"step_start":  {Aggregate: false, Immediate: true},   // Show immediately
	"step_finish": {Aggregate: true, SameTypeOnly: true}, // Can aggregate with next step

	// Command events
	"command_progress": {Aggregate: true, UseUpdate: true},  // Stream with chat.update
	"command_complete": {Aggregate: false, Immediate: true}, // Show at end

	// Other
	"system": {Aggregate: true, SameTypeOnly: true}, // Can aggregate - low priority
	"user":   {Aggregate: false, Immediate: true},   // Show immediately - reflect user msg
	"raw":    {Aggregate: false, Immediate: true},   // Show immediately - raw output
}

// MessageAggregatorProcessor aggregates multiple rapid messages into one
type MessageAggregatorProcessor struct {
	logger *slog.Logger

	// Buffer for aggregating messages
	buffers map[string]*messageBuffer
	mu      sync.Mutex

	// Configuration
	window     time.Duration // Time window for aggregation
	minContent int           // Minimum content difference to trigger send
	maxMsgs    int           // Maximum messages in buffer (default: maxBufferMsgs)
	maxBytes   int           // Maximum total bytes in buffer (default: maxBufferBytes)

	// Sender for flushing aggregated messages
	sender AggregatedMessageSender
}

// AggregatedMessageSender sends aggregated messages
type AggregatedMessageSender interface {
	SendAggregatedMessage(ctx context.Context, msg *base.ChatMessage) error
}

// messageBuffer holds buffered messages for aggregation
type messageBuffer struct {
	messages   []*base.ChatMessage
	createdAt  time.Time
	timer      *time.Timer
	done       chan *base.ChatMessage
	eventType  string // Event type for same-type aggregation
	messageTS  string // Timestamp for chat.update (first message)
	totalBytes int    // Total bytes in buffer for limit checking
}

// MessageAggregatorProcessorOptions configures the aggregator
type MessageAggregatorProcessorOptions struct {
	Window     time.Duration // Time window to wait for more messages
	MinContent int           // Minimum characters before sending immediately
	MaxMsgs    int           // Maximum messages in buffer (default: maxBufferMsgs)
	MaxBytes   int           // Maximum total bytes in buffer (default: maxBufferBytes)
}

// NewMessageAggregatorProcessor creates a new MessageAggregatorProcessor
func NewMessageAggregatorProcessor(logger *slog.Logger, opts MessageAggregatorProcessorOptions) *MessageAggregatorProcessor {
	if logger == nil {
		logger = slog.Default()
	}

	// Set defaults
	if opts.Window == 0 {
		opts.Window = 100 * time.Millisecond
	}
	if opts.MinContent == 0 {
		opts.MinContent = 200
	}
	if opts.MaxMsgs == 0 {
		opts.MaxMsgs = maxBufferMsgs
	}
	if opts.MaxBytes == 0 {
		opts.MaxBytes = maxBufferBytes
	}

	return &MessageAggregatorProcessor{
		logger:     logger,
		buffers:    make(map[string]*messageBuffer),
		window:     opts.Window,
		minContent: opts.MinContent,
		maxMsgs:    opts.MaxMsgs,
		maxBytes:   opts.MaxBytes,
	}
}

// SetSender sets the sender for flushing aggregated messages
func (p *MessageAggregatorProcessor) SetSender(sender AggregatedMessageSender) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sender = sender
}

// Name returns the processor name
func (p *MessageAggregatorProcessor) Name() string {
	return "MessageAggregatorProcessor"
}

// Order returns the processor order
func (p *MessageAggregatorProcessor) Order() int {
	return int(OrderAggregation)
}

// getEventConfig returns the EventConfig for a given event type
func (p *MessageAggregatorProcessor) getEventConfig(eventType string) EventConfig {
	if config, ok := defaultEventConfig[eventType]; ok {
		return config
	}
	// Default config for unknown event types: aggregate normally
	// Log unknown event types for debugging
	p.logger.Debug("Unknown event type, using default aggregation", "event_type", eventType)
	return EventConfig{Aggregate: true}
}

// Process aggregates messages with event-type awareness
func (p *MessageAggregatorProcessor) Process(ctx context.Context, msg *base.ChatMessage) (*base.ChatMessage, error) {
	if msg == nil || msg.Metadata == nil {
		return msg, nil
	}

	// Check if this is a stream message
	isStream, _ := msg.Metadata["stream"].(bool)
	if !isStream {
		return msg, nil
	}

	// Get event type from metadata
	eventType, _ := msg.Metadata["event_type"].(string)
	if eventType == "" {
		eventType = "unknown"
	}

	// Get event config
	eventConfig := p.getEventConfig(eventType)

	// Set use_update flag if UseUpdate is enabled for this event type
	if eventConfig.UseUpdate {
		if msg.Metadata == nil {
			msg.Metadata = make(map[string]any)
		}
		msg.Metadata["use_update"] = true
	}

	// Check if Immediate flag is set - send immediately without aggregation
	if eventConfig.Immediate {
		return msg, nil
	}

	// Check if this event type should not be aggregated
	if !eventConfig.Aggregate {
		return msg, nil
	}

	// Check if this is the final message
	isFinal, _ := msg.Metadata["is_final"].(bool)
	if isFinal {
		return p.flushBuffer(msg)
	}

	// Check content length - send immediately if long enough
	// Use event-type specific MinContent if set, otherwise use global default
	minContent := p.minContent
	if eventConfig.MinContent > 0 {
		minContent = eventConfig.MinContent
	}
	if len(msg.Content) >= minContent {
		return msg, nil
	}
	if len(msg.Content) >= p.minContent {
		return msg, nil
	}

	// Buffer the message with event-type awareness
	return p.bufferMessage(ctx, msg, eventConfig, eventType)
}

// bufferMessage adds message to buffer and returns nil (will be sent later)
// Implements buffer safety limits with FIFO overflow strategy
// Note: This method handles its own locking to support safe flush operations
func (p *MessageAggregatorProcessor) bufferMessage(ctx context.Context, msg *base.ChatMessage, eventConfig EventConfig, eventType string) (*base.ChatMessage, error) {
	// Build session key with event type for SameTypeOnly aggregation
	sessionKey := msg.Platform + ":" + msg.SessionID
	if eventConfig.SameTypeOnly {
		sessionKey = sessionKey + ":" + eventType
	}

	// Helper function to handle buffer overflow flush
	// Returns the buffer after flush (may be nil or new)
	handleOverflowFlush := func(buf *messageBuffer, overflowType string) *messageBuffer {
		p.logger.Warn("Buffer overflow ("+overflowType+"), forcing flush",
			"session_key", sessionKey)

		// Record dropped metrics for overflow
		for _, droppedMsg := range buf.messages {
			droppedEventType, _ := droppedMsg.Metadata["event_type"].(string)
			if droppedEventType == "" {
				droppedEventType = "unknown"
			}
			MessagesDroppedTotal.WithLabelValues(droppedEventType, msg.Platform, "overflow").Inc()
		}

		// Stop the timer to prevent double flush
		if buf.timer != nil {
			buf.timer.Stop()
		}

		// Clear buffer messages (they will be dropped)
		buf.messages = nil
		buf.totalBytes = 0

		// Return nil so a new buffer will be created
		return nil
	}

	p.mu.Lock()

	buf, exists := p.buffers[sessionKey]
	if !exists {
		buf = &messageBuffer{
			messages:   make([]*base.ChatMessage, 0, 10),
			createdAt:  time.Now(),
			done:       make(chan *base.ChatMessage, 1),
			eventType:  eventType,
			messageTS:  "", // Will be set on first message send
			totalBytes: 0,
		}

		// Set timer to flush buffer after window
		buf.timer = time.AfterFunc(p.window, func() {
			p.flushBufferByTimer(sessionKey)
		})

		p.buffers[sessionKey] = buf
	}

	// Check buffer limits before adding new message
	newMsgBytes := len(msg.Content)
	needNewBuffer := false

	// Check message count limit
	if len(buf.messages) >= p.maxMsgs {
		buf = handleOverflowFlush(buf, "message count")
		needNewBuffer = true
	}

	// Check byte limit (only if we still have a buffer)
	if buf != nil && buf.totalBytes+newMsgBytes > p.maxBytes {
		buf = handleOverflowFlush(buf, "bytes")
		needNewBuffer = true
	}

	// Create new buffer if needed
	if needNewBuffer || buf == nil {
		buf = &messageBuffer{
			messages:   make([]*base.ChatMessage, 0, 10),
			createdAt:  time.Now(),
			done:       make(chan *base.ChatMessage, 1),
			eventType:  eventType,
			messageTS:  "",
			totalBytes: 0,
		}
		buf.timer = time.AfterFunc(p.window, func() {
			p.flushBufferByTimer(sessionKey)
		})
		p.buffers[sessionKey] = buf
	}

	// Capture messageTS from first message if use_update is enabled
	useUpdate, _ := msg.Metadata["use_update"].(bool)
	if useUpdate && buf.messageTS == "" {
		// Extract ts from metadata if available (passed from adapter)
		if ts, ok := msg.Metadata["message_ts"].(string); ok && ts != "" {
			buf.messageTS = ts
			p.logger.Debug("Captured message_ts for chat.update", "session_key", sessionKey, "message_ts", ts)
		}
	}

	// Add message to buffer
	buf.messages = append(buf.messages, msg)
	buf.totalBytes += newMsgBytes

	// Record metrics
	MessagesAggregatedTotal.WithLabelValues(eventType, msg.Platform).Inc()
	BufferSizeGauge.WithLabelValues(msg.Platform).Set(float64(len(buf.messages)))

	p.logger.Debug("Message buffered for aggregation",
		"session_key", sessionKey,
		"buffer_size", len(buf.messages),
		"content_len", newMsgBytes,
		"total_bytes", buf.totalBytes)

	p.mu.Unlock()

	// Return nil to indicate message is buffered (not sent yet)
	return nil, nil
}

// flushBufferByTimer flushes buffer when timer expires
func (p *MessageAggregatorProcessor) flushBufferByTimer(sessionKey string) {
	p.mu.Lock()
	buf, exists := p.buffers[sessionKey]
	sender := p.sender
	if !exists {
		p.mu.Unlock()
		return
	}

	// Extract platform and event_type before unlocking
	var platform, eventType string
	if len(buf.messages) > 0 && buf.messages[0] != nil {
		platform = buf.messages[0].Platform
		if et, ok := buf.messages[0].Metadata["event_type"].(string); ok {
			eventType = et
		} else {
			eventType = "stream"
		}
	}

	// Calculate buffer duration before removing
	duration := time.Since(buf.createdAt)
	msgCount := len(buf.messages)

	// Remove buffer
	delete(p.buffers, sessionKey)
	p.mu.Unlock()

	// Aggregate messages
	aggregated := p.aggregateMessages(buf.messages)
	if aggregated == nil {
		return
	}

	// Record metrics
	MessagesFlushedTotal.WithLabelValues(eventType, platform, "timer").Inc()
	BufferDurationHistogram.WithLabelValues(platform).Observe(duration.Seconds())
	MessageSizeHistogram.WithLabelValues(eventType, platform).Observe(float64(len(aggregated.Content)))
	BufferSizeGauge.WithLabelValues(platform).Set(0)

	// Send via sender if available
	if sender != nil {
		p.logger.Info("Flushing aggregated message via sender",
			"session_key", sessionKey,
			"messages_count", msgCount,
			"content_len", len(aggregated.Content))

		if err := sender.SendAggregatedMessage(context.Background(), aggregated); err != nil {
			p.logger.Error("Failed to send aggregated message",
				"session_key", sessionKey,
				"error", err)
		}
	} else {
		p.logger.Warn("No sender configured, aggregated message dropped",
			"session_key", sessionKey,
			"messages_count", msgCount)
	}
}

// flushBuffer flushes buffer for final message
func (p *MessageAggregatorProcessor) flushBuffer(finalMsg *base.ChatMessage) (*base.ChatMessage, error) {
	sessionKey := finalMsg.Platform + ":" + finalMsg.SessionID

	p.mu.Lock()
	buf, exists := p.buffers[sessionKey]
	if !exists {
		p.mu.Unlock()
		return finalMsg, nil
	}

	// Stop timer
	if buf.timer != nil {
		buf.timer.Stop()
	}

	// Extract platform and event_type before unlocking
	var platform, eventType string
	if len(buf.messages) > 0 && buf.messages[0] != nil {
		platform = buf.messages[0].Platform
		if et, ok := buf.messages[0].Metadata["event_type"].(string); ok {
			eventType = et
		} else {
			eventType = "stream"
		}
	}

	// Calculate buffer duration before removing
	duration := time.Since(buf.createdAt)
	msgCount := len(buf.messages)

	// Add final message
	buf.messages = append(buf.messages, finalMsg)
	buf.totalBytes += len(finalMsg.Content)

	// Remove buffer
	delete(p.buffers, sessionKey)
	p.mu.Unlock()

	// Aggregate all messages
	aggregated := p.aggregateMessages(buf.messages)

	p.logger.Debug("Buffer flushed",
		"session_key", sessionKey,
		"messages_count", msgCount,
		"aggregated_len", len(aggregated.Content))

	// Record metrics
	MessagesFlushedTotal.WithLabelValues(eventType, platform, "final").Inc()
	BufferDurationHistogram.WithLabelValues(platform).Observe(duration.Seconds())
	MessageSizeHistogram.WithLabelValues(eventType, platform).Observe(float64(len(aggregated.Content)))
	BufferSizeGauge.WithLabelValues(platform).Set(0)

	return aggregated, nil
}

// aggregateMessages combines multiple messages into one
func (p *MessageAggregatorProcessor) aggregateMessages(messages []*base.ChatMessage) *base.ChatMessage {
	if len(messages) == 0 {
		return nil
	}

	if len(messages) == 1 {
		return messages[0]
	}

	// Use first message as base
	first := messages[0]

	// Calculate total content length for efficient pre-allocation
	totalLen := 0
	for _, msg := range messages {
		totalLen += len(msg.Content)
	}
	// Add space for newlines between messages
	totalLen += len(messages) - 1

	// Combine content with pre-allocated buffer
	var combined strings.Builder
	combined.Grow(totalLen)

	for i, msg := range messages {
		if i > 0 {
			combined.WriteString("\n")
		}
		combined.WriteString(msg.Content)
	}

	// Create aggregated message
	aggregated := &base.ChatMessage{
		Platform:    first.Platform,
		SessionID:   first.SessionID,
		UserID:      first.UserID,
		Content:     combined.String(),
		MessageID:   first.MessageID,
		Timestamp:   first.Timestamp,
		Metadata:    first.Metadata,
		RichContent: first.RichContent,
	}

	// Merge RichContent from all messages
	if len(messages) > 1 {
		aggregated.RichContent = p.mergeRichContent(messages)
	}

	return aggregated
}

// mergeRichContent merges RichContent from multiple messages
func (p *MessageAggregatorProcessor) mergeRichContent(messages []*base.ChatMessage) *base.RichContent {
	// Get first non-nil RichContent for default values
	var firstRichContent *base.RichContent
	for _, msg := range messages {
		if msg.RichContent != nil {
			firstRichContent = msg.RichContent
			break
		}
	}

	// If no RichContent found, return a default one
	if firstRichContent == nil {
		return &base.RichContent{
			Attachments: make([]base.Attachment, 0),
			Reactions:   make([]base.Reaction, 0),
			Blocks:      make([]any, 0),
			Embeds:      make([]any, 0),
		}
	}

	merged := &base.RichContent{
		ParseMode:      firstRichContent.ParseMode,
		Attachments:    make([]base.Attachment, 0),
		Reactions:      make([]base.Reaction, 0),
		Blocks:         make([]any, 0),
		Embeds:         make([]any, 0),
		InlineKeyboard: firstRichContent.InlineKeyboard,
	}

	seenReactions := make(map[string]bool)

	for _, msg := range messages {
		if msg.RichContent == nil {
			continue
		}

		// Merge attachments
		merged.Attachments = append(merged.Attachments, msg.RichContent.Attachments...)

		// Merge reactions (deduplicate)
		for _, reaction := range msg.RichContent.Reactions {
			key := reaction.Name
			if !seenReactions[key] {
				merged.Reactions = append(merged.Reactions, reaction)
				seenReactions[key] = true
			}
		}

		// Merge blocks
		merged.Blocks = append(merged.Blocks, msg.RichContent.Blocks...)

		// Merge embeds
		merged.Embeds = append(merged.Embeds, msg.RichContent.Embeds...)
	}

	return merged
}

// Stop stops the aggregator and cleans up buffers
func (p *MessageAggregatorProcessor) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, buf := range p.buffers {
		if buf.timer != nil {
			buf.timer.Stop()
		}
		// Record dropped messages for any remaining buffered messages
		for _, msg := range buf.messages {
			eventType, _ := msg.Metadata["event_type"].(string)
			if eventType == "" {
				eventType = "unknown"
			}
			MessagesDroppedTotal.WithLabelValues(eventType, msg.Platform, "stop").Inc()
		}
	}

	p.buffers = make(map[string]*messageBuffer)
	p.logger.Info("Message aggregator stopped")
}
