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

	// Status message state for dynamic event type indicator
	// Reuses thinking message infrastructure for in-place updates
	thinkingChannelID string           // Channel ID for status message updates
	thinkingMessageTS string           // Message TS for status message updates
	currentStatus     base.MessageType // Current status type (thinking, tool_use, answer)

	// Stream state for throttled updates
	streamState *StreamState
}

// StreamState tracks the state for streaming updates
type StreamState struct {
	ChannelID   string
	MessageTS   string
	LastUpdated time.Time
	mu          sync.Mutex
}

// NewStreamCallback creates a new StreamCallback
func NewStreamCallback(ctx context.Context, sessionID, platform string, adapters *AdapterManager, logger *slog.Logger, metadata map[string]any) *StreamCallback {
	cb := &StreamCallback{
		ctx:       ctx,
		sessionID: sessionID,
		platform:  platform,
		adapters:  adapters,
		logger:    logger,
		isFirst:   true,
		metadata:  metadata,
		processor: NewDefaultProcessorChain(logger),
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

	// Skip empty thinking content
	if thinkingContent == "" {
		return nil
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

	return slackAdapter.DeleteMessageSDK(c.ctx, c.thinkingChannelID, c.thinkingMessageTS)
}

// updateStatusMessage updates the status indicator message in-place
// It sends a base.ChatMessage with the appropriate MessageType
// The Adapter's MessageBuilder will handle conversion to platform-specific blocks
// NOTE: Caller must hold c.mu lock before calling this method
func (c *StreamCallback) updateStatusMessage(statusType base.MessageType, displayText string) error {
	// Skip if status hasn't changed (avoid redundant updates)
	if c.currentStatus == statusType && statusType != base.MessageTypeThinking {
		return nil
	}

	c.logger.Debug("Updating status message",
		"status_type", statusType,
		"display_text", displayText,
		"is_first", c.isFirst,
		"thinking_sent", c.thinkingSent)

	// Create message with platform-agnostic MessageType
	// The Adapter's MessageBuilder will convert this to Slack blocks
	msg := &base.ChatMessage{
		Type:     statusType,
		Content:  displayText,
		Metadata: c.copyMessageMetadata(),
	}
	msg.Metadata["stream"] = true
	msg.Metadata["event_type"] = string(statusType) // Critical for aggregator to know event type

	if c.isFirst {
		// First status event - create new message
		c.isFirst = false
		c.currentStatus = statusType

		// Send new message and capture ts for future updates
		if err := c.sendMessageAndGetTS(convertToChatMessage(msg)); err != nil {
			// Reset state on send failure to allow retry
			c.isFirst = true
			c.currentStatus = ""
			return err
		}

		// Extract ts from metadata after successful send
		if ts, ok := msg.Metadata["message_ts"].(string); ok && ts != "" {
			c.thinkingMessageTS = ts
			if channelID, ok := msg.Metadata["channel_id"].(string); ok {
				c.thinkingChannelID = channelID
			}
			// Only set thinkingSent=true AFTER successful send and TS capture
			c.thinkingSent = true
			c.logger.Debug("Captured status message ts for updates", "ts", ts, "channel_id", c.thinkingChannelID)
		} else {
			// TS not captured - log warning but still mark as sent
			c.logger.Warn("Failed to capture status message ts, updates may not work")
			c.thinkingSent = true
		}

		return nil
	} else if c.thinkingSent {
		// Subsequent status event - update the existing message
		c.currentStatus = statusType

		if c.thinkingMessageTS != "" && c.thinkingChannelID != "" {
			// Use message_ts to update existing message
			msg.Metadata["message_ts"] = c.thinkingMessageTS
			msg.Metadata["channel_id"] = c.thinkingChannelID

			return c.adapters.SendMessage(c.ctx, c.platform, c.sessionID, convertToChatMessage(msg))
		}

		// Fallback: send as new message
		return c.adapters.SendMessage(c.ctx, c.platform, c.sessionID, convertToChatMessage(msg))
	}

	return nil
}

// convertToChatMessage converts base.ChatMessage to ChatMessage (local type)
func convertToChatMessage(msg *base.ChatMessage) *ChatMessage {
	return &ChatMessage{
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
			"input":      input,
			"truncated":  truncated,
			"event_type": string(provider.EventTypeToolUse),
		},
	}
	msg.Metadata = c.mergeMetadata(msg.Metadata)
	return c.adapters.SendMessage(c.ctx, c.platform, c.sessionID, convertToChatMessage(msg))
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

	// Send tool result message with platform-agnostic MessageType
	msg := &base.ChatMessage{
		Type:    base.MessageTypeToolResult,
		Content: output,
		Metadata: map[string]any{
			"success":     success,
			"duration_ms": durationMs,
			"tool_name":   toolName,
			"file_path":   filePath,
			"event_type":  string(provider.EventTypeToolResult),
		},
	}
	msg.Metadata = c.mergeMetadata(msg.Metadata)
	return c.adapters.SendMessage(c.ctx, c.platform, c.sessionID, convertToChatMessage(msg))
}

func (c *StreamCallback) handleAnswer(data any) error {
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

	// Create platform-agnostic message with MessageType
	msg := &base.ChatMessage{
		Type:    base.MessageTypeAnswer,
		Content: answerContent,
		Metadata: map[string]any{
			"event_type": string(provider.EventTypeAnswer),
		},
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
		return c.streamState.updateThrottled(c.ctx, c.adapters, c.platform, c.sessionID, msg)
	}

	// Otherwise send as new message
	return c.adapters.SendMessage(c.ctx, c.platform, c.sessionID, convertToChatMessage(msg))
}

func (c *StreamCallback) handleError(data any) error {
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
	return c.adapters.SendMessage(c.ctx, c.platform, c.sessionID, convertToChatMessage(msg))
}

func (c *StreamCallback) handleDangerBlock(data any) error {
	var reason string
	switch v := data.(type) {
	case string:
		reason = v
	default:
		reason = "security_block"
	}

	// Send danger block message with platform-agnostic MessageType
	msg := &base.ChatMessage{
		Type:    base.MessageTypeDangerBlock,
		Content: reason,
		Metadata: map[string]any{
			"event_type": "security_block",
		},
	}
	msg.Metadata = c.mergeMetadata(msg.Metadata)
	return c.adapters.SendMessage(c.ctx, c.platform, c.sessionID, convertToChatMessage(msg))
}

// handleSessionStats handles session statistics events
// Implements EventTypeResult (Turn Complete)
func (c *StreamCallback) handleSessionStats(data any) error {
	stats, ok := data.(*event.SessionStatsData)
	if !ok {
		c.logger.Debug("session_stats: invalid data type", "type", fmt.Sprintf("%T", data))
		return nil
	}

	// Send stats message with platform-agnostic MessageType
	msg := &base.ChatMessage{
		Type:    base.MessageTypeSessionStats,
		Content: "",
		Metadata: map[string]any{
			"event_type":           "session_stats",
			"session_id":           stats.SessionID,
			"duration_ms":          stats.TotalDurationMs,
			"thinking_duration_ms": stats.ThinkingDurationMs,
			"tool_duration_ms":     stats.ToolDurationMs,
			"tokens_in":            stats.InputTokens,
			"tokens_out":           stats.OutputTokens,
			"total_tokens":         stats.TotalTokens,
			"tool_count":           stats.ToolCallCount,
			"tools_used":           stats.ToolsUsed,
			"files_modified":       stats.FilesModified,
		},
	}
	msg.Metadata = c.mergeMetadata(msg.Metadata)
	return c.adapters.SendMessage(c.ctx, c.platform, c.sessionID, convertToChatMessage(msg))
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
	return c.adapters.SendMessage(c.ctx, c.platform, c.sessionID, convertToChatMessage(msg))
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
	return c.adapters.SendMessage(c.ctx, c.platform, c.sessionID, convertToChatMessage(msg))
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
	return c.adapters.SendMessage(c.ctx, c.platform, c.sessionID, convertToChatMessage(msg))
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
	return c.adapters.SendMessage(c.ctx, c.platform, c.sessionID, convertToChatMessage(msg))
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
	return c.adapters.SendMessage(c.ctx, c.platform, c.sessionID, convertToChatMessage(msg))
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
	return c.adapters.SendMessage(c.ctx, c.platform, c.sessionID, convertToChatMessage(msg))
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
	return c.adapters.SendMessage(c.ctx, c.platform, c.sessionID, convertToChatMessage(msg))
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
		// Include message_ts for reaction updates and message tracking
		if messageTS, ok := c.metadata["message_ts"]; ok {
			metadata["message_ts"] = messageTS
		}
	}
	return metadata
}

// mergeMetadata merges the callback's stored metadata with the provided metadata
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
// Limits updates to 1 per second to avoid rate limiting
func (s *StreamState) updateThrottled(ctx context.Context, adapters *AdapterManager, platform, sessionID string, msg *base.ChatMessage) error {
	s.mu.Lock()

	// Throttle: max 1 update per second
	if time.Since(s.LastUpdated) < time.Second {
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
	chatMsg := &ChatMessage{
		Platform:    msg.Platform,
		SessionID:   msg.SessionID,
		UserID:      msg.UserID,
		Content:     msg.Content,
		MessageID:   msg.MessageID,
		Timestamp:   msg.Timestamp,
		Metadata:    msg.Metadata,
		RichContent: msg.RichContent,
	}

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
	return c.adapters.SendMessage(c.ctx, c.platform, c.sessionID, convertToChatMessage(msg))
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
	return c.adapters.SendMessage(c.ctx, c.platform, c.sessionID, convertToChatMessage(msg))
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
	return c.adapters.SendMessage(c.ctx, c.platform, c.sessionID, convertToChatMessage(msg))
}

// =============================================================================
// Session Start / Engine Starting / User Message Received Event Handlers
// =============================================================================

// handleSessionStart handles session start events (cold start)
// Implements EventTypeSessionStart per spec (0.4)
// Triggered when user sends first message or CLI needs cold start
func (c *StreamCallback) handleSessionStart(data any) error {
	var content string

	if m, ok := data.(*event.EventWithMeta); ok {
		content = m.EventData
	} else if s, ok := data.(string); ok {
		content = s
	} else {
		content = "Initializing AI assistant..."
	}

	// Get session ID from callback
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
	return c.adapters.SendMessage(c.ctx, c.platform, c.sessionID, convertToChatMessage(msg))
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
	return c.adapters.SendMessage(c.ctx, c.platform, c.sessionID, convertToChatMessage(msg))
}

// handleUserMessageReceived handles user message received acknowledgment
// Implements EventTypeUserMessageReceived per spec (0.6)
// Triggered immediately after user message is received
func (c *StreamCallback) handleUserMessageReceived(data any) error {
	// This event is typically sent before processing begins
	// to acknowledge receipt of user message with minimal latency

	msg := &base.ChatMessage{
		Type:    base.MessageTypeUserMessageReceived,
		Content: "",
		Metadata: map[string]any{
			"event_type": string(provider.EventTypeUserMessageReceived),
		},
	}
	msg.Metadata = c.mergeMetadata(msg.Metadata)
	return c.adapters.SendMessage(c.ctx, c.platform, c.sessionID, convertToChatMessage(msg))
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
	return c.adapters.SendMessage(c.ctx, c.platform, c.sessionID, convertToChatMessage(msg))
}
