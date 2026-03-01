package chatapps

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/hrygo/hotplex/chatapps/base"
	"github.com/hrygo/hotplex/engine"
	"github.com/hrygo/hotplex/event"
	eng "github.com/hrygo/hotplex/internal/engine"
	"github.com/hrygo/hotplex/provider"
	"github.com/hrygo/hotplex/types"
)

// sessionWrapper wraps eng.Session to implement chatapps.Session interface
type sessionWrapper struct {
	sess *eng.Session
}

func (w *sessionWrapper) ID() string {
	if w.sess == nil {
		return ""
	}
	return w.sess.ID
}

func (w *sessionWrapper) Status() string {
	if w.sess == nil {
		return ""
	}
	return "active"
}

func (w *sessionWrapper) CreatedAt() time.Time {
	if w.sess == nil {
		return time.Time{}
	}
	return w.sess.CreatedAt
}

// engineWrapper wraps engine.Engine to implement chatapps.Engine interface
type engineWrapper struct {
	eng *engine.Engine
}

func (w *engineWrapper) Execute(ctx context.Context, cfg *types.Config, prompt string, callback event.Callback) error {
	return w.eng.Execute(ctx, cfg, prompt, callback)
}

func (w *engineWrapper) GetSession(sessionID string) (Session, bool) {
	sess, ok := w.eng.GetSession(sessionID)
	if !ok || sess == nil {
		return nil, false
	}
	return &sessionWrapper{sess: sess}, true
}

func (w *engineWrapper) Close() error {
	return w.eng.Close()
}

func (w *engineWrapper) GetSessionStats(sessionID string) *SessionStats {
	stats := w.eng.GetSessionStats(sessionID)
	if stats == nil {
		return nil
	}
	return &SessionStats{
		SessionID:     stats.SessionID,
		Status:        stats.SessionID,
		TotalTokens:   int64(stats.InputTokens + stats.OutputTokens + stats.CacheReadTokens + stats.CacheWriteTokens),
		InputTokens:   int64(stats.InputTokens),
		OutputTokens:  int64(stats.OutputTokens),
		CacheRead:     int64(stats.CacheReadTokens),
		CacheWrite:    int64(stats.CacheWriteTokens),
		TotalCost:     0,
		Duration:      time.Duration(stats.TotalDurationMs) * time.Millisecond,
		ToolCallCount: int(stats.ToolCallCount),
		ErrorCount:    0,
	}
}

func (w *engineWrapper) ValidateConfig(cfg *types.Config) error {
	return w.eng.ValidateConfig(cfg)
}

func (w *engineWrapper) StopSession(sessionID string, reason string) error {
	return w.eng.StopSession(sessionID, reason)
}

func (w *engineWrapper) ResetSessionProvider(sessionID string) {
	w.eng.ResetSessionProvider(sessionID)
}

func (w *engineWrapper) SetDangerAllowPaths(paths []string) {
	w.eng.SetDangerAllowPaths(paths)
}

func (w *engineWrapper) SetDangerBypassEnabled(token string, enabled bool) error {
	return w.eng.SetDangerBypassEnabled(token, enabled)
}

func (w *engineWrapper) SetAllowedTools(tools []string) {
	w.eng.SetAllowedTools(tools)
}

func (w *engineWrapper) SetDisallowedTools(tools []string) {
	w.eng.SetDisallowedTools(tools)
}

func (w *engineWrapper) GetAllowedTools() []string {
	return w.eng.GetAllowedTools()
}

func (w *engineWrapper) GetDisallowedTools() []string {
	return w.eng.GetDisallowedTools()
}

// EngineHolder holds the Engine instance and configuration for ChatApps integration
type EngineHolder struct {
	engine           Engine
	logger           *slog.Logger
	adapters         *AdapterManager
	defaultWorkDir   string
	defaultTaskInstr string
}

// NewEngineHolder creates a new EngineHolder with the given options
func NewEngineHolder(opts EngineHolderOptions) (*EngineHolder, error) {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	if opts.Timeout == 0 {
		opts.Timeout = 5 * time.Minute
	}
	if opts.IdleTimeout == 0 {
		opts.IdleTimeout = 30 * time.Minute
	}

	engineOpts := engine.EngineOptions{
		Timeout:         opts.Timeout,
		IdleTimeout:     opts.IdleTimeout,
		Namespace:       opts.Namespace,
		PermissionMode:  opts.PermissionMode,
		AllowedTools:    opts.AllowedTools,
		DisallowedTools: opts.DisallowedTools,
		Logger:          logger,
	}

	eng, err := engine.NewEngine(engineOpts)
	if err != nil {
		return nil, fmt.Errorf("create engine: %w", err)
	}

	// Wrap engine.Engine to implement chatapps.Engine interface
	wrappedEngine := &engineWrapper{eng: eng}

	return &EngineHolder{
		engine:           wrappedEngine,
		logger:           logger,
		adapters:         opts.Adapters,
		defaultWorkDir:   opts.DefaultWorkDir,
		defaultTaskInstr: opts.DefaultTaskInstr,
	}, nil
}

// EngineHolderOptions configures the EngineHolder
type EngineHolderOptions struct {
	Logger           *slog.Logger
	Adapters         *AdapterManager
	Timeout          time.Duration
	IdleTimeout      time.Duration
	Namespace        string
	PermissionMode   string
	AllowedTools     []string
	DisallowedTools  []string
	DefaultWorkDir   string
	DefaultTaskInstr string
}

// GetEngine returns the underlying Engine instance
func (h *EngineHolder) GetEngine() Engine {
	return h.engine
}

// GetAdapterManager returns the AdapterManager for sending messages
func (h *EngineHolder) GetAdapterManager() *AdapterManager {
	return h.adapters
}

// StreamCallback implements event.Callback to receive Engine events and forward to ChatApp
type StreamCallback struct {
	ctx          context.Context
	sessionID    string
	platform     string
	adapters     *AdapterManager
	logger       *slog.Logger
	mu           sync.Mutex
	isFirst      bool
	thinkingSent bool            // Tracks if thinking/status message was sent
	metadata     map[string]any  // Original message metadata (channel_id, thread_ts, etc.)
	processor    *ProcessorChain // Message processor chain

	// Session lifecycle state - ensures correct event ordering
	sessionStartSent bool // Tracks if session_start event has been sent

	// Status message state for dynamic event type indicator
	// Reuses thinking message infrastructure for in-place updates
	thinkingChannelID string           // Channel ID for status message updates
	thinkingMessageTS string           // Message TS for status message updates
	currentStatus     base.MessageType // Current status type (thinking, tool_use, answer)

	// Stream state for throttled updates
	streamState *StreamState

	// Reaction lifecycle state — tracks the user's trigger message for emoji status
	reactionChannelID string // Channel for reactions (from user msg)
	reactionMessageTS string // User's message TS for reactions
	currentReaction   string // Currently active reaction emoji name

	// Starting message records for 3s delayed deletion (session_start + engine_starting)
	startingMsgRecords []msgRecord

	// Tracking records for 3s delayed deletion on Answer (Zone 0 & 1)
	// Note: For concurrent turn support, new turns should use turnState instead
	cleanupMsgRecords []msgRecord

	// Turn state for concurrent turn support - stores cleanup records per turn
	turnState *eng.TurnState

	// Platform-specific operations (dependency injection for testability and platform agnosticism)
	messageOps MessageOperations
	sessionOps SessionOperations
}

// msgRecord tracks a sent message for later deletion or sliding window management.
type msgRecord struct {
	ChannelID string
	MessageTS string
	ZoneIndex int    // Zone index (0-3) for filtering and sliding window
	EventType string // Event type (tool_use, session_start, etc.) to protection markers
}

// StreamState tracks the state for streaming updates
type StreamState struct {
	ChannelID   string
	MessageTS   string
	LastUpdated time.Time
	mu          sync.Mutex
}

// NewStreamCallback creates a new StreamCallback with injected platform-specific operations
func NewStreamCallback(
	ctx context.Context,
	sessionID, platform string,
	adapters *AdapterManager,
	logger *slog.Logger,
	metadata map[string]any,
	messageOps MessageOperations,
	sessionOps SessionOperations,
	turnState *eng.TurnState,
) *StreamCallback {
	cb := &StreamCallback{
		ctx:        ctx,
		sessionID:  sessionID,
		platform:   platform,
		adapters:   adapters,
		logger:     logger,
		isFirst:    true,
		metadata:   metadata,
		processor:  NewDefaultProcessorChain(ctx, logger),
		messageOps: messageOps,
		sessionOps: sessionOps,
		turnState:  turnState,
	}

	// Extract user message coordinates for reaction lifecycle
	if metadata != nil {
		if ch, ok := metadata["channel_id"].(string); ok {
			cb.reactionChannelID = ch
		}
		if ts, ok := metadata["message_ts"].(string); ok {
			cb.reactionMessageTS = ts
		}
	}

	// Set callback as the sender for aggregated messages
	cb.processor.SetAggregatorSender(cb)

	return cb
}

// SendAggregatedMessage implements AggregatedMessageSender interface
// This is called by the MessageAggregatorProcessor when timer flushes buffered messages
func (c *StreamCallback) SendAggregatedMessage(ctx context.Context, msg *ChatMessage) error {
	c.logger.Info("SendAggregatedMessage called", "session_id", c.sessionID, "content_len", len(msg.Content))

	if c.adapters == nil {
		c.logger.Warn("No adapters in SendAggregatedMessage", "platform", c.platform)
		return nil
	}

	if err := c.adapters.SendMessage(ctx, c.platform, c.sessionID, msg); err != nil {
		return err
	}

	// Track action/thinking zone messages for sliding window and cleanup.
	// Aggregated messages bypass the normal path because the aggregator buffers them.
	c.trackMessage((*base.ChatMessage)(msg))

	return nil
}

// Handle implements event.Callback
func (c *StreamCallback) Handle(eventType string, data any) error {
	// Note: No mutex lock here - individual handlers manage their own locking
	// This prevents deadlock when handlers need to access shared state

	switch provider.ProviderEventType(eventType) {
	case provider.EventTypeThinking:
		return c.handleThinking(data)
	case provider.EventTypeToolUse:
		return c.handleToolUse(data)
	case provider.EventTypeToolResult:
		return c.handleToolResult(data)
	case provider.EventTypeAnswer:
		return c.handleAnswer(data)
	case provider.EventTypeError:
		return c.handleError(data)
	case provider.EventTypePlanMode:
		return c.handlePlanMode(data)
	case provider.EventTypeExitPlanMode:
		return c.handleExitPlanMode(data)
	case provider.EventTypeAskUserQuestion:
		return c.handleAskUserQuestion(data)
	case provider.EventTypeResult:
		return c.handleSessionStats(data)
	case provider.EventTypeCommandProgress:
		return c.handleCommandProgress(data)
	case provider.EventTypeCommandComplete:
		return c.handleCommandComplete(data)
	case provider.EventTypeSystem:
		return c.handleSystem(data)
	case provider.EventTypeUser:
		return c.handleUser(data)
	case provider.EventTypeStepStart:
		return c.handleStepStart(data)
	case provider.EventTypeStepFinish:
		return c.handleStepFinish(data)
	case provider.EventTypeRaw:
		return c.handleRaw(data)
	case provider.EventTypeSessionStart:
		return c.handleSessionStart(data)
	case provider.EventTypeEngineStarting:
		return c.handleEngineStarting(data)
	case provider.EventTypeUserMessageReceived:
		return c.handleUserMessageReceived(data)
	case provider.EventTypePermissionRequest:
		return c.handlePermissionRequest(data)
	default:
		// Check for specific engine/extended events
		if eventType == "danger_block" {
			return c.handleDangerBlock(data)
		}
		if eventType == "session_stats" {
			return c.handleSessionStats(data)
		}
		c.logger.Debug("Ignoring unknown event", "type", eventType)
	}
	return nil
}

func (c *StreamCallback) handleThinking(data any) error {
	// Extract thinking content from EventWithMeta
	var thinkingContent string
	if m, ok := data.(*event.EventWithMeta); ok {
		// Use EventData directly (contains thinking content from CLI)
		thinkingContent = m.EventData
	}

	// Log what we received
	c.logger.Debug("handleThinking received",
		"data_type", fmt.Sprintf("%T", data),
		"event_data", thinkingContent,
		"is_first", c.isFirst,
		"thinking_sent", c.thinkingSent,
		"thinking_channel_id", c.thinkingChannelID,
		"thinking_message_ts", c.thinkingMessageTS)

	// Skip empty thinking content if not the first event
	if thinkingContent == "" && !c.isFirst {
		return nil
	}

	// Reaction lifecycle: 📥 → 🧠
	c.setReaction("brain")

	c.mu.Lock()
	sessionStartSent := c.sessionStartSent
	c.mu.Unlock()

	if !sessionStartSent {
		c.logger.Warn("thinking event received before session_start - proceeding anyway",
			"session_id", c.sessionID,
			"thinking_content", thinkingContent)
	}

	// Use updateStatusMessage for dynamic status indicator
	// This reuses thinking message infrastructure for in-place updates
	return c.updateStatusMessage(base.MessageTypeThinking, thinkingContent)
}

// sendMessageAndGetTS sends a message and populates message_ts in metadata
func (c *StreamCallback) sendMessageAndGetTS(msg *ChatMessage) error {
	if c.adapters == nil {
		c.logger.Debug("No adapters, skipping message send", "platform", c.platform)
		return nil
	}

	// Process message through processor chain
	processedMsg, err := c.processor.Process(c.ctx, msg)
	if err != nil {
		c.logger.Error("Message processing failed",
			"platform", c.platform,
			"session_id", c.sessionID,
			"error", err)
		processedMsg = msg
	}

	if processedMsg == nil {
		c.logger.Debug("Message dropped by processor",
			"platform", c.platform,
			"session_id", c.sessionID)
		return nil
	}

	// Send the message - adapter will populate message_ts in metadata
	if err := c.adapters.SendMessage(c.ctx, c.platform, c.sessionID, processedMsg); err != nil {
		return err
	}

	// Automatic tracking via zone metadata (Thinking or Action zones)
	c.trackMessage(processedMsg)

	// Copy message_ts back to original msg metadata if present in processedMsg
	if processedMsg.Metadata != nil {
		if ts, ok := processedMsg.Metadata["message_ts"].(string); ok && ts != "" {
			if msg.Metadata == nil {
				msg.Metadata = make(map[string]any)
			}
			msg.Metadata["message_ts"] = ts
		}
		if channelID, ok := processedMsg.Metadata["channel_id"].(string); ok && channelID != "" {
			if msg.Metadata == nil {
				msg.Metadata = make(map[string]any)
			}
			msg.Metadata["channel_id"] = channelID
		}
	}

	return nil
}

// setReaction sets a reaction on the user's trigger message.
// Removes previous reaction before adding new one for clean status transitions.
func (c *StreamCallback) setReaction(emoji string) {
	if c.reactionChannelID == "" || c.reactionMessageTS == "" {
		return
	}

	// Use injected message operations interface (no type assertion needed)
	if c.messageOps == nil {
		c.logger.Debug("Message operations not supported on this platform", "platform", c.platform)
		return
	}

	c.mu.Lock()
	prevReaction := c.currentReaction
	if prevReaction == emoji {
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()

	// Remove previous reaction (ignore errors — may not exist)
	if prevReaction != "" {
		_ = c.messageOps.RemoveReaction(c.ctx, base.Reaction{
			Name:      prevReaction,
			Channel:   c.reactionChannelID,
			Timestamp: c.reactionMessageTS,
		})
	}

	// Add new reaction
	if err := c.messageOps.AddReaction(c.ctx, base.Reaction{
		Name:      emoji,
		Channel:   c.reactionChannelID,
		Timestamp: c.reactionMessageTS,
	}); err == nil {
		c.mu.Lock()
		c.currentReaction = emoji
		c.mu.Unlock()
	} else {
		c.logger.Warn("Failed to set reaction", "emoji", emoji, "error", err)
	}
}

// scheduleDeleteStartingMessage schedules a 3-second delayed deletion
// for all startup-phase messages (session_start + engine_starting).
func (c *StreamCallback) scheduleDeleteStartingMessage() {
	c.mu.Lock()
	if len(c.startingMsgRecords) == 0 {
		c.mu.Unlock()
		return
	}
	records := c.startingMsgRecords
	c.startingMsgRecords = nil // Clear immediately to prevent double-delete
	c.mu.Unlock()

	time.AfterFunc(3*time.Second, func() {
		// Use injected message operations interface (no type assertion needed)
		if c.messageOps == nil {
			c.logger.Debug("Message operations not supported", "platform", c.platform)
			return
		}
		for _, rec := range records {
			if err := c.messageOps.DeleteMessage(context.Background(), rec.ChannelID, rec.MessageTS); err != nil {
				c.logger.Debug("Failed to delete starting message", "ts", rec.MessageTS, "error", err)
			}
		}
	})
}

const maxSlidingMessages = 5

// trackMessage records a message for sliding window management and final cleanup.
func (c *StreamCallback) trackMessage(msg *base.ChatMessage) {
	if msg == nil || msg.Metadata == nil {
		return
	}

	zone, hasZone := msg.Metadata["zone_index"].(int)
	if !hasZone || (zone != ZoneInitialization && zone != ZoneThinking && zone != ZoneAction) {
		return
	}

	ts, _ := msg.Metadata["message_ts"].(string)
	ch, _ := msg.Metadata["channel_id"].(string)
	eventType, _ := msg.Metadata["event_type"].(string)
	if ts == "" || ch == "" {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if this TS is already tracked to handle in-place updates gracefully
	if c.turnState != nil && c.turnState.HasMessageTS(ts) {
		return
	}

	// Record the message to turn state (for concurrent turn support)
	if c.turnState != nil {
		c.turnState.AddCleanupMsg(eng.CleanupMsgRecord{
			ChannelID: ch,
			MessageTS: ts,
			ZoneIndex: zone,
			EventType: eventType,
		})
	} else {
		// Also record to legacy cleanupMsgRecords for backward compatibility
		// TODO: Remove this after full migration to turnState
		c.cleanupMsgRecords = append(c.cleanupMsgRecords, msgRecord{
			ChannelID: ch,
			MessageTS: ts,
			ZoneIndex: zone,
			EventType: eventType,
		})
	}

	// Enforce sliding window independently for Thinking and Action zones
	c.enforceSlidingWindow(zone)
}

// enforceSlidingWindow deletes oldest messages when the limit is exceeded.
// Uses turnState for concurrent turn support, falls back to legacy cleanupMsgRecords
// NOTE: Caller must hold c.mu lock before calling this method
func (c *StreamCallback) enforceSlidingWindow(zone int) {
	// Use turnState if available (concurrent turn support)
	if c.turnState != nil {
		c.enforceSlidingWindowWithTurnState(zone)
		return
	}

	// Legacy fallback
	var zoneRecords []msgRecord
	var otherRecords []msgRecord

	// Split records into current zone and others
	for _, rec := range c.cleanupMsgRecords {
		if rec.ZoneIndex == zone {
			zoneRecords = append(zoneRecords, rec)
		} else {
			otherRecords = append(otherRecords, rec)
		}
	}

	if len(zoneRecords) <= maxSlidingMessages {
		return
	}

	// Find the oldest evictable record (skip startup messages in Zone 1)
	var toEvict msgRecord
	var remainingInZone []msgRecord
	found := false

	for _, rec := range zoneRecords {
		if !found && (zone == ZoneAction || zone == ZoneInitialization) {
			// Protection: never evict startup markers from sliding window
			if rec.EventType == string(provider.EventTypeSessionStart) ||
				rec.EventType == string(provider.EventTypeEngineStarting) {
				remainingInZone = append(remainingInZone, rec)
				continue
			}
		}

		if !found {
			toEvict = rec
			found = true
			continue
		}
		remainingInZone = append(remainingInZone, rec)
	}

	if !found {
		return
	}

	// Rebuild the final records slice
	c.cleanupMsgRecords = append(remainingInZone, otherRecords...)

	// Delete evicted message in background
	go func() {
		if c.messageOps == nil {
			return
		}
		_ = c.messageOps.DeleteMessage(context.Background(), toEvict.ChannelID, toEvict.MessageTS)
	}()
}

// enforceSlidingWindowWithTurnState enforces sliding window using turnState
func (c *StreamCallback) enforceSlidingWindowWithTurnState(zone int) {
	c.turnState.EnforceSlidingWindow(zone, func(rec eng.CleanupMsgRecord) {
		if c.messageOps == nil {
			return
		}
		go func() {
			_ = c.messageOps.DeleteMessage(context.Background(), rec.ChannelID, rec.MessageTS)
		}()
	})
}

// scheduleDeleteActionMessages schedules 3-second delayed deletion
// of all tracked Thinking and Action Zone messages.
// Uses turnState for concurrent turn support, falls back to legacy cleanupMsgRecords
func (c *StreamCallback) scheduleDeleteActionMessages() {
	// Use turnState if available (concurrent turn support)
	if c.turnState != nil {
		c.scheduleDeleteActionMessagesWithTurnState()
		return
	}

	// Legacy fallback
	c.mu.Lock()
	if len(c.cleanupMsgRecords) == 0 {
		c.mu.Unlock()
		return
	}
	records := c.cleanupMsgRecords
	c.cleanupMsgRecords = nil // Clear immediately
	c.mu.Unlock()

	time.AfterFunc(3*time.Second, func() {
		if c.messageOps == nil {
			c.logger.Debug("Message operations not supported", "platform", c.platform)
			return
		}
		for _, rec := range records {
			if err := c.messageOps.DeleteMessage(context.Background(), rec.ChannelID, rec.MessageTS); err != nil {
				c.logger.Debug("Failed to delete tracked message", "ts", rec.MessageTS, "error", err)
			}
		}
	})
}

// scheduleDeleteActionMessagesWithTurnState schedules deletion using turnState
func (c *StreamCallback) scheduleDeleteActionMessagesWithTurnState() {
	if c.turnState.Len() == 0 {
		return
	}
	records := c.turnState.GetAllAndClear()

	time.AfterFunc(3*time.Second, func() {
		if c.messageOps == nil {
			c.logger.Debug("Message operations not supported", "platform", c.platform)
			return
		}
		for _, rec := range records {
			if err := c.messageOps.DeleteMessage(context.Background(), rec.ChannelID, rec.MessageTS); err != nil {
				c.logger.Debug("Failed to delete tracked message", "ts", rec.MessageTS, "error", err)
			}
		}
	})
}

// updateStatusMessage updates the status indicator message in-place
// It sends a base.ChatMessage with the appropriate MessageType
// The Adapter's MessageBuilder will handle conversion to platform-specific blocks
// NOTE: Caller must hold c.mu lock before calling this method
func (c *StreamCallback) updateStatusMessage(statusType base.MessageType, displayText string) error {
	c.mu.Lock()
	// Skip if status hasn't changed (avoid redundant updates)
	if c.currentStatus == statusType && statusType != base.MessageTypeThinking {
		c.mu.Unlock()
		return nil
	}

	c.logger.Debug("Updating status message",
		"status_type", statusType,
		"display_text", displayText,
		"is_first", c.isFirst,
		"thinking_sent", c.thinkingSent)

	// Create message with platform-agnostic MessageType
	msg := &base.ChatMessage{
		Type:     statusType,
		Content:  displayText,
		Metadata: c.copyMessageMetadata(),
	}
	msg.Metadata["stream"] = true
	msg.Metadata["event_type"] = string(statusType)

	isFirst := c.isFirst
	isUpdate := !isFirst && c.thinkingSent && c.thinkingMessageTS != "" && c.thinkingChannelID != ""

	if isFirst {
		c.isFirst = false // Optimistic update
		c.currentStatus = statusType
	} else {
		c.currentStatus = statusType
	}

	msgTS := c.thinkingMessageTS
	channelID := c.thinkingChannelID
	c.mu.Unlock()

	if isFirst {
		// First status event - create new message
		if err := c.sendMessageAndGetTS(convertToChatMessage(msg)); err != nil {
			// Rollback state on send failure
			c.mu.Lock()
			c.isFirst = true
			c.currentStatus = ""
			c.mu.Unlock()
			return err
		}

		// Extract ts from metadata after successful send
		if ts, ok := msg.Metadata["message_ts"].(string); ok && ts != "" {
			c.mu.Lock()
			c.thinkingMessageTS = ts
			if chID, ok := msg.Metadata["channel_id"].(string); ok {
				c.thinkingChannelID = chID
			}
			c.thinkingSent = true
			c.mu.Unlock()
			c.logger.Debug("Captured status message ts for updates", "ts", ts)
		} else {
			c.mu.Lock()
			c.thinkingSent = true
			c.mu.Unlock()
			c.logger.Warn("Failed to capture status message ts, updates may not work")
		}
		return nil
	} else if isUpdate {
		// Subsequent status event - update the existing message
		msg.Metadata["message_ts"] = msgTS
		msg.Metadata["channel_id"] = channelID
		return c.sendMessageAndGetTS(convertToChatMessage(msg))
	}

	// Fallback: send as new message
	return c.sendMessageAndGetTS(convertToChatMessage(msg))
}

// convertToChatMessage converts base.ChatMessage to ChatMessage (local type)
func convertToChatMessage(msg *base.ChatMessage) *ChatMessage {
	return &ChatMessage{
		Type:        msg.Type,
		Platform:    msg.Platform,
		SessionID:   msg.SessionID,
		UserID:      msg.UserID,
		Content:     msg.Content,
		MessageID:   msg.MessageID,
		Timestamp:   msg.Timestamp,
		Metadata:    msg.Metadata,
		RichContent: msg.RichContent,
	}
}

func (c *StreamCallback) handleToolUse(data any) error {
	c.logger.Debug("[TOOL] handleToolUse called", "data_type", fmt.Sprintf("%T", data))

	toolName := string(provider.EventTypeToolUse)
	input := ""
	truncated := false
	var inputSummary string

	if m, ok := data.(*event.EventWithMeta); ok {
		var metaToolName, metaInputSummary string
		if m.Meta != nil {
			metaToolName = m.Meta.ToolName
			metaInputSummary = m.Meta.InputSummary
		}
		c.logger.Debug("[TOOL] handleToolUse EventWithMeta",
			"event_data", m.EventData,
			"meta_tool_name", metaToolName,
			"meta_input_summary", metaInputSummary)
		if m.Meta != nil && m.Meta.ToolName != "" {
			toolName = m.Meta.ToolName
			inputSummary = m.Meta.InputSummary
		}
		if m.EventData != "" {
			input = m.EventData
			if len(input) > 100 {
				truncated = true
			}
		}
	}

	// Update status indicator to show current tool being used
	// This updates the thinking message in-place with "Tool: Read" etc.
	if err := c.updateStatusMessage(base.MessageTypeToolUse, toolName); err != nil {
		c.logger.Warn("Failed to update status for tool_use", "error", err)
	}

	c.logger.Debug("[TOOL] handleToolUse sending", "tool_name", toolName, "input_len", len(input))

	// Send tool use message with platform-agnostic MessageType
	// The Adapter's MessageBuilder will convert to platform-specific blocks
	msg := &base.ChatMessage{
		Type:    base.MessageTypeToolUse,
		Content: toolName,
		Metadata: map[string]any{
			"input":         input,
			"input_summary": inputSummary,
			"truncated":     truncated,
			"event_type":    string(provider.EventTypeToolUse),
			"stream":        true, // Mark as stream to allow aggregation window
		},
	}
	msg.Metadata = c.mergeMetadata(msg.Metadata)
	if err := c.sendMessageAndGetTS(convertToChatMessage(msg)); err != nil {
		return err
	}
	return nil
}

func (c *StreamCallback) handleToolResult(data any) error {
	c.logger.Debug("[TOOL] handleToolResult called", "data_type", fmt.Sprintf("%T", data))

	success := true
	var durationMs int64
	var toolName string
	var filePath string
	output := ""

	var contentLength int64
	if m, ok := data.(*event.EventWithMeta); ok {
		c.logger.Debug("[TOOL] handleToolResult EventWithMeta",
			"event_data_len", len(m.EventData),
			"meta_tool_name", m.Meta.ToolName,
			"meta_duration_ms", m.Meta.DurationMs,
			"meta_status", m.Meta.Status,
			"meta_file_path", m.Meta.FilePath)
		if m.Meta != nil {
			if m.Meta.Status == "error" {
				success = false
			}
			if m.Meta.ErrorMsg != "" {
				output = m.Meta.ErrorMsg
			}
			durationMs = m.Meta.DurationMs
			toolName = m.Meta.ToolName
			filePath = m.Meta.FilePath
		}

		// Calculate real content length even if we use a placeholder
		contentLength = int64(len(m.EventData))

		// Only use EventData as output if we don't have an error message
		// This prevents large file contents from being used as output
		if output == "" && m.EventData != "" {
			// For tool_result, we only need a brief summary, not the full content
			// The content length is shown in the message, but not the actual content
			output = "Output generated"
		}
	}

	// Skip empty tool_result events (no output, no error, no length)
	if output == "" && toolName == "" && contentLength == 0 {
		c.logger.Debug("[TOOL] handleToolResult skipped: empty output and tool name")
		return nil
	}

	c.logger.Debug("[TOOL] handleToolResult sending",
		"tool_name", toolName,
		"success", success,
		"duration_ms", durationMs,
		"output_len", len(output))

	// Send tool result message with platform-agnostic MessageType
	msg := &base.ChatMessage{
		Type:    base.MessageTypeToolResult,
		Content: output,
		Metadata: map[string]any{
			"success":        success,
			"duration_ms":    durationMs,
			"tool_name":      toolName,
			"file_path":      filePath,
			"content_length": contentLength,
			"event_type":     string(provider.EventTypeToolResult),
			"stream":         true, // Consistency with other flow events
		},
	}
	msg.Metadata = c.mergeMetadata(msg.Metadata)
	if err := c.sendMessageAndGetTS(convertToChatMessage(msg)); err != nil {
		return err
	}
	return nil
}

func (c *StreamCallback) handleAnswer(data any) error {
	c.mu.Lock()
	// Schedule 3-second delayed deletion of thinking message for smooth UX transition
	if c.thinkingSent && c.thinkingMessageTS != "" && c.thinkingChannelID != "" {
		c.logger.Debug("Scheduling delayed thinking message deletion for answer",
			"ts", c.thinkingMessageTS,
			"channel", c.thinkingChannelID)
		channelID, msgTS := c.thinkingChannelID, c.thinkingMessageTS
		c.thinkingSent = false
		c.thinkingMessageTS = ""
		c.thinkingChannelID = ""
		c.mu.Unlock()

		time.AfterFunc(3*time.Second, func() {
			if c.messageOps == nil {
				return
			}
			// Note: Using context.Background() is correct here - the original c.ctx is likely cancelled
			// This is a delayed cleanup operation that should complete regardless of session lifecycle
			if err := c.messageOps.DeleteMessage(context.Background(), channelID, msgTS); err != nil {
				c.logger.Debug("Failed to delete thinking message (delayed)", "error", err)
			}
		})
	} else if c.thinkingSent {
		c.thinkingSent = false
		c.logger.Debug("Clearing thinking state for answer")
		c.mu.Unlock()
	} else {
		c.mu.Unlock()
	}

	// Schedule deletion of all tracked Thinking/Action messages (3s delay)
	c.scheduleDeleteActionMessages()

	// Clear session start message records (3s delay)
	c.scheduleDeleteStartingMessage()

	// Capture answer content
	var content string
	var metadata map[string]any
	if m, ok := data.(*event.EventWithMeta); ok {
		content = m.EventData
		if m.Meta != nil {
			metadata = map[string]any{
				"duration_ms": m.Meta.DurationMs,
				"status":      m.Meta.Status,
			}
		}
	} else if s, ok := data.(string); ok {
		content = s
	}

	msg := &base.ChatMessage{
		Type:    base.MessageTypeAnswer,
		Content: content,
		Metadata: map[string]any{
			"event_type": string(provider.EventTypeAnswer),
		},
	}
	for k, v := range metadata {
		msg.Metadata[k] = v
	}
	msg.Metadata = c.mergeMetadata(msg.Metadata)

	// Initialize stream state on first answer for throttled updates
	if c.streamState == nil {
		channelID := ""
		if c.metadata != nil {
			if ch, ok := c.metadata["channel_id"].(string); ok {
				channelID = ch
			}
		}
		if channelID != "" {
			c.streamState = &StreamState{
				ChannelID: channelID,
				// MessageTS will be set after first send
			}
		}
	}

	// Use throttled streaming update if we have a message to update
	if c.streamState != nil {
		err := c.streamState.updateThrottled(c.ctx, c.adapters, c.platform, c.sessionID, msg)
		if err == nil {
			// Clear processor session state on successful answer
			c.processor.ResetSession(c.platform, c.sessionID)
		}
		return err
	}

	err := c.sendMessageAndGetTS(convertToChatMessage(msg))
	if err == nil {
		c.processor.ResetSession(c.platform, c.sessionID)
	}
	return err
}

func (c *StreamCallback) handleError(data any) error {
	// Reaction lifecycle: set ❌ on error
	c.setReaction("x")

	// Clear thinking state on first non-thinking event
	if c.thinkingSent {
		c.thinkingSent = false
		c.logger.Debug("Clearing thinking state for error")
	}

	var errMsg string
	switch v := data.(type) {
	case string:
		errMsg = v
	case error:
		errMsg = v.Error()
	case *event.EventWithMeta:
		errMsg = v.EventData
		if errMsg == "" && v.Meta != nil {
			errMsg = v.Meta.ErrorMsg
		}
	default:
		errMsg = fmt.Sprintf("%v", data)
	}

	// Send error message with platform-agnostic MessageType
	msg := &base.ChatMessage{
		Type:    base.MessageTypeError,
		Content: errMsg,
		Metadata: map[string]any{
			"event_type": string(provider.EventTypeError),
		},
	}
	msg.Metadata = c.mergeMetadata(msg.Metadata)
	if err := c.sendMessageAndGetTS(convertToChatMessage(msg)); err != nil {
		return err
	}

	// Clear processor session state on error
	c.processor.ResetSession(c.platform, c.sessionID)

	return nil
}

func (c *StreamCallback) handleDangerBlock(data any) error {
	var reason string
	var originalMsg *base.ChatMessage

	// Extract reason and original message from data
	switch v := data.(type) {
	case string:
		reason = v
	case *base.ChatMessage:
		originalMsg = v
		reason = "security_block"
	default:
		reason = "security_block"
	}

	// Store the original message for later recovery (Phase 2)
	if originalMsg != nil && c.adapters != nil {
		// Find the EngineMessageHandler to access pendingStore
		// Note: This requires the handler to be accessible from adapters
		c.logger.Debug("Danger block: storing original message for recovery",
			"session_id", c.sessionID,
			"channel_id", c.reactionChannelID,
			"message_ts", c.reactionMessageTS)
	}

	// Send danger block message with platform-agnostic MessageType
	msg := &base.ChatMessage{
		Type:    base.MessageTypeDangerBlock,
		Content: reason,
		Metadata: map[string]any{
			"event_type": "security_block",
			"session_id": c.sessionID,
		},
	}
	msg.Metadata = c.mergeMetadata(msg.Metadata)
	return c.sendMessageAndGetTS(convertToChatMessage(msg))
}

// handleSessionStats handles session statistics events
// Implements EventTypeResult (Turn Complete)
func (c *StreamCallback) handleSessionStats(data any) error {
	// Reaction lifecycle: set ✅ on turn complete
	c.setReaction("white_check_mark")

	stats, ok := data.(*event.SessionStatsData)
	if !ok {
		c.logger.Debug("session_stats: invalid data type", "type", fmt.Sprintf("%T", data))
		return nil
	}

	// Flush any pending stream state before sending session stats
	// This ensures the last answer message is sent before the turn completes
	if c.streamState != nil {
		c.streamState.Flush()
		c.streamState = nil
	}

	// Send stats message with platform-agnostic MessageType
	msg := &base.ChatMessage{
		Type:    base.MessageTypeSessionStats,
		Content: "",
		Metadata: map[string]any{
			"event_type":           "session_stats",
			"session_id":           stats.SessionID,
			"total_duration_ms":    stats.TotalDurationMs,
			"thinking_duration_ms": stats.ThinkingDurationMs,
			"tool_duration_ms":     stats.ToolDurationMs,
			"input_tokens":         int64(stats.InputTokens),
			"output_tokens":        int64(stats.OutputTokens),
			"total_tokens":         stats.TotalTokens,
			"tool_call_count":      int64(stats.ToolCallCount),
			"tools_used":           stats.ToolsUsed,
			"files_modified":       int64(stats.FilesModified),
			"file_paths":           stats.FilePaths,
		},
	}
	msg.Metadata = c.mergeMetadata(msg.Metadata)
	if err := c.sendMessageAndGetTS(convertToChatMessage(msg)); err != nil {
		return err
	}

	// Final cleanup: ensure all processor state is reset
	c.processor.ResetSession(c.platform, c.sessionID)

	return nil
}

// handleCommandProgress handles command progress events
// Implements EventTypeCommandProgress per spec
func (c *StreamCallback) handleCommandProgress(data any) error {
	var title string
	var metadata map[string]any

	if m, ok := data.(*event.EventWithMeta); ok {
		title = m.EventData
		if m.Meta != nil {
			metadata = map[string]any{
				"duration_ms":    m.Meta.DurationMs,
				"total_steps":    int(m.Meta.TotalSteps),
				"current_step":   int(m.Meta.CurrentStep),
				"progress":       int(m.Meta.Progress),
				"tool_name":      m.Meta.ToolName,
				"status":         m.Meta.Status,
				"input_summary":  m.Meta.InputSummary,
				"output_summary": m.Meta.OutputSummary,
			}
		}
	} else if s, ok := data.(string); ok {
		title = s
	} else {
		title = "Processing..."
	}

	msg := &base.ChatMessage{
		Type:    base.MessageTypeCommandProgress,
		Content: title,
		Metadata: map[string]any{
			"event_type": string(provider.EventTypeCommandProgress),
		},
	}
	for k, v := range metadata {
		msg.Metadata[k] = v
	}
	msg.Metadata = c.mergeMetadata(msg.Metadata)
	return c.sendMessageAndGetTS(convertToChatMessage(msg))
}

// handleCommandComplete handles command completion events
// Implements EventTypeCommandComplete per spec
func (c *StreamCallback) handleCommandComplete(data any) error {
	var title string
	var metadata map[string]any

	if m, ok := data.(*event.EventWithMeta); ok {
		title = m.EventData
		if m.Meta != nil {
			metadata = map[string]any{
				"duration_ms":       m.Meta.DurationMs,
				"total_duration_ms": m.Meta.TotalDurationMs,
				"completed_steps":   int(m.Meta.CurrentStep),
				"total_steps":       int(m.Meta.TotalSteps),
				"tool_name":         m.Meta.ToolName,
				"status":            m.Meta.Status,
				"input_tokens":      int(m.Meta.InputTokens),
				"output_tokens":     int(m.Meta.OutputTokens),
			}
		}
	} else if s, ok := data.(string); ok {
		title = s
	} else {
		title = "Command completed"
	}

	msg := &base.ChatMessage{
		Type:    base.MessageTypeCommandComplete,
		Content: title,
		Metadata: map[string]any{
			"event_type": string(provider.EventTypeCommandComplete),
		},
	}
	for k, v := range metadata {
		msg.Metadata[k] = v
	}
	msg.Metadata = c.mergeMetadata(msg.Metadata)
	return c.sendMessageAndGetTS(convertToChatMessage(msg))
}

// handleSystem handles system-level messages
// Implements EventTypeSystem per spec - uses context block for low visual weight
func (c *StreamCallback) handleSystem(data any) error {
	var content string

	if m, ok := data.(*event.EventWithMeta); ok {
		content = m.EventData
	} else if s, ok := data.(string); ok {
		content = s
	} else {
		return nil
	}

	msg := &base.ChatMessage{
		Type:    base.MessageTypeSystem,
		Content: content,
		Metadata: map[string]any{
			"event_type": string(provider.EventTypeSystem),
		},
	}
	msg.Metadata = c.mergeMetadata(msg.Metadata)
	return c.sendMessageAndGetTS(convertToChatMessage(msg))
}

// handleUser handles user message reflection
// Implements EventTypeUser per spec
func (c *StreamCallback) handleUser(data any) error {
	var content string

	if m, ok := data.(*event.EventWithMeta); ok {
		content = m.EventData
	} else if s, ok := data.(string); ok {
		content = s
	} else {
		return nil
	}

	msg := &base.ChatMessage{
		Type:    base.MessageTypeUser,
		Content: content,
		Metadata: map[string]any{
			"event_type": string(provider.EventTypeUser),
		},
	}
	msg.Metadata = c.mergeMetadata(msg.Metadata)
	return c.sendMessageAndGetTS(convertToChatMessage(msg))
}

// handleStepStart handles step start events (OpenCode specific)
// Implements EventTypeStepStart per spec
func (c *StreamCallback) handleStepStart(data any) error {
	var content string
	var metadata map[string]any

	if m, ok := data.(*event.EventWithMeta); ok {
		content = m.EventData
		if m.Meta != nil {
			metadata = map[string]any{
				"step":          int(m.Meta.CurrentStep),
				"total":         int(m.Meta.TotalSteps),
				"duration_ms":   m.Meta.DurationMs,
				"tool_name":     m.Meta.ToolName,
				"input_summary": m.Meta.InputSummary,
			}
		}
	} else if s, ok := data.(string); ok {
		content = s
	} else {
		content = "Starting step..."
	}

	msg := &base.ChatMessage{
		Type:    base.MessageTypeStepStart,
		Content: content,
		Metadata: map[string]any{
			"event_type": string(provider.EventTypeStepStart),
		},
	}
	for k, v := range metadata {
		msg.Metadata[k] = v
	}
	msg.Metadata = c.mergeMetadata(msg.Metadata)
	return c.sendMessageAndGetTS(convertToChatMessage(msg))
}

// handleStepFinish handles step finish events (OpenCode specific)
// Implements EventTypeStepFinish per spec
func (c *StreamCallback) handleStepFinish(data any) error {
	var content string
	var metadata map[string]any

	if m, ok := data.(*event.EventWithMeta); ok {
		content = m.EventData
		if m.Meta != nil {
			metadata = map[string]any{
				"step":           int(m.Meta.CurrentStep),
				"total":          int(m.Meta.TotalSteps),
				"duration_ms":    m.Meta.DurationMs,
				"tool_name":      m.Meta.ToolName,
				"status":         m.Meta.Status,
				"output_summary": m.Meta.OutputSummary,
			}
		}
	} else if s, ok := data.(string); ok {
		content = s
	} else {
		content = "Step completed"
	}

	msg := &base.ChatMessage{
		Type:    base.MessageTypeStepFinish,
		Content: content,
		Metadata: map[string]any{
			"event_type": string(provider.EventTypeStepFinish),
		},
	}
	for k, v := range metadata {
		msg.Metadata[k] = v
	}
	msg.Metadata = c.mergeMetadata(msg.Metadata)
	return c.sendMessageAndGetTS(convertToChatMessage(msg))
}

// handleRaw handles raw/unparsed output
// Implements EventTypeRaw per spec - shows only type and length
func (c *StreamCallback) handleRaw(data any) error {
	var content string

	if m, ok := data.(*event.EventWithMeta); ok {
		content = m.EventData
	} else if s, ok := data.(string); ok {
		content = s
	} else {
		return nil
	}

	msg := &base.ChatMessage{
		Type:    base.MessageTypeRaw,
		Content: content,
		Metadata: map[string]any{
			"event_type": string(provider.EventTypeRaw),
		},
	}
	msg.Metadata = c.mergeMetadata(msg.Metadata)
	return c.sendMessageAndGetTS(convertToChatMessage(msg))
}

// copyMessageMetadata copies important metadata from original message
// NOTE: message_ts is intentionally NOT copied because it refers to the user's message,
// not the bot's message. Copying it causes Slack API errors (cant_update_message)
// when the system tries to update a message that doesn't belong to the bot.
func (c *StreamCallback) copyMessageMetadata() map[string]any {
	metadata := make(map[string]any)
	if c.metadata != nil {
		if channelID, ok := c.metadata["channel_id"]; ok {
			metadata["channel_id"] = channelID
		}
		if channelType, ok := c.metadata["channel_type"]; ok {
			metadata["channel_type"] = channelType
		}
		if threadTS, ok := c.metadata["thread_ts"]; ok {
			metadata["thread_ts"] = threadTS
		}
		if userID, ok := c.metadata["user_id"]; ok {
			metadata["user_id"] = userID
		}
		if messageID, ok := c.metadata["message_id"]; ok {
			metadata["message_id"] = messageID
		}
		// Do NOT copy message_ts - it refers to the user's message, not the bot's message.
		// The bot's message_ts will be set after successfully posting a new message.
	}
	return metadata
}

// mergeMetadata merges the callback's stored metadata with the provided metadata
// NOTE: message_ts is intentionally NOT copied because it refers to the user's message,
// not the bot's message. Copying it causes Slack API errors (cant_update_message).
func (c *StreamCallback) mergeMetadata(metadata map[string]any) map[string]any {
	if metadata == nil {
		metadata = make(map[string]any)
	}
	// Copy over important metadata from stored metadata
	if c.metadata != nil {
		if channelID, ok := c.metadata["channel_id"]; ok {
			metadata["channel_id"] = channelID
		}
		if channelType, ok := c.metadata["channel_type"]; ok {
			metadata["channel_type"] = channelType
		}
		if threadTS, ok := c.metadata["thread_ts"]; ok {
			metadata["thread_ts"] = threadTS
		}
		if userID, ok := c.metadata["user_id"]; ok {
			metadata["user_id"] = userID
		}
		if messageID, ok := c.metadata["message_id"]; ok {
			metadata["message_id"] = messageID
		}
		// Do NOT copy message_ts - it refers to the user's message, not the bot's message.
	}
	return metadata
}

// EngineMessageHandler implements MessageHandler and integrates with Engine
type EngineMessageHandler struct {
	engine         *engine.Engine
	adapters       *AdapterManager
	workDirFn      func(sessionID string) string
	taskInstrFn    func(sessionID string) string
	systemPromptFn func(sessionID, platform string) string
	configLoader   *ConfigLoader
	logger         *slog.Logger
	pendingStore   *base.PendingMessageStore // Store for pending danger block approvals
}

// NewEngineMessageHandler creates a new EngineMessageHandler
func NewEngineMessageHandler(engine *engine.Engine, adapters *AdapterManager, opts ...EngineMessageHandlerOption) *EngineMessageHandler {
	h := &EngineMessageHandler{
		engine:       engine,
		adapters:     adapters,
		logger:       slog.Default(),
		pendingStore: base.NewPendingMessageStore(5 * time.Minute),
	}

	for _, opt := range opts {
		opt(h)
	}

	return h
}

// EngineMessageHandlerOption configures the EngineMessageHandler
type EngineMessageHandlerOption func(*EngineMessageHandler)

func WithWorkDirFn(fn func(sessionID string) string) EngineMessageHandlerOption {
	return func(h *EngineMessageHandler) {
		h.workDirFn = fn
	}
}

func WithTaskInstrFn(fn func(sessionID string) string) EngineMessageHandlerOption {
	return func(h *EngineMessageHandler) {
		h.taskInstrFn = fn
	}
}

func WithLogger(logger *slog.Logger) EngineMessageHandlerOption {
	return func(h *EngineMessageHandler) {
		h.logger = logger
	}
}

func WithConfigLoader(loader *ConfigLoader) EngineMessageHandlerOption {
	return func(h *EngineMessageHandler) {
		h.configLoader = loader
	}
}

// Handle implements EngineMessageHandler
func (h *EngineMessageHandler) Handle(ctx context.Context, msg *ChatMessage) error {
	// Determine work directory
	workDir := ""
	if h.workDirFn != nil {
		workDir = h.workDirFn(msg.SessionID)
	}
	if workDir == "" {
		workDir = "/tmp/hotplex-chatapps"
	}

	// Determine task instructions
	taskInstr := ""
	if h.taskInstrFn != nil {
		taskInstr = h.taskInstrFn(msg.SessionID)
	}
	if taskInstr == "" && h.configLoader != nil {
		taskInstr = h.configLoader.GetTaskInstructions(msg.Platform)
	}
	if taskInstr == "" {
		taskInstr = "You are a helpful AI assistant. Execute user commands and provide clear feedback."
	}

	// Determine system prompt
	systemPrompt := ""
	if h.systemPromptFn != nil {
		systemPrompt = h.systemPromptFn(msg.SessionID, msg.Platform)
	}
	if systemPrompt == "" && h.configLoader != nil {
		systemPrompt = h.configLoader.GetSystemPrompt(msg.Platform)
	}

	// Combine task instructions with system prompt
	fullInstructions := taskInstr
	if systemPrompt != "" {
		fullInstructions = systemPrompt + "\n\n" + taskInstr
	}

	// Build config
	cfg := &types.Config{
		WorkDir:          workDir,
		SessionID:        msg.SessionID,
		TaskInstructions: fullInstructions,
	}

	// Get platform-specific operations from AdapterManager
	messageOps := h.adapters.GetMessageOperations(msg.Platform)
	sessionOps := h.adapters.GetSessionOperations(msg.Platform)

	// Get or create turn state for concurrent turn support
	// Each turn maintains independent cleanup records to prevent message leakage
	sess, _ := h.engine.GetSession(msg.SessionID)
	turnState := sess.GetOrCreateTurn(msg.SessionID + ":" + time.Now().Format("150405.000"))

	// Create stream callback with injected dependencies
	callback := NewStreamCallback(ctx, msg.SessionID, msg.Platform, h.adapters, h.logger, msg.Metadata, messageOps, sessionOps, turnState)
	wrappedCallback := func(eventType string, data any) error {
		return callback.Handle(eventType, data)
	}

	// Execute with Engine
	h.logger.Info("Executing prompt via Engine",
		"session_id", msg.SessionID,
		"platform", msg.Platform,
		"prompt_len", len(msg.Content))

	err := h.engine.Execute(ctx, cfg, msg.Content, wrappedCallback)
	if err != nil {
		h.logger.Error("Engine execution failed",
			"session_id", msg.SessionID,
			"error", err)

		// Send error message back
		if h.adapters != nil {
			errMsg := &ChatMessage{
				Platform:  msg.Platform,
				SessionID: msg.SessionID,
				Content:   err.Error(),
				Metadata: map[string]any{
					"event_type": string(provider.EventTypeError),
				},
			}
			// Copy metadata from original message (channel_id, thread_ts)
			if msg.Metadata != nil {
				for k, v := range msg.Metadata {
					if k == "channel_id" || k == "thread_ts" {
						errMsg.Metadata[k] = v
					}
				}
			}
			if err := h.adapters.SendMessage(ctx, msg.Platform, msg.SessionID, errMsg); err != nil {
				h.logger.Error("Failed to send error message", "session_id", msg.SessionID, "error", err)
			}
		}
		return err
	}

	return nil
}

// updateThrottled sends throttled streaming updates to Slack
// Limits updates to 1 per second to avoid rate limiting
func (s *StreamState) updateThrottled(ctx context.Context, adapters *AdapterManager, platform, sessionID string, msg *base.ChatMessage) error {
	s.mu.Lock()

	// Check if this is the final message - always send final messages
	isFinal, _ := msg.Metadata["is_final"].(bool)

	// Throttle: max 1 update per second (skip for final messages)
	if !isFinal && time.Since(s.LastUpdated) < time.Second {
		s.mu.Unlock()
		return nil
	}
	// Set timestamp immediately to prevent race condition
	// Other goroutines will be throttled while we're sending
	s.LastUpdated = time.Now()
	s.mu.Unlock()

	// Add thread_ts and channel_id if present in metadata
	if msg.Metadata == nil {
		msg.Metadata = make(map[string]any)
	}
	if threadTS, ok := msg.Metadata["thread_ts"]; ok {
		msg.Metadata["thread_ts"] = threadTS
	}

	// Update existing message
	if s.ChannelID != "" && s.MessageTS != "" {
		msg.Metadata["message_ts"] = s.MessageTS
		msg.Metadata["channel_id"] = s.ChannelID
	}

	// Convert base.ChatMessage to ChatMessage for adapter
	chatMsg := convertToChatMessage(msg)

	// Send update
	err := adapters.SendMessage(ctx, platform, sessionID, chatMsg)

	// Update timestamp on success
	s.mu.Lock()
	if err == nil {
		s.LastUpdated = time.Now()
	} else {
		s.LastUpdated = time.Time{} // Reset on error to allow retry
	}
	s.mu.Unlock()

	return err
}

// Flush forces a flush of any pending stream state
// This ensures the last message in a stream is sent immediately
func (s *StreamState) Flush() {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Reset LastUpdated to allow immediate send on next update
	s.LastUpdated = time.Time{}
}

// =============================================================================
// Plan Mode Event Handlers
// =============================================================================

// handlePlanMode handles plan generation events (thinking with subtype=plan_generation)
func (c *StreamCallback) handlePlanMode(data any) error {
	var planContent string

	if m, ok := data.(*event.EventWithMeta); ok {
		planContent = m.EventData
		c.logger.Debug("handlePlanMode received",
			"data_type", fmt.Sprintf("%T", data),
			"event_data_len", len(planContent))
	}

	// Skip empty content
	if planContent == "" {
		return nil
	}

	// Send plan mode message with platform-agnostic MessageType
	msg := &base.ChatMessage{
		Type:    base.MessageTypePlanMode,
		Content: planContent,
		Metadata: map[string]any{
			"event_type": string(provider.EventTypePlanMode),
		},
	}
	msg.Metadata = c.mergeMetadata(msg.Metadata)
	return c.sendMessageAndGetTS(convertToChatMessage(msg))
}

// handleExitPlanMode handles exit plan mode requests (tool_use with name=ExitPlanMode)
func (c *StreamCallback) handleExitPlanMode(data any) error {
	var planSummary string

	if m, ok := data.(*event.EventWithMeta); ok {
		planSummary = m.EventData
		c.logger.Debug("handleExitPlanMode received",
			"data_type", fmt.Sprintf("%T", data),
			"plan_summary_len", len(planSummary))
	}

	// Use default message if empty
	if planSummary == "" {
		planSummary = "Plan content will be executed upon approval."
	}

	// Send exit plan mode message with platform-agnostic MessageType
	msg := &base.ChatMessage{
		Type:    base.MessageTypeExitPlanMode,
		Content: planSummary,
		Metadata: map[string]any{
			"event_type": string(provider.EventTypeExitPlanMode),
			"session_id": c.sessionID,
		},
	}
	msg.Metadata = c.mergeMetadata(msg.Metadata)
	return c.sendMessageAndGetTS(convertToChatMessage(msg))
}

// =============================================================================
// AskUserQuestion Event Handler (Degraded Mode)
// =============================================================================

// handleAskUserQuestion handles AskUserQuestion tool (degraded mode)
// Note: AskUserQuestion is not fully supported in headless mode,
// so we display the question as a text prompt
func (c *StreamCallback) handleAskUserQuestion(data any) error {
	var question string

	if m, ok := data.(*event.EventWithMeta); ok {
		question = m.EventData
		c.logger.Debug("handleAskUserQuestion received",
			"data_type", fmt.Sprintf("%T", data),
			"question_len", len(question))
	}

	// Skip empty question
	if question == "" {
		return nil
	}

	// Send ask user question message with platform-agnostic MessageType
	msg := &base.ChatMessage{
		Type:    base.MessageTypeAskUserQuestion,
		Content: question,
		Metadata: map[string]any{
			"event_type": string(provider.EventTypeAskUserQuestion),
		},
	}
	msg.Metadata = c.mergeMetadata(msg.Metadata)
	return c.sendMessageAndGetTS(convertToChatMessage(msg))
}

// =============================================================================
// Session Start / Engine Starting / User Message Received Event Handlers
// =============================================================================

// handleSessionStart handles session start events (cold start)
// Implements EventTypeSessionStart per spec (0.4)
// Triggered when user sends first message or CLI needs cold start
func (c *StreamCallback) handleSessionStart(data any) error {
	c.mu.Lock()
	c.sessionStartSent = true
	c.mu.Unlock()

	// Reaction lifecycle: set 📥 on session start
	c.setReaction("inbox_tray")

	var content string

	if m, ok := data.(*event.EventWithMeta); ok {
		content = m.EventData
	} else if s, ok := data.(string); ok {
		content = s
	} else {
		content = "Initializing AI assistant..."
	}

	sessionID := c.sessionID

	msg := &base.ChatMessage{
		Type:    base.MessageTypeSessionStart,
		Content: content,
		Metadata: map[string]any{
			"event_type": string(provider.EventTypeSessionStart),
			"session_id": sessionID,
		},
	}
	msg.Metadata = c.mergeMetadata(msg.Metadata)
	if err := c.sendMessageAndGetTS(convertToChatMessage(msg)); err != nil {
		return err
	}

	// Track starting message for 3s delayed deletion
	if ts, ok := msg.Metadata["message_ts"].(string); ok && ts != "" {
		if ch, ok := msg.Metadata["channel_id"].(string); ok && ch != "" {
			c.mu.Lock()
			c.startingMsgRecords = append(c.startingMsgRecords, msgRecord{
				ChannelID: ch,
				MessageTS: ts,
				ZoneIndex: ZoneInitialization,
				EventType: string(provider.EventTypeSessionStart),
			})
			c.mu.Unlock()
		}
	}

	return nil
}

// handleEngineStarting handles engine starting events (CLI cold start in progress)
// Implements EventTypeEngineStarting per spec (0.5)
// Triggered during CLI cold start when engine is being initialized
func (c *StreamCallback) handleEngineStarting(data any) error {
	var content string

	if m, ok := data.(*event.EventWithMeta); ok {
		content = m.EventData
	} else if s, ok := data.(string); ok {
		content = s
	} else {
		content = "Engine starting..."
	}

	msg := &base.ChatMessage{
		Type:    base.MessageTypeEngineStarting,
		Content: content,
		Metadata: map[string]any{
			"event_type": string(provider.EventTypeEngineStarting),
		},
	}
	msg.Metadata = c.mergeMetadata(msg.Metadata)
	if err := c.sendMessageAndGetTS(convertToChatMessage(msg)); err != nil {
		return err
	}

	// Track engine_starting message for 3s delayed deletion
	if ts, ok := msg.Metadata["message_ts"].(string); ok && ts != "" {
		if ch, ok := msg.Metadata["channel_id"].(string); ok && ch != "" {
			c.mu.Lock()
			c.startingMsgRecords = append(c.startingMsgRecords, msgRecord{
				ChannelID: ch,
				MessageTS: ts,
				ZoneIndex: ZoneInitialization,
				EventType: string(provider.EventTypeEngineStarting),
			})
			c.mu.Unlock()
		}
	}

	return nil
}

// handleUserMessageReceived handles user message received acknowledgment
// Implements EventTypeUserMessageReceived per spec (0.6)
// Triggered immediately after user message is received
func (c *StreamCallback) handleUserMessageReceived(_ any) error {
	// Per spec: context block with :inbox: emoji
	// Content must not be empty to avoid "no_text" error
	msg := &base.ChatMessage{
		Type:    base.MessageTypeUserMessageReceived,
		Content: "Message received",
		Metadata: map[string]any{
			"event_type": string(provider.EventTypeUserMessageReceived),
		},
	}
	msg.Metadata = c.mergeMetadata(msg.Metadata)
	return c.sendMessageAndGetTS(convertToChatMessage(msg))
}

// handlePermissionRequest handles permission request events
// Implements EventTypePermissionRequest per spec (7)
// Triggered when Claude Code requests user approval for tool execution
func (c *StreamCallback) handlePermissionRequest(data any) error {
	var req *provider.PermissionRequest
	var messageID string

	switch v := data.(type) {
	case *provider.PermissionRequest:
		req = v
		if req != nil {
			messageID = req.MessageID
		}
	case *event.EventWithMeta:
		// Try to parse EventData as PermissionRequest
		if v.EventData != "" {
			parsed, err := provider.ParsePermissionRequest([]byte(v.EventData))
			if err == nil {
				req = parsed
				messageID = parsed.MessageID
			}
		}
		// Also check Meta for tool info if parsing failed
		if req == nil && v.Meta != nil {
			// Create a synthetic request from metadata
			req = &provider.PermissionRequest{
				MessageID: c.sessionID + "-" + time.Now().Format("20060102150405"),
			}
			if v.Meta.ToolName != "" {
				messageID = c.sessionID + "-" + v.Meta.ToolID
			}
		}
	default:
		c.logger.Debug("Unknown permission request data type", "type", fmt.Sprintf("%T", data))
		return nil
	}

	if req == nil {
		c.logger.Debug("Empty permission request, skipping")
		return nil
	}

	// Get tool and input for display
	tool, input := req.GetToolAndInput()

	msg := &base.ChatMessage{
		Type:    base.MessageTypePermissionRequest,
		Content: "", // Content is built by MessageBuilder from metadata
		Metadata: map[string]any{
			"event_type": string(provider.EventTypePermissionRequest),
			"tool_name":  tool,
			"input":      input,
			"message_id": messageID,
			"session_id": c.sessionID,
			"decision":   req.Decision,
			"reason":     req.GetDescription(),
		},
	}
	msg.Metadata = c.mergeMetadata(msg.Metadata)
	return c.sendMessageAndGetTS(convertToChatMessage(msg))
}
