package chatapps

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/hrygo/hotplex/chatapps/base"
	"github.com/hrygo/hotplex/chatapps/slack"
	"github.com/hrygo/hotplex/engine"
	"github.com/hrygo/hotplex/event"
	"github.com/hrygo/hotplex/provider"
	"github.com/hrygo/hotplex/types"
)

// EngineHolder holds the Engine instance and configuration for ChatApps integration
type EngineHolder struct {
	engine           *engine.Engine
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

	return &EngineHolder{
		engine:           eng,
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
func (h *EngineHolder) GetEngine() *engine.Engine {
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
	blockBuilder *slack.BlockBuilder

	// Status message state for dynamic event type indicator
	// Reuses thinking message infrastructure for in-place updates
	thinkingChannelID string           // Channel ID for status message updates
	thinkingMessageTS string           // Message TS for status message updates
	currentStatus     slack.StatusType // Current status type (thinking, tool_use, answer)

	// Stream state for throttled updates
	streamState *StreamState

	// ACK reaction state for Slack message reactions
	// Tracks the original user message to add reaction to
	reactionChannelID   string // Channel ID of original user message
	reactionMessageTS   string // Timestamp of original user message
	ackReactionAdded    bool   // Whether the ack reaction has been added
	ackReactionComplete bool   // Whether the processing is complete (reaction replaced)
}

// StreamState tracks the state for streaming updates
type StreamState struct {
	ChannelID          string
	MessageTS          string
	LastUpdated        time.Time
	ChatUpdateInterval time.Duration // Configurable update interval (default 3s)
	mu                 sync.Mutex
}

// NewStreamCallback creates a new StreamCallback
func NewStreamCallback(ctx context.Context, sessionID, platform string, adapters *AdapterManager, logger *slog.Logger, metadata map[string]any) *StreamCallback {
	cb := &StreamCallback{
		ctx:          ctx,
		sessionID:    sessionID,
		platform:     platform,
		adapters:     adapters,
		logger:       logger,
		isFirst:      true,
		metadata:     metadata,
		processor:    NewDefaultProcessorChain(logger),
		blockBuilder: slack.NewBlockBuilder(),
	}

	// Extract reaction metadata from original message
	if metadata != nil {
		if channelID, ok := metadata["channel_id"].(string); ok {
			cb.reactionChannelID = channelID
		}
		if messageTS, ok := metadata["message_id"].(string); ok {
			cb.reactionMessageTS = messageTS
		}
	}

	// Set callback as the sender for aggregated messages
	cb.processor.SetAggregatorSender(cb)

	return cb
}

// addAckReaction adds the eyes reaction to the original user message
// This indicates that the message is being processed
func (c *StreamCallback) addAckReaction() error {
	// Skip if not Slack platform or reaction already added
	if c.platform != "slack" {
		return nil
	}
	c.mu.Lock()
	if c.ackReactionAdded || c.reactionChannelID == "" || c.reactionMessageTS == "" {
		c.mu.Unlock()
		return nil
	}
	c.ackReactionAdded = true
	c.mu.Unlock()

	// Get Slack adapter
	adapter, ok := c.adapters.GetAdapter(c.platform)
	if !ok || adapter == nil {
		c.logger.Warn("No adapter for reaction", "platform", c.platform)
		return nil
	}

	slackAdapter, ok := adapter.(*slack.Adapter)
	if !ok {
		c.logger.Warn("Adapter is not a Slack adapter")
		return nil
	}

	// Add eyes reaction
	reaction := base.Reaction{
		Name:      "eyes",
		Channel:   c.reactionChannelID,
		Timestamp: c.reactionMessageTS,
	}

	if err := slackAdapter.AddReaction(c.ctx, reaction); err != nil {
		c.logger.Warn("Failed to add ack reaction", "error", err)
		return err
	}

	c.logger.Debug("Added ack reaction", "channel", c.reactionChannelID, "ts", c.reactionMessageTS)
	return nil
}

// completeAckReaction removes the eyes reaction and adds checkmark reaction
// This indicates that the message processing is complete
func (c *StreamCallback) completeAckReaction(success bool) error {
	// Skip if not Slack platform or reaction not added yet
	if c.platform != "slack" {
		return nil
	}

	c.mu.Lock()
	if c.ackReactionComplete {
		c.mu.Unlock()
		return nil
	}
	// Only mark complete if ack was added
	if !c.ackReactionAdded {
		c.mu.Unlock()
		return nil
	}
	c.ackReactionComplete = true
	channelID := c.reactionChannelID
	messageTS := c.reactionMessageTS
	c.mu.Unlock()

	if channelID == "" || messageTS == "" {
		return nil
	}

	// Get Slack adapter
	adapter, ok := c.adapters.GetAdapter(c.platform)
	if !ok || adapter == nil {
		c.logger.Warn("No adapter for reaction", "platform", c.platform)
		return nil
	}

	slackAdapter, ok := adapter.(*slack.Adapter)
	if !ok {
		c.logger.Warn("Adapter is not a Slack adapter")
		return nil
	}

	// Determine reaction based on success
	reactionName := "white_check_mark" // Success
	if !success {
		reactionName = "x" // Failure
	}

	// First remove the eyes reaction
	eyesReaction := base.Reaction{
		Name:      "eyes",
		Channel:   channelID,
		Timestamp: messageTS,
	}
	_ = slackAdapter.RemoveReaction(c.ctx, eyesReaction)

	// Then add the completion reaction
	completionReaction := base.Reaction{
		Name:      reactionName,
		Channel:   channelID,
		Timestamp: messageTS,
	}

	if err := slackAdapter.AddReaction(c.ctx, completionReaction); err != nil {
		c.logger.Warn("Failed to add completion reaction", "error", err)
		return err
	}

	c.logger.Debug("Completed ack reaction", "success", success, "reaction", reactionName,
		"channel", channelID, "ts", messageTS)
	return nil
}

// SendAggregatedMessage implements AggregatedMessageSender interface
// This is called by the MessageAggregatorProcessor when timer flushes buffered messages
func (c *StreamCallback) SendAggregatedMessage(ctx context.Context, msg *ChatMessage) error {
	c.logger.Info("SendAggregatedMessage called", "session_id", c.sessionID, "content_len", len(msg.Content))

	if c.adapters == nil {
		c.logger.Warn("No adapters in SendAggregatedMessage", "platform", c.platform)
		return nil
	}

	return c.adapters.SendMessage(ctx, c.platform, c.sessionID, msg)
}

// Handle implements event.Callback
func (c *StreamCallback) Handle(eventType string, data any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

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
	// Add ack reaction on first thinking event (indicates processing started)
	if c.isFirst {
		if err := c.addAckReaction(); err != nil {
			c.logger.Warn("Failed to add ack reaction", "error", err)
		}

		// Set user typing status to indicate AI is processing
		// This helps reduce "black hole" feeling
		if adapter, ok := c.adapters.GetAdapter(c.platform); ok {
			if slackAdapter, ok := adapter.(*slack.Adapter); ok {
				if userID, ok := c.metadata["user_id"].(string); ok {
					if err := slackAdapter.SetUserTypingStatus(c.ctx, userID, true); err != nil {
						c.logger.Warn("Failed to set user typing status", "error", err)
					}
				}
			}
		}
	}

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

	// Skip empty thinking content
	if thinkingContent == "" {
		return nil
	}

	// Use updateStatusMessage for dynamic status indicator
	// This reuses thinking message infrastructure for in-place updates
	return c.updateStatusMessage(slack.StatusThinking, thinkingContent)
}

// buildThinkingMessage constructs a thinking message with proper metadata
func (c *StreamCallback) buildThinkingMessage(blocks []map[string]any, isFirst bool) (*ChatMessage, error) {
	metadata := c.copyMessageMetadata()
	metadata["stream"] = true
	metadata["event_type"] = string(provider.EventTypeThinking)
	metadata["is_final"] = isFirst

	var blocksAny []any
	for _, b := range blocks {
		blocksAny = append(blocksAny, b)
	}

	return &ChatMessage{
		Platform:  c.platform,
		SessionID: c.sessionID,
		Content:   string(provider.EventTypeThinking),
		Metadata:  metadata,
		RichContent: &base.RichContent{
			Blocks: blocksAny,
		},
	}, nil
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

// deleteThinkingMessage deletes the thinking message from Slack
func (c *StreamCallback) deleteThinkingMessage() error {
	if c.thinkingChannelID == "" || c.thinkingMessageTS == "" {
		return nil
	}

	// Get Slack adapter and delete the message
	adapter, ok := c.adapters.GetAdapter(c.platform)
	if !ok || adapter == nil {
		return fmt.Errorf("adapter not found for platform: %s", c.platform)
	}

	slackAdapter, ok := adapter.(*slack.Adapter)
	if !ok {
		return fmt.Errorf("adapter is not a Slack adapter")
	}

	return slackAdapter.DeleteMessage(c.ctx, c.thinkingChannelID, c.thinkingMessageTS)
}

// updateStatusMessage updates the status indicator message in-place
// It reuses the thinking message infrastructure for efficient updates
// Supported status types: "thinking", "tool_use", "answer"
// NOTE: Caller must hold c.mu lock before calling this method
func (c *StreamCallback) updateStatusMessage(statusType slack.StatusType, displayText string) error {
	// Skip if status hasn't changed (avoid redundant updates)
	if c.currentStatus == statusType && statusType != slack.StatusThinking {
		return nil
	}

	c.logger.Debug("Updating status message",
		"status_type", statusType,
		"display_text", displayText,
		"is_first", c.isFirst,
		"thinking_sent", c.thinkingSent)

	// Build status blocks using the new BuildStatusBlock method
	blocks := c.blockBuilder.BuildStatusBlock(statusType, displayText)

	if c.isFirst {
		// First status event - create new message
		c.isFirst = false
		c.thinkingSent = true
		c.currentStatus = statusType

		// Send new message and capture ts for future updates
		msg, err := c.buildThinkingMessage(blocks, true)
		if err != nil {
			return err
		}

		if err := c.sendMessageAndGetTS(msg); err != nil {
			return err
		}

		// Extract ts from metadata after successful send
		if ts, ok := msg.Metadata["message_ts"].(string); ok && ts != "" {
			c.thinkingMessageTS = ts
			if channelID, ok := msg.Metadata["channel_id"].(string); ok {
				c.thinkingChannelID = channelID
			}
			c.logger.Debug("Captured status message ts for updates", "ts", ts, "channel_id", c.thinkingChannelID)
		}

		return nil
	} else if c.thinkingSent {
		// Subsequent status event - update the existing message
		c.currentStatus = statusType

		if c.thinkingMessageTS != "" && c.thinkingChannelID != "" {
			// Use message_ts to update existing message
			msg, err := c.buildThinkingMessage(blocks, false)
			if err != nil {
				return err
			}
			msg.Metadata["message_ts"] = c.thinkingMessageTS
			msg.Metadata["channel_id"] = c.thinkingChannelID

			return c.adapters.SendMessage(c.ctx, c.platform, c.sessionID, msg)
		}

		// Fallback: send as new message if we don't have ts
		return c.sendBlockMessage(string(provider.EventTypeThinking), blocks, false)
	}

	return nil
}

func (c *StreamCallback) handleToolUse(data any) error {
	c.logger.Debug("[TOOL] handleToolUse called", "data_type", fmt.Sprintf("%T", data))

	toolName := string(provider.EventTypeToolUse)
	input := ""
	truncated := false

	if m, ok := data.(*event.EventWithMeta); ok {
		c.logger.Debug("[TOOL] handleToolUse EventWithMeta",
			"event_data", m.EventData,
			"meta_tool_name", m.Meta.ToolName,
			"meta_input_summary", m.Meta.InputSummary)
		if m.Meta != nil && m.Meta.ToolName != "" {
			toolName = m.Meta.ToolName
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
	if err := c.updateStatusMessage(slack.StatusToolUse, toolName); err != nil {
		c.logger.Warn("Failed to update status for tool_use", "error", err)
	}

	c.logger.Debug("[TOOL] handleToolUse sending", "tool_name", toolName, "input_len", len(input))
	blocks := c.blockBuilder.BuildToolUseBlock(toolName, input, truncated)
	return c.sendBlockMessage(toolName, blocks, false)
}

func (c *StreamCallback) handleToolResult(data any) error {
	c.logger.Debug("[TOOL] handleToolResult called", "data_type", fmt.Sprintf("%T", data))

	success := true
	var durationMs int64
	var toolName string
	var filePath string
	output := ""

	_ = toolName // used in BuildToolResultBlock

	if m, ok := data.(*event.EventWithMeta); ok {
		c.logger.Debug("[TOOL] handleToolResult EventWithMeta",
			"event_data", m.EventData,
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
		if m.EventData != "" {
			output = m.EventData
		}
	}

	c.logger.Debug("[TOOL] handleToolResult sending", "success", success, "duration_ms", durationMs, "output_len", len(output))
	blocks := c.blockBuilder.BuildToolResultBlock(success, durationMs, output, false, toolName, filePath)
	return c.sendBlockMessage(string(provider.EventTypeToolResult), blocks, false)
}

func (c *StreamCallback) handleAnswer(data any) error {
	// Complete ack reaction on first answer (indicates processing succeeded)
	if c.isFirst {
		if err := c.completeAckReaction(true); err != nil {
			c.logger.Warn("Failed to complete ack reaction", "error", err)
		}
		c.isFirst = false
	}

	// Clear thinking message on first answer if it exists
	if c.thinkingSent && c.thinkingMessageTS != "" && c.thinkingChannelID != "" {
		c.logger.Debug("Deleting thinking message for answer",
			"ts", c.thinkingMessageTS,
			"channel", c.thinkingChannelID)
		if err := c.deleteThinkingMessage(); err != nil {
			c.logger.Warn("Failed to delete thinking message", "error", err)
		}
		c.thinkingSent = false
		c.thinkingMessageTS = ""
		c.thinkingChannelID = ""
	} else if c.thinkingSent {
		c.thinkingSent = false
		c.logger.Debug("Clearing thinking state for answer")
	}

	// Clear user typing status (AI finished processing)
	if adapter, ok := c.adapters.GetAdapter(c.platform); ok {
		if slackAdapter, ok := adapter.(*slack.Adapter); ok {
			if userID, ok := c.metadata["user_id"].(string); ok {
				if err := slackAdapter.SetUserTypingStatus(c.ctx, userID, false); err != nil {
					c.logger.Warn("Failed to clear user typing status", "error", err)
				}
			}
		}
	}

	var answerContent string
	switch v := data.(type) {
	case string:
		answerContent = v
	case *event.EventWithMeta:
		answerContent = v.EventData
	default:
		answerContent = fmt.Sprintf("%v", data)
	}

	if answerContent == "" {
		return nil
	}

	// Use throttled streaming update if we have a message to update
	if c.streamState != nil {
		return c.streamState.updateThrottled(c.ctx, c.adapters, c.platform, c.sessionID, answerContent, c.blockBuilder, c.metadata)
	}

	// Otherwise send as new message
	blocks := c.blockBuilder.BuildAnswerBlock(answerContent)
	return c.sendBlockMessage(string(provider.EventTypeAnswer), blocks, false)
}

func (c *StreamCallback) handleError(data any) error {
	// Complete ack reaction on error (indicates processing failed)
	if err := c.completeAckReaction(false); err != nil {
		c.logger.Warn("Failed to complete ack reaction", "error", err)
	}

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

	blocks := c.blockBuilder.BuildErrorBlock(errMsg, false)
	return c.sendBlockMessage(string(provider.EventTypeError), blocks, true)
}

func (c *StreamCallback) handleDangerBlock(data any) error {
	var reason string
	switch v := data.(type) {
	case string:
		reason = v
	default:
		reason = "security_block"
	}
	blocks := c.blockBuilder.BuildErrorBlock(reason, true)
	return c.sendBlockMessage("security_block", blocks, true)
}

// handleSessionStats handles session statistics events
func (c *StreamCallback) handleSessionStats(data any) error {
	stats, ok := data.(*event.SessionStatsData)
	if !ok {
		c.logger.Debug("session_stats: invalid data type", "type", fmt.Sprintf("%T", data))
		return nil
	}

	// Build rich session stats UI using card style (recommended default)
	blocks := c.blockBuilder.BuildSessionStatsBlock(stats, slack.StatsStyleCompact)
	if len(blocks) == 0 {
		return nil
	}

	// Send stats as informational message (not final)
	return c.sendBlockMessage("session_stats", blocks, false)
}

// sendBlockMessage sends a message with Slack blocks
func (c *StreamCallback) sendBlockMessage(content string, blocks []map[string]any, isFinal bool) error {
	if c.adapters == nil {
		c.logger.Debug("No adapters, skipping message send", "platform", c.platform)
		return nil
	}

	// Build metadata with original message's platform-specific data
	metadata := c.copyMessageMetadata()
	metadata["stream"] = true
	metadata["event_type"] = content
	metadata["is_final"] = isFinal

	// Initialize stream state on first non-thinking message
	if content != string(provider.EventTypeThinking) && c.streamState == nil {
		c.streamState = &StreamState{
			ChatUpdateInterval: 3 * time.Second, // Default: 3 seconds (per Slack recommendation)
		}
	}

	// Convert blocks to []any for RichContent
	var blocksAny []any
	for _, b := range blocks {
		blocksAny = append(blocksAny, b)
	}

	msg := &ChatMessage{
		Platform:  c.platform,
		SessionID: c.sessionID,
		Content:   content,
		Metadata:  metadata,
		RichContent: &base.RichContent{
			Blocks: blocksAny,
		},
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

	return c.adapters.SendMessage(c.ctx, c.platform, c.sessionID, processedMsg)
}

// copyMessageMetadata copies important metadata from original message
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
}

// NewEngineMessageHandler creates a new EngineMessageHandler
func NewEngineMessageHandler(engine *engine.Engine, adapters *AdapterManager, opts ...EngineMessageHandlerOption) *EngineMessageHandler {
	h := &EngineMessageHandler{
		engine:   engine,
		adapters: adapters,
		logger:   slog.Default(),
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

	// Create stream callback with original message metadata
	callback := NewStreamCallback(ctx, msg.SessionID, msg.Platform, h.adapters, h.logger, msg.Metadata)
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
			if err := h.adapters.SendMessage(ctx, msg.Platform, msg.SessionID, errMsg); err != nil {
				h.logger.Error("Failed to send error message", "session_id", msg.SessionID, "error", err)
			}
		}
		return err
	}

	return nil
}

// updateThrottled sends throttled streaming updates to Slack
// Uses configurable ChatUpdateInterval (default 3s) to avoid rate limiting
func (s *StreamState) updateThrottled(ctx context.Context, adapters *AdapterManager, platform, sessionID, content string, blockBuilder *slack.BlockBuilder, metadata map[string]any) error {
	s.mu.Lock()

	// Throttle: use configurable interval (default 3s per Slack recommendation)
	interval := s.ChatUpdateInterval
	if interval <= 0 {
		interval = 3 * time.Second // Fallback to default
	}
	if time.Since(s.LastUpdated) < interval {
		s.mu.Unlock()
		return nil
	}
	s.LastUpdated = time.Time{} // Mark as updating
	s.mu.Unlock()

	// Build blocks with content
	blocks := blockBuilder.BuildAnswerBlock(content)

	// Convert blocks to []any
	var blocksAny []any
	for _, b := range blocks {
		blocksAny = append(blocksAny, b)
	}

	// Create chat message for update
	msg := &ChatMessage{
		Platform:  platform,
		SessionID: sessionID,
		Content:   content,
		RichContent: &RichContent{
			Blocks: blocksAny,
		},
		Metadata: make(map[string]any),
	}

	// Copy metadata
	for k, v := range metadata {
		msg.Metadata[k] = v
	}

	// Add thread_ts if present
	if threadTS, ok := metadata["thread_ts"]; ok {
		msg.Metadata["thread_ts"] = threadTS
	}

	// Update existing message
	if s.ChannelID != "" && s.MessageTS != "" {
		msg.Metadata["message_ts"] = s.MessageTS
		msg.Metadata["channel_id"] = s.ChannelID
	}

	// Send update
	err := adapters.SendMessage(ctx, platform, sessionID, msg)

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

	blocks := c.blockBuilder.BuildPlanModeBlock(planContent)
	return c.sendBlockMessage(string(provider.EventTypePlanMode), blocks, false)
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

	blocks := c.blockBuilder.BuildExitPlanModeBlock(c.sessionID, planSummary)
	return c.sendBlockMessage(string(provider.EventTypeExitPlanMode), blocks, false)
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

	// Options are not available in this context, display question only
	blocks := c.blockBuilder.BuildAskUserQuestionBlock(question, nil)
	return c.sendBlockMessage(string(provider.EventTypeAskUserQuestion), blocks, false)
}
