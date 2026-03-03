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

// ZoneConfig defines per-zone aggregation limits.
type ZoneConfig struct {
	MaxMsgs  int // Maximum messages kept in the zone (0 = no limit)
	MaxBytes int // Maximum total content bytes (0 = no limit)
}

// zoneConfigs maps zone index to its configuration.
var zoneConfigs = map[int]ZoneConfig{
	ZoneInitialization: {MaxMsgs: 2, MaxBytes: 1000}, // 初始化: 2 messages (start + engine_starting)
	ZoneThinking:       {MaxMsgs: 5, MaxBytes: 1500}, // 思考区: 5 messages
	ZoneAction:         {MaxMsgs: 8, MaxBytes: 2000}, // 行动区: 8 messages
	ZoneOutput:         {MaxMsgs: 0, MaxBytes: 4000}, // 展示区: no msg limit, 4000 byte pages
	ZoneSummary:        {MaxMsgs: 1, MaxBytes: 0},    // 总结区: only latest
}

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
	"thinking":    {Aggregate: false, Immediate: true},   // Show immediately, 500ms dedup window in handler
	"tool_use":    {Aggregate: true, SameTypeOnly: true}, // Aggregate rapid invocations (e.g. multi-file reads)
	"tool_result": {Aggregate: true, SameTypeOnly: true}, // Aggregate rapid results (e.g. LS outputs)
	"answer":      {Aggregate: false, Immediate: true},   // Handled by StreamState.updateThrottled

	// Status events
	"error":         {Aggregate: false, Immediate: true}, // Show immediately - errors need instant feedback
	"result":        {Aggregate: false, Immediate: true}, // Show at end - final stats
	"session_stats": {Aggregate: false, Immediate: true}, // Show at end - session complete

	// Interactive events
	"permission_request": {Aggregate: false, Immediate: true}, // Need immediate user decision
	"danger_block":       {Aggregate: false, Immediate: true}, // Need immediate user decision

	// Plan mode events
	"plan_mode":      {Aggregate: false, Immediate: true}, // Handled by StreamState.updateThrottled
	"exit_plan_mode": {Aggregate: false, Immediate: true}, // Need immediate user decision

	// Question events
	"ask_user_question": {Aggregate: false, Immediate: true}, // Need immediate user response

	// Step events (OpenCode)
	"step_start":  {Aggregate: false, Immediate: true},   // Show immediately
	"step_finish": {Aggregate: true, SameTypeOnly: true}, // Can aggregate with next step

	// Command events
	"command_progress": {Aggregate: false, Immediate: true}, // Handled by handler/adapter throttling
	"command_complete": {Aggregate: false, Immediate: true}, // Show at end

	// Other
	"system": {Aggregate: true, SameTypeOnly: true}, // Can aggregate - low priority
	"user":   {Aggregate: false, Immediate: true},   // Show immediately - reflect user msg
	"raw":    {Aggregate: false, Immediate: true},   // Show immediately - raw output
}

// MessageAggregatorProcessor aggregates multiple rapid messages into one
type MessageAggregatorProcessor struct {
	logger *slog.Logger
	ctx    context.Context // Context for background timer operations

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
func NewMessageAggregatorProcessor(ctx context.Context, logger *slog.Logger, opts MessageAggregatorProcessorOptions) *MessageAggregatorProcessor {
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
		ctx:        ctx,
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

// getZoneLimits returns the effective MaxMsgs and MaxBytes for a message
// based on its zone_index metadata. Falls back to processor-level defaults.
func (p *MessageAggregatorProcessor) getZoneLimits(msg *base.ChatMessage) (maxMsgs, maxBytes int) {
	maxMsgs = p.maxMsgs
	maxBytes = p.maxBytes

	if msg.Metadata == nil {
		return
	}

	zoneIndex, ok := msg.Metadata["zone_index"].(int)
	if !ok {
		return
	}

	if zc, found := zoneConfigs[zoneIndex]; found {
		if zc.MaxMsgs > 0 {
			maxMsgs = zc.MaxMsgs
		}
		if zc.MaxBytes > 0 {
			maxBytes = zc.MaxBytes
		}
	}
	return
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
func (p *MessageAggregatorProcessor) bufferMessage(_ context.Context, msg *base.ChatMessage, eventConfig EventConfig, eventType string) (*base.ChatMessage, error) {
	// Build session key with event type for SameTypeOnly aggregation
	sessionKey := msg.Platform + ":" + msg.SessionID
	if eventConfig.SameTypeOnly {
		sessionKey = sessionKey + ":" + eventType
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
		// Check ctx status to avoid timer leak if context is already cancelled
		if p.ctx.Err() == nil {
			buf.timer = time.AfterFunc(p.window, func() {
				// Check ctx again before flushing to avoid work on cancelled context
				if p.ctx.Err() == nil {
					p.flushBufferByTimer(p.ctx, sessionKey)
				}
			})
		}

		p.buffers[sessionKey] = buf
	}

	// Check buffer limits before adding new message
	newMsgBytes := len(msg.Content)

	// Get zone-aware limits (falls back to processor-level defaults)
	zoneMsgs, zoneBytes := p.getZoneLimits(msg)

	// Sliding window: evict oldest messages when zone limit is reached
	// This keeps the most recent N messages visible in the UI
	if zoneMsgs > 0 && len(buf.messages) >= zoneMsgs {
		// Evict oldest message to make room
		evicted := buf.messages[0]
		buf.messages = buf.messages[1:]
		evictedBytes := len(evicted.Content)
		buf.totalBytes -= evictedBytes
		if buf.totalBytes < 0 {
			buf.totalBytes = 0
		}

		// Record eviction metrics
		evictedEventType := "unknown"
		if evicted.Metadata != nil {
			if et, ok := evicted.Metadata["event_type"].(string); ok && et != "" {
				evictedEventType = et
			}
		}
		MessagesDroppedTotal.WithLabelValues(evictedEventType, msg.Platform, "sliding_window").Inc()

		p.logger.Debug("Sliding window eviction (message count)",
			"session_key", sessionKey,
			"evicted_bytes", evictedBytes,
			"remaining_msgs", len(buf.messages))
	}

	// Sliding window for byte limit: evict oldest until under threshold
	if zoneBytes > 0 {
		for len(buf.messages) > 0 && buf.totalBytes+newMsgBytes > zoneBytes {
			evicted := buf.messages[0]
			buf.messages = buf.messages[1:]
			evictedBytes := len(evicted.Content)
			buf.totalBytes -= evictedBytes
			if buf.totalBytes < 0 {
				buf.totalBytes = 0
			}

			evictedEventType := "unknown"
			if evicted.Metadata != nil {
				if et, ok := evicted.Metadata["event_type"].(string); ok && et != "" {
					evictedEventType = et
				}
			}
			MessagesDroppedTotal.WithLabelValues(evictedEventType, msg.Platform, "sliding_window_bytes").Inc()
		}
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
func (p *MessageAggregatorProcessor) flushBufferByTimer(ctx context.Context, sessionKey string) {
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

		if err := sender.SendAggregatedMessage(ctx, aggregated); err != nil {
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
	eventType, _ := finalMsg.Metadata["event_type"].(string)
	eventConfig := p.getEventConfig(eventType)

	sessionKey := finalMsg.Platform + ":" + finalMsg.SessionID
	if eventConfig.SameTypeOnly {
		sessionKey = sessionKey + ":" + eventType
	}

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
	var platform string
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
		Type:        first.Type,
		Platform:    first.Platform,
		SessionID:   first.SessionID,
		UserID:      first.UserID,
		Content:     combined.String(),
		MessageID:   first.MessageID,
		Timestamp:   first.Timestamp,
		Metadata:    make(map[string]any),
		RichContent: first.RichContent,
	}

	// Copy metadata from first message
	if first.Metadata != nil {
		for k, v := range first.Metadata {
			aggregated.Metadata[k] = v
		}
	}

	// Store original messages for sophisticated rendering in platform builders
	aggregated.Metadata["_original_messages"] = messages

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
			Blocks:      make([]any, 0),
			Embeds:      make([]any, 0),
		}
	}

	merged := &base.RichContent{
		ParseMode:      firstRichContent.ParseMode,
		Attachments:    make([]base.Attachment, 0),
		Blocks:         make([]any, 0),
		Embeds:         make([]any, 0),
		InlineKeyboard: firstRichContent.InlineKeyboard,
	}

	for _, msg := range messages {
		if msg.RichContent == nil {
			continue
		}

		// Merge attachments
		merged.Attachments = append(merged.Attachments, msg.RichContent.Attachments...)

		// Merge blocks
		merged.Blocks = append(merged.Blocks, msg.RichContent.Blocks...)

		// Merge embeds
		merged.Embeds = append(merged.Embeds, msg.RichContent.Embeds...)
	}

	return merged
}

// ResetSession clears all buffers for a specific session.
func (p *MessageAggregatorProcessor) ResetSession(platform, sessionID string) {
	sessionKey := platform + ":" + sessionID
	p.mu.Lock()
	defer p.mu.Unlock()

	buf, exists := p.buffers[sessionKey]
	if !exists {
		return
	}

	if buf.timer != nil {
		buf.timer.Stop()
	}

	// Record dropped messages
	for _, msg := range buf.messages {
		eventType, _ := msg.Metadata["event_type"].(string)
		if eventType == "" {
			eventType = "unknown"
		}
		MessagesDroppedTotal.WithLabelValues(eventType, msg.Platform, "reset").Inc()
	}

	delete(p.buffers, sessionKey)
	p.logger.Debug("Message aggregator: session buffers cleared", "platform", platform, "session_id", sessionID)
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

// Verify MessageAggregatorProcessor implements MessageProcessor at compile time
var _ MessageProcessor = (*MessageAggregatorProcessor)(nil)
